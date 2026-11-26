package providers

// Adds connections from a NeoRouter key file (Settings → export saved API
// keys). Each key is checked with its provider like one added by hand; a key
// that is already connected is skipped, and one that fails does not stop the
// rest. Prompts and keys are never logged.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Key export file facts.
const (
	// KeyImportKind is the kind of a NeoRouter key file.
	KeyImportKind = "neorouter-keys"
	// MaxImportedKeys is how many keys one import takes.
	MaxImportedKeys = 200
)

// ImportFile is the body of an import request.
type ImportFile struct {
	Kind string  `json:"kind"`
	Keys []Input `json:"keys"`
}

// ImportedKey is one connection an import added.
type ImportedKey struct {
	Label     string `json:"label"`
	AccountID string `json:"accountId"`
}

// SkippedKey is one key an import did not need to add.
type SkippedKey struct {
	Label string `json:"label"`
}

// FailedKey is one key an import could not add, and why.
type FailedKey struct {
	Label   string `json:"label"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ImportResult is what one import did.
type ImportResult struct {
	Imported []ImportedKey `json:"imported"`
	Skipped  []SkippedKey  `json:"skipped"`
	Failed   []FailedKey   `json:"failed"`
}

// ImportKeys adds every key of an exported file. ownerID is the space the
// connections go into, actorID is who imports them.
func (s *Service) ImportKeys(ctx context.Context, ownerID, actorID string, actor *Actor, file ImportFile) (ImportResult, error) {
	if err := s.validateImport(file); err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{Imported: []ImportedKey{}, Skipped: []SkippedKey{}, Failed: []FailedKey{}}
	keys := file.Keys
	if len(keys) > MaxImportedKeys {
		keys = keys[:MaxImportedKeys]
	}
	for _, key := range keys {
		definitionID := stringField(key.DefinitionID)
		label := stringField(key.Label)
		if label == "" {
			if definition, ok := s.Registry.Get(definitionID); ok {
				label = definition.Label
			} else {
				label = definitionID
			}
		}
		if refused := s.refusal(ownerID, definitionID); refused != nil {
			result.Failed = append(result.Failed, FailedKey{Label: label, Code: refused.Code, Message: refused.Message})
			continue
		}
		connected, err := s.alreadyConnected(ctx, ownerID, definitionID, stringField(key.Secret), stringField(key.BaseURL))
		if err != nil {
			return ImportResult{}, err
		}
		if connected {
			result.Skipped = append(result.Skipped, SkippedKey{Label: label})
			continue
		}
		created, err := s.Create(ctx, ownerID, key, actorID, actor)
		if err != nil {
			code := "IMPORT_FAILED"
			var httpErr *httpserver.HttpError
			if errors.As(err, &httpErr) && httpErr.Code != "" {
				code = httpErr.Code
			}
			result.Failed = append(result.Failed, FailedKey{Label: label, Code: code, Message: err.Error()})
			continue
		}
		s.FinishSetup(ctx, created.Account.ID)
		result.Imported = append(result.Imported, ImportedKey{Label: created.Account.Label, AccountID: created.Account.ID})
	}
	s.log().Info("Keys imported", "ownerId", ownerID, "imported", len(result.Imported), "skipped", len(result.Skipped), "failed", len(result.Failed))
	return result, nil
}

// validateImport checks the file: the export kind, and one valid account input
// per key with a secret.
func (s *Service) validateImport(file ImportFile) error {
	if file.Kind != KeyImportKind {
		return validationError(fmt.Sprintf("kind: Invalid literal value, expected %q", KeyImportKind))
	}
	if len(file.Keys) == 0 {
		return validationError("keys: Array must contain at least 1 element(s)")
	}
	if len(file.Keys) > MaxImportedKeys {
		return validationError(fmt.Sprintf("keys: Array must contain at most %d element(s)", MaxImportedKeys))
	}
	for _, key := range file.Keys {
		if err := s.ValidateInput(key, true); err != nil {
			return err
		}
		if stringField(key.Secret) == "" {
			return validationError("secret: String must contain at least 1 character(s)")
		}
	}
	return nil
}

// refusal is why a key cannot be imported here at all, or nil.
func (s *Service) refusal(ownerID, definitionID string) *FailedKey {
	definition, ok := s.Registry.Get(definitionID)
	if !ok || definition.Category == CategoryPool {
		return &FailedKey{Code: "UNKNOWN_PROVIDER", Message: "This provider is not available on this server."}
	}
	if definition.ID == LocalDefinitionID && (s.LocalModels == nil || !s.LocalModels.Allowed(ownerID)) {
		return &FailedKey{Code: "LOCAL_MODELS_NOT_SHARED", Message: "Local models are not shared with you."}
	}
	return nil
}

// alreadyConnected reports whether the same provider and key (and address, where
// it can be changed) is already there.
func (s *Service) alreadyConnected(ctx context.Context, ownerID, definitionID, secret, baseURL string) (bool, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT base_url, secret_encrypted FROM provider_accounts
		WHERE user_id = ? AND definition_id = ? AND secret_encrypted IS NOT NULL`, ownerID, definitionID)
	if err != nil {
		return false, fmt.Errorf("providers: read connected keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var storedBase sql.NullString
		var sealed sql.NullString
		if err := rows.Scan(&storedBase, &sealed); err != nil {
			return false, fmt.Errorf("providers: scan connected key: %w", err)
		}
		opened, err := s.Secrets.Open(sealed.String)
		if err != nil {
			continue
		}
		if opened != secret {
			continue
		}
		if baseURL == "" || storedBase.String == "" || storedBase.String == baseURL {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("providers: read connected keys: %w", err)
	}
	return false, nil
}
