package routing

import (
	"errors"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Ported from test/unit/routing_groups_cache.test.js: the routing assertions.
// The Anthropic translator round-trip in that file belongs to the adapters
// domain (spec 10b), and the usage/budget pricing cases to specs 05.

// groupsFixture creates two paid OpenAI-like connections, a and b, each with
// one model "m", and returns a plan builder like the Node test's makePlan.
func groupsFixture(t *testing.T) (*fixture, func(model string, extra ...func(*PlanOptions)) *RoutingPlan) {
	f := newFixture(t)
	f.account("a", "u", "openai", 0, "paid")
	f.account("b", "u", "openai", 0, "paid")
	for _, id := range []string{"a", "b"} {
		f.entry("m", id, func(e *Entry) {
			e.DefinitionID = "openai"
			e.BillingMode = "paid"
		})
	}
	key := RouterKey{ID: "k", UserID: "u", Name: "test", Mode: "both", BudgetPeriod: "total", AllowedModelsJSON: "[]", Profile: "balanced"}
	makePlan := func(model string, extra ...func(*PlanOptions)) *RoutingPlan {
		opts := PlanOptions{
			UserID:         "u",
			Key:            key,
			Request:        planRequest("hello"),
			Needs:          Needs{},
			InputTokens:    1000,
			RequestedModel: model,
			Random:         func() float64 { return 0 },
		}
		for _, tweak := range extra {
			tweak(&opts)
		}
		return mustPlan(t, f.svc, opts)
	}
	return f, makePlan
}

func TestGroupsIsolateMembersFallBackAndRejectCycles(t *testing.T) {
	f, makePlan := groupsFixture(t)
	allow := true
	fallback := mustSaveGroup(t, f, GroupInput{Name: "fallback", Accounts: []string{"b"}, AllowMetered: &allow})
	primary := mustSaveGroup(t, f, GroupInput{Name: "primary", Accounts: []string{"a"}, Fallback: strPtr("fallback"), AllowMetered: &allow})

	plan := makePlan("pool:primary")
	if choice := plan.Next(NextOptions{}); choice == nil || choice.Entry.AccountID != "a" {
		t.Fatalf("first choice = %v, want account a", choice)
	}
	if choice := plan.Next(NextOptions{}); choice == nil || choice.Entry.AccountID != "b" {
		t.Fatalf("second choice = %v, want account b", choice)
	}
	if plan.Group == nil || plan.Group.Name != "fallback" {
		t.Fatalf("group = %v, want fallback", plan.Group)
	}
	if choice := plan.Next(NextOptions{}); choice != nil {
		t.Fatalf("third choice = %v, want none", choice.Entry.ID)
	}

	f.limits.set("a", StatedLimits{Concurrent: 1})
	f.limits.set("b", StatedLimits{Concurrent: 1})
	var entryA, entryB *Entry
	for _, entry := range plan.Entries {
		if entry.AccountID == "a" {
			entryA = entry
		}
		if entry.AccountID == "b" {
			entryB = entry
		}
	}
	releaseA := f.svc.Lease(entryA)
	releaseB := f.svc.Lease(entryB)
	waiting := makePlan("pool:primary")
	now := time.Now()
	if choice := waiting.Next(NextOptions{Now: now}); choice != nil {
		t.Fatalf("a full set of connections still offered %s", choice.Entry.ID)
	}
	if at := waiting.FreesUpAt(now, nil); at == nil || *at <= now.UnixMilli() {
		t.Fatalf("freesUpAt = %v, want a future time", at)
	}
	releaseA()
	if choice := waiting.Next(NextOptions{Now: now}); choice == nil || choice.Entry.AccountID != "a" {
		t.Fatalf("primary capacity is reconsidered after the fallback was full: %v", choice)
	}
	releaseB()
	f.limits.set("a", StatedLimits{})
	f.limits.set("b", StatedLimits{})

	// Fallback groups must not form a cycle.
	if _, err := f.svc.SaveGroup("u", GroupInput{Name: "fallback", Accounts: []string{"b"}, Fallback: strPtr("primary")}, strPtr(fallback.ID)); err == nil || httpCode(err) != 400 {
		t.Fatalf("cycle = %v, want 400 GROUP_CYCLE", err)
	}
	// A connection of another space is refused.
	if _, err := f.svc.SaveGroup("v", GroupInput{Name: "other", Accounts: []string{"a"}}, nil); err == nil || httpCode(err) != 400 {
		t.Fatalf("cross-space account = %v, want 400 UNKNOWN_ACCOUNT", err)
	}
	if got := f.svc.ResolveGroup("v", primary.Name); got != nil {
		t.Fatalf("group of another space resolved: %v", got)
	}

	limited := RouterKey{ID: "k", UserID: "u", Mode: "both", AllowedModelsJSON: mustJSON([]string{"pool:primary", "a/*"}), Profile: "balanced"}
	if !f.svc.RouteAllowedByKey("pool:primary", limited) {
		t.Fatalf("pool:primary should be allowed")
	}
	if f.svc.RouteAllowedByKey("pool:fallback", limited) {
		t.Fatalf("pool:fallback should not be allowed")
	}
	restricted := makePlan("pool:primary", func(opts *PlanOptions) { opts.Key = limited })
	if choice := restricted.Next(NextOptions{}); choice == nil || choice.Entry.AccountID != "a" {
		t.Fatalf("restricted first choice = %v", choice)
	}
	if choice := restricted.Next(NextOptions{}); choice != nil {
		t.Fatalf("fallback never widens allowed models: %v", choice.Entry.ID)
	}
	byKey := makePlan("auto", func(opts *PlanOptions) {
		opts.Key.RoutingGroup = primary.Name
	})
	if choice := byKey.Next(NextOptions{}); choice == nil || choice.Entry.AccountID != "a" {
		t.Fatalf("key routing group = %v", choice)
	}
	byGroup := makePlan("auto", func(opts *PlanOptions) { opts.RoutingGroup = "fallback" })
	if choice := byGroup.Next(NextOptions{}); choice == nil || choice.Entry.AccountID != "b" {
		t.Fatalf("explicit routing group = %v", choice)
	}
	if !ParseModel("pool:primary").Auto {
		t.Fatalf("pool:primary is not auto")
	}

	// A group with nothing in the catalog can still be rescued by last resort.
	outside := true
	metered := true
	unavailable := mustSaveGroup(t, f, GroupInput{Name: "unavailable", Models: []string{"absent/*"}, AllowMetered: &metered, AllowOutside: &outside})
	rescue := makePlan("pool:" + unavailable.Name)
	if choice := rescue.Next(NextOptions{}); choice != nil {
		t.Fatalf("unavailable group offered %s", choice.Entry.ID)
	}
	if !rescue.EnableLastResort(time.Now(), nil) {
		t.Fatalf("last resort was not enabled")
	}
	if choice := rescue.Next(NextOptions{}); choice == nil {
		t.Fatalf("a last-resort pass can use other models without bypassing key policy")
	}
}

func TestCacheUsageIsLearnedWithoutRetainingConversationText(t *testing.T) {
	f, makePlan := groupsFixture(t)
	allow := true
	mustSaveGroup(t, f, GroupInput{Name: "fallback", Accounts: []string{"b"}, AllowMetered: &allow})
	mustSaveGroup(t, f, GroupInput{Name: "primary", Accounts: []string{"a"}, Fallback: strPtr("fallback"), AllowMetered: &allow})

	now := time.Now()
	plan := makePlan("pool:primary")
	entry := plan.Next(NextOptions{Now: now}).Entry
	f.svc.RememberAffinity("k", "conversation", entry, now)
	ref := f.svc.AffinityRef("k", "conversation", now)
	if ref == nil || *ref != entry.Ref {
		t.Fatalf("affinity = %v, want %d", ref, entry.Ref)
	}
	if ref := f.svc.AffinityRef("other-key", "conversation", now); ref != nil {
		t.Fatalf("affinity leaked across keys: %v", *ref)
	}
	if bonus := f.svc.CacheBonus(entry, 0, 10000); bonus != 0 {
		t.Fatalf("a zero hit rate earned a bonus of %v", bonus)
	}
	ranked := makePlan("pool:primary", func(opts *PlanOptions) { opts.SessionID = "conversation" }).Rank(RankOptions{Sample: false, Now: now})
	if ranked[0].Details.Factors.CacheAffinity == nil || *ranked[0].Details.Factors.CacheAffinity != 0 {
		t.Fatalf("without measured cache rates the factor is 0: %v", ranked[0].Details.Factors.CacheAffinity)
	}
	if _, err := f.db.W.Exec(`INSERT INTO requests (id, user_id, created_at, api_format, requested_model, model_ref, model_label, account_id, status, http_status, input_tokens, cached_input_tokens, output_tokens, cost_usd, is_free, latency_ms, attempts, stream, trail_json)
		VALUES ('r', 'u', ?, 'openai', 'pool:primary', ?, ?, ?, 'success', 200, 100, 80, 5, 0, 0, 100, 1, 0, '[]')`, now.UnixMilli(), entry.Ref, entry.ID, entry.AccountID); err != nil {
		t.Fatalf("insert request: %v", err)
	}
	ranked = makePlan("pool:primary", func(opts *PlanOptions) { opts.SessionID = "conversation" }).Rank(RankOptions{Sample: false, Now: now})
	bonus := ranked[0].Details.Factors.CacheAffinity
	if bonus == nil || *bonus <= 0 {
		t.Fatalf("with a measured cache rate the affinity earns a bonus: %v", bonus)
	}
}

func TestFullPaidSlotsWithNothingFreeFallThroughToTheFallbackGroup(t *testing.T) {
	f := newFixture(t)
	f.account("flat", "u", "openai", 3, "paid")
	f.account("spare", "u", "openai", 0, "paid")
	for _, id := range []string{"flat", "spare"} {
		f.entry("m", id, func(e *Entry) { e.DefinitionID = "openai"; e.BillingMode = "paid" })
	}
	allow := true
	mustSaveGroup(t, f, GroupInput{Name: "spill", Accounts: []string{"spare"}, AllowMetered: &allow})
	mustSaveGroup(t, f, GroupInput{Name: "slots", Accounts: []string{"flat"}, Fallback: strPtr("spill"), AllowMetered: &allow})
	f.limits.set("flat", StatedLimits{Concurrent: 1})
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hello"), RequestedModel: "pool:slots",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: time.Now(),
	})
	var flatEntry *Entry
	for _, entry := range plan.Entries {
		if entry.AccountID == "flat" {
			entry.IsFlatRate = true
			entry.OverflowPolicy = "auto"
			flatEntry = entry
		}
	}
	release := f.svc.Lease(flatEntry)
	defer func() {
		release()
		f.limits.set("flat", StatedLimits{})
	}()
	choice := plan.Next(NextOptions{Now: time.Now()})
	if choice == nil || choice.Entry.AccountID != "spare" {
		t.Fatalf("choice = %v, want the fallback group's connection", choice)
	}
}

