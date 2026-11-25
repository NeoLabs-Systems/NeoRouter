package gateway

// Port of the gateway-owned cases of test/api/auto_variants.test.js: the auto
// variants the model list offers for the kinds of model a key may use.

import (
	"net/http"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// kindsKey creates a key that allows the given kinds of model.
func kindsKey(t *testing.T, f *fixture, free, plan, paid bool, options ...func(*routerkeys.Input)) string {
	t.Helper()
	freeValue, planValue, paidValue := free, plan, paid
	secret, _ := f.key(func(input *routerkeys.Input) {
		input.AllowFree = routerkeys.Field[bool]{Set: true, Value: &freeValue}
		input.AllowPlan = routerkeys.Field[bool]{Set: true, Value: &planValue}
		input.AllowPaid = routerkeys.Field[bool]{Set: true, Value: &paidValue}
		for _, option := range options {
			option(input)
		}
	})
	return secret
}

// modelIDs is the ids of GET /v1/models.
func modelIDs(t *testing.T, f *fixture, secret string) []string {
	t.Helper()
	response := f.do(http.MethodGet, "/v1/models", secret, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("models status = %d: %s", response.Code, response.Body.String())
	}
	ids := make([]string, 0, 8)
	for _, item := range getList(decode(t, response), "data") {
		ids = append(ids, getString(asObj(item), "id"))
	}
	return ids
}

// twoKinds is a free model and a paid one, as the JS oracle's two providers.
func twoKinds(t *testing.T, f *fixture) {
	t.Helper()
	f.mockProvider("free", "free_tier", "ok-free")
	f.mockProvider("paid", "paid", "ok-paid")
	for index := range f.catalog.entries {
		entry := f.catalog.entries[index]
		if entry.ProviderSlug == "paid" {
			entry.IsFree = false
			entry.BillingMode = "paid"
		}
	}
}

func TestFreeAndPaidModelsGetVariantsForEachKind(t *testing.T) {
	f := newFixture(t, nil)
	twoKinds(t, f)
	ids := modelIDs(t, f, kindsKey(t, f, true, true, true))
	list := map[string]bool{}
	for _, id := range ids {
		list[id] = true
	}
	for _, id := range []string{"auto", "auto:fast", "auto-free", "auto-free:fast", "auto-paid", "auto-paid:smart"} {
		if !list[id] {
			t.Errorf("missing %q in %v", id, ids)
		}
	}
	for _, id := range ids {
		if len(id) > len("auto-plan") && id[:len("auto-plan")] == "auto-plan" {
			t.Errorf("subscription variant without subscription models: %q", id)
		}
	}
}

func TestPlainAutoFollowsTheKeyProfile(t *testing.T) {
	f := newFixture(t, nil)
	twoKinds(t, f)
	secret := kindsKey(t, f, true, false, false)
	ids := modelIDs(t, f, secret)
	autos := make([]string, 0, 4)
	for _, id := range ids {
		if len(id) >= 4 && id[:4] == "auto" {
			autos = append(autos, id)
		}
	}
	want := []string{"auto", "auto:balanced", "auto:fast", "auto:smart"}
	if len(autos) != len(want) {
		t.Fatalf("autos = %v", autos)
	}
	for index := range want {
		if autos[index] != want[index] {
			t.Fatalf("autos = %v", autos)
		}
	}
	profile := "fast"
	withProfile := kindsKey(t, f, true, false, false, func(input *routerkeys.Input) {
		input.Profile = routerkeys.Field[string]{Set: true, Value: &profile}
	})
	response := f.do(http.MethodGet, "/v1/models", withProfile, nil, nil)
	body := decode(t, response)
	for _, item := range getList(body, "data") {
		model := asObj(item)
		if getString(model, "id") != "auto" {
			continue
		}
		if got := getString(getObj(model, "neorouter"), "profile"); got != "fast" {
			t.Errorf("auto profile = %q", got)
		}
	}
	// auto:default is plain auto.
	sent := f.chat(withProfile, newObjOf("model", "auto:default", "messages", []any{newObjOf("role", "user", "content", "hi")}))
	if sent.Code != http.StatusOK {
		t.Fatalf("auto:default status = %d: %s", sent.Code, sent.Body.String())
	}
}

func TestSingleKindGetsOnlyPlainAutos(t *testing.T) {
	f := newFixture(t, nil)
	twoKinds(t, f)
	ids := modelIDs(t, f, kindsKey(t, f, true, false, false))
	hasFast := false
	for _, id := range ids {
		if id == "auto:fast" {
			hasFast = true
		}
		if len(id) > 6 && id[:6] == "auto-f" {
			t.Errorf("kind variant for a single kind: %q", id)
		}
	}
	if !hasFast {
		t.Errorf("auto:fast is missing in %v", ids)
	}
}

func TestCustomStyleIsListedWithoutKindVariants(t *testing.T) {
	f := newFixture(t, nil)
	twoKinds(t, f)
	profile, err := f.routing.CreateProfile(f.userID, routing.ProfileInput{
		Name:        stringPtr("Paid first"),
		Weights:     &routing.Weights{Intelligence: 0.4, Speed: 0.2, Reliability: 0.3, Cost: 0.1},
		Constraints: &routing.Constraints{Kinds: &routing.KindWeights{Free: 0, Plan: 0, Paid: 1}, Adaptive: true, Exploration: 0.08},
	}, f.userID)
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	ids := modelIDs(t, f, kindsKey(t, f, true, true, true))
	found := false
	for _, id := range ids {
		if id == "auto:"+profile.Slug {
			found = true
		}
		if id == "auto-free:"+profile.Slug || id == "auto-paid:"+profile.Slug {
			t.Errorf("own style got a kind variant: %q", id)
		}
	}
	if !found {
		t.Fatalf("auto:%s is missing in %v", profile.Slug, ids)
	}
}

func TestRoutingGroupsAreListedAsModels(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha", "ok-beta")
	if _, err := f.routing.SaveGroup(f.userID, routing.GroupInput{
		Name:   "small-work",
		Models: []string{"mock/ok-alpha"},
	}, nil); err != nil {
		t.Fatalf("save group: %v", err)
	}
	ids := modelIDs(t, f, kindsKey(t, f, true, true, false))
	found := false
	for _, id := range ids {
		if id == "pool:small-work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no pool:small-work in %v", ids)
	}
	// The group routes, and the answer names the group.
	response := f.chat(kindsKey(t, f, true, true, false), newObjOf(
		"model", "pool:small-work",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if group := response.Header().Get("X-NeoRouter-Group"); group != "small-work" {
		t.Errorf("X-NeoRouter-Group = %q", group)
	}
	if model := response.Header().Get("X-NeoRouter-Model"); model != "mock/ok-alpha" {
		t.Errorf("X-NeoRouter-Model = %q", model)
	}
}

func TestKindVariantsRouteOnlyToThatKind(t *testing.T) {
	f := newFixture(t, nil)
	twoKinds(t, f)
	secret := kindsKey(t, f, true, true, true)
	for attempt := 0; attempt < 3; attempt++ {
		paid := f.chat(secret, newObjOf("model", "auto-paid:fast", "messages", []any{newObjOf("role", "user", "content", "hi")}))
		if model := paid.Header().Get("X-NeoRouter-Model"); model != "paid/ok-paid" {
			t.Errorf("paid variant answered from %q", model)
		}
		free := f.chat(secret, newObjOf("model", "auto-free", "messages", []any{newObjOf("role", "user", "content", "hi")}))
		if model := free.Header().Get("X-NeoRouter-Model"); model != "free/ok-free" {
			t.Errorf("free variant answered from %q", model)
		}
	}
	// A variant never widens what the key may use.
	freeOnly := kindsKey(t, f, true, false, false)
	response := f.chat(freeOnly, newObjOf("model", "auto-paid", "messages", []any{newObjOf("role", "user", "content", "hi")}))
	if response.Code == http.StatusOK {
		t.Errorf("a paid variant answered on a free-only key: %s", response.Body.String())
	}
}

func TestNothingAllowedExplainsWhy(t *testing.T) {
	f := newFixture(t, nil)
	twoKinds(t, f)
	// Only paid models exist on this key's reachable list if free is refused and
	// no plan/paid model is in reach.
	for index := range f.catalog.entries {
		f.catalog.entries[index].IsFree = false
		f.catalog.entries[index].IsPlan = false
		f.catalog.entries[index].BillingMode = "paid"
	}
	secret := kindsKey(t, f, true, true, false)
	response := f.chat(secret, newObjOf("model", "auto", "messages", []any{newObjOf("role", "user", "content", "hi")}))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	message := getString(getObj(decode(t, response), "error"), "message")
	if message == "" {
		t.Error("no explanation")
	}
}
