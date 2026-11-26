package processing

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The privacy shield: replaces sensitive values with typed, numbered
// placeholders ("[EMAIL_1]") before a request leaves the server, and puts the
// real values back into the answer. The same value always gets the same
// placeholder in a request, so the model can still tell people and accounts
// apart. The mapping lives only in memory for the duration of one request.
//
// Detection lives in the shield packs (rules with checksums and context words,
// code detectors for names, addresses and seed phrases, and optionally the
// entities a privacy filter finds).

// Models rarely repeat a placeholder letter for letter: they add spaces, change
// the case, escape the brackets or the underscore the way Markdown does
// ("\[SECRET\_26\]"), swap the brackets for <>, {} or 【】, or drop them.
const (
	placeholderOpen  = `(?:\\?\[|[<{【])`
	placeholderClose = `(?:\\?\]|[>}】])`
)

var (
	// restorePattern is a placeholder in any of the shapes a model writes: brackets
	// (or \[ \] { } 【 】) around the token, or the bare token, which only counts
	// when it is in the vault. Groups 1 and 2 are the bracketed token and number,
	// groups 3 and 4 the bare ones.
	restorePattern = mustJS(placeholderOpen + `\s*([A-Za-z]{2,12})(?:\\?_|[ -])?(\d{1,4})\s*` + placeholderClose + `|` + `(?<![A-Za-z0-9_])([A-Z]{2,12})\\?_(\d{1,4})(?![A-Za-z0-9_])`)
	// partialPlaceholderPattern matches text from an unfinished "[" that may still
	// become a placeholder once more chunks arrive.
	partialPlaceholderPattern = mustJS(`(?:` + placeholderOpen + `\s*[A-Za-z]{0,12}(?:\\?_|[ -])?\d{0,4}\s*\\?|(?<![A-Za-z0-9_])[A-Z]{1,12}\\?_?\d{0,4}|\\)$`)
)

// mustJS compiles a pattern the packs rely on; a bad one is a programming error.
func mustJS(source string) *jsPattern {
	pattern, err := compileJSPattern(source, "")
	if err != nil {
		panic("processing: " + source + ": " + err.Error())
	}
	return pattern
}

// Finding is one hidden value as the settings preview shows it.
type Finding struct {
	Token    string `json:"token"`
	Category string `json:"category"`
	Label    Label  `json:"label"`
}

// VaultEntry is what one placeholder stands for.
type VaultEntry struct {
	Value    string
	Category string
	RuleID   string
	// Restore is false for a category the user chose to hide for good.
	Restore bool
}

// Vault is one request's mapping between real values and placeholders.
type Vault struct {
	engine *Engine
	modes  map[string]string

	byValue  map[string]string
	byToken  map[string]*VaultEntry
	counters map[string]int
	order    []string
	// Counts is how many values were hidden per category.
	Counts map[string]int
}

// NewVault builds an empty vault for one request. engine may be nil when only
// TokenFor and RestoreText are used.
func NewVault(engine *Engine, modes map[string]string) *Vault {
	return &Vault{
		engine:   engine,
		modes:    modes,
		byValue:  map[string]string{},
		byToken:  map[string]*VaultEntry{},
		counters: map[string]int{},
		Counts:   map[string]int{},
	}
}

// TokenFor is the placeholder for one value: the same value always gets the same
// token, and each category numbers its own.
func (v *Vault) TokenFor(category, value, ruleID string) string {
	key := category + "\x00" + value
	if token, ok := v.byValue[key]; ok {
		return token
	}
	tokenType, ok := TokenTypes[category]
	if !ok {
		tokenType = strings.ToUpper(category)
	}
	next := v.counters[tokenType] + 1
	v.counters[tokenType] = next
	stem := tokenType + "_" + strconv.Itoa(next)
	token := "[" + stem + "]"
	v.byValue[key] = token
	v.byToken[stem] = &VaultEntry{Value: value, Category: category, RuleID: ruleID, Restore: v.modes[category] == "restore"}
	v.order = append(v.order, stem)
	v.Counts[category]++
	return token
}

