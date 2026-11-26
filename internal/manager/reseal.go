package manager

// Re-sealing the values the Node.js server encrypted.
//
// server/utils/crypto.js encryptString wrote raw-key AES-256-GCM in base64url
// with no prefix; this binary seals with neolabs-go/secretbox under "enc:v1:"
// (internal/providers, internal/proxies and internal/auth all read both). The
// migration decrypts every legacy value with the install's secret.key and writes
// it back sealed, once, before the server starts.
//
// The columns are the ones the Node server called encryptString/decryptString
// on; see the comments for the module each one belongs to.

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// legacyColumn is one place the Node server stored an encrypted value.
type legacyColumn struct {
	// table, keys and column name the row and the value.
	table  string
	keys   []string
	column string
	// jsonString marks a value stored as a JSON string ("<ciphertext>"), the
	// shape settings_service.setGlobal and setUserSecret write.
	jsonString bool
	// only is an extra condition selecting the rows that hold a secret.
	only string
	// quiet skips values that do not decrypt without saying so: a user setting
	// that merely looks like ciphertext is not a secret.
	quiet bool
}

// legacyColumns is every encrypted column of the Node-era schema.
var legacyColumns = []legacyColumn{
	// providers/account_service.js, providers/codex_oauth.js, providers/nous_oauth.js.
	{table: "provider_accounts", keys: []string{"id"}, column: "secret_encrypted", only: "secret_encrypted IS NOT NULL"},
	// auth/two_factor_service.js: the confirmed and the pending TOTP secret.
	{table: "user_two_factor", keys: []string{"user_id"}, column: "secret_encrypted", only: "secret_encrypted IS NOT NULL"},
	{table: "user_two_factor", keys: []string{"user_id"}, column: "pending_secret_encrypted", only: "pending_secret_encrypted IS NOT NULL"},
	// pools/identity.js: the member's signing key and its box key.
	{table: "pool_identities", keys: []string{"user_id"}, column: "private_key_encrypted", only: "private_key_encrypted IS NOT NULL"},
	{table: "pool_identities", keys: []string{"user_id"}, column: "box_private_key_encrypted", only: "box_private_key_encrypted IS NOT NULL"},
	// proxies/pool.js: the SOCKS URL of a discovered proxy.
	{table: "outbound_proxies", keys: []string{"id"}, column: "secret_encrypted", only: "secret_encrypted IS NOT NULL"},
	// catalog/metadata_service.js: the admin's Artificial Analysis key.
	{table: "settings", keys: []string{"key"}, column: "value", jsonString: true,
		only: "key = 'artificial_analysis_key' AND value IS NOT NULL"},
	// settings/settings_service.js getUserSecret/setUserSecret.
	{table: "user_settings", keys: []string{"user_id", "key"}, column: "value", jsonString: true,
		only: "value IS NOT NULL", quiet: true},
}

// resealLegacySecrets re-seals every legacy value in the install's database and
// returns how many it rewrote. A value that does not decrypt is left alone: the
// server still reads it, and the next run tries again.
func (m *Manager) resealLegacySecrets(ctx context.Context, cfg config.Config) (int, error) {
	key, err := providers.EnsureSecretKey(cfg.Paths.SecretKey)
	if err != nil {
		return 0, err
	}
	box, err := secretbox.New(providers.SealedPrefix, hex.EncodeToString(key))
	if err != nil {
		return 0, fmt.Errorf("manager: build sealer: %w", err)
	}
	db, err := m.openDB(ctx, cfg)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	total, skipped := 0, 0
	for _, column := range legacyColumns {
		resealed, missed, err := resealColumn(ctx, db, box, key, column)
		if err != nil {
			return total, err
		}
		total += resealed
		if !column.quiet {
			skipped += missed
		}
	}
	if skipped > 0 {
		m.UI.Warn(fmt.Sprintf("%d encrypted value(s) could not be opened with this install's secret.key and were left as they were", skipped))
	}
	return total, nil
}

// resealColumn rewrites the legacy values of one column inside one transaction
// and returns how many it rewrote and how many it could not open.
func resealColumn(ctx context.Context, db *dbx.DB, box *secretbox.Box, key []byte, spec legacyColumn) (int, int, error) {
	query := fmt.Sprintf(`SELECT %s, %s FROM %s WHERE %s`,
		strings.Join(spec.keys, ", "), spec.column, spec.table, spec.only)
	rows, err := db.R.QueryContext(ctx, query)
	if err != nil {
		return 0, 0, fmt.Errorf("manager: read %s.%s: %w", spec.table, spec.column, err)
	}
	type pending struct {
		keys  []string
		old   string
		plain string
	}
	var updates []pending
	skipped := 0
	for rows.Next() {
		values := make([]string, len(spec.keys))
		pointers := make([]any, len(spec.keys))
		for i := range values {
			pointers[i] = &values[i]
		}
		var value sql.NullString
		if err := rows.Scan(append(pointers, &value)...); err != nil {
			rows.Close()
			return 0, skipped, fmt.Errorf("manager: read %s.%s: %w", spec.table, spec.column, err)
		}
		encoded := value.String
		if spec.jsonString {
			var inner string
			if json.Unmarshal([]byte(encoded), &inner) != nil {
				continue
			}
			encoded = inner
		}
		if encoded == "" || strings.HasPrefix(encoded, providers.SealedPrefix) {
			continue
		}
		plain, oerr := providers.OpenLegacy(encoded, key)
		if oerr != nil {
			skipped++
			continue
		}
		updates = append(updates, pending{keys: values, old: value.String, plain: plain})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, skipped, fmt.Errorf("manager: read %s.%s: %w", spec.table, spec.column, err)
	}
	rows.Close()
	if len(updates) == 0 {
		return 0, skipped, nil
	}

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, skipped, fmt.Errorf("manager: begin re-seal: %w", err)
	}
	conditions := make([]string, 0, len(spec.keys)+1)
	for _, name := range spec.keys {
		conditions = append(conditions, name+" = ?")
	}
	conditions = append(conditions, spec.column+" = ?")
	statement := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s`,
		spec.table, spec.column, strings.Join(conditions, " AND "))
	resealed := 0
	for _, update := range updates {
		sealed, serr := box.Seal(update.plain)
		if serr != nil {
			tx.Rollback()
			return resealed, skipped, fmt.Errorf("manager: seal %s.%s: %w", spec.table, spec.column, serr)
		}
		stored := sealed
		if spec.jsonString {
			encoded, jerr := json.Marshal(sealed)
			if jerr != nil {
				tx.Rollback()
				return resealed, skipped, fmt.Errorf("manager: encode %s.%s: %w", spec.table, spec.column, jerr)
			}
			stored = string(encoded)
		}
		args := make([]any, 0, len(update.keys)+2)
		args = append(args, stored)
		for _, value := range update.keys {
			args = append(args, value)
		}
		args = append(args, update.old)
		result, uerr := tx.ExecContext(ctx, statement, args...)
		if uerr != nil {
			tx.Rollback()
			return resealed, skipped, fmt.Errorf("manager: re-seal %s.%s: %w", spec.table, spec.column, uerr)
		}
		if changed, _ := result.RowsAffected(); changed > 0 {
			resealed++
		}
	}
	if err := tx.Commit(); err != nil {
		return resealed, skipped, fmt.Errorf("manager: commit re-seal of %s.%s: %w", spec.table, spec.column, err)
	}
	return resealed, skipped, nil
}
