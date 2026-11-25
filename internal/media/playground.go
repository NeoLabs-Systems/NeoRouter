package media

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// The playground's non-chat modes (server/routes/playground_media.js). They run
// through the same handlers as /v1 with one of the person's stored keys, and
// audio comes and goes as base64 JSON so the dashboard needs neither multipart
// nor binary handling.

// PlaygroundRoutes registers the dashboard's media endpoints at
// /api/v1/playground, behind the playground permission.
func (s *Service) PlaygroundRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/playground/media/{kind}", s.permission(s.playgroundMedia))
	mux.Handle("GET /api/v1/playground/voices", s.permission(s.playgroundVoices))
	mux.Handle("GET /api/v1/playground/videos/{id}", s.permission(s.playgroundVideo))
	mux.Handle("GET /api/v1/playground/videos/{id}/content", s.permission(s.playgroundVideoContent))
}

// openAIVoices are the voice names the playground offers when the model is not
// fixed: every speech adapter maps OpenAI's names onto a voice of its own
// (providers/adapters/openai_media.js OPENAI_VOICES).
var openAIVoices = []string{"alloy", "ash", "ballad", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer", "verse"}

// voiceList names voices the way the API lists them.
func voiceList(ids []string) []providers.Voice {
	voices := make([]providers.Voice, 0, len(ids))
	for _, id := range ids {
		name := id
		if id != "" {
			name = strings.ToUpper(id[:1]) + id[1:]
		}
		voices = append(voices, providers.Voice{ID: id, Name: name})
	}
	return voices
}

// playgroundKind reports whether the playground serves one kind of request.
func playgroundKind(kind string) bool {
	switch kind {
	case "embedding", "image", "speech", "moderation", "decision", "search", "weather", "video", "transcription":
		return true
	}
	return false
}

// playgroundMedia is POST /api/v1/playground/media/{kind}.
func (s *Service) playgroundMedia(w http.ResponseWriter, r *http.Request) {
	space, err := s.requestSpace(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	kind := r.PathValue("kind")
	if !playgroundKind(kind) {
		httpserver.WriteError(w, r, s.log, httpserver.New(http.StatusNotFound, "UNKNOWN_KIND", fmt.Sprintf("Unknown kind \"%s\".", kind)))
		return
	}
	var raw any
	if err := httpserver.ReadJSON(w, r, &raw, httpserver.PlaygroundMediaBodyLimit); err != nil {
		return
	}
	key, body, freeOnly, err := s.playgroundKey(space, raw)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	c := &call{w: w, r: r, key: key, body: body, freeOnly: freeOnly}
	switch kind {
	case "embedding":
		err = s.Embeddings(c)
	case "image":
		// The dashboard shows the image itself, so raw bytes are the default here.
		if fields, ok := body.(map[string]any); ok {
			if _, sent := fields["response_format"]; !sent {
				fields["response_format"] = "b64_json"
			}
		}
		err = s.Images(c)
	case "speech":
		c.asJSON = true
		err = s.Speech(c)
	case "moderation":
		err = s.Moderations(c)
	case "decision":
		err = s.Decide(c)
	case "search":
		err = s.Search(c)
	case "weather":
		err = s.Weather(c)
	case "video":
		err = s.CreateVideo(c)
	case "transcription":
		err = s.playgroundTranscription(c)
	}
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
	}
}

// playgroundKey reads the envelope every playground request carries: which of the
// person's keys to run it with, and whether to stay on free models.
func (s *Service) playgroundKey(space Space, raw any) (*routerkeys.Row, any, bool, error) {
	missing := httpserver.New(http.StatusBadRequest, "INVALID_REQUEST", "keyId is required.")
	body, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, false, missing
	}
	keyID, ok := body["keyId"].(string)
	if !ok || keyID == "" {
		return nil, nil, false, missing
	}
	freeOnly := false
	if value, present := body["freeOnly"]; present && value != nil {
		flag, isBool := value.(bool)
		if !isBool {
			return nil, nil, false, missing
		}
		freeOnly = flag
	}
	if s.keyRow == nil {
		return nil, nil, false, fmt.Errorf("media: the router key store is not wired")
	}
	key, err := s.keyRow(space.OwnerID, keyID)
	if err != nil {
		return nil, nil, false, err
	}
	if key.RevokedAt != nil {
		return nil, nil, false, httpserver.New(http.StatusBadRequest, "KEY_REVOKED", "This router key is revoked.")
	}
	rest := make(map[string]any, len(body))
	for name, value := range body {
		if name == "keyId" || name == "freeOnly" {
			continue
		}
		rest[name] = value
	}
	return key, rest, freeOnly, nil
}

