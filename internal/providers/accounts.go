package providers

// Provider connections: the accounts people add, the key they use and what it
// costs (server/services/providers/account_service.js, minus the adapters).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// AuthFailuresBeforeDisable is how many confirmed rejections a connection may
// collect before it is switched off. Transient errors never disable one.
const AuthFailuresBeforeDisable = 3

// LocalDefinitionID is the definition of this server's own local models: its
// connections follow an admin's sharing policy, not a listing anyone can add.
const LocalDefinitionID = "neorouter_local"

// Catalog is the model catalog as the provider accounts use it
// (internal/catalog): a new connection lists its models at once, a change of
// billing reclassifies them, and a charge for a "free" model is recorded.
type Catalog interface {
	// SyncAccount lists and reconciles an account's models. The result is the
	// catalog's own sync report, returned to the caller unchanged.
	SyncAccount(ctx context.Context, accountID string) (any, error)
	// ReclassifyAccount recomputes free/paid for an account's models.
	ReclassifyAccount(ctx context.Context, accountID string) error
	// FlagModel records evidence about one model; true means it changed.
	FlagModel(ctx context.Context, ref int64, flags map[string]any) (bool, error)
}

// AccountLimits is what people stated for provider accounts (internal/quota's
// AccountLimits).
type AccountLimits interface {
	Get(accountID string) map[string]float64
	Set(accountID string, patch map[string]*float64, now time.Time) (map[string]float64, error)
}

// QuotaRecorder stores what a provider reported about an account's allowance
// (internal/quota's Service.SetState).
type QuotaRecorder interface {
	SetState(scope, metric string, obs QuotaObservation, source string, now time.Time) error
}

// QuotaObservation is one metric a provider reported. Its fields are quota's
// Observation, so the wiring passes one straight through.
type QuotaObservation struct {
	Metric    string
	Limit     *float64
	Remaining *float64
	ResetAt   *int64
}

// LocalModels is this server's local-model sharing policy
// (internal/localmodels): admins always may use the models, everyone else only
// when an admin shares them.
type LocalModels interface {
	// Allowed reports whether this person may reach the local models.
	Allowed(userID string) bool
	// Offered reports whether "NeoRouter Local" is offered to them: allowed and
	// at least one model installed.
	Offered(userID string) bool
}

// FixedResult is what marking a connection as fixed did
// (routing.MarkAccountFixed).
type FixedResult struct {
	Models  int `json:"models"`
	Chances int `json:"chances"`
}

// ConcurrencyLimit is an account's simultaneous-request limit and where it came
// from (routing's concurrency.limitFor).
type ConcurrencyLimit struct {
	Limit  int    `json:"limit"`
	Source string `json:"source"`
}

// PublicUser is the only account projection teammates see about each other
// (teams members.publicUser).
type PublicUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// Observable is one routed model an observation is about: the fields of a
// routing entry that billing_check.js reads.
type Observable struct {
	Ref          int64
	ID           string
	AccountID    string
	DefinitionID string
	BillingMode  BillingMode
	IsFree       bool
}

// Service owns provider connections. Every method is safe for concurrent use.
type Service struct {
	DB       *dbx.DB
	Registry *Registry
	// Cfg returns the current configuration.
	Cfg func() config.Config
	Log *slog.Logger
	// Secrets opens stored keys and seals new ones.
	Secrets *Sealer
	// HTTP is the provider HTTP layer: the adapters' outbound calls and the
	// sign-in flows.
	HTTP *Client
	// UpstreamTimeout is the default for one upstream call.
	UpstreamTimeout time.Duration
	// Getenv reads the environment overrides of the sign-in issuers; nil means
	// os.Getenv.
	Getenv func(string) string
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	// Catalog lists a new connection's models.
	Catalog Catalog
	// Limits are the limits people stated for an account.
	Limits AccountLimits
	// Quota stores what a provider reported about an allowance.
	Quota QuotaRecorder
	// Fixed forgets what routing held against a connection
	// (routing.MarkAccountFixed).
	Fixed func(accountID string) FixedResult
	// Concurrency reports an account's simultaneous-request limit, nil when
	// unknown (routing's concurrency.limitFor); slotPlan is the plan the account
	// selected, when it selected one.
	Concurrency func(accountID, definitionID, slotPlan string) *ConcurrencyLimit
	// LocalModels is this server's local-model sharing policy.
	LocalModels LocalModels
	// SlotWatch is told when an account's traffic limits change, so routing can
	// take a utilization snapshot around the write (slot_utilization.watch).
	SlotWatch func(accountID string)

	mu    sync.Mutex
	oauth *OAuth
}

