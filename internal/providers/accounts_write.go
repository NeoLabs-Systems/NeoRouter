package providers

// Adding, changing and removing provider connections, and the traffic settings
// they share.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Create adds a connection: it checks the key with the provider, stores the
// account and records what the check concluded.
func (s *Service) Create(ctx context.Context, userID string, in Input, createdBy string, actor *Actor) (Created, error) {
	definition, err := s.definitionOrThrow(stringField(in.DefinitionID))
	if err != nil {
		return Created{}, err
	}
	baseURL := ""
	if definition.EditableBaseURL {
		baseURL = firstNonEmpty(stringField(in.BaseURL), definition.BaseURL)
	}
	options := map[string]any{}
	for name, value := range valueOr(in.Options.Value, map[string]any{}) {
		options[name] = value
	}
	if in.Headers.Set {
		options["headers"] = headerMap(in.Headers)
	}
	secret := stringField(in.Secret)
	if err := validateInput(definition, secret, baseURL, options); err != nil {
		return Created{}, err
	}
	if err := assertSlotPlan(definition, options); err != nil {
		return Created{}, err
	}
	requested := definition.Billing.DefaultMode
	if mode, set := billingModeOf(in.BillingMode); set {
		// The free tier is only a choice for a provider that has one.
		if mode != BillingFreeTier || s.declaresFreeModels(definition.Billing) || definition.Billing.TrustListingFree {
			requested = mode
		}
	}
	billingMode, prepaid := StoredBilling(requested)

	privateAllowed := s.mayReachPrivate(ctx, userID, actor)
	restricted := s.publicOnlyFor(ctx, userID, privateAllowed)
	if err := s.assertDestination(ctx, baseURL, restricted); err != nil {
		return Created{}, err
	}
	// The key is checked with a throwaway adapter: nothing is stored until the
	// provider accepts it.
	adapter, err := s.Registry.CreateAdapter(definition, AdapterDeps{
		HTTP:            s.HTTP,
		Account:         Account{UserID: userID, DefinitionID: definition.ID, BaseURL: baseURL, Secret: secret, Options: options, PublicOnly: restricted},
		UpstreamTimeout: s.UpstreamTimeout,
		Log:             s.Log,
	})
	if err != nil {
		return Created{}, err
	}
	result, err := adapter.Validate(ctx)
	if err != nil {
		return Created{}, err
	}
	if result.Status == ValidationInvalid {
		return Created{}, httpserver.New(http.StatusUnprocessableEntity, "KEY_REJECTED",
			fmt.Sprintf("%s rejected this key: %s", definition.Label, stringValue(result.Message)))
	}
	id := newAccountID()
	slug, err := s.uniqueSlug(ctx, userID, firstNonEmpty(stringField(in.Slug), definition.ID))
	if err != nil {
		return Created{}, err
	}
	// A second account of the same provider is "Groq 2" unless it is named.
	label := stringField(in.Label)
	if label == "" {
		siblings, err := s.count(ctx, `SELECT COUNT(*) FROM provider_accounts WHERE user_id = ? AND definition_id = ?`, userID, definition.ID)
		if err != nil {
			return Created{}, err
		}
		label = definition.Label
		if siblings > 0 {
			label = fmt.Sprintf("%s %d", definition.Label, siblings+1)
		}
	}
	sealed, err := s.seal(secret)
	if err != nil {
		return Created{}, err
	}
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return Created{}, fmt.Errorf("providers: encode options: %w", err)
	}
	status := "error"
	if result.Status == ValidationHealthy {
		status = "healthy"
	}
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO provider_accounts
		(id, user_id, definition_id, slug, label, base_url, secret_encrypted, options_json, billing_mode, status, status_message, last_validated_at, created_by_user_id, prepaid, private_allowed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'), ?, ?, ?)`,
		id, userID, definition.ID, slug, label, nullableEmpty(baseURL), sealed, string(optionsJSON), string(billingMode),
		status, nullableString(result.Message), nullableEmpty(createdBy), prepaid, boolInt(privateAllowed)); err != nil {
		return Created{}, fmt.Errorf("providers: create account: %w", err)
	}
	if value := stringField(in.Classification); value != "" && value != "auto" {
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET classification = ? WHERE id = ?`, value, id); err != nil {
			return Created{}, fmt.Errorf("providers: set classification: %w", err)
		}
	}
	if err := s.ConfigureTraffic(ctx, id, in); err != nil {
		return Created{}, err
	}
	row, err := s.GetRow(ctx, userID, id)
	if err != nil {
		return Created{}, err
	}
	account, err := s.publicAccount(ctx, row)
	if err != nil {
		return Created{}, err
	}
	return Created{Account: account, Validation: result}, nil
}

// Probe checks credentials and lists models without saving anything, so the UI
// can preview what an endpoint offers before the account is created.
func (s *Service) Probe(ctx context.Context, userID string, in Input, actor *Actor) (Probed, error) {
	definition, err := s.definitionOrThrow(stringField(in.DefinitionID))
	if err != nil {
		return Probed{}, err
	}
	baseURL := ""
	if definition.EditableBaseURL {
		baseURL = firstNonEmpty(stringField(in.BaseURL), definition.BaseURL)
	}
	options := map[string]any{}
	for name, value := range valueOr(in.Options.Value, map[string]any{}) {
		options[name] = value
	}
	if in.Headers.Set {
		options["headers"] = headerMap(in.Headers)
	}
	secret := stringField(in.Secret)
	if err := validateInput(definition, secret, baseURL, options); err != nil {
		return Probed{}, err
	}
	if err := assertSlotPlan(definition, options); err != nil {
		return Probed{}, err
	}
	restricted := s.PublicOnly(ctx, userID, actor)
	if err := s.assertDestination(ctx, baseURL, restricted); err != nil {
		return Probed{}, err
	}
	adapter, err := s.Registry.CreateAdapter(definition, AdapterDeps{
		HTTP:            s.HTTP,
		Account:         Account{UserID: userID, DefinitionID: definition.ID, BaseURL: baseURL, Secret: secret, Options: options, PublicOnly: restricted},
		UpstreamTimeout: s.UpstreamTimeout,
		Log:             s.Log,
	})
	if err != nil {
		return Probed{}, err
	}
	validation, err := adapter.Validate(ctx)
	if err != nil {
		return Probed{}, err
	}
	models := []ProbedModel{}
	listed, listErr := adapter.ListModels(ctx)
	var modelsError *string
	if listErr != nil {
		message := listErr.Error()
		modelsError = &message
	}
	for _, model := range listed {
		models = append(models, ProbedModel{ID: model.ID, Name: model.Name, Kind: model.Kind, FreeHint: model.FreeHint, ContextWindow: model.ContextWindow})
	}
	return Probed{Validation: validation, Models: models, ModelsError: modelsError}, nil
}

// Update changes a connection. The key is checked again whenever anything that
// decides where it is sent changed.
func (s *Service) Update(ctx context.Context, userID, accountID string, in Input, actor *Actor) (PublicAccount, error) {
	row, err := s.GetRow(ctx, userID, accountID)
	if err != nil {
		return PublicAccount{}, err
	}
	definition, err := s.definitionOrThrow(row.DefinitionID)
	if err != nil {
		return PublicAccount{}, err
	}
	runtime, err := s.runtimeOf(ctx, row)
	if err != nil {
		return PublicAccount{}, err
	}
	baseURL := runtime.BaseURL
	if definition.EditableBaseURL && in.BaseURL.Set {
		baseURL = stringField(in.BaseURL)
	}
	secret := runtime.Secret
	if in.Secret.Set {
		secret = stringField(in.Secret)
	}
	// Options are replaced as a whole so cleared fields disappear; headers are
	// kept unless the caller sends them too.
	options := cloneOptions(runtime.Options)
	if in.Options.Set {
		options = map[string]any{}
		for name, value := range valueOr(in.Options.Value, map[string]any{}) {
			options[name] = value
		}
		if headers, ok := runtime.Options["headers"]; ok {
			options["headers"] = headers
		}
	}
	if in.Headers.Set {
		options["headers"] = headerMap(in.Headers)
	}
	// Members never see the stored key, so they may not send it somewhere new:
	// moving an account to another base URL needs the key typed again.
	if actor != nil && !actor.IsOwner && runtime.Secret != "" && !in.Secret.Set && baseURL != runtime.BaseURL {
		return PublicAccount{}, httpserver.New(http.StatusBadRequest, "SECRET_REQUIRED",
			"Enter the API key again to change where this provider is reached.")
	}
	if err := assertSlotPlan(definition, options); err != nil {
		return PublicAccount{}, err
	}
	// Private access goes with the destination: a new base URL may reach a
	// private network only when whoever sets it may (a member who moves an
	// admin's connection makes it public-only).
	privateAllowed := row.PrivateAllowed
	if baseURL != runtime.BaseURL {
		privateAllowed = s.mayReachPrivate(ctx, userID, actor)
	}
	restricted := s.publicOnlyFor(ctx, userID, privateAllowed)
	credentialsChanged := in.Secret.Set || in.BaseURL.Set || in.Options.Set
	if credentialsChanged {
		if err := validateInput(definition, secret, baseURL, options); err != nil {
			return PublicAccount{}, err
		}
		if err := s.assertDestination(ctx, baseURL, restricted); err != nil {
			return PublicAccount{}, err
		}
		adapter, err := s.Registry.CreateAdapter(definition, AdapterDeps{
			HTTP:            s.HTTP,
			Account:         Account{ID: row.ID, UserID: userID, DefinitionID: definition.ID, BaseURL: baseURL, Secret: secret, Options: options, PublicOnly: restricted},
			UpstreamTimeout: s.UpstreamTimeout,
			Log:             s.Log,
		})
		if err != nil {
			return PublicAccount{}, err
		}
		result, err := adapter.Validate(ctx)
		if err != nil {
			return PublicAccount{}, err
		}
		if result.Status == ValidationInvalid {
			return PublicAccount{}, httpserver.New(http.StatusUnprocessableEntity, "KEY_REJECTED",
				fmt.Sprintf("%s rejected this key: %s", definition.Label, stringValue(result.Message)))
		}
	}
	if mode, set := billingModeOf(in.BillingMode); set && mode == BillingFreeTier {
		free, err := s.offersFree(ctx, definition, row)
		if err != nil {
			return PublicAccount{}, err
		}
		if !free {
			return PublicAccount{}, httpserver.New(http.StatusBadRequest, "NO_FREE_TIER",
				fmt.Sprintf("%s has no free models.", definition.Label))
		}
	}
	if in.Slug.Set {
		slug := slugify(stringField(in.Slug))
		if slug != row.Slug {
			taken, err := s.count(ctx, `SELECT COUNT(*) FROM provider_accounts WHERE user_id = ? AND slug = ? AND id != ?`, userID, slug, accountID)
			if err != nil {
				return PublicAccount{}, err
			}
			if taken > 0 {
				return PublicAccount{}, httpserver.New(http.StatusConflict, "SLUG_TAKEN",
					"Another provider already uses this name in model ids.")
			}
			if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET slug = ? WHERE id = ?`, slug, accountID); err != nil {
				return PublicAccount{}, fmt.Errorf("providers: set slug: %w", err)
			}
		}
	}
	label := row.Label
	if in.Label.Set {
		label = stringField(in.Label)
	}
	billingMode := row.BillingMode
	prepaid := row.Prepaid
	modeChanged := false
	if mode, set := billingModeOf(in.BillingMode); set {
		stored, marker := StoredBilling(mode)
		modeChanged = string(stored) != row.BillingMode
		billingMode, prepaid = string(stored), marker
	}
	enabled := row.Enabled
	if in.Enabled.Set {
		enabled = in.Enabled.Value != nil && *in.Enabled.Value
	}
	sealed, err := s.seal(secret)
	if err != nil {
		return PublicAccount{}, err
	}
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return PublicAccount{}, fmt.Errorf("providers: encode options: %w", err)
	}
	resetFailures := credentialsChanged || (in.Enabled.Set && enabled)
	resetBilling := credentialsChanged || modeChanged
	resetStatus := credentialsChanged || (in.Enabled.Set && enabled && row.Status == "disabled")
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET label = ?, base_url = ?, secret_encrypted = ?, options_json = ?, billing_mode = ?,
		enabled = ?, private_allowed = ?, auth_failures = CASE WHEN ? THEN 0 ELSE auth_failures END,
		billing_state = CASE WHEN ? THEN 'unknown' ELSE billing_state END,
		status = CASE WHEN ? THEN 'unknown' ELSE status END WHERE id = ?`,
		label, nullableEmpty(baseURL), sealed, string(optionsJSON), billingMode,
		boolInt(enabled), boolInt(privateAllowed), boolInt(resetFailures), boolInt(resetBilling), boolInt(resetStatus), accountID); err != nil {
		return PublicAccount{}, fmt.Errorf("providers: update account: %w", err)
	}
	if in.BillingMode.Set {
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET prepaid = ? WHERE id = ?`, prepaid, accountID); err != nil {
			return PublicAccount{}, fmt.Errorf("providers: set prepaid: %w", err)
		}
	}
	if in.Classification.Set {
		var value any
		if text := stringField(in.Classification); text != "" && text != "auto" {
			value = text
		}
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET classification = ? WHERE id = ?`, value, accountID); err != nil {
			return PublicAccount{}, fmt.Errorf("providers: set classification: %w", err)
		}
	}
	if err := s.ConfigureTraffic(ctx, accountID, in); err != nil {
		return PublicAccount{}, err
	}
	return s.Get(ctx, userID, accountID)
}

// Remove deletes a connection and everything that cascades from it.
func (s *Service) Remove(ctx context.Context, userID, accountID string) error {
	if _, err := s.GetRow(ctx, userID, accountID); err != nil {
		return err
	}
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM provider_accounts WHERE id = ?`, accountID); err != nil {
		return fmt.Errorf("providers: delete account: %w", err)
	}
	return nil
}

