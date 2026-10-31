'use strict';

const { words, hits } = require('./signals');

// The kinds of task NeoRouter recognises in a request. Each kind is data: how
// to spot it (a detector over the request signals, mostly word patterns in
// English and German), which skills it calls on (skills.js) and how hard it
// usually is. Adding a kind, or a language, means adding an entry here; the
// router itself knows no kind by name.
//
// detect(signals) returns 0..1: how strongly the request looks like this kind.
// difficulty is the kind's typical difficulty (0..1), mixed into the
// request's own difficulty (classifier.js).
//
// The task types follow those of public prompt classifiers (NVIDIA's prompt
// task and complexity classifier, the Dolly-15k categories): open and closed
// questions, brainstorming and creative writing, classification and
// extraction, summarisation, code. Kind words are read in the ask, not in a
// passage pasted after it (signals.js), so an article about servers does not
// make its summary a coding task.

const saturate = (count, full = 3) => Math.min(1, count / full);

const CODE_WORDS = words('function', 'class(es)?$', 'def ', 'import ', 'const ', 'return ', 'async ', 'await ', 'compile', 'refactor', 'debug', 'bug', 'stack ?trace',
  'exception', 'regex', 'sql', 'json', 'api', 'endpoint', 'typescript', 'javascript', 'python', 'rust$', 'golang', 'java', 'kotlin', 'swift$', 'dart$', 'flutter',
  'react$', 'node', 'docker', 'kubernetes', 'bash', 'shell', 'git$', 'unit ?test', 'code', 'programm', 'skript', 'fehlermeldung', 'kompilier',
  'backend', 'frontend', 'database', 'datenbank', 'postgres', 'mysql', 'redis', 'kafka', 'microservice', 'server$', 'algorithm', 'algorithmus', 'array',
  'integer', 'variable', 'linked list', 'binary tree', 'recursion', 'recursive', 'rekursi', 'html', 'css', 'c\\+\\+', 'c#', 'php', 'ruby', 'scala', 'haskell',
  'implement', 'implementier', 'snippet', 'npm$', 'pip$', 'cli$', 'linux', 'query', 'schema', 'unittest', 'pytest',
  'lambda', 'method', 'methode', 'parser', 'state machine', 'interface', 'pull request', 'commits?$', 'repos?$', 'repository', 'deploy', 'runtime', 'syntax', 'leetcode', 'big-?o', 'time complexity');
