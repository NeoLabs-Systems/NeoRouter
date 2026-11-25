package media

import (
	"context"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/google/uuid"
)

// checkFamilies says which System One families can answer which question. Prompt
// Guard only spots prompt attacks and topic control needs a policy, so neither
// may stand in for a general safety check.
var checkFamilies = map[string]map[string]bool{
	"safety":           {"llama_guard": true, "nemoguard": true, "safeguard": true, "shieldgemma": true, "native": true, "generic": true},
	"prompt_injection": {"prompt_guard": true},
	"topic":            {"topic_control": true, "safeguard": true},
}

// moderationResponse is the /v1/moderations body.
type moderationResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Results []moderationResult `json:"results"`
}

// Moderations is POST /v1/moderations.
func (s *Service) Moderations(c *call) error {
	input, err := parseModerationInput(c.body)
	if err != nil {
		return err
	}
	if input.Check == "topic" && input.Policy == nil {
		return httpserver.New(http.StatusBadRequest, "INVALID_REQUEST",
			"policy: a topic check needs a policy that says which topics are allowed.")
	}
	if len(input.Inputs) == 0 {
		return httpserver.New(http.StatusBadRequest, "INVALID_REQUEST", "input: only text inputs are supported.")
	}
	tokens := estimateTokens(input.Inputs)
	if input.Policy != nil {
		tokens += estimateTokens([]string{*input.Policy})
	}
	auto := routing.IsAutoModel(input.Model)
	families := checkFamilies[input.Check]
	inputs := input.Inputs
	policy := input.Policy
	options := &RunOptions{
		Capability: "moderation", Method: providers.CapChat, RequestedModel: input.Model,
		FreeOnly: c.freeOnly, InputTokens: tokens, Units: float64(len(inputs)),
		// A model named explicitly is taken at its word; auto picks a family that
		// fits the check. Decision models answer other questions and never stand in
		// for a check.
		Filter: func(entry catalog.Entry, _ providers.Adapter) bool {
			if entry.Decision {
				return false
			}
			if !auto {
				return true
			}
			return families[systemOneFamily(entry.ModelID)]
		},
		EstimateCost: func(entry catalog.Entry) *float64 {
			return tokenCost(entry, float64(tokens+400*len(inputs)))
		},
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			answer, err := classifySystemOne(ctx, attempt.Adapter, inputs, policy,
				providers.Call{ModelID: attempt.Entry.ModelID, Timeout: attempt.Timeout})
			if err != nil {
				return nil, err
			}
			outcome := &Outcome{Value: answer, Headers: answer.Headers}
			if answer.InputTokens > 0 {
				reported := answer.InputTokens
				outcome.InputTokens = &reported
			}
			return outcome, nil
		},
		CostOf: func(entry catalog.Entry, outcome *Outcome) *float64 {
			used := tokens
			if answer, ok := outcome.Value.(*classifyResult); ok && answer.InputTokens > 0 {
				used = answer.InputTokens
			}
			if cost := tokenCost(entry, float64(used)); cost != nil {
				return cost
			}
			return zeroCost()
		},
		Respond: func(w http.ResponseWriter, outcome *Outcome, entry catalog.Entry) error {
			answer, _ := outcome.Value.(*classifyResult)
			results := []moderationResult{}
			if answer != nil {
				results = answer.Results
			}
			httpx.WriteJSON(w, http.StatusOK, moderationResponse{ID: "modr-" + uuid.NewString(), Model: entry.ID, Results: results})
			return nil
		},
	}
	_, err = s.run(c, options)
	return err
}

// decisionResponse is the /v1/systemone body.
type decisionResponse struct {
	Model   string `json:"model"`
	Answers any    `json:"answers,omitempty"`
	Usage   any    `json:"usage,omitempty"`
}

// Decide is POST /v1/systemone: TypeSafe's decision API (state + typed questions
// → answers), routed over the decision models of every connected provider.
func (s *Service) Decide(c *call) error {
	input, err := parseDecisionInput(c.body)
	if err != nil {
		return err
	}
	stateText := `""`
	if input.State != nil {
		stateText = jsonText(input.State)
	}
	tokens := estimateTokens([]string{stateText, jsonText(input.Questions)})
	state, questions := input.State, input.Questions
	options := &RunOptions{
		Capability: "moderation", Method: providers.CapDecide, RequestedModel: input.Model,
		FreeOnly: c.freeOnly, InputTokens: tokens, Units: 1,
		Filter:       func(entry catalog.Entry, _ providers.Adapter) bool { return entry.Decision },
		EstimateCost: func(entry catalog.Entry) *float64 { return tokenCost(entry, float64(tokens+200)) },
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			decider, ok := attempt.Adapter.(providers.Decider)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "This provider cannot answer decisions.", providers.ErrorOptions{})
			}
			result, err := decider.Decide(ctx, providers.DecideRequest{State: state, Questions: questions},
				providers.Call{ModelID: attempt.Entry.ModelID, Timeout: attempt.Timeout})
			if err != nil {
				return nil, err
			}
			return &Outcome{Value: result, InputTokens: result.InputTokens, Headers: result.Headers}, nil
		},
		CostOf: func(entry catalog.Entry, outcome *Outcome) *float64 {
			used := tokens
			if result, ok := outcome.Value.(*providers.DecideResult); ok && result.InputTokens != nil {
				used = *result.InputTokens
			}
			if cost := tokenCost(entry, float64(used)); cost != nil {
				return cost
			}
			return zeroCost()
		},
		Respond: func(w http.ResponseWriter, outcome *Outcome, entry catalog.Entry) error {
			answer, _ := outcome.Value.(*providers.DecideResult)
			body := decisionResponse{Model: entry.ID}
			if answer != nil {
				body.Answers = answer.Answers
				body.Usage = answer.Usage
			}
			httpx.WriteJSON(w, http.StatusOK, body)
			return nil
		},
	}
	_, err = s.run(c, options)
	return err
}
