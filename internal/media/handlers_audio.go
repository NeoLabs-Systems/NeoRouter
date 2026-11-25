package media

import (
	"context"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// Transcription is POST /v1/audio/transcriptions (and /v1/audio/translations,
// which asks for English whatever the source language).
func (s *Service) Transcription(c *call) error {
	input, err := parseTranscriptionInput(c.fields)
	if err != nil {
		return err
	}
	if c.file == nil || len(c.file.data) == 0 {
		return httpserver.New(http.StatusBadRequest, "INVALID_REQUEST", "file: an audio file is required.")
	}
	if len(c.file.data) > maxAudioBytes {
		return httpserver.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Audio files can be at most 25 MB.")
	}
	minutes := estimateAudioMinutes(len(c.file.data))
	tokens := tokensForAudio(minutes)
	// Subtitles need segment timestamps: prefer models that return them, as long
	// as one is in the catalog (otherwise the whole text is one cue).
	needsTimestamps := (input.ResponseFormat == "srt" || input.ResponseFormat == "vtt" || input.ResponseFormat == "verbose_json") &&
		routing.IsAutoModel(input.Model)
	timestampsWanted := false
	if needsTimestamps {
		entries, err := s.models(c.r.Context(), c.key.UserID, "transcription")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.Enabled {
				continue
			}
			if adapter := s.adapterFor(entry.AccountID); adapter != nil && adapter.TranscribeTimestamps() {
				timestampsWanted = true
				break
			}
		}
	}
	// Text that must fit a window: a prompt, not the audio (Whisper lists 448
	// tokens for one, whatever the file's length).
	contextTokens := 0
	if input.Prompt != nil {
		contextTokens = estimateTokens([]string{*input.Prompt})
	}
	language := ""
	if input.Language != nil {
		language = *input.Language
	}
	prompt := ""
	if input.Prompt != nil {
		prompt = *input.Prompt
	}
	translate := c.translate
	file := c.file
	options := &RunOptions{
		Capability: "transcription", Method: providers.CapTranscribe, RequestedModel: input.Model,
		FreeOnly: c.freeOnly, InputTokens: tokens, ContextTokens: &contextTokens, Units: minutes,
		EstimateCost: func(entry catalog.Entry) *float64 {
			return priced(unitCost(entry, "minute", minutes), tokenCost(entry, float64(tokens)))
		},
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			transcriber, ok := attempt.Adapter.(providers.Transcriber)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "This provider cannot transcribe.", providers.ErrorOptions{})
			}
			result, err := transcriber.Transcribe(ctx, providers.TranscribeRequest{
				File: file.data, Filename: file.filename, Mime: file.mime,
				Language: language, Prompt: prompt, Translate: translate,
			}, providers.Call{ModelID: attempt.Entry.ModelID, Timeout: attempt.Timeout})
			if err != nil {
				return nil, err
			}
			outcome := &Outcome{Value: result, Headers: result.Headers}
			if result.Duration != nil {
				// What the log bills is the duration the provider reported.
				used := *result.Duration / 60
				outcome.Units = &used
			}
			return outcome, nil
		},
		CostOf: func(entry catalog.Entry, outcome *Outcome) *float64 {
			used := minutes
			if result, ok := outcome.Value.(*providers.Transcript); ok && result.Duration != nil {
				used = *result.Duration / 60
			}
			if cost := priced(unitCost(entry, "minute", used), tokenCost(entry, float64(tokensForAudio(used)))); cost != nil {
				return cost
			}
			return zeroCost()
		},
		Respond: func(w http.ResponseWriter, outcome *Outcome, entry catalog.Entry) error {
			return respondTranscription(w, c, input, outcome, entry, translate)
		},
	}
	if translate || timestampsWanted {
		options.Filter = func(entry catalog.Entry, adapter providers.Adapter) bool {
			// Translation only goes to models that can translate (not Deepgram or
			// ElevenLabs), and subtitles to models that return timestamps.
			if translate && !providers.CanTranslate(adapter, entry.ModelID) {
				return false
			}
			return !timestampsWanted || adapter.TranscribeTimestamps()
		}
	}
	_, err = s.run(c, options)
	return err
}

// verboseTranscript is one timed piece of a transcript as the API lists it.
type verboseTranscript struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// verboseJSON is the verbose_json transcription answer.
type verboseJSON struct {
	Task     string              `json:"task"`
	Language *string             `json:"language"`
	Duration *float64            `json:"duration"`
	Text     string              `json:"text"`
	Segments []verboseTranscript `json:"segments"`
}

// playgroundTranscript is the playground's transcription answer.
type playgroundTranscript struct {
	Text     string   `json:"text"`
	Language *string  `json:"language"`
	Duration *float64 `json:"duration"`
	Model    string   `json:"model"`
}

// respondTranscription writes the transcript in the shape the caller asked for.
func respondTranscription(w http.ResponseWriter, c *call, input transcriptionInput, outcome *Outcome, entry catalog.Entry, translate bool) error {
	result, _ := outcome.Value.(*providers.Transcript)
	if result == nil {
		result = &providers.Transcript{}
	}
	if c.includeModel {
		httpx.WriteJSON(w, http.StatusOK, playgroundTranscript{
			Text: result.Text, Language: optionalText(result.Language), Duration: result.Duration, Model: entry.ID,
		})
		return nil
	}
	switch input.ResponseFormat {
	case "text":
		return writeText(w, "text/plain; charset=utf-8", result.Text)
	case "srt", "vtt":
		if input.ResponseFormat == "srt" {
			return writeText(w, "application/x-subrip; charset=utf-8", subtitles(result.Text, result.Duration, result.Segments, input.ResponseFormat))
		}
		return writeText(w, "text/vtt; charset=utf-8", subtitles(result.Text, result.Duration, result.Segments, input.ResponseFormat))
	case "verbose_json":
		segments := make([]verboseTranscript, 0, len(result.Segments))
		for index, segment := range result.Segments {
			segments = append(segments, verboseTranscript{ID: index, Start: segment.Start, End: segment.End, Text: segment.Text})
		}
		task := "transcribe"
		if translate {
			task = "translate"
		}
		httpx.WriteJSON(w, http.StatusOK, verboseJSON{
			Task: task, Language: optionalText(result.Language), Duration: result.Duration,
			Text: result.Text, Segments: segments,
		})
		return nil
	}
	httpx.WriteJSON(w, http.StatusOK, textAnswer{Text: result.Text})
	return nil
}

// textAnswer is the plain transcription answer.
type textAnswer struct {
	Text string `json:"text"`
}

// optionalText is a string that is null when empty, as `value || null` renders.
func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// writeText answers with a plain-text document, the way res.type(...).send(text)
// does: the content type it names, the UTF-8 charset Express adds and the body's
// length.
func writeText(w http.ResponseWriter, contentType, body string) error {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, err := w.Write([]byte(body))
	return err
}
