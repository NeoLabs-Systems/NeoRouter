package processing

import (
	"crypto/sha256"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Code detectors for what one regex cannot express: postal addresses built from
// parts that confirm each other, last names scored from name and word lists,
// wallet seed phrases checked against BIP-39, the user's own terms and the
// entities the privacy filter found.
//
// The patterns are in JavaScript syntax, compiled through compileJSPattern so
// the lookarounds work.

// builtinDetectors is the detector set the registry ships.
func builtinDetectors() []*Detector {
	return []*Detector{
		addressDetector(),
		lastNamesDetector(),
		seedPhraseDetector(),
		customTermsDetector(),
		aiEntitiesDetector(),
	}
}

// --- postal addresses -----------------------------------------------------

const (
	streetSuffix = `straße|strasse|str\.|weg|gasse|allee|platz|ring|damm|ufer|chaussee|steig|pfad|stieg|promenade|graben|zeile`
	streetWord   = `Straße|Strasse|Str\.|Weg|Allee|Platz|Ring|Damm|Ufer|Chaussee|Gasse|Markt|Steig`
	addressNum   = `\d{1,4}\s?[a-zA-Z]?(?:\s?[-/]\s?\d{1,4}[a-zA-Z]?)?`
	addressEnd   = `(?![\p{L}\p{N}])`
	addressStart = `(?<![\p{L}\p{N}])`
)

type addressPart struct {
	score   float64
	pattern *jsPattern
}

var (
	addressParts []addressPart
	addressCtx   *jsPattern
	addressJoin  *regexp.Regexp
	addressOnce  sync.Once
	addressErr   error
)

// addressPartsInit compiles the address detector's parts once.
func addressPartsInit() {
	type spec struct {
		score  float64
		source string
		flags  string
	}
	specs := []spec{
		{0.6, addressStart + `\p{Lu}[\p{L}.-]*?(?:` + streetSuffix + `|-(?:` + streetWord + `))\s?` + addressNum + addressEnd, "u"},
		{0.6, addressStart + `(?!(?:Der|Die|Das|Den|Dem|Des|Ein|Eine|Einer|Jeder|Jede|Unser|Unsere|Dieser|Diese|Welche|Welcher)\s)\p{Lu}[\p{L}-]*(?:er|e|es|en)\s+(?:` + streetWord + `)\s?` + addressNum + addressEnd, "u"},
		{0.3, addressStart + `(?:Am|An der|An den|Auf der|Auf dem|Im|In der|In den|Zum|Zur|Unter den|Hinter der|Vor dem|Alt)\s+\p{Lu}[\p{L}-]+\s` + addressNum + addressEnd, "u"},
		{0.7, addressStart + `\d{1,5}\s+(?:[NSEW]\.?\s+)?(?:\p{Lu}[\p{L}'-]*\s+){1,3}(?:Street|St\.|Avenue|Ave\.?|Road|Rd\.?|Lane|Ln\.?|Drive|Dr\.|Boulevard|Blvd\.?|Court|Ct\.?|Place|Pl\.?|Way|Terrace|Parkway|Square|Close|Crescent)(?![\p{L}])`, "u"},
		{0.6, `(?:Postfach|P\.?\s?O\.?\s?Box)\s+\d{1,6}`, "iu"},
		{0.3, addressStart + `(?:D-|A-|CH-)?\d{4,5}\s+\p{Lu}[\p{Ll}ß]+(?:[- ](?:an der|am|im|bei|ob der|\p{Lu}[\p{Ll}ß]+|\(\p{L}+\)))*(?![\p{L}])`, "u"},
		{0.6, `(?<![\p{L}])\p{Lu}[\p{L}]+(?:\s\p{Lu}[\p{L}]+)*,\s[A-Z]{2}\s\d{5}(?:-\d{4})?(?!\d)`, "u"},
		{0.3, `\b(?:[A-Z]{1,2}\d[A-Z\d]?|GIR)\s?\d[A-Z]{2}\b`, ""},
	}
	for _, item := range specs {
		pattern, err := compileJSPattern(item.source, item.flags)
		if err != nil {
			if addressErr == nil {
				addressErr = err
			}
			return
		}
		addressParts = append(addressParts, addressPart{score: item.score, pattern: pattern})
	}
	addressCtx, addressErr = compileJSPattern(`(?<![\p{L}])(?:wohne|wohnhaft|wohnt|adresse|anschrift|address|lives at|live at|lieferadresse|rechnungsadresse|shipping|billing|postanschrift|meldeadresse|zustellung|ship to|send to)(?![\p{L}])`, "iu")
	addressJoin = regexp.MustCompile(`^[ \t]*[,\n]?[ \t]*\n?[ \t]*$`)
}

func addressDetector() *Detector {
	addressOnce.Do(addressPartsInit)
	return &Detector{
		ID:       "postal_address",
		Label:    Label{EN: "Postal address", DE: "Postanschrift"},
		Category: "address",
		File:     "detectors/address.go",
		Scan:     scanAddress,
	}
}

// scanAddress finds every part, lets parts that stand next to each other confirm
// each other, and scores the result.
func scanAddress(text string, _ *ScanContext) []Span {
	addressOnce.Do(addressPartsInit)
	if addressErr != nil {
		return nil
	}
	parts := []Span{}
	for _, part := range addressParts {
		iteratePattern(part.pattern, text, func(loc []int) bool {
			start, end := part.pattern.span(loc)
			parts = append(parts, Span{Start: start, End: end, Score: part.score})
			return true
		})
	}
	sort.SliceStable(parts, func(a, b int) bool {
		if parts[a].Start != parts[b].Start {
			return parts[a].Start < parts[b].Start
		}
		return parts[a].End > parts[b].End
	})
	merged := make([]Span, 0, len(parts))
	for _, part := range parts {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if part.Start < last.End {
				if part.End > last.End {
					last.End = part.End
				}
				last.Score = maxFloat(last.Score, part.Score)
				continue
			}
			if addressJoin.MatchString(text[last.End:part.Start]) {
				last.End = part.End
				last.Score = maxFloat(last.Score, part.Score) + 0.3
				continue
			}
		}
		merged = append(merged, part)
	}
	for index := range merged {
		if addressCtx.matchesAnywhere(beforeRunes(text, merged[index].Start, 40)) {
			merged[index].Score += 0.3
		}
	}
	return merged
}

