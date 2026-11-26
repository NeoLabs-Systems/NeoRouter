package usage

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Routes registers the activity API at /api/v1/usage.
func (s *Service) Routes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/usage/summary", s.guard(s.summary))
	mux.Handle("GET /api/v1/usage/analytics", s.guard(s.analytics))
	mux.Handle("GET /api/v1/usage/health", s.guard(s.health))
	mux.Handle("GET /api/v1/usage/learning", s.guard(s.learning))
	mux.Handle("GET /api/v1/usage/logs", s.guard(s.logs))
	mux.Handle("GET /api/v1/usage/capacity", s.guard(s.capacity))
	mux.Handle("GET /api/v1/usage/quotas", s.guard(s.quotas))
}

// guard applies the activity permission the whole router is mounted behind.
func (s *Service) guard(handler http.HandlerFunc) http.Handler {
	if s.Require == nil {
		return handler
	}
	return s.Require("activity.view")(handler)
}

func (s *Service) space(r *http.Request) (Space, error) {
	if s.Space == nil {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	space, ok := s.Space(r.Context())
	if !ok {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	return space, nil
}

func (s *Service) summary(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	query := r.URL.Query()
	body, err := s.SummaryOf(space.OwnerID, rangeOf(query), s.Now(), query.Get("only") == "totals")
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (s *Service) analytics(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	query := r.URL.Query()
	body, err := s.AnalyticsOf(space.OwnerID, rangeOf(query), s.Now(), keyIDsOf(query))
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (s *Service) health(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	body, err := s.Health(space.OwnerID, s.Now())
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (s *Service) learning(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	body, err := s.LearningOf(space.OwnerID, s.Now())
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

// logsBody wraps the request log.
type logsBody struct {
	Logs []LogRow `json:"logs"`
}

func (s *Service) logs(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	query := r.URL.Query()
	logQuery := LogQuery{
		Limit:  int(numberOf(query.Get("limit"))),
		KeyID:  query.Get("keyId"),
		KeyIDs: keyIDsOf(query),
		Status: query.Get("status"),
	}
	if before := query.Get("before"); before != "" {
		value := int64(numberOf(before))
		logQuery.Before = &value
	}
	rows, err := s.Logs(space.OwnerID, logQuery)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, logsBody{Logs: rows})
}

func (s *Service) capacity(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	if s.CapacityView == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{})
		return
	}
	body, err := s.CapacityView(space.OwnerID, s.Now())
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (s *Service) quotas(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	if s.QuotasView == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"accounts": []any{}})
		return
	}
	body, err := s.QuotasView(space.OwnerID, s.Now())
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

// rangeOf is the requested range, 24h when none was asked for.
func rangeOf(query map[string][]string) string {
	if values := query["range"]; len(values) > 0 && values[0] != "" {
		return values[0]
	}
	return "24h"
}

// keyIDsOf is ?keyIds=a,b split into trimmed ids.
func keyIDsOf(query map[string][]string) []string {
	out := []string{}
	values, ok := query["keyIds"]
	if !ok {
		return out
	}
	for _, part := range strings.Split(values[0], ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// numberOf is the number in text: 0 when the text is blank or unreadable.
func numberOf(text string) float64 {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0
	}
	return value
}
