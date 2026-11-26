package processing

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Prompt compression in levels. Every engine works on the canonical request, only
// ever touches text (never images or tool-call structure) and is kept only when
// it makes the request smaller.
//
//	lossless    whitespace clean-up, minified JSON, duplicate blocks dropped
//	balanced    + repeated log lines collapsed, uniform JSON lists as tables,
//	              very long tool outputs cut to their head and tail
//	aggressive  + older tool outputs hidden (observation masking), older long
//	              turns condensed, optional hard token target
//
// System messages and the latest user message only ever get lossless clean-up.
// Lossy engines start only above a size threshold, so short chats pass through
// untouched.

// Levels are the compression levels, in order.
var Levels = []string{"off", "lossless", "balanced", "aggressive"}

// Engines maps an engine name to the level it needs.
var Engines = map[string]string{
	"whitespace":        "lossless",
	"jsonMinify":        "lossless",
	"dedupe":            "lossless",
	"repeatedLines":     "balanced",
	"jsonTables":        "balanced",
	"trimToolOutput":    "balanced",
	"maskOldToolOutput": "aggressive",
	"condenseHistory":   "aggressive",
	"tokenTarget":       "aggressive",
}

// keepsInformation marks the steps that leave content out: only they wait for the
// size threshold.
var keepsInformation = map[string]bool{
	"whitespace":    true,
	"jsonMinify":    true,
	"dedupe":        true,
	"repeatedLines": true,
	"jsonTables":    true,
}

const (
	dedupeMinChars   = 400
	toolLimitOlder   = 4000
	toolLimitLatest  = 16000
	maskKeepLast     = 3
	condenseMinChars = 1500
)

// CompressionDefaults is where the settings start.
type CompressionDefaults struct {
	Level        string          `json:"level"`
	Engines      map[string]bool `json:"engines"`
	MinTokens    int             `json:"minTokens"`
	TargetTokens *int            `json:"targetTokens"`
	KeepRecent   int             `json:"keepRecent"`
}

// DefaultCompression is {level: "off", engines: {}, minTokens: 2000,
// targetTokens: null, keepRecent: 6}.
func DefaultCompression() CompressionDefaults {
	return CompressionDefaults{Level: "off", Engines: map[string]bool{}, MinTokens: 2000, KeepRecent: 6}
}

// CompressionSettings is a resolved level plus its knobs.
type CompressionSettings struct {
	Level        string
	Engines      map[string]bool
	MinTokens    int
	TargetTokens *int
	KeepRecent   int
}

// CompressionStats is what one pass did.
type CompressionStats struct {
	Level        string         `json:"level"`
	BeforeTokens int            `json:"beforeTokens"`
	AfterTokens  int            `json:"afterTokens"`
	SavedTokens  int            `json:"savedTokens"`
	Engines      map[string]int `json:"engines"`
}

// CompressionResult is the request after compression plus its stats.
type CompressionResult struct {
	Canonical Canonical
	Stats     CompressionStats
}

var (
	codeBlockPattern = regexp.MustCompile("(?s)```.*?```")
	zeroWidthPattern = regexp.MustCompile(`[\x{200b}-\x{200d}\x{feff}]`)
	trailingSpaceRun = regexp.MustCompile(`(?m)[ \t]+$`)
	manyNewlines     = regexp.MustCompile(`\n{3,}`)
	jsonStartPattern = regexp.MustCompile(`^[[{]`)
)

// estimate is the token estimate of a text: about four characters per token.
func estimate(text string) int { return (len([]rune(text)) + 3) / 4 }

// textParts are the editable text positions of one message: its content when that
// is a string, or its "text" parts when the content is a list.
type textPart struct {
	message int
	part    int // -1 for a string content
}

func textParts(message map[string]any, index int) []textPart {
	switch content := message["content"].(type) {
	case string:
		return []textPart{{message: index, part: -1}}
	case []any:
		parts := []textPart{}
		for partIndex, raw := range content {
			part, ok := raw.(map[string]any)
			if !ok || part["type"] != "text" {
				continue
			}
			if _, ok := part["text"].(string); !ok {
				continue
			}
			parts = append(parts, textPart{message: index, part: partIndex})
		}
		return parts
	}
	return nil
}

func partText(messages []map[string]any, part textPart) string {
	message := messages[part.message]
	if part.part < 0 {
		text, _ := message["content"].(string)
		return text
	}
	content, _ := message["content"].([]any)
	item, _ := content[part.part].(map[string]any)
	text, _ := item["text"].(string)
	return text
}

