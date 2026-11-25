package processing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestCategoryOf pins the model's labels to the shield's categories.
func TestCategoryOf(t *testing.T) {
	want := map[string]string{
		"private_person":  "person",
		"private_address": "address",
		"private_email":   "email",
		"private_phone":   "phone",
		"account_number":  "id",
		"private_date":    "birthdate",
		"secret":          "secret",
	}
	if len(CategoryOf) != len(want) {
		t.Fatalf("CategoryOf has %d entries, want %d", len(CategoryOf), len(want))
	}
	for label, category := range want {
		if CategoryOf[label] != category {
			t.Errorf("%s is %q, want %q", label, CategoryOf[label], category)
		}
		if !KnownCategory(category) {
			t.Errorf("%s maps to the unknown shield category %s", label, category)
		}
	}
}

// TestTidy pins the rules that turn a model span into a value: the JavaScript uses
// two lookarounds here, so this is the port's proof that they still behave.
func TestTidy(t *testing.T) {
	cases := []struct {
		input    string
		value    string
		category string
		want     string
		why      string
	}{
		{"Konto …, IBAN DE89", "IB", "id", "IBAN", "a span that stops inside a word is widened to the word"},
		{"token 9xK2 here", "xK2", "secret", "9xK2", "a span that starts inside a word is widened to the word"},
		{"last month", "last month", "birthdate", "", "a date must hold a digit"},
		{"yesterday 10.18.4.27", "yesterday 10.18.4.27", "birthdate", "", "an IP address is not a date"},
		{"Am 12.03.1985 geboren", "12.03.1985 geboren", "birthdate", "12.03.1985", "the words after a date are dropped"},
		{"geboren am 3. Mai 1990 in Berlin", "geboren am 3. Mai 1990 in Berlin", "birthdate", "3. Mai 1990", "words before and after the date are dropped"},
		{"3. Mai 1990", "3. Mai 1990", "birthdate", "3. Mai 1990", "a date on its own stays"},
		{"mail jana@example.org now", "jana@example.org", "email", "jana@example.org", "the words around a value stay outside"},
		{"nichts dabei", "fehlt", "email", "", "a value that is not in the text is dropped"},
	}
	for _, test := range cases {
		if got := tidy(test.input, test.value, test.category); got != test.want {
			t.Errorf("tidy(%q, %q, %s) = %q, want %q (%s)", test.input, test.value, test.category, got, test.want, test.why)
		}
	}
}

// labelPipeline answers with fixed spans, like the model would.
type labelPipeline struct {
	raw      []RawEntity
	seen     []string
	disposed bool
}

func (p *labelPipeline) Classify(ctx context.Context, text string) ([]RawEntity, error) {
	p.seen = append(p.seen, text)
	return p.raw, nil
}

func (p *labelPipeline) Dispose() error {
	p.disposed = true
	return nil
}

// TestDetectAppliesTheLabelRules proves CATEGORY_OF, MIN_SCORE, the B-/I- prefix
// and MAX_CHARS are applied where the JavaScript applies them.
func TestDetectAppliesTheLabelRules(t *testing.T) {
	service, _ := testService(t)
	pipeline := &labelPipeline{raw: []RawEntity{
		{EntityGroup: "B-private_person", Word: "Johanna Krause", Score: 0.98},
		{EntityGroup: "private_email", Word: "jana@example.org", Score: 0.99},
		{EntityGroup: "private_phone", Word: "0170 1234567", Score: 0.2}, // below MIN_SCORE
		{EntityGroup: "private_url", Word: "example.org/x", Score: 0.99}, // no shield category
		{EntityGroup: "account_number", Word: "Q", Score: 0.99},          // too short: one character
	}}
	service.Privacy.SetLoader(func(ctx context.Context, progress func(float64)) (Pipeline, error) {
		return pipeline, nil
	})
	if _, err := service.Privacy.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer service.Privacy.SetEnabled(context.Background(), false)
	items, err := service.Privacy.Detect(context.Background(), "Lunch with Johanna Krause, mail jana@example.org, phone 0170 1234567, room Q")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, item := range items {
		got = append(got, item.Category+":"+item.Text)
	}
	want := []string{"person:Johanna Krause", "email:jana@example.org"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entities %v, want %v", got, want)
	}

	// MAX_CHARS clips what the model reads.
	long := strings.Repeat("a", PrivacyModelMaxChars+500)
	if _, err := service.Privacy.Detect(context.Background(), long); err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(pipeline.seen[len(pipeline.seen)-1])); got != PrivacyModelMaxChars {
		t.Fatalf("model read %d characters", got)
	}
}

// TestPrivacyStatusWireShape pins {enabled, model, memoryMb, status, progress,
// error}, the order and the nulls included.
func TestPrivacyStatusWireShape(t *testing.T) {
	service, _ := testService(t)
	encoded, err := json.Marshal(service.Privacy.Status(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"enabled":false,"model":"openai/privacy-filter","memoryMb":2048,"status":"off","progress":null,"error":null}`
	if string(encoded) != want {
		t.Fatalf("status %s, want %s", encoded, want)
	}
	// While loading, progress is a fraction and the error is empty.
	progress := 0.5
	message := "boom"
	service.Privacy.progress = &progress
	service.Privacy.errText = &message
	service.Privacy.status = "downloading"
	encoded, err = json.Marshal(service.Privacy.Status(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"progress":0.5`) || !strings.Contains(string(encoded), `"error":"boom"`) {
		t.Fatalf("status %s", encoded)
	}
}

// TestPrivacyModelResumeAndFailedLoad pins the start-up step and the error state.
func TestPrivacyModelResumeAndFailedLoad(t *testing.T) {
	service, _ := testService(t)
	// Off: resume does nothing.
	service.Privacy.Resume(context.Background())
	if status := service.Privacy.Status(context.Background()); status.Status != "off" {
		t.Fatalf("resume with the filter off: %s", status.Status)
	}
	// On: resume goes to idle and tries to load (the model is not installed here,
	// so the load fails and is recorded).
	if _, err := service.Privacy.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer service.Privacy.SetEnabled(context.Background(), false)
	service.Privacy.SetLoader(func(ctx context.Context, progress func(float64)) (Pipeline, error) {
		return nil, errPrivacyNotInstalled
	})
	if _, err := service.Privacy.Detect(context.Background(), "some text to classify"); err == nil {
		t.Fatal("a failing load is an error")
	}
	status := service.Privacy.Status(context.Background())
	if status.Status != "error" || status.Error == nil || *status.Error == "" {
		t.Fatalf("status after a failed load: %+v", status)
	}
	// Turning it off clears the state.
	if _, err := service.Privacy.SetEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if status := service.Privacy.Status(context.Background()); status.Status != "off" {
		t.Fatalf("status after turning it off: %s", status.Status)
	}
}

var errPrivacyNotInstalled = privacyError("the model is not installed")

type privacyError string

func (e privacyError) Error() string { return string(e) }
