package task

// The kinds of task NeoRouter recognises in a request. Each kind is data: how
// to spot it (a detector over the request signals, mostly word patterns in
// English and German), which skills it calls on (skills.go) and how hard it
// usually is. Adding a kind, or a language, means adding an entry here; the
// router itself knows no kind by name.
//
// detect returns 0..1: how strongly the request looks like this kind. The task
// types follow those of public prompt classifiers (NVIDIA's prompt task and
// complexity classifier, the Dolly-15k categories): open and closed questions,
// brainstorming and creative writing, classification and extraction,
// summarisation, code.

func saturate(count, full float64) float64 { return minf(1, count/full) }

var codeWords = words("function", "class(?:es)?$", "def ", "import ", "const ", "return ", "async ", "await ", "compile", "refactor", "debug", "bug", "stack ?trace",
	"exception", "regex", "sql", "json", "api", "endpoint", "typescript", "javascript", "python", "rust$", "golang", "java", "kotlin", "swift$", "dart$", "flutter",
	"react$", "node", "docker", "kubernetes", "bash", "shell", "git$", "unit ?test", "code", "programm", "skript", "fehlermeldung", "kompilier",
	"backend", "frontend", "database", "datenbank", "postgres", "mysql", "redis", "kafka", "microservice", "server$", "algorithm", "algorithmus", "array",
	"integer", "variable", "linked list", "binary tree", "recursion", "recursive", "rekursi", "html", "css", `c\+\+`, "c#", "php", "ruby", "scala", "haskell",
	"implement", "implementier", "snippet", "npm$", "pip$", "cli$", "linux", "query", "schema", "unittest", "pytest",
	"lambda", "method", "methode", "parser", "state machine", "interface", "pull request", "commits?$", "repos?$", "repository", "deploy", "runtime", "syntax", "leetcode", "big-?o", "time complexity")

var codeSyntax = compile("(?m)```|=>|\\)\\s*\\{|;\\s*$|^\\s*(?:def|class|import|from|const|let|var|function|public|private|#include)\\b|\\b\\w+\\.(?:js|ts|py|go|rs|java|kt|swift|dart|cpp|rb|php|sql|ya?ml|json)\\b|Traceback \\(most recent call last\\)|at \\w+ \\(.+:\\d+:\\d+\\)|\\b\\w+_\\w+\\(|\\b[a-z]+[A-Z]\\w*\\(")

// Asking for code to be written, whatever the language is called.
var codeAsk = compile(`(?i)\b(?:write|create|implement|build|make|develop|generate|code)\s+(?:a|an|the)?\s*(?:\w+\s+){0,3}(?:function|program|script|class|method|query|regex|pattern|api|app|bot|cli|module|component|form|page|endpoint|parser|test)s?\b|\b(?:schreib|erstell)\w*\s+(?:ein(?:e|en)?)?\s*(?:\w+\s+){0,2}(?:funktion|programm|skript|klasse|methode|abfrage)`)

// Identifiers in prose: snake_case, camelCase, dotted.paths.
var identifier = compile(`\b[a-z]+_[a-z_]+\b|\b[a-z]+[A-Z][a-z]+[A-Za-z]*\b|\b[a-z]+\.[a-z]+\.[a-z]+\(`)

var mathWords = words("prove", "proof", "theorem", "lemma", "integral", "derivative", "differentiate", "equation", "solve for", "probability", "matrix",
	"eigen", "limit of", "calculate", "compute", "beweise", "beweis", "gleichung", "ableitung", "wahrscheinlichkeit", "berechne", "löse", "polynomial", "polynom",
	"prime", "primzahl", "factorial", "fakultät", "divisible", "teilbar", "remainder when", "modulo", "logarithm", "logarithmus", "geometry", "geometrie",
	"triangle", "dreieck", "circle", "kreis", "radius", "area of", "fläche", "perimeter", "umfang", "volume of", "volumen", "angle", "winkel", "vector", "vektor",
	"combinatori", "kombinatorik", "permutation", "how many ways", "expected value", "erwartungswert", "variance", "varianz", "mean of", "median", "simplify",
	"vereinfache", "evaluate the", "real numbers", "integers", "ganze zahlen", "sequence", "folge", "series", "reihe", "inequality", "ungleichung", "quadratic",
	"quadratisch", "algebra", "calculus", "arithmetic", "fraction", "bruch$", "brüche", "ratio", "verhältnis")

