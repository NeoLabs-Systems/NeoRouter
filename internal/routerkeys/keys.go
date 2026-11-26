// Package routerkeys owns NeoRouter's router keys: the nrt_ tokens people and
// apps send as bearer credentials, the limits stored on them (model kinds,
// budget, tokens, rpm, concurrency, priority, expiry) and the per-key request
// and concurrency limits.
package routerkeys

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/google/uuid"
)

// Prefix is the scheme of a router key secret: nrt_<8 hex>_<32 random bytes>.
const Prefix = "nrt"

// Modes a key can be in: free models only, paid only, or both.
const (
	ModeFreeOnly = "free_only"
	ModePaid     = "paid"
	ModeBoth     = "both"
)

// PoolAccess values.
const (
	PoolOwn  = "own"
	PoolAlso = "pool"
)

// DefaultLevels are the priorities a key can have, lowest first (routing's
// priority.LEVELS).
var DefaultLevels = []string{"low", "normal", "high"}

// Field is an optional request field: Set says the client sent it, Value is
// what it sent (nil when the client sent null).
type Field[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON records that the field was present, then its value.
func (f *Field[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	if string(data) == "null" {
		f.Value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = &value
	return nil
}

// Input is a create or update request for a router key.
type Input struct {
	Name               Field[string]   `json:"name"`
	Mode               Field[string]   `json:"mode"`
	AllowFree          Field[bool]     `json:"allowFree"`
	AllowPlan          Field[bool]     `json:"allowPlan"`
	AllowPaid          Field[bool]     `json:"allowPaid"`
	BudgetUSD          Field[float64]  `json:"budgetUsd"`
	BudgetPeriod       Field[string]   `json:"budgetPeriod"`
	ConcurrentLimit    Field[int64]    `json:"concurrentLimit"`
	RPMLimit           Field[int64]    `json:"rpmLimit"`
	MaxPricePerMillion Field[float64]  `json:"maxPricePerMillion"`
	PoolAccess         Field[string]   `json:"poolAccess"`
	TokenLimit         Field[int64]    `json:"tokenLimit"`
	TokenPeriod        Field[string]   `json:"tokenPeriod"`
	Priority           Field[string]   `json:"priority"`
	AllowedModels      Field[[]string] `json:"allowedModels"`
	RoutingGroup       Field[string]   `json:"routingGroup"`
	Profile            Field[string]   `json:"profile"`
	ExpiresAt          Field[string]   `json:"expiresAt"`
}

// App marks a key made for a connected app ("Connect with NeoRouter"). Origin
// is nil for an app that has none (the OAuth device flow): the key then reports
// "origin": null, not an empty string.
type App struct {
	Name   string
	Origin *string
	Scopes []string
}

// Row is one stored router key.
type Row struct {
	ID              string
	UserID          string
	Name            string
	Prefix          string
	KeyHash         string
	Mode            string
	BudgetUSD       *float64
	BudgetPeriod    string
	RPMLimit        *int64
	ConcurrentLimit *int64
	AllowedModels   string
	Profile         string
	ExpiresAt       *string
	RevokedAt       *string
	LastUsedAt      *string
	CreatedAt       string
	CreatedByUserID *string
	AllowFree       bool
	AllowPlan       bool
	AppName         *string
	AppOrigin       *string
	AppScopes       *string
	TokenLimit      *int64
	TokenPeriod     string
	Priority        string
	MaxPricePerMil  *float64
	PoolAccess      string
	PeerLinkID      *string
	RoutingGroup    *string
	// UserDisabled is set by Authenticate: the owner's account is disabled.
	UserDisabled *string
}

const rowColumns = `k.id, k.user_id, k.name, k.prefix, k.key_hash, k.mode, k.budget_usd, k.budget_period,
	k.rpm_limit, k.concurrent_limit, k.allowed_models_json, k.profile, k.expires_at, k.revoked_at,
	k.last_used_at, k.created_at, k.created_by_user_id, k.allow_free, k.allow_plan, k.app_name,
	k.app_origin, k.app_scopes_json, k.token_limit, k.token_period, k.priority, k.max_price_per_million,
	k.pool_access, k.peer_link_id, k.routing_group`

// scanRow reads rowColumns, plus the owner's disabled_at when extra is set.
func scanRow(scan func(...any) error, extra ...any) (*Row, error) {
	var row Row
	var budget, maxPrice sql.NullFloat64
	var rpm, concurrent, tokenLimit sql.NullInt64
	var expires, revoked, lastUsed, createdBy, appName, appOrigin, appScopes, peerLink, routingGroup sql.NullString
	dest := []any{&row.ID, &row.UserID, &row.Name, &row.Prefix, &row.KeyHash, &row.Mode, &budget, &row.BudgetPeriod,
		&rpm, &concurrent, &row.AllowedModels, &row.Profile, &expires, &revoked,
		&lastUsed, &row.CreatedAt, &createdBy, &row.AllowFree, &row.AllowPlan, &appName,
		&appOrigin, &appScopes, &tokenLimit, &row.TokenPeriod, &row.Priority, &maxPrice,
		&row.PoolAccess, &peerLink, &routingGroup}
	dest = append(dest, extra...)
	if err := scan(dest...); err != nil {
		return nil, err
	}
	row.BudgetUSD = nullFloat(budget)
	row.MaxPricePerMil = nullFloat(maxPrice)
	row.RPMLimit = nullInt(rpm)
	row.ConcurrentLimit = nullInt(concurrent)
	row.TokenLimit = nullInt(tokenLimit)
	row.ExpiresAt = nullString(expires)
	row.RevokedAt = nullString(revoked)
	row.LastUsedAt = nullString(lastUsed)
	row.CreatedByUserID = nullString(createdBy)
	row.AppName = nullString(appName)
	row.AppOrigin = nullString(appOrigin)
	row.AppScopes = nullString(appScopes)
	row.PeerLinkID = nullString(peerLink)
	row.RoutingGroup = nullString(routingGroup)
	return &row, nil
}

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	out := v.Float64
	return &out
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	out := v.String
	return &out
}

// Pace is how a key's budget is pacing through its period.
type Pace struct {
	State    string
	Forecast *float64
}

// Budget is the narrow surface internal/routerkeys consumes from
// internal/budget; internal/app satisfies it.
type Budget interface {
	Spent(routerKeyID, budgetPeriod string, now time.Time) (float64, error)
	TokensUsed(routerKeyID, tokenPeriod string, now time.Time) (int64, error)
	PeriodEnd(period string, now time.Time) *int64
	Pace(routerKeyID string, budgetUSD *float64, budgetPeriod string, now time.Time) *Pace
}

// Space is the request's resolved space (internal/teams).
type Space struct {
	OwnerID string
	ActorID string
}

// Keys is the router key store. Every method is safe for concurrent use.
type Keys struct {
	DB  *dbx.DB
	Now func() time.Time
	Log *slog.Logger

	// Budget is the spend ledger the responses report (nil: no budget figures).
	Budget Budget
	// Profiles reports whether a routing profile exists for the person.
	Profiles func(userID, profile string) bool
	// RequireGroup answers whether a routing group exists for the person.
	RequireGroup func(userID, group string) error
	// PublicUser is the public user object of a key's creator (internal/teams).
	PublicUser func(userID string) any
	// Levels are the priorities a key accepts (routing's priority.LEVELS);
	// nil means DefaultLevels.
	Levels []string
	// Require wraps a handler with the permission check (internal/teams).
	Require func(permission string) func(http.Handler) http.Handler
	// Space resolves the request's space (internal/teams).
	Space func(ctx context.Context) (Space, bool)

	Concurrency *Concurrency

	mu  sync.Mutex
	rpm map[string][]int64
}

// New builds the store.
func New(db *dbx.DB) *Keys {
	return &Keys{DB: db, Now: time.Now, Concurrency: NewConcurrency(), rpm: map[string][]int64{}}
}

// SetClock replaces the clock (tests).
func (k *Keys) SetClock(now func() time.Time) {
	if now != nil {
		k.Now = now
	}
}

// Kinds is the stored mode and the model kinds a key allows.
type Kinds struct {
	Mode      string
	AllowFree bool
	AllowPlan bool
}

func boolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// ResolveKinds turns the free/plan/paid switches into the stored mode and
// flags. With paid and free both allowed, a used-up budget falls back to free
// models ("both"); paid without free stops there ("paid").
func ResolveKinds(in Input, row *Row) (Kinds, error) {
	currentFree, currentPlan, currentPaid := true, true, false
	if row != nil {
		currentFree = row.AllowFree
		currentPlan = row.AllowPlan
		currentPaid = row.Mode != ModeFreeOnly
	}
	if !in.AllowFree.Set && !in.AllowPlan.Set && !in.AllowPaid.Set {
		mode := ModeBoth
		if in.Mode.Set && in.Mode.Value != nil {
			mode = *in.Mode.Value
		} else if row != nil {
			mode = row.Mode
		}
		return Kinds{Mode: mode, AllowFree: currentFree, AllowPlan: currentPlan}, nil
	}
	free := pickBool(in.AllowFree, currentFree)
	plan := pickBool(in.AllowPlan, currentPlan)
	var paid bool
	switch {
	case in.AllowPaid.Set && in.AllowPaid.Value != nil:
		paid = *in.AllowPaid.Value
	case in.Mode.Set && in.Mode.Value != nil:
		paid = *in.Mode.Value != ModeFreeOnly
	default:
		paid = currentPaid
	}
	if !free && !plan && !paid {
		return Kinds{}, httpserver.New(http.StatusBadRequest, "NO_MODELS_ALLOWED", "Allow at least one kind of model: free, prepaid plans or paid.")
	}
	mode := ModeFreeOnly
	if paid {
		mode = ModePaid
		if free {
			mode = ModeBoth
		}
	}
	return Kinds{Mode: mode, AllowFree: free, AllowPlan: plan}, nil
}

func pickBool(field Field[bool], fallback bool) bool {
	if field.Set && field.Value != nil {
		return *field.Value
	}
	return fallback
}

// AssertExpiry refuses an expiry that is in the past or unreadable. Unrelated
// edits may preserve a past expiry; only changing it renews a key.
func AssertExpiry(expiresAt *string, previous *string, now time.Time) error {
	if expiresAt == nil {
		return nil
	}
	timestamp, ok := parseTime(*expiresAt)
	previousTime, previousOk := int64(0), false
	if previous != nil {
		previousTime, previousOk = parseTime(*previous)
	}
	if !ok || (!(previousOk && timestamp == previousTime) && timestamp <= now.UnixMilli()) {
		return httpserver.New(http.StatusBadRequest, "INVALID_KEY_EXPIRY", "Choose an expiry date and time in the future.")
	}
	return nil
}

// parseTime reads an ISO instant into epoch milliseconds.
func parseTime(value string) (int64, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UnixMilli(), true
		}
	}
	return 0, false
}