// beforeRunes is the n runes of text in front of a byte offset, like
// text.slice(start - n, start).
func beforeRunes(text string, byteStart, n int) string {
	start := runeBack(text, byteStart, n)
	if start < 0 {
		start = 0
	}
	return text[start:byteStart]
}

// --- last names -----------------------------------------------------------

const (
	nameWord        = `(?<![\p{L}\p{N}_@./\\#-])\p{L}[\p{L}\p{M}]*(?:['’-]\p{L}[\p{L}\p{M}]+)*(?![\p{L}\p{N}_@])`
	nameCapitalised = `^\p{Lu}[\p{Ll}\p{M}]`
	nameGap         = `^[ \t` + "\u00a0" + `]{1,3}$`
	particleGroup   = `(?:von|van|de|der|den|del|della|di|da|du|dos|das|le|la|zu|vom|zum|ter|ten|af|bin|ibn|al|el|mac)`
	addressedSrc    = `(?:^|[^\p{L}])(?:herrn?|frau|hr\.|fr\.|dr\.|prof\.|mr\.?|mrs\.?|ms\.?|mx\.?|miss|sir|madame|monsieur|mme\.?|mlle\.?|signor|signora|señor|señora|sr\.|sra\.)(?:\s+(?:dr|prof|med|ing|dipl\.-ing|rer\.\s?nat)\.?)*\s+((?:` + particleGroup + `\s+)*)$`
	introSrc        = `(?:^|[^\p{L}])(?:hallo|hi|hey|moin|servus|dear|liebe[rs]?|sehr geehrte[rs]?|guten (?:morgen|tag|abend)|name(?:\s+(?:ist|is|lautet))?\s*:?|namens|heiße|heisse|ich bin|i am|i'm|this is|hier ist|ansprechpartner(?:in)?:?|kontakt:?|contact:?|gez\.|signed|cc:?)[\s,:]*$`
	signOffSrc      = `(?:grüße|grüßen|gruß|gruss|grüsse|regards|best|cheers|thanks|thank you|danke|sincerely|mfg|lg|vg|bg)[\s,!.]*\n[ \t]*$`
)