// ConfigureTraffic applies the traffic part of a create or update: the limits
// the person stated, the overflow policy and the slot wait.
func (s *Service) ConfigureTraffic(ctx context.Context, accountID string, in Input) error {
	// The concurrent shorthand is spread first, so a concurrent limit in the
	// limits object wins over it.
	manual := map[string]*float64{}
	if in.Concurrent.Set {
		manual["concurrent"] = in.Concurrent.Value
	}
	for metric, value := range in.Limits {
		manual[metric] = value
	}
	if len(manual) > 0 && s.Limits != nil {
		if _, err := s.Limits.Set(accountID, manual, s.now()); err != nil {
			return fmt.Errorf("providers: set account limits: %w", err)
		}
	}
	if in.OverflowPolicy.Set {
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET overflow_policy = ? WHERE id = ?`,
			stringField(in.OverflowPolicy), accountID); err != nil {
			return fmt.Errorf("providers: set overflow policy: %w", err)
		}
	}
	if in.SlotWaitMS.Set {
		var value any
		if in.SlotWaitMS.Value != nil {
			value = int64(*in.SlotWaitMS.Value)
		}
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET slot_wait_ms = ? WHERE id = ?`, value, accountID); err != nil {
			return fmt.Errorf("providers: set slot wait: %w", err)
		}
	}
	if s.SlotWatch != nil {
		s.SlotWatch(accountID)
	}
	return nil
}