// isoTime formats epoch milliseconds the way Date.toISOString does.
func isoTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// round6 rounds to six decimals.
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// Create stores a new key and returns it with the one-time secret.
func (k *Keys) Create(userID string, in Input, createdBy string, app *App) (*PublicKey, string, error) {
	if in.Name.Value == nil || *in.Name.Value == "" {
		return nil, "", httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", "name: Required")
	}
	if err := AssertExpiry(in.ExpiresAt.Value, nil, k.Now()); err != nil {
		return nil, "", err
	}
	if err := k.assertProfile(userID, in.Profile.Value); err != nil {
		return nil, "", err
	}
	if in.RoutingGroup.Value != nil && *in.RoutingGroup.Value != "" {
		if err := k.requireGroup(userID, *in.RoutingGroup.Value); err != nil {
			return nil, "", err
		}
	}
	kinds, err := ResolveKinds(in, nil)
	if err != nil {
		return nil, "", err
	}
	prefixBytes := make([]byte, 4)
	if _, err := rand.Read(prefixBytes); err != nil {
		return nil, "", fmt.Errorf("routerkeys: generate prefix: %w", err)
	}
	token, err := passhash.RandomToken(32)
	if err != nil {
		return nil, "", fmt.Errorf("routerkeys: generate secret: %w", err)
	}
	id := uuid.NewString()
	prefixHex := hex.EncodeToString(prefixBytes)
	secret := Prefix + "_" + prefixHex + "_" + token

	var appName, appOrigin, appScopes any
	if app != nil {
		appName = app.Name
		if app.Origin != nil && *app.Origin != "" {
			appOrigin = *app.Origin
		}
		scopes, err := json.Marshal(app.Scopes)
		if err != nil {
			return nil, "", fmt.Errorf("routerkeys: encode app scopes: %w", err)
		}
		if app.Scopes == nil {
			scopes = []byte("[]")
		}
		appScopes = string(scopes)
	}
	allowedModels := "[]"
	if in.AllowedModels.Value != nil {
		encoded, err := json.Marshal(*in.AllowedModels.Value)
		if err != nil {
			return nil, "", fmt.Errorf("routerkeys: encode allowed models: %w", err)
		}
		allowedModels = string(encoded)
	}
	tokenPeriod := "monthly"
	if in.TokenPeriod.Value != nil {
		tokenPeriod = *in.TokenPeriod.Value
	}
	priority := "normal"
	if in.Priority.Value != nil {
		priority = *in.Priority.Value
	}
	poolAccess := PoolOwn
	if in.PoolAccess.Value != nil && *in.PoolAccess.Value == PoolAlso {
		poolAccess = PoolAlso
	}
	if _, err := k.DB.W.ExecContext(context.Background(), `INSERT INTO router_keys
		(id, user_id, name, prefix, key_hash, mode, budget_usd, budget_period, rpm_limit, concurrent_limit, allowed_models_json,
		 profile, expires_at, created_by_user_id, allow_free, allow_plan, app_name, app_origin, app_scopes_json, token_limit,
		 token_period, priority, max_price_per_million, pool_access)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, *in.Name.Value, prefixHex, passhash.DigestToken(secret), kinds.Mode,
		valueOr(in.BudgetUSD.Value, nil), stringOr(in.BudgetPeriod.Value, "total"),
		valueOr(in.RPMLimit.Value, nil), valueOr(in.ConcurrentLimit.Value, nil), allowedModels,
		stringOr(in.Profile.Value, "balanced"), valueOr(in.ExpiresAt.Value, nil),
		// A key the server issues itself (pool partners) was created by nobody.
		sql.NullString{String: createdBy, Valid: createdBy != ""},
		boolToInt(kinds.AllowFree), boolToInt(kinds.AllowPlan), appName, appOrigin, appScopes,
		valueOr(in.TokenLimit.Value, nil), tokenPeriod, priority, valueOr(in.MaxPricePerMillion.Value, nil), poolAccess); err != nil {
		return nil, "", fmt.Errorf("routerkeys: create key: %w", err)
	}
	if in.RoutingGroup.Value != nil {
		if _, err := k.DB.W.ExecContext(context.Background(), `UPDATE router_keys SET routing_group = ? WHERE id = ?`, *in.RoutingGroup.Value, id); err != nil {
			return nil, "", fmt.Errorf("routerkeys: set routing group: %w", err)
		}
	}
	key, err := k.Get(userID, id)
	if err != nil {
		return nil, "", err
	}
	return key, secret, nil
}

// patchOr is what a patch leaves in a nullable column: the value it sent (nil
// when it sent null), or the stored one when the field was left out.
func patchOr[T any](field Field[T], current *T) any {
	if !field.Set {
		if current == nil {
			return nil
		}
		return *current
	}
	if field.Value == nil {
		return nil
	}
	return *field.Value
}

func valueOr[T any](v *T, fallback *T) any {
	if v == nil {
		if fallback == nil {
			return nil
		}
		return *fallback
	}
	return *v
}

func stringOr(v *string, fallback string) string {
	if v == nil {
		return fallback
	}
	return *v
}

func (k *Keys) assertProfile(userID string, profile *string) error {
	if profile == nil || *profile == "" || k.Profiles == nil {
		return nil
	}
	if !k.Profiles(userID, *profile) {
		return httpserver.New(http.StatusBadRequest, "UNKNOWN_PROFILE", "This routing profile does not exist.")
	}
	return nil
}

func (k *Keys) requireGroup(userID, group string) error {
	if k.RequireGroup == nil {
		return nil
	}
	return k.RequireGroup(userID, group)
}

// Row reads one key of a person. Keys pool partners use are managed by the
// pool, not on the keys page.
func (k *Keys) Row(userID, id string) (*Row, error) {
	row, err := scanRow(k.DB.R.QueryRowContext(context.Background(),
		`SELECT `+rowColumns+` FROM router_keys k WHERE k.id = ? AND k.user_id = ? AND k.peer_link_id IS NULL`, id, userID).Scan)
	if err == sql.ErrNoRows {
		return nil, errKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("routerkeys: read key: %w", err)
	}
	return row, nil
}

var errKeyNotFound = httpserver.New(http.StatusNotFound, "KEY_NOT_FOUND", "This router key does not exist.")

// Get is one key in its public form.
func (k *Keys) Get(userID, id string) (*PublicKey, error) {
	row, err := k.Row(userID, id)
	if err != nil {
		return nil, err
	}
	return k.Public(row)
}

// List is every key of a person, newest first.
func (k *Keys) List(userID string) ([]PublicKey, error) {
	rows, err := k.DB.R.QueryContext(context.Background(),
		`SELECT `+rowColumns+` FROM router_keys k WHERE k.user_id = ? AND k.peer_link_id IS NULL ORDER BY k.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("routerkeys: list keys: %w", err)
	}
	defer rows.Close()
	out := []PublicKey{}
	for rows.Next() {
		row, err := scanRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("routerkeys: list keys: %w", err)
		}
		key, err := k.Public(row)
		if err != nil {
			return nil, err
		}
		out = append(out, *key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("routerkeys: list keys: %w", err)
	}
	return out, nil
}