func setPartText(messages []map[string]any, part textPart, value string) {
	message := messages[part.message]
	if part.part < 0 {
		message["content"] = value
		return
	}
	content, _ := message["content"].([]any)
	item, _ := content[part.part].(map[string]any)
	copied := cloneMap(item)
	copied["text"] = value
	content[part.part] = copied
}

func messageText(message map[string]any, index int) string {
	texts := []string{}
	for _, part := range textParts(message, index) {
		texts = append(texts, partText([]map[string]any{message}, textPart{message: 0, part: part.part}))
	}
	return strings.Join(texts, "\n")
}

// toolCallsJSON is the JSON of (message.tool_calls || ”).
func toolCallsJSON(message map[string]any) string {
	calls, ok := message["tool_calls"]
	if !ok || calls == nil {
		return `""`
	}
	encoded, err := marshalJSON(calls)
	if err != nil {
		return `""`
	}
	return encoded
}

func totalTokens(messages []map[string]any) int {
	total := 0
	for index, message := range messages {
		total += estimate(messageText(message, index)) + estimate(toolCallsJSON(message))
	}
	return total
}

// outsideCode applies a text transform outside fenced code blocks only.
func outsideCode(text string, transform func(string) string) string {
	blocks := codeBlockPattern.FindAllStringIndex(text, -1)
	if len(blocks) == 0 {
		return transform(text)
	}
	var out strings.Builder
	cursor := 0
	for _, block := range blocks {
		out.WriteString(transform(text[cursor:block[0]]))
		out.WriteString(text[block[0]:block[1]])
		cursor = block[1]
	}
	out.WriteString(transform(text[cursor:]))
	return out.String()
}

// whitespace strips zero-width characters and trailing spaces, and collapses runs
// of blank lines.
func whitespace(text string) string {
	cleaned := zeroWidthPattern.ReplaceAllString(text, "")
	return outsideCode(cleaned, func(chunk string) string {
		return manyNewlines.ReplaceAllString(trailingSpaceRun.ReplaceAllString(chunk, ""), "\n\n")
	})
}

// parseJSON decodes a text that is a whole JSON object or list.
func parseJSON(text string) (any, bool) {
	trimmed := strings.TrimSpace(text)
	if !jsonStartPattern.MatchString(trimmed) {
		return nil, false
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, false
	}
	return parsed, true
}

// jsonMinify minifies a JSON text.
func jsonMinify(text string) string {
	parsed, ok := parseJSON(text)
	if !ok {
		return text
	}
	encoded, err := marshalJSON(parsed)
	if err != nil {
		return text
	}
	return encoded
}

// tabulate turns [{a,b},{a,b},…] into {"columns":[a,b],"rows":[[…],[…]]}: same
// data, keys once. Columns are sorted, because a Go map has no key order.
func tabulate(value any) any {
	switch node := value.(type) {
	case []any:
		if len(node) >= 4 {
			objects := true
			for _, item := range node {
				if _, ok := item.(map[string]any); !ok {
					objects = false
					break
				}
			}
			if objects {
				first, _ := node[0].(map[string]any)
				columns := make([]string, 0, len(first))
				for key := range first {
					columns = append(columns, key)
				}
				sort.Strings(columns)
				uniform := true
				for _, item := range node {
					row, _ := item.(map[string]any)
					if len(row) != len(columns) {
						uniform = false
						break
					}
					for _, column := range columns {
						if _, ok := row[column]; !ok {
							uniform = false
							break
						}
					}
				}
				if uniform {
					rows := make([]any, 0, len(node))
					for _, item := range node {
						row, _ := item.(map[string]any)
						cells := make([]any, 0, len(columns))
						for _, column := range columns {
							cells = append(cells, tabulate(row[column]))
						}
						rows = append(rows, cells)
					}
					return map[string]any{"columns": columns, "rows": rows}
				}
			}
		}
		out := make([]any, 0, len(node))
		for _, item := range node {
			out = append(out, tabulate(item))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(node))
		for key, item := range node {
			out[key] = tabulate(item)
		}
		return out
	default:
		return value
	}
}

// jsonTables renders uniform JSON lists as tables.
func jsonTables(text string) string {
	parsed, ok := parseJSON(text)
	if !ok {
		return text
	}
	encoded, err := marshalJSON(tabulate(parsed))
	if err != nil {
		return text
	}
	return encoded
}

