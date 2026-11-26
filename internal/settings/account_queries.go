package settings

// The read-only queries behind the account exports. Every SELECT fixes its
// column list and ORDER BY, which the export format depends on.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// userRowData is the account itself, as the export shows it.
type userRowData struct {
	id          string
	username    string
	email       string
	displayName *string
	role        string
	createdAt   string
	lastLoginAt *string
}

func (a *AccountData) userRow(ctx context.Context, userID string) (userRowData, error) {
	var user userRowData
	err := a.DB.R.QueryRowContext(ctx, `SELECT id, username, email, display_name, role, created_at, last_login_at
		FROM users WHERE id = ?`, userID).
		Scan(&user.id, &user.username, &user.email, &user.displayName, &user.role, &user.createdAt, &user.lastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return userRowData{}, httpserver.New(http.StatusNotFound, "USER_NOT_FOUND", "This account does not exist.")
	}
	if err != nil {
		return userRowData{}, fmt.Errorf("settings: read account: %w", err)
	}
	return user, nil
}

// listSettings is the person's user settings as one object, keys in name order
// (the order the row index returns them in).
func (a *AccountData) listSettings(ctx context.Context, userID string) (*orderedObject, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT key, value FROM user_settings WHERE user_id = ? ORDER BY key`, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: list account settings: %w", err)
	}
	defer rows.Close()
	out := newOrderedObject()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("settings: list account settings: %w", err)
		}
		out.set(key, jsonOrSelf(value))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list account settings: %w", err)
	}
	return out, nil
}

// providerAccount is one saved connection with the columns the two exports read.
type providerAccount struct {
	id              string
	definitionID    string
	label           string
	slug            string
	baseURL         *string
	secretEncrypted *string
	options         string
	billingMode     string
	prepaid         int64
	enabled         int64
	status          string
	createdAt       string
	classification  *string
	overflowPolicy  string
	slotWaitMS      *int64
}

// ownAccounts lists the connections this person added, in creation order,
// without the pool-partner connections NeoRouter makes for itself.
func (a *AccountData) ownAccounts(ctx context.Context, userID string) ([]providerAccount, error) {
	query := `SELECT id, definition_id, label, slug, base_url, secret_encrypted, options_json, billing_mode,
		prepaid, enabled, status, created_at, classification, overflow_policy, slot_wait_ms
		FROM provider_accounts WHERE user_id = ? AND definition_id NOT IN (`
	args := []any{userID}
	for i, definition := range internalDefinitions {
		if i > 0 {
			query += ", "
		}
		query += "?"
		args = append(args, definition)
	}
	query += `) ORDER BY created_at`
	rows, err := a.DB.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("settings: list account connections: %w", err)
	}
	defer rows.Close()
	out := []providerAccount{}
	for rows.Next() {
		var account providerAccount
		if err := rows.Scan(&account.id, &account.definitionID, &account.label, &account.slug, &account.baseURL,
			&account.secretEncrypted, &account.options, &account.billingMode, &account.prepaid, &account.enabled,
			&account.status, &account.createdAt, &account.classification, &account.overflowPolicy,
			&account.slotWaitMS); err != nil {
			return nil, fmt.Errorf("settings: list account connections: %w", err)
		}
		out = append(out, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list account connections: %w", err)
	}
	return out, nil
}

func (a *AccountData) listConnections(ctx context.Context, userID string) ([]ExportConnection, error) {
	accounts, err := a.ownAccounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []ExportConnection{}
	for _, account := range accounts {
		models, err := a.listModels(ctx, account.id)
		if err != nil {
			return nil, err
		}
		out = append(out, ExportConnection{
			ID:           account.id,
			Provider:     a.providerName(account.definitionID),
			DefinitionID: account.definitionID,
			Label:        account.label,
			Slug:         account.slug,
			BaseURL:      account.baseURL,
			Options:      jsonOr(&account.options, jsonObject),
			Billing:      account.billingMode,
			Prepaid:      account.prepaid != 0,
			Enabled:      account.enabled != 0,
			Status:       account.status,
			CreatedAt:    account.createdAt,
			Models:       models,
		})
	}
	return out, nil
}

func (a *AccountData) listModels(ctx context.Context, accountID string) ([]ExportModel, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT model_id, display_name, capability, enabled, is_free
		FROM models WHERE account_id = ? ORDER BY model_id`, accountID)
	if err != nil {
		return nil, fmt.Errorf("settings: list connection models: %w", err)
	}
	defer rows.Close()
	out := []ExportModel{}
	for rows.Next() {
		var (
			model   ExportModel
			enabled int64
			free    int64
		)
		if err := rows.Scan(&model.ID, &model.Name, &model.Capability, &enabled, &free); err != nil {
			return nil, fmt.Errorf("settings: list connection models: %w", err)
		}
		model.Enabled = enabled != 0
		model.Free = free != 0
		out = append(out, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list connection models: %w", err)
	}
	return out, nil
}

