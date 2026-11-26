package processing

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// OpenAI's open privacy filter (openai/privacy-filter, Apache 2.0): a small token
// classifier that finds names, addresses, account numbers and other private
// values. It runs on this server, so no provider ever sees the raw text. An admin
// turns it on once; the weights download in the background, load on first use and
// unload again after a while without requests.

const (
	// PrivacyModelID is the model the filter downloads.
	PrivacyModelID = "openai/privacy-filter"
	// PrivacyModelDType is the quantisation of the ONNX weights.
	PrivacyModelDType = "q4"
	// PrivacyModelSetting is the global setting an admin turns on.
	PrivacyModelSetting = "privacy_model_enabled"
	// PrivacyModelMemoryMB is roughly what it takes while loaded; local models keep
	// this much free while it is on.
	PrivacyModelMemoryMB = 2048
	// PrivacyModelMaxChars is how much of a text the model reads.
	PrivacyModelMaxChars = 20000
	// PrivacyModelMinScore drops low-confidence spans.
	PrivacyModelMinScore = 0.5
	// PrivacyModelIdle is how long the model may sit unused before it is unloaded.
	PrivacyModelIdle = 15 * time.Minute
)

// CategoryOf maps the model's labels to the shield's categories.
var CategoryOf = map[string]string{
	"private_person":  "person",
	"private_address": "address",
	"private_email":   "email",
	"private_phone":   "phone",
	"account_number":  "id",
	"private_date":    "birthdate",
	"secret":          "secret",
}

// RawEntity is one span as the model returns it, before CATEGORY_OF, MIN_SCORE and
// tidy() are applied. It is what a fake pipeline produces in tests.
type RawEntity struct {
	EntityGroup string  `json:"entity_group"`
	Entity      string  `json:"entity"`
	Word        string  `json:"word"`
	Score       float64 `json:"score"`
}

// Pipeline is a loaded token-classification model.
type Pipeline interface {
	// Classify runs the model over one text.
	Classify(ctx context.Context, text string) ([]RawEntity, error)
	// Dispose frees the model's memory.
	Dispose() error
}

// Loader loads the pipeline. progress reports the download fraction (0..1).
// Tests swap the loader out with SetLoader.
type Loader func(ctx context.Context, progress func(fraction float64)) (Pipeline, error)

// PrivacyStatus is privacy_model.status(): {enabled, model, memoryMb, status,
// progress, error}.
type PrivacyStatus struct {
	Enabled  bool     `json:"enabled"`
	Model    string   `json:"model"`
	MemoryMB int      `json:"memoryMb"`
	Status   string   `json:"status"`
	Progress *float64 `json:"progress"`
	Error    *string  `json:"error"`
}

// PrivacyModel is the on-server privacy filter and its lifecycle.
type PrivacyModel struct {
	Cfg      func() config.Config
	Settings SettingsStore
	Log      *slog.Logger
	// Idle overrides PrivacyModelIdle (tests).
	Idle time.Duration
	// Load overrides how the pipeline is loaded (tests swap in a fake).
	Load Loader

	mu       sync.Mutex
	status   string
	progress *float64
	errText  *string
	pipe     Pipeline
	loading  chan struct{}
	idle     *time.Timer
}

// NewPrivacyModel builds the filter in its starting state ("off").
func NewPrivacyModel(cfg func() config.Config, settings SettingsStore, log *slog.Logger) *PrivacyModel {
	return &PrivacyModel{Cfg: cfg, Settings: settings, Log: log, status: "off"}
}

func (m *PrivacyModel) log() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

func (m *PrivacyModel) idleTimeout() time.Duration {
	if m.Idle > 0 {
		return m.Idle
	}
	return PrivacyModelIdle
}

// CacheDir is where the weights live: DATA_DIR/models.
func (m *PrivacyModel) CacheDir() string {
	if m.Cfg == nil {
		return filepath.Join("models")
	}
	cfg := m.Cfg()
	if cfg.Paths.Data == "" {
		return "models"
	}
	return filepath.Join(cfg.Paths.Data, "models")
}

