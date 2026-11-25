package processing

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// memorySettings is the settings store the tests stand in for: the same JSON
// values the settings domain keeps.
type memorySettings struct {
	mu      sync.Mutex
	globals map[string]json.RawMessage
	users   map[string]map[string]json.RawMessage
}

func newMemorySettings() *memorySettings {
	return &memorySettings{globals: map[string]json.RawMessage{}, users: map[string]map[string]json.RawMessage{}}
}

func (m *memorySettings) GetGlobal(ctx context.Context, key string) (json.RawMessage, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.globals[key]
	return value, ok, nil
}

func (m *memorySettings) GetGlobalBool(ctx context.Context, key string, def bool) bool {
	value, ok, err := m.GetGlobal(ctx, key)
	if err != nil || !ok {
		return def
	}
	var decoded bool
	if err := json.Unmarshal(value, &decoded); err != nil {
		return def
	}
	return decoded
}

func (m *memorySettings) SetGlobal(ctx context.Context, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globals[key] = encoded
	return nil
}

func (m *memorySettings) GetUserSetting(ctx context.Context, userID, key string) (json.RawMessage, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.users[userID][key]
	return value, ok, nil
}

func (m *memorySettings) SetUserSetting(ctx context.Context, userID, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.users[userID] == nil {
		m.users[userID] = map[string]json.RawMessage{}
	}
	m.users[userID][key] = encoded
	return nil
}

// testService is a Service over in-memory settings and a silent logger.
func testService(t *testing.T) (*Service, *memorySettings) {
	t.Helper()
	store := newMemorySettings()
	service, err := New(func() config.Config { return config.Config{} }, store, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return service, store
}

// vaultFixture is testdata/vault.json: values the restore tests hide and put back.
type vaultFixture struct {
	Key string `json:"key"`
	PEM string `json:"pem"`
}

func loadVaultFixture(t *testing.T) vaultFixture {
	t.Helper()
	var fixture vaultFixture
	readTestdata(t, "vault.json", &fixture)
	if fixture.Key == "" || fixture.PEM == "" {
		t.Fatalf("vault.json is empty")
	}
	return fixture
}

// restoreModes is DEFAULT_MODES with secrets put back, as the JS restore test
// uses them.
func restoreModes() map[string]string {
	modes := map[string]string{}
	for category, mode := range DefaultModes {
		modes[category] = mode
	}
	modes["secret"] = "restore"
	return modes
}

// hideSecretModes hides secrets for good.
func hideSecretModes() map[string]string {
	modes := restoreModes()
	modes["secret"] = "hide"
	return modes
}

// userMessage is a one-message canonical request.
func userMessage(content string) Canonical {
	return Canonical{"messages": []any{map[string]any{"role": "user", "content": content}}}
}

// lastContent is the content of the last message of a canonical request.
func lastContent(canonical Canonical) string {
	messages := canonical.Messages()
	if len(messages) == 0 {
		return ""
	}
	content, _ := messages[len(messages)-1]["content"].(string)
	return content
}