// SetLimits stores the limits a person states for a connection; a nil value
// clears one, an absent metric stays as it is.
func (s *Service) SetLimits(ctx context.Context, accountID string, patch map[string]*float64) error {
	if s.Limits == nil {
		return errors.New("providers: no account limits store is wired")
	}
	if _, err := s.Limits.Set(accountID, patch, s.now()); err != nil {
		return fmt.Errorf("providers: set account limits: %w", err)
	}
	return nil
}

// PublicOnly reports whether a destination set now, by actor in ownerID's
// space, is limited to public hosts. Calls on a stored connection use the
// decision stored with it instead (publicOnlyFor with Row.PrivateAllowed).
func (s *Service) PublicOnly(ctx context.Context, ownerID string, actor *Actor) bool {
	return s.publicOnlyFor(ctx, ownerID, s.mayReachPrivate(ctx, ownerID, actor))
}

// mayReachPrivate reports whether whoever sets a destination now may point it
// at a local or private address: only the instance admin, in their own space.
// A member of the admin's team may not, whatever the space. A nil actor is a
// trusted local caller (the CLI, a pool link) acting for the owner.
func (s *Service) mayReachPrivate(ctx context.Context, ownerID string, actor *Actor) bool {
	if actor != nil && (actor.Role != "admin" || !actor.IsOwner) {
		return false
	}
	return s.IsAdmin(ctx, ownerID)
}

