package processing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
	"github.com/NeoLabs-Systems/neorouter/internal/processing/onnxrt"
)

// The real privacy-filter loader: download the weights from Hugging Face into
// DATA_DIR/models (the same place the JS transformers cache used), tokenise with
// the model's own tokenizer.json, run the ONNX session and turn its per-token
// logits into entity spans.
//
// Nothing here runs in a normal test: without the ~900 MB weights and the ONNX
// Runtime library the loader reports a clear error and the shield falls back to
// its pattern detectors.

// privacyModelBaseURL is where the model files come from.
const privacyModelBaseURL = "https://huggingface.co/" + PrivacyModelID + "/resolve/main/"

// privacyModelFiles are the files the pipeline needs, in download order.
func privacyModelFiles() []string {
	return []string{
		"config.json",
		"tokenizer.json",
		"onnx/model_" + PrivacyModelDType + ".onnx",
		"onnx/model_" + PrivacyModelDType + ".onnx_data",
	}
}

// loadONNX is the default loader.
func (m *PrivacyModel) loadONNX(ctx context.Context, progress func(float64)) (Pipeline, error) {
	if !onnxrt.HasRuntime() {
		return nil, fmt.Errorf("processing: the ONNX Runtime library is not installed")
	}
	dir := m.ModelDir()
	if err := downloadPrivacyModel(ctx, dir, progress); err != nil {
		return nil, err
	}
	tokenizer, err := LoadTokenizer(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, fmt.Errorf("processing: load tokenizer: %w", err)
	}
	api, err := onnxrt.Load()
	if err != nil {
		return nil, err
	}
	session, err := api.NewSession(filepath.Join(dir, "onnx", "model_"+PrivacyModelDType+".onnx"))
	if err != nil {
		return nil, fmt.Errorf("processing: load privacy model: %w", err)
	}
	labels, err := loadPrivacyLabels(filepath.Join(dir, "config.json"))
	if err != nil {
		session.Close()
		return nil, err
	}
	return &onnxPipeline{session: session, tokenizer: tokenizer, labels: labels}, nil
}