var nameParticles = map[string]struct{}{
	"von": {}, "van": {}, "de": {}, "der": {}, "den": {}, "del": {}, "della": {}, "di": {}, "da": {},
	"du": {}, "dos": {}, "das": {}, "le": {}, "la": {}, "zu": {}, "vom": {}, "zum": {}, "ter": {},
	"ten": {}, "af": {}, "bin": {}, "ibn": {}, "al": {}, "el": {}, "mac": {},
}

var (
	nameWordPattern  *jsPattern
	nameCapitalisedR *regexp.Regexp
	nameGapR         *regexp.Regexp
	addressedPattern *jsPattern
	introPattern     *jsPattern
	signOffPattern   *jsPattern
	nameListPattern  *regexp.Regexp
	nameOnce         sync.Once
	nameErr          error
)

func namesInit() {
	nameWordPattern, nameErr = compileJSPattern(nameWord, "u")
	if nameErr != nil {
		return
	}
	nameCapitalisedR = regexp.MustCompile(nameCapitalised)
	nameGapR = regexp.MustCompile(nameGap)
	nameListPattern = regexp.MustCompile(`^,[ \t]?$`)
	if addressedPattern, nameErr = compileJSPattern(addressedSrc, "u"); nameErr != nil {
		return
	}
	if introPattern, nameErr = compileJSPattern(introSrc, "u"); nameErr != nil {
		return
	}
	signOffPattern, nameErr = compileJSPattern(signOffSrc, "u")
}

func lastNamesDetector() *Detector {
	nameOnce.Do(namesInit)
	return &Detector{
		ID:       "last_names",
		Label:    Label{EN: "Last name", DE: "Nachname"},
		Category: "person",
		File:     "detectors/names.go",
		Scan:     scanLastNames,
	}
}

// nameWordItem is one capitalised or lowercase word of a text.
type nameWordItem struct {
	start, end  int
	text        string
	lower       string
	capitalised bool
}

// scanLastNames hides family names: the word after a known first name, after a
// form of address, or in "Schmidt, Anna" form.
func scanLastNames(text string, ctx *ScanContext) []Span {
	nameOnce.Do(namesInit)
	if nameErr != nil || ctx.Lists == nil {
		return nil
	}
	lists := nameLists{
		first:    ctx.Lists.WordList("first_names"),
		last:     ctx.Lists.WordList("last_names"),
		common:   ctx.Lists.WordList("common_words"),
		frequent: ctx.Lists.WordList("frequent_words"),
	}
	inList := func(word nameWordItem, list map[string]struct{}) bool {
		if _, ok := list[word.lower]; !ok {
			return false
		}
		if word.capitalised {
			return true
		}
		_, frequent := lists.frequent[word.lower]
		_, common := lists.common[word.lower]
		return !frequent && !common
	}
	isFirst := func(word nameWordItem) bool { return inList(word, lists.first) }
	isLast := func(word nameWordItem) bool { return inList(word, lists.last) }
	isWordGap := func(from, to int) bool { return nameGapR.MatchString(text[from:to]) }
	words := nameWords(text)
	spans := []Span{}
	lowerBefore := func(start int) string { return strings.ToLower(beforeRunes(text, start, 48)) }
	for index := 0; index < len(words); index++ {
		word := words[index]
		if isFirst(word) && !(isLast(word) && index > 0 && isFirst(words[index-1]) && isWordGap(words[index-1].end, word.start)) {
			// Skip up to two middle names, then expect the last name on the same
			// line.
			next := index + 1
			for next < len(words) && next-index <= 2 && isFirst(words[next]) && !isLast(words[next]) &&
				next+1 < len(words) && isWordGap(words[next-1].end, words[next].start) {
				next++
			}
			if next >= len(words) {
				continue
			}
			surname := words[next]
			join, ok := particlesBetween(text, words[next-1].end, surname.start)
			if !ok {
				continue
			}
			if isFirst(surname) && !isLast(surname) {
				continue
			}
			score := 0.4 + lastNameScore(surname, lists, inList)
			if _, common := lists.common[word.lower]; common && !isLast(surname) {
				score -= 0.3
			}
			if !word.capitalised || !surname.capitalised {
				score -= 0.3
			}
			prefix := lowerBefore(word.start)
			if addressedPattern.matchesAnywhere(prefix) || introPattern.matchesAnywhere(prefix) ||
				signOffPattern.matchesAnywhere(beforeRunes(text, word.start, 48)) {
				score += 0.3
			}
			spans = append(spans, Span{Start: join, End: surname.end, Score: score})
			index = next
			continue
		}
		prefix := lowerBefore(word.start)
		if groups := addressedPattern.findSubmatch(prefix); groups != nil {
			known := isLast(word)
			if !known && !word.capitalised {
				continue
			}
			score := 0.6
			if known {
				score += 0.2
			} else if _, common := lists.common[word.lower]; common {
				score -= 0.5
			}
			particles := groups[1]
			spans = append(spans, Span{Start: word.start - len(particles), End: word.end, Score: score})
			continue
		}
		// "Schmidt, Anna"
		if index+1 < len(words) && isLast(word) {
			following := words[index+1]
			if _, isFirstName := lists.first[word.lower]; !isFirstName &&
				nameListPattern.MatchString(text[word.end:following.start]) && isFirst(following) {
				spans = append(spans, Span{Start: word.start, End: word.end, Score: 0.7})
			}
		}
	}
	return spans
}

