package teams_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/teams"
)

// TestInviteLinkStoresOnlyAHash ports the first team test: an invite link
// carries a token whose digest is all the database keeps, and either side can
// preview what it would give.
func TestInviteLinkStoresOnlyAHash(t *testing.T) {
	h := newHarness(t)
	memberToken, _ := h.register("member")

	created := h.do("POST", "/api/v1/teams/invites", map[string]any{
		"label": "test", "permissions": []string{"keys.create"},
	}, h.ownerToken, "")
	if created.status != http.StatusCreated {
		t.Fatalf("create invite: %d %s", created.status, created.body)
	}
	equalStrings(t, "create keys", orderedKeys(t, created.body), []string{"invite", "link"})
	equalStrings(t, "invite keys", orderedKeys(t, nested(t, created.body, "invite")),
		[]string{"id", "label", "permissions", "singleUse", "useCount", "expiresAt", "createdAt", "status"})
	if label := path(t, created.object(t), "invite", "label"); label != "test" {
		t.Fatalf("label: %v", label)
	}
	if status := path(t, created.object(t), "invite", "status"); status != "active" {
		t.Fatalf("status: %v", status)
	}
	link := path(t, created.object(t), "link").(string)
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if !strings.HasSuffix(parsed.Path, "/app/") {
		t.Fatalf("the link opens the app page: %q", link)
	}
	token := parsed.Query().Get("invite")
	if len(token) != 32 {
		t.Fatalf("invite token: %q", token)
	}
	var count int
	if err := h.db.R.QueryRow(`SELECT COUNT(*) FROM team_invites WHERE token_hash = ?`, token).Scan(&count); err != nil {
		t.Fatalf("count by raw token: %v", err)
	}
	if count != 0 {
		t.Fatalf("only the digest is stored, never the token")
	}

	preview := h.do("POST", "/api/v1/teams/invites/preview", map[string]any{"link": link}, memberToken, "")
	if preview.status != http.StatusOK {
		t.Fatalf("preview: %d %s", preview.status, preview.body)
	}
	equalStrings(t, "preview keys", orderedKeys(t, preview.body), []string{"owner", "permissions", "expiresAt", "singleUse"})
	if owner := path(t, preview.object(t), "owner", "username"); owner != "owner" {
		t.Fatalf("preview owner: %v", owner)
	}
	equalStrings(t, "create brings view with it",
		anyStrings(t, path(t, preview.object(t), "permissions")), []string{"keys.view", "keys.create"})

	own := h.do("POST", "/api/v1/teams/invites/preview", map[string]any{"link": link}, h.ownerToken, "")
	if own.status != http.StatusBadRequest || own.code(t) != "INVITE_SELF" {
		t.Fatalf("the owner's own link: %d %s", own.status, own.body)
	}
}