// publicOnlyFor is the address rule of a connection whose destination was set
// with privateAllowed: private addresses stay for the instance admin's own
// space, so an owner who is no longer admin loses them too.
func (s *Service) publicOnlyFor(ctx context.Context, ownerID string, privateAllowed bool) bool {
	if s.config().AllowPrivateProviderURLs {
		return false
	}
	return !privateAllowed || !s.IsAdmin(ctx, ownerID)
}

// IsAdmin reports whether an account is an administrator of this instance.
func (s *Service) IsAdmin(ctx context.Context, userID string) bool {
	if userID == "" {
		return false
	}
	var role sql.NullString
	if err := s.DB.R.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, userID).Scan(&role); err != nil {
		return false
	}
	return role.String == "admin"
}

// assertDestination refuses a local or private base URL for a connection that
// is limited to public hosts.
func (s *Service) assertDestination(ctx context.Context, baseURL string, restricted bool) error {
	if !restricted || baseURL == "" {
		return nil
	}
	if IsPrivateDestination(ctx, baseURL, nil) {
		return httpserver.New(http.StatusBadRequest, "PRIVATE_ADDRESS",
			"This address is on a local or private network. Only the instance admin can use those.")
	}
	return nil
}

// seal stores a secret, or NULL when there is none.
func (s *Service) seal(secret string) (any, error) {
	if secret == "" {
		return nil, nil
	}
	sealed, err := s.Secrets.Seal(secret)
	if err != nil {
		return nil, err
	}
	return sealed, nil
}

