package providers

// A connection as it is stored and as the API returns it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Row is one provider_accounts row as stored.
type Row struct {
	ID               string
	UserID           string
	DefinitionID     string
	Slug             string
	Label            string
	BaseURL          *string
	SecretEncrypted  *string
	OptionsJSON      string
	BillingMode      string
	Enabled          bool
	Status           string
	StatusMessage    *string
	AuthFailures     int
	LastValidatedAt  *string
	LastSyncedAt     *string
	CreatedAt        string
	BillingState     string
	BillingMessage   *string
	BillingCheckedAt *string
	CreatedByUserID  *string
	Prepaid          int
	PoolShare        int
	Classification   *string
	OverflowPolicy   string
	SlotWaitMS       *int64
}

// rowColumns is every stored column, in the order scanRow reads them.
const rowColumns = `id, user_id, definition_id, slug, label, base_url, secret_encrypted, options_json, billing_mode,
	enabled, status, status_message, auth_failures, last_validated_at, last_synced_at, created_at, billing_state,
	billing_message, billing_checked_at, created_by_user_id, prepaid, pool_share, classification, overflow_policy, slot_wait_ms`

// scanRow reads one row through a Scan function.
func scanRow(scan func(...any) error) (Row, error) {
	var row Row
	var enabled, prepaid, poolShare, authFailures int
	err := scan(
		&row.ID, &row.UserID, &row.DefinitionID, &row.Slug, &row.Label, &row.BaseURL, &row.SecretEncrypted,
		&row.OptionsJSON, &row.BillingMode, &enabled, &row.Status, &row.StatusMessage, &authFailures,
		&row.LastValidatedAt, &row.LastSyncedAt, &row.CreatedAt, &row.BillingState, &row.BillingMessage,
		&row.BillingCheckedAt, &row.CreatedByUserID, &prepaid, &poolShare, &row.Classification,
		&row.OverflowPolicy, &row.SlotWaitMS,
	)
	if err != nil {
		return Row{}, err
	}
	row.Enabled = enabled != 0
	row.Prepaid = prepaid
	row.PoolShare = poolShare
	row.AuthFailures = authFailures
	return row, nil
}

// Runtime is one connection as an adapter sees it: the stored row with its
// secret opened.
type Runtime struct {
	Account
	Slug        string
	Label       string
	BillingMode BillingMode
	Prepaid     int
	IsFlatRate  bool
	Enabled     bool
	Status      string
}

// runtimeOf opens a row's secret and reads its options.
func (s *Service) runtimeOf(ctx context.Context, row Row) (*Runtime, error) {
	options := map[string]any{}
	if text := strings.TrimSpace(row.OptionsJSON); text != "" && text != "{}" {
		if err := json.Unmarshal([]byte(text), &options); err != nil {
			return nil, fmt.Errorf("providers: read account options: %w", err)
		}
	}
	secret := ""
	if row.SecretEncrypted != nil && *row.SecretEncrypted != "" {
		opened, err := s.Secrets.Open(*row.SecretEncrypted)
		if err != nil {
			return nil, err
		}
		secret = opened
	}
	baseURL := ""
	if row.BaseURL != nil {
		baseURL = *row.BaseURL
	}
	return &Runtime{
		Account: Account{
			ID:           row.ID,
			UserID:       row.UserID,
			DefinitionID: row.DefinitionID,
			BaseURL:      baseURL,
			Secret:       secret,
			Options:      options,
			PublicOnly:   s.PublicOnly(ctx, row.UserID, nil),
		},
		Slug:        row.Slug,
		Label:       row.Label,
		BillingMode: BillingMode(row.BillingMode),
		Prepaid:     row.Prepaid,
		IsFlatRate:  row.Prepaid == PrepaidFlatRate,
		Enabled:     row.Enabled,
		Status:      row.Status,
	}, nil
}

// AccountModels is how many of an account's models are free and switched on.
type AccountModels struct {
	Total   int `json:"total"`
	Free    int `json:"free"`
	Enabled int `json:"enabled"`
}