// TestSingleUseInviteAndRevokedLink ports the second: a single-use link works
// once, an account in a team cannot join another, and a revoked link is dead.
func TestSingleUseInviteAndRevokedLink(t *testing.T) {
	h := newHarness(t)
	memberToken, _ := h.register("member")
	outsiderToken, _ := h.register("outsider")

	single := h.invite(map[string]any{"permissions": []string{"keys.view"}, "singleUse": true})
	joined := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": single}, memberToken, "")
	if joined.status != http.StatusOK {
		t.Fatalf("redeem: %d %s", joined.status, joined.body)
	}
	equalStrings(t, "memberOf keys", orderedKeys(t, nested(t, joined.body, "memberOf")),
		[]string{"owner", "permissions", "joinedAt"})
	equalStrings(t, "team user keys", orderedKeys(t, nested(t, nested(t, joined.body, "memberOf"), "owner")),
		[]string{"id", "username", "displayName"})
	if owner := path(t, joined.object(t), "memberOf", "owner", "username"); owner != "owner" {
		t.Fatalf("the redeemed summary names the team owner: %v", owner)
	}
	again := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": single}, outsiderToken, "")
	if again.status != http.StatusGone || again.code(t) != "INVITE_USED" {
		t.Fatalf("a single-use link works once: %d %s", again.status, again.body)
	}

	second := h.invite(map[string]any{"permissions": []string{"keys.view"}})
	already := h.do("POST", "/api/v1/teams/invites/preview", map[string]any{"link": second}, memberToken, "")
	if already.status != http.StatusConflict || already.code(t) != "ALREADY_MEMBER" {
		t.Fatalf("an account already in a team: %d %s", already.status, already.body)
	}
	summary := h.do("GET", "/api/v1/teams", nil, h.ownerToken, "")
	var openID string
	for _, item := range path(t, summary.object(t), "invites").([]any) {
		invite := item.(map[string]any)
		if invite["status"] == "active" {
			openID = invite["id"].(string)
			break
		}
	}
	if openID == "" {
		t.Fatalf("expected an active invite: %s", summary.body)
	}
	revoked := h.do("DELETE", "/api/v1/teams/invites/"+openID, nil, h.ownerToken, "")
	if revoked.status != http.StatusOK {
		t.Fatalf("revoke: %d %s", revoked.status, revoked.body)
	}
	events, err := h.teams.AuditList(t.Context(), 100)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(events) == 0 || events[0].Action != "invite.revoke" {
		t.Fatalf("the revocation is in the audit trail: %+v", events)
	}
	dead := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": second}, outsiderToken, "")
	if dead.status != http.StatusGone || dead.code(t) != "INVITE_REVOKED" {
		t.Fatalf("a revoked link: %d %s", dead.status, dead.body)
	}
}

// TestSetPermissionsNormalizes ports the permission normalisation of the owner's
// grant and the shape of the team summary.
func TestSetPermissionsNormalizes(t *testing.T) {
	h := newHarness(t)
	memberToken, memberID := h.register("member")
	link := h.invite(map[string]any{"permissions": []string{"keys.view"}})
	if redeemed := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": link}, memberToken, ""); redeemed.status != http.StatusOK {
		t.Fatalf("redeem: %d %s", redeemed.status, redeemed.body)
	}

	updated := h.do("PUT", "/api/v1/teams/members/"+memberID+"/permissions",
		map[string]any{"permissions": []string{"keys.create", "providers.view"}}, h.ownerToken, "")
	if updated.status != http.StatusOK {
		t.Fatalf("set permissions: %d %s", updated.status, updated.body)
	}
	equalStrings(t, "summary keys", orderedKeys(t, updated.body),
		[]string{"memberOf", "members", "invites", "permissions", "presets"})
	equalStrings(t, "a write right brings its view with it",
		anyStrings(t, path(t, updated.object(t), "members", "0", "permissions")),
		[]string{"keys.view", "keys.create", "providers.view"})
	if name := path(t, updated.object(t), "members", "0", "user", "username"); name != "member" {
		t.Fatalf("the member is listed with their name: %v", name)
	}
	equalStrings(t, "the permission catalog", anyStrings(t, path(t, updated.object(t), "permissions")), teams.PERMISSIONS)
	equalStrings(t, "preset keys", orderedKeys(t, nested(t, updated.body, "presets")),
		[]string{"view", "keys", "full"})

	unknown := h.do("PUT", "/api/v1/teams/members/does-not-exist/permissions",
		map[string]any{"permissions": []string{"keys.view"}}, h.ownerToken, "")
	if unknown.status != http.StatusNotFound || unknown.code(t) != "MEMBER_NOT_FOUND" {
		t.Fatalf("a person who is not in the team: %d %s", unknown.status, unknown.body)
	}
}

