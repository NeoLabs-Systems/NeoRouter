package providers

// Billing: how a connection is paid for, and the checks that prove a provider's
// free tier really cannot charge money.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// Prepaid plan markers stored in provider_accounts.prepaid: every one of them
// is a paid subscription, even when its incremental request price is zero.
const (
	// PrepaidNone is billing_mode as stored: 'free_tier' or 'paid'.
	PrepaidNone = 0
	// PrepaidPlan is "plan": a subscription whose allowance is spent first.
	PrepaidPlan = 1
	// PrepaidPlanPaid is "plan_paid": the plan, then pay per use.
	PrepaidPlanPaid = 2
	// PrepaidFlatRate is "flat_rate": unlimited use, never billed per request.
	PrepaidFlatRate = 3
)

// prepaidMarkers maps a public billing mode to what is stored.
var prepaidMarkers = map[BillingMode]int{
	BillingPlan:     PrepaidPlan,
	BillingPlanPaid: PrepaidPlanPaid,
	BillingFlatRate: PrepaidFlatRate,
}

// StoredBilling is the billing_mode and prepaid marker a public billing mode
// is stored as.
func StoredBilling(mode BillingMode) (BillingMode, int) {
	if marker, ok := prepaidMarkers[mode]; ok {
		return BillingPaid, marker
	}
	return mode, PrepaidNone
}

// PublicBilling is the mode people chose, from the stored pair.
func PublicBillingMode(row Row) BillingMode {
	for mode, marker := range prepaidMarkers {
		if marker == row.Prepaid {
			return mode
		}
	}
	return BillingMode(row.BillingMode)
}

// freeTierError is the refusal a provider without a billing account gives for a
// model that has no free tier.
var freeTierError = regexp.MustCompile(`(?i)free[_ ]tier`)

// billingTimeout bounds one billing check.
const billingTimeout = 30 * time.Second

// setBillingState stores what a check concluded and reclassifies the account's
// models when the verdict changed.
func (s *Service) setBillingState(ctx context.Context, accountID, state string, message *string) error {
	var current string
	err := s.DB.W.QueryRowContext(ctx, `SELECT billing_state FROM provider_accounts WHERE id = ?`, accountID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("providers: read billing state: %w", err)
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET billing_state = ?, billing_message = ?,
		billing_checked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
		state, nullableString(message), accountID); err != nil {
		return fmt.Errorf("providers: set billing state: %w", err)
	}
	if current != state {
		if state == "paid" {
			s.log().Warn("Billing state changed", "accountId", accountID, "from", current, "to", state, "message", stringValue(message))
		} else {
			s.log().Info("Billing state changed", "accountId", accountID, "from", current, "to", state, "message", stringValue(message))
		}
		if s.Catalog != nil {
			if err := s.Catalog.ReclassifyAccount(ctx, accountID); err != nil {
				return err
			}
		}
	}
	return nil
}

