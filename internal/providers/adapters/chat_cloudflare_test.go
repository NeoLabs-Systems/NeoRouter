package adapters

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Cloudflare Workers AI, ported from providers/adapters/cloudflare.js: the key
// check is Cloudflare's own token endpoint, the listing walks the task
// catalogue, and chat travels on the OpenAI-compatible path the account's base
// URL points at.

// cloudflareControlPlane points the key check, the listing and the model calls
// at the fake upstream's control-plane root.
func cloudflareControlPlane(t *testing.T, baseURL string) {
	t.Helper()
	previous := cloudflareAPI
	cloudflareAPI = baseURL + "/client/v4"
	t.Cleanup(func() { cloudflareAPI = previous })
}

func cloudflareAdapter(t *testing.T, baseURL string) *cloudflare {
	t.Helper()
	return newCloudflare(chatDefinition(t, "cloudflare"), chatDeps{
		baseURL: baseURL + "/accounts/acct-1/ai/v1",
		secret:  "SI_SECRET_TOKEN_2FOEBMDH",
		options: map[string]any{"accountId": "acct-1"},
	}.deps())
}

func TestCloudflareValidate(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   providers.ValidationStatus
		text   string
	}{
		{"active", http.StatusOK, `{"result":{"status":"active"}}`, providers.ValidationHealthy, ""},
		{"disabled", http.StatusOK, `{"result":{"status":"disabled"}}`, providers.ValidationInvalid, "Token status: disabled"},
		{"no status", http.StatusOK, `{"result":{}}`, providers.ValidationInvalid, "Token status: unknown"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
				if r.URL.Path != "/client/v4/user/tokens/verify" {
					t.Errorf("path = %q", r.URL.Path)
				}
				chatJSON(w, testCase.status, testCase.body)
			})
			cloudflareControlPlane(t, upstream.url())
			adapter := cloudflareAdapter(t, upstream.url())
			verdict, err := adapter.Validate(context.Background())
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if verdict.Status != testCase.want {
				t.Fatalf("status = %q, want %q", verdict.Status, testCase.want)
			}
			if testCase.text != "" && (verdict.Message == nil || *verdict.Message != testCase.text) {
				t.Errorf("message = %v, want %q", verdict.Message, testCase.text)
			}
		})
	}
}

func TestCloudflareValidateRefusedKey(t *testing.T) {
	// Both verify endpoints refuse the token: the connection is invalid.
	var paths []string
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		paths = append(paths, r.URL.Path)
		chatJSON(w, http.StatusUnauthorized, `{"success":false,"errors":[{"message":"Invalid API Token"}]}`)
	})
	cloudflareControlPlane(t, upstream.url())
	adapter := cloudflareAdapter(t, upstream.url())
	verdict, err := adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if verdict.Status != providers.ValidationInvalid {
		t.Fatalf("status = %q", verdict.Status)
	}
	if verdict.Message == nil || *verdict.Message != "Cloudflare rejected this API token." {
		t.Errorf("message = %v", verdict.Message)
	}
	if len(paths) != 2 {
		t.Errorf("paths = %v, want the account endpoint tried too", paths)
	}
	if !strings.HasSuffix(paths[1], "/accounts/acct-1/tokens/verify") {
		t.Errorf("second path = %q", paths[1])
	}
}

func TestCloudflareValidateUnknownOnAnUnexpectedFailure(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusInternalServerError, `{"errors":[{"message":"cloudflare is down"}]}`)
	})
	cloudflareControlPlane(t, upstream.url())
	adapter := cloudflareAdapter(t, upstream.url())
	verdict, err := adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if verdict.Status != providers.ValidationUnknown {
		t.Fatalf("status = %q", verdict.Status)
	}
}

