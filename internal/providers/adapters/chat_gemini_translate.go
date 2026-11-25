package adapters

import (
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// geminiReasoningModel is the model family that takes a thinking setting.
var geminiReasoningModel = regexp.MustCompile(`gemini-(2\.5|[3-9])`)

// geminiRate reads the sample rate out of an audio/L16 MIME type.
var geminiRate = regexp.MustCompile(`rate=(\d+)`)

// The Gemini half of providers/adapters/google_gemini.js: one canonical request
// rendered as a generateContent body, Gemini's parts read back as canonical
// deltas, and the speech-request helpers Gemini's own dialect needs. google_gemini.js
// keeps this translation inside the adapter (unlike Anthropic's translator
// module), so it is not shared with the gateway.

// geminiFinish is Gemini's finish reason as the canonical one.
var geminiFinish = map[string]string{
	"STOP": "stop", "MAX_TOKENS": "length", "SAFETY": "content_filter",
	"RECITATION": "content_filter", "PROHIBITED_CONTENT": "content_filter",
	"BLOCKLIST": "content_filter", "SPII": "content_filter",
	"MALFORMED_FUNCTION_CALL": "stop",
}

// geminiSchemaKeys are the JSON-schema keys Gemini accepts. Anything else in a
// tool's schema is refused, so it is dropped.
var geminiSchemaKeys = []string{
	"type", "format", "description", "nullable", "enum", "properties", "required",
	"items", "anyOf", "minimum", "maximum", "minItems", "maxItems", "minLength",
	"maxLength", "pattern", "title", "propertyOrdering",
}

// geminiSignatureBypass is Google's documented value for a function call whose
// thought signature is unknown: OpenAI-style clients drop the signature, and a
// thinking model refuses a call that comes back without one.
const geminiSignatureBypass = "skip_thought_signature_validator"

// geminiMaxSignatures bounds the signature memory.
const geminiMaxSignatures = 5000

// geminiSignatures remembers the thought signature of each tool call, bounded
// and oldest-first, so the next turn can echo it back. google_gemini.js keeps
// one map for the process; one cache per account keeps two accounts' signatures
// apart.
type geminiSignatures struct {
	mu    sync.Mutex
	order []string
	byID  map[string]string
}

// remember stores one signature, dropping the oldest when the bound is reached.
func (s *geminiSignatures) remember(id, signature string) {
	if signature == "" || id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = map[string]string{}
	}
	if _, known := s.byID[id]; !known {
		s.order = append(s.order, id)
	}
	s.byID[id] = signature
	for len(s.order) > geminiMaxSignatures {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.byID, oldest)
	}
}

// signature reads one signature, the bypass value when unknown.
func (s *geminiSignatures) signature(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value, ok := s.byID[id]; ok {
		return value
	}
	return geminiSignatureBypass
}

// geminiSchema drops the JSON-schema keys Gemini does not accept and turns a
// union type into one type plus nullable.
func geminiSchema(schema any) any {
	switch value := schema.(type) {
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = geminiSchema(item)
		}
		return out
	case *transport.Obj:
		out := transport.NewObj()
		for _, key := range value.Keys() {
			if !chatInList(geminiSchemaKeys, key) {
				continue
			}
			inner, _ := value.Get(key)
			switch key {
			case "type":
				if types, ok := inner.([]any); ok {
					kept := make([]any, 0, len(types))
					nullable := false
					for _, item := range types {
						if item == "null" {
							nullable = true
							continue
						}
						kept = append(kept, item)
					}
					first := any("string")
					if len(kept) > 0 {
						first = kept[0]
					}
					out.Set("type", first)
					if nullable {
						out.Set("nullable", true)
					}
					continue
				}
				out.Set(key, inner)
			case "properties":
				properties := transport.NewObj()
				if source := chatObj(inner); source != nil {
					for _, name := range source.Keys() {
						inner2, _ := source.Get(name)
						properties.Set(name, geminiSchema(inner2))
					}
				} else if source, ok := inner.(map[string]any); ok {
					for name, plain := range source {
						properties.Set(name, geminiSchema(plain))
					}
				}
				out.Set("properties", properties)
			default:
				out.Set(key, geminiSchema(inner))
			}
		}
		return out
	}
	return schema
}