type nameLists struct {
	first, last, common, frequent map[string]struct{}
}

func lastNameScore(word nameWordItem, lists nameLists, inList func(nameWordItem, map[string]struct{}) bool) float64 {
	if inList(word, lists.last) {
		return 0.3
	}
	if !word.capitalised {
		return -1
	}
	if _, common := lists.common[word.lower]; common {
		return -0.5
	}
	return 0.2
}

// nameWords is every word except lowercase particles, which stay in the gaps
// ("von der").
func nameWords(text string) []nameWordItem {
	words := []nameWordItem{}
	iteratePattern(nameWordPattern, text, func(loc []int) bool {
		start, end := nameWordPattern.span(loc)
		word := text[start:end]
		lower := strings.ToLower(word)
		if _, isParticle := nameParticles[lower]; isParticle {
			return true
		}
		words = append(words, nameWordItem{
			start:       start,
			end:         end,
			text:        word,
			lower:       lower,
			capitalised: nameCapitalisedR.MatchString(word),
		})
		return true
	})
	return words
}

// particlesBetween is the gap between two words: a short same-line gap, or
// nothing but particles ("von der"); ok is false when neither.
func particlesBetween(text string, from, to int) (int, bool) {
	gap := text[from:to]
	if nameGapR.MatchString(gap) {
		return to, true
	}
	if !startsWithSpaceRun(gap) || !endsWithSpaceRun(gap) || strings.Contains(gap, "\n") {
		return 0, false
	}
	parts := splitSpaceRuns(strings.Trim(gap, " \t\u00a0"))
	if len(parts) == 0 {
		return 0, false
	}
	for _, part := range parts {
		if _, isParticle := nameParticles[strings.ToLower(part)]; !isParticle {
			return 0, false
		}
	}
	return from + strings.Index(gap, parts[0]), true
}

func startsWithSpaceRun(text string) bool {
	return len(text) > 0 && (text[0] == ' ' || text[0] == '\t')
}

func endsWithSpaceRun(text string) bool {
	if text == "" {
		return false
	}
	last := text[len(text)-1]
	return last == ' ' || last == '\t'
}