func TestASmartAliasSpillsAtTheAccountLimitAndKeepsTheRouteWarm(t *testing.T) {
	f, makePlan := groupsFixture(t)
	allow := true
	mustSaveGroup(t, f, GroupInput{Name: "work", Accounts: []string{"a", "b"}, PreferredAccount: strPtr("a"), AllowMetered: &allow})
	now := time.Now()
	first := makePlan("pool:work").Next(NextOptions{Now: now}).Entry
	if first.AccountID != "a" {
		t.Fatalf("new conversations start on the preferred account: %s", first.AccountID)
	}
	f.limits.set("a", StatedLimits{Concurrent: 1})
	release := f.svc.Lease(first)
	burstPlan := makePlan("pool:work")
	for _, entry := range burstPlan.Entries {
		if entry.AccountID != "a" && entry.AccountID != "b" {
			continue
		}
		entry.IsPlan = true
		if entry.AccountID == "a" {
			entry.IsFlatRate = true
			entry.OverflowPolicy = "auto"
		}
	}
	overflow := burstPlan.Next(NextOptions{Now: now}).Entry
	if overflow.AccountID != "b" {
		t.Fatalf("overflow went to %s, want b", overflow.AccountID)
	}
	if overflow.RoutingReason != "preferred_busy" {
		t.Fatalf("routing reason = %q, want preferred_busy", overflow.RoutingReason)
	}
	f.svc.RememberAffinity(burstPlan.AffinityScope, "overflow-session", overflow, now)
	release()
	f.limits.set("a", StatedLimits{})
	if choice := makePlan("pool:work", func(opts *PlanOptions) { opts.SessionID = "overflow-session" }).Next(NextOptions{Now: now}); choice == nil || choice.Entry.AccountID != "b" {
		t.Fatalf("an active conversation keeps its endpoint: %v", choice)
	}
	if choice := makePlan("pool:work").Next(NextOptions{Now: now}); choice == nil || choice.Entry.AccountID != "a" {
		t.Fatalf("a new conversation goes back to the preferred account: %v", choice)
	}

	// New groups exclude metered (paid) models by default.
	strict := mustSaveGroup(t, f, GroupInput{Name: "strict", Accounts: []string{"a"}})
	plan := makePlan("pool:strict")
	if choice := plan.Next(NextOptions{Now: now}); choice != nil {
		t.Fatalf("new groups exclude metered models by default: %s", choice.Entry.ID)
	}
	if plan.EnableLastResort(now, nil) {
		t.Fatalf("a last resort cannot escape the selected connections")
	}
	mustSaveGroup(t, f, GroupInput{Name: "strict", Accounts: []string{"a"}, AllowMetered: &allow}, strPtr(strict.ID))
	limited := makePlan("pool:strict")
	if choice := limited.Next(NextOptions{Now: now}); choice == nil || choice.Entry.AccountID != "a" {
		t.Fatalf("an allowed metered group = %v", choice)
	}
	if choice := limited.Next(NextOptions{Now: now}); choice != nil {
		t.Fatalf("second choice = %s, want none", choice.Entry.ID)
	}
	if limited.EnableLastResort(now, nil) {
		t.Fatalf("a last resort cannot escape the selected connections")
	}
}

