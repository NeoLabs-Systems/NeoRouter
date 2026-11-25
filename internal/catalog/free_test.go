package catalog

import (
	"testing"
)

// classifyTest runs one classification against the fixture definitions.
func classifyTest(t *testing.T, definitionID string, mutate func(*ClassificationInput)) Classification {
	t.Helper()
	definition, ok := testDefinition(definitionID)
	if !ok {
		t.Fatalf("unknown definition %q", definitionID)
	}
	input := ClassificationInput{Definition: definition}
	if mutate != nil {
		mutate(&input)
	}
	return Classify(input)
}

func TestUnknownPriceIsNeverFree(t *testing.T) {
	result := classifyTest(t, "openai", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "gpt-x"
		in.Listing = Listing{PriceKnown: false}
	})
	if result.IsFree || result.Reason != ReasonPriceUnknown {
		t.Errorf("got %+v, want paid/price_unknown", result)
	}
}

func TestOpenRouterFreeModelsAreFreeEvenOnAPaidAccount(t *testing.T) {
	if result := classifyTest(t, "openrouter", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "meta-llama/llama-3.3-70b-instruct:free"
	}); !result.IsFree {
		t.Errorf(":free model is not free: %+v", result)
	}
	if result := classifyTest(t, "openrouter", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "openai/gpt-4o"
		in.Listing = Listing{PriceKnown: true}
	}); result.IsFree {
		t.Errorf("priced model is free: %+v", result)
	}
}

func TestFreeTierAccountsAreFreeOnlyWhereTheTierCannotCharge(t *testing.T) {
	cases := []struct {
		definition   string
		billingMode  string
		billingState string
		modelID      string
		want         bool
		wantReason   Reason
	}{
		{"groq", "free_tier", "free", "llama-3.1-8b-instant", true, ReasonFreeTierAccount},
		{"groq", "paid", "free", "llama-3.1-8b-instant", false, ReasonPriceUnknown},
		{"openai", "free_tier", "", "gpt-4o", false, ReasonPriceUnknown},
		// Credit trials and pay-per-token clouds are not free tiers.
		{"cerebras", "free_tier", "", "gpt-oss-120b", false, ReasonPriceUnknown},
		// No payment method exists on build.nvidia.com, so no verification is needed.
		{"nvidia", "free_tier", "", "meta/llama-3.3-70b-instruct", true, ReasonFreeTierAccount},
	}
	for _, testCase := range cases {
		result := classifyTest(t, testCase.definition, func(in *ClassificationInput) {
			in.BillingMode = testCase.billingMode
			in.BillingState = testCase.billingState
			in.ModelID = testCase.modelID
		})
		if result.IsFree != testCase.want || result.Reason != testCase.wantReason {
			t.Errorf("%s %s/%s = %+v, want free=%v reason=%s", testCase.definition, testCase.billingMode, testCase.modelID,
				result, testCase.want, testCase.wantReason)
		}
	}
}

func TestProvidersThatCanBillACardNeedAVerifiedFreeAccount(t *testing.T) {
	for _, testCase := range []struct {
		state  string
		reason Reason
		free   bool
	}{
		{"unknown", ReasonBillingUnverified, false},
		{"paid", ReasonBillingActive, false},
		{"free", ReasonFreeTierAccount, true},
	} {
		result := classifyTest(t, "groq", func(in *ClassificationInput) {
			in.BillingMode = "free_tier"
			in.BillingState = testCase.state
			in.ModelID = "openai/gpt-oss-20b"
		})
		if result.IsFree != testCase.free || result.Reason != testCase.reason {
			t.Errorf("billingState %q = %+v, want free=%v reason=%s", testCase.state, result, testCase.free, testCase.reason)
		}
	}
}

func TestPartialFreeTiersCoverOnlyTheirListedModels(t *testing.T) {
	google := func(modelID string) Classification {
		return classifyTest(t, "google", func(in *ClassificationInput) {
			in.BillingMode = "free_tier"
			in.BillingState = "free"
			in.ModelID = modelID
		})
	}
	if result := google("gemini-2.5-flash"); !result.IsFree {
		t.Errorf("gemini-2.5-flash: %+v", result)
	}
	if result := google("gemma-4-31b-it"); !result.IsFree {
		t.Errorf("gemma-4-31b-it: %+v", result)
	}
	if result := google("gemini-3.1-pro-preview"); result.IsFree || result.Reason != ReasonNotInFreeTier {
		t.Errorf("gemini-3.1-pro-preview: %+v", result)
	}
	if result := google("gemini-3.1-flash-image"); result.IsFree {
		t.Errorf("gemini-3.1-flash-image: %+v", result)
	}
	if result := google("gemini-4-argon"); result.IsFree {
		t.Errorf("gemini-4-argon: %+v", result)
	}
	ollamaCloud := func(modelID string) Classification {
		return classifyTest(t, "ollama_cloud", func(in *ClassificationInput) {
			in.BillingMode = "free_tier"
			in.ModelID = modelID
		})
	}
	if result := ollamaCloud("gpt-oss:120b"); !result.IsFree {
		t.Errorf("gpt-oss:120b: %+v", result)
	}
	if result := ollamaCloud("nemotron-3-super"); !result.IsFree {
		t.Errorf("nemotron-3-super: %+v", result)
	}
	if result := ollamaCloud("kimi-k2.6"); result.IsFree || result.Reason != ReasonNotInFreeTier {
		t.Errorf("kimi-k2.6: %+v", result)
	}
	if result := ollamaCloud("glm-5.3"); result.IsFree {
		t.Errorf("glm-5.3: %+v", result)
	}
}

