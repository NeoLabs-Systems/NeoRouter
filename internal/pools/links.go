package pools

// One partner link in both directions:
//
//	inbound   a router key on this instance the partner calls us with. Its own
//	          cost settings are not used: the share policy decides what it
//	          reaches. Hidden from the keys page, and stored as a hash like every
//	          key.
//	outbound  a provider account ("Pool partner") holding the partner's key for
//	          us, encrypted like every provider key. Through it the router uses
//	          the partner like any other connection.

import (
	"context"
	"errors"
)

// IsPeerEntry reports whether a catalog entry belongs to a pool partner's
// connection (isPeerEntry).
func IsPeerEntry(definitionID string) bool { return definitionID == PeerDefinition }

// issueInboundKey creates the key the partner will use us with and returns its
// secret (sent to the partner once, never stored in clear). It is made with the
// lowest priority, free models only, so the owner's own requests go first.
func (s *Service) issueInboundKey(ctx context.Context, userID string, link *peerLink) (string, error) {
	if s.RouterKeys == nil {
		return "", errors.New("pools: no router key issuer configured")
	}
	keyID, secret, err := s.RouterKeys.CreatePoolKey(ctx, userID, "Pool: "+link.Name)
	if err != nil {
		return "", err
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE router_keys SET peer_link_id = ? WHERE id = ?`, link.ID, keyID); err != nil {
		return "", err
	}
	if err := s.updateLink(ctx, link.ID, linkUpdate{RouterKeyID: &keyID}); err != nil {
		return "", err
	}
	return secret, nil
}

// connectOutbound adds the partner as a connection. A partner that cannot be
// reached right now (behind NAT, offline) still gets the link; the error is shown
// and the regular model sync tries again.
func (s *Service) connectOutbound(ctx context.Context, userID string, link *peerLink, peerKey string) {
	if s.Accounts == nil {
		s.linkFailure(ctx, link, errors.New("pools: no accounts service configured"))
		return
	}
	accountID, err := s.Accounts.AddPoolPeer(ctx, userID, PeerAccount{
		Label:   link.Name,
		Slug:    "pool-" + link.Name,
		BaseURL: link.URL + "/v1",
		Secret:  peerKey,
	})
	if err != nil {
		s.log().Warn("Pool partner could not be added as a connection", "linkId", link.ID, "error", err)
		s.linkFailure(ctx, link, err)
		return
	}
	if err := s.updateLink(ctx, link.ID, linkUpdate{AccountID: &accountID, ClearError: true}); err != nil {
		s.log().Warn("Could not store the partner's connection", "linkId", link.ID, "error", err)
		return
	}
	updated := *link
	updated.AccountID = &accountID
	s.applyEnabled(ctx, userID, &updated)
	if s.Catalog != nil {
		if err := s.Catalog(ctx, accountID); err != nil {
			message := err.Error()
			_ = s.updateLink(ctx, link.ID, linkUpdate{LastError: &message})
		}
	}
}

// linkFailure records why a partner's connection could not be made.
func (s *Service) linkFailure(ctx context.Context, link *peerLink, err error) {
	message := err.Error()
	if updateErr := s.updateLink(ctx, link.ID, linkUpdate{LastError: &message}); updateErr != nil {
		s.log().Warn("Could not record a partner link failure", "linkId", link.ID, "error", updateErr)
	}
}

// teardown removes both directions at once: the partner's key stops working here
// immediately, whatever the partner does.
func (s *Service) teardown(ctx context.Context, userID string, link *peerLink) {
	if link == nil {
		return
	}
	if link.RouterKeyID != nil {
		if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM router_keys WHERE id = ? AND user_id = ?`,
			*link.RouterKeyID, userID); err != nil {
			s.log().Warn("Could not remove a partner key", "linkId", link.ID, "error", err)
		}
	}
	if link.AccountID != nil && s.Accounts != nil {
		if err := s.Accounts.Remove(ctx, userID, *link.AccountID); err != nil {
			s.log().Debug("A partner connection was already gone", "linkId", link.ID, "error", err)
		}
	}
	if err := s.deleteLink(ctx, link.ID); err != nil {
		s.log().Warn("Could not remove a partner link", "linkId", link.ID, "error", err)
	}
	s.forgetShare(link.ID)
}

// applyEnabled uses the partner's connection only while neither this link nor the
// whole pool is paused.
func (s *Service) applyEnabled(ctx context.Context, userID string, link *peerLink) {
	if link == nil || link.AccountID == nil {
		return
	}
	owner, err := s.pool(ctx, userID, link.PoolID)
	if err != nil {
		s.log().Warn("Could not read a pool while pausing a partner", "error", err)
		return
	}
	usable := link.Status == "active" && !(owner != nil && owner.Paused)
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET enabled = ? WHERE id = ? AND user_id = ?`,
		boolInt(usable), *link.AccountID, userID); err != nil {
		s.log().Warn("Could not enable a partner connection", "linkId", link.ID, "error", err)
	}
}

// setPaused pauses (or resumes) giving to and taking from one partner.
func (s *Service) setPaused(ctx context.Context, userID string, link *peerLink, paused bool) error {
	status := "active"
	if paused {
		status = "paused"
	}
	if err := s.updateLink(ctx, link.ID, linkUpdate{Status: &status}); err != nil {
		return err
	}
	updated := *link
	updated.Status = status
	s.applyEnabled(ctx, userID, &updated)
	return nil
}