// Size is the number of hidden values.
func (v *Vault) Size() int { return len(v.byToken) }

// VaultItem is one hidden value: its placeholder stem ("EMAIL_1") and what it
// stands for.
type VaultItem struct {
	Token string
	Entry *VaultEntry
}

// Entries iterates the vault in the order values were hidden.
func (v *Vault) Entries() []VaultItem {
	out := make([]VaultItem, 0, len(v.order))
	for _, stem := range v.order {
		out = append(out, VaultItem{Token: stem, Entry: v.byToken[stem]})
	}
	return out
}

// Lookup is the entry a placeholder stem stands for ("EMAIL_1").
func (v *Vault) Lookup(stem string) (*VaultEntry, bool) {
	entry, ok := v.byToken[stem]
	return entry, ok
}

// RedactOptions is what may be hidden in one text, plus the AI entities for it.
type RedactOptions struct {
	Modes       map[string]string
	CustomTerms []string
	AllowTerms  []string
	Entities    []Entity
}

// Detect is every private span of a text under this vault's modes.
func (v *Vault) Detect(text string, options RedactOptions) []Span {
	if v.engine == nil {
		return nil
	}
	modes := options.Modes
	if modes == nil {
		modes = v.modes
	}
	return v.engine.Scan(text, ScanOptions{
		Modes:       modes,
		CustomTerms: options.CustomTerms,
		AllowTerms:  options.AllowTerms,
		Entities:    options.Entities,
	})
}

// RedactText replaces the sensitive values of one text with placeholders.
func (v *Vault) RedactText(text string, options RedactOptions) string {
	spans := v.Detect(text, options)
	if len(spans) == 0 {
		return text
	}
	var out strings.Builder
	cursor := 0
	for _, span := range spans {
		out.WriteString(text[cursor:span.Start])
		out.WriteString(v.TokenFor(span.Category, span.Value, span.RuleID))
		cursor = span.End
	}
	out.WriteString(text[cursor:])
	return out.String()
}

// Findings is what was hidden and which rule found it, for the settings preview.
func (v *Vault) Findings() []Finding {
	findings := make([]Finding, 0, len(v.order))
	for _, stem := range v.order {
		entry := v.byToken[stem]
		label := Label{}
		if v.engine != nil {
			label = v.engine.Registry().LabelOf(entry.RuleID)
		}
		findings = append(findings, Finding{Token: "[" + stem + "]", Category: entry.Category, Label: label})
	}
	return findings
}

// RestoreText puts the real values back, tolerating the changes models make
// ("[EMAIL 1]", "[ email_1 ]", "\[SECRET\_26\]"). inJSONString escapes values
// with quotes, backslashes or line breaks so that JSON stays valid.
func (v *Vault) RestoreText(text string, inJSONString bool) string {
	if text == "" || len(v.byToken) == 0 {
		return text
	}
	var out strings.Builder
	cursor := 0
	iteratePattern(restorePattern, text, func(loc []int) bool {
		start, end := restorePattern.span(loc)
		name := groupText(text, loc, restorePattern, restorePattern.groupOf[1])
		number := groupText(text, loc, restorePattern, restorePattern.groupOf[2])
		if name == "" {
			name = groupText(text, loc, restorePattern, restorePattern.groupOf[3])
			number = groupText(text, loc, restorePattern, restorePattern.groupOf[4])
		}
		out.WriteString(text[cursor:start])
		entry, ok := v.lookupFold(name, number)
		if !ok || !entry.Restore {
			out.WriteString(text[start:end])
		} else if inJSONString {
			out.WriteString(escapeJSONStringBody(entry.Value))
		} else {
			out.WriteString(entry.Value)
		}
		cursor = end
		return true
	})
	out.WriteString(text[cursor:])
	return out.String()
}

// lookupFold finds a token by its written name and number: the name may arrive in
// any case and the number with leading zeros.
func (v *Vault) lookupFold(name, number string) (*VaultEntry, bool) {
	value, err := strconv.Atoi(number)
	if err != nil {
		return nil, false
	}
	return v.Lookup(strings.ToUpper(name) + "_" + strconv.Itoa(value))
}

