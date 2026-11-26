package localmodels

// speechparts.go splits text for speech: text-to-speech tokenizers truncate
// long input, so text is spoken in parts — sentences first, then words, so no
// part is longer than the limit.

import (
	"regexp"
	"strings"
)

// MaxSpeechChars is the longest part a voice speaks in one go.
const MaxSpeechChars = 300

var (
	// sentencePattern splits after sentence ends, in either script.
	sentencePattern = regexp.MustCompile(`[^.!?。！？]+[.!?。！？]*\s*`)
	spaceRun        = regexp.MustCompile(`\s+`)
)

// splitPieces breaks one sentence longer than max into pieces of at most max
// characters, each ending at a space where one can be found, the way
// `.{1,max}(\s|$)` does.
func splitPieces(sentence string, max int) []string {
	runes := []rune(sentence)
	pieces := []string{}
	for index := 0; index < len(runes); {
		limit := index + max
		if limit > len(runes) {
			limit = len(runes)
		}
		cut := limit
		if limit < len(runes) {
			for position := limit - 1; position > index; position-- {
				if isSpace(runes[position]) {
					cut = position + 1
					break
				}
			}
		}
		pieces = append(pieces, string(runes[index:cut]))
		index = cut
	}
	return pieces
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == 0x00A0
}

// SpeechParts splits text into parts of at most max characters (MaxSpeechChars
// when max is zero), ending at sentence ends where possible.
func SpeechParts(text string, max int) []string {
	if max <= 0 {
		max = MaxSpeechChars
	}
	collapsed := strings.TrimSpace(spaceRun.ReplaceAllString(text, " "))
	sentences := sentencePattern.FindAllString(collapsed, -1)
	parts := []string{}
	current := ""
	for _, sentence := range sentences {
		pieces := []string{sentence}
		if len([]rune(sentence)) > max {
			pieces = splitPieces(sentence, max)
		}
		for _, piece := range pieces {
			if len([]rune(current+piece)) > max && current != "" {
				parts = append(parts, strings.TrimSpace(current))
				current = ""
			}
			current += piece
		}
	}
	if strings.TrimSpace(current) != "" {
		parts = append(parts, strings.TrimSpace(current))
	}
	return parts
}
