package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The transcription half of the OpenAI-style media surface: the multipart form
// most providers take and the base64 JSON OpenRouter takes.

// Transcribe uploads audio for a transcript. A provider that takes the audio as
// base64 JSON (OpenRouter) says so with quirks.transcriptionJson; the rest take
// a multipart form, and translation is the same form at the translations path.
func (m *MediaMethods) Transcribe(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	if providers.IsTrue(m.def.Quirks.TranscriptionJSON) {
		return m.transcribeJSON(ctx, req, call)
	}
	path, ok := MediaPath(m.def, providers.CapTranscribe, call.ModelID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapTranscribe)
	}
	filename := req.Filename
	if filename == "" {
		filename = "audio"
	}
	mime := req.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	form := newMediaForm()
	form.file("file", filename, mime, req.File)
	form.field("model", call.ModelID)
	form.field("response_format", "verbose_json")
	if req.Language != "" && !req.Translate {
		form.field("language", req.Language)
	}
	if req.Prompt != "" {
		form.field("prompt", req.Prompt)
	}
	if req.Translate {
		path = strings.Replace(path, "transcriptions", "translations", 1)
	}
	body, contentType := form.close()
	response, err := m.do(ctx, path, providers.Request{
		Method: http.MethodPost, Body: bytes.NewReader(body), ContentType: contentType,
	}, call)
	if err != nil {
		return nil, err
	}
	result := response.Data
	if text, isText := result.(string); isText {
		result = map[string]any{"text": text}
	} else if result == nil {
		result = map[string]any{}
	}
	transcript, ok := mediaMember(result, "text").(string)
	if !ok {
		return nil, mediaInvalidOutput("The provider returned no transcript.")
	}
	return &providers.Transcript{
		Text:     transcript,
		Language: mediaFirstString(mediaString(mediaMember(result, "language")), req.Language),
		Duration: mediaNumber(mediaMember(result, "duration")),
		Segments: mediaSegments(mediaMember(result, "segments")),
		Headers:  response.Headers,
	}, nil
}

// transcribeJSON is the base64-JSON transcription OpenRouter takes.
func (m *MediaMethods) transcribeJSON(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	path, ok := MediaPath(m.def, providers.CapTranscribe, call.ModelID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapTranscribe)
	}
	body := map[string]any{
		"model": call.ModelID,
		"input_audio": map[string]any{
			"data":   base64.StdEncoding.EncodeToString(req.File),
			"format": mediaAudioFormat(req.Filename, req.Mime),
		},
	}
	if req.Language != "" {
		body["language"] = req.Language
	}
	if req.Prompt != "" {
		body["prompt"] = req.Prompt
	}
	response, err := m.doPost(ctx, path, body, call)
	if err != nil {
		return nil, err
	}
	transcript, ok := mediaMember(response.Data, "text").(string)
	if !ok {
		return nil, mediaInvalidOutput("The provider returned no transcript.")
	}
	return &providers.Transcript{
		Text:     transcript,
		Language: mediaFirstString(mediaString(mediaMember(response.Data, "language")), req.Language),
		Duration: mediaNumber(mediaMember(response.Data, "duration")),
		Segments: mediaSegments(mediaMember(response.Data, "segments")),
		Headers:  response.Headers,
	}, nil
}

// mediaFirstString is the first non-empty string of the candidates.
func mediaFirstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// mediaSegments reads the timed segments of a transcript.
func mediaSegments(value any) []providers.Segment {
	items := mediaList(value)
	if len(items) == 0 {
		return nil
	}
	segments := make([]providers.Segment, 0, len(items))
	for _, item := range items {
		segment := providers.Segment{Text: mediaString(mediaMember(item, "text"))}
		if start := mediaNumber(mediaMember(item, "start")); start != nil {
			segment.Start = *start
		}
		if end := mediaNumber(mediaMember(item, "end")); end != nil {
			segment.End = *end
		}
		segments = append(segments, segment)
	}
	return segments
}

// mediaAudioExtension matches a filename's extension.
var mediaAudioExtension = regexp.MustCompile(`\.(\w+)$`)

// mediaAudioFormat is the format OpenAI's JSON transcription expects: the
// filename's extension, else the MIME type's subtype (mpeg is mp3, x-wav is
// wav).
func mediaAudioFormat(filename, mime string) string {
	if match := mediaAudioExtension.FindStringSubmatch(filename); match != nil {
		extension := strings.ToLower(match[1])
		if extension == "mpeg" {
			return "mp3"
		}
		return extension
	}
	_, subtype, found := strings.Cut(mime, "/")
	if !found || subtype == "" {
		return "wav"
	}
	subtype = strings.TrimSpace(subtype)
	if index := strings.IndexAny(subtype, ";"); index >= 0 {
		subtype = subtype[:index]
	}
	subtype = strings.Replace(subtype, "mpeg", "mp3", 1)
	subtype = strings.Replace(subtype, "x-wav", "wav", 1)
	return subtype
}