// geminiRequest is one canonical request as a generateContent body.
func (a *googleGemini) geminiRequest(canonical *transport.Obj, modelID string) (*transport.Obj, error) {
	var system []string
	contents := []any{}
	toolNames := map[string]string{}
	push := func(role string, parts []any) {
		if len(parts) == 0 {
			return
		}
		if len(contents) > 0 {
			last := chatObj(contents[len(contents)-1])
			if chatField(last, "role") == role {
				last.Set("parts", append(chatList(chatGet(last, "parts")), parts...))
				return
			}
		}
		contents = append(contents, transport.NewObj().Set("role", role).Set("parts", parts))
	}
	for _, message := range chatMessages(canonical) {
		switch chatField(message, "role") {
		case "system", "developer":
			system = append(system, chatTextOf(chatGet(message, "content")))
		case "assistant":
			var parts []any
			if text := chatTextOf(chatGet(message, "content")); text != "" {
				parts = append(parts, transport.NewObj().Set("text", text))
			}
			for _, raw := range chatList(chatGet(message, "tool_calls")) {
				call := chatObj(raw)
				fn := chatObj(chatGet(call, "function"))
				id := chatField(call, "id")
				toolNames[id] = chatField(fn, "name")
				parts = append(parts, transport.NewObj().
					Set("functionCall", transport.NewObj().
						Set("name", chatGet(fn, "name")).
						Set("args", chatToolArguments(chatField(fn, "arguments")))).
					Set("thoughtSignature", a.signatures.signature(id)))
			}
			push("model", parts)
		case "tool":
			raw := chatTextOf(chatGet(message, "content"))
			var result any = raw
			if decoded, err := transport.ParseOrdered([]byte(raw)); err == nil {
				result = decoded
			}
			name := toolNames[chatField(message, "tool_call_id")]
			if name == "" {
				name = "tool"
			}
			push("user", []any{transport.NewObj().Set("functionResponse", transport.NewObj().
				Set("name", name).
				Set("response", transport.NewObj().Set("result", result)))})
		default:
			var parts []any
			for _, part := range chatContentParts(chatGet(message, "content")) {
				switch chatField(part, "type") {
				case "text":
					parts = append(parts, transport.NewObj().Set("text", chatField(part, "text")))
				case "image_url":
					image, err := a.geminiImagePart(part)
					if err != nil {
						return nil, err
					}
					parts = append(parts, image)
				}
			}
			push("user", parts)
		}
	}
	body := transport.NewObj().Set("contents", contents)
	if len(system) > 0 {
		body.Set("systemInstruction", transport.NewObj().Set("parts", []any{transport.NewObj().Set("text", strings.Join(system, "\n\n"))}))
	}
	config := transport.NewObj()
	if value, ok := chatNumber(canonical, "temperature"); ok {
		config.Set("temperature", value)
	}
	if value, ok := chatNumber(canonical, "top_p"); ok {
		config.Set("topP", value)
	}
	if value, ok := chatNumber(canonical, "max_tokens"); ok {
		config.Set("maxOutputTokens", chatCount(value))
	}
	if stop, present := canonical.Get("stop"); present && stop != nil {
		if items, isList := stop.([]any); isList {
			config.Set("stopSequences", items)
		} else {
			config.Set("stopSequences", []any{stop})
		}
	}
	if seed, present := canonical.Get("seed"); present && seed != nil {
		config.Set("seed", seed)
	}
	if format := chatObj(chatGet(canonical, "response_format")); format != nil {
		switch chatField(format, "type") {
		case "json_object":
			config.Set("responseMimeType", "application/json")
		case "json_schema":
			config.Set("responseMimeType", "application/json")
			if schema := chatGet(chatObj(chatGet(format, "json_schema")), "schema"); schema != nil {
				config.Set("responseSchema", geminiSchema(chatObj(schema)))
			}
		}
	}
	if effort := chatField(canonical, "reasoning_effort"); effort != "" && geminiReasoningModel.MatchString(modelID) {
		if effort == "minimal" {
			effort = "low"
		}
		if strings.Contains(modelID, "gemini-2.5") {
			budget, known := map[string]any{"low": 1024, "medium": 8192, "high": 24576}[effort]
			if !known {
				budget = -1
			}
			config.Set("thinkingConfig", transport.NewObj().Set("thinkingBudget", budget))
		} else {
			level := "low"
			if effort == "high" {
				level = "high"
			}
			config.Set("thinkingConfig", transport.NewObj().Set("thinkingLevel", level))
		}
	}
	if len(config.Keys()) > 0 {
		body.Set("generationConfig", config)
	}
	if tools := chatList(chatGet(canonical, "tools")); len(tools) > 0 {
		declarations := make([]any, 0, len(tools))
		for _, raw := range tools {
			fn := chatObj(chatGet(chatObj(raw), "function"))
			parameters := chatGet(fn, "parameters")
			if parameters == nil {
				parameters = transport.NewObj().Set("type", "object").Set("properties", transport.NewObj())
			}
			declaration := transport.NewObj().
				Set("name", chatGet(fn, "name")).
				Set("description", chatDescription(fn)).
				Set("parameters", geminiSchema(parameters))
			declarations = append(declarations, declaration)
		}
		body.Set("tools", []any{transport.NewObj().Set("functionDeclarations", declarations)})
		switch choice := chatGet(canonical, "tool_choice").(type) {
		case string:
			switch choice {
			case "required":
				body.Set("toolConfig", transport.NewObj().Set("functionCallingConfig", transport.NewObj().Set("mode", "ANY")))
			case "none":
				body.Set("toolConfig", transport.NewObj().Set("functionCallingConfig", transport.NewObj().Set("mode", "NONE")))
			}
		case *transport.Obj:
			if name := chatField(chatObj(chatGet(choice, "function")), "name"); name != "" {
				body.Set("toolConfig", transport.NewObj().Set("functionCallingConfig", transport.NewObj().
					Set("mode", "ANY").
					Set("allowedFunctionNames", []any{name})))
			}
		}
	}
	return body, nil
}

