package live

import (
	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// The wave-1 services satisfy the consumers of this package as they are; wiring
// passes them straight into Options. Only Accounts needs an adapter, because the
// relay wants an account's connection and the definition it was created from in
// one value.
var (
	_ Routing    = (*routing.Service)(nil)
	_ RouterKeys = (*routerkeys.Keys)(nil)
	_ KeySlots   = (*routerkeys.Concurrency)(nil)
	_ Quota      = (*quota.Service)(nil)
	_ Budget     = (*budget.Service)(nil)
	_ Usage      = (*usage.Service)(nil)
)
