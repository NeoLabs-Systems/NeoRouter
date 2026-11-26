package media

import (
	"errors"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// The request handlers of the non-chat kinds. Each validates an OpenAI-style
// body, runs it through the shared pipeline (routing, fallback, learning,
// quotas, budget, usage log) and writes the OpenAI response shape. The /v1
// gateway and the dashboard playground share these, so both behave the same.

// call is one media request being answered.
type call struct {
	w   http.ResponseWriter
	r   *http.Request
	key *routerkeys.Row
	// body is the decoded JSON body (nil when the request carries none).
	body     any
	freeOnly bool
	// fields and file are a transcription's multipart members.
	fields map[string]any
	file   *audioFile
	// translate asks a transcription for English whatever the source language.
	translate bool
	// includeModel makes a transcription answer the playground's shape.
	includeModel bool
	// asJSON makes speech answer base64 JSON instead of audio bytes.
	asJSON bool
}

// audioFile is one uploaded audio clip.
type audioFile struct {
	data     []byte
	filename string
	mime     string
}

// run sends one capability request through the shared pipeline.
func (s *Service) run(c *call, options *RunOptions) (*Result, error) {
	if s.runner == nil {
		return nil, errors.New("media: the shared capability pipeline is not wired")
	}
	options.Request = c.r
	options.Response = c.w
	options.Key = c.key
	if options.Format == "" {
		options.Format = options.Capability
	}
	if options.ContextTokens == nil {
		tokens := options.InputTokens
		options.ContextTokens = &tokens
	}
	return s.runner.Run(c.r.Context(), options)
}

// providerError is the classified failure an adapter just returned for a
// request whose own options were refused (a voice this provider does not have).
func isBadRequest(err error) bool {
	return providers.KindOf(err) == providers.KindBadRequest
}
