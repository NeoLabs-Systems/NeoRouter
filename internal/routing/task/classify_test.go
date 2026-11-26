package task

import "testing"

func ask(content string, extra ...Message) *Result {
	messages := append([]Message{{Role: "user", Content: content}}, extra...)
	return Classify(Request{Messages: messages}, 0, "")
}

func TestPromptsAreRecognisedByKind(t *testing.T) {
	cases := []struct{ prompt, kind string }{
		{"hi", "chat"},
		{"Danke!", "chat"},
		{"Write a python function that parses ISO dates", "coding"},
		{"Fix this: TypeError: x is undefined at foo (app.js:12:5)", "coding"},
		{"Prove that the square root of 2 is irrational", "math"},
		{"Berechne das Integral von x^2", "math"},
		{"Why is the sky blue? Explain step by step", "reasoning"},
		{"Schreib mir ein Gedicht über den Herbst", "writing"},
		{"Übersetze das ins Englische: Guten Morgen", "translation"},
		{"Fasse diesen Artikel zusammen", "summarization"},
		{"Extract the names and dates from this text as JSON", "extraction"},
		{"What is the capital of France?", "knowledge"},
	}
	for _, test := range cases {
		if got := ask(test.prompt).Primary; got != test.kind {
			t.Errorf("classify(%q).primary = %q, want %q", test.prompt, got, test.kind)
		}
	}
}

func TestWordProblemsPassagesOptionsIdeasFollowUps(t *testing.T) {
	article := "Kubernetes is an open-source system for automating deployment of containerised applications on a server cluster. "
	for i := 0; i < 6; i++ {
		article += "Kubernetes is an open-source system for automating deployment of containerised applications on a server cluster. "
	}
	cases := []struct{ prompt, kind string }{
		{"Janet has 16 eggs per day. She eats three and bakes with four. How many eggs does she sell each week?", "math"},
		{"Lisa kauft 3 Äpfel zu je 2 Euro. Wie viel bezahlt sie insgesamt?", "math"},
		{"Given this paragraph, when was the system first released?\n\n" + article, "knowledge"},
		{"Summarise this article in three sentences.\n\n" + article, "summarization"},
		{"Identify which instrument is string or percussion: Cantaro, Gudok", "extraction"},
		{"Give me five ideas for a rainy Sunday with kids", "writing"},
		{"Write in Bash a script that deletes empty subdirectories", "coding"},
	}
	for _, test := range cases {
		if got := ask(test.prompt).Primary; got != test.kind {
			t.Errorf("classify(%q).primary = %q, want %q", test.prompt, got, test.kind)
		}
	}
	// A short follow-up to an answer with code stays coding.
	followUp := Classify(Request{Messages: []Message{
		{Role: "user", Content: "Write a debounce helper"},
		{Role: "assistant", Content: "```js\nfunction debounce() {}\n```"},
		{Role: "user", Content: "now make it faster"},
	}}, 0, "")
	if followUp.Primary != "coding" {
		t.Errorf("follow-up primary = %q, want coding", followUp.Primary)
	}
}

func TestAgentWorkIsToldApartByItsTools(t *testing.T) {
	tool := func(name, description string) Tool {
		return Tool{Type: "function", Function: ToolFunction{Name: name, Description: description}}
	}
	tools := []Tool{
		tool("run_terminal_cmd", "Run a shell command"),
		tool("browserNavigate", "Open a URL"),
		tool("browser_click", ""),
		tool("read_file", ""),
	}
	turn := func(content, called string) *Result {
		messages := []Message{{Role: "user", Content: "start"}}
		if called != "" {
			messages = append(messages,
				Message{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "1", Type: "function", Function: Function{Name: called, Arguments: "{}"}}}},
				Message{Role: "tool", ToolCallID: "1", Content: "done"},
			)
		}
		messages = append(messages, Message{Role: "user", Content: content})
		return Classify(Request{Messages: messages, Tools: tools}, 0, "")
	}
	if got := turn("Run the tests and fix whatever fails", "").Primary; got != "terminal" {
		t.Errorf("terminal turn = %q", got)
	}
	if got := turn("Go to amazon.com and add the cheapest USB-C cable to the cart", "").Primary; got != "browser" {
		t.Errorf("browser turn = %q", got)
	}
	if got := turn("Öffne die Seite und klick auf Anmelden", "").Primary; got != "browser" {
		t.Errorf("german browser turn = %q", got)
	}
	// The tool the agent just called says what "go on" means.
	if got := turn("ok continue", "run_terminal_cmd").Primary; got != "terminal" {
		t.Errorf("continue terminal = %q", got)
	}
	if got := turn("ok continue", "browser_click").Primary; got != "browser" {
		t.Errorf("continue browser = %q", got)
	}
	if got, _ := turn("Run the tests", "").Skills.get("terminal"); got <= 0 {
		t.Errorf("terminal skill = %v", got)
	}
	// Tools on offer alone are no work: "hi" stays small talk, a request stays
	// agent work.
	if got := turn("hi", "").Primary; got != "chat" {
		t.Errorf("hi with tools = %q", got)
	}
	if got := turn("Book me a table for two tomorrow at 7", "").Primary; got != "agent" {
		t.Errorf("booking turn = %q", got)
	}
	// Without tools it is a question about the terminal, not work in it.
	if got := ask("How do I rebase a branch in git?").Primary; got == "terminal" {
		t.Errorf("rebase question classified as terminal work")
	}
}