func TestCloudflareListsModelsPerTask(t *testing.T) {
	asked := map[string]bool{}
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		task := r.URL.Query().Get("task")
		asked[task] = true
		if r.URL.Query().Get("per_page") != "1000" {
			t.Errorf("per_page = %q", r.URL.Query().Get("per_page"))
		}
		if !strings.HasPrefix(r.URL.Path, "/client/v4/accounts/acct-1/ai/models/search") {
			t.Errorf("path = %q", r.URL.Path)
		}
		if task != "Text Generation" {
			chatJSON(w, http.StatusOK, `{"result":[]}`)
			return
		}
		chatJSON(w, http.StatusOK, `{"result":[
			{"name":"@cf/meta/llama-3.3-70b","description":"llama","properties":[{"property_id":"context_window","value":"131072"},{"property_id":"function_calling","value":"true"}]},
			{"name":"@cf/llava-hf/llava-1.5","properties":[]}
		]}`)
	})
	cloudflareControlPlane(t, upstream.url())
	adapter := cloudflareAdapter(t, upstream.url())
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(asked) != 5 {
		t.Errorf("tasks asked = %v, want the whole catalogue", asked)
	}
	if len(models) != 2 {
		t.Fatalf("models = %v", models)
	}
	llama := models[0]
	if llama.ID != "@cf/meta/llama-3.3-70b" {
		t.Errorf("id = %q", llama.ID)
	}
	if llama.ContextWindow == nil || *llama.ContextWindow != 131072 {
		t.Errorf("context window = %v", llama.ContextWindow)
	}
	if llama.Tools == nil || !*llama.Tools {
		t.Error("function_calling=true means tools")
	}
	if llama.Vision == nil || *llama.Vision {
		t.Error("a plain llama reads no images")
	}
	if models[1].Vision == nil || !*models[1].Vision {
		t.Error("llava reads images")
	}
	if models[0].Kind != providers.KindChat {
		t.Errorf("kind = %q", models[0].Kind)
	}
}

