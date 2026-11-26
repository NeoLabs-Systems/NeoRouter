package routing

import (
	"sort"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// Preview is what `auto` would choose right now, without sending a request
// (router.preview, GET/POST /routing/preview).
type PreviewOptions struct {
	UserID         string
	Key            RouterKey
	RequestedModel string
	Request        RequestInfo
	Needs          Needs
	InputTokens    int
	Limit          int
	Workload       string
	Now            time.Time
}

// PreviewCandidate is one model in the preview, in the order the router would
// try it.
type PreviewCandidate struct {
	ID       string    `json:"id"`
	Ref      int64     `json:"ref"`
	Name     string    `json:"name"`
	IsFree   bool      `json:"isFree"`
	IsPlan   bool      `json:"isPlan"`
	Rejected *string   `json:"rejected"`
	Score    *float64  `json:"score"`
	Features *Features `json:"features"`
	Factors  *Factors  `json:"factors"`
	// Tier is 0 for a kind the style prefers, higher when only reached if
	// those cannot answer.
	Tier int `json:"tier"`
}

// PreviewSelection is the model the router would pick.
type PreviewSelection struct {
	Model string  `json:"model"`
	Group *string `json:"group"`
}

// PreviewResult is the preview response body.
type PreviewResult struct {
	Group      *string            `json:"group"`
	Profile    *Profile           `json:"profile"`
	Weights    Weights            `json:"weights"`
	Complexity *float64           `json:"complexity"`
	Task       *task.Result       `json:"task"`
	Need       *float64           `json:"need"`
	Candidates []PreviewCandidate `json:"candidates"`
	Selection  *PreviewSelection  `json:"selection"`
}

// Preview ranks what `auto` would choose right now.
func (s *Service) Preview(opts PreviewOptions) (PreviewResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	requested := opts.RequestedModel
	if requested == "" {
		requested = "auto"
	}
	plan, err := s.NewPlan(PlanOptions{
		UserID:         opts.UserID,
		Key:            opts.Key,
		Request:        opts.Request,
		RequestedModel: requested,
		Needs:          opts.Needs,
		InputTokens:    opts.InputTokens,
		Workload:       opts.Workload,
		Now:            opts.Now,
	})
	if err != nil {
		return PreviewResult{}, err
	}
	ranked := plan.Rank(RankOptions{IncludeRejected: true, Sample: false, Now: opts.Now})
	// In the order the router tries them: a style's preferred kinds first.
	sort.SliceStable(ranked, func(i, j int) bool {
		left, right := ranked[i], ranked[j]
		leftRejected, rightRejected := left.Rejected != "", right.Rejected != ""
		if leftRejected != rightRejected {
			return !leftRejected
		}
		if order := compareRoutingPriority(left, right); order != 0 {
			return order < 0
		}
		return scoreKey(left) > scoreKey(right)
	})
	result := PreviewResult{
		Profile:    plan.Profile,
		Weights:    plan.Weights,
		Complexity: plan.Complexity,
		Task:       plan.Task,
		Need:       plan.Need,
		Candidates: []PreviewCandidate{},
	}
	if plan.InitialGroup != nil {
		group := plan.InitialGroup.Name
		result.Group = &group
	}
	for index, item := range ranked {
		if index >= limit*3 {
			break
		}
		candidate := PreviewCandidate{
			ID:     item.Entry.ID,
			Ref:    item.Entry.Ref,
			Name:   item.Entry.Name,
			IsFree: item.Entry.IsFree,
			IsPlan: item.Entry.IsPlan,
			Tier:   item.Tier,
		}
		if item.Rejected != "" {
			rejected := item.Rejected
			candidate.Rejected = &rejected
		}
		if item.Details != nil {
			score := item.Score
			features := item.Details.Features
			factors := item.Details.Factors
			candidate.Score = &score
			candidate.Features = &features
			candidate.Factors = &factors
		}
		if len(result.Candidates) < limit {
			result.Candidates = append(result.Candidates, candidate)
		}
	}
	if selected := plan.Next(NextOptions{Sample: false, Now: opts.Now}); selected != nil {
		selection := &PreviewSelection{Model: selected.Entry.ID}
		if selected.Entry.RoutingGroup != "" {
			group := selected.Entry.RoutingGroup
			selection.Group = &group
		}
		result.Selection = selection
	}
	return result, nil
}
