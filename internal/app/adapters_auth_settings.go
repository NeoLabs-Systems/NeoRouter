package app

// The auth, settings and teams seams: registration open, the account
// export/delete surface auth serves, the settings page's cross-domain status
// blocks, and the public user objects other domains show.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/auth"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/processing"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/settings"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
)

// actor is who a request is signed in as.
type actor struct {
	id   string
	role string
}

// actorOf is req.auth of the session middleware.
func actorOf(r *http.Request) (actor, bool) {
	session, ok := auth.FromContext(r.Context())
	if !ok || session == nil {
		return actor{}, false
	}
	return actor{id: session.UserID, role: session.User.Role}, true
}

// registrationGate answers whether registration is open beyond the first
// account.
type registrationGate struct {
	svc *settings.Service
}

func (g registrationGate) RegistrationOpen() bool {
	open, err := g.svc.RegistrationOpen(context.Background())
	return err == nil && open
}

// authAccountData is the settings domain's export/delete surface in the shape
// auth's account routes call.
type authAccountData struct {
	svc *settings.AccountData
}

func (a authAccountData) ExportData(ctx context.Context, userID, currentPassword, code string) (any, error) {
	return a.svc.ExportData(ctx, userID, settings.Credentials{CurrentPassword: currentPassword, Code: code})
}

func (a authAccountData) ExportKeys(ctx context.Context, userID, currentPassword, code string) (any, error) {
	return a.svc.ExportKeys(ctx, userID, settings.Credentials{CurrentPassword: currentPassword, Code: code})
}

func (a authAccountData) DeleteAccount(ctx context.Context, userID, currentPassword, code string) error {
	return a.svc.DeleteAccount(ctx, userID, settings.Credentials{CurrentPassword: currentPassword, Code: code})
}

// accountDataSeams fills the cross-domain func fields of the account data
// surface. Each one belongs to another domain, so the wiring supplies it.
func (d *deps) accountDataSeams() *settings.AccountData {
	account := &settings.AccountData{
		DB:     d.db,
		Log:    d.log,
		Sealer: d.sealer,
	}
	account.TwoFactorConfirm = func(ctx context.Context, userID string, creds settings.Credentials, requireFactor bool) error {
		return d.Auth.Confirm(ctx, userID, creds.CurrentPassword, creds.Code, requireFactor)
	}
	account.TwoFactorEnabled = func(ctx context.Context, userID string) (bool, error) {
		status, err := d.Auth.TwoFactorStatus(ctx, userID)
		return status.Enabled, err
	}
	account.RoutingGroups = func(ctx context.Context, userID string) (json.RawMessage, error) {
		return json.Marshal(d.Routing.Groups(userID))
	}
	account.RecordAudit = func(ctx context.Context, event settings.AuditEvent) {
		d.recordTeamAudit(ctx, event)
	}
	account.LeavePool = d.Pools.Leave
	if d.Registry != nil {
		registry := d.Registry
		account.ProviderLabel = func(definitionID string) string {
			if def, ok := registry.Get(definitionID); ok {
				return def.Label
			}
			return definitionID
		}
	}
	return account
}

// recordTeamAudit appends one team audit row (teams/audit.record). The teams
// domain keeps its own writer unexported, so the wiring writes the same row.
func (d *deps) recordTeamAudit(ctx context.Context, event settings.AuditEvent) {
	var detail []byte
	if event.Detail != nil {
		detail, _ = json.Marshal(event.Detail)
	}
	if len(detail) == 0 {
		detail = []byte(`{}`)
	}
	var actorID any
	if event.ActorUserID != nil {
		actorID = *event.ActorUserID
	}
	var subjectID any
	if event.SubjectUserID != nil {
		subjectID = *event.SubjectUserID
	}
	if _, err := d.db.W.ExecContext(ctx,
		`INSERT INTO team_audit (action, actor_user_id, subject_user_id, detail_json) VALUES (?, ?, ?, ?)`,
		event.Action, actorID, subjectID, string(detail)); err != nil {
		d.log.Warn("app: record team audit", "action", event.Action, "error", err)
	}
}

// settingsSeams fills the settings page's cross-domain status blocks.
func (d *deps) settingsSeams(svc *settings.Service, meta *catalog.Metadata, proc *processing.Service) {
	svc.MetadataStatus = func() any {
		status, err := meta.Status()
		if err != nil {
			d.log.Warn("app: metadata status", "error", err)
			return nil
		}
		return status
	}
	svc.SetArtificialAnalysisKey = func(ctx context.Context, key *string) error {
		value := ""
		if key != nil {
			value = *key
		}
		return meta.SetArtificialAnalysisKey(value)
	}
	svc.PrivacyModelStatus = func() any {
		return proc.Privacy.Status(context.Background())
	}
	svc.SetPrivacyModelEnabled = func(ctx context.Context, enabled bool) error {
		_, err := proc.Privacy.SetEnabled(ctx, enabled)
		return err
	}
}

// publicUserOf is one account as teammates see it (teams members.publicUser).
func (d *deps) publicUserOf(userID string) any {
	user, err := d.Teams.TeamUser(context.Background(), userID)
	if err != nil || user == nil {
		return nil
	}
	return user
}

// accountCardsOf lists a person's enabled connections for the live view.
func (d *deps) accountCardsOf(userID string) []routing.AccountCard {
	list, err := d.Accounts.List(context.Background(), userID)
	if err != nil {
		return nil
	}
	cards := make([]routing.AccountCard, 0, len(list))
	for _, account := range list {
		if !account.Enabled {
			continue
		}
		cards = append(cards, routing.AccountCard{
			ID:   account.ID,
			Key:  account.ProviderKey,
			Name: account.ProviderName,
			Slug: account.Slug,
		})
	}
	return cards
}

// requirePermissionFunc is the teams permission gate in the shape a domain that
// wraps a handler with it (media, gateway, localmodels, proxies) reads.
func requirePermissionFunc() func(permission string) func(http.Handler) http.Handler {
	return teams.RequirePermission
}

// requirePermissionHandler is the same gate in the shape catalog, discovery and
// the oauth pages read: it takes the handler directly.
func requirePermissionHandler(permission string, next http.Handler) http.Handler {
	return teams.RequirePermission(permission)(next)
}

// requirePermissionError is the gate in the shape routing's routes read: an
// error instead of a written response.
func requirePermissionError(r *http.Request, permission string) error {
	space, _ := teams.SpaceFromContext(r.Context())
	if space.Can(permission) {
		return nil
	}
	return httpserver.New(http.StatusForbidden, "PERMISSION_DENIED",
		"The team owner has not allowed this for you.", map[string]string{"permission": permission})
}
