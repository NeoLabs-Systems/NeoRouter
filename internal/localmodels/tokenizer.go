package localmodels

// tokenizer.go loads the tokenizer.json a speech model ships and turns text
// into the token ids the ONNX graphs take. The three tokenizer families the
// catalog's models use are supported: byte-level BPE (Whisper, Moonshine), a
// Unigram vocabulary (the MMS voices' SentencePiece models) and WordPiece. It
// follows the same shapes refs/NeoRecall/internal/embeddings/tokenizer.go uses.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// tokenizerJSON is the parts of tokenizer.json this package reads.
type tokenizerJSON struct {
	Model struct {
		Type  string `json:"type"`
		UnkID int    `json:"unk_id"`
		// Vocab is an object for byte-level BPE and an array of [token, score]
		// pairs for Unigram, so it is decoded twice.
		Vocab  json.RawMessage `json:"vocab"`
		Merges []any           `json:"merges"`
	} `json:"model"`
	// RawVocab is the flat token→id map a BPE tokenizer ships.
	RawVocab    map[string]int `json:"vocab"`
	AddedTokens []struct {
		ID      int32  `json:"id"`
		Content string `json:"content"`
	} `json:"added_tokens"`
}

// Tokenizer is one model's tokenizer.
type Tokenizer struct {
	kind   string // BPE, Unigram or WordPiece
	vocab  map[string]int32
	tokens map[int32]string
	scores map[string]float64
	merges map[[2]string]int
	unkID  int32
	bosID  int32
	eosID  int32
	padID  int32
	// byteToRune and runeToByte map between raw bytes and the printable
	// characters a byte-level BPE vocabulary uses.
	byteToRune [256]rune
	runeToByte map[rune]byte
	maxRune    int
}

// gpt2BytesToUnicode is the byte→character table byte-level BPE vocabularies
// use: the printable Latin range, then the remaining bytes in order.
func gpt2BytesToUnicode() [256]rune {
	var table [256]rune
	inRange := func(b int) bool {
		return (b >= 33 && b <= 126) || (b >= 161 && b <= 172) || (b >= 174 && b <= 255)
	}
	next := 0
	for b := 0; b < 256; b++ {
		if inRange(b) {
			table[b] = rune(b)
			continue
		}
		table[b] = rune(256 + next)
		next++
	}
	return table
}

// LoadTokenizer reads tokenizer.json.
func LoadTokenizer(path string) (*Tokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed tokenizerJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("localmodels: read %s: %w", path, err)
	}
	t := &Tokenizer{
		kind:       strings.TrimSpace(parsed.Model.Type),
		vocab:      make(map[string]int32, len(parsed.RawVocab)+len(parsed.Model.Vocab)),
		tokens:     map[int32]string{},
		scores:     map[string]float64{},
		merges:     map[[2]string]int{},
		runeToByte: map[rune]byte{},
		unkID:      -1,
		bosID:      -1,
		eosID:      -1,
		padID:      -1,
	}
	table := gpt2BytesToUnicode()
	t.byteToRune = table
	for b, r := range table {
		t.runeToByte[r] = byte(b)
	}
	if parsed.RawVocab == nil {
		parsed.RawVocab = map[string]int{}
	}
	// tokenizer.json keeps a byte-level BPE vocabulary under model.vocab as an
	// object; a Unigram one keeps it as an array of [token, score] pairs.
	if len(parsed.Model.Vocab) > 0 {
		var object map[string]int
		if err := json.Unmarshal(parsed.Model.Vocab, &object); err == nil {
			for token, id := range object {
				parsed.RawVocab[token] = id
			}
		}
	}
	for token, id := range parsed.RawVocab {
		t.vocab[token] = int32(id)
		t.tokens[int32(id)] = token
		if runes := len([]rune(token)); runes > t.maxRune {
			t.maxRune = runes
		}
	}
	unigram, _ := parseUnigramVocab(parsed.Model.Vocab)
	// A Unigram vocabulary is a list of [token, score] pairs.
	for index, entry := range unigram {
		if len(entry) < 2 {
			continue
		}
		token, _ := entry[0].(string)
		score, _ := entry[1].(float64)
		if _, ok := t.vocab[token]; !ok {
			id := int32(index)
			t.vocab[token] = id
			t.tokens[id] = token
		}
		t.scores[token] = score
		if runes := len([]rune(token)); runes > t.maxRune {
			t.maxRune = runes
		}
	}
	rank := 0
	for _, merge := range parsed.Model.Merges {
		var left, right string
		switch value := merge.(type) {
		case string:
			parts := strings.SplitN(value, " ", 2)
			if len(parts) != 2 {
				continue
			}
			left, right = parts[0], parts[1]
		case []any:
			if len(value) != 2 {
				continue
			}
			left, _ = value[0].(string)
			right, _ = value[1].(string)
		default:
			continue
		}
		t.merges[[2]string{left, right}] = rank
		rank++
	}
	for _, added := range parsed.AddedTokens {
		t.vocab[added.Content] = added.ID
		t.tokens[added.ID] = added.Content
	}
	if parsed.Model.UnkID > 0 {
		t.unkID = int32(parsed.Model.UnkID)
	}
	if id, ok := t.vocab["<unk>"]; ok {
		t.unkID = id
	}
	if id, ok := t.vocab["<s>"]; ok {
		t.bosID = id
	}
	if id, ok := t.vocab["</s>"]; ok {
		t.eosID = id
	}
	if id, ok := t.vocab["<pad>"]; ok {
		t.padID = id
	}
	if t.maxRune > 32 {
		t.maxRune = 32
	}
	return t, nil
}