// concurrentLimit is quota/account_limits.get(id).concurrent, which is one row
// of account_limits; nil means the person stated none.
func (a *AccountData) concurrentLimit(ctx context.Context, accountID string) (*int64, error) {
	var value int64
	err := a.DB.R.QueryRowContext(ctx,
		`SELECT value FROM account_limits WHERE account_id = ? AND metric = 'concurrent'`, accountID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("settings: read connection concurrency: %w", err)
	}
	return &value, nil
}

func (a *AccountData) listRoutingProfiles(ctx context.Context, userID string) ([]ExportRoutingProfile, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT id, name, weights_json, constraints_json, created_at
		FROM routing_profiles WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: list routing profiles: %w", err)
	}
	defer rows.Close()
	out := []ExportRoutingProfile{}
	for rows.Next() {
		var (
			profile     ExportRoutingProfile
			weights     *string
			constraints *string
		)
		if err := rows.Scan(&profile.ID, &profile.Name, &weights, &constraints, &profile.CreatedAt); err != nil {
			return nil, fmt.Errorf("settings: list routing profiles: %w", err)
		}
		profile.Weights = jsonOr(weights, jsonObject)
		profile.Constraints = jsonOr(constraints, jsonObject)
		out = append(out, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list routing profiles: %w", err)
	}
	return out, nil
}

func (a *AccountData) listRouterKeys(ctx context.Context, userID string) ([]ExportRouterKey, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT id, name, prefix, mode, budget_usd, budget_period, rpm_limit,
		concurrent_limit, allowed_models_json, profile, routing_group, expires_at, revoked_at, last_used_at,
		created_at, app_name, token_limit, token_period, priority
		FROM router_keys WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: list router keys: %w", err)
	}
	defer rows.Close()
	out := []ExportRouterKey{}
	for rows.Next() {
		var (
			key           ExportRouterKey
			allowedModels string
		)
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &key.Mode, &key.BudgetUSD, &key.BudgetPeriod,
			&key.RPMLimit, &key.Concurrent, &allowedModels, &key.Profile, &key.RoutingGroup, &key.ExpiresAt,
			&key.RevokedAt, &key.LastUsedAt, &key.CreatedAt, &key.AppName, &key.TokenLimit, &key.TokenPeriod,
			&key.Priority); err != nil {
			return nil, fmt.Errorf("settings: list router keys: %w", err)
		}
		key.AllowedModels = jsonOr(&allowedModels, jsonNull)
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list router keys: %w", err)
	}
	return out, nil
}

