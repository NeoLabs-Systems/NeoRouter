package routing

import (
	"math"
	"testing"
	"time"
)

// A fixed catalog (one paid custom_openai connection with three models, one of
// them without metadata) with exact expected numbers, so a change in the scoring
// formula, the relative blend, the task classification or the weights shows up
// here.
func TestPreviewScoresAFixedCatalog(t *testing.T) {
	f := newFixture(t)
	f.account("a", "u", "custom_openai", 0, "paid")
	mk := func(model string, priceIn, priceOut float64, intelligence *float64) {
		f.entry(model, "a", func(e *Entry) {
			e.DefinitionID = "custom_openai"
			e.BillingMode = "paid"
			e.PriceIn = floatPtr(priceIn)
			e.PriceOut = floatPtr(priceOut)
			e.Intelligence = intelligence
			if intelligence != nil {
				e.IntelligenceSource = "test"
			}
		})
	}
	mk("fast-cheap", 0.2, 0.8, nil)
	mk("smart-pricey", 5, 20, floatPtr(0.8))
	mk("mid", 1, 2, floatPtr(0.5))

	key := RouterKey{ID: "k", UserID: "u", Mode: "both", BudgetPeriod: "total", AllowedModelsJSON: "[]", Profile: "balanced"}
	result, err := f.svc.Preview(PreviewOptions{
		UserID:         "u",
		Key:            key,
		RequestedModel: "auto",
		Request:        planRequest("hello"),
		InputTokens:    1000,
		Limit:          20,
		Now:            time.Now(),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if result.Group != nil {
		t.Fatalf("group = %v, want null", *result.Group)
	}
	if result.Profile == nil || result.Profile.ID != "balanced" {
		t.Fatalf("profile = %v", result.Profile)
	}
	if result.Complexity == nil || *result.Complexity != 0.1 {
		t.Fatalf("complexity = %v, want 0.1", result.Complexity)
	}
	if result.Need == nil || *result.Need != 0.365 {
		t.Fatalf("need = %v, want 0.365", result.Need)
	}
	if result.Task == nil || result.Task.Primary != "chat" {
		t.Fatalf("task = %+v", result.Task)
	}
	wantWeights := Weights{
		Intelligence: 0.23333333333333334,
		Speed:        0.2,
		Reliability:  0.3,
		Cost:         0.26666666666666666,
	}
	if diff := math.Abs(result.Weights.Intelligence-wantWeights.Intelligence) +
		math.Abs(result.Weights.Speed-wantWeights.Speed) +
		math.Abs(result.Weights.Reliability-wantWeights.Reliability) +
		math.Abs(result.Weights.Cost-wantWeights.Cost); diff > 1e-12 {
		t.Fatalf("weights = %+v, want %+v", result.Weights, wantWeights)
	}

	want := []struct {
		id           string
		score        float64
		intelligence float64
		speed        float64
		reliability  float64
		cost         float64
	}{
		{"a/fast-cheap", 0.608622838331328, 0.43, 0.5, 0.6666666666666666, 0.8627722806023504},
		{"a/mid", 0.3495635585859631, 0.5666666666666667, 0.5, 0.6666666666666666, 0.5752668328121078},
		{"a/smart-pricey", 0.27006987002566424, 0.9766666666666666, 0.5, 0.6666666666666666, 0.1357736246339164},
	}
	if len(result.Candidates) != len(want) {
		t.Fatalf("candidates = %d, want %d: %+v", len(result.Candidates), len(want), result.Candidates)
	}
	for index, expected := range want {
		candidate := result.Candidates[index]
		if candidate.ID != expected.id {
			t.Fatalf("candidate %d = %s, want %s", index, candidate.ID, expected.id)
		}
		if candidate.Rejected != nil {
			t.Fatalf("%s rejected: %s", candidate.ID, *candidate.Rejected)
		}
		if candidate.Score == nil || math.Abs(*candidate.Score-expected.score) > 1e-12 {
			t.Fatalf("%s score = %v, want %v", candidate.ID, candidate.Score, expected.score)
		}
		features := candidate.Features
		if features == nil {
			t.Fatalf("%s has no features", candidate.ID)
		}
		for name, got := range map[string]float64{
			"intelligence": features.Intelligence,
			"speed":        features.Speed,
			"reliability":  features.Reliability,
			"cost":         features.Cost,
		} {
			var expectedValue float64
			switch name {
			case "intelligence":
				expectedValue = expected.intelligence
			case "speed":
				expectedValue = expected.speed
			case "reliability":
				expectedValue = expected.reliability
			case "cost":
				expectedValue = expected.cost
			}
			if math.Abs(got-expectedValue) > 1e-12 {
				t.Fatalf("%s %s = %v, want %v", candidate.ID, name, got, expectedValue)
			}
		}
		factors := candidate.Factors
		if factors == nil || factors.Quota != 1 || factors.Penalty != 1 || factors.Load != 1 {
			t.Fatalf("%s factors = %+v", candidate.ID, factors)
		}
		if candidate.Tier != 0 {
			t.Fatalf("%s tier = %d", candidate.ID, candidate.Tier)
		}
	}
	if result.Selection == nil || result.Selection.Model != "a/fast-cheap" || result.Selection.Group != nil {
		t.Fatalf("selection = %+v", result.Selection)
	}
}