func TestEvidenceFromTrafficBeatsEveryFreeRuleExceptAManualOverride(t *testing.T) {
	result := classifyTest(t, "openrouter", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x:free"
		in.Overrides = Overrides{BilledAt: "2026-10-28"}
	})
	if result.IsFree || result.Reason != ReasonBilledObserved {
		t.Errorf("billed evidence: %+v", result)
	}
	result = classifyTest(t, "openrouter", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x:free"
		in.Overrides = Overrides{BilledAt: "2026-10-28", Free: bp(true)}
	})
	if !result.IsFree {
		t.Errorf("manual free must win over billing evidence: %+v", result)
	}
	result = classifyTest(t, "google", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.BillingState = "free"
		in.ModelID = "gemini-2.5-pro"
		in.Overrides = Overrides{NoFreeTier: true}
	})
	if result.Reason != ReasonNoFreeTier {
		t.Errorf("learned no-free-tier: %+v", result)
	}
}

func TestListingPriceZeroCountsOnlyWhenTheListingIsTrusted(t *testing.T) {
	zero := Listing{FreeHint: true, PriceKnown: true}
	if result := classifyTest(t, "siliconflow", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x"
		in.Listing = zero
	}); !result.IsFree {
		t.Errorf("siliconflow zero listing: %+v", result)
	}
	if result := classifyTest(t, "together", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x"
		in.Listing = zero
	}); result.IsFree {
		t.Errorf("together zero listing is free: %+v", result)
	}
	// Together needs a prepaid purchase before any use, so even "-Free" names
	// are not free.
	if result := classifyTest(t, "together", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "Llama-3.3-70B-Instruct-Turbo-Free"
		in.Listing = zero
	}); result.IsFree {
		t.Errorf("together -Free name is free: %+v", result)
	}
}

func TestManualOverridesWin(t *testing.T) {
	if result := classifyTest(t, "openai", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x"
		in.Overrides = Overrides{Free: bp(true)}
	}); result.Reason != ReasonManualFree {
		t.Errorf("manual free: %+v", result)
	}
	if result := classifyTest(t, "openrouter", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x:free"
		in.Overrides = Overrides{Free: bp(false)}
	}); result.IsFree {
		t.Errorf("manual paid on a :free model: %+v", result)
	}
}

func TestConnectionWideChoiceSetsEveryModelButSpecificEvidenceWins(t *testing.T) {
	if result := classifyTest(t, "openai", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x"
		in.AccountOverride = "free"
	}); result.Reason != ReasonManualFree {
		t.Errorf("account free: %+v", result)
	}
	if result := classifyTest(t, "openrouter", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x:free"
		in.AccountOverride = "paid"
	}); result.IsFree {
		t.Error("a paid connection must beat a free-looking model")
	}
	if result := classifyTest(t, "openai", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x"
		in.AccountOverride = "paid"
		in.Overrides = Overrides{Free: bp(true)}
	}); !result.IsFree {
		t.Error("the model override is more specific than the connection")
	}
	if result := classifyTest(t, "openai", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x"
		in.AccountOverride = "free"
		in.Overrides = Overrides{BilledAt: 1761696000000.0}
	}); result.IsFree {
		t.Error("real billing beats a blanket free")
	}
	if result := classifyTest(t, "openai", func(in *ClassificationInput) {
		in.BillingMode = "paid"
		in.ModelID = "x"
		in.AccountOverride = "free"
		in.Listing = Listing{PaidOnly: true}
	}); result.IsFree {
		t.Error("a listing that needs paid credit beats a blanket free")
	}
}

func TestOnlyTheListedZaiFlashModelsAreFree(t *testing.T) {
	cases := map[string]bool{"glm-4.5-flash": true, "glm-5.3-flash": false, "glm-4.7-flashx": false, "glm-4.7-flash": true}
	for modelID, want := range cases {
		result := classifyTest(t, "zai", func(in *ClassificationInput) {
			in.BillingMode = "paid"
			in.ModelID = modelID
		})
		if result.IsFree != want {
			t.Errorf("%s = %+v, want free=%v", modelID, result, want)
		}
	}
}

