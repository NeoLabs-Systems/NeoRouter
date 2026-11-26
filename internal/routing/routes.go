package routing

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// RouteDeps is what the /api/v1/routing routes need from the request context:
// the space owner and the permission check (server/routes/routing.js
// requirePermission('routing.view' | 'routing.edit')).
type RouteDeps struct {
	// Owner is the space owner a request acts for, and the signed-in actor.
	Owner func(r *http.Request) (ownerID, actorID string)
	// Require checks a permission on the request; a non-nil error is written.
	Require func(r *http.Request, permission string) error
}

const (
	permissionView = "routing.view"
	permissionEdit = "routing.edit"
)

// Routes mounts the routing API.
func (s *Service) Routes(mux *http.ServeMux, deps RouteDeps) {
	handle := func(pattern, permission string, handler func(http.ResponseWriter, *http.Request, string, string)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			// An empty permission is a route any signed-in member may read
			// (profile names, since key forms pick from them).
			if deps.Require != nil && permission != "" {
				if err := deps.Require(r, permission); err != nil {
					httpserver.WriteError(w, r, s.log(), err)
					return
				}
			}
			ownerID, actorID := "", ""
			if deps.Owner != nil {
				ownerID, actorID = deps.Owner(r)
			}
			handler(w, r, ownerID, actorID)
		})
	}

	handle("GET /api/v1/routing/groups", permissionView, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		now := time.Now()
		groups := s.GroupStatuses(ownerID, now)
		if groups == nil {
			groups = []*GroupStatus{}
		}
		httpx.WriteJSON(w, http.StatusOK, struct {
			Groups []*GroupStatus `json:"groups"`
		}{Groups: groups})
	})
	handle("POST /api/v1/routing/groups", permissionEdit, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		var input GroupInput
		if err := httpserver.ReadJSONManagement(w, r, &input, httpserver.DefaultJSONBodyLimit); err != nil {
			return
		}
		group, err := s.SaveGroup(ownerID, input, nil)
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, struct {
			Group *Group `json:"group"`
		}{Group: group})
	})
	handle("PUT /api/v1/routing/groups/{id}", permissionEdit, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		var input GroupInput
		if err := httpserver.ReadJSONManagement(w, r, &input, httpserver.DefaultJSONBodyLimit); err != nil {
			return
		}
		id := r.PathValue("id")
		group, err := s.SaveGroup(ownerID, input, &id)
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, struct {
			Group *Group `json:"group"`
		}{Group: group})
	})
	handle("DELETE /api/v1/routing/groups/{id}", permissionEdit, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		if err := s.RemoveGroup(ownerID, r.PathValue("id")); err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Profile names are readable by every member, since key forms pick from
	// them.
	handle("GET /api/v1/routing/profiles", "", func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		httpx.WriteJSON(w, http.StatusOK, struct {
			Profiles []*Profile `json:"profiles"`
		}{Profiles: s.Profiles(ownerID)})
	})
	handle("POST /api/v1/routing/profiles", permissionEdit, func(w http.ResponseWriter, r *http.Request, ownerID, actorID string) {
		var body struct {
			Name        *string         `json:"name"`
			Weights     *Weights        `json:"weights"`
			Constraints json.RawMessage `json:"constraints"`
		}
		if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
			return
		}
		constraints, err := decodeConstraints(body.Constraints)
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		profile, err := s.CreateProfile(ownerID, ProfileInput{Name: body.Name, Weights: body.Weights, Constraints: constraints}, actorID)
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, struct {
			Profile *Profile `json:"profile"`
		}{Profile: profile})
	})
	handle("PATCH /api/v1/routing/profiles/{id}", permissionEdit, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		var body struct {
			Name        *string         `json:"name"`
			Weights     *Weights        `json:"weights"`
			Constraints json.RawMessage `json:"constraints"`
		}
		if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
			return
		}
		constraints, err := decodeConstraints(body.Constraints)
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		profile, err := s.UpdateProfile(ownerID, r.PathValue("id"), ProfileInput{Name: body.Name, Weights: body.Weights, Constraints: constraints})
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, struct {
			Profile *Profile `json:"profile"`
		}{Profile: profile})
	})
	handle("DELETE /api/v1/routing/profiles/{id}", permissionEdit, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		if err := s.RemoveProfile(ownerID, r.PathValue("id")); err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Shows what `auto` would choose right now, without sending a request.
	handle("POST /api/v1/routing/preview", permissionView, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		var body struct {
			Profile  *string `json:"profile"`
			Mode     *string `json:"mode"`
			Workload *string `json:"workload"`
			Model    *string `json:"model"`
			Prompt   *string `json:"prompt"`
			Tools    *bool   `json:"tools"`
			Vision   *bool   `json:"vision"`
		}
		if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
			return
		}
		if body.Mode != nil && *body.Mode != "free_only" && *body.Mode != "paid" && *body.Mode != "both" {
			httpserver.WriteError(w, r, s.log(), httpserver.New(400, "VALIDATION_ERROR", "mode: Invalid enum value. Expected 'free_only' | 'paid' | 'both'"))
			return
		}
		if body.Workload != nil && *body.Workload != "subagent" {
			httpserver.WriteError(w, r, s.log(), httpserver.New(400, "VALIDATION_ERROR", "workload: Invalid enum value. Expected 'subagent'"))
			return
		}
		if body.Prompt != nil && len(*body.Prompt) > 200000 {
			httpserver.WriteError(w, r, s.log(), httpserver.New(400, "VALIDATION_ERROR", "prompt: String must contain at most 200000 character(s)"))
			return
		}
		prompt := "Hello"
		if body.Prompt != nil && *body.Prompt != "" {
			prompt = *body.Prompt
		}
		var content any = prompt
		if body.Vision != nil && *body.Vision {
			content = []task.Part{{Type: "text", Text: prompt}, {Type: "image_url"}}
		}
		request := RequestInfo{Messages: []task.Message{{Role: "user", Content: content}}}
		if body.Tools != nil && *body.Tools {
			request.Tools = []task.Tool{{Type: "function", Function: task.ToolFunction{Name: "probe", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}}
		}
		mode := "both"
		if body.Mode != nil {
			mode = *body.Mode
		}
		profile := "balanced"
		if body.Profile != nil {
			profile = *body.Profile
		}
		model := "auto"
		if body.Model != nil {
			model = *body.Model
		}
		key := RouterKey{ID: "preview", UserID: ownerID, Mode: mode, BudgetPeriod: "total", AllowedModelsJSON: "[]", Profile: profile}
		workload := ""
		if body.Workload != nil {
			workload = *body.Workload
		}
		result, err := s.Preview(PreviewOptions{
			UserID:         ownerID,
			Key:            key,
			RequestedModel: model,
			Request:        request,
			Needs:          Requirements(request),
			InputTokens:    EstimateInputTokens(request),
			Workload:       workload,
			Now:            time.Now(),
		})
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	})

	// Settings → Usage: even out usage, once there is enough data to go by.
	handle("GET /api/v1/routing/smoothing", permissionView, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		httpx.WriteJSON(w, http.StatusOK, s.SmoothingStatusOf(ownerID, time.Now()))
	})
	handle("PUT /api/v1/routing/smoothing", permissionEdit, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
			return
		}
		status, err := s.SetSmoothingEnabled(ownerID, body.Enabled, time.Now())
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, status)
	})

	// The home screen's live view: what this account's router is doing, by long
	// polling.
	handle("GET /api/v1/routing/live", permissionView, func(w http.ResponseWriter, r *http.Request, ownerID, _ string) {
		since := parseInt64(r.URL.Query().Get("since"), 0)
		if since < 0 {
			since = 0
		}
		waitMs := parseInt64(r.URL.Query().Get("wait"), 0)
		if waitMs < 0 {
			waitMs = 0
		}
		if waitMs > 25_000 {
			waitMs = 25_000
		}
		w.Header().Set("Cache-Control", "no-store")
		result := s.ReadLive(r.Context(), ownerID, since, time.Duration(waitMs)*time.Millisecond)
		if result.Snapshot {
			cards := s.LiveCards(ownerID, time.Now())
			httpx.WriteJSON(w, http.StatusOK, struct {
				LiveRead
				Cards *LiveCards `json:"cards,omitempty"`
			}{LiveRead: result, Cards: &cards})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, struct {
			LiveRead
			Cards *LiveCards `json:"cards,omitempty"`
		}{LiveRead: result})
	})

	handle("GET /api/v1/routing/load", permissionView, func(w http.ResponseWriter, r *http.Request, _, _ string) {
		httpx.WriteJSON(w, http.StatusOK, s.Load())
	})
}

