package media

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Asking one System One classifier: the canonical chat request each family is
// asked and the loop that reads one answer per input
// (server/capabilities/media/system_one.js classify).

// chatMessage is one canonical chat message.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// systemOneRequest is the canonical chat request a classifier is asked.
type systemOneRequest struct {
	Messages       []chatMessage `json:"messages"`
	MaxTokens      int           `json:"max_tokens"`
	Temperature    float64       `json:"temperature"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

// systemOneCompletion is the part of a canonical completion a classifier reads.
type systemOneCompletion struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens float64 `json:"prompt_tokens"`
	} `json:"usage"`
}

// systemOneMessages is what one input is asked, per family.
func systemOneMessages(family, input string, policy *string) []chatMessage {
	policyText := ""
	if policy != nil {
		policyText = *policy
	}
	switch family {
	case "safeguard", "generic":
		if policyText == "" {
			policyText = defaultSafetyPolicy
		}
		return []chatMessage{{Role: "system", Content: policyText}, {Role: "user", Content: input}}
	case "topic_control":
		if policyText == "" {
			policyText = "You are a general assistant. Allowed topics: anything legal and safe."
		}
		return []chatMessage{{Role: "system", Content: policyText}, {Role: "user", Content: input}}
	}
	return []chatMessage{{Role: "user", Content: input}}
}

// classifyResult is one classifier run over a request's inputs.
type classifyResult struct {
	Results     []moderationResult
	Headers     http.Header
	InputTokens int
}

// classifySystemOne classifies each input with one model. A native moderation
// model on an adapter with a moderation endpoint answers in one call; anything
// else is one chat call per input.
func classifySystemOne(ctx context.Context, adapter providers.Adapter, inputs []string, policy *string, call providers.Call) (*classifyResult, error) {
	family := systemOneFamily(call.ModelID)
	if family == "native" && adapter.Supports(providers.CapModerate) {
		if moderator, ok := adapter.(providers.Moderator); ok {
			answer, err := moderator.Moderate(ctx, providers.ModerateRequest{Input: inputs}, call)
			if err != nil {
				return nil, err
			}
			return &classifyResult{Results: fromNative(answer.Results), Headers: answer.Headers}, nil
		}
	}
	if chatter, ok := adapter.(providers.Chatter); ok {
		results := make([]moderationResult, 0, len(inputs))
		var headers http.Header
		inputTokens := 0
		for _, input := range inputs {
			request := systemOneRequest{
				Messages:    systemOneMessages(family, input, policy),
				MaxTokens:   200,
				Temperature: 0,
			}
			if family == "prompt_guard" {
				request.MaxTokens = 16
			}
			if family == "safeguard" || family == "generic" {
				request.ResponseFormat = &struct {
					Type string `json:"type"`
				}{Type: "json_object"}
			}
			body, err := json.Marshal(request)
			if err != nil {
				return nil, fmt.Errorf("media: encode classifier request: %w", err)
			}
			answer, err := chatter.Chat(ctx, body, call)
			if err != nil {
				return nil, err
			}
			headers = answer.Headers
			var completion systemOneCompletion
			if len(answer.Body) > 0 {
				if err := json.Unmarshal(answer.Body, &completion); err != nil {
					return nil, invalidOutput("The classifier did not answer in the expected JSON format.")
				}
			}
			inputTokens += int(completion.Usage.PromptTokens)
			content, _ := completionContent(completion)
			result, err := parseSystemOne(family, content)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
		return &classifyResult{Results: results, Headers: headers, InputTokens: inputTokens}, nil
	}
	return nil, invalidOutput("This provider cannot classify.")
}

// completionContent is the text of the first choice.
func completionContent(completion systemOneCompletion) (string, bool) {
	if len(completion.Choices) == 0 {
		return "", false
	}
	content, ok := completion.Choices[0].Message.Content.(string)
	return content, ok
}
