package gateway

// server/core/capability_registry.js: each routable kind of model and its
// gateway routes under /v1. Routing, quota, budget and the catalog are
// capability-agnostic and keyed by the model's capability column; the
// non-chat kinds share one router (internal/media).

import (
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// RouteSet is which routes serve a capability: the chat ones live in this
// package, the media ones in internal/media, and live voice is a WebSocket the
// server attaches itself.
type RouteSet string

const (
	// RoutesChat is capabilities/chat/gateway_routes.
	RoutesChat RouteSet = "chat"
	// RoutesMedia is capabilities/media/gateway_routes.
	RoutesMedia RouteSet = "media"
	// RoutesNone is a capability that has no HTTP route of its own (live voice).
	RoutesNone RouteSet = "none"
)

// Capability is one kind of model the gateway serves.
type Capability struct {
	ID    string
	Label string
	// Method is the adapter method that serves the kind, so a model is only
	// listed and routed when its provider's adapter can actually do it. "" means
	// any model of the kind can be called (chat, moderation/System One).
	Method providers.Capability
	// Routes is which route set serves the kind.
	Routes RouteSet
}

// Capabilities are the routable kinds, in the order the API lists them.
var Capabilities = []Capability{
	{ID: "chat", Label: "Chat", Method: providers.CapChat, Routes: RoutesChat},
	{ID: "embedding", Label: "Embeddings", Method: providers.CapEmbed, Routes: RoutesMedia},
	{ID: "image", Label: "Images", Method: providers.CapGenerateImage, Routes: RoutesMedia},
	{ID: "speech", Label: "Speech", Method: providers.CapSpeech, Routes: RoutesMedia},
	{ID: "transcription", Label: "Transcription", Method: providers.CapTranscribe, Routes: RoutesMedia},
	{ID: "moderation", Label: "System One", Method: "", Routes: RoutesMedia},
	{ID: "video", Label: "Video", Method: providers.CapCreateVideo, Routes: RoutesMedia},
	{ID: "search", Label: "Web search", Method: providers.CapSearch, Routes: RoutesMedia},
	{ID: "weather", Label: "Weather", Method: providers.CapWeather, Routes: RoutesMedia},
	// Live voice sessions are WebSockets, attached to the HTTP server.
	{ID: "live", Label: "Live voice", Method: "", Routes: RoutesNone},
}

// CapabilityInfo is the id and label GET /api/docs and the app's screens show.
type CapabilityInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// List is list: the capability ids and labels, in order.
func List() []CapabilityInfo {
	out := make([]CapabilityInfo, 0, len(Capabilities))
	for _, capability := range Capabilities {
		out = append(out, CapabilityInfo{ID: capability.ID, Label: capability.Label})
	}
	return out
}

// MethodFor is methodFor: the adapter method serving a kind, "" when any model
// of it can be called.
func MethodFor(id string) providers.Capability {
	for _, capability := range Capabilities {
		if capability.ID == id {
			return capability.Method
		}
	}
	return ""
}

// IsKind reports whether id is one of the capability ids.
func IsKind(id string) bool {
	for _, capability := range Capabilities {
		if capability.ID == id {
			return true
		}
	}
	return false
}

// Routes mounts the chat half of the gateway on mux (the media half is mounted
// by internal/media through its own entry point, and live voice by
// internal/live on the server).
func (s *Service) Routes(mux *http.ServeMux) {
	s.mountChatRoutes(mux)
	s.mountPlaygroundRoutes(mux)
	s.mountAPIDocsRoutes(mux)
}
