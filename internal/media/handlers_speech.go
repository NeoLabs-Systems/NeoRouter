package media

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Speech is POST /v1/audio/speech.
func (s *Service) Speech(c *call) error {
	input, err := parseSpeechInput(c.body)
	if err != nil {
		return err
	}
	chars := float64(utf16Length(input.Input))
	tokens := estimateTokens([]string{input.Input})
	estimate := func(entry catalog.Entry) *float64 {
		return priced(unitCost(entry, "mchar", chars), tokenCost(entry, float64(tokens)))
	}
	voice := ""
	if input.Voice != nil {
		voice = *input.Voice
	}
	options := &RunOptions{
		Capability: "speech", Method: providers.CapSpeech, RequestedModel: input.Model,
		FreeOnly: c.freeOnly, InputTokens: tokens, Units: chars,
		EstimateCost: estimate,
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			speaker, ok := attempt.Adapter.(providers.Speaker)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "This provider cannot speak.", providers.ErrorOptions{})
			}
			call := providers.Call{ModelID: attempt.Entry.ModelID, Timeout: attempt.Timeout}
			say := func(voice string) (*providers.SpeechResult, error) {
				return speaker.Speech(ctx, providers.SpeechRequest{Input: input.Input, Voice: voice, Format: input.Format, Speed: input.Speed}, call)
			}
			if voice == "" {
				result, err := say("")
				if err != nil {
					return nil, err
				}
				return &Outcome{Value: result, Headers: result.Headers}, nil
			}
			result, err := say(voice)
			if err != nil && isBadRequest(err) {
				// A voice this provider does not have (voice names differ per
				// provider): its default voice rather than no audio.
				result, err = say("")
			}
			if err != nil {
				return nil, err
			}
			return &Outcome{Value: result, Headers: result.Headers}, nil
		},
		CostOf: func(entry catalog.Entry, _ *Outcome) *float64 {
			if cost := estimate(entry); cost != nil {
				return cost
			}
			return zeroCost()
		},
		Respond: func(w http.ResponseWriter, outcome *Outcome, entry catalog.Entry) error {
			return s.respondSpeech(w, c, input, outcome, entry)
		},
	}
	_, err = s.run(c, options)
	return err
}

// speechJSON is the playground's speech answer: the audio as base64.
type speechJSON struct {
	Audio  string `json:"audio"`
	Mime   string `json:"mime"`
	Format string `json:"format"`
	Model  string `json:"model"`
}

// respondSpeech writes the audio in the format the client asked for.
func (s *Service) respondSpeech(w http.ResponseWriter, c *call, input speechInput, outcome *Outcome, entry catalog.Entry) error {
	result, _ := outcome.Value.(*providers.SpeechResult)
	var audio []byte
	mime := ""
	if result != nil {
		audio, mime = result.Audio, result.Mime
	}
	converted := toFormat(audio, mime, input.Format)
	w.Header().Set("X-NeoRouter-Audio-Format", converted.Format)
	if c.asJSON {
		httpx.WriteJSON(w, http.StatusOK, speechJSON{
			Audio: base64.StdEncoding.EncodeToString(converted.Audio), Mime: converted.Mime,
			Format: converted.Format, Model: entry.ID,
		})
		return nil
	}
	w.Header().Set("Content-Type", converted.Mime)
	w.Header().Set("Content-Length", fmt.Sprint(len(converted.Audio)))
	_, err := w.Write(converted.Audio)
	return err
}