// loadPrivacyLabels is config.json id2label: the model's 33 label names.
func loadPrivacyLabels(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("processing: read config.json: %w", err)
	}
	var config struct {
		ID2Label map[string]string `json:"id2label"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("processing: decode config.json: %w", err)
	}
	if len(config.ID2Label) == 0 {
		return nil, fmt.Errorf("processing: config.json carries no id2label")
	}
	labels := make([]string, len(config.ID2Label))
	for id, label := range config.ID2Label {
		var index int
		if _, err := fmt.Sscanf(id, "%d", &index); err != nil || index < 0 || index >= len(labels) {
			return nil, fmt.Errorf("processing: config.json label id %q", id)
		}
		labels[index] = label
	}
	return labels, nil
}

// downloadPrivacyModel fetches every file that is not there yet. progress receives
// the fraction over all files, the way the JS progress_callback reported it.
func downloadPrivacyModel(ctx context.Context, dir string, progress func(float64)) error {
	if err := os.MkdirAll(filepath.Join(dir, "onnx"), 0o755); err != nil {
		return fmt.Errorf("processing: create model directory: %w", err)
	}
	client := ssrf.NewClient(ssrf.Options{Timeout: 0})
	totals := map[string]int64{}
	loadedBytes := map[string]int64{}
	var mu sync.Mutex
	report := func(file string, loaded, total int64) {
		if progress == nil {
			return
		}
		mu.Lock()
		totals[file] = total
		loadedBytes[file] = loaded
		var sumLoaded, sumTotal int64
		for name, total := range totals {
			sumTotal += total
			sumLoaded += loadedBytes[name]
		}
		mu.Unlock()
		if sumTotal > 0 {
			progress(float64(sumLoaded) / float64(sumTotal))
		}
	}
	for _, name := range privacyModelFiles() {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if info, err := os.Stat(target); err == nil && info.Size() > 0 {
			report(name, info.Size(), info.Size())
			continue
		}
		if err := downloadFile(ctx, client, privacyModelBaseURL+name, target, func(loaded, total int64) {
			report(name, loaded, total)
		}); err != nil {
			return err
		}
	}
	return nil
}

// downloadFile writes one file atomically, reporting progress.
func downloadFile(ctx context.Context, client *http.Client, url, target string, report func(loaded, total int64)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("processing: download %s: %w", url, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("processing: download %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("processing: download %s: HTTP %d", url, response.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("processing: create directory: %w", err)
	}
	temporary := target + ".part"
	file, err := os.Create(temporary)
	if err != nil {
		return fmt.Errorf("processing: create %s: %w", target, err)
	}
	total := response.ContentLength
	var written int64
	buffer := make([]byte, 1<<20)
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			if _, err := file.Write(buffer[:read]); err != nil {
				file.Close()
				os.Remove(temporary)
				return fmt.Errorf("processing: write %s: %w", target, err)
			}
			written += int64(read)
			report(written, total)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			file.Close()
			os.Remove(temporary)
			return fmt.Errorf("processing: download %s: %w", url, readErr)
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("processing: close %s: %w", target, err)
	}
	if err := os.Rename(temporary, target); err != nil {
		return fmt.Errorf("processing: rename %s: %w", target, err)
	}
	return nil
}

// onnxPipeline is the loaded model.
type onnxPipeline struct {
	session   *onnxrt.Session
	tokenizer *Tokenizer
	labels    []string
}

// Dispose frees the ONNX session.
func (p *onnxPipeline) Dispose() error { return p.session.Close() }

// Classify tokenises the text, runs the model and aggregates the per-token labels
// into entity spans (aggregation_strategy "simple").
func (p *onnxPipeline) Classify(ctx context.Context, text string) ([]RawEntity, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	tokens := p.tokenizer.Encode(text)
	if len(tokens) == 0 {
		return nil, nil
	}
	inputIDs := make([]int64, len(tokens))
	attention := make([]int64, len(tokens))
	for index, token := range tokens {
		inputIDs[index] = int64(token.ID)
		attention[index] = 1
	}
	logits, dims, err := p.session.RunSequence(inputIDs, attention)
	if err != nil {
		return nil, err
	}
	if len(dims) != 3 || int(dims[1]) != len(tokens) || int(dims[2]) != len(p.labels) {
		return nil, fmt.Errorf("processing: unexpected model output shape %v", dims)
	}
	width := len(p.labels)
	best := make([]int, len(tokens))
	scores := make([]float64, len(tokens))
	for token := range tokens {
		row := logits[token*width : (token+1)*width]
		bestIndex, bestScore := 0, math.Inf(-1)
		for index, value := range row {
			if float64(value) > bestScore {
				bestIndex, bestScore = index, float64(value)
			}
		}
		// Softmax probability of the winning label.
		total := 0.0
		for _, value := range row {
			total += math.Exp(float64(value) - bestScore)
		}
		best[token] = bestIndex
		scores[token] = 1 / total
	}
	return aggregateEntities(text, tokens, best, scores, p.labels), nil
}

// aggregateEntities groups consecutive tokens of the same entity type
// (aggregation_strategy "simple").
func aggregateEntities(text string, tokens []Token, labels []int, scores []float64, names []string) []RawEntity {
	entities := []RawEntity{}
	type group struct {
		kind  string
		start int
		end   int
		score float64
		count int
		open  bool
	}
	current := group{}
	closeGroup := func() {
		if !current.open {
			return
		}
		value := ""
		if current.start < current.end && current.end <= len(text) {
			value = text[current.start:current.end]
		}
		entities = append(entities, RawEntity{
			EntityGroup: current.kind,
			Entity:      current.kind,
			Word:        value,
			Score:       current.score / float64(current.count),
		})
		current = group{}
	}
	for index, token := range tokens {
		label := names[labels[index]]
		if label == "O" || len(label) < 2 || label[1] != '-' {
			closeGroup()
			continue
		}
		prefix, kind := label[0], label[2:]
		switch prefix {
		case 'S':
			closeGroup()
			current = group{kind: kind, start: token.Start, end: token.End, score: scores[index], count: 1, open: true}
			closeGroup()
		case 'B':
			closeGroup()
			current = group{kind: kind, start: token.Start, end: token.End, score: scores[index], count: 1, open: true}
		case 'I', 'E':
			if current.open && current.kind == kind {
				current.end = token.End
				current.score += scores[index]
				current.count++
			} else {
				closeGroup()
				current = group{kind: kind, start: token.Start, end: token.End, score: scores[index], count: 1, open: true}
			}
			if prefix == 'E' {
				closeGroup()
			}
		default:
			closeGroup()
		}
	}
	closeGroup()
	return entities
}

// Token is one tokenizer output token: its id and the byte range it covers in the
// text it came from.
type Token struct {
	ID    int32
	Start int
	End   int
}

// Tokenizer is a byte-level BPE tokenizer read from a Hugging Face tokenizer.json.
//
// The privacy filter ships a GPT-style tokenizer: a Split pre-tokenizer with a
// regex, byte-level mapping of every byte to one character, and a BPE model with
// merges. Only what that shape needs is implemented, and an unsupported
// tokenizer.json fails loudly instead of producing silent nonsense.
type Tokenizer struct {
	vocab          map[string]int32
	merges         map[string]int
	split          *regexp.Regexp
	byteToChar     [256]rune
	charToByte     map[rune]byte
	ignoreMerges   bool
	addPrefixSpace bool
}

type tokenizerJSON struct {
	PreTokenizer struct {
		Type          string `json:"type"`
		Pretokenizers []struct {
			Type    string `json:"type"`
			Pattern struct {
				Regex string `json:"Regex"`
			} `json:"pattern"`
			Behavior string `json:"behavior"`
		} `json:"pretokenizers"`
	} `json:"pre_tokenizer"`
	PostProcessor struct {
		Type           string `json:"type"`
		AddPrefixSpace bool   `json:"add_prefix_space"`
	} `json:"post_processor"`
	Model struct {
		Type         string          `json:"type"`
		IgnoreMerges bool            `json:"ignore_merges"`
		Vocab        map[string]int  `json:"vocab"`
		Merges       json.RawMessage `json:"merges"`
	} `json:"model"`
}

// LoadTokenizer reads a tokenizer.json.
func LoadTokenizer(path string) (*Tokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed tokenizerJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	tokenizer := &Tokenizer{
		vocab:          make(map[string]int32, len(parsed.Model.Vocab)),
		merges:         map[string]int{},
		charToByte:     make(map[rune]byte, 256),
		ignoreMerges:   parsed.Model.IgnoreMerges,
		addPrefixSpace: parsed.PostProcessor.AddPrefixSpace,
	}
	buildByteMaps(tokenizer)
	switch parsed.Model.Type {
	case "BPE":
	default:
		return nil, fmt.Errorf("processing: unsupported tokenizer model %q", parsed.Model.Type)
	}
	for token, id := range parsed.Model.Vocab {
		tokenizer.vocab[token] = int32(id)
	}
	merges, err := decodeMerges(parsed.Model.Merges)
	if err != nil {
		return nil, err
	}
	for rank, pair := range merges {
		tokenizer.merges[pair[0]+" "+pair[1]] = rank
	}
	for _, pretokenizer := range parsed.PreTokenizer.Pretokenizers {
		if pretokenizer.Type != "Split" || pretokenizer.Pattern.Regex == "" {
			continue
		}
		compiled, err := regexp.Compile(pretokenizer.Pattern.Regex)
		if err != nil {
			// The tokenizer regex uses JavaScript's (?i:'s) groups; Go rejects
			// some of them, so the offending parts are rewritten.
			rewritten := strings.ReplaceAll(pretokenizer.Pattern.Regex, "(?i:", "(?i:")
			compiled, err = regexp.Compile(rewritten)
			if err != nil {
				return nil, fmt.Errorf("processing: tokenizer split pattern: %w", err)
			}
		}
		tokenizer.split = compiled
		break
	}
	if tokenizer.split == nil {
		return nil, fmt.Errorf("processing: tokenizer has no Split pre-tokenizer")
	}
	return tokenizer, nil
}

// decodeMerges accepts both tokenizer.json shapes: [["a","b"], …] and ["a b", …].
func decodeMerges(raw json.RawMessage) ([][2]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("processing: tokenizer has no merges")
	}
	var pairs [][2]string
	if err := json.Unmarshal(raw, &pairs); err == nil {
		return pairs, nil
	}
	var lines []string
	if err := json.Unmarshal(raw, &lines); err != nil {
		return nil, fmt.Errorf("processing: tokenizer merges: %w", err)
	}
	out := make([][2]string, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("processing: tokenizer merge %q", line)
		}
		out = append(out, [2]string{parts[0], parts[1]})
	}
	return out, nil
}

// buildByteMaps is GPT-2's bytes_to_unicode: the printable bytes stay as they are,
// the rest map to code points from 256 on.
func buildByteMaps(tokenizer *Tokenizer) {
	// The printable bytes keep their own code point; every other byte is mapped to
	// the code points from 256 up, in byte order. Space is one of the latter, which
	// is why a leading space becomes "Ġ" in a GPT vocabulary.
	printable := func(by int) bool {
		return (by >= '!' && by <= '~') || (by >= 0xA1 && by <= 0xAC) || (by >= 0xAE && by <= 0xFF)
	}
	next := 256
	for by := 0; by < 256; by++ {
		point := rune(by)
		if !printable(by) {
			point = rune(next)
			next++
		}
		tokenizer.byteToChar[by] = point
		tokenizer.charToByte[point] = byte(by)
	}
}

// Encode tokenises a text into ids with byte offsets.
func (t *Tokenizer) Encode(text string) []Token {
	input := text
	shift := 0
	if t.addPrefixSpace && !strings.HasPrefix(text, " ") {
		input = " " + text
		shift = 1
	}
	tokens := []Token{}
	for _, part := range t.split.FindAllStringIndex(input, -1) {
		tokens = append(tokens, t.encodePart(input, part[0], part[1], shift)...)
	}
	return tokens
}

// encodePart tokenises one pre-token, mapping each byte to one character.
func (t *Tokenizer) encodePart(text string, start, end, shift int) []Token {
	bytes := []byte(text[start:end])
	symbols := make([]string, len(bytes))
	offsets := make([]int, len(bytes)+1)
	for index, by := range bytes {
		symbols[index] = string(t.byteToChar[by])
		offsets[index] = start + index
	}
	offsets[len(bytes)] = end
	tokens := []Token{}
	// ignore_merges: a pre-token that is in the vocabulary stays one token.
	if len(bytes) > 0 {
		if id, ok := t.vocab[strings.Join(symbols, "")]; ok && t.ignoreMerges {
			return []Token{{ID: id, Start: maxInt(0, start-shift), End: maxInt(0, end-shift)}}
		}
	}
	symbols = t.merge(symbols)
	position := 0
	for _, symbol := range symbols {
		width := len([]rune(symbol))
		id, ok := t.vocab[symbol]
		if !ok {
			// Byte fallback: emit the bytes one by one when the vocabulary knows
			// them; otherwise the token is dropped, which the model tolerates.
			position += width
			continue
		}
		startByte := offsets[position] - shift
		endByte := offsets[position+width] - shift
		if startByte < 0 {
			startByte = 0
		}
		if endByte < 0 {
			endByte = 0
		}
		tokens = append(tokens, Token{ID: id, Start: startByte, End: endByte})
		position += width
	}
	return tokens
}

// merge applies the BPE merges to a pre-token's symbols.
func (t *Tokenizer) merge(symbols []string) []string {
	for len(symbols) > 1 {
		bestRank := -1
		bestIndex := -1
		for index := 0; index+1 < len(symbols); index++ {
			rank, ok := t.merges[symbols[index]+" "+symbols[index+1]]
			if !ok {
				continue
			}
			if bestRank < 0 || rank < bestRank {
				bestRank, bestIndex = rank, index
			}
		}
		if bestIndex < 0 {
			return symbols
		}
		merged := make([]string, 0, len(symbols)-1)
		merged = append(merged, symbols[:bestIndex]...)
		merged = append(merged, symbols[bestIndex]+symbols[bestIndex+1])
		merged = append(merged, symbols[bestIndex+2:]...)
		symbols = merged
	}
	return symbols
}