// Update applies a patch and returns the key.
func (k *Keys) Update(userID, id string, in Input) (*PublicKey, error) {
	row, err := k.Row(userID, id)
	if err != nil {
		return nil, err
	}
	if in.ExpiresAt.Set {
		if err := AssertExpiry(in.ExpiresAt.Value, row.ExpiresAt, k.Now()); err != nil {
			return nil, err
		}
	}
	if err := k.assertProfile(userID, in.Profile.Value); err != nil {
		return nil, err
	}
	if in.RoutingGroup.Value != nil && *in.RoutingGroup.Value != "" {
		if err := k.requireGroup(userID, *in.RoutingGroup.Value); err != nil {
			return nil, err
		}
	}
	kinds, err := ResolveKinds(in, row)
	if err != nil {
		return nil, err
	}
	allowedModels := row.AllowedModels
	if in.AllowedModels.Value != nil {
		encoded, err := json.Marshal(*in.AllowedModels.Value)
		if err != nil {
			return nil, fmt.Errorf("routerkeys: encode allowed models: %w", err)
		}
		allowedModels = string(encoded)
	}
	poolAccess := row.PoolAccess
	if in.PoolAccess.Value != nil {
		poolAccess = PoolOwn
		if *in.PoolAccess.Value == PoolAlso {
			poolAccess = PoolAlso
		}
	}
	expiresAt := row.ExpiresAt
	if in.ExpiresAt.Set {
		expiresAt = in.ExpiresAt.Value
	}
	if _, err := k.DB.W.ExecContext(context.Background(), `UPDATE router_keys SET name = ?, mode = ?, budget_usd = ?, budget_period = ?,
		rpm_limit = ?, concurrent_limit = ?, allowed_models_json = ?, profile = ?, expires_at = ?, allow_free = ?, allow_plan = ?,
		token_limit = ?, token_period = ?, priority = ?, max_price_per_million = ?, pool_access = ? WHERE id = ?`,
		stringOr(in.Name.Value, row.Name), kinds.Mode, patchOr(in.BudgetUSD, row.BudgetUSD),
		stringOr(in.BudgetPeriod.Value, row.BudgetPeriod), patchOr(in.RPMLimit, row.RPMLimit),
		patchOr(in.ConcurrentLimit, row.ConcurrentLimit), allowedModels, stringOr(in.Profile.Value, row.Profile),
		valueOr(expiresAt, nil), boolToInt(kinds.AllowFree), boolToInt(kinds.AllowPlan),
		patchOr(in.TokenLimit, row.TokenLimit), stringOr(in.TokenPeriod.Value, row.TokenPeriod),
		stringOr(in.Priority.Value, row.Priority), patchOr(in.MaxPricePerMillion, row.MaxPricePerMil),
		poolAccess, id); err != nil {
		return nil, fmt.Errorf("routerkeys: update key: %w", err)
	}
	if in.RoutingGroup.Set {
		if _, err := k.DB.W.ExecContext(context.Background(), `UPDATE router_keys SET routing_group = ? WHERE id = ?`, in.RoutingGroup.Value, id); err != nil {
			return nil, fmt.Errorf("routerkeys: set routing group: %w", err)
		}
	}
	return k.Get(userID, id)
}

