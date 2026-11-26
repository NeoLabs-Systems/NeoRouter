package adapters

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Gemini's dialect, ported from google_gemini.js: the generateContent body, the
// candidate reading, and the media calls Gemini answers with the same endpoint.

func geminiAdapter(t *testing.T, baseURL string) *googleGemini {
	t.Helper()
	return newGoogleGemini(chatDefinition(t, "google"), chatDeps{baseURL: baseURL + "/v1beta", secret: "goog-key"}.deps())
}

func TestGeminiChatRequestShape(t *testing.T) {
	var body map[string]any
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		body = call.body
		chatJSON(w, http.StatusOK, `{"responseId":"resp-1","candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"thoughtsTokenCount":3,"cachedContentTokenCount":1}}`)
	})
	adapter := geminiAdapter(t, upstream.url())
	request := chatRequest(t, `{
		"messages":[
			{"role":"system","content":"be brief"},
			{"role":"developer","content":"and kind"},
			{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},
			{"role":"assistant","content":"calling","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get","arguments":"{\"a\":1}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"{\"ok\":true}"}
		],
		"temperature":0.3,"top_p":0.9,"max_tokens":256,"stop":["x"],"seed":11,
		"response_format":{"type":"json_schema","json_schema":{"schema":{"type":"object","properties":{"a":{"type":["string","null"]}},"additionalProperties":false}}},
		"tools":[{"type":"function","function":{"name":"get","description":"gets","parameters":{"type":"object","properties":{"a":{"type":"string"}},"$schema":"x"}}}],
		"tool_choice":{"type":"function","function":{"name":"get"}}
	}`)
	result, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "gemini-3-pro"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	// The system turns are one systemInstruction.
	system := chatPath(t, body, "systemInstruction", "parts").([]any)[0].(map[string]any)["text"]
	if system != "be brief\n\nand kind" {
		t.Errorf("systemInstruction = %v", system)
	}
	// The tool answer is its own user turn: Gemini alternates user and model.
	contents := body["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents = %v", contents)
	}
	userParts := contents[0].(map[string]any)["parts"].([]any)
	if contents[0].(map[string]any)["role"] != "user" || len(userParts) != 2 {
		t.Fatalf("the first turn = %v", contents[0])
	}
	inline := userParts[1].(map[string]any)["inlineData"].(map[string]any)
	if inline["mimeType"] != "image/png" || inline["data"] != "AAAA" {
		t.Errorf("the image part = %v", inline)
	}
	model := contents[1].(map[string]any)
	if model["role"] != "model" {
		t.Fatalf("the assistant turn = %v", model)
	}
	modelParts := model["parts"].([]any)
	call := modelParts[1].(map[string]any)
	functionCall := call["functionCall"].(map[string]any)
	if functionCall["name"] != "get" || functionCall["args"].(map[string]any)["a"] != float64(1) {
		t.Errorf("the function call = %v", call)
	}
	if call["thoughtSignature"] != geminiSignatureBypass {
		t.Errorf("an unknown tool call must use the bypass signature: %v", call)
	}

	config := body["generationConfig"].(map[string]any)
	if config["temperature"] != 0.3 || config["topP"] != 0.9 || chatNumberAt(t, config["maxOutputTokens"]) != 256 || chatNumberAt(t, config["seed"]) != 11 {
		t.Errorf("generationConfig = %v", config)
	}
	if config["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v", config["responseMimeType"])
	}
	schema := config["responseSchema"].(map[string]any)
	if _, present := schema["additionalProperties"]; present {
		t.Errorf("Gemini refuses schema keys it does not know: %v", schema)
	}
	property := schema["properties"].(map[string]any)["a"].(map[string]any)
	if property["type"] != "string" || property["nullable"] != true {
		t.Errorf("a union type must become one type plus nullable: %v", property)
	}

	tools := body["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)
	declaration := tools[0].(map[string]any)
	if declaration["name"] != "get" || declaration["description"] != "gets" {
		t.Errorf("declaration = %v", declaration)
	}
	if _, present := declaration["parameters"].(map[string]any)["$schema"]; present {
		t.Errorf("the schema key must be dropped: %v", declaration["parameters"])
	}
	toolConfig := body["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)
	if toolConfig["mode"] != "ANY" || toolConfig["allowedFunctionNames"].([]any)[0] != "get" {
		t.Errorf("toolConfig = %v", toolConfig)
	}

	completion := decodedChatResult(t, result.Body)
	message := chatPath(t, completion, "choices").([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "hi" {
		t.Errorf("content = %v", message["content"])
	}
	if completion["id"] != "resp-1" {
		t.Errorf("id = %v", completion["id"])
	}
	usage := completion["usage"].(map[string]any)
	if chatNumberAt(t, usage["prompt_tokens"]) != 5 || chatNumberAt(t, usage["completion_tokens"]) != 5 {
		t.Errorf("thoughts count as output: %v", usage)
	}
	if chatNumberAt(t, usage["total_tokens"]) != 10 {
		t.Errorf("total = %v", usage)
	}
	if chatNumberAt(t, usage["prompt_tokens_details"].(map[string]any)["cached_tokens"]) != 1 {
		t.Errorf("cached = %v", usage["prompt_tokens_details"])
	}
}

func TestGeminiReasoningEffortAndThinking(t *testing.T) {
	cases := []struct {
		model  string
		effort string
		want   map[string]any
	}{
		{"gemini-2.5-pro", "low", map[string]any{"thinkingBudget": float64(1024)}},
		{"gemini-2.5-pro", "minimal", map[string]any{"thinkingBudget": float64(1024)}},
		{"gemini-2.5-pro", "high", map[string]any{"thinkingBudget": float64(24576)}},
		{"gemini-3-pro", "high", map[string]any{"thinkingLevel": "high"}},
		{"gemini-3-pro", "medium", map[string]any{"thinkingLevel": "low"}},
	}
	for _, testCase := range cases {
		var body map[string]any
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			body = call.body
			chatJSON(w, http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`)
		})
		adapter := geminiAdapter(t, upstream.url())
		request := chatRequest(t, `{"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"`+testCase.effort+`"}`)
		if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: testCase.model}); err != nil {
			t.Fatalf("chat: %v", err)
		}
		config, _ := body["generationConfig"].(map[string]any)
		thinking, _ := config["thinkingConfig"].(map[string]any)
		for name, value := range testCase.want {
			if thinking[name] != value {
				t.Errorf("%s %s: %v = %v, want %v", testCase.model, testCase.effort, name, thinking[name], value)
			}
		}
	}
}

func TestGeminiChatReadsToolCallsAndTheirSignatures(t *testing.T) {
	var bodies []map[string]any
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		bodies = append(bodies, call.body)
		if len(bodies) == 1 {
			chatJSON(w, http.StatusOK, `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_9","name":"get","args":{"q":"x"}},"thoughtSignature":"sig-1"}]},"finishReason":"STOP"}]}`)
			return
		}
		chatJSON(w, http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"done"}]},"finishReason":"STOP"}]}`)
	})
	adapter := geminiAdapter(t, upstream.url())
	first, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`), providers.Call{ModelID: "gemini-3-pro"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	choice := chatPath(t, decodedChatResult(t, first.Body), "choices").([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls for a function call", choice["finish_reason"])
	}
	call := choice["message"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "call_9" || call["type"] != "function" {
		t.Errorf("tool call = %v", call)
	}
	if _, present := call["index"]; present {
		t.Errorf("a completion's tool call carries no stream index: %v", call)
	}
	if call["function"].(map[string]any)["arguments"] != `{"q":"x"}` {
		t.Errorf("arguments = %v", call["function"])
	}

	// The next turn echoes the signature the model issued.
	second := `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_9","type":"function","function":{"name":"get","arguments":"{\"q\":\"x\"}"}}]},{"role":"tool","tool_call_id":"call_9","content":"ok"}]}`
	if _, err := adapter.Chat(context.Background(), chatRequest(t, second), providers.Call{ModelID: "gemini-3-pro"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	model := bodies[1]["contents"].([]any)[1].(map[string]any)
	signature := model["parts"].([]any)[0].(map[string]any)["thoughtSignature"]
	if signature != "sig-1" {
		t.Errorf("thoughtSignature = %v, want the one the model issued", signature)
	}
	// The tool answer travels back as a functionResponse.
	toolTurn := bodies[1]["contents"].([]any)[2].(map[string]any)
	response := toolTurn["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if response["name"] != "get" {
		t.Errorf("functionResponse = %v", response)
	}
}

func TestGeminiStream(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("the stream needs alt=sse: %q", r.URL.RawQuery)
		}
		chatSSE(w, []string{
			`{"candidates":[{"content":{"parts":[{"text":"Hel"}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"text":"lo"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2}}`,
		}, false)
	})
	adapter := geminiAdapter(t, upstream.url())
	chunks := chatChunks(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`), providers.Call{ModelID: "gemini-3-pro"}))
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d: %v", len(chunks), chunks)
	}
	first := chatPath(t, chunks[0], "choices").([]any)[0].(map[string]any)["delta"].(map[string]any)
	if first["content"] != "Hel" {
		t.Errorf("content = %v", first)
	}
	last := chatPath(t, chunks[len(chunks)-1], "choices").([]any)[0].(map[string]any)
	if last["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", last["finish_reason"])
	}
	if chatNumberAt(t, chunks[len(chunks)-1]["usage"].(map[string]any)["prompt_tokens"]) != 4 {
		t.Errorf("the usage block must end the stream: %v", chunks[len(chunks)-1])
	}
}

func TestGeminiStreamWithoutContentStillFinishes(t *testing.T) {
	// A stream that only carries usage still ends with one chunk.
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatSSE(w, []string{`{"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`}, false)
	})
	adapter := geminiAdapter(t, upstream.url())
	chunks := chatChunks(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gemini-3-pro"}))
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d: %v", len(chunks), chunks)
	}
	if chunks[0]["object"] != "chat.completion.chunk" {
		t.Errorf("chunk = %v", chunks[0])
	}
}

func TestGeminiRefusals(t *testing.T) {
	t.Run("empty answer", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			chatJSON(w, http.StatusOK, `{"candidates":[]}`)
		})
		adapter := geminiAdapter(t, upstream.url())
		_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gemini-3-pro"})
		if providers.KindOf(err) != providers.KindEmpty {
			t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
		}
		if err.Error() != "The provider returned an empty response." {
			t.Errorf("message = %q", err.Error())
		}
	})
	t.Run("blocked prompt", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			chatJSON(w, http.StatusOK, `{"promptFeedback":{"blockReason":"SAFETY"}}`)
		})
		adapter := geminiAdapter(t, upstream.url())
		_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gemini-3-pro"})
		if providers.KindOf(err) != providers.KindBadRequest {
			t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
		}
		if err.Error() != "Prompt blocked: SAFETY" {
			t.Errorf("message = %q", err.Error())
		}
	})
	t.Run("error body", func(t *testing.T) {
		upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
			chatJSON(w, http.StatusOK, `{"error":{"code":429,"message":"Quota exceeded"}}`)
		})
		adapter := geminiAdapter(t, upstream.url())
		_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gemini-3-pro"})
		// "Quota exceeded" is the allowance being used up, not a burst limit.
		if providers.KindOf(err) != providers.KindQuotaExhausted {
			t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
		}
	})
}

