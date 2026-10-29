'use strict';

// The kinds of task NeoRouter recognises in a request. Each kind is data: how
// to spot it (a detector over the request signals, mostly word patterns in
// English and German), which skills it calls on (skills.js) and how hard it
// usually is. Adding a kind, or a language, means adding an entry here; the
// router itself knows no kind by name.
//
// detect(signals) returns 0..1: how strongly the request looks like this kind.
// difficulty is the kind's typical difficulty (0..1), mixed into the
// request's own difficulty (classifier.js).

// Patterns match from the start of a word, so German stems ("schreib")
// cover their forms; end a pattern with "$" to require the whole word.
const words = (...patterns) => new RegExp(`(^|[^\\p{L}])(${patterns.map((pattern) => pattern.replace(/\$$/, '(?![\\p{L}])')).join('|')})`, 'iu');
const hits = (pattern, text) => (text.match(new RegExp(pattern.source, `${pattern.flags.replace('g', '')}g`)) || []).length;
const saturate = (count, full = 3) => Math.min(1, count / full);

const CODE_WORDS = words('function', 'class', 'def ', 'import ', 'const ', 'return ', 'async ', 'await ', 'compile', 'refactor', 'debug', 'bug', 'stack ?trace',
  'exception', 'regex', 'sql', 'json', 'api', 'endpoint', 'typescript', 'javascript', 'python', 'rust', 'golang', 'java', 'kotlin', 'swift', 'dart', 'flutter',
  'react', 'node', 'docker', 'kubernetes', 'bash', 'shell', 'git$', 'unit ?test', 'code', 'programm', 'skript', 'fehlermeldung', 'kompilier',
  'backend', 'frontend', 'database', 'datenbank', 'postgres', 'mysql', 'redis', 'kafka', 'microservice', 'server$');