// ModelDir is the directory of the downloaded model.
func (m *PrivacyModel) ModelDir() string {
	parts := append([]string{m.CacheDir()}, strings.Split(PrivacyModelID, "/")...)
	return filepath.Join(parts...)
}

// Enabled reports whether an admin turned the filter on for this server.
func (m *PrivacyModel) Enabled(ctx context.Context) bool {
	if m.Settings == nil {
		return false
	}
	return m.Settings.GetGlobalBool(ctx, PrivacyModelSetting, false)
}

// Status is the state the settings page shows.
func (m *PrivacyModel) Status(ctx context.Context) PrivacyStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return PrivacyStatus{
		Enabled:  m.Enabled(ctx),
		Model:    PrivacyModelID,
		MemoryMB: PrivacyModelMemoryMB,
		Status:   m.status,
		Progress: m.progress,
		Error:    m.errText,
	}
}

// SetEnabled turns the filter on (downloading right away, so the first request
// does not wait) or off (freeing the model).
func (m *PrivacyModel) SetEnabled(ctx context.Context, value bool) (PrivacyStatus, error) {
	if m.Settings != nil {
		if err := m.Settings.SetGlobal(ctx, PrivacyModelSetting, value); err != nil {
			return m.Status(ctx), err
		}
	}
	if value {
		m.mu.Lock()
		loaded := m.pipe != nil
		m.mu.Unlock()
		if !loaded {
			go func() {
				if _, err := m.load(context.WithoutCancel(ctx)); err != nil {
					m.log().Warn("Privacy filter could not load", "error", err)
				}
			}()
		}
	} else {
		m.unload("off")
	}
	return m.Status(ctx), nil
}

// Resume is the start-up step: finish a download that was cut short, otherwise
// load on first use.
func (m *PrivacyModel) Resume(ctx context.Context) {
	if !m.Enabled(ctx) {
		return
	}
	m.mu.Lock()
	m.status = "idle"
	m.mu.Unlock()
	data := filepath.Join(m.ModelDir(), "onnx", "model_"+PrivacyModelDType+".onnx_data")
	if _, err := os.Stat(data); err != nil {
		go func() {
			if _, err := m.load(context.WithoutCancel(ctx)); err != nil {
				m.log().Warn("Privacy filter could not load", "error", err)
			}
		}()
	}
}

// load downloads (the first time) and loads the model; one load at a time.
func (m *PrivacyModel) load(ctx context.Context) (Pipeline, error) {
	m.mu.Lock()
	if m.pipe != nil {
		pipe := m.pipe
		m.mu.Unlock()
		return pipe, nil
	}
	if m.loading != nil {
		wait := m.loading
		m.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		m.mu.Lock()
		pipe := m.pipe
		m.mu.Unlock()
		if pipe == nil {
			return nil, ErrPrivacyLoad
		}
		return pipe, nil
	}
	done := make(chan struct{})
	m.loading = done
	m.status = "loading"
	m.errText = nil
	m.progress = nil
	loader := m.Load
	m.mu.Unlock()

	if loader == nil {
		if err := os.MkdirAll(m.CacheDir(), 0o755); err != nil {
			m.finishLoad(nil, err, done)
			return nil, err
		}
		loader = m.defaultLoader()
	}
	pipe, err := loader(ctx, func(fraction float64) {
		m.mu.Lock()
		m.status = "downloading"
		value := fraction
		m.progress = &value
		m.mu.Unlock()
	})
	m.finishLoad(pipe, err, done)
	if err != nil {
		return nil, err
	}
	return pipe, nil
}

// ErrPrivacyLoad is returned when a concurrent load failed and nothing is loaded.
var ErrPrivacyLoad = errors.New("processing: the privacy filter is not loaded")