// playgroundTranscription is the playground's transcription: base64 audio and its
// file name in the body rather than a multipart upload.
func (s *Service) playgroundTranscription(c *call) error {
	fields, ok := c.body.(map[string]any)
	if !ok {
		return invalid("audio: base64 audio is required.")
	}
	audio, _ := fields["audio"].(string)
	if audio == "" {
		return invalid("audio: base64 audio is required.")
	}
	filename := "audio.wav"
	if value, ok := fields["filename"].(string); ok && value != "" {
		filename = value
	}
	mime := ""
	if value, ok := fields["mime"].(string); ok && value != "" {
		mime = value
	}
	translate, _ := fields["translate"].(bool)
	rest := make(map[string]any, len(fields))
	for name, value := range fields {
		switch name {
		case "audio", "filename", "mime", "translate":
			continue
		}
		rest[name] = value
	}
	c.fields = rest
	c.file = &audioFile{data: decodeBase64(audio), filename: filename, mime: mime}
	c.translate = translate
	c.includeModel = true
	return s.Transcription(c)
}

// decodeBase64 is Buffer.from(value, 'base64'): base64 characters are decoded and
// everything else is skipped.
func decodeBase64(value string) []byte {
	clean := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch {
		case character >= 'A' && character <= 'Z',
			character >= 'a' && character <= 'z',
			character >= '0' && character <= '9',
			character == '+', character == '/':
			clean = append(clean, character)
		}
	}
	data, err := base64.RawStdEncoding.DecodeString(string(clean))
	if err != nil {
		return nil
	}
	return data
}

// playgroundVoices is GET /api/v1/playground/voices: the voices a speech model
// offers. Auto may land on any provider, so it offers OpenAI's names, which every
// speech adapter maps to a voice of its own.
func (s *Service) playgroundVoices(w http.ResponseWriter, r *http.Request) {
	space, err := s.requestSpace(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	model := r.URL.Query().Get("model")
	if model == "" {
		model = "auto"
	}
	voices := voiceList(openAIVoices)
	if !strings.HasPrefix(model, "auto") {
		entries, err := s.models(r.Context(), space.OwnerID, "speech")
		if err != nil {
			httpserver.WriteError(w, r, s.log, err)
			return
		}
		for _, entry := range entries {
			if entry.ID != model {
				continue
			}
			if adapter := s.adapterFor(entry.AccountID); adapter != nil {
				if voicer, ok := adapter.(providers.Voicer); ok {
					if listed, err := voicer.Voices(r.Context(), entry.ModelID); err == nil {
						voices = listed
					}
				}
			}
			break
		}
	}
	httpx.WriteJSON(w, http.StatusOK, voicesResponse{Voices: voices})
}

// voicesResponse is the voice list the playground offers a model.
type voicesResponse struct {
	Voices []providers.Voice `json:"voices"`
}

// playgroundVideo is GET /api/v1/playground/videos/{id}.
func (s *Service) playgroundVideo(w http.ResponseWriter, r *http.Request) {
	space, err := s.requestSpace(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	job, err := s.findVideoJob(r.Context(), space.OwnerID, r.PathValue("id"))
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	if job, err = s.refreshVideoJob(r.Context(), job); err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job.object())
}

// playgroundVideoContent is GET /api/v1/playground/videos/{id}/content: the
// finished file as base64, so the dashboard can offer it for download.
func (s *Service) playgroundVideoContent(w http.ResponseWriter, r *http.Request) {
	space, err := s.requestSpace(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	job, err := s.findVideoJob(r.Context(), space.OwnerID, r.PathValue("id"))
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	if job, err = s.refreshVideoJob(r.Context(), job); err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	response, err := s.videoContent(r.Context(), job)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	mime := response.Header.Get("Content-Type")
	if mime == "" {
		mime = "video/mp4"
	}
	httpx.WriteJSON(w, http.StatusOK, videoContentResponse{
		Video: base64.StdEncoding.EncodeToString(data), Mime: mime,
	})
}

// videoContentResponse is a finished video as the playground downloads it.
type videoContentResponse struct {
	Video string `json:"video"`
	Mime  string `json:"mime"`
}