func TestAuditedProvidersLocalOnlyAndKeylessOnlyFreeTiers(t *testing.T) {
	if result := classifyTest(t, "nous", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.ModelID = "some/model"
	}); result.IsFree {
		t.Errorf("nous free tier model is free: %+v", result)
	}
	if result := classifyTest(t, "huggingface", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.ModelID = "x"
	}); result.IsFree {
		t.Errorf("huggingface free tier model is free: %+v", result)
	}
	if result := classifyTest(t, "ollama", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.ModelID = "llama3"
		in.BaseURL = "http://192.168.1.5:11434/v1"
	}); !result.IsFree {
		t.Errorf("private Ollama host: %+v", result)
	}
	if result := classifyTest(t, "ollama", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.ModelID = "llama3"
		in.BaseURL = "https://ollama.com/v1"
	}); result.IsFree {
		t.Errorf("public Ollama host: %+v", result)
	}
	if result := classifyTest(t, "ovh", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.ModelID = "x"
	}); !result.IsFree {
		t.Errorf("keyless OVH: %+v", result)
	}
	if result := classifyTest(t, "ovh", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.ModelID = "x"
		in.HasSecret = true
	}); result.IsFree {
		t.Errorf("OVH with a key: %+v", result)
	}
}

func TestProvidersWithFreeModelsAreFreeWhenTheTierCoversThem(t *testing.T) {
	if result := classifyTest(t, "nous", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.ModelID = "hermes-5:free"
	}); !result.IsFree {
		t.Errorf("nous :free model: %+v", result)
	}
	if result := classifyTest(t, "nous", func(in *ClassificationInput) {
		in.BillingMode = "free_tier"
		in.ModelID = "hermes-5-405b"
		in.Listing = Listing{PriceKnown: true}
	}); result.IsFree {
		t.Errorf("priced nous model is free: %+v", result)
	}
}

func TestGoogleLiveModelsAreFreeOnlyOnceBillingIsRuledOut(t *testing.T) {
	for _, modelID := range []string{"gemini-3.8-live", "gemini-3.1-flash-live-preview", "gemini-2.5-flash-native-audio-latest",
		"gemini-3.5-transcribe-live", "gemini-3.5-live-translate-preview"} {
		for _, testCase := range []struct {
			state string
			want  Classification
		}{
			{"free", Classification{IsFree: true, Reason: ReasonFreeTierAccount}},
			{"paid", Classification{IsFree: false, Reason: ReasonBillingActive}},
			{"unknown", Classification{IsFree: false, Reason: ReasonBillingUnverified}},
		} {
			result := classifyTest(t, "google", func(in *ClassificationInput) {
				in.BillingMode = "free_tier"
				in.BillingState = testCase.state
				in.ModelID = modelID
			})
			if result != testCase.want {
				t.Errorf("%s with billing %s = %+v, want %+v", modelID, testCase.state, result, testCase.want)
			}
		}
	}
	for _, modelID := range []string{"gemini-robotics-er-2-streaming-preview", "gemini-3.1-pro-preview"} {
		if result := classifyTest(t, "google", func(in *ClassificationInput) {
			in.BillingMode = "free_tier"
			in.BillingState = "free"
			in.ModelID = modelID
		}); result.IsFree {
			t.Errorf("%s is free: %+v", modelID, result)
		}
	}
}

func TestAPoolPartnersModelStaysFreeWhateverIsOverridden(t *testing.T) {
	cases := []overrideCase{
		{Overrides{Free: bp(false)}, ""},
		{Overrides{}, "paid"},
		{Overrides{BilledAt: 1761696000000.0}, ""},
	}
	for _, testCase := range cases {
		result := classifyTest(t, "neorouter_peer", func(in *ClassificationInput) {
			in.BillingMode = "free_tier"
			in.ModelID = "x"
			in.Overrides = testCase.overrides
			in.AccountOverride = testCase.accountOverride
		})
		if !result.IsFree {
			t.Errorf("pool partner with %+v/%q = %+v, want free", testCase.overrides, testCase.accountOverride, result)
		}
	}
}

// overrideCase is one pool-partner classification input.
type overrideCase struct {
	overrides       Overrides
	accountOverride string
}

func TestPrivateURLDetection(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:11434/v1":   true,
		"http://127.0.0.1:8080/v1":    true,
		"http://10.1.2.3/v1":          true,
		"http://192.168.1.5:11434/v1": true,
		"http://172.16.0.1/v1":        true,
		"http://172.31.255.255/v1":    true,
		"http://172.32.0.1/v1":        false,
		"http://169.254.1.1/v1":       true,
		"https://ollama.com/v1":       false,
		"http://my-box.local/v1":      true,
		"http://[fd00::1]:8080/v1":    true,
		"not a url":                   false,
		"":                            false,
	}
	for url, want := range cases {
		if got := IsPrivateURL(url); got != want {
			t.Errorf("IsPrivateURL(%q) = %v, want %v", url, got, want)
		}
	}
}