func TestAnExplicitRoutingStyleTakesPriorityOverThePreferredAccount(t *testing.T) {
	f, makePlan := groupsFixture(t)
	allow := true
	kinds := KindWeights{Free: 0.5, Plan: 1, Paid: 0.5}
	style, err := f.svc.CreateProfile("u", ProfileInput{
		Name:        strPtr("Subscriptions first"),
		Weights:     &Presets[0].Weights,
		Constraints: &Constraints{Adaptive: false, Exploration: 0.08, Kinds: &kinds, IncludeProviders: []string{}, ExcludeProviders: []string{}, ExcludeModels: []string{}},
	}, "u")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	mustSaveGroup(t, f, GroupInput{Name: "styled-work", Accounts: []string{"a", "b"}, PreferredAccount: strPtr("a"), Profile: &style.ID, AllowMetered: &allow})
	plan := makePlan("pool:styled-work")
	for _, entry := range plan.Entries {
		if entry.AccountID == "b" {
			entry.IsPlan = true
		}
	}
	choice := plan.Next(NextOptions{Now: time.Now()})
	if choice == nil || choice.Entry.AccountID != "b" {
		t.Fatalf("an explicit style takes priority over the preferred account: %v (profile=%+v kinds=%v candidates=%d)", choice, plan.Profile, plan.Profile.Constraints.Kinds, len(plan.Entries))
	}
}