func (a *AccountData) team(ctx context.Context, userID string) (ExportTeam, error) {
	out := ExportTeam{Members: []ExportTeamMember{}}
	var memberOf ExportTeamMemberOf
	err := a.DB.R.QueryRowContext(ctx, `SELECT owner_user_id, created_at FROM team_members WHERE member_user_id = ?`, userID).
		Scan(&memberOf.OwnerUserID, &memberOf.CreatedAt)
	switch {
	case err == nil:
		out.MemberOf = &memberOf
	case !errors.Is(err, sql.ErrNoRows):
		return out, fmt.Errorf("settings: read team membership: %w", err)
	}
	rows, err := a.DB.R.QueryContext(ctx, `SELECT member_user_id, created_at FROM team_members WHERE owner_user_id = ?`, userID)
	if err != nil {
		return out, fmt.Errorf("settings: read team members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member ExportTeamMember
		if err := rows.Scan(&member.MemberUserID, &member.CreatedAt); err != nil {
			return out, fmt.Errorf("settings: read team members: %w", err)
		}
		out.Members = append(out.Members, member)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("settings: read team members: %w", err)
	}
	return out, nil
}

func (a *AccountData) listPools(ctx context.Context, userID string) ([]ExportPool, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT pool_id, name, is_host, self_name, share_json, paused, joined_at
		FROM pools WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: list pools: %w", err)
	}
	defer rows.Close()
	out := []ExportPool{}
	for rows.Next() {
		var (
			pool         ExportPool
			host, paused int64
			share        string
		)
		if err := rows.Scan(&pool.ID, &pool.Name, &host, &pool.MyName, &share, &paused, &pool.JoinedAt); err != nil {
			return nil, fmt.Errorf("settings: list pools: %w", err)
		}
		pool.Host = host != 0
		pool.Paused = paused != 0
		pool.Share = jsonOr(&share, jsonObject)
		out = append(out, pool)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list pools: %w", err)
	}
	return out, nil
}

func (a *AccountData) listRequests(ctx context.Context, userID string) ([]ExportRequest, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT id, created_at, capability, task, requested_model, model_label,
		status, http_status, error_code, input_tokens, output_tokens, units, routing_group, cached_input_tokens,
		cache_creation_tokens, cost_usd, saved_usd, is_free, latency_ms, stream
		FROM requests WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: list requests: %w", err)
	}
	defer rows.Close()
	out := []ExportRequest{}
	for rows.Next() {
		var request ExportRequest
		if err := rows.Scan(&request.ID, &request.CreatedAt, &request.Capability, &request.Task,
			&request.RequestedModel, &request.ModelLabel, &request.Status, &request.HTTPStatus,
			&request.ErrorCode, &request.InputTokens, &request.OutputTokens, &request.Units,
			&request.RoutingGroup, &request.CachedInputTokens, &request.CacheCreationTokens,
			&request.CostUSD, &request.SavedUSD, &request.IsFree, &request.LatencyMS,
			&request.Stream); err != nil {
			return nil, fmt.Errorf("settings: list requests: %w", err)
		}
		out = append(out, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list requests: %w", err)
	}
	return out, nil
}

func (a *AccountData) listMediaJobs(ctx context.Context, userID string) ([]ExportMediaJob, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT id, capability, model_label, status, request_json, result_json, created_at
		FROM media_jobs WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: list media jobs: %w", err)
	}
	defer rows.Close()
	out := []ExportMediaJob{}
	for rows.Next() {
		var (
			job     ExportMediaJob
			request *string
			result  *string
		)
		if err := rows.Scan(&job.ID, &job.Capability, &job.ModelLabel, &job.Status, &request, &result,
			&job.CreatedAt); err != nil {
			return nil, fmt.Errorf("settings: list media jobs: %w", err)
		}
		job.Request = jsonOr(request, jsonNull)
		job.Result = jsonOr(result, jsonNull)
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list media jobs: %w", err)
	}
	return out, nil
}

// poolIDs lists the pools this person joined, so DeleteAccount can leave them.
func (a *AccountData) poolIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT pool_id FROM pools WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: list account pools: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var poolID string
		if err := rows.Scan(&poolID); err != nil {
			return nil, fmt.Errorf("settings: list account pools: %w", err)
		}
		out = append(out, poolID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: list account pools: %w", err)
	}
	return out, nil
}