// now is the service clock.
func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// config is the current configuration; a service built without one still works
// (the documented defaults).
func (s *Service) config() config.Config {
	if s.Cfg == nil {
		return config.Config{}
	}
	return s.Cfg()
}

// log is a logger even when none was wired.
func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// oauthFlows is the device-code sign-in machinery, built on first use.
func (s *Service) oauthFlows() *OAuth {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oauth == nil {
		s.oauth = NewOAuth(s.HTTP, s.Now, s.Getenv)
	}
	return s.oauth
}

// definitionOrThrow is registry.get with the API's refusal for an unknown id.
func (s *Service) definitionOrThrow(id string) (Definition, error) {
	definition, ok := s.Registry.Get(id)
	if !ok {
		return Definition{}, httpserver.New(http.StatusBadRequest, "UNKNOWN_PROVIDER", fmt.Sprintf("Unknown provider %q.", id))
	}
	return definition, nil
}

// List is the connections a person added, pool partners excluded (they are
// managed on the pool page).
func (s *Service) List(ctx context.Context, userID string) ([]PublicAccount, error) {
	rows, err := s.queryRows(ctx, `SELECT `+rowColumns+` FROM provider_accounts
		WHERE user_id = ? AND definition_id != 'neorouter_peer' ORDER BY created_at, rowid`, userID)
	if err != nil {
		return nil, err
	}
	out := make([]PublicAccount, 0, len(rows))
	for _, row := range rows {
		account, err := s.publicAccount(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, nil
}

// Get is one connection as the API returns it.
func (s *Service) Get(ctx context.Context, userID, accountID string) (PublicAccount, error) {
	row, err := s.GetRow(ctx, userID, accountID)
	if err != nil {
		return PublicAccount{}, err
	}
	return s.publicAccount(ctx, row)
}

// GetRow is the stored row of one of a person's connections.
func (s *Service) GetRow(ctx context.Context, userID, accountID string) (Row, error) {
	row, err := s.queryRow(ctx, `SELECT `+rowColumns+` FROM provider_accounts WHERE id = ? AND user_id = ?`, accountID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Row{}, httpserver.New(http.StatusNotFound, "ACCOUNT_NOT_FOUND", "This provider account does not exist.")
	}
	if err != nil {
		return Row{}, fmt.Errorf("providers: read account: %w", err)
	}
	return row, nil
}

// Runtime is one connection as an adapter sees it: the stored row with its
// secret opened. It returns nil when the account is gone.
func (s *Service) Runtime(ctx context.Context, accountID string) (*Runtime, error) {
	row, err := s.queryRow(ctx, `SELECT `+rowColumns+` FROM provider_accounts WHERE id = ?`, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("providers: read account: %w", err)
	}
	return s.runtimeOf(ctx, row)
}

// AdapterFor builds the protocol adapter of a connection.
func (s *Service) AdapterFor(ctx context.Context, runtime *Runtime) (Adapter, error) {
	definition, err := s.definitionOrThrow(runtime.DefinitionID)
	if err != nil {
		return nil, err
	}
	account := runtime.Account
	// A signed-in provider's token is renewed here, before an adapter sees it, so
	// a rotated refresh token is stored once and the adapter never has to guess.
	if err := s.refreshRuntime(ctx, runtime, &account); err != nil {
		return nil, err
	}
	return s.Registry.CreateAdapter(definition, AdapterDeps{
		HTTP:            s.HTTP,
		Account:         account,
		UpstreamTimeout: s.UpstreamTimeout,
		Log:             s.Log,
	})
}

// refreshRuntime renews a signed-in connection's tokens before an adapter sees
// them and stores the rotated secret: refresh tokens are single-use, so a
// refresh that is not stored leaves the saved connection with a dead one.
func (s *Service) refreshRuntime(ctx context.Context, runtime *Runtime, account *Account) error {
	if account.Secret == "" || account.ID == "" {
		// A connection that is not saved yet uses the token it just got.
		return nil
	}
	definition, known := s.Registry.Get(account.DefinitionID)
	if !known {
		return nil
	}
	persist := func(secret string) error { return s.storeSecret(ctx, account.ID, secret) }
	switch definition.Adapter {
	case AdapterCodex:
		_, secret, err := s.oauthFlows().FreshCodexCredentials(ctx, account.Secret, account.ID, persist)
		if err != nil {
			return err
		}
		account.Secret = secret
	case AdapterNous:
		_, secret, err := s.oauthFlows().FreshNousCredentials(ctx, account.Secret, account.ID, persist)
		if err != nil {
			return err
		}
		account.Secret = secret
	}
	return nil
}

// storeSecret writes a rotated secret back to the connection.
func (s *Service) storeSecret(ctx context.Context, accountID, secret string) error {
	sealed, err := s.Secrets.Seal(secret)
	if err != nil {
		return err
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET secret_encrypted = ? WHERE id = ?`, sealed, accountID); err != nil {
		return fmt.Errorf("providers: store refreshed secret: %w", err)
	}
	return nil
}

// CanServe reports whether a connection's adapter can do one thing (speech,
// embed, …): some providers list a kind of model through an API NeoRouter does
// not speak.
func (s *Service) CanServe(ctx context.Context, accountID string, method Capability) (bool, error) {
	runtime, err := s.Runtime(ctx, accountID)
	if err != nil || runtime == nil {
		return false, err
	}
	adapter, err := s.AdapterFor(ctx, runtime)
	if err != nil {
		return false, err
	}
	return adapter.Supports(method), nil
}

// ValidateAccount checks a stored connection's key and records the verdict.
func (s *Service) ValidateAccount(ctx context.Context, accountID string) (*Validation, error) {
	runtime, err := s.Runtime(ctx, accountID)
	if err != nil || runtime == nil {
		return nil, err
	}
	adapter, err := s.AdapterFor(ctx, runtime)
	if err != nil {
		return nil, err
	}
	result, err := adapter.Validate(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.RecordValidation(ctx, accountID, result); err != nil {
		return &result, err
	}
	return &result, nil
}

// RecordValidation stores a key check's verdict on the connection. Only
// repeated confirmed rejections disable an account; transient errors never do,
// so a flaky provider is not silently dropped.
func (s *Service) RecordValidation(ctx context.Context, accountID string, result Validation) error {
	var failures int
	err := s.DB.W.QueryRowContext(ctx, `SELECT auth_failures FROM provider_accounts WHERE id = ?`, accountID).Scan(&failures)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("providers: read account failures: %w", err)
	}
	next := 0
	if result.Status == ValidationInvalid {
		next = failures + 1
	}
	status := string(result.Status)
	switch {
	case result.Status == ValidationInvalid && next >= AuthFailuresBeforeDisable:
		status = "disabled"
	case result.Status == ValidationUnknown:
		status = "error"
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET status = ?, status_message = ?, auth_failures = ?,
		enabled = CASE WHEN ? = 'disabled' THEN 0 ELSE enabled END,
		last_validated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
		status, nullableString(result.Message), next, status, accountID); err != nil {
		return fmt.Errorf("providers: record validation: %w", err)
	}
	if status == "disabled" {
		s.log().Warn("provider account disabled after repeated auth failures", "accountId", accountID)
	}
	return nil
}

// AllEnabledIDs are the connections people switched on, for the background jobs
// that keep them in step.
func (s *Service) AllEnabledIDs(ctx context.Context) ([]string, error) {
	return s.ids(ctx, `SELECT id FROM provider_accounts WHERE enabled = 1`)
}

// PrepaidIDs are the enabled connections on a prepaid plan.
func (s *Service) PrepaidIDs(ctx context.Context) ([]string, error) {
	return s.ids(ctx, `SELECT id FROM provider_accounts WHERE enabled = 1 AND prepaid > 0`)
}

func (s *Service) ids(ctx context.Context, query string) ([]string, error) {
	rows, err := s.DB.R.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("providers: read account ids: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("providers: scan account id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("providers: read account ids: %w", err)
	}
	return out, nil
}

// ProbeQuota reads the balance and plan allowance the provider reports
// (quota.probe in the definition) and stores them, so routing and the limits
// page can use them.
func (s *Service) ProbeQuota(ctx context.Context, accountID string) ([]QuotaWindow, error) {
	runtime, err := s.Runtime(ctx, accountID)
	if err != nil || runtime == nil {
		return nil, err
	}
	adapter, err := s.AdapterFor(ctx, runtime)
	if err != nil {
		return nil, err
	}
	windows, err := adapter.FetchQuota(ctx)
	if err != nil {
		return nil, err
	}
	if s.Quota == nil {
		return windows, nil
	}
	now := s.now()
	for _, item := range windows {
		observation := QuotaObservation{Metric: item.Metric, Limit: item.Limit, Remaining: item.Remaining, ResetAt: item.ResetAt}
		if err := s.Quota.SetState("a:"+accountID, item.Metric, observation, "probe", now); err != nil {
			return windows, fmt.Errorf("providers: record quota: %w", err)
		}
	}
	return windows, nil
}
