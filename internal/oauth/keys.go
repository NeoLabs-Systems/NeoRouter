package oauth

import "context"

// AppInfo marks a router key that a connected app received.
type AppInfo struct {
	Name   string
	Origin *string
	Scopes []string
}

// KeyRequest is what "Connect with NeoRouter" asks internal/routerkeys to make:
// the approval's options, already restricted the way issueKey() restricts them
// (a budget only for paid models, a token limit only for free and plan models).
// A nil optional field is undefined in the JS flow, so the key service's own
// default applies.
type KeyRequest struct {
	Name               string
	AllowFree          bool
	AllowPlan          bool
	AllowPaid          bool
	BudgetUSD          *float64
	BudgetPeriod       *string // total, monthly or daily
	RPMLimit           *int64
	ExpiresAt          *string // ISO timestamp
	TokenLimit         *int64
	TokenPeriod        *string // total, monthly or daily
	MaxPricePerMillion *float64
}

// IssuedKey is the router key handed to the app.
type IssuedKey struct {
	ID        string
	Secret    string
	ExpiresAt *string // ISO timestamp, or nil when the key does not expire
}

// Keys makes the router keys an app receives (internal/routerkeys).
type Keys interface {
	// Modes validates the free/plan/paid switches before an approval is
	// accepted, like router_key_service.kinds: 400 NO_MODELS_ALLOWED
	// "Allow at least one kind of model: free, prepaid plans or paid."
	Modes(allowFree, allowPlan, allowPaid bool) error
	// AssertExpiry rejects an expiry that is not in the future, like
	// router_key_service.assertExpiry: 400 INVALID_KEY_EXPIRY
	// "Choose an expiry date and time in the future."
	AssertExpiry(expiresAt *string) error
	// Create issues the key for ownerID, records who created it and what it was
	// made for, and returns it with its one-time secret.
	Create(ownerID string, request KeyRequest, createdBy string, app AppInfo) (*IssuedKey, error)
}

// Accounts connects provider accounts (internal/providers): the OpenRouter key
// the flow receives is stored like any manually entered key.
type Accounts interface {
	// CreateAccount stores a new account and returns its id. It answers the
	// errors account_service.create raises, such as 422 KEY_REJECTED.
	CreateAccount(ctx context.Context, ownerID string, request NewAccount, createdBy string) (string, error)
}

// NewAccount is the account a completed OpenRouter flow stores.
type NewAccount struct {
	DefinitionID string // "openrouter"
	Secret       string
	BillingMode  string // "paid"
}

// Catalog refreshes the models of a provider account (internal/catalog).
type Catalog interface {
	// SyncAccount fetches the account's models; the connection still succeeds
	// when it fails, exactly like openrouter_oauth.js.
	SyncAccount(ctx context.Context, accountID string) error
}