func TestGeminiListsModelsByWhatTheyCanDo(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if call.query == "pageSize=1000" {
			chatJSON(w, http.StatusOK, `{"models":[
				{"name":"models/gemini-3-pro","displayName":"Gemini 3 Pro","supportedGenerationMethods":["generateContent"],"inputTokenLimit":1000000,"thinking":true},
				{"name":"models/gemma-3-27b","supportedGenerationMethods":["generateContent"]},
				{"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"],"inputTokenLimit":2048},
				{"name":"models/gemini-live-2","supportedGenerationMethods":["bidiGenerateContent"]}
			],"nextPageToken":"p2"}`)
			return
		}
		if call.query != "pageSize=1000&pageToken=p2" {
			t.Errorf("query = %q", call.query)
		}
		chatJSON(w, http.StatusOK, `{"models":[{"name":"models/gemini-2.5-pro","supportedGenerationMethods":["generateContent","countTokens"]}]}`)
	})
	adapter := geminiAdapter(t, upstream.url())
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	byID := map[string]providers.Model{}
	var ids []string
	for _, model := range models {
		byID[model.ID] = model
		ids = append(ids, model.ID)
	}
	if strings.Join(ids, ",") != "gemini-3-pro,gemma-3-27b,text-embedding-004,gemini-live-2,gemini-2.5-pro" {
		t.Fatalf("ids = %v", ids)
	}
	if byID["gemini-3-pro"].Kind != providers.KindChat {
		t.Errorf("kind = %q", byID["gemini-3-pro"].Kind)
	}
	if byID["gemini-3-pro"].ContextWindow == nil || *byID["gemini-3-pro"].ContextWindow != 1000000 {
		t.Errorf("context window = %v", byID["gemini-3-pro"].ContextWindow)
	}
	if byID["gemini-3-pro"].Reasoning == nil || !*byID["gemini-3-pro"].Reasoning {
		t.Error("a thinking model reasons")
	}
	if byID["gemma-3-27b"].Tools == nil || *byID["gemma-3-27b"].Tools {
		t.Error("gemma takes no tools")
	}
	if byID["text-embedding-004"].Kind != providers.KindEmbedding {
		t.Errorf("embedding kind = %q", byID["text-embedding-004"].Kind)
	}
	if byID["gemini-live-2"].Kind != providers.KindLive {
		t.Errorf("live kind = %q", byID["gemini-live-2"].Kind)
	}
}