func TestConditionsAndSpecialistWordsMakeARequestHarder(t *testing.T) {
	plain := ask("Write a product description for a water bottle")
	strict := ask("Write a product description for a water bottle. It must mention the warranty, must not use the word cheap, should stay under 80 words and only use British spelling.")
	if !(strict.Difficulty > plain.Difficulty) {
		t.Errorf("%v > %v", strict.Difficulty, plain.Difficulty)
	}
	// A long pasted passage does not make a simple ask hard.
	passageText := "Summarise this.\n\n"
	for i := 0; i < 60; i++ {
		passageText += "The meeting went well and everyone agreed on the plan. "
	}
	if passage := ask(passageText); passage.Difficulty >= 0.4 {
		t.Errorf("passage difficulty = %v", passage.Difficulty)
	}
}

func TestDifficultySeparatesSmallTalkFromHardWork(t *testing.T) {
	if hi := ask("hi"); hi.Difficulty > 0.1 {
		t.Errorf("hi difficulty = %v", hi.Difficulty)
	}
	hard := ask("Design a scalable architecture for a chat backend with Kafka and Postgres, consider edge cases and failure modes")
	if hard.Difficulty < 0.45 {
		t.Errorf("hard difficulty = %v", hard.Difficulty)
	}
	// An agent's long system prompt and tools do not make "hi" hard.
	system := ""
	for i := 0; i < 800; i++ {
		system += "You are an agent. "
	}
	agent := Classify(Request{
		Messages: []Message{{Role: "system", Content: system}, {Role: "user", Content: "hi"}},
		Tools:    []Tool{{}, {}},
	}, 9000, "")
	if agent.Primary != "chat" {
		t.Errorf("agent hi primary = %q", agent.Primary)
	}
	if agent.Difficulty > 0.1 {
		t.Errorf("agent hi difficulty = %v", agent.Difficulty)
	}
}

func TestClientHintNamesTheTask(t *testing.T) {
	hinted := Classify(Request{Messages: []Message{{Role: "user", Content: "Schreib mir ein Gedicht"}}}, 0, "coding")
	if hinted.Primary != "coding" || !hinted.Hinted {
		t.Errorf("hinted = %+v", hinted)
	}
	if got := Classify(Request{Messages: []Message{{Role: "user", Content: "hi"}}}, 0, "nonsense"); got.Hinted {
		t.Errorf("nonsense hint was accepted")
	}
}

func TestTaskIntelligenceMovesTowardsTaskSkills(t *testing.T) {
	coder := map[string]float64{"coding": 0.9, "math": 0.3}
	if TaskIntelligence(0.5, coder, map[string]float64{"coding": 1}) <= 0.5 {
		t.Errorf("coding task did not raise intelligence")
	}
	if TaskIntelligence(0.5, coder, map[string]float64{"math": 1}) >= 0.5 {
		t.Errorf("math task did not lower intelligence")
	}
	if got := TaskIntelligence(0.5, nil, map[string]float64{"coding": 1}); got != 0.5 {
		t.Errorf("no benchmarks: %v", got)
	}
	if got := TaskIntelligence(0.5, coder, map[string]float64{}); got != 0.5 {
		t.Errorf("no skills: %v", got)
	}
	got := SkillsFromBenchmarks(map[string]float64{"livecodebench": 0.8, "scicode": 0.6, "tau2": 0.5})
	if got["coding"] != 0.7 || got["agentic"] != 0.5 {
		t.Errorf("skillsFromBenchmarks = %v", got)
	}
	if needed := NeededIntelligence(nil); needed != nil {
		t.Errorf("neededIntelligence(null) = %v", needed)
	}
	need := 0.5
	if EnoughIntelligence(0.7, &need, 1) != 0.7 {
		t.Errorf("enoughIntelligence full headroom")
	}
}