// parseUnigramVocab reads a vocabulary that is a list of [token, score] pairs.
func parseUnigramVocab(raw json.RawMessage) ([][]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var entries [][]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// Kind is the tokenizer family: BPE, Unigram or WordPiece.
func (t *Tokenizer) Kind() string { return t.kind }

// ID looks a token up, including the special tokens a graph needs.
func (t *Tokenizer) ID(token string) (int32, bool) {
	id, ok := t.vocab[token]
	return id, ok
}

// SpecialID is a named special token's id.
func (t *Tokenizer) SpecialID(token string) (int32, bool) { return t.ID(token) }

// Encode turns text into token ids.
func (t *Tokenizer) Encode(text string) []int32 {
	switch t.kind {
	case "WordPiece":
		return t.encodeWordPiece(text)
	case "Unigram":
		return t.encodeUnigram(text)
	default:
		return t.encodeBPE(text)
	}
}

// Decode turns token ids back into text.
func (t *Tokenizer) Decode(ids []int32) string {
	var out strings.Builder
	for _, id := range ids {
		token, ok := t.tokens[id]
		if !ok {
			continue
		}
		if strings.HasPrefix(token, "<|") && strings.HasSuffix(token, "|>") {
			continue
		}
		if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
			continue
		}
		if t.kind == "WordPiece" {
			if strings.HasPrefix(token, "##") {
				out.WriteString(token[2:])
			} else {
				if out.Len() > 0 {
					out.WriteString(" ")
				}
				out.WriteString(token)
			}
			continue
		}
		out.WriteString(t.decodeBytes(token))
	}
	return out.String()
}

// decodeBytes maps a byte-level token back to the bytes it stands for.
func (t *Tokenizer) decodeBytes(token string) string {
	bytes := make([]byte, 0, len(token))
	for _, r := range token {
		if b, ok := t.runeToByte[r]; ok {
			bytes = append(bytes, b)
			continue
		}
		bytes = append(bytes, []byte(string(r))...)
	}
	decoded := string(bytes)
	if strings.Contains(token, "▁") {
		decoded = strings.ReplaceAll(decoded, "▁", " ")
	}
	return decoded
}

// preTokenize is GPT-2's split: contractions, words, numbers and punctuation,
// keeping the leading space of a word.
func preTokenize(text string) []string {
	pieces := []string{}
	runes := []rune(text)
	for index := 0; index < len(runes); {
		if isSpace(runes[index]) {
			end := index
			for end < len(runes) && isSpace(runes[end]) {
				end++
			}
			if end >= len(runes) {
				// A trailing run of whitespace is one piece.
				pieces = append(pieces, string(runes[index:end]))
				break
			}
			if end-index > 1 {
				// One space stands alone; the rest is the next word's prefix.
				pieces = append(pieces, string(runes[index:index+1]))
				index++
				continue
			}
		}
		start := index
		space := false
		if isSpace(runes[index]) {
			space = true
			index++
		}
		if index >= len(runes) {
			pieces = append(pieces, string(runes[start:index]))
			break
		}
		// A contraction is one piece before anything else, as in GPT-2's pattern.
		if end, ok := contractionEnd(runes, index); ok {
			if space {
				pieces = append(pieces, string(runes[start:end]))
			} else {
				pieces = append(pieces, string(runes[index:end]))
			}
			index = end
			continue
		}
		r := runes[index]
		end := index
		switch {
		case unicode.IsLetter(r):
			for end < len(runes) && unicode.IsLetter(runes[end]) {
				end++
			}
		case unicode.IsDigit(r):
			for end < len(runes) && unicode.IsDigit(runes[end]) {
				end++
			}
		default:
			for end < len(runes) && !unicode.IsLetter(runes[end]) && !unicode.IsDigit(runes[end]) && !isSpace(runes[end]) {
				end++
			}
			if end == index {
				end = index + 1
			}
		}
		if space {
			pieces = append(pieces, string(runes[start:end]))
		} else {
			pieces = append(pieces, string(runes[index:end]))
		}
		index = end
	}
	return pieces
}