var mathSyntax = compile(`\$[^$]+\$|\\(?:frac|sum|int|sqrt|lim|cdot|binom|pi|theta|alpha)|\b\d+\s*[\^*/+-]\s*\d+\s*=|∫|∑|√|≤|≥|π|\b[a-z]\s*\^\s*\d|\b\d*[a-z]\s*[+-]\s*\d+\s*=|\bf\([a-z]\)`)

// Word problems: a question of quantity over numbers ("How many eggs does she
// sell?").
var quantityQuestion = words("how many", "how much", "how long", "how far", "how old", "how fast", "what is the total", "what percentage", "what fraction",
	"find the (?:number|value|total|sum|cost|price|area|probability)", "wie viele", "wie viel", "wie lange", "wie weit", "wie alt", "wie schnell", "wie hoch ist der")

var reasoningWords = words("step by step", "why", "explain", "compare", "pros and cons", "trade-?offs?", "analy[sz]e", "evaluate", "plan$", "strategy",
	"decide", "should i", "reason", "puzzle", "riddle", "logic", "what would happen", "what if", "implications", "argue", "justify", "critique", "assess",
	"which is better", "difference between", "cause", "consequence", "warum", "erkläre", "erklär", "vergleiche", "analysiere", "bewerte", "vor- und nachteile",
	"soll ich", "schritt für schritt", "was wäre wenn", "unterschied zwischen", "begründe", "rätsel", "logik")

// Creative writing: texts made up for the asker.
var writingWords = words("poem", "story", "essay", "blog", "email$", "e-mail$", "letter$", "tweet", "post$", "rewrite", "rephrase", "paraphrase",
	"proofread", "tone", "slogan", "lyrics", "song$", "haiku", "limerick", "joke", "scenes?$", "screenplay", "movie script", "dialogue", "speech", "novel$", "plot$", "characters?$",
	"(?:a|one|short) paragraph", "imagine", "caption", "headline", "tagline", "cover letter", "bio$", "product description", "toast$", "eulogy", "fairy tale", "fiction",
	"make it (?:sound|more)", "improve (?:the|my) (?:writing|text|wording)", "grammar", "pretend", "roleplay", "role-play",
	"schreib", "gedicht", "geschichte", "aufsatz", "brief", "formulier", "umformulier", "korrigier", "text für", "rede$", "witz", "absatz", "stell dir vor",
	"märchen", "liedtext", "überschrift", "werbetext")

// Brainstorming: ideas, suggestions and lists of options.
var brainstormWords = words("ideas?$", "brainstorm", "suggest", "come up with", "think of", "ways to", "ways (?:in which|that|of)", `give me (?:a list|some|\d+|two|three|four|five|six|seven|eight|nine|ten)`,
	`list (?:of|some|\d+|two|three|four|five|ten)`, `top \d+`, "recommend", "tips (?:for|on|to)",
	"options for", "alternatives", "ideen", "vorschläge", "schlag .* vor", "nenne", "liste (?:mit|von|der)", "tipps", "empfiehl", "empfehlung", "was sind gute")

var translationWords = words("translate", "translation", "in english", "into english", "in german", "into german", "auf deutsch", "auf englisch",
	"übersetz", "ins deutsche", "ins englische", "in french", "into french", "auf französisch", "in spanish", "into spanish", "auf spanisch", "in italian",
	"auf italienisch", "in japanese", "in chinese", "how do you say", "what does .* mean in", "wie sagt man")

var summaryWords = words("summari[sz]", "summary", "tl;?dr", "key points", "main points", "in short", "condense", "recap", "gist", "in a nutshell",
	`in (?:one|two|three|a few|\d+) sentences`, "briefly describe", "outline the", "zusammenfass", "kernpunkte", "kurzfassung", "fasse", "in kürze", "das wichtigste")

var extractionWords = words("extract", "as json", "in json", "json format", "classify", "categori[sz]e", "label", "fill in", "table of", "parse$",
	"pull out", "list (?:all|out|down|the)", "identify (?:which|the|all|whether)", "which of (?:these|the following|those)", "sort (?:these|the following|them)",
	"group (?:these|the following|them)", "tag (?:these|each)", "sentiment", "comma separated", "bullet(?:ed)? list of (?:the|all)",
	"extrahier", "als json", "klassifizier", "kategorisier", "tabelle", "ordne .* zu", "welche (?:davon|dieser|der folgenden)", "stimmung")

