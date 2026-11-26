package media

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Images is POST /v1/images/generations.
func (s *Service) Images(c *call) error {
	input, err := parseImageInput(c.body)
	if err != nil {
		return err
	}
	size := ""
	if input.Size != nil && *input.Size != "auto" {
		size = *input.Size
	}
	shielded, err := s.shieldTexts(c.key.UserID, []string{input.Prompt})
	if err != nil {
		return err
	}
	if len(shielded.Texts) > 0 {
		input.Prompt = shielded.Texts[0]
	}
	quality := ""
	if input.Quality != nil {
		quality = *input.Quality
	}
	prompt := input.Prompt
	options := &RunOptions{
		Capability: "image", Method: providers.CapGenerateImage, RequestedModel: input.Model,
		FreeOnly: c.freeOnly, InputTokens: estimateTokens([]string{prompt}), Units: float64(input.N),
		RedactedCount: shielded.Redacted,
		EstimateCost:  func(entry catalog.Entry) *float64 { return unitCost(entry, "image", float64(input.N)) },
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			generator, ok := attempt.Adapter.(providers.ImageGenerator)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "This provider cannot generate images.", providers.ErrorOptions{})
			}
			result, err := generator.GenerateImage(ctx, providers.ImageRequest{Prompt: prompt, N: input.N, Size: size, Quality: quality},
				providers.Call{ModelID: attempt.Entry.ModelID, Timeout: attempt.Timeout})
			if err != nil {
				return nil, err
			}
			return &Outcome{Value: result, Headers: result.Headers}, nil
		},
		CostOf: func(entry catalog.Entry, outcome *Outcome) *float64 {
			images := 0
			if result, ok := outcome.Value.(*providers.ImageResult); ok {
				images = len(result.Images)
			}
			if cost := unitCost(entry, "image", float64(images)); cost != nil {
				return cost
			}
			return zeroCost()
		},
		Respond: func(w http.ResponseWriter, outcome *Outcome, entry catalog.Entry) error {
			return s.respondImages(w, c, input, outcome, entry)
		},
	}
	_, err = s.run(c, options)
	return err
}

// imageItem is one image of an images response: base64 data or a link.
type imageItem struct {
	B64JSON       *string `json:"b64_json,omitempty"`
	URL           *string `json:"url,omitempty"`
	RevisedPrompt *string `json:"revised_prompt,omitempty"`
}

// imageResponse is the images response body.
type imageResponse struct {
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Data    []imageItem `json:"data"`
}

// respondImages writes the images response, downloading what the provider only
// linked.
func (s *Service) respondImages(w http.ResponseWriter, c *call, input imageInput, outcome *Outcome, entry catalog.Entry) error {
	result, _ := outcome.Value.(*providers.ImageResult)
	data := []imageItem{}
	if result != nil {
		for _, image := range result.Images {
			b64, mime := image.B64, image.Mime
			if b64 == "" && input.ResponseFormat == "b64_json" {
				fetched, fetchedMime, err := s.fetchImage(c.r.Context(), image.URL)
				if err != nil {
					return err
				}
				b64, mime = fetched, fetchedMime
			}
			item := imageItem{}
			if image.RevisedPrompt != nil && *image.RevisedPrompt != "" {
				item.RevisedPrompt = image.RevisedPrompt
			}
			if input.ResponseFormat == "b64_json" {
				item.B64JSON = &b64
			} else if b64 != "" {
				bytes, err := base64.StdEncoding.DecodeString(b64)
				if err != nil {
					return err
				}
				if mime == "" {
					mime = "image/png"
				}
				url := s.publicURL(c.r, s.store.put(bytes, mime))
				item.URL = &url
			} else {
				link := image.URL
				item.URL = &link
			}
			data = append(data, item)
		}
	}
	w.Header().Set("X-NeoRouter-Model", entry.ID)
	httpx.WriteJSON(w, http.StatusOK, imageResponse{Created: s.now().Unix(), Model: entry.ID, Data: data})
	return nil
}

// fetchImage downloads an image the provider only returned a URL for.
func (s *Service) fetchImage(ctx context.Context, url string) (string, string, error) {
	unavailable := httpserver.New(http.StatusBadGateway, "IMAGE_FETCH_FAILED",
		"The generated image could not be downloaded from the provider.")
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", unavailable
	}
	response, err := s.client().Do(request)
	if err != nil {
		return "", "", unavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", "", unavailable
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", "", unavailable
	}
	mime := response.Header.Get("Content-Type")
	if mime == "" {
		mime = "image/png"
	}
	return base64.StdEncoding.EncodeToString(body), mime, nil
}