func TestGeminiEmbed(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if !strings.Contains(r.URL.Path, ":batchEmbedContents") {
			t.Errorf("path = %q", r.URL.Path)
		}
		chatJSON(w, http.StatusOK, `{"embeddings":[{"values":[0.1,0.2]},{"values":[0.3,0.4]}]}`)
	})
	adapter := geminiAdapter(t, upstream.url())
	result, err := adapter.Embed(context.Background(), providers.EmbedRequest{Input: []string{"a", "b"}, InputType: "passage"}, providers.Call{ModelID: "text-embedding-001"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(result.Vectors) != 2 || result.Vectors[1][0] != 0.3 {
		t.Fatalf("vectors = %v", result.Vectors)
	}
	if result.InputTokens != nil {
		t.Error("Gemini reports no prompt tokens for embeddings")
	}
	sent := upstream.last(t)
	requests := sent.body["requests"].([]any)
	first := requests[0].(map[string]any)
	if first["model"] != "models/text-embedding-001" {
		t.Errorf("model = %v", first["model"])
	}
	// Only the first-generation model takes a task type.
	if first["taskType"] != "RETRIEVAL_DOCUMENT" {
		t.Errorf("taskType = %v", first["taskType"])
	}

	// A listing that does not match the inputs is unusable.
	mismatch := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"embeddings":[{"values":[0.1]}]}`)
	})
	adapter = geminiAdapter(t, mismatch.url())
	_, err = adapter.Embed(context.Background(), providers.EmbedRequest{Input: []string{"a", "b"}}, providers.Call{ModelID: "text-embedding-004"})
	if providers.KindOf(err) != providers.KindInvalidOutput {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
}

func TestGeminiSpeech(t *testing.T) {
	pcm := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=24000","data":"`+pcm+`"}}]}}]}`)
	})
	adapter := geminiAdapter(t, upstream.url())
	result, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "hi", Voice: "alloy"}, providers.Call{ModelID: "gemini-3-flash-tts"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	// Raw PCM gets a WAV header so players accept it.
	if result.Mime != "audio/wav" {
		t.Errorf("mime = %q", result.Mime)
	}
	if len(result.Audio) != 48 || string(result.Audio[0:4]) != "RIFF" || string(result.Audio[8:12]) != "WAVE" {
		t.Fatalf("audio is not a WAV file: %v bytes", len(result.Audio))
	}
	sent := upstream.last(t)
	config := sent.body["generationConfig"].(map[string]any)
	voice := config["speechConfig"].(map[string]any)["voiceConfig"].(map[string]any)["prebuiltVoiceConfig"].(map[string]any)["voiceName"]
	if voice != "Kore" {
		t.Errorf("voice = %v, want the Gemini name an OpenAI name maps to", voice)
	}
	modalities := config["responseModalities"].([]any)
	if modalities[0] != "AUDIO" {
		t.Errorf("responseModalities = %v", modalities)
	}
}

