package gateway

// The model list and the per-key endpoints beside it
// (server/capabilities/chat/gateway_routes.js).

import (
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// kindLabels are the words an auto variant's name uses.
var kindLabels = map[string]string{"free": "free models", "plan": "subscriptions", "paid": "paid models"}

// modelsVisibleTo is modelsVisibleTo: the models of one kind this key may use. A
// pool partner sees only what this pool is offered.
func (s *Service) modelsVisibleTo(key *Key, capability string) []*routing.Entry {
	if capability == "" {
		capability = "chat"
	}
	if key.IsPeer() && s.opts.SharingWith != nil && !s.opts.SharingWith(key) {
		return nil
	}
	if s.opts.Catalog == nil {
		return nil
	}
	method := MethodFor(capability)
	now := time.Now()
	servable := map[string]bool{}
	canServe := func(entry *routing.Entry) bool {
		if method == "" || s.opts.Adapters == nil {
			return true
		}
		cached, known := servable[entry.AccountID]
		if !known {
			cached = s.opts.Adapters.CanServe(entry.AccountID, method)
			servable[entry.AccountID] = cached
		}
		return cached
	}
	out := make([]*routing.Entry, 0, 8)
	for _, entry := range s.opts.Catalog.ListForUser(key.UserID(), routing.CatalogQuery{Capability: capability}) {
		if !canServe(entry) {
			continue
		}
		if !entry.Enabled || !entry.AccountEnabled || !s.opts.Routing.AllowedByKey(entry, key.Routing, now) {
			continue
		}
		if !key.IsPeer() && routing.KindAllowed(entry, key.Routing, false) != "" {
			continue
		}
		if key.IsPeer() {
			if s.opts.PoolOffers != nil && !s.opts.PoolOffers(key, entry) {
				continue
			}
		} else if s.isPeerEntry(entry) && key.Row.PoolAccess != "pool" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// modelsHiddenFrom is modelsHiddenFrom: models of this kind the key's free/plan/
// paid choice leaves out, so an app can tell "nothing there" from "not allowed
// for this key".
func (s *Service) modelsHiddenFrom(key *Key, capability string) *Obj {
	hidden := map[string]int{"free": 0, "plan": 0, "paid": 0}
	decisions := map[string]int{"free": 0, "plan": 0, "paid": 0}
	if s.opts.Catalog != nil {
		now := time.Now()
		for _, entry := range s.opts.Catalog.ListForUser(key.UserID(), routing.CatalogQuery{Capability: capability}) {
			if !entry.Enabled || !entry.AccountEnabled || !s.opts.Routing.AllowedByKey(entry, key.Routing, now) {
				continue
			}
			if routing.KindAllowed(entry, key.Routing, false) == "" {
				continue
			}
			kind := routing.ModelKind(entry)
			hidden[kind]++
			if entry.Decision {
				decisions[kind]++
			}
		}
	}
	out := newObjOf("free", float64(hidden["free"]), "plan", float64(hidden["plan"]), "paid", float64(hidden["paid"]))
	if capability == "moderation" {
		out.Set("decision_models", newObjOf("free", float64(decisions["free"]), "plan", float64(decisions["plan"]), "paid", float64(decisions["paid"])))
	}
	return out
}

// liveProtocols is liveProtocols: the live dialects the entry's provider speaks.
func (s *Service) liveProtocols(entry *routing.Entry) []any {
	if s.opts.Adapters == nil {
		return []any{}
	}
	protocols := s.opts.Adapters.LiveProtocols(entry.AccountID)
	out := make([]any, 0, len(protocols))
	seen := map[string]bool{}
	for _, protocol := range protocols {
		if seen[protocol] {
			continue
		}
		seen[protocol] = true
		out = append(out, protocol)
	}
	return out
}

// styleKinds is styleKinds: the kinds a style routes over — those it gives any
// weight, of those in reach.
func styleKinds(profile *routing.Profile, kinds []string) []any {
	out := make([]any, 0, len(kinds))
	if profile == nil {
		for _, kind := range kinds {
			out = append(out, kind)
		}
		return out
	}
	weights := profile.Constraints.Kinds
	for _, kind := range kinds {
		if weights != nil {
			weight := weights.Paid
			switch kind {
			case "free":
				weight = weights.Free
			case "plan":
				weight = weights.Plan
			}
			if weight <= 0 {
				continue
			}
		}
		out = append(out, kind)
	}
	return out
}

// autoModels is autoModels: "auto" and one entry per routing profile, over every
// kind of model the key may use; with more than one kind in reach, the same again
// limited to each kind ("auto-free:fast", "auto-plan", "auto-paid:best").
func (s *Service) autoModels(key *Key, entries []*routing.Entry, capability string) []*Obj {
	kinds := make([]string, 0, len(routing.ModelKinds))
	for _, kind := range routing.ModelKinds {
		for _, entry := range entries {
			if routing.ModelKind(entry) == kind {
				kinds = append(kinds, kind)
				break
			}
		}
	}
	scopes := make([]string, 0, len(kinds)+1)
	scopes = append(scopes, "")
	if len(kinds) > 1 {
		scopes = append(scopes, kinds...)
	}
	list := make([]*Obj, 0, len(scopes)*(len(routing.Presets)+4))
	userID := key.UserID()
	for _, scope := range scopes {
		inScope := entries
		if scope != "" {
			inScope = make([]*routing.Entry, 0, len(entries))
			for _, entry := range entries {
				if routing.ModelKind(entry) == scope {
					inScope = append(inScope, entry)
				}
			}
		}
		extra := newObj()
		switch capability {
		case "live":
			protocols := make([]any, 0, 2)
			seen := map[string]bool{}
			for _, entry := range inScope {
				for _, protocol := range s.liveProtocols(entry) {
					name := protocol.(string)
					if seen[name] {
						continue
					}
					seen[name] = true
					protocols = append(protocols, name)
				}
			}
			extra.Set("protocols", protocols)
		case "moderation":
			decision := false
			for _, entry := range inScope {
				if entry.Decision {
					decision = true
					break
				}
			}
			extra.Set("decision", decision)
		}
		base := "auto"
		only := ""
		if scope != "" {
			base = "auto-" + scope
			only = ", " + kindLabels[scope] + " only"
		}
		// Plain auto follows the key's own profile; every profile is also listed
		// by name. An own style already says which kinds of model it uses, so it
		// gets no free/plan/paid variants (nor does plain auto when it follows
		// one).
		keyProfile := s.opts.Routing.ResolveProfile(userID, key.Row.Profile)
		if keyProfile == nil {
			keyProfile = s.opts.Routing.ResolveProfile(userID, "balanced")
		}
		if scope == "" || (keyProfile != nil && keyProfile.Preset) {
			neorouter := newObjOf(
				"auto", true,
				"profile", profileID(keyProfile),
				"keyDefault", true,
				"kinds", scopeValues(scope, keyProfile, kinds),
			)
			setAll(neorouter, extra)
			name := "Auto (key default: " + profileName(keyProfile) + only + ")"
			list = append(list, newObjOf("id", base, "name", name, "neorouter", neorouter))
		}
		for _, profile := range s.opts.Routing.Profiles(userID) {
			if scope != "" && !profile.Preset {
				continue
			}
			neorouter := newObjOf(
				"auto", true,
				"profile", profile.ID,
				"kinds", scopeValues(scope, profile, kinds),
			)
			setAll(neorouter, extra)
			list = append(list, newObjOf("id", base+":"+profile.Slug, "name", "Auto ("+profile.Name+only+")", "neorouter", neorouter))
		}
	}
	return list
}

// scopeValues is the kinds a variant routes over: its single kind, or the
// style's own kinds.
func scopeValues(scope string, profile *routing.Profile, kinds []string) []any {
	if scope != "" {
		return []any{scope}
	}
	return styleKinds(profile, kinds)
}

// profileID is a profile's id, "" when there is none.
func profileID(profile *routing.Profile) any {
	if profile == nil {
		return nil
	}
	return profile.ID
}

// profileName is a profile's name, "balanced" when there is none.
func profileName(profile *routing.Profile) string {
	if profile == nil {
		return "balanced"
	}
	return profile.Name
}

// listedName is listedName: the same model is offered by several providers, so a
// picker that only shows the name cannot tell the copies apart.
func (s *Service) listedName(entry *routing.Entry) string {
	provider := entry.ProviderName
	if provider == "" {
		provider = entry.ProviderSlug
	}
	if s.isPeerEntry(entry) && provider != "" {
		if strings.HasSuffix(entry.Name, ")") {
			return strings.TrimSuffix(entry.Name, ")") + ", via " + provider + ")"
		}
		return entry.Name + " (via " + provider + ")"
	}
	if provider != "" && !strings.Contains(strings.ToLower(entry.Name), strings.ToLower(provider)) {
		return entry.Name + " (" + provider + ")"
	}
	return entry.Name
}

// priceString is String(price / 1e6): USD per token.
func priceString(price *float64) any {
	if price == nil {
		return nil
	}
	return jsString(*price / 1e6)
}

// optionalInt is a number that is null when unknown.
func optionalInt(value int) any {
	if value == 0 {
		return nil
	}
	return float64(value)
}

// optionalFloat is a pointer value as JSON (null when unknown).
func optionalFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

// optionalBool is a pointer value as JSON (null when unknown).
func optionalBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

// modelJSON is one model of the OpenAI-shaped list.
func (s *Service) modelJSON(key *Key, entry *routing.Entry, capability string, created int64) *Obj {
	pricing := newObjOf("prompt", "0", "completion", "0")
	if !entry.IsFree && !entry.IsPlan && !key.IsPeer() {
		pricing = newObjOf("prompt", priceString(entry.PriceIn), "completion", priceString(entry.PriceOut))
	}
	neorouter := newObjOf(
		"kind", capability,
		"free", entry.IsFree || key.IsPeer(),
		"plan", entry.IsPlan,
		"intelligence", optionalFloat(entry.Intelligence),
		"tools", optionalBool(entry.Tools),
		"vision", optionalBool(entry.Vision),
	)
	if entry.PriceUnit != "" {
		neorouter.Set("unit_price", optionalFloat(entry.UnitPrice))
		neorouter.Set("price_unit", entry.PriceUnit)
	}
	if capability == "live" {
		neorouter.Set("protocols", s.liveProtocols(entry))
	}
	if capability == "moderation" {
		neorouter.Set("decision", entry.Decision)
	}
	model := newObjOf(
		"id", entry.ID,
		"object", "model",
		"created", created,
		"owned_by", entry.ProviderSlug,
		"name", s.listedName(entry),
		"context_length", optionalInt(entry.ContextWindow),
	)
	if entry.MaxOutput != 0 {
		model.Set("max_output_tokens", float64(entry.MaxOutput))
	}
	if entry.Tools != nil {
		model.Set("supports_tools", *entry.Tools)
	}
	if entry.Vision != nil {
		model.Set("supports_vision", *entry.Vision)
	}
	model.Set("pricing", pricing)
	model.Set("neorouter", neorouter)
	return model
}

// groupContextLength is groupContextLength: a routing group is a union of
// models, so what a client may safely send is the smallest window in the group,
// because any member may be the one that answers. Members that publish no window
// are held to routing.UnknownContextLimit, so they cap the group too. The group
// itself is not a catalog entry and has no limit of its own, which is why it used
// to be listed with none at all. A group with no reachable member has no window
// to report (nil, so the key is left out of the listing altogether).
func (s *Service) groupContextLength(group *routing.Group, entries []*routing.Entry) any {
	smallest := 0
	members := 0
	for _, entry := range entries {
		if !s.opts.Routing.Contains(group, entry) {
			continue
		}
		window := routing.UnknownContextLimit
		if entry.ContextWindow > 0 {
			window = entry.ContextWindow
		}
		if members == 0 || window < smallest {
			smallest = window
		}
		members++
	}
	if members == 0 {
		return nil
	}
	return float64(smallest)
}

// poolModel is one `pool:<group>` entry of the auto list: the group's name, the
// smallest window its members offer (when it has one) and the routing group.
func (s *Service) poolModel(group *routing.Group, entries []*routing.Entry) *Obj {
	model := newObjOf("id", "pool:"+group.Name, "name", group.Name)
	if contextLength := s.groupContextLength(group, entries); contextLength != nil {
		model.Set("context_length", contextLength)
	}
	model.Set("neorouter", newObjOf("routingGroup", group.Name))
	return model
}

// handleModels is GET /v1/models: the models this key may use, autos first.
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request, key *Key) {
	capability := "chat"
	if requested := r.URL.Query().Get("type"); IsKind(requested) {
		capability = requested
	}
	reachable := s.modelsVisibleTo(key, capability)
	created := nowSeconds()
	// Only the autos the key may ask for, and models only when it may name them.
	// A partner's router picks models itself, so it gets no autos.
	autos := make([]*Obj, 0, 8)
	if !key.IsPeer() {
		for _, auto := range s.autoModels(key, reachable, capability) {
			if s.opts.Routing.RouteAllowedByKey(getString(auto, "id"), key.Routing) {
				autos = append(autos, auto)
			}
		}
		for _, group := range s.opts.Routing.Groups(key.UserID()) {
			if !s.opts.Routing.RouteAllowedByKey("pool:"+group.Name, key.Routing) {
				continue
			}
			autos = append(autos, s.poolModel(group, reachable))
		}
	}
	entries := make([]*routing.Entry, 0, len(reachable))
	for _, entry := range reachable {
		if s.opts.Routing.RouteAllowedByKey(entry.ID, key.Routing) {
			entries = append(entries, entry)
		}
	}
	if r.Header.Get("anthropic-version") != "" {
		createdAt := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		data := make([]any, 0, len(autos)+len(entries))
		for _, auto := range autos {
			model := newObjOf("type", "model", "id", getString(auto, "id"), "display_name", getString(auto, "name"), "created_at", createdAt)
			if contextLength, ok := get(auto, "context_length"); ok {
				model.Set("context_length", contextLength)
			}
			data = append(data, model)
		}
		for _, entry := range entries {
			data = append(data, newObjOf("type", "model", "id", entry.ID, "display_name", s.listedName(entry), "created_at", createdAt))
		}
		var first, last any
		if len(data) > 0 {
			first = getString(asObj(data[0]), "id")
			last = getString(asObj(data[len(data)-1]), "id")
		}
		httpx.WriteJSON(w, http.StatusOK, newObjOf("data", data, "has_more", false, "first_id", first, "last_id", last))
		return
	}
	data := make([]any, 0, len(autos)+len(entries))
	for _, auto := range autos {
		model := newObjOf(
			"id", getString(auto, "id"),
			"object", "model",
			"created", created,
			"owned_by", "neorouter",
			"name", getString(auto, "name"),
		)
		// A group's window comes after its name and before its routing group,
		// as the Node listing sends it.
		if contextLength, ok := get(auto, "context_length"); ok {
			model.Set("context_length", contextLength)
		}
		model.Set("neorouter", getValue(auto, "neorouter"))
		data = append(data, model)
	}
	for _, entry := range entries {
		data = append(data, s.modelJSON(key, entry, capability, created))
	}
	body := newObjOf("object", "list", "data", data)
	if !key.IsPeer() {
		body.Set("neorouter", newObjOf("not_allowed_for_key", s.modelsHiddenFrom(key, capability)))
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

// handleModel is GET /v1/models/{model}: one chat model, or 404.
func (s *Service) handleModel(w http.ResponseWriter, r *http.Request, key *Key) {
	requested := r.PathValue("model")
	for _, entry := range s.modelsVisibleTo(key, "chat") {
		if entry.ID != requested || !s.opts.Routing.RouteAllowedByKey(entry.ID, key.Routing) {
			continue
		}
		httpx.WriteJSON(w, http.StatusOK, newObjOf(
			"id", entry.ID,
			"object", "model",
			"created", nowSeconds(),
			"owned_by", entry.ProviderSlug,
		))
		return
	}
	s.WriteError(w, r, dialectOpenAIChat, key, requested, httpserver.New(http.StatusNotFound, "MODEL_NOT_FOUND", "Model "+requested+" not found."))
}

// limitsResponse is GET /v1/limits: the same object an answer carries.
type limitsResponse struct {
	Object     string                     `json:"object"`
	Requests   *routing.RequestRateBudget `json:"requests"`
	Concurrent routing.ConcurrentBudget   `json:"concurrent"`
}

// handleLimits is GET /v1/limits: how many requests this key may send now, and
// how many at once.
func (s *Service) handleLimits(w http.ResponseWriter, r *http.Request, key *Key) {
	if key.IsPeer() {
		writeGatewayError(w, http.StatusForbidden, "POOL_CHAT_ONLY", "Pool partners cannot read limits.")
		return
	}
	budget := s.requestBudget(key)
	setHeaders(w, routing.BudgetHeaders(budget))
	httpx.WriteJSON(w, http.StatusOK, limitsResponse{Object: "neorouter.limits", Requests: budget.Requests, Concurrent: budget.Concurrent})
}

// handleFeedback is POST /v1/feedback: feedback on an answer ("good" or "bad")
// teaches routing how the model that gave it does on that kind of task.
func (s *Service) handleFeedback(w http.ResponseWriter, r *http.Request, key *Key) {
	body, err := s.readBody(w, r)
	if err != nil {
		return
	}
	requestID := ""
	if value, ok := get(body, "request_id"); ok {
		requestID = jsString(value)
	}
	rating := getValue(body, "rating")
	good := false
	switch value := rating.(type) {
	case string:
		good = value == "good"
		if value != "good" && value != "bad" {
			writeGatewayError(w, http.StatusBadRequest, "INVALID_RATING", "rating must be \"good\" or \"bad\".")
			return
		}
	case float64:
		if value != 1 && value != -1 {
			writeGatewayError(w, http.StatusBadRequest, "INVALID_RATING", "rating must be \"good\" or \"bad\".")
			return
		}
		good = value == 1
	default:
		writeGatewayError(w, http.StatusBadRequest, "INVALID_RATING", "rating must be \"good\" or \"bad\".")
		return
	}
	var answer *Answer
	if s.opts.AnswerForRequest != nil {
		found, lookupErr := s.opts.AnswerForRequest(requestID, key.ID())
		if lookupErr != nil {
			s.log().Error("Failed to read the answered request", "requestId", requestID, "error", lookupErr)
		}
		answer = found
	}
	if answer == nil || answer.ModelRef == 0 {
		writeGatewayError(w, http.StatusNotFound, "REQUEST_NOT_FOUND", "No answer with this request id for this key.")
		return
	}
	taskName := answer.Task
	if taskName == "" {
		taskName = "general"
	}
	if s.opts.RecordFeedback != nil {
		s.opts.RecordFeedback(answer.ModelRef, taskName, good)
	}
	httpx.WriteJSON(w, http.StatusOK, newObjOf("ok", true, "model", answer.ModelLabel, "task", taskName))
}

// handleCountTokens is POST /v1/messages/count_tokens: an estimate is better
// than failing the call.
func (s *Service) handleCountTokens(w http.ResponseWriter, r *http.Request, key *Key) {
	body, err := s.readBody(w, r)
	if err != nil {
		return
	}
	canonical, err := AnthropicTranslator{}.ToCanonical(body)
	if err != nil {
		s.WriteError(w, r, dialectOpenAIChat, key, getString(body, "model"), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newObjOf("input_tokens", float64(EstimateInputTokens(canonical))))
}