// geminiUsage is Gemini's usage metadata as the canonical usage block.
func geminiUsage(metadata *transport.Obj) *transport.Obj {
	if metadata == nil {
		return nil
	}
	input, _ := chatNumber(metadata, "promptTokenCount")
	candidates, _ := chatNumber(metadata, "candidatesTokenCount")
	thoughts, _ := chatNumber(metadata, "thoughtsTokenCount")
	cached, _ := chatNumber(metadata, "cachedContentTokenCount")
	output := candidates + thoughts
	return transport.NewObj().
		Set("prompt_tokens", chatCount(input)).
		Set("prompt_tokens_details", transport.NewObj().Set("cached_tokens", chatCount(cached))).
		Set("completion_tokens", chatCount(output)).
		Set("total_tokens", chatCount(input+output))
}

// geminiDelta reads one candidate's parts as a canonical delta. The counter
// numbers the tool calls across the whole stream.
func (a *googleGemini) geminiDelta(parts []any, counter *int) *transport.Obj {
	delta := transport.NewObj()
	var toolCalls []any
	content, reasoning := "", ""
	for _, raw := range parts {
		part := chatObj(raw)
		if call := chatObj(chatGet(part, "functionCall")); call != nil {
			id := chatField(call, "id")
			if id == "" {
				id = chatCallID()
			}
			a.signatures.remember(id, chatField(part, "thoughtSignature"))
			toolCalls = append(toolCalls, transport.NewObj().
				Set("index", *counter).
				Set("id", id).
				Set("type", "function").
				Set("function", transport.NewObj().
					Set("name", chatGet(call, "name")).
					Set("arguments", chatJSONText(chatGet(call, "args")))))
			*counter++
			continue
		}
		if text, ok := chatGet(part, "text").(string); ok {
			if chatTruthy(chatGet(part, "thought")) {
				reasoning += text
			} else {
				content += text
			}
		}
	}
	if reasoning != "" {
		delta.Set("reasoning_content", reasoning)
	}
	if content != "" {
		delta.Set("content", content)
	}
	if len(toolCalls) > 0 {
		delta.Set("tool_calls", toolCalls)
	}
	return delta
}

