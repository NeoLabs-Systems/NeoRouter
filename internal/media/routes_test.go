package media

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Capability-level cases of test/api/media.test.js: what the OpenAI-shaped
// endpoints answer for each kind, driven through a real HTTP server.

func TestEmbeddingsRequestShape(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("small", providers.CapEmbed)
	adapter.embedFn = func(request providers.EmbedRequest) (*providers.EmbedResult, error) {
		inputs, ok := request.Input.([]string)
		if !ok {
			t.Fatalf("the adapter is asked for a list of texts, got %T", request.Input)
		}
		vectors := make([][]float64, len(inputs))
		for index := range inputs {
			vectors[index] = []float64{1, 2, 3, 4}
		}
		tokens := 3
		return &providers.EmbedResult{Vectors: vectors, InputTokens: &tokens}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)

	response, payload := env.doJSON(t, server, "POST", "/v1/embeddings", map[string]any{"input": []string{"hello", "world"}, "dimensions": 4})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if got := response.Header.Get("X-NeoRouter-Embedding-Pin"); got != "small" {
		t.Fatalf("X-NeoRouter-Embedding-Pin = %q", got)
	}
	want := `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,2,3,4]},` +
		`{"object":"embedding","index":1,"embedding":[1,2,3,4]}],"model":"mock/small",` +
		`"usage":{"prompt_tokens":3,"total_tokens":3}}`
	if string(payload) != want {
		t.Fatalf("body = %s\nwant   %s", payload, want)
	}
	options := env.runner.last()
	if options.Capability != "embedding" || options.Method != providers.CapEmbed || options.Format != "embedding" {
		t.Fatalf("pipeline options = %+v", options)
	}
	if options.Units != 2 || options.InputTokens != 4 || options.RequestedModel != "auto" {
		t.Fatalf("units = %v, tokens = %v", options.Units, options.InputTokens)
	}
	pinned, err := env.svc.embeddingPin(context.Background(), env.key.ID)
	if err != nil || pinned == nil || pinned.CanonicalID != "small" || pinned.Dimensions == nil || *pinned.Dimensions != 4 {
		t.Fatalf("pin = %+v (%v)", pinned, err)
	}

	// Later auto requests keep the pinned model, whichever provider serves it.
	response, payload = env.doJSON(t, server, "POST", "/v1/embeddings", map[string]any{"input": "again"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	options = env.runner.last()
	if options.Filter(catalog.Entry{CanonicalID: "small"}, adapter) != true {
		t.Fatal("the pinned model is a candidate")
	}
	if options.Filter(catalog.Entry{CanonicalID: "other"}, adapter) != false {
		t.Fatal("another embedding model is not a candidate")
	}

	// The pin is readable and releasable.
	response, payload = env.do(t, server, "GET", "/v1/embeddings/pin", nil, "")
	if response.StatusCode != http.StatusOK || string(payload) != `{"pin":{"canonicalId":"small","dimensions":4}}` {
		t.Fatalf("pin body = %s (%d)", payload, response.StatusCode)
	}
	response, payload = env.do(t, server, "DELETE", "/v1/embeddings/pin", nil, "")
	if response.StatusCode != http.StatusOK || string(payload) != `{"reset":true}` {
		t.Fatalf("reset body = %s (%d)", payload, response.StatusCode)
	}
	response, payload = env.do(t, server, "GET", "/v1/embeddings/pin", nil, "")
	if string(payload) != `{"pin":null}` {
		t.Fatalf("pin body = %s (%d)", payload, response.StatusCode)
	}
}

func TestEmbeddingsBase64AndMismatch(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("small", providers.CapEmbed)
	adapter.embedFn = func(request providers.EmbedRequest) (*providers.EmbedResult, error) {
		return &providers.EmbedResult{Vectors: [][]float64{{1, 2, 3, 4}}}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)

	response, payload := env.doJSON(t, server, "POST", "/v1/embeddings", map[string]any{"input": "x", "dimensions": 4, "encoding_format": "base64"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	body := decodeBody(t, payload)
	vector, _ := body["data"].([]any)[0].(map[string]any)["embedding"].(string)
	decoded, err := base64.StdEncoding.DecodeString(vector)
	if err != nil || len(decoded) != 16 {
		t.Fatalf("embedding = %q (%v), want 4 float32 bytes", vector, err)
	}
	// Without a reported token count the request's own estimate is logged.
	usage, _ := body["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(1) {
		t.Fatalf("usage = %+v", usage)
	}

	response, payload = env.doJSON(t, server, "POST", "/v1/embeddings", map[string]any{"input": "x", "dimensions": 16})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "EMBEDDING_PIN_MISMATCH" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	want := "This key is pinned to small at 4 dimensions; vectors of another size would not be comparable. Ask for 4 dimensions or reset the pin."
	if got := errorMessage(t, payload); got != want {
		t.Fatalf("message = %q", got)
	}
}

func TestEmbeddingsRefuseWrongSizedVectors(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("fixed-embed", providers.CapEmbed)
	adapter.embedFn = func(request providers.EmbedRequest) (*providers.EmbedResult, error) {
		return &providers.EmbedResult{Vectors: [][]float64{make([]float64, 8)}}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/embeddings", map[string]any{"model": "mock/fixed-embed", "input": "hello", "dimensions": 4})
	if response.StatusCode == http.StatusOK {
		t.Fatalf("a vector of the wrong size is not passed on: %s", payload)
	}
	if !strings.Contains(errorMessage(t, payload), "8 dimensions instead of the 4") {
		t.Fatalf("message = %q", errorMessage(t, payload))
	}
	if pinned, _ := env.svc.embeddingPin(context.Background(), env.key.ID); pinned != nil {
		t.Fatalf("nothing is pinned at that size: %+v", pinned)
	}
}

func TestEmbeddingsPinnedModelNobodyServes(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("small", providers.CapEmbed)
	adapter.embedFn = func(providers.EmbedRequest) (*providers.EmbedResult, error) {
		return &providers.EmbedResult{Vectors: [][]float64{{1}}}, nil
	}
	env.useAdapter("account-1", adapter)
	if err := env.svc.setEmbeddingPin(context.Background(), env.key.ID, "gone/model", intPtr(1)); err != nil {
		t.Fatalf("pin: %v", err)
	}
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/embeddings", map[string]any{"input": "x", "dimensions": 1})
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "EMBEDDING_PIN_UNAVAILABLE" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if got := errorMessage(t, payload); !strings.Contains(got, "pinned to the embedding model gone/model") {
		t.Fatalf("message = %q", got)
	}
}

func TestImagesBase64AndLinks(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("flux", providers.CapGenerateImage)
	asked := []providers.ImageRequest{}
	adapter.imageFn = func(request providers.ImageRequest) (*providers.ImageResult, error) {
		asked = append(asked, request)
		revised := "a lighthouse at dusk"
		return &providers.ImageResult{Images: []providers.Image{
			{B64: base64.StdEncoding.EncodeToString([]byte("png-1")), Mime: "image/png", RevisedPrompt: &revised},
			{B64: base64.StdEncoding.EncodeToString([]byte("png-2")), Mime: "image/png"},
		}}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)

	response, payload := env.doJSON(t, server, "POST", "/v1/images/generations", map[string]any{"prompt": "a lighthouse", "n": 2})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if got := response.Header.Get("X-NeoRouter-Model"); got != "mock/flux" {
		t.Fatalf("X-NeoRouter-Model = %q", got)
	}
	body := decodeBody(t, payload)
	data, _ := body["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("data = %s", payload)
	}
	first, _ := data[0].(map[string]any)
	if first["b64_json"] != base64.StdEncoding.EncodeToString([]byte("png-1")) {
		t.Fatalf("first = %+v", first)
	}
	if first["revised_prompt"] != "a lighthouse at dusk" {
		t.Fatalf("first = %+v", first)
	}
	if len(asked) != 1 || asked[0].N != 2 || asked[0].Prompt != "a lighthouse" {
		t.Fatalf("asked = %+v", asked)
	}
	if options := env.runner.last(); options.Units != 2 {
		t.Fatalf("units = %v", options.Units)
	}

	// response_format: url hands out a link to the generated file instead.
	response, payload = env.doJSON(t, server, "POST", "/v1/images/generations", map[string]any{"prompt": "a lighthouse", "response_format": "url"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	body = decodeBody(t, payload)
	link, _ := body["data"].([]any)[0].(map[string]any)["url"].(string)
	if !strings.Contains(link, "/v1/media/") {
		t.Fatalf("url = %q", link)
	}
	image := fetchWithoutKey(t, link)
	if image.status != http.StatusOK || image.contentType != "image/png" || string(image.body) != "png-1" {
		t.Fatalf("image = %+v", image)
	}
	if image.cacheControl != "private, max-age=3600" || image.nosniff != "nosniff" {
		t.Fatalf("headers = %+v", image)
	}
	missing := fetchWithoutKey(t, server.URL+"/v1/media/nope")
	if missing.status != http.StatusNotFound || !strings.Contains(string(missing.body), "This media link expired or does not exist.") {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestImagesDownloadWhatTheProviderOnlyLinked(t *testing.T) {
	picture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write([]byte("webp-bytes"))
	}))
	t.Cleanup(picture.Close)
	env := newTestEnv(t)
	adapter := newFakeAdapter("flux", providers.CapGenerateImage)
	adapter.imageFn = func(request providers.ImageRequest) (*providers.ImageResult, error) {
		return &providers.ImageResult{Images: []providers.Image{{URL: picture.URL + "/one.webp"}}}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/images/generations", map[string]any{"prompt": "a cat"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	want := base64.StdEncoding.EncodeToString([]byte("webp-bytes"))
	if got, _ := decodeBody(t, payload)["data"].([]any)[0].(map[string]any)["b64_json"].(string); got != want {
		t.Fatalf("b64_json = %q, want the downloaded bytes", got)
	}
	// A link that cannot be read is a provider failure, not an empty answer.
	adapter.imageFn = func(request providers.ImageRequest) (*providers.ImageResult, error) {
		return &providers.ImageResult{Images: []providers.Image{{URL: picture.URL + "/gone.png"}}}, nil
	}
	picture.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	response, payload = env.doJSON(t, server, "POST", "/v1/images/generations", map[string]any{"prompt": "a cat"})
	if response.StatusCode != http.StatusBadGateway || errorCode(t, payload) != "IMAGE_FETCH_FAILED" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
}

func TestSpeechVoiceFallbackAndFormat(t *testing.T) {
	original := hasFfmpeg
	hasFfmpeg = func() bool { return false }
	t.Cleanup(func() { hasFfmpeg = original })

	env := newTestEnv(t)
	adapter := newFakeAdapter("tts", providers.CapSpeech)
	adapter.speechFn = func(request providers.SpeechRequest) (*providers.SpeechResult, error) {
		if request.Voice == "Kore" {
			// A voice this provider does not have.
			return nil, providers.NewError(providers.KindBadRequest, "voice not found", providers.ErrorOptions{})
		}
		voice := request.Voice
		if voice == "" {
			// The adapter picks its own default voice when none is asked for.
			voice = "alloy"
		}
		return &providers.SpeechResult{Audio: []byte("fake-mp3:" + voice + ":" + request.Input), Mime: "audio/mpeg"}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)

	response, payload := env.doJSON(t, server, "POST", "/v1/audio/speech", map[string]any{"input": "Hello there", "voice": "Kore"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if string(payload) != "fake-mp3:alloy:Hello there" {
		t.Fatalf("body = %q, want the provider's default voice", payload)
	}
	if got := response.Header.Get("X-NeoRouter-Audio-Format"); got != "mp3" {
		t.Fatalf("X-NeoRouter-Audio-Format = %q", got)
	}
	if got := response.Header.Get("Content-Type"); got != "audio/mpeg" {
		t.Fatalf("Content-Type = %q", got)
	}
	if recorded := adapter.recorded(); len(recorded) != 2 || recorded[0] != "speech:Kore" || recorded[1] != "speech:" {
		t.Fatalf("calls = %v, want the requested voice then the default", recorded)
	}
	// The billed unit is the character count of the input.
	if options := env.runner.last(); options.Units != float64(len("Hello there")) {
		t.Fatalf("units = %v", options.Units)
	}

	// A provider that speaks another container answers with it when ffmpeg is
	// missing, and the header names what came back.
	adapter.speechFn = func(request providers.SpeechRequest) (*providers.SpeechResult, error) {
		return &providers.SpeechResult{Audio: []byte("wav-bytes"), Mime: "audio/wav"}, nil
	}
	response, payload = env.doJSON(t, server, "POST", "/v1/audio/speech", map[string]any{"input": "hi", "response_format": "mp3"})
	if response.StatusCode != http.StatusOK || string(payload) != "wav-bytes" {
		t.Fatalf("body = %q (%d)", payload, response.StatusCode)
	}
	if got := response.Header.Get("X-NeoRouter-Audio-Format"); got != "wav" {
		t.Fatalf("X-NeoRouter-Audio-Format = %q", got)
	}
	if got := response.Header.Get("Content-Type"); got != "audio/wav" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestPrivacyShieldStopsTheRequest(t *testing.T) {
	env := newTestEnv(t)
	seen := []string{}
	env.shield.seen = &seen
	adapter := newFakeAdapter("small", providers.CapEmbed)
	adapter.embedFn = func(request providers.EmbedRequest) (*providers.EmbedResult, error) {
		inputs, _ := request.Input.([]string)
		if len(inputs) != 1 || inputs[0] != "<REDACTED>" {
			t.Fatalf("the provider is sent the shielded text, got %v", inputs)
		}
		return &providers.EmbedResult{Vectors: [][]float64{{1}}}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/embeddings", map[string]any{"input": "<secret>"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if len(seen) != 1 || seen[0] != "<secret>" {
		t.Fatalf("shield saw %v", seen)
	}
	if options := env.runner.last(); options.RedactedCount != 1 {
		t.Fatalf("redacted = %d", options.RedactedCount)
	}

	// A shield that fails stops the request: text that was not checked does not
	// leave.
	env.shield.fail = true
	response, payload = env.doJSON(t, server, "POST", "/v1/embeddings", map[string]any{"input": "hello"})
	if response.StatusCode != http.StatusInternalServerError || errorCode(t, payload) != "PRIVACY_SHIELD_FAILED" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
}

// linkedImage is the response of one generated-media link.
type linkedImage struct {
	status       int
	contentType  string
	cacheControl string
	nosniff      string
	body         []byte
}

// fetchWithoutKey reads a generated-media link, which carries no router key.
func fetchWithoutKey(t *testing.T, url string) linkedImage {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("fetch %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return linkedImage{
		status: response.StatusCode, contentType: response.Header.Get("Content-Type"),
		cacheControl: response.Header.Get("Cache-Control"), nosniff: response.Header.Get("X-Content-Type-Options"),
		body: body,
	}
}