func splitSpaceRuns(text string) []string {
	out := []string{}
	current := strings.Builder{}
	for index := 0; index < len(text); index++ {
		if text[index] == ' ' || text[index] == '\t' {
			if current.Len() > 0 {
				out = append(out, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(text[index])
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

// --- seed phrases ---------------------------------------------------------

var (
	seedLengths   = map[int]struct{}{12: {}, 15: {}, 18: {}, 21: {}, 24: {}}
	seedSeparator *regexp.Regexp
	seedOnce      sync.Once
)

func seedPhraseDetector() *Detector {
	seedOnce.Do(func() {
		seedSeparator = regexp.MustCompile(`^[` + classContents(jsSpace) + `,;]*(?:\d{1,2}[.):]?[` + classContents(jsSpace) + `]*)?$`)
	})
	return &Detector{
		ID:       "seed_phrase",
		Label:    Label{EN: "Wallet recovery phrase", DE: "Wiederherstellungsphrase einer Wallet"},
		Category: "secret",
		File:     "detectors/seed_phrase.go",
		Scan:     scanSeedPhrase,
	}
}

// scanSeedPhrase finds 12 to 24 consecutive BIP-39 words, optionally numbered. A
// phrase whose checksum is right is certain; one with the right length but a
// wrong checksum is still hidden, because a typo does not make it harmless.
func scanSeedPhrase(text string, ctx *ScanContext) []Span {
	if ctx.Lists == nil {
		return nil
	}
	list := ctx.Lists.WordList("bip39_english")
	if len(list) == 0 {
		return nil
	}
	index := func(word string) (int, bool) {
		value, ok := seedWordIndex(ctx.Lists)[word]
		return value, ok
	}
	spans := []Span{}
	run := []seedWord{}
	closeRun := func() {
		if len(run) >= 12 {
			words := make([]string, 0, len(run))
			for _, word := range run {
				words = append(words, word.text)
			}
			score := 0.6
			if _, known := seedLengths[len(words)]; known {
				if checksumValid(words, index) {
					score = 1
				} else {
					score = 0.7
				}
			}
			spans = append(spans, Span{Start: run[0].start, End: run[len(run)-1].end, Score: score})
		}
		run = nil
	}
	// The pattern matches [a-z]+ in the lowercased text; scanning the original
	// keeps the spans byte-accurate, so map each rune as it is read.
	for _, match := range wordsLower(text) {
		isWord := false
		if _, ok := list[match.text]; ok {
			isWord = true
			if match.start > 0 {
				previous := text[match.start-1]
				if isASCIILetter(previous) {
					isWord = false
				}
			}
		}
		if isWord && (len(run) == 0 || seedSeparator.MatchString(text[run[len(run)-1].end:match.start])) {
			run = append(run, match)
			continue
		}
		closeRun()
		if isWord {
			run = append(run, match)
		}
	}
	closeRun()
	return spans
}

type seedWord struct {
	start, end int
	text       string
}

// wordsLower is every run of ASCII letters, lowercased, with byte offsets.
func wordsLower(text string) []seedWord {
	words := []seedWord{}
	for index := 0; index < len(text); {
		char := text[index]
		if char >= 'A' && char <= 'Z' {
			start := index
			for index < len(text) && isASCIILetter(text[index]) {
				index++
			}
			words = append(words, seedWord{start: start, end: index, text: strings.ToLower(text[start:index])})
			continue
		}
		if char >= 'a' && char <= 'z' {
			start := index
			for index < len(text) && isASCIILetter(text[index]) {
				index++
			}
			words = append(words, seedWord{start: start, end: index, text: text[start:index]})
			continue
		}
		index++
	}
	return words
}

func isASCIILetter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

var seedIndexMu sync.Mutex
var seedIndexes = map[*Registry]map[string]int{}

// seedWordIndex is the word list with the position of each word, which is also
// the word's value.
func seedWordIndex(registry *Registry) map[string]int {
	seedIndexMu.Lock()
	defer seedIndexMu.Unlock()
	if index, ok := seedIndexes[registry]; ok {
		return index
	}
	index := map[string]int{}
	raw := registry.seedWords()
	for position, word := range raw {
		index[word] = position
	}
	seedIndexes[registry] = index
	return index
}

// seedWords is data/bip39_english.txt in file order (the order matters: a word's
// position is its value).
func (r *Registry) seedWords() []string {
	words := make([]string, 0, 2048)
	raw, err := listFiles.ReadFile("shield/data/bip39_english.txt")
	if err != nil {
		return words
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words = append(words, strings.ToLower(line))
	}
	return words
}

func checksumValid(words []string, index func(string) (int, bool)) bool {
	bits := strings.Builder{}
	for _, word := range words {
		value, ok := index(word)
		if !ok {
			return false
		}
		encoded := strconv.FormatInt(int64(value), 2)
		bits.WriteString(strings.Repeat("0", 11-len(encoded)))
		bits.WriteString(encoded)
	}
	all := bits.String()
	checksumBits := len(words) / 3
	entropyBits := all[:len(all)-checksumBits]
	entropy := make([]byte, 0, len(entropyBits)/8)
	for index := 0; index+8 <= len(entropyBits); index += 8 {
		value, err := strconv.ParseUint(entropyBits[index:index+8], 2, 8)
		if err != nil {
			return false
		}
		entropy = append(entropy, byte(value))
	}
	hash := sha256.Sum256(entropy)
	first := strconv.FormatUint(uint64(hash[0]), 2)
	first = strings.Repeat("0", 8-len(first)) + first
	return first[:checksumBits] == all[len(all)-checksumBits:]
}

// --- the user's terms and the AI entities ---------------------------------

var (
	termMu       sync.Mutex
	termPatterns = map[string]*jsPattern{}
)

func customTermsDetector() *Detector {
	return &Detector{
		ID:       "custom_terms",
		Label:    Label{EN: "Your private terms", DE: "Deine privaten Begriffe"},
		Category: "custom",
		File:     "detectors/terms.go",
		Scan:     scanCustomTerms,
	}
}

func aiEntitiesDetector() *Detector {
	return &Detector{
		ID: "ai_entities",
		Label: Label{
			EN: "AI detection",
			DE: "KI-Erkennung",
		},
		Category: "person",
		File:     "detectors/terms.go",
		Scan:     scanAIEntities,
	}
}

// termPattern is a term as one whole-word pattern, cached.
func termPattern(term string) (*jsPattern, error) {
	termMu.Lock()
	defer termMu.Unlock()
	if pattern, ok := termPatterns[term]; ok {
		return pattern, nil
	}
	if len(termPatterns) > 5000 {
		termPatterns = map[string]*jsPattern{}
	}
	pattern, err := compileJSPattern(`(?<![\p{L}\p{N}])`+escapeRegExp(term)+`(?![\p{L}\p{N}])`, "iu")
	if err != nil {
		return nil, err
	}
	termPatterns[term] = pattern
	return pattern, nil
}

func scanCustomTerms(text string, ctx *ScanContext) []Span {
	spans := []Span{}
	for _, term := range ctx.CustomTerms {
		pattern, err := termPattern(term)
		if err != nil {
			continue
		}
		iteratePattern(pattern, text, func(loc []int) bool {
			start, end := pattern.span(loc)
			spans = append(spans, Span{Start: start, End: end, Value: text[start:end]})
			return true
		})
	}
	return spans
}

func scanAIEntities(text string, ctx *ScanContext) []Span {
	spans := []Span{}
	for _, entity := range ctx.Entities {
		value := entity.Text
		if entity.Category == "person" {
			value = withoutFirstNames(strings.TrimSpace(entity.Text), ctx.Lists)
		}
		if len([]rune(value)) < 2 {
			continue
		}
		for _, span := range exactSpans(text, value) {
			span.Category = entity.Category
			spans = append(spans, span)
		}
	}
	return spans
}

// exactSpans is every occurrence of value in text.
func exactSpans(text, value string) []Span {
	spans := []Span{}
	for index := 0; ; {
		found := strings.Index(text[index:], value)
		if found < 0 {
			return spans
		}
		start := index + found
		spans = append(spans, Span{Start: start, End: start + len(value), Value: value})
		index = start + len(value)
	}
}

// withoutFirstNames keeps the leading first names of a full name: only the
// family name is hidden.
func withoutFirstNames(name string, registry *Registry) string {
	first := registry.WordList("first_names")
	parts := splitFields(name)
	index := 0
	for index < len(parts)-1 {
		if _, ok := first[strings.ToLower(parts[index])]; !ok {
			break
		}
		index++
	}
	if len(parts) == 1 {
		if _, ok := first[strings.ToLower(parts[0])]; ok {
			return ""
		}
	}
	return strings.Join(parts[index:], " ")
}

// splitFields splits on runs of whitespace after a trim.
func splitFields(text string) []string {
	return strings.Fields(text)
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