func (m *PrivacyModel) finishLoad(pipe Pipeline, err error, done chan struct{}) {
	m.mu.Lock()
	if err != nil {
		m.status = "error"
		message := err.Error()
		m.errText = &message
		m.progress = nil
	} else {
		m.pipe = pipe
		m.status = "ready"
		m.progress = nil
	}
	m.loading = nil
	close(done)
	m.mu.Unlock()
	if err != nil {
		m.log().Warn("Privacy filter could not load", "error", err)
		return
	}
	m.log().Info("Privacy filter loaded")
	m.touch()
}

// unload frees the model and leaves the filter in the given state.
func (m *PrivacyModel) unload(status string) {
	m.mu.Lock()
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	pipe := m.pipe
	m.pipe = nil
	m.status = status
	m.progress = nil
	m.mu.Unlock()
	if pipe != nil {
		if err := pipe.Dispose(); err != nil {
			m.log().Debug("Privacy filter dispose", "error", err)
		}
	}
}

// touch restarts the idle timer that frees the model.
func (m *PrivacyModel) touch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idle != nil {
		m.idle.Stop()
	}
	m.idle = time.AfterFunc(m.idleTimeout(), func() {
		m.mu.Lock()
		loaded := m.pipe
		m.pipe = nil
		enabled := m.Settings != nil && m.Settings.GetGlobalBool(context.Background(), PrivacyModelSetting, false)
		if enabled {
			m.status = "idle"
		} else {
			m.status = "off"
		}
		m.idle = nil
		m.mu.Unlock()
		if loaded != nil {
			if err := loaded.Dispose(); err != nil {
				m.log().Debug("Privacy filter dispose", "error", err)
			}
		}
		m.log().Info("Privacy filter unloaded after idling")
	})
}

// Detect is the private values in text, as {text, category} with exact
// substrings; nil when the filter is off.
func (m *PrivacyModel) Detect(ctx context.Context, text string) ([]Entity, error) {
	if !m.Enabled(ctx) {
		return nil, nil
	}
	pipe, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	m.touch()
	input := clipRunes(text, PrivacyModelMaxChars)
	raw, err := pipe.Classify(ctx, input)
	if err != nil {
		return nil, err
	}
	items := []Entity{}
	for _, span := range raw {
		label := span.EntityGroup
		if label == "" {
			label = span.Entity
		}
		label = entityPrefixPattern.ReplaceAllString(label, "")
		category, ok := CategoryOf[label]
		if !ok || span.Score < PrivacyModelMinScore {
			continue
		}
		value := tidy(input, strings.TrimSpace(span.Word), category)
		if value != "" && len([]rune(value)) >= 2 {
			items = append(items, Entity{Text: value, Category: category})
		}
	}
	return items, nil
}

var entityPrefixPattern = regexp.MustCompile(`^[BIES]-`)

// isPrivacyWord is [\p{L}\p{N}_] for one rune.
func isPrivacyWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

var (
	// privacyDigits requires a digit, so "last month" is not a date.
	privacyDigits = regexp.MustCompile(`\d`)
	// privacyIPv4 is an IP address, which the IP rule names properly.
	privacyIPv4 = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)
)

// Tidy widens a model span to whole words and applies the date rules.
//
// Spans follow tokens, which can stop inside a word ("…, IB" of "IBAN", "xK2" of
// "9xK2"): widen them to whole words. A date must hold a digit, so "last month"
// is not a date, and words without digits around one are dropped.
//
// Two lookarounds would be natural here, which Go's RE2 engine does not have:
//
//	text.replace(/^(?:[^\d\s]+\s+)+(?=\S*\d)/u, '')
//	text.replace(/(?<=\d\S*)(?:\s+[^\d\s]+)+$/u, '')
//
// The lookahead at the end of the first pattern is a condition on where the
// leading words may stop; requiring that a digit is still ahead is the same as
// demanding that the remaining text holds a digit, so the condition is checked on
// the remainder. The trailing lookbehind says "the last whitespace-separated run
// follows a piece of text that starts with a digit"; the first rune of that piece
// is what the lookbehind reaches back to, so the rule is applied from the end of
// the last digit run instead. Both cases are covered by the tests.
func tidy(input, value, category string) string {
	start := strings.Index(input, value)
	if start < 0 {
		return ""
	}
	end := start + len(value)
	for start > 0 {
		previous, previousSize := utf8.DecodeLastRuneInString(input[:start])
		current, _ := utf8.DecodeRuneInString(input[start:])
		if !isPrivacyWord(previous) || !isPrivacyWord(current) {
			break
		}
		start -= previousSize
	}
	for end < len(input) {
		current, currentSize := utf8.DecodeRuneInString(input[end:])
		previous, _ := utf8.DecodeLastRuneInString(input[:end])
		if !isPrivacyWord(current) || !isPrivacyWord(previous) {
			break
		}
		end += currentSize
	}
	text := input[start:end]
	if category == "birthdate" {
		if !privacyDigits.MatchString(text) || privacyIPv4.MatchString(text) {
			return ""
		}
		text = trimLeadingWords(text)
		text = trimTrailingWords(text)
	}
	return text
}