// RepeatedLines collapses runs of three or more identical non-blank lines.
func RepeatedLines(text string) string {
	lines := strings.Split(text, "\n")
	output := make([]string, 0, len(lines))
	for index := 0; index < len(lines); {
		run := 1
		for index+run < len(lines) && lines[index+run] == lines[index] && strings.TrimSpace(lines[index]) != "" {
			run++
		}
		output = append(output, lines[index])
		if run >= 3 {
			output = append(output, fmt.Sprintf(texts().RepeatedLine, run-1))
		} else {
			for extra := 1; extra < run; extra++ {
				output = append(output, lines[index])
			}
		}
		index += run
	}
	return strings.Join(output, "\n")
}

// HeadTail is the head and tail of a long text, cut at line breaks where possible.
func HeadTail(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	headSize := limit * 65 / 100
	tailSize := limit - headSize
	headEnd := lastIndexRune(runes, '\n', headSize)
	if headEnd < headSize*6/10 {
		headEnd = headSize
	}
	tailStart := indexRuneFrom(runes, '\n', len(runes)-tailSize)
	if tailStart < 0 || tailStart > len(runes)-tailSize*6/10 {
		tailStart = len(runes) - tailSize
	}
	omitted := tailStart - headEnd
	annotation := fmt.Sprintf(texts().OmittedChars, groupThousands(omitted))
	return string(runes[:headEnd]) + "\n" + annotation + "\n" + string(runes[tailStart:])
}

// lastIndexRune is the last index of r in runes[:limit].
func lastIndexRune(runes []rune, r rune, limit int) int {
	if limit > len(runes) {
		limit = len(runes)
	}
	for index := limit - 1; index >= 0; index-- {
		if runes[index] == r {
			return index
		}
	}
	return 0
}

// indexRuneFrom is the first index of r at or after from, or -1.
func indexRuneFrom(runes []rune, r rune, from int) int {
	if from < 0 {
		from = 0
	}
	for index := from; index < len(runes); index++ {
		if runes[index] == r {
			return index
		}
	}
	return -1
}

// groupThousands is Number.prototype.toLocaleString('en') for a count.
func groupThousands(value int) string {
	digits := strconv.Itoa(value)
	var out strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return out.String()
}

// ResolveCompression fills in defaults and applies the header level, which may
// only lower the configured one.
func ResolveCompression(raw CompressionDefaults, headerLevel string) CompressionSettings {
	settings := CompressionSettings{
		Level:        raw.Level,
		Engines:      map[string]bool{},
		MinTokens:    raw.MinTokens,
		TargetTokens: raw.TargetTokens,
		KeepRecent:   raw.KeepRecent,
	}
	for name, enabled := range raw.Engines {
		settings.Engines[name] = enabled
	}
	if levelIndex(settings.Level) < 0 {
		settings.Level = "off"
	}
	if headerIndex := levelIndex(headerLevel); headerIndex >= 0 && headerIndex < levelIndex(settings.Level) {
		settings.Level = headerLevel
	}
	return settings
}

func levelIndex(level string) int {
	for index, name := range Levels {
		if name == level {
			return index
		}
	}
	return -1
}

func engineEnabled(settings CompressionSettings, name string, lossyAllowed bool) bool {
	if settings.Level == "off" {
		return false
	}
	if enabled, ok := settings.Engines[name]; ok && !enabled {
		return false
	}
	required, ok := Engines[name]
	if !ok || levelIndex(required) > levelIndex(settings.Level) {
		return false
	}
	return keepsInformation[name] || lossyAllowed
}