func TestGeminiVoiceNames(t *testing.T) {
	cases := map[string]string{
		"alloy": "Kore", "echo": "Puck", "nova": "Aoede", "Kore": "Kore", "onward": "Kore", "schedar": "Schedar",
	}
	for voice, want := range cases {
		if got := geminiVoice(voice); got != want {
			t.Errorf("geminiVoice(%q) = %q, want %q", voice, got, want)
		}
	}
}

func TestGeminiTranscribe(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"candidates":[{"content":{"parts":[{"audioTranscription":{"text":"hello there"}}]}}],"usageMetadata":{"promptTokenCount":9}}`)
	})
	adapter := geminiAdapter(t, upstream.url())
	result, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte("audio"), Mime: "audio/wav", Language: "de"}, providers.Call{ModelID: "gemini-3-flash"})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "hello there" {
		t.Errorf("text = %q", result.Text)
	}
	if result.Language != "de" {
		t.Errorf("language = %q", result.Language)
	}
	if result.Segments != nil {
		t.Error("Gemini reports no timestamps")
	}
	sent := upstream.last(t)
	parts := sent.body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	instruction := parts[0].(map[string]any)["text"].(string)
	if !strings.Contains(instruction, "language: de") {
		t.Errorf("instruction = %q", instruction)
	}
	if parts[1].(map[string]any)["inlineData"].(map[string]any)["mimeType"] != "audio/wav" {
		t.Errorf("the audio was not sent inline: %v", parts[1])
	}

	// Translation asks for English.
	if _, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte("audio"), Mime: "audio/wav", Translate: true}, providers.Call{ModelID: "gemini-3-flash"}); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	translated := upstream.last(t).body["contents"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(translated, "Translate the speech") {
		t.Errorf("instruction = %q", translated)
	}
}

func TestGeminiGenerateImage(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"here"},{"inlineData":{"mimeType":"image/png","data":"`+payload+`"}}]}}]}`)
	})
	adapter := geminiAdapter(t, upstream.url())
	result, err := adapter.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat", N: 2, Size: "1280x720"}, providers.Call{ModelID: "gemini-3-pro-image"})
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if len(result.Images) != 2 {
		t.Fatalf("images = %d, want one per requested image: %v", len(result.Images), result.Images)
	}
	if result.Images[0].B64 != payload || result.Images[0].Mime != "image/png" {
		t.Errorf("image = %+v", result.Images[0])
	}
	config := upstream.last(t).body["generationConfig"].(map[string]any)
	if config["imageConfig"].(map[string]any)["aspectRatio"] != "16:9" {
		t.Errorf("aspectRatio = %v", config["imageConfig"])
	}
	modalities := config["responseModalities"].([]any)
	if len(modalities) != 2 || modalities[1] != "IMAGE" {
		t.Errorf("responseModalities = %v", modalities)
	}
}

func TestGeminiCapabilities(t *testing.T) {
	adapter := geminiAdapter(t, "http://127.0.0.1:1")
	for _, capability := range []providers.Capability{
		providers.CapChat, providers.CapEmbed, providers.CapSpeech, providers.CapTranscribe, providers.CapGenerateImage,
	} {
		if !adapter.Supports(capability) {
			t.Errorf("%s should be supported", capability)
		}
	}
	if adapter.Supports(providers.CapCreateVideo) {
		t.Error("Gemini makes no videos here")
	}
	if adapter.TranscribeTimestamps() {
		t.Error("Gemini transcripts carry no timestamps")
	}
}
