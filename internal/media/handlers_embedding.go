package media

import (
	"context"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// Embeddings is POST /v1/embeddings.
func (s *Service) Embeddings(c *call) error {
	input, err := parseEmbeddingInput(c.body)
	if err != nil {
		return err
	}
	shielded, err := s.shieldTexts(c.key.UserID, input.Input)
	if err != nil {
		return err
	}
	texts := shielded.Texts
	inputType := "query"
	if input.InputType != nil && containsAny(*input.InputType, "passage", "document") {
		inputType = "passage"
	}
	tokens := estimateTokens(texts)
	auto := routing.IsAutoModel(input.Model)
	var pinned *pin
	if auto {
		if pinned, err = s.embeddingPin(c.r.Context(), c.key.ID); err != nil {
			return err
		}
	}
	if pinned != nil && pinned.Dimensions != nil && input.Dimensions != nil && *input.Dimensions != *pinned.Dimensions {
		return embeddingPinMismatch(pinned)
	}
	dimensions := input.Dimensions
	if dimensions == nil && pinned != nil {
		dimensions = pinned.Dimensions
	}
	options := &RunOptions{
		Capability: "embedding", Method: providers.CapEmbed, RequestedModel: input.Model,
		FreeOnly: c.freeOnly, InputTokens: tokens, Units: float64(len(texts)),
		RedactedCount: shielded.Redacted,
		EstimateCost:  func(entry catalog.Entry) *float64 { return tokenCost(entry, float64(tokens)) },
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			embedder, ok := attempt.Adapter.(providers.Embedder)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "This provider cannot embed.", providers.ErrorOptions{})
			}
			result, err := embedder.Embed(ctx, providers.EmbedRequest{Input: texts, Dimensions: dimensions, InputType: inputType},
				providers.Call{ModelID: attempt.Entry.ModelID, Timeout: attempt.Timeout})
			if err != nil {
				return nil, err
			}
			// Some embedders ignore the size they are asked for. Their vectors would
			// not fit the caller's index, and pinning them would lock the key to a
			// model that never answers at that size, so another model is tried.
			if dimensions != nil {
				for _, vector := range result.Vectors {
					if len(vector) != *dimensions {
						return nil, providers.NewError(providers.KindBadRequest,
							fmt.Sprintf("%s returned %d dimensions instead of the %d asked for.", attempt.Entry.ID, len(vector), *dimensions),
							providers.ErrorOptions{})
					}
				}
			}
			return &Outcome{Value: result, InputTokens: result.InputTokens, Headers: result.Headers}, nil
		},
		CostOf: func(entry catalog.Entry, result *Outcome) *float64 {
			used := float64(tokens)
			if result.InputTokens != nil {
				used = float64(*result.InputTokens)
			}
			if cost := tokenCost(entry, used); cost != nil {
				return cost
			}
			return zeroCost()
		},
		Respond: func(w http.ResponseWriter, result *Outcome, entry catalog.Entry) error {
			return s.respondEmbeddings(w, c, input, result, entry, auto, pinned, tokens)
		},
	}
	if pinned != nil {
		canonicalID := pinned.CanonicalID
		options.Filter = func(entry catalog.Entry, _ providers.Adapter) bool { return entry.CanonicalID == canonicalID }
		options.NoCandidate = func() error { return embeddingPinUnavailable(pinned) }
	}
	_, err = s.run(c, options)
	return err
}

// respondEmbeddings writes the embeddings response, pinning an auto request to
// the model (and size) it used.
func (s *Service) respondEmbeddings(w http.ResponseWriter, c *call, input embeddingInput, outcome *Outcome, entry catalog.Entry,
	auto bool, pinned *pin, tokens int) error {
	result, _ := outcome.Value.(*providers.EmbedResult)
	size := 0
	if result != nil && len(result.Vectors) > 0 {
		size = len(result.Vectors[0])
	}
	if auto && pinned == nil {
		dimensions := input.Dimensions
		if dimensions == nil && size > 0 {
			dimensions = &size
		}
		if err := s.setEmbeddingPin(c.r.Context(), c.key.ID, entry.CanonicalID, dimensions); err != nil {
			return err
		}
	}
	if auto {
		w.Header().Set("X-NeoRouter-Embedding-Pin", entry.CanonicalID)
	}
	used := tokens
	if outcome.InputTokens != nil {
		used = *outcome.InputTokens
	}
	var vectors [][]float64
	if result != nil {
		vectors = result.Vectors
	}
	data := make([]embeddingItem, 0, len(vectors))
	for index, vector := range vectors {
		item := embeddingItem{Object: "embedding", Index: index}
		if input.EncodingFormat == "base64" {
			item.Embedding = float32Base64(vector)
		} else {
			item.Embedding = vector
		}
		data = append(data, item)
	}
	httpx.WriteJSON(w, http.StatusOK, embeddingResponse{
		Object: "list", Data: data, Model: entry.ID,
		Usage: embeddingUsage{PromptTokens: used, TotalTokens: used},
	})
	return nil
}

// embeddingItem is one vector of an embeddings response.
type embeddingItem struct {
	Object    string `json:"object"`
	Index     int    `json:"index"`
	Embedding any    `json:"embedding"`
}

// embeddingResponse is the embeddings response body.
type embeddingResponse struct {
	Object string          `json:"object"`
	Data   []embeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  embeddingUsage  `json:"usage"`
}

// embeddingUsage is what an embeddings call used.
type embeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
