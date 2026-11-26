package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// A chat stream outlives the upstream timeout; the caller's context bounds it.
func TestChatStreamsOutliveTheUpstreamTimeout(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, _ *http.Request, _ chatCall) {
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	})
	deps := chatDeps{baseURL: upstream.server.URL, secret: "k"}.deps()
	deps.HTTP = providers.NewClient(providers.Options{Timeout: time.Nanosecond})
	deps.UpstreamTimeout = time.Nanosecond
	adapter, err := (&Factory{}).NewAdapter(chatDefinition(t, "openai"), deps)
	if err != nil {
		t.Fatal(err)
	}
	chunks := 0
	for _, streamErr := range adapter.(providers.Streamer).Stream(context.Background(), json.RawMessage(`{"messages":[]}`),
		providers.Call{ModelID: "m", Timeout: time.Nanosecond}) {
		if streamErr != nil {
			t.Fatal(streamErr)
		}
		chunks++
	}
	if chunks != 1 {
		t.Fatalf("chunks = %d", chunks)
	}
}
