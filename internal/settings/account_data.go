package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Format is the export document version.
const Format = 1

const (
	exportKindAccount = "neorouter-account"
	exportKindKeys    = "neorouter-keys"
)

// internalDefinitions are connections NeoRouter makes for itself (pool
// partners); their keys are not the user's, so exports leave them out.
var internalDefinitions = []string{"neorouter_peer"}

// prepaidModes maps the stored prepaid marker to the paid arrangement it names.
var prepaidModes = map[int64]string{1: "plan", 2: "plan_paid", 3: "flat_rate"}

// Credentials is the confirmation a person types before exporting or deleting
// their account: the current password and, when two-factor is on, a code.
type Credentials struct {
	CurrentPassword string
	Code            string
}

// AuditEvent is one teams audit row.
type AuditEvent struct {
	Action        string
	ActorUserID   *string
	SubjectUserID *string
	Detail        any
}

// AccountData is what a person can do with their own account: take everything
// with them, take the provider keys they saved, or delete the account.
//
// Every step asks for the password again and, when two-factor is on, for a code;
// exporting the keys requires two-factor either way, because they leave the
// server in plain text.
//
// TwoFactorConfirm, TwoFactorEnabled, RoutingGroups, RecordAudit and LeavePool
// belong to other domains and must be wired by internal/app; ProviderLabel is
// optional. AccountData never imports them.
//
// The routes that serve these methods (POST /api/v1/auth/account/export,
// /export-keys and /delete) belong to auth; they set Cache-Control: no-store on
// the two exports.
type AccountData struct {
	DB     *dbx.DB
	Log    *slog.Logger
	Sealer Sealer
	// Now is the clock the exportedAt timestamps come from; nil means time.Now.
	Now func() time.Time

	// TwoFactorConfirm is auth two_factor_service.confirm: it verifies the
	// password, then the code when two-factor is on, and refuses outright when
	// requireFactor is set and two-factor is off. It returns the *HttpError the
	// route answers with (401 INVALID_PASSWORD, 403 TWO_FACTOR_REQUIRED, 401
	// INVALID_TWO_FACTOR).
	TwoFactorConfirm func(ctx context.Context, userID string, creds Credentials, requireFactor bool) error
	// TwoFactorEnabled is two_factor_service.isEnabled.
	TwoFactorEnabled func(ctx context.Context, userID string) (bool, error)
	// RoutingGroups is routing/groups.list, the account's routing groups as JSON.
	RoutingGroups func(ctx context.Context, userID string) (json.RawMessage, error)
	// RecordAudit is teams/audit.record.
	RecordAudit func(ctx context.Context, event AuditEvent)
	// LeavePool is the pools domain's leave: it tells a pool partner that this
	// person is gone.
	LeavePool func(ctx context.Context, userID, poolID string) error
	// ProviderLabel is the label of a provider definition; nil keeps the
	// definition id.
	ProviderLabel func(definitionID string) string
}

func (a *AccountData) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