// A choice among listed options: "Which is a fish? Tope or Rope", "string or
// percussion: Cantaro, Gudok".
var optionList = compile(`(?i)\b(\w+ or \w+)\b[^\n]{0,40}[:?]\s*\S[^\n]{0,120}(?:,| or )`)

var actionWords = words("search", "look up", "open", "send", "create", "schedule", "book", "remind", "run", "execute", "download", "browse",
	"such", "öffne", "sende", "schick", "erstell", "erinnere", "führe", "buche")

// Agents that work a terminal or a browser. Tool names say what an agent can
// do; the ask and the tools it just called say what this turn is about.
var terminalTools = words("bash", "shell", "terminal", "exec", "run command", "run cmd", "run terminal", "command", "cmd$", "powershell", "zsh", "ssh",
	"subprocess", "process")

var terminalWords = words("terminal", "command line", "kommandozeile", "shell", "bash", "run (?:the )?(?:command|script|tests?|build|migration)", "npm (?:install|run|test)",
	"pip install", "yarn", "git (?:clone|push|pull|commit|status|log|checkout|rebase|merge)", "ssh", "chmod", "sudo", "docker (?:run|build|compose|ps)", "kubectl",
	"make (?:build|test)", "install (?:the )?(?:package|dependenc)", "befehl", "führe .* aus", "installier")

var browserTools = words("browser", "navigate", "click", "goto", "go to", "open url", "open page", "page", "screenshot", "scroll", "type text", "fill",
	"hover", "playwright", "puppeteer", "selenium", "dom$", "tab$", "tabs$", "computer", "mouse", "keyboard", "web page", "webpage")

var browserWords = words("browser", "website", "web ?site", "web ?page", "click", "log ?in (?:to|on)", "sign in", "fill (?:out|in) (?:the|this|a) form", "go to", "navigate",
	"open (?:the |this )?(?:site|page|url|link|tab)", "scroll", "screenshot", "add (?:it )?to (?:the )?cart", "checkout", "webseite", "internetseite", "klick", "formular",
	"öffne die seite", "melde dich an", "geh auf", "in den warenkorb")

var urlPattern = compile(`(?i)https?://\S+|\bwww\.\S+`)

var questionWords = words("who", "what", "when", "where", "which", "how many", "how much", "how does", "how do", "how is", "is there", "are there", "does", "did",
	"tell me about", "what is", "define", "wer", "was", "wann", "wo$", "woher", "welche", "wie viele", "wie viel", "wie funktioniert", "gibt es", "erzähl mir")

// Asking about a passage: "Given this paragraph, …", "From the text, …".
var passageWords = words("given (?:this|the|a) (?:text|paragraph|passage|reference|article)", "from the (?:text|passage|paragraph|article)", "based on (?:this|the) (?:text|passage|paragraph|article)",
	"according to the", "in this (?:text|passage|paragraph|article)", "reference text", "laut (?:dem|diesem) text", "aus dem text", "im text")

var smallTalk = compile(`(?i)^\s*(?:hi|hey|hello|hallo|servus|moin|yo|thanks?( you)?|thx|danke( dir| schön)?|ok(?:ay)?|cool|nice|super|good (?:morning|night)|guten (?:morgen|abend|tag)|wie geht'?s|how are you)\b[\s!?.,:)]*$`)

// A short message that builds on code from the last answer ("now add tests").
const followUpTokens = 40

// How much a turn looks like work with one kind of tool: calling it now says
// most, being asked to it next. Being offered the tool only adds to those:
// agents are offered every tool on every turn, "hi" included. Without tools on
// offer it is a question about the tool ("how do I rebase?"), not work with it.
func toolWork(s *Signals, tools, askWords *wordPattern, extra float64) float64 {
	if s.ToolCount == 0 {
		return 0
	}
	signal := 0.0
	if tools.test(s.CalledTools) {
		signal += 0.6
	}
	if askWords.test(s.Ask) {
		signal += 0.35
	}
	if signal == 0 {
		return 0
	}
	if tools.test(s.Tools) {
		signal += 0.2
	}
	return minf(1, signal+extra)
}