func TestASubagentGroupUsesItsAvailableSubscriptionBeforeWaiting(t *testing.T) {
	f, makePlan := groupsFixture(t)
	mustSaveGroup(t, f, GroupInput{Name: "helpers", Accounts: []string{"a", "b"}})
	mustSaveGroup(t, f, GroupInput{Name: "parent", Accounts: []string{"a"}, SubagentGroup: strPtr("helpers")})
	f.limits.set("a", StatedLimits{Concurrent: 4})
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
		f.limits.set("a", StatedLimits{})
	}()
	counts := map[string]int{"a": 0, "b": 0}
	now := time.Now()
	for i := 0; i < 10; i++ {
		var plan *RoutingPlan
		if i%2 == 0 {
			plan = makePlan("pool:parent", func(opts *PlanOptions) { opts.Workload = "subagent" })
		} else {
			plan = makePlan("pool:helpers")
		}
		for _, entry := range plan.Entries {
			if entry.AccountID != "a" && entry.AccountID != "b" {
				continue
			}
			entry.IsPlan = true
			if entry.AccountID == "a" {
				entry.IsFlatRate = true
				entry.OverflowPolicy = "auto"
			}
		}
		choice := plan.Next(NextOptions{Now: now})
		if choice == nil {
			t.Fatalf("round %d: no choice", i)
		}
		if choice.Entry.RoutingGroup != "helpers" {
			t.Fatalf("round %d: routing group = %q, want helpers", i, choice.Entry.RoutingGroup)
		}
		counts[choice.Entry.AccountID]++
		releases = append(releases, f.svc.Lease(choice.Entry))
	}
	if counts["a"] != 4 || counts["b"] != 6 {
		t.Fatalf("counts = %v, want a 4, b 6", counts)
	}
}

// --- helpers ---------------------------------------------------------------

func strPtr(value string) *string { return &value }

func mustSaveGroup(t *testing.T, f *fixture, input GroupInput, ids ...*string) *Group {
	var id *string
	if len(ids) > 0 {
		id = ids[0]
	}
	t.Helper()
	group, err := f.svc.SaveGroup("u", input, id)
	if err != nil {
		t.Fatalf("save group %s: %v", input.Name, err)
	}
	return group
}

func httpCode(err error) int {
	var target *httpserver.HttpError
	if errors.As(err, &target) {
		return target.Status
	}
	return 0
}
