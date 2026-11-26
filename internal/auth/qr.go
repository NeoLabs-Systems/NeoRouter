package auth

import (
	"context"
	"net/url"

	"github.com/NeoLabs-Systems/neolabs-go/qrlogin"
	"github.com/NeoLabs-Systems/neolabs-go/userid"
)

// QR sign-in runs on neolabs-go/qrlogin: a signed-out screen shows a code, a
// signed-in device approves it and the screen claims a session.

func (s *Service) qr() *qrlogin.Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.qrStore == nil {
		s.qrStore = &qrlogin.Store{DB: s.DB}
	}
	return s.qrStore
}

// QRChallenge is what the signing-in screen shows and polls with.
type QRChallenge struct {
	ChallengeID string `json:"challengeId"`
	PollToken   string `json:"pollToken"`
	ExpiresAt   string `json:"expiresAt"`
	Status      string `json:"status"`
	QRPayload   string `json:"qrPayload"`
	BackendURL  string `json:"backendUrl"`
}

// createQRChallenge starts a sign-in another signed-in device can approve. The
// code is a link into the web app, which any phone camera can open; the app
// asks for an explicit approval there.
func (s *Service) createQRChallenge(ctx context.Context, baseURL string, request qrlogin.Request) (QRChallenge, error) {
	created, err := s.qr().Create(ctx, request)
	if err != nil {
		return QRChallenge{}, err
	}
	query := url.Values{"qr_login": {"1"}, "challenge": {created.ChallengeID}, "secret": {created.ApproveSecret}}
	return QRChallenge{
		ChallengeID: created.ChallengeID, PollToken: created.PollToken,
		ExpiresAt: created.ExpiresAt, Status: created.Status,
		QRPayload: baseURL + "/app/?" + query.Encode(), BackendURL: baseURL,
	}, nil
}

// claimQRLogin turns an approved challenge into a session, once. The approving
// device was signed in, so no second factor is asked again.
func (s *Service) claimQRLogin(ctx context.Context, challengeID, pollToken string, rc RequestContext) (LoginResult, error) {
	claimed, err := s.qr().Claim(ctx, challengeID, pollToken)
	if err != nil {
		return LoginResult{}, err
	}
	return s.signIn(ctx, string(claimed.UserID), rc)
}

// approveQRLogin lets the signed-in account sign the other screen in.
func (s *Service) approveQRLogin(ctx context.Context, caller *Auth, challengeID, secret string, metadata any) (*qrlogin.Challenge, error) {
	return s.qr().Approve(ctx, challengeID, secret, userid.ID(caller.UserID), sessionDigest(caller.SessionID), metadata)
}