// groupText is one capture group of a match as text, "" when it did not take part.
func groupText(text string, loc []int, pattern *jsPattern, group int) string {
	if group <= 0 || 2*group+1 >= len(loc) {
		return ""
	}
	start, end := loc[2*group], loc[2*group+1]
	if start < 0 || end < start {
		return ""
	}
	return text[start:end]
}

// escapeJSONStringBody is JSON.stringify(value) without the quotes.
func escapeJSONStringBody(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) < 2 {
		return value
	}
	return string(encoded[1 : len(encoded)-1])
}

// Redaction is the result of redacting one request.
type Redaction struct {
	Canonical Canonical
	Vault     *Vault
}

// RequestRedactOptions is what a request may hide, plus where the AI entities per
// message come from.
type RequestRedactOptions struct {
	Modes       map[string]string
	CustomTerms []string
	AllowTerms  []string
	// EntitiesFor gives the entities the AI pass found in message index i.
	EntitiesFor func(index int) []Entity
}

// RedactRequest redacts every text in a canonical request. It returns nil when
// nothing was found.
func RedactRequest(engine *Engine, canonical Canonical, options RequestRedactOptions) *Redaction {
	vault := NewVault(engine, options.Modes)
	messages := canonical.Messages()
	redacted := make([]map[string]any, 0, len(messages))
	for index, message := range messages {
		entities := []Entity(nil)
		if options.EntitiesFor != nil {
			entities = options.EntitiesFor(index)
		}
		redact := func(text string) string {
			return vault.RedactText(text, RedactOptions{
				Modes:       options.Modes,
				CustomTerms: options.CustomTerms,
				AllowTerms:  options.AllowTerms,
				Entities:    entities,
			})
		}
		redacted = append(redacted, mapMessageText(message, redact, func(text string) string {
			return mapArguments(text, redact, redact)
		}))
	}
	if vault.Size() == 0 {
		return nil
	}
	instruction := texts().RedactionInstruction
	systemIndex := -1
	for index, message := range redacted {
		if messageRole(message) == "system" {
			systemIndex = index
			break
		}
	}
	if systemIndex >= 0 {
		if content, ok := redacted[systemIndex]["content"].(string); ok {
			copied := cloneMap(redacted[systemIndex])
			copied["content"] = content + "\n\n" + instruction
			redacted[systemIndex] = copied
		}
	} else {
		redacted = append([]map[string]any{{"role": "system", "content": instruction}}, redacted...)
	}
	return &Redaction{Canonical: canonical.WithMessages(redacted), Vault: vault}
}

// RestoreResponse puts every placeholder back in an answer: text, reasoning and
// tool-call arguments.
func RestoreResponse(response map[string]any, vault *Vault) map[string]any {
	next := cloneMap(response)
	choices, _ := response["choices"].([]any)
	restored := make([]any, len(choices))
	for index, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			restored[index] = raw
			continue
		}
		copied := cloneMap(choice)
		if message, ok := choice["message"].(map[string]any); ok {
			copied["message"] = restoreMessage(message, vault)
		}
		restored[index] = copied
	}
	next["choices"] = restored
	return next
}

// restoreMessage restores the text, reasoning and tool-call arguments of one
// answer message.
func restoreMessage(message map[string]any, vault *Vault) map[string]any {
	next := mapMessageText(message,
		func(text string) string { return vault.RestoreText(text, false) },
		func(text string) string { return vault.RestoreText(text, true) },
	)
	for _, field := range []string{"reasoning", "reasoning_content", "refusal"} {
		if value, ok := next[field].(string); ok {
			next[field] = vault.RestoreText(value, false)
		}
	}
	return next
}

// StreamRestorer restores placeholders in a streamed answer, holding back text
// from an unfinished "[" until it can be decided.
type StreamRestorer struct {
	vault   *Vault
	pending map[string]string
	order   []string
}

// NewStreamRestorer builds a restorer for one streamed answer.
func NewStreamRestorer(vault *Vault) *StreamRestorer {
	return &StreamRestorer{vault: vault, pending: map[string]string{}}
}

