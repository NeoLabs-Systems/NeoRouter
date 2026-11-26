package localmodels

// access.go decides who may use the models running on this server, and their
// connections to them. Admins always may; everyone else only when an admin
// allows it. Each person reaches the models through a "NeoRouter Local"
// connection, which the installing admin gets automatically and others can add
// like any provider.

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// DefinitionID is the provider definition of the local models connection.
const DefinitionID = "neorouter_local"

// AccessSetting is the global setting an admin's choice is stored in.
const AccessSetting = "local_models_access"

// AccessLevels are the values of that setting.
var AccessLevels = []string{"admins", "everyone"}

// syncDelayMs is how long a change waits before the connections are relisted,
// so a burst of model starts costs one sync.
const syncDelayMs = 1000

// SettingsStore is the global key/value store internal/settings provides.
type SettingsStore interface {
	GetGlobalString(ctx context.Context, key, def string) string
	SetGlobal(ctx context.Context, key string, value any) error
}

// Access decides who may use local models and keeps their connections current.
type Access struct {
	DB *dbx.DB
	// Settings is internal/settings.
	Settings SettingsStore
	Log      *slog.Logger
	// ConnectAccount gives a person a "NeoRouter Local" provider account; nil
	// means accounts cannot be created from here.
	ConnectAccount func(ctx context.Context, userID string) error
	// SyncAccounts relists one local connection's models; nil skips the sync.
	SyncAccounts func(ctx context.Context, accountID string) error

	mu    sync.Mutex
	timer *time.Timer
}

func (a *Access) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

// Level is who may use local models: "admins" or "everyone".
func (a *Access) Level(ctx context.Context) string {
	value := "admins"
	if a.Settings != nil {
		value = a.Settings.GetGlobalString(ctx, AccessSetting, "admins")
	}
	for _, level := range AccessLevels {
		if value == level {
			return level
		}
	}
	return "admins"
}

// SetLevel stores a new level and relists the connections.
func (a *Access) SetLevel(ctx context.Context, value string) error {
	known := false
	for _, level := range AccessLevels {
		if value == level {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("Unknown access level %s.", value)
	}
	if a.Settings == nil {
		return fmt.Errorf("localmodels: no settings store is wired")
	}
	if err := a.Settings.SetGlobal(ctx, AccessSetting, value); err != nil {
		return err
	}
	a.ScheduleSync()
	return nil
}

// IsAdmin reports whether a user is an administrator.
func (a *Access) IsAdmin(ctx context.Context, userID string) bool {
	var role string
	if err := a.DB.R.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, userID).Scan(&role); err != nil {
		return false
	}
	return role == "admin"
}

// Allowed reports whether a person may use this server's models.
func (a *Access) Allowed(ctx context.Context, userID string) bool {
	return a.Level(ctx) == "everyone" || a.IsAdmin(ctx, userID)
}

// Offered reports whether "NeoRouter Local" appears under Add connection for
// this person.
func (a *Access) Offered(ctx context.Context, userID string) bool {
	allowed := a.Allowed(ctx, userID)
	if !allowed {
		return false
	}
	store := Store{DB: a.DB}
	installed, err := store.HasInstalled(ctx)
	if err != nil {
		return false
	}
	return installed
}

// AccountIDs lists every local models connection.
func (a *Access) AccountIDs(ctx context.Context) ([]string, error) {
	rows, err := a.DB.R.QueryContext(ctx, `SELECT id FROM provider_accounts WHERE definition_id = ?`, DefinitionID)
	if err != nil {
		return nil, fmt.Errorf("localmodels: list local connections: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("localmodels: read local connection: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// HasAccount reports whether a person already has a local connection.
func (a *Access) HasAccount(ctx context.Context, userID string) bool {
	var found int
	err := a.DB.R.QueryRowContext(ctx,
		`SELECT 1 FROM provider_accounts WHERE definition_id = ? AND user_id = ?`, DefinitionID, userID).Scan(&found)
	return err == nil
}

// Connect gives the person a connection to the local models, unless they have
// one. A failure is logged, not returned: the models are installed either way.
func (a *Access) Connect(ctx context.Context, userID string) {
	if a.HasAccount(ctx, userID) || a.ConnectAccount == nil {
		return
	}
	if err := a.ConnectAccount(ctx, userID); err != nil {
		a.log().Warn("Could not add the local models connection", "error", err)
	}
}

// ScheduleSync relists every connection's models shortly after a model starts,
// stops or goes away.
func (a *Access) ScheduleSync() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.timer != nil {
		a.timer.Stop()
	}
	a.timer = time.AfterFunc(syncDelayMs*time.Millisecond, func() {
		if err := a.SyncAll(context.Background()); err != nil {
			a.log().Warn("Local models connection could not be synced", "error", err)
		}
	})
}

// SyncAll relists every local connection now.
func (a *Access) SyncAll(ctx context.Context) error {
	if a.SyncAccounts == nil {
		return nil
	}
	ids, err := a.AccountIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := a.SyncAccounts(ctx, id); err != nil {
			a.log().Warn("Local models connection could not be synced", "accountId", id, "error", err)
		}
	}
	return nil
}

// StopSync cancels a pending sync, for shutdown.
func (a *Access) StopSync() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
}
