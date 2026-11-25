package settings

// The JSON documents exportData() and exportKeys() return. Field order is the
// order the JavaScript object literals produce, and nullable columns are
// pointers so a missing value is null rather than omitted.

import "encoding/json"

// AccountExport is the bundle exportData() returns: everything stored for a
// person, without passwords, keys or other secrets.
type AccountExport struct {
	Format          int                    `json:"format"`
	Kind            string                 `json:"kind"`
	ExportedAt      string                 `json:"exportedAt"`
	Profile         ExportProfile          `json:"profile"`
	TwoFactor       ExportTwoFactor        `json:"twoFactor"`
	Settings        *orderedObject         `json:"settings"`
	Connections     []ExportConnection     `json:"connections"`
	RoutingGroups   json.RawMessage        `json:"routingGroups"`
	RoutingProfiles []ExportRoutingProfile `json:"routingProfiles"`
	RouterKeys      []ExportRouterKey      `json:"routerKeys"`
	Team            ExportTeam             `json:"team"`
	Pools           []ExportPool           `json:"pools"`
	Requests        []ExportRequest        `json:"requests"`
	MediaJobs       []ExportMediaJob       `json:"mediaJobs"`
}

// ExportProfile is the account itself.
type ExportProfile struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	Email       string  `json:"email"`
	DisplayName *string `json:"displayName"`
	Role        string  `json:"role"`
	CreatedAt   string  `json:"createdAt"`
	LastLoginAt *string `json:"lastLoginAt"`
}

// ExportTwoFactor reports whether two-factor authentication is on.
type ExportTwoFactor struct {
	Enabled bool `json:"enabled"`
}

// ExportConnection is one saved provider connection and its models.
type ExportConnection struct {
	ID           string          `json:"id"`
	Provider     string          `json:"provider"`
	DefinitionID string          `json:"definitionId"`
	Label        string          `json:"label"`
	Slug         string          `json:"slug"`
	BaseURL      *string         `json:"baseUrl"`
	Options      json.RawMessage `json:"options"`
	Billing      string          `json:"billing"`
	Prepaid      bool            `json:"prepaid"`
	Enabled      bool            `json:"enabled"`
	Status       string          `json:"status"`
	CreatedAt    string          `json:"createdAt"`
	Models       []ExportModel   `json:"models"`
}

// ExportModel is one catalogued model of a connection.
type ExportModel struct {
	ID         string  `json:"id"`
	Name       *string `json:"name"`
	Capability string  `json:"capability"`
	Enabled    bool    `json:"enabled"`
	Free       bool    `json:"free"`
}

// ExportRoutingProfile is one saved routing style.
type ExportRoutingProfile struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Weights     json.RawMessage `json:"weights"`
	Constraints json.RawMessage `json:"constraints"`
	CreatedAt   string          `json:"createdAt"`
}

// ExportRouterKey is one gateway key. Keys are stored only as a hash, so they
// are listed but cannot be exported.
type ExportRouterKey struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Prefix        string          `json:"prefix"`
	Mode          string          `json:"mode"`
	BudgetUSD     *float64        `json:"budget_usd"`
	BudgetPeriod  string          `json:"budget_period"`
	RPMLimit      *int64          `json:"rpm_limit"`
	Concurrent    *int64          `json:"concurrent_limit"`
	Profile       string          `json:"profile"`
	RoutingGroup  *string         `json:"routing_group"`
	ExpiresAt     *string         `json:"expires_at"`
	RevokedAt     *string         `json:"revoked_at"`
	LastUsedAt    *string         `json:"last_used_at"`
	CreatedAt     string          `json:"created_at"`
	AppName       *string         `json:"app_name"`
	TokenLimit    *int64          `json:"token_limit"`
	TokenPeriod   string          `json:"token_period"`
	Priority      string          `json:"priority"`
	AllowedModels json.RawMessage `json:"allowedModels"`
}

// ExportTeam is who this person belongs to and who belongs to them.
type ExportTeam struct {
	MemberOf *ExportTeamMemberOf `json:"memberOf"`
	Members  []ExportTeamMember  `json:"members"`
}

// ExportTeamMemberOf is the team this person is a member of.
type ExportTeamMemberOf struct {
	OwnerUserID string `json:"owner_user_id"`
	CreatedAt   string `json:"created_at"`
}

// ExportTeamMember is one member of this person's own team.
type ExportTeamMember struct {
	MemberUserID string `json:"member_user_id"`
	CreatedAt    string `json:"created_at"`
}

// ExportPool is one pool this person joined.
type ExportPool struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Host     bool            `json:"host"`
	MyName   string          `json:"myName"`
	Share    json.RawMessage `json:"share"`
	Paused   bool            `json:"paused"`
	JoinedAt string          `json:"joinedAt"`
}

// ExportRequest is one request record: which model answered, tokens, cost.
// Prompts are never stored.
type ExportRequest struct {
	ID                  string  `json:"id"`
	CreatedAt           int64   `json:"created_at"`
	Capability          string  `json:"capability"`
	Task                *string `json:"task"`
	RequestedModel      string  `json:"requested_model"`
	ModelLabel          *string `json:"model_label"`
	Status              string  `json:"status"`
	HTTPStatus          *int64  `json:"http_status"`
	ErrorCode           *string `json:"error_code"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	Units               float64 `json:"units"`
	RoutingGroup        *string `json:"routing_group"`
	CachedInputTokens   int64   `json:"cached_input_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CostUSD             float64 `json:"cost_usd"`
	SavedUSD            float64 `json:"saved_usd"`
	IsFree              int64   `json:"is_free"`
	LatencyMS           *int64  `json:"latency_ms"`
	Stream              int64   `json:"stream"`
}

// ExportMediaJob is one media job with its request and result documents.
type ExportMediaJob struct {
	ID         string          `json:"id"`
	Capability string          `json:"capability"`
	ModelLabel *string         `json:"model_label"`
	Status     string          `json:"status"`
	CreatedAt  int64           `json:"created_at"`
	Request    json.RawMessage `json:"request"`
	Result     json.RawMessage `json:"result"`
}

// KeysExport is the key file exportKeys() returns.
type KeysExport struct {
	Format     int           `json:"format"`
	Kind       string        `json:"kind"`
	ExportedAt string        `json:"exportedAt"`
	Keys       []ExportedKey `json:"keys"`
}

// ExportedKey is one saved provider key in plain text, everything needed to add
// the connection again (Connections → import).
type ExportedKey struct {
	Provider       string          `json:"provider"`
	DefinitionID   string          `json:"definitionId"`
	Label          string          `json:"label"`
	BaseURL        *string         `json:"baseUrl"`
	Secret         string          `json:"secret"`
	BillingMode    string          `json:"billingMode"`
	Classification string          `json:"classification"`
	OverflowPolicy string          `json:"overflowPolicy"`
	SlotWaitMS     *int64          `json:"slotWaitMs"`
	Concurrent     *int64          `json:"concurrent"`
	Options        *orderedObject  `json:"options,omitempty"`
	Headers        json.RawMessage `json:"headers,omitempty"`
}