// probeModel picks the model a billing check asks the provider about: the first
// listing entry matching one of the patterns, else the first chat model.
func (s *Service) probeModel(ctx context.Context, accountID string, patterns []string) (string, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT model_id FROM models WHERE account_id = ? AND available = 1 AND capability = 'chat'
		ORDER BY model_id`, accountID)
	if err != nil {
		return "", fmt.Errorf("providers: read probe models: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("providers: scan probe model: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("providers: read probe models: %w", err)
	}
	if len(patterns) == 0 {
		if len(ids) == 0 {
			return "", nil
		}
		return ids[0], nil
	}
	for _, pattern := range patterns {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if re.MatchString(id) {
				return id, nil
			}
		}
	}
	return "", nil
}

// judgeHeaders separates a free plan from a paid one by the daily request limit
// the provider publishes in the response headers.
func judgeHeaders(verify *Verify, header http.Header) *billingVerdict {
	raw := header.Get("x-ratelimit-limit-requests")
	limit, err := strconv.ParseFloat(raw, 64)
	if err != nil || limit <= 0 {
		return nil
	}
	above := limit > verify.MaxFreeRequestsPerDay
	message := fmt.Sprintf("Request limit %s/day matches the free plan.", formatNumber(limit))
	if above {
		message = fmt.Sprintf("Request limit %s/day is above the free plan (%s).", formatNumber(limit), formatNumber(verify.MaxFreeRequestsPerDay))
	}
	state := "free"
	if above {
		state = "paid"
	}
	return &billingVerdict{State: state, Message: message}
}

// billingVerdict is one check's conclusion.
type billingVerdict struct {
	State   string
	Message string
}

// messagePtr is the verdict's message, for the stored column.
func (v billingVerdict) messagePtr() *string {
	message := v.Message
	return &message
}

// formatNumber prints a number in its shortest form.
func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// VerifyAccount proves, for one account, that a provider's free tier really
// cannot charge money. Providers whose free tier ends silently once a card or
// billing account is attached declare billing.verify; until a check here
// returns "free", their free-tier models are not treated as free.
func (s *Service) VerifyAccount(ctx context.Context, accountID string) (*billingVerdict, error) {
	runtime, err := s.Runtime(ctx, accountID)
	if err != nil || runtime == nil {
		return nil, err
	}
	if runtime.BillingMode != BillingFreeTier {
		return nil, nil
	}
	definition, known := s.Registry.Get(runtime.DefinitionID)
	if !known || definition.Billing.Verify == nil {
		return nil, nil
	}
	verify := definition.Billing.Verify
	var verdict billingVerdict
	switch verify.Type {
	case VerifyRateLimitHeaders:
		verdict, err = s.checkRateLimitHeaders(ctx, accountID, runtime, definition, verify)
	case VerifyPaidModelProbe:
		verdict, err = s.checkPaidModelProbe(ctx, accountID, runtime, definition, verify)
	default:
		return nil, nil
	}
	if err != nil {
		s.log().Warn("Billing check failed", "accountId", accountID, "error", err)
		message := err.Error()
		return &billingVerdict{State: "unknown", Message: message}, nil
	}
	// An inconclusive check keeps a known state instead of flapping to unknown.
	var current sql.NullString
	if err := s.DB.W.QueryRowContext(ctx, `SELECT billing_state FROM provider_accounts WHERE id = ?`, accountID).Scan(&current); err != nil {
		return &verdict, nil
	}
	if verdict.State == "unknown" && current.String != "" && current.String != "unknown" {
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE provider_accounts SET billing_message = ? WHERE id = ?`,
			verdict.Message, accountID); err != nil {
			return &verdict, fmt.Errorf("providers: set billing message: %w", err)
		}
		return &billingVerdict{State: current.String, Message: verdict.Message}, nil
	}
	if err := s.setBillingState(ctx, accountID, verdict.State, verdict.messagePtr()); err != nil {
		return &verdict, err
	}
	return &verdict, nil
}

// checkRateLimitHeaders sends a one-token request and reads the daily request
// limit from the response headers.
func (s *Service) checkRateLimitHeaders(ctx context.Context, accountID string, runtime *Runtime, definition Definition, verify *Verify) (billingVerdict, error) {
	model, err := s.probeModel(ctx, accountID, verify.ProbeModels)
	if err != nil {
		return billingVerdict{}, err
	}
	if model == "" {
		return billingVerdict{State: "unknown", Message: "No model to check the plan with."}, nil
	}
	baseURL := firstNonEmpty(runtime.BaseURL, definition.BaseURL)
	result, err := s.billingPost(ctx, baseURL+"/chat/completions",
		http.Header{"Authorization": {"Bearer " + runtime.Secret}},
		map[string]any{"model": model, "messages": []any{map[string]any{"role": "user", "content": "."}}, "max_tokens": 1})
	if err != nil {
		return billingVerdict{}, err
	}
	if verdict := judgeHeaders(verify, result.Headers); verdict != nil {
		return *verdict, nil
	}
	return billingVerdict{State: "unknown", Message: fmt.Sprintf("No plan limits in the response (HTTP %d).", result.Status)}, nil
}

// checkPaidModelProbe sends a one-token request to a model without a free tier:
// a free project is refused with "free_tier … limit: 0", a billed one answers.
func (s *Service) checkPaidModelProbe(ctx context.Context, accountID string, runtime *Runtime, definition Definition, verify *Verify) (billingVerdict, error) {
	model, err := s.probeModel(ctx, accountID, verify.PaidOnlyModels)
	if err != nil {
		return billingVerdict{}, err
	}
	if model == "" {
		return billingVerdict{State: "unknown", Message: "No model without a free tier to check with."}, nil
	}
	baseURL := firstNonEmpty(runtime.BaseURL, definition.BaseURL)
	result, err := s.billingPost(ctx, baseURL+"/models/"+urlPath(model)+":generateContent",
		http.Header{"X-Goog-Api-Key": {runtime.Secret}},
		map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]any{"text": "."}}}}, "generationConfig": map[string]any{"maxOutputTokens": 1}})
	if err != nil {
		return billingVerdict{}, err
	}
	switch {
	case result.Status == http.StatusTooManyRequests && freeTierError.MatchString(result.Text):
		return billingVerdict{State: "free", Message: model + " is refused with a free-tier limit: no billing account."}, nil
	case result.Status >= 200 && result.Status < 300:
		return billingVerdict{State: "paid", Message: model + " answered although it has no free tier: billing is enabled."}, nil
	}
	return billingVerdict{State: "unknown", Message: fmt.Sprintf("Inconclusive answer from %s (HTTP %d).", model, result.Status)}, nil
}

