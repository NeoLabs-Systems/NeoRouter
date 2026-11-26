package task

import (
	"regexp"
	"strings"
)

// What the task detectors look at, read once per request. Like the hosted
// routers, the latest user message carries most of the signal; the system
// prompt and earlier turns only add context (agents send long system prompts
// with every turn, which says nothing about this turn).
const (
	lastUserChars = 12000
	systemChars   = 4000
	previousChars = 2000
)

// A message that opens with a short ask and goes on with a longer text
// ("Summarise this: <article>") carries a passage: the ask says what to do, the
// passage only what to do it with.
const (
	askMaxChars     = 400
	passageMinChars = 300
)

// Numbers ("16", "2.5", "40%", "$5") and the words of quantity that word
// problems are told in.
var quantityWords = words("half$", "twice$", "double$", "triple$", "third$", "quarter$", "percent", "each$", "per$", "every", "total", "sum$", "remain",
	"left over", "hälfte", "doppelt", "drittel", "viertel", "prozent", "jede", "pro$", "insgesamt", "summe$", "übrig")

// Conditions an answer has to meet: the "constraints" dimension of NVIDIA's
// prompt complexity classifier.
var constraintWords = words("must$", "should$", "shouldn't", "do not", "don't", "never", "always", "only$", "at least", "at most", "no more than", "exactly",
	"without", "make sure", "ensure", "limit(?:ed)? to", `under \d+`, `within \d+`, "muss", "darf nicht", "nicht mehr als", "mindestens", "höchstens",
	"genau$", "ohne$", "nur$", "achte darauf", "stelle sicher")

var (
	listItem    = compile(`(?m)^\s*(?:[-*•]|\d+[.)])\s+\S`)
	exampleLine = compile(`(?im)^\s*(?:example|beispiel|input|output|eingabe|ausgabe|q|a)\s*\d*\s*:`)
	// Long words and acronyms: a rough measure of specialist vocabulary.
	longWord = compile(`\p{L}{13,}`)
	acronym  = compile(`\b[A-Z]{2,6}s?\b`)
)

func userMessages(messages []Message) []Message {
	users := make([]Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == "user" {
			users = append(users, message)
		}
	}
	return users
}

// splitPassage is signals.splitPassage: the ask and its passage, when the
// message reads "<short ask>\n\n<long text>".
func splitPassage(text string) (ask, passage string) {
	trimmed := strings.TrimSpace(text)
	breakAt := searchPassageBreak(trimmed)
	end := -1
	if breakAt > 0 && breakAt <= askMaxChars {
		end = breakAt
	}
	if end < 0 || utf16Len(trimmed)-end < passageMinChars {
		return trimmed, ""
	}
	return strings.TrimSpace(cutRunes(trimmed, end)), strings.TrimSpace(sliceFromRunes(trimmed, end))
}

// passageBreak is /\n\s*\n|(?<=[?:])\s*\n/ evaluated by hand: Go has no
// lookbehind, so the second alternative (a newline after ':' or '?' with
// optional blanks between) is found separately.
var passageBreakNewline = regexp.MustCompile(`\n\s*\n`)

func searchPassageBreak(text string) int {
	best := -1
	if loc := passageBreakNewline.FindStringIndex(text); loc != nil {
		best = loc[0]
	}
	for i := 1; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		j := i - 1
		for j >= 0 && isSpaceByte(text[j]) {
			j--
		}
		if j >= 0 && (text[j] == ':' || text[j] == '?') {
			if start := j + 1; best < 0 || start < best {
				best = start
			}
		}
	}
	return best
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\v' || b == '\f'
}

// cutRunes cuts a string at a UTF-16 code-unit offset.
func cutRunes(s string, at int) string {
	runes := []rune(s)
	if at >= len(runes) {
		return s
	}
	return string(runes[:at])
}

// sliceFromRunes is the text from offset n.
func sliceFromRunes(s string, at int) string {
	if at <= 0 {
		return s
	}
	runes := []rune(s)
	if at >= len(runes) {
		return ""
	}
	return string(runes[at:])
}

// Tool names as words ("run_terminal_cmd", "browserNavigate" → "run terminal
// cmd", "browser navigate"), so detectors can read them like text.
func toolWords(name string) string {
	var b strings.Builder
	prevLower := false
	for _, r := range name {
		if prevLower && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prevLower = r >= 'a' && r <= 'z'
	}
	out := strings.NewReplacer("_", " ", "-", " ", ".", " ", ":", " ", "/", " ").Replace(b.String())
	return strings.ToLower(out)
}

// The tools an agent is offered say what it may do; the ones it called in the
// last few messages say what it is doing.
const (
	recentMessages = 6
	toolTextChars  = 4000
)