const CODE_SYNTAX = /```|=>|\)\s*\{|;\s*$|^\s*(def|class|import|from|const|let|var|function|public|private|#include)\b|\b\w+\.(js|ts|py|go|rs|java|kt|swift|dart|cpp|rb|php|sql|ya?ml|json)\b|Traceback \(most recent call last\)|at \w+ \(.+:\d+:\d+\)/m;
const MATH_WORDS = words('prove', 'proof', 'theorem', 'lemma', 'integral', 'derivative', 'differentiate', 'equation', 'solve for', 'probability', 'matrix',
  'eigen', 'limit of', 'calculate', 'compute', 'beweise', 'beweis', 'gleichung', 'ableitung', 'wahrscheinlichkeit', 'berechne', 'löse');
const MATH_SYNTAX = /\$[^$]+\$|\\(frac|sum|int|sqrt|lim|cdot)|\b\d+\s*[\^*/+-]\s*\d+\s*=|∫|∑|√|≤|≥/;
const REASONING_WORDS = words('step by step', 'why', 'explain', 'compare', 'pros and cons', 'trade-?offs?', 'analy[sz]e', 'evaluate', 'plan', 'strategy',
  'decide', 'should i', 'reason', 'puzzle', 'riddle', 'warum', 'erkläre', 'erklär', 'vergleiche', 'analysiere', 'bewerte', 'vor- und nachteile', 'soll ich', 'schritt für schritt');
const WRITING_WORDS = words('poem', 'story', 'essay', 'blog', 'email$', 'e-mail$', 'letter$', 'tweet', 'post$', 'rewrite', 'rephrase',
  'proofread', 'tone', 'slogan', 'lyrics', 'schreib', 'gedicht', 'geschichte', 'aufsatz', 'brief', 'formulier', 'umformulier', 'korrigier', 'text für');
const TRANSLATION_WORDS = words('translate', 'translation', 'in english', 'into english', 'in german', 'into german', 'auf deutsch', 'auf englisch',
  'übersetz', 'ins deutsche', 'ins englische', 'in french', 'auf französisch', 'in spanish', 'auf spanisch');
const SUMMARY_WORDS = words('summari[sz]e', 'summary', 'tl;?dr', 'key points', 'main points', 'in short', 'condense', 'zusammenfass', 'kernpunkte', 'kurzfassung', 'fasse');
const EXTRACTION_WORDS = words('extract', 'as json', 'in json', 'json format', 'classify', 'categori[sz]e', 'label', 'fill in', 'table of',
  'extrahier', 'als json', 'klassifizier', 'kategorisier', 'tabelle');
const ACTION_WORDS = words('search', 'look up', 'open', 'send', 'create', 'schedule', 'book', 'remind', 'run', 'execute', 'download', 'browse',
  'such', 'öffne', 'sende', 'schick', 'erstell', 'erinnere', 'führe', 'buche');
const QUESTION_WORDS = words('who', 'what', 'when', 'where', 'which', 'how many', 'how much', 'wer', 'was', 'wann', 'wo', 'welche', 'wie viele', 'wie viel');
const SMALL_TALK = /^\s*(hi|hey|hello|hallo|servus|moin|yo|thanks?( you)?|thx|danke( dir| schön)?|ok(ay)?|cool|nice|super|good (morning|night)|guten (morgen|abend|tag)|wie geht'?s|how are you)\b[\s!?.,:)]*$/i;

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
    detect: (s) => Math.min(1, (CODE_SYNTAX.test(s.lastUser) ? 0.5 : 0) + 0.25 * Math.min(3, hits(CODE_WORDS, s.lastUser)) + Math.min(0.3, s.codeBlocks * 0.15)),
  },
  {
    id: 'math',
    skills: { math: 1, reasoning: 0.4 },
    difficulty: 0.55,
    detect: (s) => Math.min(1, (MATH_SYNTAX.test(s.lastUser) ? 0.5 : 0) + 0.35 * Math.min(2, hits(MATH_WORDS, s.lastUser))),
  },
  {
    id: 'reasoning',
    skills: { reasoning: 1, knowledge: 0.3 },
    difficulty: 0.5,
    detect: (s) => 0.3 * Math.min(2, hits(REASONING_WORDS, s.lastUser)),
  },
  {
    id: 'writing',
    skills: { instruction: 1, knowledge: 0.3 },
    difficulty: 0.3,
    detect: (s) => 0.4 * Math.min(2, hits(WRITING_WORDS, s.lastUser)),
  },
  {
    id: 'translation',
    skills: { knowledge: 0.6, instruction: 0.6 },
    difficulty: 0.25,
    detect: (s) => (TRANSLATION_WORDS.test(s.lastUser) ? 0.8 : 0),
  },
  {
    id: 'summarization',
    skills: { instruction: 0.8, long_context: 0.6 },
    difficulty: 0.3,
    detect: (s) => (SUMMARY_WORDS.test(s.lastUser) ? 0.8 : 0) + (s.lastUserTokens > 1500 && !CODE_SYNTAX.test(s.lastUser) ? 0.2 : 0),
  },
  {
    id: 'extraction',
    skills: { instruction: 1 },
    difficulty: 0.3,
    detect: (s) => Math.min(1, (s.wantsJson ? 0.6 : 0) + (EXTRACTION_WORDS.test(s.lastUser) ? 0.5 : 0)),
  },
  {
    id: 'agent',
    skills: { agentic: 1, instruction: 0.5 },
    difficulty: 0.35,
    // Tools on offer are only a hint (agents send them every turn); asking for
    // an action, or tool results coming back, make it agent work.
    detect: (s) => (s.toolCount ? Math.min(1, 0.25 + (ACTION_WORDS.test(s.lastUser) ? 0.4 : 0) + Math.min(0.5, s.toolResults * 0.25)) : 0),
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
    detect: (s) => (QUESTION_WORDS.test(s.lastUser) && /\?\s*$/.test(s.lastUser.trim()) ? 0.4 : 0),
  },
];

// Words that make any request harder, whatever its kind.
const HARD_WORDS = words('prove', 'rigorous', 'optimi[sz]e', 'architecture', 'design a', 'edge cases', 'complex', 'from scratch', 'production', 'scal(e|able)',
  'multi-?step', 'thorough', 'in depth', 'beweise', 'optimier', 'architektur', 'komplex', 'gründlich', 'ausführlich', 'von grund auf');

// Below this many tokens, a message that matches no kind is small talk.
const SHORT_MESSAGE_TOKENS = 12;

module.exports = { KINDS, HARD_WORDS, SHORT_MESSAGE_TOKENS, hits, saturate };