// Revoke marks a key revoked; changing anything else never unrevokes it.
func (k *Keys) Revoke(userID, id string) (*PublicKey, error) {
	if _, err := k.Row(userID, id); err != nil {
		return nil, err
	}
	if _, err := k.DB.W.ExecContext(context.Background(),
		`UPDATE router_keys SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("routerkeys: revoke key: %w", err)
	}
	return k.Get(userID, id)
}

// Remove deletes a key.
func (k *Keys) Remove(userID, id string) error {
	if _, err := k.Row(userID, id); err != nil {
		return err
	}
	if _, err := k.DB.W.ExecContext(context.Background(), `DELETE FROM router_keys WHERE id = ?`, id); err != nil {
		return fmt.Errorf("routerkeys: delete key: %w", err)
	}
	return nil
}

// Authenticate finds the key a bearer token belongs to, nil when the token is
// unknown, revoked, expired or its owner's account is disabled.
func (k *Keys) Authenticate(token string) (*Row, error) {
	if token == "" || !strings.HasPrefix(token, Prefix+"_") {
		return nil, nil
	}
	var disabled sql.NullString
	row, err := scanRow(k.DB.R.QueryRowContext(context.Background(),
		`SELECT `+rowColumns+`, u.disabled_at FROM router_keys k JOIN users u ON u.id = k.user_id WHERE k.key_hash = ?`,
		passhash.DigestToken(token)).Scan, &disabled)
	if disabled.Valid {
		row.UserDisabled = &disabled.String
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("routerkeys: authenticate: %w", err)
	}
	if row.RevokedAt != nil || row.UserDisabled != nil {
		return nil, nil
	}
	if row.ExpiresAt != nil {
		expiry, ok := parseTime(*row.ExpiresAt)
		if !ok || expiry <= k.Now().UnixMilli() {
			return nil, nil
		}
	}
	if _, err := k.DB.W.ExecContext(context.Background(),
		`UPDATE router_keys SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, row.ID); err != nil {
		return nil, fmt.Errorf("routerkeys: touch key: %w", err)
	}
	return row, nil
}