// billingResult is one raw answer a billing check reads.
type billingResult struct {
	Status  int
	Headers http.Header
	Text    string
}

// billingPost sends one billing check request and returns the answer whatever
// its status, because a refusal is the answer the check is looking for.
func (s *Service) billingPost(ctx context.Context, rawURL string, headers http.Header, body any) (billingResult, error) {
	request := Request{Method: http.MethodPost, Headers: headers.Clone(), JSON: body, Timeout: billingTimeout}
	response, err := s.HTTP.Do(ctx, rawURL, request)
	if err == nil {
		defer response.Body.Close()
		text, readErr := readText(response)
		if readErr != nil {
			return billingResult{}, readErr
		}
		return billingResult{Status: response.StatusCode, Headers: response.Header, Text: text}, nil
	}
	// A refusal is classified by the HTTP layer; its status and headers are the
	// facts the check needs, so it is an answer rather than a failure.
	classified := AsProviderError(err)
	if classified == nil || classified.Status == 0 {
		return billingResult{}, err
	}
	text := ""
	if body, ok := classified.Body.(string); ok {
		text = body
	} else if classified.Body != nil {
		text = compactJSON(classified.Body)
	}
	return billingResult{Status: classified.Status, Headers: classified.Headers, Text: text}, nil
}

// ObserveHeaders is the passive check on every routed response of providers
// verified by headers.
func (s *Service) ObserveHeaders(ctx context.Context, entry Observable, header http.Header) {
	if header == nil || entry.BillingMode != BillingFreeTier {
		return
	}
	definition, known := s.Registry.Get(entry.DefinitionID)
	if !known || definition.Billing.Verify == nil || definition.Billing.Verify.Type != VerifyRateLimitHeaders {
		return
	}
	verdict := judgeHeaders(definition.Billing.Verify, header)
	if verdict == nil || verdict.State != "paid" {
		return
	}
	if err := s.setBillingState(ctx, entry.AccountID, "paid", verdict.messagePtr()); err != nil {
		s.log().Warn("Could not record the plan limit", "accountId", entry.AccountID, "error", err)
	}
}

// ObserveCost records that the provider charged for a model routed as free.
func (s *Service) ObserveCost(ctx context.Context, entry Observable, cost float64) {
	if !entry.IsFree || cost <= 0 || s.Catalog == nil {
		return
	}
	changed, err := s.Catalog.FlagModel(ctx, entry.Ref, map[string]any{"billedAt": s.now().UTC().Format("2006-01-02T15:04:05.000Z")})
	if err != nil {
		s.log().Warn("Could not record a billed free model", "model", entry.ID, "error", err)
		return
	}
	if changed {
		s.log().Warn("Provider charged a model classified as free; it is now treated as paid", "model", entry.ID, "cost", cost)
	}
}

// ObserveFailure records "free_tier … limit: 0": this model has no free quota on
// this account.
func (s *Service) ObserveFailure(ctx context.Context, entry Observable, err error) {
	if entry.BillingMode != BillingFreeTier || err == nil || s.Catalog == nil {
		return
	}
	classified := AsProviderError(err)
	if classified == nil || classified.Learned == nil || classified.Learned.Value != 0 {
		return
	}
	if !freeTierError.MatchString(classified.Message) {
		return
	}
	changed, flagErr := s.Catalog.FlagModel(ctx, entry.Ref, map[string]any{"noFreeTier": true})
	if flagErr != nil {
		s.log().Warn("Could not record a model without a free tier", "model", entry.ID, "error", flagErr)
		return
	}
	if changed {
		s.log().Info("Model has no free tier on this account", "model", entry.ID)
	}
}

// readText reads a response body as text.
func readText(response *http.Response) (string, error) {
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return "", FromNetwork(err)
	}
	return string(raw), nil
}

// urlPath escapes one path segment (encodeURIComponent).
func urlPath(value string) string { return url.PathEscape(value) }