// offersFree reports whether a connection can be used for free at all: the
// provider's own rules, or free models in this account's synced listing.
func (s *Service) offersFree(ctx context.Context, definition Definition, row Row) (bool, error) {
	if s.declaresFreeModels(definition.Billing) {
		return true, nil
	}
	count, err := s.count(ctx, `SELECT COUNT(*) FROM models WHERE account_id = ? AND is_free = 1`, row.ID)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// declaresFreeModels reports whether the provider is known to have free models:
// a free tier, or models that are always free (":free"). An account can also
// turn out to have free models in its own listing, see offersFree.
func (s *Service) declaresFreeModels(billing Billing) bool {
	return billing.FreeTierAll || len(billing.FreeTierModels) > 0 || len(billing.AlwaysFree) > 0
}

// uniqueSlug makes a slug no other connection of this person uses.
func (s *Service) uniqueSlug(ctx context.Context, userID, base string) (string, error) {
	slug := slugify(base)
	for index := 2; ; index++ {
		count, err := s.count(ctx, `SELECT COUNT(*) FROM provider_accounts WHERE user_id = ? AND slug = ?`, userID, slug)
		if err != nil {
			return "", err
		}
		if count == 0 {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", slugify(base), index)
	}
}

func (s *Service) count(ctx context.Context, query string, args ...any) (int, error) {
	var value int
	if err := s.DB.R.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, fmt.Errorf("providers: count: %w", err)
	}
	return value, nil
}

// queryRow reads one account row.
func (s *Service) queryRow(ctx context.Context, query string, args ...any) (Row, error) {
	return scanRow(s.DB.R.QueryRowContext(ctx, query, args...).Scan)
}

// queryRows reads many account rows.
func (s *Service) queryRows(ctx context.Context, query string, args ...any) ([]Row, error) {
	rows, err := s.DB.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("providers: read accounts: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		row, err := scanRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("providers: read accounts: %w", err)
	}
	return out, nil
}

// validateInput checks the facts every connection must have before its key is
// checked anywhere.
func validateInput(definition Definition, secret, baseURL string, options map[string]any) error {
	if !definition.KeyOptional && secret == "" {
		return httpserver.New(http.StatusBadRequest, "KEY_REQUIRED", definition.Label+" requires an API key.")
	}
	if definition.EditableBaseURL && baseURL == "" && definition.BaseURL == "" {
		return httpserver.New(http.StatusBadRequest, "BASE_URL_REQUIRED", "A base URL is required for this provider.")
	}
	if baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Scheme == "" || (isHTTPScheme(parsed.Scheme) && parsed.Host == "") {
			return httpserver.New(http.StatusBadRequest, "INVALID_BASE_URL", "The base URL is not a valid URL.")
		}
		schemes := definition.BaseURLSchemes
		if len(schemes) == 0 {
			schemes = []string{"http", "https"}
		}
		allowed := false
		for _, scheme := range schemes {
			if strings.EqualFold(scheme, parsed.Scheme) {
				allowed = true
				break
			}
		}
		if !allowed {
			return httpserver.New(http.StatusBadRequest, "INVALID_BASE_URL",
				"The base URL must use "+strings.Join(schemes, " or ")+".")
		}
	}
	// A full URL here would send the key to another host than the base URL.
	if path, ok := options["modelsPath"].(string); ok && path != "" && !modelsPathOnBase(path) {
		return httpserver.New(http.StatusBadRequest, "INVALID_MODELS_PATH",
			"The models path must start with / and stay on the base URL.")
	}
	for _, field := range definition.Fields {
		value, _ := options[field.Key].(string)
		if field.Required && strings.TrimSpace(value) == "" {
			return httpserver.New(http.StatusBadRequest, "FIELD_REQUIRED", field.Label+" is required.")
		}
	}
	return nil
}

// billingModeOf reads an optional billing mode.
func billingModeOf(field Optional[string]) (BillingMode, bool) {
	if field.Value == nil || *field.Value == "" {
		return "", false
	}
	return BillingMode(*field.Value), true
}