// PublicAccount is one connection as the API returns it: the connect-screen
// facts, never the key.
type PublicAccount struct {
	ID            string `json:"id"`
	DefinitionID  string `json:"definitionId"`
	ProviderLabel string `json:"providerLabel"`
	// ProviderKey and ProviderName group the accounts of one provider into one
	// provider wherever models are shown or counted.
	ProviderKey       string             `json:"providerKey"`
	ProviderName      string             `json:"providerName"`
	Category          *string            `json:"category"`
	Slug              string             `json:"slug"`
	Label             string             `json:"label"`
	BaseURL           *string            `json:"baseUrl"`
	KeyPreview        *string            `json:"keyPreview"`
	Options           map[string]any     `json:"options"`
	BillingMode       BillingMode        `json:"billingMode"`
	OverflowPolicy    string             `json:"overflowPolicy"`
	SlotWaitMS        int64              `json:"slotWaitMs"`
	SlotMaxWaitMS     int64              `json:"slotMaxWaitMs"`
	PlanNote          *string            `json:"planNote"`
	BillingState      string             `json:"billingState"`
	BillingMessage    *string            `json:"billingMessage"`
	BillingCheckedAt  *string            `json:"billingCheckedAt"`
	BillingNote       *string            `json:"billingNote"`
	BillingVerifiable bool               `json:"billingVerifiable"`
	Classification    string             `json:"classification"`
	Enabled           bool               `json:"enabled"`
	Status            string             `json:"status"`
	StatusMessage     *string            `json:"statusMessage"`
	LastValidatedAt   *string            `json:"lastValidatedAt"`
	LastSyncedAt      *string            `json:"lastSyncedAt"`
	CreatedAt         string             `json:"createdAt"`
	CreatedBy         *PublicUser        `json:"createdBy"`
	Models            AccountModels      `json:"models"`
	FreeAvailable     bool               `json:"freeAvailable"`
	FreeTierAll       bool               `json:"freeTierAll"`
	Limits            map[string]float64 `json:"limits"`
	Concurrency       *ConcurrencyLimit  `json:"concurrency"`
	SharedInPools     []string           `json:"sharedInPools"`
}

// publicAccount renders one stored connection for the API.
func (s *Service) publicAccount(ctx context.Context, row Row) (PublicAccount, error) {
	definition, known := s.Registry.Get(row.DefinitionID)
	runtime, err := s.runtimeOf(ctx, row)
	if err != nil {
		return PublicAccount{}, err
	}
	limits := map[string]float64{}
	var concurrency *ConcurrencyLimit
	if s.Limits != nil {
		for metric, value := range s.Limits.Get(row.ID) {
			limits[metric] = value
		}
	}
	if s.Concurrency != nil {
		concurrency = s.Concurrency(row.ID, row.DefinitionID, runtime.OptionString("slotPlan"))
	}
	counts, err := s.modelCounts(ctx, row.ID)
	if err != nil {
		return PublicAccount{}, err
	}
	shared, err := s.sharedInPools(ctx, row.ID)
	if err != nil {
		return PublicAccount{}, err
	}
	createdBy, err := s.publicUser(ctx, row.CreatedByUserID)
	if err != nil {
		return PublicAccount{}, err
	}
	waits := s.waits()
	identity := s.ProviderIdentity(row.DefinitionID, row.Slug, row.Label, runtime.BaseURL)

	account := PublicAccount{
		ID:               row.ID,
		DefinitionID:     row.DefinitionID,
		ProviderLabel:    row.DefinitionID,
		ProviderKey:      identity.ProviderKey,
		ProviderName:     identity.ProviderName,
		Slug:             row.Slug,
		Label:            row.Label,
		BaseURL:          firstURL(row.BaseURL, definition.BaseURL),
		KeyPreview:       maskSecret(runtime.Secret),
		Options:          runtime.Options,
		BillingMode:      PublicBillingMode(row),
		OverflowPolicy:   firstNonEmpty(row.OverflowPolicy, "auto"),
		SlotWaitMS:       waits.SlotWait.Milliseconds(),
		SlotMaxWaitMS:    waits.SlotMaxWait.Milliseconds(),
		BillingState:     row.BillingState,
		BillingMessage:   textOrNil(row.BillingMessage),
		BillingCheckedAt: textOrNil(row.BillingCheckedAt),
		Classification:   firstNonEmpty(stringValue(row.Classification), "auto"),
		Enabled:          row.Enabled,
		Status:           row.Status,
		StatusMessage:    textOrNil(row.StatusMessage),
		LastValidatedAt:  textOrNil(row.LastValidatedAt),
		LastSyncedAt:     textOrNil(row.LastSyncedAt),
		CreatedAt:        row.CreatedAt,
		CreatedBy:        createdBy,
		Models:           counts,
		Limits:           limits,
		Concurrency:      concurrency,
		SharedInPools:    shared,
	}
	if known {
		label := definition.Label
		account.ProviderLabel = label
		category := string(definition.Category)
		account.Category = &category
		if definition.Billing.Plan != nil {
			account.PlanNote = textOrNil(&definition.Billing.Plan.Note)
		}
		account.BillingNote = textOrNil(&definition.Billing.Note)
		account.BillingVerifiable = definition.Billing.Verify != nil
		account.FreeTierAll = definition.Billing.FreeTierAll
		account.FreeAvailable = s.declaresFreeModels(definition.Billing) || counts.Free > 0
	}
	if row.SlotWaitMS != nil {
		account.SlotWaitMS = *row.SlotWaitMS
	}
	return account, nil
}