// RPMResult is whether a request may go ahead under the key's rate limit.
type RPMResult struct {
	Allowed           bool
	RetryAfterSeconds int
}

// RPMState is the key's current minute window without using it up.
type RPMState struct {
	Limit        int64 `json:"limit"`
	Remaining    int64 `json:"remaining"`
	ResetSeconds int   `json:"resetSeconds"`
}

// ConsumeRPM counts one request against the key's per-minute limit (a sliding
// 60 s window).
func (k *Keys) ConsumeRPM(key *Row, now time.Time) RPMResult {
	if key.RPMLimit == nil || *key.RPMLimit == 0 {
		return RPMResult{Allowed: true}
	}
	nowMS := now.UnixMilli()
	k.mu.Lock()
	defer k.mu.Unlock()
	recent := k.recentLocked(key.ID, nowMS)
	if int64(len(recent)) >= *key.RPMLimit {
		k.rpm[key.ID] = recent
		return RPMResult{Allowed: false, RetryAfterSeconds: max(1, int(ceilSeconds(recent[0]+60_000-nowMS)))}
	}
	k.rpm[key.ID] = append(recent, nowMS)
	return RPMResult{Allowed: true}
}

// RPMStateOf is the key's minute window as it stands.
func (k *Keys) RPMStateOf(key *Row, now time.Time) *RPMState {
	if key.RPMLimit == nil || *key.RPMLimit == 0 {
		return nil
	}
	nowMS := now.UnixMilli()
	k.mu.Lock()
	defer k.mu.Unlock()
	recent := k.recentLocked(key.ID, nowMS)
	resetSeconds := 0
	if len(recent) > 0 {
		resetSeconds = max(1, int(ceilSeconds(recent[0]+60_000-nowMS)))
	}
	return &RPMState{Limit: *key.RPMLimit, Remaining: max(0, *key.RPMLimit-int64(len(recent))), ResetSeconds: resetSeconds}
}

func (k *Keys) recentLocked(keyID string, now int64) []int64 {
	recent := k.rpm[keyID][:0]
	for _, at := range k.rpm[keyID] {
		if now-at < 60_000 {
			recent = append(recent, at)
		}
	}
	return recent
}

// ceilSeconds is ms/1000 rounded up without floating point drift.
func ceilSeconds(ms int64) int64 {
	if ms <= 0 {
		return 0
	}
	return (ms + 999) / 1000
}

// Reset drops the in-memory rate limit windows (tests).
func (k *Keys) Reset() {
	k.mu.Lock()
	defer k.mu.Unlock()
	clear(k.rpm)
	k.Concurrency.Reset()
}
