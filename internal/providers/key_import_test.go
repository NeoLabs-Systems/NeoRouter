package providers

// The key-file import.

import (
	"context"
	"strings"
	"testing"
)

// importKey is one entry of an exported file.
func importKey(definitionID, label, secret, baseURL string) Input {
	in := Input{
		DefinitionID: Optional[string]{Set: true, Value: &definitionID},
		Secret:       Optional[string]{Set: true, Value: &secret},
	}
	if label != "" {
		in.Label = Optional[string]{Set: true, Value: &label}
	}
	if baseURL != "" {
		in.BaseURL = Optional[string]{Set: true, Value: &baseURL}
	}
	return in
}

func TestImportKeysImportsSkipsAndFails(t *testing.T) {
	service, _ := testServiceWith(t, func(service *Service) {
		service.LocalModels = fakeLocalModels{allowed: false, offered: false}
	})
	existing := createAccount(t, service, "admin", "custom_openai", withSecret("already-there"))
	stored, err := service.Secrets.Open(*accountRow(t, service, existing.Account.ID).SecretEncrypted)
	if err != nil {
		t.Fatalf("open stored secret: %v", err)
	}
	result, err := service.ImportKeys(context.Background(), "admin", "admin", nil, ImportFile{
		Kind: KeyImportKind,
		Keys: []Input{
			// The same provider, key and address: already connected.
			importKey("custom_openai", "Same", stored, "http://127.0.0.1:9/v1"),
			// A new provider key is imported.
			importKey("groq", "", "gsk-imported", ""),
			// A provider this server does not have is refused, not thrown.
			importKey("nope", "Mystery", "key", ""),
			// Local models are an admin's decision.
			importKey("neorouter_local", "Local", "unused", ""),
		},
	})
	if err != nil {
		t.Fatalf("ImportKeys: %v", err)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Label != "Same" {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	if len(result.Imported) != 1 || result.Imported[0].Label != "Groq" {
		t.Fatalf("imported = %+v", result.Imported)
	}
	if len(result.Failed) != 2 {
		t.Fatalf("failed = %+v", result.Failed)
	}
	if result.Failed[0].Code != "UNKNOWN_PROVIDER" || result.Failed[0].Message != "This provider is not available on this server." {
		t.Fatalf("unknown provider = %+v", result.Failed[0])
	}
	if result.Failed[1].Code != "LOCAL_MODELS_NOT_SHARED" || result.Failed[1].Message != "Local models are not shared with you." {
		t.Fatalf("local models = %+v", result.Failed[1])
	}
	// The imported connection is a real one, listed for its owner.
	accounts, err := service.List(context.Background(), "admin")
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts = %+v, %v", accounts, err)
	}
	// Its models were listed once (the catalog is told about every import).
	catalog := service.Catalog.(*fakeCatalog)
	if len(catalog.synced) != 1 || catalog.synced[0] != result.Imported[0].AccountID {
		t.Fatalf("synced = %+v", catalog.synced)
	}
}

func TestImportKeysReportsARefusedKeyAndKeepsGoing(t *testing.T) {
	service, factory := testService(t)
	factory.validate = func(account Account) (Validation, error) {
		if account.Secret == "refuse-me" {
			return InvalidKey("bad key"), nil
		}
		return ValidKey(), nil
	}
	result, err := service.ImportKeys(context.Background(), "admin", "admin", nil, ImportFile{
		Kind: KeyImportKind,
		Keys: []Input{
			importKey("groq", "Bad", "refuse-me", ""),
			importKey("groq", "Good", "fine", ""),
		},
	})
	if err != nil {
		t.Fatalf("ImportKeys: %v", err)
	}
	if len(result.Failed) != 1 || result.Failed[0].Code != "KEY_REJECTED" {
		t.Fatalf("failed = %+v", result.Failed)
	}
	if want := "Groq rejected this key: bad key"; result.Failed[0].Message != want {
		t.Fatalf("message = %q", result.Failed[0].Message)
	}
	if len(result.Imported) != 1 || result.Imported[0].Label != "Good" {
		t.Fatalf("imported = %+v", result.Imported)
	}
}

func TestImportFileChecks(t *testing.T) {
	service, _ := testService(t)
	cases := []struct {
		name     string
		file     ImportFile
		wantText string
	}{
		{
			name:     "the export kind",
			file:     ImportFile{Kind: "other", Keys: []Input{importKey("groq", "", "key", "")}},
			wantText: `kind: Invalid literal value, expected "neorouter-keys"`,
		},
		{
			name:     "at least one key",
			file:     ImportFile{Kind: KeyImportKind},
			wantText: "keys: Array must contain at least 1 element(s)",
		},
		{
			name:     "a key needs a secret",
			file:     ImportFile{Kind: KeyImportKind, Keys: []Input{importKey("groq", "", "", "")}},
			wantText: "secret: String must contain at least 1 character(s)",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.ImportKeys(context.Background(), "admin", "admin", nil, test.file)
			if err == nil || errorCode(t, err) != "VALIDATION_ERROR" {
				t.Fatalf("err = %v", err)
			}
			if err.Error() != test.wantText {
				t.Fatalf("message = %q, want %q", err.Error(), test.wantText)
			}
		})
	}
	// A file larger than the API takes is refused, and a caller that bypasses the
	// check never imports more than the maximum.
	keys := make([]Input, MaxImportedKeys+1)
	for i := range keys {
		keys[i] = importKey("groq", "", "key", "")
	}
	if _, err := service.ImportKeys(context.Background(), "admin", "admin", nil, ImportFile{Kind: KeyImportKind, Keys: keys}); err == nil {
		t.Fatalf("an oversized file was accepted")
	}
}

func TestImportUsesTheProviderLabelForAnUnnamedKey(t *testing.T) {
	service, _ := testService(t)
	result, err := service.ImportKeys(context.Background(), "admin", "admin", nil, ImportFile{
		Kind: KeyImportKind,
		Keys: []Input{importKey("strict", "", "key", "")},
	})
	if err != nil {
		t.Fatalf("ImportKeys: %v", err)
	}
	if len(result.Failed) != 1 || result.Failed[0].Label != "Strict Provider" {
		t.Fatalf("failed = %+v", result.Failed)
	}
	if !strings.Contains(result.Failed[0].Message, "Account ID is required.") {
		t.Fatalf("message = %q", result.Failed[0].Message)
	}
}