// waits is the slot-queue waits the public catalog reports.
func (s *Service) waits() Waits {
	routing := s.config().Routing
	return Waits{SlotWait: routing.SlotQueueWait, SlotMaxWait: routing.SlotMaxWait}
}

// modelCounts is how many of an account's models are listed, free and enabled.
func (s *Service) modelCounts(ctx context.Context, accountID string) (AccountModels, error) {
	var total, free, enabled sql.NullInt64
	err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) total, SUM(is_free) free, SUM(enabled) enabled
		FROM models WHERE account_id = ? AND available = 1 AND capability NOT IN ('router', 'other')`, accountID).
		Scan(&total, &free, &enabled)
	if err != nil {
		return AccountModels{}, fmt.Errorf("providers: count account models: %w", err)
	}
	return AccountModels{Total: int(total.Int64), Free: int(free.Int64), Enabled: int(enabled.Int64)}, nil
}

// sharedInPools are the pools this account's free models are shared in.
func (s *Service) sharedInPools(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT pool_id FROM pool_shared_accounts WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, fmt.Errorf("providers: read shared pools: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("providers: scan shared pool: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("providers: read shared pools: %w", err)
	}
	return out, nil
}

// publicUser is the account projection teammates see (teams members.publicUser).
func (s *Service) publicUser(ctx context.Context, userID *string) (*PublicUser, error) {
	if userID == nil || *userID == "" {
		return nil, nil
	}
	var id, username string
	var displayName sql.NullString
	err := s.DB.R.QueryRowContext(ctx, `SELECT id, username, display_name FROM users WHERE id = ?`, *userID).
		Scan(&id, &username, &displayName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("providers: read user: %w", err)
	}
	name := displayName.String
	if name == "" {
		name = username
	}
	return &PublicUser{ID: id, Username: username, DisplayName: name}, nil
}

// ProviderIdentity is how one provider is keyed and named wherever models are
// shown or counted.
type ProviderIdentity struct {
	ProviderKey  string `json:"providerKey"`
	ProviderName string `json:"providerName"`
}

var customLocalDefinition = regexp.MustCompile(`^custom|^local`)

// ProviderIdentity groups the accounts of one provider into one provider:
// several accounts with one provider (two Groq keys) share a key and a name
// wherever models are shown or counted. Custom endpoints are one provider per
// address.
func (s *Service) ProviderIdentity(definitionID, slug, label, baseURL string) ProviderIdentity {
	definition, known := s.Registry.Get(definitionID)
	// A pool partner is named after the partner, one provider entry each.
	if known && definition.Category == CategoryPool {
		return ProviderIdentity{ProviderKey: definitionID + ":" + slug, ProviderName: firstNonEmpty(label, slug)}
	}
	if !known || customLocalDefinition.MatchString(definitionID) {
		address := firstNonEmpty(baseURL, slug)
		// A custom endpoint keeps the generic label it was created with
		// ("Custom OpenAI-compatible", "… 2"); its own name says more.
		name := slug
		if label != "" && (!known || !strings.HasPrefix(label, definition.Label)) {
			name = label
		}
		return ProviderIdentity{ProviderKey: definitionID + ":" + address, ProviderName: name}
	}
	return ProviderIdentity{ProviderKey: definitionID, ProviderName: definition.Label}
}

// firstURL is the account's base URL, else the definition's, else null.
func firstURL(value *string, fallback string) *string {
	if value != nil && *value != "" {
		return value
	}
	if fallback != "" {
		return &fallback
	}
	return nil
}

// optionalText keeps a text a connection may not have, mapping "" to null
// (`value || null`).
func textOrNil(value *string) *string {
	if value == nil || *value == "" {
		return nil
	}
	return value
}