const CODE_SYNTAX = /```|=>|\)\s*\{|;\s*$|^\s*(def|class|import|from|const|let|var|function|public|private|#include)\b|\b\w+\.(js|ts|py|go|rs|java|kt|swift|dart|cpp|rb|php|sql|ya?ml|json)\b|Traceback \(most recent call last\)|at \w+ \(.+:\d+:\d+\)|\b\w+_\w+\(|\b[a-z]+[A-Z]\w*\(/m;
// Asking for code to be written, whatever the language is called.
const CODE_ASK = /\b(write|create|implement|build|make|develop|generate|code)\s+(a|an|the)?\s*(\w+\s+){0,3}(function|program|script|class|method|query|regex|pattern|api|app|bot|cli|module|component|form|page|endpoint|parser|test)s?\b|\b(schreib|erstell)\w*\s+(ein(e|en)?)?\s*(\w+\s+){0,2}(funktion|programm|skript|klasse|methode|abfrage)/i;
// Identifiers in prose: snake_case, camelCase, dotted.paths.
const IDENTIFIER = /\b[a-z]+_[a-z_]+\b|\b[a-z]+[A-Z][a-z]+[A-Za-z]*\b|\b[a-z]+\.[a-z]+\.[a-z]+\(/g;
const MATH_WORDS = words('prove', 'proof', 'theorem', 'lemma', 'integral', 'derivative', 'differentiate', 'equation', 'solve for', 'probability', 'matrix',
  'eigen', 'limit of', 'calculate', 'compute', 'beweise', 'beweis', 'gleichung', 'ableitung', 'wahrscheinlichkeit', 'berechne', 'löse', 'polynomial', 'polynom',
  'prime', 'primzahl', 'factorial', 'fakultät', 'divisible', 'teilbar', 'remainder when', 'modulo', 'logarithm', 'logarithmus', 'geometry', 'geometrie',
  'triangle', 'dreieck', 'circle', 'kreis', 'radius', 'area of', 'fläche', 'perimeter', 'umfang', 'volume of', 'volumen', 'angle', 'winkel', 'vector', 'vektor',
  'combinatori', 'kombinatorik', 'permutation', 'how many ways', 'expected value', 'erwartungswert', 'variance', 'varianz', 'mean of', 'median', 'simplify',
  'vereinfache', 'evaluate the', 'real numbers', 'integers', 'ganze zahlen', 'sequence', 'folge', 'series', 'reihe', 'inequality', 'ungleichung', 'quadratic',
  'quadratisch', 'algebra', 'calculus', 'arithmetic', 'fraction', 'bruch$', 'brüche', 'ratio', 'verhältnis');
const MATH_SYNTAX = /\$[^$]+\$|\\(frac|sum|int|sqrt|lim|cdot|binom|pi|theta|alpha)|\b\d+\s*[\^*/+-]\s*\d+\s*=|∫|∑|√|≤|≥|π|\b[a-z]\s*\^\s*\d|\b\d*[a-z]\s*[+-]\s*\d+\s*=|\bf\([a-z]\)/;
// Word problems: a question of quantity over numbers ("How many eggs does she sell?").
const QUANTITY_QUESTION = words('how many', 'how much', 'how long', 'how far', 'how old', 'how fast', 'what is the total', 'what percentage', 'what fraction',
  'find the (number|value|total|sum|cost|price|area|probability)', 'wie viele', 'wie viel', 'wie lange', 'wie weit', 'wie alt', 'wie schnell', 'wie hoch ist der');
const REASONING_WORDS = words('step by step', 'why', 'explain', 'compare', 'pros and cons', 'trade-?offs?', 'analy[sz]e', 'evaluate', 'plan$', 'strategy',
  'decide', 'should i', 'reason', 'puzzle', 'riddle', 'logic', 'what would happen', 'what if', 'implications', 'argue', 'justify', 'critique', 'assess',
  'which is better', 'difference between', 'cause', 'consequence', 'warum', 'erkläre', 'erklär', 'vergleiche', 'analysiere', 'bewerte', 'vor- und nachteile',
  'soll ich', 'schritt für schritt', 'was wäre wenn', 'unterschied zwischen', 'begründe', 'rätsel', 'logik');
// Creative writing: texts made up for the asker.
const WRITING_WORDS = words('poem', 'story', 'essay', 'blog', 'email$', 'e-mail$', 'letter$', 'tweet', 'post$', 'rewrite', 'rephrase', 'paraphrase',
  'proofread', 'tone', 'slogan', 'lyrics', 'song$', 'haiku', 'limerick', 'joke', 'scenes?$', 'screenplay', 'movie script', 'dialogue', 'speech', 'novel$', 'plot$', 'characters?$',
  '(a|one|short) paragraph', 'imagine', 'caption', 'headline', 'tagline', 'cover letter', 'bio$', 'product description', 'toast$', 'eulogy', 'fairy tale', 'fiction',
  'make it (sound|more)', 'improve (the|my) (writing|text|wording)', 'grammar', 'pretend', 'roleplay', 'role-play',
  'schreib', 'gedicht', 'geschichte', 'aufsatz', 'brief', 'formulier', 'umformulier', 'korrigier', 'text für', 'rede$', 'witz', 'absatz', 'stell dir vor',
  'märchen', 'liedtext', 'überschrift', 'werbetext');
// Brainstorming: ideas, suggestions and lists of options.
const BRAINSTORM_WORDS = words('ideas?$', 'brainstorm', 'suggest', 'come up with', 'think of', 'ways to', 'ways (in which|that|of)', 'give me (a list|some|\\d+|two|three|four|five|six|seven|eight|nine|ten)',
  'list (of|some|\\d+|two|three|four|five|ten)', 'top \\d+', 'recommend', 'tips (for|on|to)',
  'options for', 'alternatives', 'ideen', 'vorschläge', 'schlag .* vor', 'nenne', 'liste (mit|von|der)', 'tipps', 'empfiehl', 'empfehlung', 'was sind gute');
const TRANSLATION_WORDS = words('translate', 'translation', 'in english', 'into english', 'in german', 'into german', 'auf deutsch', 'auf englisch',
  'übersetz', 'ins deutsche', 'ins englische', 'in french', 'into french', 'auf französisch', 'in spanish', 'into spanish', 'auf spanisch', 'in italian',
  'auf italienisch', 'in japanese', 'in chinese', 'how do you say', 'what does .* mean in', 'wie sagt man');
const SUMMARY_WORDS = words('summari[sz]', 'summary', 'tl;?dr', 'key points', 'main points', 'in short', 'condense', 'recap', 'gist', 'in a nutshell',
  'in (one|two|three|a few|\\d+) sentences', 'briefly describe', 'outline the', 'zusammenfass', 'kernpunkte', 'kurzfassung', 'fasse', 'in kürze', 'das wichtigste');
const EXTRACTION_WORDS = words('extract', 'as json', 'in json', 'json format', 'classify', 'categori[sz]e', 'label', 'fill in', 'table of', 'parse$',
  'pull out', 'list (all|out|down|the)', 'identify (which|the|all|whether)', 'which of (these|the following|those)', 'sort (these|the following|them)',
  'group (these|the following|them)', 'tag (these|each)', 'sentiment', 'comma separated', 'bullet(ed)? list of (the|all)',
  'extrahier', 'als json', 'klassifizier', 'kategorisier', 'tabelle', 'ordne .* zu', 'welche (davon|dieser|der folgenden)', 'stimmung');
// A choice among listed options: "Which is a fish? Tope or Rope", "string or percussion: Cantaro, Gudok".
const OPTION_LIST = /\b(\w+ or \w+)\b[^\n]{0,40}[:?]\s*\S[^\n]{0,120}(,| or )/i;
const ACTION_WORDS = words('search', 'look up', 'open', 'send', 'create', 'schedule', 'book', 'remind', 'run', 'execute', 'download', 'browse',
  'such', 'öffne', 'sende', 'schick', 'erstell', 'erinnere', 'führe', 'buche');
// Agents that work a terminal or a browser. Tool names say what an agent can
// do; the ask and the tools it just called say what this turn is about.
const TERMINAL_TOOLS = words('bash', 'shell', 'terminal', 'exec', 'run command', 'run cmd', 'run terminal', 'command', 'cmd$', 'powershell', 'zsh', 'ssh',
  'subprocess', 'process');
const TERMINAL_WORDS = words('terminal', 'command line', 'kommandozeile', 'shell', 'bash', 'run (the )?(command|script|tests?|build|migration)', 'npm (install|run|test)',
  'pip install', 'yarn', 'git (clone|push|pull|commit|status|log|checkout|rebase|merge)', 'ssh', 'chmod', 'sudo', 'docker (run|build|compose|ps)', 'kubectl',
  'make (build|test)', 'install (the )?(package|dependenc)', 'befehl', 'führe .* aus', 'installier');
const BROWSER_TOOLS = words('browser', 'navigate', 'click', 'goto', 'go to', 'open url', 'open page', 'page', 'screenshot', 'scroll', 'type text', 'fill',
  'hover', 'playwright', 'puppeteer', 'selenium', 'dom$', 'tab$', 'tabs$', 'computer', 'mouse', 'keyboard', 'web page', 'webpage');
const BROWSER_WORDS = words('browser', 'website', 'web ?site', 'web ?page', 'click', 'log ?in (to|on)', 'sign in', 'fill (out|in) (the|this|a) form', 'go to', 'navigate',
  'open (the |this )?(site|page|url|link|tab)', 'scroll', 'screenshot', 'add (it )?to (the )?cart', 'checkout', 'webseite', 'internetseite', 'klick', 'formular',
  'öffne die seite', 'melde dich an', 'geh auf', 'in den warenkorb');
const URL = /https?:\/\/\S+|\bwww\.\S+/i;
const QUESTION_WORDS = words('who', 'what', 'when', 'where', 'which', 'how many', 'how much', 'how does', 'how do', 'how is', 'is there', 'are there', 'does', 'did',
  'tell me about', 'what is', 'define', 'wer', 'was', 'wann', 'wo$', 'woher', 'welche', 'wie viele', 'wie viel', 'wie funktioniert', 'gibt es', 'erzähl mir');
// Asking about a passage: "Given this paragraph, …", "From the text, …".
const PASSAGE_WORDS = words('given (this|the|a) (text|paragraph|passage|reference|article)', 'from the (text|passage|paragraph|article)', 'based on (this|the) (text|passage|paragraph|article)',
  'according to the', 'in this (text|passage|paragraph|article)', 'reference text', 'laut (dem|diesem) text', 'aus dem text', 'im text');
const SMALL_TALK = /^\s*(hi|hey|hello|hallo|servus|moin|yo|thanks?( you)?|thx|danke( dir| schön)?|ok(ay)?|cool|nice|super|good (morning|night)|guten (morgen|abend|tag)|wie geht'?s|how are you)\b[\s!?.,:)]*$/i;
// A short message that builds on code from the last answer ("now add tests").
const FOLLOW_UP_TOKENS = 40;

// How much a turn looks like work with one kind of tool: calling it now says
// most, being asked to it next. Being offered the tool only adds to those:
// agents are offered every tool on every turn, "hi" included. Without tools
// on offer it is a question about the tool ("how do I rebase?"), not work with it.
function toolWork(s, tools, askWords, extra = 0) {
  if (!s.toolCount) return 0;
  const signal = (tools.test(s.calledTools) ? 0.6 : 0) + (askWords.test(s.ask) ? 0.35 : 0);
  return signal ? Math.min(1, signal + (tools.test(s.tools) ? 0.2 : 0) + extra) : 0;
}

const opensWithQuestion = (s) => s.ask.match(QUESTION_WORDS)?.index === 0 || /\?\s*$/.test(s.ask.split('\n')[0]);
const followsCode = (s) => s.previousCode && s.lastUserTokens < FOLLOW_UP_TOKENS && !s.passage;

const KINDS = [
  {
    id: 'chat',
    skills: {},
    difficulty: 0,
    // Short messages that match nothing else count as chat too (classifier.js).
    detect: (s) => (SMALL_TALK.test(s.lastUser) ? 1 : 0),
  },
  {
    id: 'coding',
    skills: { coding: 1, agentic: 0.2 },
    difficulty: 0.45,
    detect: (s) => Math.min(1, (CODE_SYNTAX.test(s.lastUser) ? 0.5 : 0) + 0.25 * Math.min(3, hits(CODE_WORDS, s.ask)) + Math.min(0.3, s.codeBlocks * 0.15)
      + (CODE_ASK.test(s.ask) ? 0.35 : 0) + ((s.ask.match(IDENTIFIER) || []).length >= 2 ? 0.25 : 0) + (followsCode(s) ? 0.4 : 0)),
  },
  {
    id: 'math',
    skills: { math: 1, reasoning: 0.4 },
    difficulty: 0.55,
    detect: (s) => {
      const wordProblem = QUANTITY_QUESTION.test(s.ask) && s.numbers + s.quantityWords >= 2 ? 0.45 + 0.1 * Math.min(3, s.numbers - 1) : 0;
      return Math.min(1, (MATH_SYNTAX.test(s.ask) ? 0.5 : 0) + 0.35 * Math.min(2, hits(MATH_WORDS, s.ask)) + wordProblem);
    },
  },
  {
    id: 'reasoning',
    skills: { reasoning: 1, knowledge: 0.3 },
    difficulty: 0.5,
    detect: (s) => 0.3 * Math.min(2, hits(REASONING_WORDS, s.ask)),
  },
  {
    id: 'writing',
    skills: { instruction: 1, knowledge: 0.3 },
    difficulty: 0.3,
    detect: (s) => Math.min(1, 0.4 * Math.min(2, hits(WRITING_WORDS, s.ask)) + (BRAINSTORM_WORDS.test(s.ask) ? 0.5 : 0)),
  },
  {
    id: 'translation',
    skills: { knowledge: 0.6, instruction: 0.6 },
    difficulty: 0.25,
    detect: (s) => (TRANSLATION_WORDS.test(s.ask) ? 0.85 : 0),
  },
  {
    id: 'summarization',
    skills: { instruction: 0.8, long_context: 0.6 },
    difficulty: 0.3,
    detect: (s) => Math.min(1, (SUMMARY_WORDS.test(s.ask) ? 0.8 : 0) + (s.passage && SUMMARY_WORDS.test(s.ask) ? 0.1 : 0)
      + (s.lastUserTokens > 1500 && !CODE_SYNTAX.test(s.lastUser) ? 0.2 : 0)),
  },
  {
    id: 'extraction',
    skills: { instruction: 1 },
    difficulty: 0.3,
    detect: (s) => Math.min(1, (s.wantsJson ? 0.6 : 0) + 0.5 * Math.min(2, hits(EXTRACTION_WORDS, s.ask)) + (OPTION_LIST.test(s.ask) ? 0.4 : 0)),
  },
  {
    id: 'terminal',
    skills: { terminal: 1, agentic: 0.5, coding: 0.4 },
    difficulty: 0.45,
    detect: (s) => toolWork(s, TERMINAL_TOOLS, TERMINAL_WORDS),
  },
  {
    id: 'browser',
    skills: { agentic: 1, instruction: 0.5 },
    difficulty: 0.4,
    // Pages to visit and screenshots coming back are browser work too.
    detect: (s) => toolWork(s, BROWSER_TOOLS, BROWSER_WORDS, (URL.test(s.ask) ? 0.1 : 0) + (s.hasImages && BROWSER_TOOLS.test(s.calledTools) ? 0.1 : 0)),
  },
  {
    id: 'agent',
    skills: { agentic: 1, instruction: 0.5 },
    difficulty: 0.35,
    // Tools on offer are only a hint (agents send them every turn); asking for
    // an action, or tool results coming back, make it agent work. Work in a
    // terminal or a browser is that kind first, and agent work only besides.
    detect: (s) => {
      if (!s.toolCount) return 0;
      const general = Math.min(1, AGENT_BASELINE + (ACTION_WORDS.test(s.ask) ? 0.4 : 0) + Math.min(0.5, s.toolResults * 0.25));
      const specific = Math.max(toolWork(s, TERMINAL_TOOLS, TERMINAL_WORDS), toolWork(s, BROWSER_TOOLS, BROWSER_WORDS));
      return specific ? Math.min(general, specific / 2) : general;
    },
  },
  {
    id: 'vision',
    skills: {},
    difficulty: 0.35,
    detect: (s) => (s.hasImages ? 0.9 : 0),
  },
  {
    id: 'long_context',
    skills: { long_context: 1 },
    difficulty: 0.45,
    detect: (s) => Math.max(0, Math.min(1, (s.inputTokens - 30000) / 70000)),
  },
  {
    id: 'knowledge',
    skills: { knowledge: 1, reasoning: 0.3 },
    difficulty: 0.3,
    // Open questions, and closed ones about a passage that comes with them.
    detect: (s) => (opensWithQuestion(s) ? 0.45 : 0) + (s.passage && (opensWithQuestion(s) || PASSAGE_WORDS.test(s.ask)) ? 0.15 : 0),
  },
];

// Words that make any request harder, whatever its kind.
const HARD_WORDS = words('prove', 'rigorous', 'optimi[sz]e', 'architecture', 'design a', 'edge cases', 'complex', 'from scratch', 'production', 'scal(e|able)',
  'multi-?step', 'thorough', 'in depth', 'trade-?offs?', 'step[- ]by[- ]step', 'think (carefully|hard|deeply)', 'failure modes?', 'distributed',
  'concurren(t|cy)', 'race conditions?', 'efficient', 'performance', 'secure', 'security', 'thread-?safe', 'asymptotic', 'formal', 'novel approach',
  'research', 'derive', 'generali[sz]e', 'all possible', 'beweise', 'optimier', 'architektur', 'komplex', 'gründlich', 'ausführlich', 'von grund auf',
  'abwägung', 'schritt für schritt', 'nebenläufig', 'effizient', 'sicherheit', 'herleit');

// Below this many tokens, a message that matches no kind is small talk.
const SHORT_MESSAGE_TOKENS = 12;
// What the agent kind scores for tools on offer and nothing else.
const AGENT_BASELINE = 0.25;

module.exports = { KINDS, HARD_WORDS, SHORT_MESSAGE_TOKENS, AGENT_BASELINE, hits, saturate };