// exportedAt is the current time in ISO 8601 UTC with milliseconds.
func (a *AccountData) exportedAt() string {
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	return now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// ExportData bundles everything stored for this person, without secrets.
func (a *AccountData) ExportData(ctx context.Context, userID string, creds Credentials) (*AccountExport, error) {
	if err := a.confirm(ctx, userID, creds, false); err != nil {
		return nil, err
	}
	user, err := a.userRow(ctx, userID)
	if err != nil {
		return nil, err
	}
	exportedAt := a.exportedAt()

	enabled, err := a.twoFactorEnabled(ctx, userID)
	if err != nil {
		return nil, err
	}
	data := &AccountExport{
		Format:     Format,
		Kind:       exportKindAccount,
		ExportedAt: exportedAt,
		Profile: ExportProfile{
			ID:          user.id,
			Username:    user.username,
			Email:       user.email,
			DisplayName: user.displayName,
			Role:        user.role,
			CreatedAt:   user.createdAt,
			LastLoginAt: user.lastLoginAt,
		},
		TwoFactor: ExportTwoFactor{Enabled: enabled},
	}
	if data.Settings, err = a.listSettings(ctx, userID); err != nil {
		return nil, err
	}
	if data.Connections, err = a.listConnections(ctx, userID); err != nil {
		return nil, err
	}
	if data.RoutingGroups, err = a.routingGroups(ctx, userID); err != nil {
		return nil, err
	}
	if data.RoutingProfiles, err = a.listRoutingProfiles(ctx, userID); err != nil {
		return nil, err
	}
	if data.RouterKeys, err = a.listRouterKeys(ctx, userID); err != nil {
		return nil, err
	}
	if data.Team, err = a.team(ctx, userID); err != nil {
		return nil, err
	}
	if data.Pools, err = a.listPools(ctx, userID); err != nil {
		return nil, err
	}
	if data.Requests, err = a.listRequests(ctx, userID); err != nil {
		return nil, err
	}
	if data.MediaJobs, err = a.listMediaJobs(ctx, userID); err != nil {
		return nil, err
	}
	a.log().Info("Account data exported", "userId", userID)
	return data, nil
}

// ExportKeys returns the provider keys this person saved, in plain text. It
// needs two-factor authentication, because the keys leave the server.
func (a *AccountData) ExportKeys(ctx context.Context, userID string, creds Credentials) (*KeysExport, error) {
	if err := a.confirm(ctx, userID, creds, true); err != nil {
		return nil, err
	}
	// The account must exist even when it holds no keys.
	if _, err := a.userRow(ctx, userID); err != nil {
		return nil, err
	}
	accounts, err := a.ownAccounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	keys := []ExportedKey{}
	for _, account := range accounts {
		if account.secretEncrypted == nil || *account.secretEncrypted == "" {
			continue
		}
		// The key material is read only when a value actually has to be
		// decrypted.
		if a.Sealer == nil {
			return nil, ErrNoSealer
		}
		secret, err := a.Sealer.Open(*account.secretEncrypted)
		if err != nil {
			return nil, fmt.Errorf("settings: open saved key of %q: %w", account.id, err)
		}
		concurrent, err := a.concurrentLimit(ctx, account.id)
		if err != nil {
			return nil, err
		}
		key := ExportedKey{
			Provider:       a.providerName(account.definitionID),
			DefinitionID:   account.definitionID,
			Label:          account.label,
			BaseURL:        account.baseURL,
			Secret:         secret,
			BillingMode:    publicBilling(account.billingMode, account.prepaid),
			Classification: "auto",
			OverflowPolicy: account.overflowPolicy,
			SlotWaitMS:     account.slotWaitMS,
			Concurrent:     concurrent,
		}
		if account.classification != nil && *account.classification != "" {
			key.Classification = *account.classification
		}
		// Everything else needed to add the connection again, with the headers
		// kept as their own member.
		if options, ok := parseOrderedObject([]byte(account.options)); ok {
			if headers, present := options.remove("headers"); present && jsTruthy(headers) {
				key.Headers = headers
			}
			if options.len() > 0 {
				key.Options = options
			}
		}
		keys = append(keys, key)
	}
	if a.RecordAudit == nil {
		return nil, errors.New("settings: RecordAudit is not wired")
	}
	detail := map[string]int{"count": len(keys)}
	a.RecordAudit(ctx, AuditEvent{
		Action:        "account.keys_exported",
		ActorUserID:   &userID,
		SubjectUserID: &userID,
		Detail:        detail,
	})
	a.log().Info("Saved keys exported", "userId", userID, "count", len(keys))
	return &KeysExport{
		Format:     Format,
		Kind:       exportKindKeys,
		ExportedAt: a.exportedAt(),
		Keys:       keys,
	}, nil
}

// DeleteAccount deletes the account and everything that belongs to it. Pools are
// left first, so partners are told; the last admin cannot go, because the server
// would have nobody to run it.
func (a *AccountData) DeleteAccount(ctx context.Context, userID string, creds Credentials) error {
	if err := a.confirm(ctx, userID, creds, false); err != nil {
		return err
	}
	user, err := a.userRow(ctx, userID)
	if err != nil {
		return err
	}
	if user.role == "admin" {
		var admins int64
		if err := a.DB.R.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL`).Scan(&admins); err != nil {
			return fmt.Errorf("settings: count admins: %w", err)
		}
		if admins <= 1 {
			return httpserver.New(http.StatusConflict, "LAST_ADMIN",
				"You are the only admin. Make someone else an admin first.")
		}
	}
	if err := a.LeavePools(ctx, userID); err != nil {
		return err
	}
	if a.RecordAudit == nil {
		return errors.New("settings: RecordAudit is not wired")
	}
	// The account is gone, so the audit row names no actor and no subject; the
	// username is kept because the row it came from is about to disappear.
	a.RecordAudit(ctx, AuditEvent{
		Action: "account.deleted",
		Detail: map[string]string{"username": user.username},
	})
	if _, err := a.DB.W.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("settings: delete account: %w", err)
	}
	a.log().Info("Account deleted by its owner", "userId", userID)
	return nil
}

// LeavePools takes an account that is about to be deleted out of its pools, so
// partners are told. A pool that cannot be left is logged and does not keep the
// account alive.
func (a *AccountData) LeavePools(ctx context.Context, userID string) error {
	if a.LeavePool == nil {
		return errors.New("settings: LeavePool is not wired")
	}
	poolIDs, err := a.poolIDs(ctx, userID)
	if err != nil {
		return err
	}
	for _, poolID := range poolIDs {
		if err := a.LeavePool(ctx, userID, poolID); err != nil {
			a.log().Warn("Could not tell a pool that the account is gone", "userId", userID,
				"poolId", poolID, "error", err)
		}
	}
	return nil
}

// confirm runs the password (and two-factor) confirmation that precedes every
// account-data step.
func (a *AccountData) confirm(ctx context.Context, userID string, creds Credentials, requireFactor bool) error {
	if a.TwoFactorConfirm == nil {
		return errors.New("settings: TwoFactorConfirm is not wired")
	}
	return a.TwoFactorConfirm(ctx, userID, creds, requireFactor)
}

func (a *AccountData) twoFactorEnabled(ctx context.Context, userID string) (bool, error) {
	if a.TwoFactorEnabled == nil {
		return false, errors.New("settings: TwoFactorEnabled is not wired")
	}
	return a.TwoFactorEnabled(ctx, userID)
}

func (a *AccountData) routingGroups(ctx context.Context, userID string) (json.RawMessage, error) {
	if a.RoutingGroups == nil {
		return nil, errors.New("settings: RoutingGroups is not wired")
	}
	groups, err := a.RoutingGroups(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: read routing groups: %w", err)
	}
	if len(groups) == 0 {
		return json.RawMessage("[]"), nil
	}
	return groups, nil
}

func (a *AccountData) providerName(definitionID string) string {
	if a.ProviderLabel == nil {
		return definitionID
	}
	if label := a.ProviderLabel(definitionID); label != "" {
		return label
	}
	return definitionID
}

// publicBilling is the reverse of the stored prepaid marker, else the billing
// mode.
func publicBilling(mode string, prepaid int64) string {
	if name, ok := prepaidModes[prepaid]; ok {
		return name
	}
	return mode
}