// TestInviteLimit ports the cap on open invite links.
func TestInviteLimit(t *testing.T) {
	h := newHarness(t)
	summary := h.do("GET", "/api/v1/teams", nil, h.ownerToken, "")
	if summary.status != http.StatusOK {
		t.Fatalf("summary: %d %s", summary.status, summary.body)
	}
	if invites := path(t, summary.object(t), "invites").([]any); len(invites) != 0 {
		t.Fatalf("a fresh team has no invite links: %s", summary.body)
	}
	for index := 0; index < teams.MaxActiveInvites; index++ {
		h.invite(map[string]any{"permissions": []string{"keys.view"}})
	}
	blocked := h.do("POST", "/api/v1/teams/invites", map[string]any{"permissions": []string{"keys.view"}}, h.ownerToken, "")
	if blocked.status != http.StatusTooManyRequests || blocked.code(t) != "INVITE_LIMIT" {
		t.Fatalf("the invite cap: %d %s", blocked.status, blocked.body)
	}
	if message := path(t, blocked.object(t), "error", "message"); message != "You already have 25 open invite links. Revoke some first." {
		t.Fatalf("INVITE_LIMIT message: %v", message)
	}
}

// TestInviteRequestBodyValidation ports the zod body rules of POST /teams/invites.
func TestInviteRequestBodyValidation(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name    string
		body    map[string]any
		message string
	}{
		{"no permissions", map[string]any{}, "permissions: Required"},
		{"unknown permission", map[string]any{"permissions": []string{"nope"}},
			"permissions.0: Invalid enum value. Expected 'activity.view' | 'playground.use' | 'keys.view' | " +
				"'keys.create' | 'keys.edit' | 'keys.delete' | 'providers.view' | 'providers.manage' | " +
				"'providers.delete' | 'models.manage' | 'routing.view' | 'routing.edit' | 'processing.view' | " +
				"'processing.edit', received 'nope'"},
		{"float hours", map[string]any{"permissions": []string{}, "expiresInHours": 1.5},
			"expiresInHours: Expected integer, received float"},
		{"hours too small", map[string]any{"permissions": []string{}, "expiresInHours": 0},
			"expiresInHours: Number must be greater than or equal to 1"},
		{"hours too large", map[string]any{"permissions": []string{}, "expiresInHours": 24*90 + 1},
			"expiresInHours: Number must be less than or equal to 2160"},
		{"label too long", map[string]any{"permissions": []string{}, "label": strings.Repeat("x", 81)},
			"label: String must contain at most 80 character(s)"},
		{"single use not a boolean", map[string]any{"permissions": []string{}, "singleUse": "yes"},
			"singleUse: Expected boolean, received string"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := h.do("POST", "/api/v1/teams/invites", testCase.body, h.ownerToken, "")
			if response.status != http.StatusBadRequest || response.code(t) != "VALIDATION_ERROR" {
				t.Fatalf("status %d body %s", response.status, response.body)
			}
			if message := path(t, response.object(t), "error", "message"); message != testCase.message {
				t.Fatalf("message\n got %q\nwant %q", message, testCase.message)
			}
		})
	}
}

// TestInviteExpiry ports the expiry of an invite link.
func TestInviteExpiry(t *testing.T) {
	h := newHarness(t)
	memberToken, _ := h.register("member")

	link := h.invite(map[string]any{"permissions": []string{"keys.view"}, "expiresInHours": 1})
	preview := h.do("POST", "/api/v1/teams/invites/preview", map[string]any{"link": link}, memberToken, "")
	if preview.status != http.StatusOK {
		t.Fatalf("preview: %d %s", preview.status, preview.body)
	}
	if expiresAt := path(t, preview.object(t), "expiresAt"); expiresAt == nil {
		t.Fatalf("the link expires: %s", preview.body)
	}
	if _, err := h.db.W.Exec(`UPDATE team_invites SET expires_at = '2000-01-01T00:00:00.000Z'`); err != nil {
		t.Fatalf("expire the invite: %v", err)
	}
	expired := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": link}, memberToken, "")
	if expired.status != http.StatusGone || expired.code(t) != "INVITE_EXPIRED" {
		t.Fatalf("an expired link: %d %s", expired.status, expired.body)
	}
	summary := h.do("GET", "/api/v1/teams", nil, h.ownerToken, "")
	invites := path(t, summary.object(t), "invites").([]any)
	if status := path(t, invites[0], "status"); status != "expired" {
		t.Fatalf("the summary reports the expiry: %s", summary.body)
	}
}