func opensWithQuestion(s *Signals) bool {
	if questionWords.findIndex(s.Ask) == 0 {
		return true
	}
	firstLine := s.Ask
	if i := indexByte(s.Ask, '\n'); i >= 0 {
		firstLine = s.Ask[:i]
	}
	return endsWithQuestionMark(firstLine)
}

func endsWithQuestionMark(text string) bool {
	i := len(text) - 1
	for i >= 0 && isSpaceByte(text[i]) {
		i--
	}
	if i < 0 {
		return false
	}
	// \s*$ includes other whitespace; a '\n' was already cut off, so only
	// trailing blanks matter.
	return text[i] == '?'
}

func followsCode(s *Signals) bool {
	return s.PreviousCode && s.LastUserTokens < followUpTokens && s.Passage == ""
}

// SkillWeight is one skill a kind calls on and how strongly.
type SkillWeight struct {
	Name   string
	Weight float64
}

// Kind is one kind of task.
type Kind struct {
	ID         string
	Skills     []SkillWeight
	Difficulty float64
	Detect     func(*Signals) float64
}

// Kinds are the recognised task kinds, in classification order.
var Kinds = []Kind{
	{
		ID:         "chat",
		Skills:     nil,
		Difficulty: 0,
		// Short messages that match nothing else count as chat too
		// (classifier.go).
		Detect: func(s *Signals) float64 {
			if smallTalk.test(s.LastUser) {
				return 1
			}
			return 0
		},
	},
	{
		ID:         "coding",
		Skills:     []SkillWeight{{"coding", 1}, {"agentic", 0.2}},
		Difficulty: 0.45,
		Detect: func(s *Signals) float64 {
			value := 0.0
			if codeSyntax.test(s.LastUser) {
				value += 0.5
			}
			value += 0.25 * mathMin(3, float64(codeWords.hits(s.Ask)))
			value += minf(0.3, s.CodeBlocks*0.15)
			if codeAsk.test(s.Ask) {
				value += 0.35
			}
			if identifier.count(s.Ask) >= 2 {
				value += 0.25
			}
			if followsCode(s) {
				value += 0.4
			}
			return minf(1, value)
		},
	},
	{
		ID:         "math",
		Skills:     []SkillWeight{{"math", 1}, {"reasoning", 0.4}},
		Difficulty: 0.55,
		Detect: func(s *Signals) float64 {
			wordProblem := 0.0
			if quantityQuestion.test(s.Ask) && s.Numbers+s.QuantityWords >= 2 {
				wordProblem = 0.45 + 0.1*mathMin(3, float64(s.Numbers-1))
			}
			value := 0.0
			if mathSyntax.test(s.Ask) {
				value += 0.5
			}
			value += 0.35 * mathMin(2, float64(mathWords.hits(s.Ask)))
			return minf(1, value+wordProblem)
		},
	},
	{
		ID:         "reasoning",
		Skills:     []SkillWeight{{"reasoning", 1}, {"knowledge", 0.3}},
		Difficulty: 0.5,
		Detect: func(s *Signals) float64 {
			return 0.3 * mathMin(2, float64(reasoningWords.hits(s.Ask)))
		},
	},
	{
		ID:         "writing",
		Skills:     []SkillWeight{{"instruction", 1}, {"knowledge", 0.3}},
		Difficulty: 0.3,
		Detect: func(s *Signals) float64 {
			value := 0.4 * mathMin(2, float64(writingWords.hits(s.Ask)))
			if brainstormWords.test(s.Ask) {
				value += 0.5
			}
			return minf(1, value)
		},
	},
	{
		ID:         "translation",
		Skills:     []SkillWeight{{"knowledge", 0.6}, {"instruction", 0.6}},
		Difficulty: 0.25,
		Detect: func(s *Signals) float64 {
			if translationWords.test(s.Ask) {
				return 0.85
			}
			return 0
		},
	},
	{
		ID:         "summarization",
		Skills:     []SkillWeight{{"instruction", 0.8}, {"long_context", 0.6}},
		Difficulty: 0.3,
		Detect: func(s *Signals) float64 {
			value := 0.0
			if summaryWords.test(s.Ask) {
				value += 0.8
			}
			if s.Passage != "" && summaryWords.test(s.Ask) {
				value += 0.1
			}
			if s.LastUserTokens > 1500 && !codeSyntax.test(s.LastUser) {
				value += 0.2
			}
			return minf(1, value)
		},
	},
	{
		ID:         "extraction",
		Skills:     []SkillWeight{{"instruction", 1}},
		Difficulty: 0.3,
		Detect: func(s *Signals) float64 {
			value := 0.0
			if s.WantsJSON {
				value += 0.6
			}
			value += 0.5 * mathMin(2, float64(extractionWords.hits(s.Ask)))
			if optionList.test(s.Ask) {
				value += 0.4
			}
			return minf(1, value)
		},
	},
	{
		ID:         "terminal",
		Skills:     []SkillWeight{{"terminal", 1}, {"agentic", 0.5}, {"coding", 0.4}},
		Difficulty: 0.45,
		Detect: func(s *Signals) float64 {
			return toolWork(s, terminalTools, terminalWords, 0)
		},
	},
	{
		ID:         "browser",
		Skills:     []SkillWeight{{"agentic", 1}, {"instruction", 0.5}},
		Difficulty: 0.4,
		// Pages to visit and screenshots coming back are browser work too.
		Detect: func(s *Signals) float64 {
			extra := 0.0
			if urlPattern.test(s.Ask) {
				extra += 0.1
			}
			if s.HasImages && browserTools.test(s.CalledTools) {
				extra += 0.1
			}
			return toolWork(s, browserTools, browserWords, extra)
		},
	},
	{
		ID:         "agent",
		Skills:     []SkillWeight{{"agentic", 1}, {"instruction", 0.5}},
		Difficulty: 0.35,
		// Tools on offer are only a hint (agents send them every turn); asking
		// for an action, or tool results coming back, make it agent work. Work
		// in a terminal or a browser is that kind first, and agent work only
		// besides.
		Detect: func(s *Signals) float64 {
			if s.ToolCount == 0 {
				return 0
			}
			general := agentBaseline
			if actionWords.test(s.Ask) {
				general += 0.4
			}
			general += minf(0.5, float64(s.ToolResults)*0.25)
			general = minf(1, general)
			specific := mathMax(toolWork(s, terminalTools, terminalWords, 0), toolWork(s, browserTools, browserWords, 0))
			if specific != 0 {
				return mathMin(general, specific/2)
			}
			return general
		},
	},
	{
		ID:         "vision",
		Skills:     nil,
		Difficulty: 0.35,
		Detect: func(s *Signals) float64 {
			if s.HasImages {
				return 0.9
			}
			return 0
		},
	},
	{
		ID:         "long_context",
		Skills:     []SkillWeight{{"long_context", 1}},
		Difficulty: 0.45,
		Detect: func(s *Signals) float64 {
			return mathMax(0, mathMin(1, float64(s.InputTokens-30000)/70000))
		},
	},
	{
		ID:         "knowledge",
		Skills:     []SkillWeight{{"knowledge", 1}, {"reasoning", 0.3}},
		Difficulty: 0.3,
		// Open questions, and closed ones about a passage that comes with them.
		Detect: func(s *Signals) float64 {
			value := 0.0
			if opensWithQuestion(s) {
				value += 0.45
			}
			if s.Passage != "" && (opensWithQuestion(s) || passageWords.test(s.Ask)) {
				value += 0.15
			}
			return value
		},
	},
}

// Words that make any request harder, whatever its kind.
var hardWords = words("prove", "rigorous", "optimi[sz]e", "architecture", "design a", "edge cases", "complex", "from scratch", "production", "scal(?:e|able)",
	"multi-?step", "thorough", "in depth", "trade-?offs?", "step[- ]by[- ]step", "think (?:carefully|hard|deeply)", "failure modes?", "distributed",
	"concurren(?:t|cy)", "race conditions?", "efficient", "performance", "secure", "security", "thread-?safe", "asymptotic", "formal", "novel approach",
	"research", "derive", "generali[sz]e", "all possible", "beweise", "optimier", "architektur", "komplex", "gründlich", "ausführlich", "von grund auf",
	"abwägung", "schritt für schritt", "nebenläufig", "effizient", "sicherheit", "herleit")

// Below this many tokens, a message that matches no kind is small talk.
const shortMessageTokens = 12

// What the agent kind scores for tools on offer and nothing else.
const agentBaseline = 0.25
