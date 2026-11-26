package media

import (
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// Routes registers the OpenAI-shaped media endpoints under /v1: embeddings,
// image generation, speech, transcription and translation, System One
// (moderations and decisions), video jobs, web search, weather, and the
// generated-media links.
func (s *Service) Routes(mux *http.ServeMux) {
	keyed := func(pattern string, kind bodyKind, handler func(*call) error) {
		mux.Handle(pattern, s.guard(kind, handler))
	}
	keyed("POST /v1/embeddings", bodyJSON, s.Embeddings)
	keyed("GET /v1/embeddings/pin", bodyNone, s.showEmbeddingPin)
	keyed("DELETE /v1/embeddings/pin", bodyNone, s.resetEmbeddingPin)
	keyed("POST /v1/images/generations", bodyJSON, s.Images)
	keyed("POST /v1/audio/speech", bodyJSON, s.Speech)
	keyed("POST /v1/moderations", bodyJSON, s.Moderations)
	keyed("POST /v1/systemone", bodyJSON, s.Decide)
	keyed("POST /v1/search", bodyJSON, s.Search)
	keyed("POST /v1/weather", bodyJSON, s.Weather)
	keyed("POST /v1/audio/transcriptions", bodyMultipart, s.transcribe(false))
	keyed("POST /v1/audio/translations", bodyMultipart, s.transcribe(true))
	keyed("POST /v1/videos", bodyJSON, s.CreateVideo)
	keyed("GET /v1/videos", bodyNone, s.listVideos)
	keyed("GET /v1/videos/{id}", bodyNone, s.showVideo)
	keyed("GET /v1/videos/{id}/content", bodyNone, s.serveVideo)
	// Generated media handed out as URLs: the unguessable id is the capability.
	mux.Handle("GET /v1/media/{id}", http.HandlerFunc(s.serveMedia))
}

// bodyKind is how a route reads its request body.
type bodyKind int

const (
	// bodyNone is a route without a request body.
	bodyNone bodyKind = iota
	// bodyJSON is a JSON body, bounded by the gateway's limit.
	bodyJSON
	// bodyMultipart is a multipart upload (an audio file), with its own bound.
	bodyMultipart
)

// multipartLimit is the media router's own body limit for multipart/form-data.
const multipartLimit int64 = 26 << 20

// writingResponse remembers whether a handler already answered, so a late error
// does not append a second body.
type writingResponse struct {
	http.ResponseWriter
	wrote bool
}

func (w *writingResponse) WriteHeader(status int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *writingResponse) Write(data []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(data)
}

// guard wraps one media handler with the router-key check, the body and the
// gateway error shape.
func (s *Service) guard(kind bodyKind, handler func(*call) error) http.Handler {
	serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracked := &writingResponse{ResponseWriter: w}
		key, ok := s.routerKey(r)
		if !ok {
			s.writeError(tracked, r, httpserver.New(http.StatusUnauthorized, "INVALID_API_KEY",
				"A valid NeoRouter key (nrt_…) is required."))
			return
		}
		c := &call{w: tracked, r: r, key: key}
		switch kind {
		case bodyJSON:
			var body any
			if err := httpserver.ReadJSON(tracked, r, &body, s.settings().MaxBodyBytes); err != nil {
				return
			}
			c.body = body
		case bodyMultipart:
			fields, file, err := readForm(tracked, r)
			if err != nil {
				s.writeError(tracked, r, err)
				return
			}
			c.fields, c.file = fields, file
		}
		if err := handler(c); err != nil && !tracked.wrote {
			s.writeError(tracked, r, err)
		}
	})
	if s.keyAuth == nil {
		return serve
	}
	return s.keyAuth.Require(serve)
}

// routerKey is the router key the gateway's middleware resolved.
func (s *Service) routerKey(r *http.Request) (*routerkeys.Row, bool) {
	if s.keyAuth == nil {
		return nil, false
	}
	return s.keyAuth.Key(r)
}

// gatewayErrorBody is one gateway error: the OpenAI shape clients parse.
type gatewayErrorBody struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Code    *string `json:"code"`
	Details any     `json:"details,omitempty"`
}

// gatewayErrorEnvelope is the gateway error response.
type gatewayErrorEnvelope struct {
	Error gatewayErrorBody `json:"error"`
}

// writeError answers a failed /v1 request: an unknown failure is masked, the
// type follows the status, and Retry-After carries what the pipeline decided.
func (s *Service) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	message := "An internal error occurred."
	var code *string
	var details any
	var httpError *httpserver.HttpError
	if errors.As(err, &httpError) {
		if httpError.Status != 0 {
			status = httpError.Status
		}
		message = httpError.Message
		if httpError.Code != "" {
			value := httpError.Code
			code = &value
		}
		details = httpError.Details
		if httpError.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(httpError.RetryAfter))
		}
	}
	// A failure with no code of its own is unexpected: it is logged and its text
	// is not shown to the client.
	if status >= 500 && code == nil {
		message = "An internal error occurred."
		s.log.Error("Gateway request failed", "requestId", httpserver.RequestIDFrom(r.Context()), "error", err)
	}
	kind := "invalid_request_error"
	switch {
	case status >= 500:
		kind = "server_error"
	case status == http.StatusTooManyRequests:
		kind = "rate_limit_error"
	}
	httpx.WriteJSON(w, status, gatewayErrorEnvelope{Error: gatewayErrorBody{
		Message: message, Type: kind, Code: code, Details: details,
	}})
}

