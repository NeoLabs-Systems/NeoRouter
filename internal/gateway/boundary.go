package gateway

// The boundary internal/media and internal/app use: the router-key middleware the
// /v1 media routes sit behind (media.KeyAuth), a Key from a stored row, and the
// catalog view of the entry a capability request answered on.

import (
	"context"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// keyContextKey is how Require hands the router key to the handler.
type keyContextKey struct{}

// Require is the gateway's router-key middleware: only a request carrying a
// valid router key reaches next, and every failure is answered in the OpenAI
// error shape.
func (s *Service) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, err := s.Authenticate(r)
		if err != nil {
			s.WriteError(w, r, dialectOpenAIChat, nil, "", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyContextKey{}, key)))
	})
}

// KeyOfRequest is the router key Require resolved for this request.
func (s *Service) KeyOfRequest(r *http.Request) (*Key, bool) {
	key, ok := r.Context().Value(keyContextKey{}).(*Key)
	if !ok || key == nil {
		return nil, false
	}
	return key, true
}

// KeyRowOfRequest is KeyOfRequest as the stored row (media.KeyAuth.Key).
func (s *Service) KeyRowOfRequest(r *http.Request) (*routerkeys.Row, bool) {
	key, ok := s.KeyOfRequest(r)
	if !ok {
		return nil, false
	}
	return key.Row, true
}

// KeyOf is the gateway's view of a stored router key.
func KeyOf(row *routerkeys.Row) *Key {
	return keyFromRow(row)
}

// ToCatalogEntry is the catalog view of the entry a capability request answered
// on, so a caller that declared its own narrow contract (internal/media) can
// report which model produced the answer.
func ToCatalogEntry(entry *routing.Entry) catalog.Entry {
	if entry == nil {
		return catalog.Entry{}
	}
	out := catalog.Entry{
		Ref:          entry.Ref,
		ID:           entry.ID,
		ModelID:      entry.ModelID,
		Name:         entry.Name,
		CanonicalID:  entry.CanonicalID,
		Capability:   entry.Capability,
		AccountID:    entry.AccountID,
		UserID:       entry.UserID,
		ProviderSlug: entry.ProviderSlug,
		DefinitionID: entry.DefinitionID,
		ProviderIdentity: catalog.ProviderIdentity{
			ProviderKey:  entry.ProviderKey,
			ProviderName: entry.ProviderName,
		},
		BillingMode:            entry.BillingMode,
		IsFlatRate:             entry.IsFlatRate,
		SlotPlan:               stringPointer(entry.SlotPlan),
		OverflowPolicy:         entry.OverflowPolicy,
		SlotWaitMs:             int64Pointer(entry.SlotWaitMs),
		Enabled:                entry.Enabled,
		Available:              entry.Available,
		AccountEnabled:         entry.AccountEnabled,
		AccountStatus:          entry.AccountStatus,
		ContextWindow:          int64CountPointer(entry.ContextWindow),
		MaxOutput:              int64CountPointer(entry.MaxOutput),
		Tools:                  entry.Tools,
		Vision:                 entry.Vision,
		JSON:                   entry.JSON,
		Reasoning:              entry.Reasoning,
		PriceIn:                entry.PriceIn,
		PriceOut:               entry.PriceOut,
		PriceCachedIn:          entry.PriceCachedIn,
		PriceCacheWrite:        entry.PriceCacheWrite,
		PriceSource:            stringPointer(entry.PriceSource),
		PriceUnit:              stringPointer(entry.PriceUnit),
		UnitPrice:              entry.UnitPrice,
		IsFree:                 entry.IsFree,
		Decision:               entry.Decision,
		IsPlan:                 entry.IsPlan,
		PlanOverflow:           entry.PlanOverflow,
		FreeReason:             stringPointer(entry.FreeReason),
		Intelligence:           entry.Intelligence,
		Skills:                 entry.Skills,
		IntelligenceSource:     stringPointer(entry.IntelligenceSource),
		IntelligenceConfidence: entry.IntelligenceConfidence,
		SpeedPriorTps:          entry.SpeedPriorTps,
		ReleaseDate:            stringPointer(entry.ReleaseDate),
		ParamsB:                entry.ParamsB,
	}
	return out
}

// stringPointer is a *string for the catalog, nil when the value is empty.
func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}

// int64Pointer is an *int64 for the catalog.
func int64Pointer(value *int) *int64 {
	if value == nil {
		return nil
	}
	copied := int64(*value)
	return &copied
}

// int64CountPointer is an unknown-is-zero count as the catalog's null.
func int64CountPointer(value int) *int64 {
	if value == 0 {
		return nil
	}
	copied := int64(value)
	return &copied
}

// RequestID is the id the request carries (X-Request-Id, or the one the server
// generated), for callers that answer a /v1 request themselves.
func RequestID(r *http.Request) string { return httpserver.RequestIDFrom(r.Context()) }