// geminiMessages reads a delta as a canonical assistant message: the tool-call
// indexes belong to the stream, not to the answer.
func geminiMessage(delta *transport.Obj) *transport.Obj {
	content := chatGet(delta, "content")
	hasTools := len(chatList(chatGet(delta, "tool_calls"))) > 0
	if !chatTruthy(content) {
		if hasTools {
			content = nil
		} else {
			content = ""
		}
	}
	message := transport.NewObj().Set("role", "assistant").Set("content", content)
	if hasTools {
		var calls []any
		for _, raw := range chatList(chatGet(delta, "tool_calls")) {
			call := chatObj(raw)
			trimmed := transport.NewObj()
			for _, key := range call.Keys() {
				if key == "index" {
					continue
				}
				value, _ := call.Get(key)
				trimmed.Set(key, value)
			}
			calls = append(calls, trimmed)
		}
		message.Set("tool_calls", calls)
	}
	if reasoning := chatGet(delta, "reasoning_content"); chatTruthy(reasoning) {
		message.Set("reasoning_content", reasoning)
	}
	return message
}

// geminiCandidateCheck turns a Gemini refusal into a classified failure: an
// error body, or a prompt Gemini blocked before answering.
func geminiCandidateCheck(data *transport.Obj) error {
	if chatTruthy(chatGet(data, "error")) {
		status := 502
		if code, ok := chatNumber(chatObj(chatGet(data, "error")), "code"); ok && code != 0 {
			status = int(code)
		}
		return chatClassifyOrdered(status, data)
	}
	feedback := chatObj(chatGet(data, "promptFeedback"))
	if reason := chatField(feedback, "blockReason"); reason != "" && len(chatList(chatGet(data, "candidates"))) == 0 {
		body := transport.NewObj().Set("error", transport.NewObj().Set("message", "Prompt blocked: "+reason))
		return providers.ClassifyHTTP(400, chatPlainValue(body), nil)
	}
	return nil
}

// Gemini's own voice names, and the OpenAI names clients send.
var (
	geminiOpenAIVoices = map[string]string{
		"alloy": "Kore", "echo": "Puck", "fable": "Fenrir", "onyx": "Charon",
		"nova": "Aoede", "shimmer": "Leda", "ash": "Orus", "coral": "Callirrhoe",
		"sage": "Schedar", "ballad": "Achird", "verse": "Zephyr",
	}
	geminiVoices = map[string]bool{
		"Zephyr": true, "Puck": true, "Charon": true, "Kore": true, "Fenrir": true,
		"Leda": true, "Orus": true, "Aoede": true, "Callirrhoe": true, "Autonoe": true,
		"Enceladus": true, "Iapetus": true, "Umbriel": true, "Algieba": true,
		"Despina": true, "Erinome": true, "Algenib": true, "Rasalgethi": true,
		"Laomedeia": true, "Achernar": true, "Alnilam": true, "Schedar": true,
		"Gacrux": true, "Pulcherrima": true, "Achird": true, "Zubenelgenubi": true,
		"Vindemiatrix": true, "Sadachbia": true, "Sadaltager": true, "Sulafat": true,
	}
)

// geminiVoice maps a requested voice name onto Gemini's own.
func geminiVoice(voice string) string {
	if voice == "" {
		return "Kore"
	}
	proper := strings.ToUpper(voice[:1]) + strings.ToLower(voice[1:])
	if geminiVoices[proper] {
		return proper
	}
	if mapped, ok := geminiOpenAIVoices[strings.ToLower(voice)]; ok {
		return mapped
	}
	return "Kore"
}

// geminiWAVFromPCM wraps raw 16-bit PCM ("audio/L16;rate=24000") in a WAV
// header, so players accept it.
func geminiWAVFromPCM(pcm []byte, mime string) []byte {
	rate := 24000
	if match := geminiRate.FindStringSubmatch(mime); match != nil {
		if parsed, err := strconv.Atoi(match[1]); err == nil && parsed > 0 {
			rate = parsed
		}
	}
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(pcm)))
	copy(header[8:], "WAVE")
	copy(header[12:], "fmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(pcm)))
	return append(header, pcm...)
}
