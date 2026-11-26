package manager

// The account commands an admin needs when the app is not answering:
// reset-password, reset-2fa, and importing provider keys from a file
// (lib/manager.js resetPassword, resetTwoFactor, importProviders).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// minPasswordLength is what `reset-password` requires, the same minimum the
// sign-up form enforces.
const minPasswordLength = 12

// cmdResetPassword sets a new password and signs the account out everywhere.
func (m *Manager) cmdResetPassword(ctx context.Context, args []string) error {
	account, password := "", ""
	if len(args) > 0 {
		account = args[0]
	}
	if len(args) > 1 {
		password = args[1]
	}
	if account == "" || len(password) < minPasswordLength {
		return inputError("Usage: neorouter reset-password <username-or-email> <new-password-of-at-least-12-characters>")
	}
	hash, err := passhash.Hash(password, passhash.DefaultCost)
	if err != nil {
		return err
	}
	return m.withDB(ctx, func(ctx context.Context, db *dbx.DB, _ config.Config) error {
		id, username, err := userByName(ctx, db, account)
		if err != nil {
			return err
		}
		tx, err := db.W.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, hash, id); err != nil {
			tx.Rollback()
			return fmt.Errorf("manager: set password: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE user_sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id=? AND revoked_at IS NULL`,
			id); err != nil {
			tx.Rollback()
			return fmt.Errorf("manager: revoke sessions: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("manager: commit password reset: %w", err)
		}
		m.UI.OK("Password reset and sessions revoked for " + username + ".")
		return nil
	})
}

// cmdReset2FA removes the authenticator and the recovery codes of an account.
func (m *Manager) cmdReset2FA(ctx context.Context, args []string) error {
	account := ""
	if len(args) > 0 {
		account = args[0]
	}
	if account == "" {
		return inputError("Usage: neorouter reset-2fa <username-or-email>")
	}
	return m.withDB(ctx, func(ctx context.Context, db *dbx.DB, _ config.Config) error {
		id, username, err := userByName(ctx, db, account)
		if err != nil {
			return err
		}
		tx, err := db.W.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, statement := range []string{
			`DELETE FROM user_two_factor WHERE user_id=?`,
			`DELETE FROM user_recovery_codes WHERE user_id=?`,
		} {
			if _, err := tx.ExecContext(ctx, statement, id); err != nil {
				tx.Rollback()
				return fmt.Errorf("manager: remove two-factor: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("manager: commit two-factor removal: %w", err)
		}
		m.UI.OK("Two-factor authentication removed for " + username + ".")
		return nil
	})
}

// cmdProviders is `neorouter providers import <file.json> --user <account>`.
func (m *Manager) cmdProviders(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "import" {
		return inputError("Usage: neorouter providers import <file.json> --user <username-or-email>")
	}
	return m.importProviders(ctx, args[1:])
}

// importEntry is one account of an import file.
type importEntry struct {
	DefinitionID string         `json:"definitionId"`
	Secret       string         `json:"secret"`
	BillingMode  string         `json:"billingMode,omitempty"`
	Label        string         `json:"label,omitempty"`
	BaseURL      string         `json:"baseUrl,omitempty"`
	Options      map[string]any `json:"options,omitempty"`
}

// importProviders adds the accounts of a JSON file, skipping the provider
// definitions the account already has so the import can be run again.
//
// The keys are stored sealed and the account is left for the server's own model
// sync, so importing never needs the network here.
func (m *Manager) importProviders(ctx context.Context, args []string) error {
	file := ""
	if positional := cli.Positionals(args, "--user"); len(positional) > 0 {
		file = positional[0]
	}
	account, _ := cli.FlagValue(args, "--user")
	if file == "" || account == "" {
		return inputError("Usage: neorouter providers import <file.json> --user <username-or-email>")
	}
	raw, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return err
	}
	var entries []importEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return inputError("The import file must contain a JSON array.")
	}
	registry, err := providers.Load(nil, m.Getenv)
	if err != nil {
		return err
	}
	sealer, err := providers.NewSecretKeySealer(m.installPaths().SecretKey)
	if err != nil {
		return err
	}
	return m.withDB(ctx, func(ctx context.Context, db *dbx.DB, cfg config.Config) error {
		id, username, err := userByName(ctx, db, account)
		if err != nil {
			return err
		}
		m.UI.Title("Adding providers for "+username, "")
		m.UI.Blank()
		added := 0
		for _, entry := range entries {
			exists, err := accountExists(ctx, db, id, entry.DefinitionID)
			if err != nil {
				return err
			}
			if exists {
				m.UI.Info(entry.DefinitionID + ": already connected, skipped")
				continue
			}
			if err := insertAccount(ctx, db, registry, sealer, id, entry); err != nil {
				m.UI.Warn(entry.DefinitionID + ": " + err.Error())
				continue
			}
			added++
			m.UI.OK(entry.DefinitionID + ": added (unvalidated)")
		}
		m.UI.Blank()
		m.UI.Info(fmt.Sprintf("%d of %d providers added.", added, len(entries)))
		m.UI.Blank()
		return nil
	})
}

// installPaths is the install's path block, without loading the whole config.
func (m *Manager) installPaths() config.Paths {
	return config.ResolvePaths(m.Getenv, m.UserHome)
}

func accountExists(ctx context.Context, db *dbx.DB, userID, definitionID string) (bool, error) {
	var one int
	err := db.R.QueryRowContext(ctx, `SELECT 1 FROM provider_accounts WHERE user_id=? AND definition_id=?`,
		userID, definitionID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("manager: check provider account: %w", err)
	}
	return true, nil
}

// insertAccount writes one account the way account_service.create does, minus
// the credential probe: without a network round trip the account starts as
// "unknown" and the server validates it on its next sync.
func insertAccount(ctx context.Context, db *dbx.DB, registry *providers.Registry, sealer *providers.Sealer, userID string, entry importEntry) error {
	definition, ok := registry.Get(entry.DefinitionID)
	if !ok {
		return errors.New("unknown provider definition")
	}
	baseURL := ""
	if definition.EditableBaseURL {
		baseURL = entry.BaseURL
		if baseURL == "" {
			baseURL = definition.BaseURL
		}
	}
	slug, err := uniqueSlug(ctx, db, userID, firstNonEmpty(entry.DefinitionID))
	if err != nil {
		return err
	}
	label := entry.Label
	if label == "" {
		siblings, err := countAccounts(ctx, db, userID, definition.ID)
		if err != nil {
			return err
		}
		label = definition.Label
		if siblings > 0 {
			label = fmt.Sprintf("%s %d", definition.Label, siblings+1)
		}
	}
	sealed := ""
	if entry.Secret != "" {
		sealed, err = sealer.Seal(entry.Secret)
		if err != nil {
			return err
		}
	}
	options := entry.Options
	if options == nil {
		options = map[string]any{}
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("encode options: %w", err)
	}
	billing, prepaid := storedBillingFor(definition, entry.BillingMode)
	_, err = db.W.ExecContext(ctx, `INSERT INTO provider_accounts
		(id, user_id, definition_id, slug, label, base_url, secret_encrypted, options_json, billing_mode, status, status_message, last_validated_at, created_by_user_id, prepaid)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'), ?, ?)`,
		cli.NewUUID(), userID, definition.ID, slug, label, nullable(baseURL), nullable(sealed), string(encoded),
		string(billing), "unknown", nil, userID, prepaid)
	if err != nil {
		return fmt.Errorf("store account: %w", err)
	}
	return nil
}

// countAccounts is how many accounts of one provider an account already has.
func countAccounts(ctx context.Context, db *dbx.DB, userID, definitionID string) (int, error) {
	var siblings int
	if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_accounts WHERE user_id=? AND definition_id=?`,
		userID, definitionID).Scan(&siblings); err != nil {
		return 0, fmt.Errorf("count accounts: %w", err)
	}
	return siblings, nil
}

// storedBillingFor is billing_modes.storedBilling over an import entry: the
// free tier is only a choice for a provider that has one.
func storedBillingFor(definition providers.Definition, requested string) (providers.BillingMode, int) {
	mode := definition.Billing.DefaultMode
	if requested != "" {
		candidate := providers.BillingMode(requested)
		for _, known := range providers.BillingModes {
			if known == candidate && (candidate != providers.BillingFreeTier || hasFreeModels(definition.Billing)) {
				mode = candidate
			}
		}
	}
	return providers.StoredBilling(mode)
}

// hasFreeModels reports whether a provider's free tier covers any model.
func hasFreeModels(billing providers.Billing) bool {
	return billing.FreeTierAll || len(billing.FreeTierModels) > 0 || billing.TrustListingFree
}

// slugNonAlnum is account_service.slugify's character class.
var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify is account_service.slugify: lowercase letters, digits and dashes, at
// most 40 of them.
func slugify(value string) string {
	lowered := strings.ToLower(value)
	replaced := slugNonAlnum.ReplaceAllString(lowered, "-")
	trimmed := strings.Trim(replaced, "-")
	if len(trimmed) > 40 {
		trimmed = trimmed[:40]
	}
	if trimmed == "" {
		return "provider"
	}
	return trimmed
}

// uniqueSlug is providers' uniqueSlug: the first free "base", "base-2", "base-3".
func uniqueSlug(ctx context.Context, db *dbx.DB, userID, base string) (string, error) {
	slug := slugify(base)
	for index := 2; ; index++ {
		var count int
		if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_accounts WHERE user_id=? AND slug=?`,
			userID, slug).Scan(&count); err != nil {
			return "", fmt.Errorf("count slugs: %w", err)
		}
		if count == 0 {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", slugify(base), index)
	}
}

// nullable stores "" as NULL, which is what the Node server wrote for an absent
// value.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// firstNonEmpty returns the first value that is not empty.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
