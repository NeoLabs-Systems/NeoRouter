package teams_test

import (
	"net/http"
	"testing"
)

// TestMemberOnlyGetsWhatTheOwnerAllowed ports the permission checks: a member
// sees what was granted, nothing more, and never the team page of the owner.
func TestMemberOnlyGetsWhatTheOwnerAllowed(t *testing.T) {
	h := newHarness(t)
	memberToken, memberID := h.register("member")
	link := h.invite(map[string]any{"permissions": []string{"keys.view"}})
	if redeemed := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": link}, memberToken, ""); redeemed.status != http.StatusOK {
		t.Fatalf("redeem: %d %s", redeemed.status, redeemed.body)
	}

	listed := h.do("GET", "/api/v1/keys", nil, memberToken, h.ownerID)
	if listed.status != http.StatusOK {
		t.Fatalf("a granted view: %d %s", listed.status, listed.body)
	}
	if owner := path(t, listed.object(t), "owner"); owner != h.ownerID {
		t.Fatalf("the data endpoint works on the owner's space: %v", owner)
	}

	denied := h.do("POST", "/api/v1/keys", map[string]any{"name": "nope"}, memberToken, h.ownerID)
	if denied.status != http.StatusForbidden || denied.code(t) != "PERMISSION_DENIED" {
		t.Fatalf("an ungranted right: %d %s", denied.status, denied.body)
	}
	equalStrings(t, "denied details", orderedKeys(t, nested(t, nested(t, denied.body, "error"), "details")),
		[]string{"permission"})
	if permission := path(t, denied.object(t), "error", "details", "permission"); permission != "keys.create" {
		t.Fatalf("the refusal names the permission: %v", permission)
	}
	if message := path(t, denied.object(t), "error", "message"); message != "The team owner has not allowed this for you." {
		t.Fatalf("PERMISSION_DENIED message: %v", message)
	}

	providers := h.do("GET", "/api/v1/providers/accounts", nil, memberToken, h.ownerID)
	if providers.status != http.StatusForbidden {
		t.Fatalf("providers.view was not granted: %d %s", providers.status, providers.body)
	}

	teamPage := h.do("GET", "/api/v1/teams", nil, memberToken, h.ownerID)
	if teamPage.status != http.StatusForbidden || teamPage.code(t) != "OWN_SPACE_ONLY" {
		t.Fatalf("the team page is own-space only: %d %s", teamPage.status, teamPage.body)
	}
	if message := path(t, teamPage.object(t), "error", "message"); message != "Switch to your own space to do this." {
		t.Fatalf("OWN_SPACE_ONLY message: %v", message)
	}

	own := h.do("GET", "/api/v1/keys", nil, memberToken, "")
	if own.status != http.StatusOK {
		t.Fatalf("the member's own space: %d %s", own.status, own.body)
	}
	if owner := path(t, own.object(t), "owner"); owner != memberID {
		t.Fatalf("without the header the space is the caller's own: %v", owner)
	}
}

// TestUnknownSpaceLooksMissing ports the rule that a space the caller is not in
// looks like it does not exist.
func TestUnknownSpaceLooksMissing(t *testing.T) {
	h := newHarness(t)
	outsiderToken, _ := h.register("outsider")

	response := h.do("GET", "/api/v1/keys", nil, outsiderToken, h.ownerID)
	if response.status != http.StatusNotFound || response.code(t) != "SPACE_NOT_FOUND" {
		t.Fatalf("a foreign space: %d %s", response.status, response.body)
	}
	if message := path(t, response.object(t), "error", "message"); message != "This space does not exist." {
		t.Fatalf("SPACE_NOT_FOUND message: %v", message)
	}
}

// TestReleaseAndLeaveEndAccess ports the end of a membership by both sides.
func TestReleaseAndLeaveEndAccess(t *testing.T) {
	h := newHarness(t)
	memberToken, memberID := h.register("member")
	outsiderToken, _ := h.register("outsider")

	link := h.invite(map[string]any{"permissions": []string{"activity.view"}})
	if redeemed := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": link}, memberToken, ""); redeemed.status != http.StatusOK {
		t.Fatalf("redeem: %d %s", redeemed.status, redeemed.body)
	}

	released := h.do("DELETE", "/api/v1/teams/members/"+memberID, nil, h.ownerToken, "")
	if released.status != http.StatusOK {
		t.Fatalf("release: %d %s", released.status, released.body)
	}
	if members := path(t, released.object(t), "members").([]any); len(members) != 0 {
		t.Fatalf("the team should be empty: %s", released.body)
	}
	gone := h.do("GET", "/api/v1/keys", nil, memberToken, h.ownerID)
	if gone.status != http.StatusNotFound || gone.code(t) != "SPACE_NOT_FOUND" {
		t.Fatalf("a released member: %d %s", gone.status, gone.body)
	}
	unknown := h.do("DELETE", "/api/v1/teams/members/"+memberID, nil, h.ownerToken, "")
	if unknown.status != http.StatusNotFound || unknown.code(t) != "MEMBER_NOT_FOUND" {
		t.Fatalf("releasing twice: %d %s", unknown.status, unknown.body)
	}

	second := h.invite(map[string]any{"permissions": []string{"activity.view"}})
	if redeemed := h.do("POST", "/api/v1/teams/invites/redeem", map[string]any{"link": second}, outsiderToken, ""); redeemed.status != http.StatusOK {
		t.Fatalf("outsider redeem: %d %s", redeemed.status, redeemed.body)
	}
	left := h.do("POST", "/api/v1/teams/leave", nil, outsiderToken, "")
	if left.status != http.StatusOK {
		t.Fatalf("leave: %d %s", left.status, left.body)
	}
	if memberOf := path(t, left.object(t), "memberOf"); memberOf != nil {
		t.Fatalf("after leaving: %s", left.body)
	}
	notAMember := h.do("POST", "/api/v1/teams/leave", nil, outsiderToken, "")
	if notAMember.status != http.StatusNotFound || notAMember.code(t) != "NOT_A_MEMBER" {
		t.Fatalf("leaving twice: %d %s", notAMember.status, notAMember.body)
	}

	events, err := h.teams.AuditList(t.Context(), 100)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		seen[event.Action] = true
	}
	for _, action := range []string{"invite.create", "invite.revoke", "member.join", "member.release", "member.leave"} {
		if action == "invite.revoke" {
			continue // that test revokes one; this one creates and releases
		}
		if !seen[action] {
			t.Fatalf("expected a %s event in %v", action, seen)
		}
	}
}

// TestSpaceMiddlewareOwnSpace covers the header rules of the space resolution.
func TestSpaceMiddlewareOwnSpace(t *testing.T) {
	h := newHarness(t)
	memberToken, memberID := h.register("member")

	for _, space := range []string{"", memberID} {
		response := h.do("GET", "/api/v1/keys", nil, memberToken, space)
		if response.status != http.StatusOK {
			t.Fatalf("space %q: %d %s", space, response.status, response.body)
		}
		if owner := path(t, response.object(t), "owner"); owner != memberID {
			t.Fatalf("space %q resolves to the caller's own space, got %v", space, owner)
		}
		if actor := path(t, response.object(t), "actor"); actor != memberID {
			t.Fatalf("the actor is always the caller: %v", actor)
		}
	}

	anonymous := h.do("GET", "/api/v1/keys", nil, "", "")
	if anonymous.status != http.StatusUnauthorized || anonymous.code(t) != "AUTHENTICATION_REQUIRED" {
		t.Fatalf("no session: %d %s", anonymous.status, anonymous.body)
	}
}