// decodeConstraints merges a constraints body over the defaults, as
// rowToProfile does with a stored row ({...DEFAULT_CONSTRAINTS, ...parsed}):
// a field the body leaves out keeps its default, and an explicit null clears a
// pointer field. Nil when the body had no constraints at all.
func decodeConstraints(raw json.RawMessage) (*Constraints, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	constraints := DefaultConstraints()
	if err := json.Unmarshal(raw, &constraints); err != nil {
		return nil, httpserver.New(400, "VALIDATION_ERROR", "constraints: Expected an object.")
	}
	return &constraints, nil
}

func parseInt64(value string, fallback int64) int64 {
	if value == "" {
		return fallback
	}
	var out int64
	negative := false
	index := 0
	if value[0] == '-' {
		negative = true
		index = 1
	}
	digits := 0
	for ; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			break
		}
		out = out*10 + int64(value[index]-'0')
		digits++
	}
	if digits == 0 {
		return fallback
	}
	if negative {
		return -out
	}
	return out
}

// LiveKeyCard is one card of the live view: a key and its requests today.
type LiveKeyCard struct {
	Name     string `json:"name"`
	Requests int64  `json:"requests"`
}

// LiveProviderCard is one provider of the live view and its requests today.
type LiveProviderCard struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Slugs    []string `json:"slugs"`
	Requests int64    `json:"requests"`
}