// trimLeadingWords is /^(?:[^\d\s]+\s+)+(?=\S*\d)/: drop the leading words and
// their spacing, but only when the token that follows them holds a digit. The
// lookahead can only look into that one token (\S* stops at a space), and a
// stripped word can never hold a digit itself, so backtracking cannot help: the
// strip either happens for every leading word or for none.
func trimLeadingWords(text string) string {
	runes := []rune(text)
	position := 0
	for {
		end := position
		for end < len(runes) && !isDigitRune(runes[end]) && !isJSSpace(runes[end]) {
			end++
		}
		if end == position {
			break
		}
		spacing := end
		for spacing < len(runes) && isJSSpace(runes[spacing]) {
			spacing++
		}
		if spacing == end {
			break
		}
		position = spacing
	}
	if position == 0 || !tokenHasDigit(runes[position:]) {
		return text
	}
	return string(runes[position:])
}

// trimTrailingWords is /(?<=\d\S*)(?:\s+[^\d\s]+)+$/: drop the trailing words and
// their spacing, but only when the token in front of them holds a digit.
func trimTrailingWords(text string) string {
	runes := []rune(text)
	position := len(runes)
	for {
		start := position
		for start > 0 && !isDigitRune(runes[start-1]) && !isJSSpace(runes[start-1]) {
			start--
		}
		if start == position {
			break
		}
		spacing := start
		for spacing > 0 && isJSSpace(runes[spacing-1]) {
			spacing--
		}
		if spacing == start {
			break
		}
		position = spacing
	}
	if position == len(runes) || !tokenHasDigitBefore(runes[:position]) {
		return text
	}
	return string(runes[:position])
}

// tokenHasDigit reports whether the first token of runes holds a digit.
func tokenHasDigit(runes []rune) bool {
	for _, r := range runes {
		if isJSSpace(r) {
			return false
		}
		if isDigitRune(r) {
			return true
		}
	}
	return false
}

// tokenHasDigitBefore reports whether the last token of runes holds a digit.
func tokenHasDigitBefore(runes []rune) bool {
	for index := len(runes) - 1; index >= 0; index-- {
		if isJSSpace(runes[index]) {
			return false
		}
		if isDigitRune(runes[index]) {
			return true
		}
	}
	return false
}

func isDigitRune(r rune) bool { return r >= '0' && r <= '9' }

// isJSSpace reports whether r is one of the runes JavaScript's \s matches.
func isJSSpace(r rune) bool {
	for _, set := range jsSpace {
		if r >= set.lo && r <= set.hi {
			return true
		}
	}
	return false
}

// clipRunes truncates text to n runes.
func clipRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

// SetLoader replaces how the model is loaded and forgets what is loaded; the
// tests use it with a fake pipeline.
func (m *PrivacyModel) SetLoader(loader Loader) {
	m.mu.Lock()
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	m.pipe = nil
	m.loading = nil
	m.status = "off"
	m.progress = nil
	m.errText = nil
	m.Load = loader
	m.mu.Unlock()
}

// defaultLoader is the real ONNX loader (see privacy_onnx.go).
func (m *PrivacyModel) defaultLoader() Loader { return m.loadONNX }