// contractions are GPT-2's "'s|'t|'re|'ve|'m|'ll|'d", longest first.
var contractions = []string{"'ll", "'re", "'ve", "'s", "'t", "'m", "'d"}

// contractionEnd matches a contraction at index, ignoring case.
func contractionEnd(runes []rune, index int) (int, bool) {
	if runes[index] != '\'' {
		return index, false
	}
	rest := strings.ToLower(string(runes[index:]))
	for _, contraction := range contractions {
		if strings.HasPrefix(rest, contraction) {
			return index + len([]rune(contraction)), true
		}
	}
	return index, false
}

// encodeBPE is the byte-level BPE encoding.
func (t *Tokenizer) encodeBPE(text string) []int32 {
	out := []int32{}
	for _, piece := range preTokenize(text) {
		if piece == "" {
			continue
		}
		// A special token in the text stays whole.
		if id, ok := t.vocab[piece]; ok && strings.HasPrefix(piece, "<|") {
			out = append(out, id)
			continue
		}
		symbols := []string{}
		for _, b := range []byte(piece) {
			symbols = append(symbols, string(t.byteToRune[b]))
		}
		symbols = t.mergeBPE(symbols)
		for _, symbol := range symbols {
			if id, ok := t.vocab[symbol]; ok {
				out = append(out, id)
				continue
			}
			if t.unkID >= 0 {
				out = append(out, t.unkID)
			}
		}
	}
	return out
}

// mergeBPE applies the merges with the lowest rank first, until none applies.
func (t *Tokenizer) mergeBPE(symbols []string) []string {
	for len(symbols) > 1 {
		bestRank := -1
		bestIndex := -1
		for index := 0; index+1 < len(symbols); index++ {
			rank, ok := t.merges[[2]string{symbols[index], symbols[index+1]}]
			if !ok {
				continue
			}
			if bestRank < 0 || rank < bestRank {
				bestRank = rank
				bestIndex = index
			}
		}
		if bestIndex < 0 {
			break
		}
		merged := symbols[bestIndex] + symbols[bestIndex+1]
		symbols = append(symbols[:bestIndex], append([]string{merged}, symbols[bestIndex+2:]...)...)
	}
	return symbols
}

// encodeWordPiece is the greedy longest-match encoding of WordPiece models.
func (t *Tokenizer) encodeWordPiece(text string) []int32 {
	out := []int32{}
	for _, word := range strings.Fields(strings.ToLower(text)) {
		runes := []rune(word)
		start := 0
		for start < len(runes) {
			end := len(runes)
			found := int32(-1)
			for end > start {
				candidate := string(runes[start:end])
				if start > 0 {
					candidate = "##" + candidate
				}
				if id, ok := t.vocab[candidate]; ok {
					found = id
					break
				}
				end--
			}
			if found < 0 {
				if t.unkID >= 0 {
					out = append(out, t.unkID)
				}
				break
			}
			out = append(out, found)
			start = end
		}
	}
	return out
}

// encodeUnigram is the Viterbi segmentation of a Unigram vocabulary, the
// algorithm refs/NeoRecall/internal/embeddings/tokenizer.go uses.
func (t *Tokenizer) encodeUnigram(text string) []int32 {
	prepared := strings.ReplaceAll(text, " ", "▁")
	runes := []rune(prepared)
	if len(runes) == 0 {
		return nil
	}
	type node struct {
		score float64
		prev  int
		id    int32
	}
	best := make([]node, len(runes)+1)
	for index := 1; index <= len(runes); index++ {
		best[index].score = -1e18
	}
	for index := 0; index < len(runes); index++ {
		if best[index].score <= -1e17 {
			continue
		}
		limit := index + t.maxRune
		if limit > len(runes) {
			limit = len(runes)
		}
		matched := false
		for end := index + 1; end <= limit; end++ {
			piece := string(runes[index:end])
			score, ok := t.scores[piece]
			if !ok {
				continue
			}
			matched = true
			if candidate := best[index].score + score; candidate > best[end].score {
				best[end] = node{score: candidate, prev: index, id: t.vocab[piece]}
			}
		}
		if !matched && best[index].score-10 > best[index+1].score {
			best[index+1] = node{score: best[index].score - 10, prev: index, id: t.unkID}
		}
	}
	ids := []int32{}
	for position := len(runes); position > 0; position = best[position].prev {
		ids = append(ids, best[position].id)
	}
	for left, right := 0, len(ids)-1; left < right; left, right = left+1, right-1 {
		ids[left], ids[right] = ids[right], ids[left]
	}
	return ids
}