// Compress runs the pipeline over one canonical request.
func Compress(canonical Canonical, raw CompressionDefaults, headerLevel string) CompressionResult {
	settings := ResolveCompression(raw, headerLevel)
	messages := canonical.Messages()
	before := totalTokens(messages)
	stats := CompressionStats{Level: settings.Level, BeforeTokens: before, AfterTokens: before, Engines: map[string]int{}}
	result := CompressionResult{Canonical: canonical, Stats: stats}
	if settings.Level == "off" {
		return result
	}

	working := make([]map[string]any, len(messages))
	for index, message := range messages {
		copied := cloneMap(message)
		if content, ok := message["content"].([]any); ok {
			copied["content"] = append([]any{}, content...)
		}
		working[index] = copied
	}
	lastUser := -1
	for index, message := range working {
		if messageRole(message) == "user" {
			lastUser = index
		}
	}
	protected := func(index int) bool {
		return messageRole(working[index]) == "system" || index == lastUser
	}
	lossyAllowed := before >= settings.MinTokens
	toolIndexes := []int{}
	for index, message := range working {
		if messageRole(message) == "tool" {
			toolIndexes = append(toolIndexes, index)
		}
	}
	recentFrom := len(working) - settings.KeepRecent

	// run applies one engine to the texts it may touch and keeps it only when it
	// saved tokens.
	run := func(name string, selectMessage func(message map[string]any, index int) bool, transform func(text string, message map[string]any, index int) string) {
		if !engineEnabled(settings, name, lossyAllowed) {
			return
		}
		snapshot := make([]map[string]any, len(working))
		for index, message := range working {
			copied := cloneMap(message)
			if content, ok := message["content"].([]any); ok {
				copied["content"] = append([]any{}, content...)
			}
			snapshot[index] = copied
		}
		start := totalTokens(working)
		for index, message := range working {
			if !selectMessage(message, index) {
				continue
			}
			for _, part := range textParts(message, index) {
				current := partText(working, part)
				next := transform(current, message, index)
				if len([]rune(next)) < len([]rune(current)) {
					setPartText(working, part, next)
				}
			}
		}
		saved := start - totalTokens(working)
		if saved > 0 {
			stats.Engines[name] = saved
			return
		}
		copy(working, snapshot)
	}

	allMessages := func(map[string]any, int) bool { return true }
	unprotected := func(message map[string]any, index int) bool { return !protected(index) }
	run("whitespace", allMessages, func(text string, _ map[string]any, _ int) string { return whitespace(text) })
	run("jsonMinify", func(message map[string]any, index int) bool {
		return messageRole(message) == "tool" || !protected(index)
	}, func(text string, _ map[string]any, _ int) string { return jsonMinify(text) })
	if engineEnabled(settings, "dedupe", lossyAllowed) {
		lastSeen := map[string]int{}
		for index, message := range working {
			text := messageText(message, index)
			if len([]rune(text)) >= dedupeMinChars {
				lastSeen[text] = index
			}
		}
		run("dedupe", func(message map[string]any, index int) bool {
			return !protected(index) && lastSeen[messageText(message, index)] > index
		}, func(string, map[string]any, int) string { return texts().DuplicateBlock })
	}
	run("repeatedLines", func(message map[string]any, _ int) bool {
		role := messageRole(message)
		return role == "tool" || role == "user"
	}, func(text string, _ map[string]any, _ int) string { return RepeatedLines(text) })
	run("jsonTables", func(message map[string]any, _ int) bool {
		return messageRole(message) == "tool"
	}, func(text string, _ map[string]any, _ int) string { return jsonTables(text) })
	run("trimToolOutput", func(message map[string]any, _ int) bool {
		return messageRole(message) == "tool"
	}, func(text string, _ map[string]any, index int) string {
		limit := toolLimitOlder
		if len(toolIndexes) > 0 && index == toolIndexes[len(toolIndexes)-1] {
			limit = toolLimitLatest
		}
		return HeadTail(text, limit)
	})
	maskBefore := -1
	if len(toolIndexes) > maskKeepLast {
		maskBefore = toolIndexes[len(toolIndexes)-maskKeepLast]
	}
	run("maskOldToolOutput", func(message map[string]any, index int) bool {
		return messageRole(message) == "tool" && index < maskBefore
	}, func(text string, _ map[string]any, _ int) string {
		if len([]rune(text)) > 600 {
			return string([]rune(text)[:200]) + "\n" + texts().MaskedTool
		}
		return text
	})
	run("condenseHistory", func(message map[string]any, index int) bool {
		role := messageRole(message)
		return unprotected(message, index) && index < recentFrom && (role == "user" || role == "assistant")
	}, func(text string, _ map[string]any, _ int) string {
		if len([]rune(text)) > condenseMinChars && !strings.Contains(text, "```") {
			return HeadTail(text, 1000)
		}
		return text
	})
	if settings.TargetTokens != nil && totalTokens(working) > *settings.TargetTokens {
		run("tokenTarget", unprotected, func(text string, _ map[string]any, index int) string {
			if totalTokens(working) > *settings.TargetTokens && index < recentFrom {
				return texts().EarlierMessage
			}
			return text
		})
	}

	after := totalTokens(working)
	result.Canonical = canonical.WithMessages(working)
	stats.AfterTokens = after
	stats.SavedTokens = before - after
	if stats.SavedTokens < 0 {
		stats.SavedTokens = 0
	}
	result.Stats = stats
	return result
}