func TestCloudflareChatUsesTheOpenAIPath(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if r.URL.Path != "/accounts/acct-1/ai/v1/chat/completions" {
			t.Errorf("path = %q, want the account's OpenAI-compatible endpoint", r.URL.Path)
		}
		chatJSON(w, http.StatusOK, `{"id":"c1","object":"chat.completion","created":1,"model":"@cf/meta/llama-3.3-70b","choices":[{"index":0,"message":{"role":"assistant","content":"cf says hi"},"finish_reason":"stop"}]}`)
	})
	adapter := cloudflareAdapter(t, upstream.url())
	result, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`), providers.Call{ModelID: "@cf/meta/llama-3.3-70b"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := upstream.last(t).headers.Get("Authorization"); got != "Bearer SI_SECRET_TOKEN_2FOEBMDH" {
		t.Errorf("authorization = %q", got)
	}
	message := chatPath(t, decodedChatResult(t, result.Body), "choices").([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "cf says hi" {
		t.Errorf("content = %v", message["content"])
	}
}

func TestCloudflareSpeech(t *testing.T) {
	t.Run("aura takes a speaker", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("mp3-bytes"))
		})
		cloudflareControlPlane(t, upstream.url())
		adapter := cloudflareAdapter(t, upstream.url())
		result, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: "luna"}, providers.Call{ModelID: "@cf/deepgram/aura-2-en"})
		if err != nil {
			t.Fatalf("speech: %v", err)
		}
		if string(result.Audio) != "mp3-bytes" || result.Mime != "audio/mpeg" {
			t.Errorf("audio = %q mime = %q", result.Audio, result.Mime)
		}
		sent := upstream.last(t)
		if !strings.HasSuffix(sent.path, "/ai/run/@cf/deepgram/aura-2-en") {
			t.Errorf("path = %q", sent.path)
		}
		if sent.body["text"] != "Hi" || sent.body["speaker"] != "luna" {
			t.Errorf("body = %v", sent.body)
		}
	})
	t.Run("another provider's voice gets the default", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("mp3"))
		})
		cloudflareControlPlane(t, upstream.url())
		adapter := cloudflareAdapter(t, upstream.url())
		if _, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: "Kore"}, providers.Call{ModelID: "@cf/deepgram/aura-2-en"}); err != nil {
			t.Fatalf("speech: %v", err)
		}
		if _, present := upstream.last(t).body["speaker"]; present {
			t.Errorf("Aura refuses another provider's voice, so none is sent: %v", upstream.last(t).body)
		}
	})
	t.Run("melotts takes a prompt", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			chatJSON(w, http.StatusOK, `{"result":{"audio":"`+base64.StdEncoding.EncodeToString([]byte("audio"))+`"}}`)
		})
		cloudflareControlPlane(t, upstream.url())
		adapter := cloudflareAdapter(t, upstream.url())
		result, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "Hi"}, providers.Call{ModelID: "@cf/myshell-ai/melotts"})
		if err != nil {
			t.Fatalf("speech: %v", err)
		}
		if string(result.Audio) != "audio" {
			t.Errorf("audio = %q", result.Audio)
		}
		body := upstream.last(t).body
		if body["prompt"] != "Hi" || body["lang"] != "en" {
			t.Errorf("body = %v", body)
		}
	})
}

func TestCloudflareTranscribe(t *testing.T) {
	t.Run("whisper takes base64 audio", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			chatJSON(w, http.StatusOK, `{"result":{"text":"hello world","transcription_info":{"language":"english","duration":2.5},"segments":[{"start":0,"end":1.2,"text":" hello"},{"start":1.2,"end":2.5,"text":" world"}]}}`)
		})
		cloudflareControlPlane(t, upstream.url())
		adapter := cloudflareAdapter(t, upstream.url())
		transcript, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte("audio"), Mime: "audio/wav", Translate: true}, providers.Call{ModelID: "@cf/openai/whisper-large-v3-turbo"})
		if err != nil {
			t.Fatalf("transcribe: %v", err)
		}
		if transcript.Text != "hello world" || transcript.Language != "english" {
			t.Errorf("transcript = %+v", transcript)
		}
		if transcript.Duration == nil || *transcript.Duration != 2.5 {
			t.Errorf("duration = %v", transcript.Duration)
		}
		if len(transcript.Segments) != 2 || transcript.Segments[1].Text != " world" {
			t.Errorf("segments = %+v", transcript.Segments)
		}
		body := upstream.last(t).body
		if body["audio"] != base64.StdEncoding.EncodeToString([]byte("audio")) {
			t.Errorf("audio = %v", body["audio"])
		}
		if body["task"] != "translate" {
			t.Errorf("task = %v", body["task"])
		}
	})
	t.Run("nova takes the raw audio", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			chatJSON(w, http.StatusOK, `{"result":{"results":{"channels":[{"detected_language":"en","alternatives":[{"transcript":"Hello from Workers AI."}]}]}}}`)
		})
		cloudflareControlPlane(t, upstream.url())
		adapter := cloudflareAdapter(t, upstream.url())
		transcript, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte("raw-audio"), Mime: "audio/wav"}, providers.Call{ModelID: "@cf/deepgram/nova-3"})
		if err != nil {
			t.Fatalf("transcribe: %v", err)
		}
		if transcript.Text != "Hello from Workers AI." || transcript.Language != "en" {
			t.Errorf("transcript = %+v", transcript)
		}
		sent := upstream.last(t)
		if string(sent.raw) != "raw-audio" {
			t.Errorf("the audio must be the request body: %q", sent.raw)
		}
		if sent.headers.Get("Content-Type") != "audio/wav" {
			t.Errorf("content-type = %q", sent.headers.Get("Content-Type"))
		}
	})
	t.Run("no transcript is a failure", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			chatJSON(w, http.StatusOK, `{"result":{}}`)
		})
		cloudflareControlPlane(t, upstream.url())
		adapter := cloudflareAdapter(t, upstream.url())
		_, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte("audio")}, providers.Call{ModelID: "@cf/openai/whisper-large-v3-turbo"})
		if providers.KindOf(err) != providers.KindInvalidOutput {
			t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
		}
	})
}

func TestCloudflareGenerateImage(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("png"))
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"result":{"image":"`+png+`"}}`)
	})
	cloudflareControlPlane(t, upstream.url())
	adapter := cloudflareAdapter(t, upstream.url())
	result, err := adapter.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat", N: 2, Size: "1024x768"}, providers.Call{ModelID: "@cf/black-forest-labs/flux-1-schnell"})
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if len(result.Images) != 2 || result.Images[0].B64 != png || result.Images[0].Mime != "image/jpeg" {
		t.Fatalf("images = %+v", result.Images)
	}
	body := upstream.last(t).body
	if body["prompt"] != "a cat" || chatNumberAt(t, body["width"]) != 1024 || chatNumberAt(t, body["height"]) != 768 {
		t.Errorf("body = %v", body)
	}

	// FLUX.2 only takes multipart form data.
	var contentType string
	var formBody string
	flux := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		contentType = r.Header.Get("Content-Type")
		formBody = string(call.raw)
		chatJSON(w, http.StatusOK, `{"result":{"image":"`+png+`"}}`)
	})
	cloudflareControlPlane(t, flux.url())
	adapter = cloudflareAdapter(t, flux.url())
	if _, err := adapter.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat", Size: "512x512"}, providers.Call{ModelID: "@cf/black-forest-labs/flux-2-dev"}); err != nil {
		t.Fatalf("image: %v", err)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		t.Fatalf("content-type = %q", contentType)
	}
	for _, want := range []string{`name="prompt"`, "a cat", `name="width"`, "512", `name="height"`} {
		if !strings.Contains(formBody, want) {
			t.Errorf("the form body is missing %q: %s", want, formBody)
		}
	}
}

