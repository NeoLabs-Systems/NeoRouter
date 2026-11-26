package manager

// Re-sealing the values the Node.js server encrypted: raw-key AES-256-GCM with
// no prefix becomes the "enc:v1:" format internal/secrets writes (and both read).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
	"github.com/NeoLabs-Systems/neorouter/internal/secrets"
)

// legacyColumn is one place the Node server stored an encrypted value.
type legacyColumn struct {
	// table, keys and column name the row and the value.
	table  string
	keys   []string
	column string
	// jsonString marks a value stored as a JSON string ("<ciphertext>"), the
	// shape settings are written in.
	jsonString bool
	// only is an extra condition selecting the rows that hold a secret.
	only string
	// quiet skips values that do not decrypt without saying so: a user setting
	// that merely looks like ciphertext is not a secret.
	quiet bool
}

// legacyColumns is every encrypted column of the Node-era schema.
var legacyColumns = []legacyColumn{
	// Provider keys and sign-in tokens.
	{table: "provider_accounts", keys: []string{"id"}, column: "secret_encrypted", only: "secret_encrypted IS NOT NULL"},
	// The confirmed and the pending TOTP secret.
	{table: "user_two_factor", keys: []string{"user_id"}, column: "secret_encrypted", only: "secret_encrypted IS NOT NULL"},
	{table: "user_two_factor", keys: []string{"user_id"}, column: "pending_secret_encrypted", only: "pending_secret_encrypted IS NOT NULL"},
	// A pool member's signing key and box key.
	{table: "pool_identities", keys: []string{"user_id"}, column: "private_key_encrypted", only: "private_key_encrypted IS NOT NULL"},
	{table: "pool_identities", keys: []string{"user_id"}, column: "box_private_key_encrypted", only: "box_private_key_encrypted IS NOT NULL"},
	// The SOCKS URL of a discovered proxy.
	{table: "outbound_proxies", keys: []string{"id"}, column: "secret_encrypted", only: "secret_encrypted IS NOT NULL"},
	// The admin's Artificial Analysis key.
	{table: "settings", keys: []string{"key"}, column: "value", jsonString: true,
		only: "key = 'artificial_analysis_key' AND value IS NOT NULL"},
	// A person's secret settings.
	{table: "user_settings", keys: []string{"user_id", "key"}, column: "value", jsonString: true,
		only: "value IS NOT NULL", quiet: true},
}

// upgradeDatabase brings a Node.js database to this binary's schema and
// re-seals its encrypted values. The caller has already saved the data.
func (m *Manager) upgradeDatabase(ctx context.Context, cfg config.Config) error {
	key, err := secrets.EnsureKey(cfg.Paths.SecretKey)
	if err != nil {
		return err
	}
	sealer, err := secrets.New(key)
	if err != nil {
		return err
	}
	db, err := dbx.Open(ctx, cfg.Paths.Database, dbx.Options{})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := schema.Ensure(ctx, db.W); err != nil {
		return err
	}

	skipped := 0
	for _, column := range legacyColumns {
		missed, err := resealColumn(ctx, db, sealer, key, column)
		if err != nil {
			return err
		}
		if !column.quiet {
			skipped += missed
		}
	}
	if skipped > 0 {
		m.UI.Warn(fmt.Sprintf("%d encrypted value(s) could not be opened with this install's secret.key and were left as they were", skipped))
	}
	return nil
}

// resealColumn rewrites the legacy values of one column inside one transaction
// and returns how many it could not open.
func resealColumn(ctx context.Context, db *dbx.DB, sealer *secrets.Sealer, key []byte, spec legacyColumn) (int, error) {
	query := fmt.Sprintf(`SELECT %s, %s FROM %s WHERE %s`,
		strings.Join(spec.keys, ", "), spec.column, spec.table, spec.only)
	rows, err := db.R.QueryContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("manager: read %s.%s: %w", spec.table, spec.column, err)
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
			return skipped, fmt.Errorf("manager: read %s.%s: %w", spec.table, spec.column, err)
		}
		encoded := value.String
		if spec.jsonString {
			var inner string
			if json.Unmarshal([]byte(encoded), &inner) != nil {
				continue
			}
			encoded = inner
		}
		if encoded == "" || strings.HasPrefix(encoded, secrets.Prefix) {
			continue
		}
		plain, oerr := secrets.OpenLegacy(encoded, key)
		if oerr != nil {
			skipped++
			continue
		}
		updates = append(updates, pending{keys: values, old: value.String, plain: plain})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return skipped, fmt.Errorf("manager: read %s.%s: %w", spec.table, spec.column, err)
	}
	rows.Close()
	if len(updates) == 0 {
		return skipped, nil
	}

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return skipped, fmt.Errorf("manager: begin re-seal: %w", err)
	}
	conditions := make([]string, 0, len(spec.keys)+1)
	for _, name := range spec.keys {
		conditions = append(conditions, name+" = ?")
	}
	conditions = append(conditions, spec.column+" = ?")
	statement := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s`,
		spec.table, spec.column, strings.Join(conditions, " AND "))
	for _, update := range updates {
		sealed, serr := sealer.Seal(update.plain)
		if serr != nil {
			tx.Rollback()
			return skipped, fmt.Errorf("manager: seal %s.%s: %w", spec.table, spec.column, serr)
		}
		stored := sealed
		if spec.jsonString {
			encoded, jerr := json.Marshal(sealed)
			if jerr != nil {
				tx.Rollback()
				return skipped, fmt.Errorf("manager: encode %s.%s: %w", spec.table, spec.column, jerr)
			}
			stored = string(encoded)
		}
		args := make([]any, 0, len(update.keys)+2)
		args = append(args, stored)
		for _, value := range update.keys {
			args = append(args, value)
		}
		args = append(args, update.old)
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			tx.Rollback()
			return skipped, fmt.Errorf("manager: re-seal %s.%s: %w", spec.table, spec.column, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return skipped, fmt.Errorf("manager: commit re-seal of %s.%s: %w", spec.table, spec.column, err)
	}
	return skipped, nil
}