// TestInviteTokenFromLink ports the lenient token parsing: the whole link or the
// bare token work, anything else does not.
func TestInviteTokenFromLink(t *testing.T) {
	h := newHarness(t)
	memberToken, _ := h.register("member")
	link := h.invite(map[string]any{"permissions": []string{"keys.view"}})
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	token := parsed.Query().Get("invite")

	bare := h.do("POST", "/api/v1/teams/invites/preview", map[string]any{"link": token}, memberToken, "")
	if bare.status != http.StatusOK {
		t.Fatalf("a bare token previews: %d %s", bare.status, bare.body)
	}
	junk := h.do("POST", "/api/v1/teams/invites/preview", map[string]any{"link": "not a link!"}, memberToken, "")
	if junk.status != http.StatusNotFound || junk.code(t) != "INVITE_INVALID" {
		t.Fatalf("junk: %d %s", junk.status, junk.body)
	}
	if message := path(t, junk.object(t), "error", "message"); message != "This invite link isn’t valid." {
		t.Fatalf("INVITE_INVALID message: %v", message)
	}
}

// TestAuditTrailPortsShape ports the audit rows the team page relies on.
func TestAuditTrailShape(t *testing.T) {
	h := newHarness(t)
	memberToken, memberID := h.register("member")
	link := h.invite(map[string]any{"label": "audit", "permissions": []string{"keys.view"}, "singleUse": true})
	if redeemed := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": link}, memberToken, ""); redeemed.status != http.StatusOK {
		t.Fatalf("redeem: %d %s", redeemed.status, redeemed.body)
	}
	if updated := h.do("PUT", "/api/v1/teams/members/"+memberID+"/permissions",
		map[string]any{"permissions": []string{"keys.create"}}, h.ownerToken, ""); updated.status != http.StatusOK {
		t.Fatalf("set permissions: %d %s", updated.status, updated.body)
	}

	events, err := h.teams.AuditList(t.Context(), 100)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected three events, got %d", len(events))
	}
	newest := events[0]
	if newest.Action != "member.permissions" {
		t.Fatalf("newest first: %+v", newest)
	}
	if newest.Actor == nil || *newest.Actor != "owner" {
		t.Fatalf("the actor is named: %+v", newest.Actor)
	}
	if newest.Subject == nil || *newest.Subject != "member" {
		t.Fatalf("the subject is named: %+v", newest.Subject)
	}
	var detail map[string]any
	if err := json.Unmarshal(newest.Detail, &detail); err != nil {
		t.Fatalf("detail: %v", err)
	}
	if permissions := anyStrings(t, detail["permissions"]); len(permissions) != 2 || permissions[1] != "keys.create" {
		t.Fatalf("detail: %v", detail)
	}

	join := events[1]
	if join.Action != "member.join" {
		t.Fatalf("the join: %+v", join)
	}
	var joinDetail map[string]any
	if err := json.Unmarshal(join.Detail, &joinDetail); err != nil {
		t.Fatalf("join detail: %v", err)
	}
	if _, ok := joinDetail["inviteId"].(string); !ok {
		t.Fatalf("the join names the invite: %v", joinDetail)
	}
	create := events[2]
	if create.Action != "invite.create" {
		t.Fatalf("the invite creation: %+v", create)
	}
	var createDetail map[string]any
	if err := json.Unmarshal(create.Detail, &createDetail); err != nil {
		t.Fatalf("create detail: %v", err)
	}
	if createDetail["singleUse"] != true {
		t.Fatalf("the invite detail: %v", createDetail)
	}
	equalStrings(t, "audit keys", orderedKeys(t, mustJSON(t, newest)),
		[]string{"id", "action", "actor", "subject", "detail", "createdAt"})
}

// mustJSON re-encodes an audit event the way the admin endpoint would.
func mustJSON(t *testing.T, event teams.AuditEvent) []byte {
	t.Helper()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	return raw
}