func TestCloudflareRunReadsBytesAndErrors(t *testing.T) {
	// /ai/run answers raw bytes for some models.
	bytesUpstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("raw-png"))
	})
	cloudflareControlPlane(t, bytesUpstream.url())
	adapter := cloudflareAdapter(t, bytesUpstream.url())
	result, err := adapter.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "x"}, providers.Call{ModelID: "@cf/x/flux-1-schnell"})
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if result.Images[0].B64 != base64.StdEncoding.EncodeToString([]byte("raw-png")) || result.Images[0].Mime != "image/png" {
		t.Errorf("image = %+v", result.Images[0])
	}

	// A JSON answer that says it failed is a bad request with Cloudflare's words.
	failed := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"success":false,"errors":[{"message":"AiError: Model is not available on the Workers Free plan"}]}`)
	})
	cloudflareControlPlane(t, failed.url())
	adapter = cloudflareAdapter(t, failed.url())
	_, err = adapter.Speech(context.Background(), providers.SpeechRequest{Input: "hi"}, providers.Call{ModelID: "@cf/myshell-ai/melotts"})
	if providers.KindOf(err) != providers.KindBadRequest {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
	if !strings.Contains(err.Error(), "Workers Free plan") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestCloudflareCapabilities(t *testing.T) {
	adapter := cloudflareAdapter(t, "http://127.0.0.1:1")
	for _, capability := range []providers.Capability{
		providers.CapChat, providers.CapGenerateImage, providers.CapSpeech, providers.CapTranscribe,
	} {
		if !adapter.Supports(capability) {
			t.Errorf("%s should be supported", capability)
		}
	}
	if adapter.Supports(providers.CapCreateVideo) {
		t.Error("Workers AI video generation is not wired here")
	}
	// The account id is required, and the base URL carries it.
	if adapter.baseURL != "http://127.0.0.1:1/accounts/acct-1/ai/v1" {
		t.Errorf("baseURL = %q", adapter.baseURL)
	}

}