func offeredTools(tools []Tool) string {
	var b strings.Builder
	for i, tool := range tools {
		if i > 0 {
			b.WriteByte('\n')
		}
		fn := tool.Function
		b.WriteString(toolWords(fn.Name))
		b.WriteByte(' ')
		b.WriteString(firstRunes(fn.Description, 200))
	}
	return firstRunes(b.String(), toolTextChars)
}

func calledTools(messages []Message) string {
	start := len(messages) - recentMessages
	if start < 0 {
		start = 0
	}
	var names []string
	for _, message := range messages[start:] {
		for _, call := range message.ToolCalls {
			name := call.Function.Name
			if name == "" {
				name = call.Name
			}
			if name != "" {
				names = append(names, toolWords(name))
			}
		}
		if message.Role == "tool" && message.Name != "" {
			names = append(names, toolWords(message.Name))
		}
	}
	return strings.Join(names, "\n")
}

// Signals is what the detectors read (signals.readSignals).
type Signals struct {
	LastUser string
	// Ask is what the user asks for, without a pasted passage: kind words are
	// read here.
	Ask     string
	Passage string
	// LastUserTokens is a rough token count of the latest user message.
	LastUserTokens int
	System         string
	// PreviousUser is the turn before, for short follow-ups ("now make it
	// faster").
	PreviousUser string
	PreviousCode bool
	Turns        int
	ToolCount    int
	Tools        string
	CalledTools  string
	// ToolResults in this turn: the request continues agent work.
	ToolResults   int
	WantsJSON     bool
	HasImages     bool
	InputTokens   int
	CodeBlocks    float64
	Numbers       int
	QuantityWords int
	Questions     int
	Constraints   int
	Examples      int
	Jargon        int
	AskWords      int
}

// ReadSignals reads what the task detectors look at.
func ReadSignals(request Request, inputTokens int) *Signals {
	messages := request.Messages
	users := userMessages(messages)
	var last *Message
	if len(users) > 0 {
		last = &users[len(users)-1]
	}
	lastUser := ""
	if last != nil {
		lastUser = lastRunes(textOf(last.Content), lastUserChars)
	}
	previousUser := ""
	if len(users) > 1 {
		previousUser = lastRunes(textOf(users[len(users)-2].Content), previousChars)
	}
	lastIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastIndex = i
			break
		}
	}
	lastAssistant := ""
	limit := len(messages)
	if lastIndex >= 0 {
		limit = lastIndex
	}
	for i := limit - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistant = textOf(messages[i].Content)
			break
		}
	}
	var system strings.Builder
	for _, message := range messages {
		if message.Role == "system" || message.Role == "developer" {
			if system.Len() > 0 {
				system.WriteByte('\n')
			}
			system.WriteString(textOf(message.Content))
		}
	}
	hasImages := false
	for _, message := range messages {
		for _, part := range ContentParts(message.Content) {
			if IsImagePart(part) {
				hasImages = true
			}
		}
	}
	wantsJSON := false
	if request.ResponseFormat != nil {
		wantsJSON = request.ResponseFormat.Type == "json_object" || request.ResponseFormat.Type == "json_schema"
	}
	ask, passage := splitPassage(lastUser)
	start := len(messages) - recentMessages
	if start < 0 {
		start = 0
	}
	toolResults := 0
	for _, message := range messages[start:] {
		if message.Role == "tool" {
			toolResults++
		}
	}
	return &Signals{
		LastUser:       lastUser,
		Ask:            ask,
		Passage:        passage,
		LastUserTokens: (utf16Len(lastUser) + 3) / 4, // ceil(len/4)
		System:         firstRunes(system.String(), systemChars),
		PreviousUser:   previousUser,
		PreviousCode:   strings.Contains(lastAssistant, "```"),
		Turns:          len(users),
		ToolCount:      len(request.Tools),
		Tools:          offeredTools(request.Tools),
		CalledTools:    calledTools(messages),
		ToolResults:    toolResults,
		WantsJSON:      wantsJSON,
		HasImages:      hasImages,
		InputTokens:    inputTokens,
		CodeBlocks:     float64(strings.Count(lastUser, "```")) / 2,
		Numbers:        countNumbers(ask),
		QuantityWords:  quantityWords.hits(ask),
		Questions:      strings.Count(ask, "?"),
		Constraints:    constraintWords.hits(ask) + listItem.count(ask),
		Examples:       exampleLine.count(lastUser),
		Jargon:         longWord.count(ask) + acronym.count(ask),
		AskWords:       len(splitWords(ask)),
	}
}

func splitWords(text string) []string { return strings.Fields(text) }