// Take restores one piece of a streamed text, holding back an unfinished
// placeholder.
func (s *StreamRestorer) Take(key, text string, final bool, inJSONString bool) string {
	buffer := s.pending[key] + text
	if final {
		delete(s.pending, key)
		return s.vault.RestoreText(buffer, inJSONString)
	}
	cut := len(buffer)
	if start, ok := partialStart(buffer); ok {
		if len([]rune(buffer[start:])) <= 24 {
			cut = start
		}
	}
	if cut == len(buffer) {
		s.pending[key] = ""
	} else {
		s.pending[key] = buffer[cut:]
	}
	s.track(key)
	return s.vault.RestoreText(buffer[:cut], inJSONString)
}

func (s *StreamRestorer) track(key string) {
	if _, ok := s.pending[key]; !ok {
		return
	}
	for _, existing := range s.order {
		if existing == key {
			return
		}
	}
	s.order = append(s.order, key)
}

// partialStart is the byte offset where text from an unfinished "[" starts, or
// false when the text can be sent as it is.
func partialStart(buffer string) (int, bool) {
	for _, loc := range partialPlaceholderPattern.re.FindAllStringSubmatchIndex(buffer, 1) {
		return loc[0], true
	}
	return 0, false
}

// Process restores one canonical stream chunk; the final chunk flushes.
func (s *StreamRestorer) Process(chunk map[string]any) map[string]any {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return chunk
	}
	next := cloneMap(chunk)
	restored := make([]any, len(choices))
	for index, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			restored[index] = raw
			continue
		}
		finishReason := choice["finish_reason"]
		final := finishReason != nil && finishReason != ""
		delta := map[string]any{}
		if existing, ok := choice["delta"].(map[string]any); ok {
			delta = cloneMap(existing)
		}
		choiceIndex := 0
		if value, ok := choice["index"].(float64); ok {
			choiceIndex = int(value)
		}
		for _, field := range []string{"content", "reasoning", "reasoning_content"} {
			text, isText := delta[field].(string)
			key := strconv.Itoa(choiceIndex) + ":" + field
			if isText || (final && s.has(key)) {
				delta[field] = s.Take(key, text, final, false)
			}
		}
		if calls, ok := delta["tool_calls"].([]any); ok {
			mapped := make([]any, len(calls))
			for callIndex, rawCall := range calls {
				call, ok := rawCall.(map[string]any)
				if !ok {
					mapped[callIndex] = rawCall
					continue
				}
				function, ok := call["function"].(map[string]any)
				if !ok {
					mapped[callIndex] = rawCall
					continue
				}
				arguments, ok := function["arguments"].(string)
				if !ok {
					mapped[callIndex] = rawCall
					continue
				}
				index := 0
				if value, ok := call["index"].(float64); ok {
					index = int(value)
				}
				key := strconv.Itoa(choiceIndex) + ":tool:" + strconv.Itoa(index)
				copiedFunction := cloneMap(function)
				copiedFunction["arguments"] = s.Take(key, arguments, final, true)
				copiedCall := cloneMap(call)
				copiedCall["function"] = copiedFunction
				mapped[callIndex] = copiedCall
			}
			delta["tool_calls"] = mapped
		}
		copied := cloneMap(choice)
		copied["delta"] = delta
		restored[index] = copied
	}
	next["choices"] = restored
	return next
}

func (s *StreamRestorer) has(key string) bool {
	_, ok := s.pending[key]
	return ok
}

// Flush is whatever is still held when the stream ends without a finish chunk.
func (s *StreamRestorer) Flush(template map[string]any) map[string]any {
	if len(s.pending) == 0 {
		return nil
	}
	delta := map[string]any{}
	for _, key := range s.order {
		value, ok := s.pending[key]
		if !ok {
			continue
		}
		parts := strings.Split(key, ":")
		field := parts[len(parts)-1]
		if field != "tool" && value != "" {
			delta[field] = s.vault.RestoreText(value, false)
		}
	}
	s.pending = map[string]string{}
	s.order = nil
	if len(delta) == 0 {
		return nil
	}
	next := cloneMap(template)
	next["choices"] = []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}
	return next
}