// readForm parses a multipart upload: every text member is a field and the "file"
// member is the audio.
func readForm(w http.ResponseWriter, r *http.Request) (map[string]any, *audioFile, error) {
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data") {
		return nil, nil, httpserver.New(http.StatusBadRequest, "INVALID_REQUEST",
			`Send the audio as multipart/form-data with a "file" field.`)
	}
	unreadable := httpserver.New(http.StatusBadRequest, "INVALID_REQUEST", "The multipart body could not be parsed.")
	r.Body = http.MaxBytesReader(w, r.Body, multipartLimit)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, unreadable
	}
	fields := map[string]any{}
	var file *audioFile
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, nil, httpserver.New(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "The request body is too large.")
			}
			return nil, nil, unreadable
		}
		if part.FileName() == "" {
			data, err := io.ReadAll(part)
			name := part.FormName()
			part.Close()
			if err != nil {
				return nil, nil, unreadable
			}
			fields[name] = string(data)
			continue
		}
		if part.FormName() == "file" && file == nil {
			filename := part.FileName()
			mime := part.Header.Get("Content-Type")
			data, err := io.ReadAll(part)
			part.Close()
			if err != nil {
				return nil, nil, unreadable
			}
			file = &audioFile{data: data, filename: filename, mime: mime}
			continue
		}
		part.Close()
	}
	return fields, file, nil
}

// transcribe is the transcription route, in translate mode or not.
func (s *Service) transcribe(translate bool) func(*call) error {
	return func(c *call) error {
		c.translate = translate
		return s.Transcription(c)
	}
}

// showEmbeddingPin is GET /v1/embeddings/pin: the model an auto request is
// pinned to.
func (s *Service) showEmbeddingPin(c *call) error {
	pinned, err := s.embeddingPinOf(c)
	if err != nil {
		return err
	}
	httpx.WriteJSON(c.w, http.StatusOK, pinResponse{Pin: pinned})
	return nil
}

// resetEmbeddingPin is DELETE /v1/embeddings/pin.
func (s *Service) resetEmbeddingPin(c *call) error {
	cleared, err := s.clearEmbeddingPin(c.r.Context(), c.key.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(c.w, http.StatusOK, resetPinResponse{Reset: cleared})
	return nil
}

// pinResponse is the pin an auto embedding request is held to.
type pinResponse struct {
	Pin *pinBody `json:"pin"`
}

// pinBody is one stored pin.
type pinBody struct {
	CanonicalID string `json:"canonicalId"`
	Dimensions  *int   `json:"dimensions"`
}

// resetPinResponse reports whether a pin was released.
type resetPinResponse struct {
	Reset bool `json:"reset"`
}

// embeddingPinOf reads the request key's pin in the shape the API returns.
func (s *Service) embeddingPinOf(c *call) (*pinBody, error) {
	pinned, err := s.embeddingPin(c.r.Context(), c.key.ID)
	if err != nil || pinned == nil {
		return nil, err
	}
	return &pinBody{CanonicalID: pinned.CanonicalID, Dimensions: pinned.Dimensions}, nil
}

// videoList is the OpenAI videos listing.
type videoList struct {
	Object  string        `json:"object"`
	Data    []videoObject `json:"data"`
	HasMore bool          `json:"has_more"`
}

// listVideos is GET /v1/videos.
func (s *Service) listVideos(c *call) error {
	limit := 20
	if raw := c.r.URL.Query().Get("limit"); raw != "" {
		if number, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && !math.IsNaN(number) && number != 0 {
			limit = int(number)
		}
	}
	jobs, err := s.listVideoJobs(c.r.Context(), c.key.UserID, limit)
	if err != nil {
		return err
	}
	data := make([]videoObject, 0, len(jobs))
	for index := range jobs {
		data = append(data, jobs[index].object())
	}
	httpx.WriteJSON(c.w, http.StatusOK, videoList{Object: "list", Data: data, HasMore: false})
	return nil
}

// showVideo is GET /v1/videos/{id}.
func (s *Service) showVideo(c *call) error {
	job, err := s.findVideoJob(c.r.Context(), c.key.UserID, c.r.PathValue("id"))
	if err != nil {
		return err
	}
	if job, err = s.refreshVideoJob(c.r.Context(), job); err != nil {
		return err
	}
	httpx.WriteJSON(c.w, http.StatusOK, job.object())
	return nil
}

// serveVideo is GET /v1/videos/{id}/content: the finished file, streamed.
func (s *Service) serveVideo(c *call) error {
	job, err := s.findVideoJob(c.r.Context(), c.key.UserID, c.r.PathValue("id"))
	if err != nil {
		return err
	}
	if job, err = s.refreshVideoJob(c.r.Context(), job); err != nil {
		return err
	}
	response, err := s.videoContent(c.r.Context(), job)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	mime := response.Header.Get("Content-Type")
	if mime == "" {
		mime = "video/mp4"
	}
	serveHeaders(c.w, mime)
	if length := response.Header.Get("Content-Length"); length != "" {
		c.w.Header().Set("Content-Length", length)
	}
	_, err = io.Copy(c.w, response.Body)
	return err
}

// serveMedia is GET /v1/media/{id}: one generated file, by its unguessable id.
func (s *Service) serveMedia(w http.ResponseWriter, r *http.Request) {
	item := s.store.get(r.PathValue("id"))
	if item == nil {
		httpserver.WriteGatewayError(w, http.StatusNotFound, "MEDIA_NOT_FOUND", "This media link expired or does not exist.")
		return
	}
	serveHeaders(w, item.mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Length", strconv.Itoa(len(item.data)))
	_, _ = w.Write(item.data)
}