// LiveCards is what the live view draws, busiest of the last day first.
type LiveCards struct {
	Keys      []LiveKeyCard      `json:"keys"`
	Providers []LiveProviderCard `json:"providers"`
}

// LiveCards builds the cards of the live view: the keys and connections of the
// last day, busiest first.
func (s *Service) LiveCards(userID string, now time.Time) LiveCards {
	out := LiveCards{Keys: []LiveKeyCard{}, Providers: []LiveProviderCard{}}
	if s.opts.DB == nil {
		return out
	}
	since := now.UnixMilli() - dayMs
	byAccount := map[string]int64{}
	if rows, err := s.opts.DB.R.Query(`SELECT account_id, COUNT(*) FROM requests WHERE user_id = ? AND created_at >= ? AND account_id IS NOT NULL GROUP BY account_id`, userID, since); err == nil {
		for rows.Next() {
			var accountID string
			var count int64
			if rows.Scan(&accountID, &count) == nil {
				byAccount[accountID] = count
			}
		}
		rows.Close()
	}
	providers := map[string]*LiveProviderCard{}
	var order []string
	for _, account := range s.accountCards(userID) {
		card, ok := providers[account.Key]
		if !ok {
			card = &LiveProviderCard{Key: account.Key, Name: account.Name, Slugs: []string{}}
			providers[account.Key] = card
			order = append(order, account.Key)
		}
		card.Slugs = append(card.Slugs, account.Slug)
		card.Requests += byAccount[account.ID]
	}
	for _, key := range order {
		out.Providers = append(out.Providers, *providers[key])
	}
	sortLiveProviders(out.Providers)
	if rows, err := s.opts.DB.R.Query(`SELECT k.name, COUNT(r.id) FROM router_keys k
		LEFT JOIN requests r ON r.router_key_id = k.id AND r.created_at >= ?
		WHERE k.user_id = ? AND k.revoked_at IS NULL AND k.peer_link_id IS NULL
		GROUP BY k.id ORDER BY COUNT(r.id) DESC, k.created_at`, since, userID); err == nil {
		for rows.Next() {
			var card LiveKeyCard
			if rows.Scan(&card.Name, &card.Requests) == nil {
				out.Keys = append(out.Keys, card)
			}
		}
		rows.Close()
	}
	return out
}

func sortLiveProviders(cards []LiveProviderCard) {
	for i := 1; i < len(cards); i++ {
		for j := i; j > 0 && cards[j].Requests > cards[j-1].Requests; j-- {
			cards[j], cards[j-1] = cards[j-1], cards[j]
		}
	}
}

// accountCards are the user's enabled connections, or what the database knows
// when the app did not wire the providers domain in.
func (s *Service) accountCards(userID string) []AccountCard {
	if s.opts.Accounts != nil {
		return s.opts.Accounts(userID)
	}
	if s.opts.DB == nil {
		return nil
	}
	var out []AccountCard
	rows, err := s.opts.DB.R.Query(`SELECT id, definition_id, label, slug FROM provider_accounts WHERE user_id = ? AND enabled = 1`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var card AccountCard
		if rows.Scan(&card.ID, &card.Key, &card.Name, &card.Slug) == nil {
			out = append(out, card)
		}
	}
	return out
}

// Requirements is capabilities/chat/canonical.requirements: what a request
// needs from a model. The gateway computes its own; the preview route uses this
// one.
func Requirements(request RequestInfo) Needs {
	needs := Needs{}
	for _, message := range request.Messages {
		for _, part := range task.ContentParts(message.Content) {
			if task.IsImagePart(part) {
				needs.Vision = true
			}
		}
	}
	needs.Tools = len(request.Tools) > 0
	if request.ResponseFormat != nil {
		needs.JSON = request.ResponseFormat.Type == "json_object" || request.ResponseFormat.Type == "json_schema"
	}
	return needs
}

// EstimateInputTokens is canonical.estimateInputTokens: a rough token estimate
// (about four characters per token, images counted flat) used only for routing
// and budget reservations.
func EstimateInputTokens(request RequestInfo) int {
	chars := 0
	images := 0
	for _, message := range request.Messages {
		for _, part := range task.ContentParts(message.Content) {
			if part.Type == "text" {
				chars += len(part.Text)
			} else {
				images++
			}
		}
		if len(message.ToolCalls) > 0 {
			if encoded, err := json.Marshal(message.ToolCalls); err == nil {
				chars += len(encoded)
			}
		}
	}
	if len(request.Tools) > 0 {
		if encoded, err := json.Marshal(request.Tools); err == nil {
			chars += len(encoded)
		}
	}
	return (chars+3)/4 + images*850 + len(request.Messages)*4
}
