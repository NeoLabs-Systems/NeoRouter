package catalog

import (
	"regexp"
	"strings"
)

// mediaNoise are the host, vendor and version tokens arena slugs carry and
// provider ids do not.
var mediaNoise = map[string]bool{"fal": true, "replicate": true, "together": true, "black": true,
	"forest": true, "labs": true, "bfl": true, "google": true, "openai": true, "preview": true,
	"latest": true, "free": true, "v1": true, "api": true}

var (
	mediaBrackets = regexp.MustCompile(`[\[\]()]`)
	mediaSplit    = regexp.MustCompile(`[^a-z0-9]+`)
	mediaInner    = regexp.MustCompile(`\(([^)]*)\)`)
	fourDigitYear = regexp.MustCompile(`\d{4}`)
)

// mediaKey reduces an arena slug or a provider model id to one lookup key:
// "flux_flux-2" and "black-forest-labs/flux.2-klein-4b" still differ in their
// remaining tokens, but the host noise and the repeats are gone.
func mediaKey(text string) string {
	cleaned := strings.ToLower(text)
	cleaned = mediaBrackets.ReplaceAllString(cleaned, " ")
	parts := mediaSplit.Split(cleaned, -1)
	tokens := make([]string, 0, len(parts))
	for _, token := range parts {
		if token == "" || mediaNoise[token] || isDigits(token, -1) && len(token) >= 4 && len(token) <= 8 ||
			isDigits(token, 4) && (strings.HasPrefix(token, "19") || strings.HasPrefix(token, "20")) {
			continue
		}
		tokens = append(tokens, token)
	}
	// "flux_flux-2" → "flux-2": drop a token that repeats its predecessor.
	deduped := make([]string, 0, len(tokens))
	for i, token := range tokens {
		if i > 0 && token == tokens[i-1] {
			continue
		}
		deduped = append(deduped, token)
	}
	return strings.Join(deduped, "-")
}

// mediaNamesOf lists the names one arena record answers to: arena names often
// carry both names ("Nano Banana 2 (Gemini 3.1 Flash Image)").
func mediaNamesOf(record MediaRecord) []string {
	name := record.Name
	inner := ""
	if match := mediaInner.FindStringSubmatch(name); len(match) > 1 {
		inner = match[1]
	}
	withoutParens := mediaInner.ReplaceAllString(name, " ")
	if fourDigitYear.MatchString(inner) {
		inner = ""
	}
	return []string{record.Slug, name, withoutParens, inner}
}

// buildMediaIndex maps every name of every record onto the record.
func buildMediaIndex(records []MediaRecord) map[string]MediaRecord {
	index := make(map[string]MediaRecord, len(records))
	for _, record := range records {
		for _, name := range mediaNamesOf(record) {
			key := mediaKey(name)
			if key == "" {
				continue
			}
			if _, taken := index[key]; !taken {
				index[key] = record
			}
		}
	}
	return index
}

// mediaCandidates are the ids to try for a model, most specific first.
func mediaCandidates(kind, id string) []string {
	base := id
	if at := strings.LastIndexByte(base, '/'); at >= 0 {
		base = base[at+1:]
	}
	variants := []string{id, base}
	if trimmed := trimGeminiNano(base); trimmed != base {
		variants = append(variants, trimmed)
	}
	if kind == "speech" {
		variants = append(variants, trimSpeechSuffix(base))
	}
	return uniqueStrings(variants)
}

var speechSuffix = regexp.MustCompile(`-tts(-preview)?$|-preview-tts$`)

// trimGeminiNano drops the vendor word from a "gemini-nano-…" id: the arena
// lists those models by their nickname.
func trimGeminiNano(base string) string {
	const prefix = "gemini-"
	if strings.HasPrefix(base, prefix+"nano-") {
		return base[len(prefix):]
	}
	return base
}

func trimSpeechSuffix(base string) string { return speechSuffix.ReplaceAllString(base, "") }

// mediaIndex is one arena: its lookup keys and the ELO range of the data.
type mediaIndex struct {
	Map map[string]MediaRecord
	Min float64
	Max float64
}

// mediaArenaScore is a media model's arena result on the 0..1 scale.
type mediaArenaScore struct {
	Record      MediaRecord
	Score       float64
	ReleaseDate string
}

// buildMediaIndexes indexes every arena that has records.
func buildMediaIndexes(source map[string][]MediaRecord) map[string]*mediaIndex {
	result := map[string]*mediaIndex{}
	for kind, records := range source {
		if len(records) == 0 {
			continue
		}
		index := &mediaIndex{Map: buildMediaIndex(records), Min: records[0].Elo, Max: records[0].Elo}
		for _, record := range records[1:] {
			index.Min = mathMin(index.Min, record.Elo)
			index.Max = mathMax(index.Max, record.Elo)
		}
		result[kind] = index
	}
	return result
}

// lookupMedia finds a media model in its arena and normalizes its ELO: the
// best model is 1, the weakest 0.2.
func lookupMedia(indexByKind map[string]*mediaIndex, kind string, modelIDs []string) *mediaArenaScore {
	index := indexByKind[kind]
	if index == nil || len(index.Map) == 0 {
		return nil
	}
	for _, id := range modelIDs {
		for _, text := range mediaCandidates(kind, id) {
			record, found := index.Map[mediaKey(text)]
			if !found {
				continue
			}
			span := mathMax(1, index.Max-index.Min)
			release := ""
			if record.ReleaseDate != nil {
				release = *record.ReleaseDate
			}
			return &mediaArenaScore{
				Record:      record,
				Score:       0.2 + 0.8*((record.Elo-index.Min)/span),
				ReleaseDate: release,
			}
		}
	}
	return nil
}
