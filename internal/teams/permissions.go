// Package teams owns NeoRouter's sharing model: the permissions an owner can
// grant a team member, membership, invite links, the team audit trail, and the
// X-NeoRouter-Space request scope that decides whose data a request works on.
//
// It is the port of server/services/teams/* and server/middleware/space.js. The
// package never imports another domain: it takes the authenticated account from
// an actor function the wiring supplies, and exposes the resolved space through
// the request context for the other domains to scope their SQL with.
package teams

import "strings"

// PERMISSIONS is what an owner can let a team member do in the owner's space,
// in the order the API lists them. Every create/edit/delete right brings the
// matching view right with it (see Normalize), so a member is never allowed to
// change what they cannot see.
var PERMISSIONS = []string{
	"activity.view",
	"playground.use",
	"keys.view",
	"keys.create",
	"keys.edit",
	"keys.delete",
	"providers.view",
	"providers.manage",
	"providers.delete",
	"models.manage",
	"routing.view",
	"routing.edit",
	"processing.view",
	"processing.edit",
}

var permissionSet = func() map[string]bool {
	set := make(map[string]bool, len(PERMISSIONS))
	for _, p := range PERMISSIONS {
		set[p] = true
	}
	return set
}()

// Presets are the named permission bundles the Team page offers.
type Presets struct {
	View []string `json:"view"`
	Keys []string `json:"keys"`
	Full []string `json:"full"`
}

// PermissionPresets mirrors the PRESETS object of permissions.js: read-only
// access, the key management set, and everything.
var PermissionPresets = Presets{
	View: []string{"activity.view", "keys.view", "providers.view", "routing.view", "processing.view"},
	Keys: []string{
		"activity.view", "playground.use", "keys.view", "keys.create", "keys.edit",
		"keys.delete", "providers.view", "routing.view",
	},
	Full: append([]string(nil), PERMISSIONS...),
}

// viewFor returns the view permission a write permission implies, or "".
func viewFor(permission string) string {
	area, action, _ := strings.Cut(permission, ".")
	if action == "view" || action == "use" {
		return ""
	}
	if area == "models" {
		return "providers.view"
	}
	return area + ".view"
}

// Normalize keeps only known permissions, adds the view right each of them
// brings, and returns them in PERMISSIONS order.
func Normalize(permissions []string) []string {
	kept := make(map[string]bool, len(permissions))
	for _, permission := range permissions {
		if !permissionSet[permission] {
			continue
		}
		kept[permission] = true
		if view := viewFor(permission); view != "" {
			kept[view] = true
		}
	}
	out := make([]string, 0, len(kept))
	for _, permission := range PERMISSIONS {
		if kept[permission] {
			out = append(out, permission)
		}
	}
	return out
}

// Parse reads a permissions_json column: unknown entries are dropped, a
// malformed or non-array value is no permissions at all.
func Parse(raw string) []string {
	values, ok := decodeStringArray(raw)
	if !ok {
		return []string{}
	}
	return Normalize(values)
}
