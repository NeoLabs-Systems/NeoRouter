package media

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// The messages below are the ones the zod schemas of handlers.js produce for the
// same bodies (captured from the JS server); each is "<path>: <zod message>".

// decodeBodyJSON reads a request body the way the HTTP layer does.
func decodeBodyJSON(t *testing.T, document string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(document), &value); err != nil {
		t.Fatalf("decode %s: %v", document, err)
	}
	return value
}

// parseError is the message a schema's refusal carries.
func parseError(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var httpError *httpserver.HttpError
	if !errors.As(err, &httpError) {
		t.Fatalf("error %v is not a 400 INVALID_REQUEST", err)
	}
	if httpError.Status != 400 || httpError.Code != "INVALID_REQUEST" {
		t.Fatalf("error %d %s, want 400 INVALID_REQUEST", httpError.Status, httpError.Code)
	}
	return httpError.Message
}

func TestSchemasRefuseLikeZod(t *testing.T) {
	cases := []struct {
		name   string
		schema func(any) error
		body   string
		want   string
	}{
		// embeddings
		{"embeddings without input", embedErr, `{}`, "input: Invalid input"},
		{"embeddings input of another type", embedErr, `{"input":5}`, "input: Invalid input"},
		{"embeddings input null", embedErr, `{"input":null}`, "input: Invalid input"},
		{"embeddings empty list", embedErr, `{"input":[]}`, "input: Array must contain at least 1 element(s)"},
		{"embeddings empty text", embedErr, `{"input":[""]}`, "input.0: String must contain at least 1 character(s)"},
		{"embeddings text of another type", embedErr, `{"input":"x"}`, ""},
		{"embeddings list item of another type", embedErr, `{"input":["x",5]}`, "input: Invalid input"},
		{"embeddings model too long", embedErr, `{"input":"x","model":"` + strings.Repeat("a", 301) + `"}`, "model: String must contain at most 300 character(s)"},
		{"embeddings model null", embedErr, `{"input":"x","model":null}`, "model: Expected string, received null"},
		{"embeddings dimensions zero", embedErr, `{"input":"x","dimensions":0}`, "dimensions: Number must be greater than 0"},
		{"embeddings dimensions above the cap", embedErr, `{"input":"x","dimensions":9000}`, "dimensions: Number must be less than or equal to 8192"},
		{"embeddings dimensions fractional", embedErr, `{"input":"x","dimensions":1.5}`, "dimensions: Expected integer, received float"},
		{"embeddings dimensions of another type", embedErr, `{"input":"x","dimensions":"4"}`, "dimensions: Expected number, received string"},
		{"embeddings encoding format", embedErr, `{"input":"x","encoding_format":"x"}`, "encoding_format: Invalid enum value. Expected 'float' | 'base64', received 'x'"},
		{"embeddings encoding format of another type", embedErr, `{"input":"x","encoding_format":5}`, "encoding_format: Expected 'float' | 'base64', received number"},
		{"embeddings input type", embedErr, `{"input":"x","input_type":"x"}`, "input_type: Invalid enum value. Expected 'query' | 'passage' | 'document' | 'search_query' | 'search_document', received 'x'"},
		{"embeddings user", embedErr, `{"input":"x","user":5}`, "user: Expected string, received number"},
		{"embeddings too many inputs", embedErr, `{"input":` + listOfN(2049, `"a"`) + `}`, "input: Array must contain at most 2048 element(s)"},

		// images
		{"images without prompt", imageErr, `{}`, "prompt: Required"},
		{"images empty prompt", imageErr, `{"prompt":""}`, "prompt: String must contain at least 1 character(s)"},
		{"images prompt of another type", imageErr, `{"prompt":5}`, "prompt: Expected string, received number"},
		{"images n too large", imageErr, `{"prompt":"p","n":5}`, "n: Number must be less than or equal to 4"},
		{"images n too small", imageErr, `{"prompt":"p","n":0}`, "n: Number must be greater than or equal to 1"},
		{"images size", imageErr, `{"prompt":"p","size":"abc"}`, "size: Invalid"},
		{"images size of another type", imageErr, `{"prompt":"p","size":5}`, "size: Expected string, received number"},
		{"images quality", imageErr, `{"prompt":"p","quality":"` + strings.Repeat("q", 21) + `"}`, "quality: String must contain at most 20 character(s)"},
		{"images response format", imageErr, `{"prompt":"p","response_format":"x"}`, "response_format: Invalid enum value. Expected 'url' | 'b64_json', received 'x'"},

		// speech
		{"speech without input", speechErr, `{}`, "input: Required"},
		{"speech input null", speechErr, `{"input":null}`, "input: Expected string, received null"},
		{"speech input too long", speechErr, `{"input":"` + strings.Repeat("a", 4097) + `"}`, "input: String must contain at most 4096 character(s)"},
		{"speech format", speechErr, `{"input":"a","response_format":"ogg"}`, "response_format: Invalid enum value. Expected 'mp3' | 'opus' | 'aac' | 'flac' | 'wav' | 'pcm', received 'ogg'"},
		{"speech speed too slow", speechErr, `{"input":"a","speed":0.1}`, "speed: Number must be greater than or equal to 0.25"},
		{"speech speed too fast", speechErr, `{"input":"a","speed":5}`, "speed: Number must be less than or equal to 4"},
		{"speech speed of another type", speechErr, `{"input":"a","speed":"0.5"}`, "speed: Expected number, received string"},
		{"speech voice", speechErr, `{"input":"a","voice":"` + strings.Repeat("v", 61) + `"}`, "voice: String must contain at most 60 character(s)"},
		{"speech instructions", speechErr, `{"input":"a","instructions":"` + strings.Repeat("i", 4001) + `"}`, "instructions: String must contain at most 4000 character(s)"},

		// transcriptions
		{"transcription fields only", transcriptionErr, `{}`, ""},
		{"transcription temperature", transcriptionErr, `{"temperature":"abc"}`, "temperature: Expected number, received nan"},
		{"transcription temperature bound", transcriptionErr, `{"temperature":1.5}`, "temperature: Number must be less than or equal to 1"},
		{"transcription format", transcriptionErr, `{"response_format":"x"}`, "response_format: Invalid enum value. Expected 'json' | 'text' | 'srt' | 'verbose_json' | 'vtt', received 'x'"},
		{"transcription language", transcriptionErr, `{"language":"` + strings.Repeat("l", 21) + `"}`, "language: String must contain at most 20 character(s)"},
		{"transcription prompt", transcriptionErr, `{"prompt":"` + strings.Repeat("p", 4001) + `"}`, "prompt: String must contain at most 4000 character(s)"},

		// moderations
		{"moderations without input", moderationErr, `{}`, "input: Invalid input"},
		{"moderations input of another type", moderationErr, `{"input":5}`, "input: Invalid input"},
		{"moderations empty list", moderationErr, `{"input":[]}`, "input: Array must contain at least 1 element(s)"},
		{"moderations empty text", moderationErr, `{"input":[""]}`, "input.0: String must contain at least 1 character(s)"},
		{"moderations list of numbers", moderationErr, `{"input":[5]}`, "input: Invalid input"},
		{"moderations block without type", moderationErr, `{"input":[{"text":"x"}]}`, "input: Invalid input"},
		{"moderations block type of another type", moderationErr, `{"input":[{"type":5}]}`, "input: Invalid input"},
		{"moderations block text of another type", moderationErr, `{"input":[{"type":"text","text":5}]}`, "input: Invalid input"},
		{"moderations block", moderationErr, `{"input":[{"type":"text","extra":1}]}`, ""},
		{"moderations check", moderationErr, `{"input":"a","check":"x"}`, "check: Invalid enum value. Expected 'safety' | 'prompt_injection' | 'topic', received 'x'"},
		{"moderations policy", moderationErr, `{"input":"a","policy":"` + strings.Repeat("p", 20001) + `"}`, "policy: String must contain at most 20000 character(s)"},
		{"moderations too many inputs", moderationErr, `{"input":` + listOfN(33, `"a"`) + `}`, "input: Array must contain at most 32 element(s)"},

		// System One decisions
		{"decisions without questions", decisionErr, `{}`, "questions: Invalid input"},
		{"decisions with a text question", decisionErr, `{"questions":"x"}`, "questions: Invalid input"},
		{"decisions with state only", decisionErr, `{"state":{"a":1}}`, "questions: Invalid input"},
		{"decisions with a list", decisionErr, `{"questions":[]}`, ""},

		// videos
		{"videos without prompt", videoErr, `{}`, "prompt: Required"},
		{"videos seconds too short", videoErr, `{"prompt":"p","seconds":0}`, "seconds: Number must be greater than or equal to 1"},
		{"videos seconds too long", videoErr, `{"prompt":"p","seconds":61}`, "seconds: Number must be less than or equal to 60"},
		{"videos seconds null", videoErr, `{"prompt":"p","seconds":null}`, "seconds: Number must be greater than or equal to 1"},
		{"videos seconds as text", videoErr, `{"prompt":"p","seconds":"x"}`, "seconds: Expected number, received nan"},
		{"videos size", videoErr, `{"prompt":"p","size":"` + strings.Repeat("s", 21) + `"}`, "size: String must contain at most 20 character(s)"},

		// web search
		{"search without query", searchErr, `{}`, "query: Required"},
		{"search query null", searchErr, `{"query":null}`, "query: Expected string, received null"},
		{"search blank query", searchErr, `{"query":"  "}`, "query: String must contain at least 1 character(s)"},
		{"search long query", searchErr, `{"query":"` + strings.Repeat("q", 401) + `"}`, "query: String must contain at most 400 character(s)"},
		{"search max results too small", searchErr, `{"query":"a","max_results":0}`, "max_results: Number must be greater than or equal to 1"},
		{"search max results too large", searchErr, `{"query":"a","max_results":21}`, "max_results: Number must be less than or equal to 20"},
		{"search max results of another type", searchErr, `{"query":"a","max_results":"5"}`, "max_results: Expected number, received string"},
		{"search too many domains", searchErr, `{"query":"a","include_domains":` + listOfN(51, `"a.com"`) + `}`, "include_domains: Array must contain at most 50 element(s)"},
		{"search too many excluded domains", searchErr, `{"query":"a","exclude_domains":` + listOfN(51, `"a.com"`) + `}`, "exclude_domains: Array must contain at most 50 element(s)"},
		{"search long domain", searchErr, `{"query":"a","include_domains":["` + strings.Repeat("d", 254) + `"]}`, "include_domains.0: String must contain at most 253 character(s)"},
		{"search empty domain", searchErr, `{"query":"a","include_domains":[""]}`, "include_domains.0: String must contain at least 1 character(s)"},
		{"search recency", searchErr, `{"query":"a","recency":"hour"}`, "recency: Invalid enum value. Expected 'day' | 'week' | 'month' | 'year', received 'hour'"},

		// weather
		{"weather without location", weatherErr, `{}`, "location: Invalid input"},
		{"weather blank location", weatherErr, `{"location":""}`, "location: String must contain at least 1 character(s)"},
		{"weather long location", weatherErr, `{"location":"` + strings.Repeat("x", 201) + `"}`, "location: String must contain at most 200 character(s)"},
		{"weather location of another type", weatherErr, `{"location":5}`, "location: Invalid input"},
		{"weather location list", weatherErr, `{"location":[1,2]}`, "location: Invalid input"},
		{"weather latitude above the pole", weatherErr, `{"location":{"latitude":100,"longitude":0}}`, "location.latitude: Number must be less than or equal to 90"},
		{"weather longitude above the meridian", weatherErr, `{"location":{"latitude":1,"longitude":500}}`, "location.longitude: Number must be less than or equal to 180"},
		{"weather latitude of another type", weatherErr, `{"location":{"latitude":"x","longitude":0}}`, "location: Invalid input"},
		{"weather missing longitude", weatherErr, `{"location":{"latitude":1}}`, "location: Invalid input"},
		{"weather current of another type", weatherErr, `{"location":"P","current":"yes"}`, "current: Expected boolean, received string"},
		{"weather days too many", weatherErr, `{"location":"P","days":17}`, "days: Number must be less than or equal to 16"},
		{"weather days too few", weatherErr, `{"location":"P","days":0}`, "days: Number must be greater than or equal to 1"},
		{"weather units", weatherErr, `{"location":"P","units":"x"}`, "units: Invalid enum value. Expected 'metric' | 'imperial', received 'x'"},
		{"weather with nothing asked for", weatherErr, `{"location":"P","current":false,"hourly":false,"daily":false}`, "current: ask for current, hourly or daily weather"},

		// bodies that are not objects
		{"an array body", searchErr, `[]`, "body: Expected object, received array"},
		{"a string body", searchErr, `"hi"`, "body: Expected object, received string"},
		{"a number body", searchErr, `1`, "body: Expected object, received number"},
		{"a true body", searchErr, `true`, "body: Expected object, received boolean"},
		{"an absent body", searchErr, `null`, "query: Required"},
		{"an empty string body", searchErr, `""`, "query: Required"},
		{"a zero body", searchErr, `0`, "query: Required"},
		{"a false body", searchErr, `false`, "query: Required"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseError(t, testCase.schema(decodeBodyJSON(t, testCase.body)))
			if got != testCase.want {
				t.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestSchemasApplyDefaults(t *testing.T) {
	embedding, err := parseEmbeddingInput(decodeBodyJSON(t, `{"input":"x"}`))
	if err != nil {
		t.Fatalf("embeddings: %v", err)
	}
	if embedding.Model != "auto" || embedding.EncodingFormat != "float" || len(embedding.Input) != 1 {
		t.Fatalf("embeddings = %+v", embedding)
	}
	image, err := parseImageInput(decodeBodyJSON(t, `{"prompt":"p","size":"auto"}`))
	if err != nil {
		t.Fatalf("images: %v", err)
	}
	if image.N != 1 || image.ResponseFormat != "b64_json" || image.Size == nil || *image.Size != "auto" {
		t.Fatalf("images = %+v", image)
	}
	speech, err := parseSpeechInput(decodeBodyJSON(t, `{"input":"a"}`))
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if speech.Format != "mp3" || speech.Voice != nil || speech.Speed != nil {
		t.Fatalf("speech = %+v", speech)
	}
	transcription, err := parseTranscriptionInput(decodeBodyJSON(t, `{}`))
	if err != nil {
		t.Fatalf("transcription: %v", err)
	}
	if transcription.ResponseFormat != "json" || transcription.Model != "auto" {
		t.Fatalf("transcription = %+v", transcription)
	}
	moderation, err := parseModerationInput(decodeBodyJSON(t, `{"input":["a",{"type":"text","text":"b"},{"type":"image_url"}]}`))
	if err != nil {
		t.Fatalf("moderations: %v", err)
	}
	if moderation.Check != "safety" || len(moderation.Inputs) != 2 {
		t.Fatalf("moderations = %+v (a block without text is dropped, as `item.text || ''` is)", moderation)
	}
	video, err := parseVideoInput(decodeBodyJSON(t, `{"prompt":"p","seconds":"6"}`))
	if err != nil {
		t.Fatalf("videos: %v", err)
	}
	if video.Seconds != 6 {
		t.Fatalf("videos = %+v (a numeric string coerces)", video)
	}
	video, err = parseVideoInput(decodeBodyJSON(t, `{"prompt":"p","seconds":true}`))
	if err != nil {
		t.Fatalf("videos: %v", err)
	}
	if video.Seconds != 1 {
		t.Fatalf("videos = %+v (Number(true) is 1)", video)
	}
	search, err := parseSearchInput(decodeBodyJSON(t, `{"query":"  hi  "}`))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if search.Query != "hi" || search.MaxResults != 5 || search.IncludeDomains == nil || len(search.IncludeDomains) != 0 {
		t.Fatalf("search = %+v", search)
	}
	weather, err := parseWeatherInput(decodeBodyJSON(t, `{"location":" Paris "}`))
	if err != nil {
		t.Fatalf("weather: %v", err)
	}
	if weather.Location != "Paris" || !weather.Current || weather.Hourly || !weather.Daily || weather.Days != 3 || weather.Units != "metric" {
		t.Fatalf("weather = %+v", weather)
	}
	point, err := parseWeatherInput(decodeBodyJSON(t, `{"location":{"latitude":1.5,"longitude":-2.5}}`))
	if err != nil {
		t.Fatalf("weather coordinates: %v", err)
	}
	coords, ok := point.Location.(map[string]any)
	if !ok || coords["latitude"] != 1.5 || coords["longitude"] != -2.5 {
		t.Fatalf("coordinates = %+v", point.Location)
	}
}

// listOfN is a JSON array of n copies of item.
func listOfN(n int, item string) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for index := 0; index < n; index++ {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(item)
	}
	builder.WriteByte(']')
	return builder.String()
}

func embedErr(body any) error {
	_, err := parseEmbeddingInput(body)
	return err
}

func imageErr(body any) error {
	_, err := parseImageInput(body)
	return err
}

func speechErr(body any) error {
	_, err := parseSpeechInput(body)
	return err
}

func transcriptionErr(body any) error {
	_, err := parseTranscriptionInput(body)
	return err
}

func moderationErr(body any) error {
	_, err := parseModerationInput(body)
	return err
}

func decisionErr(body any) error {
	_, err := parseDecisionInput(body)
	return err
}

func videoErr(body any) error {
	_, err := parseVideoInput(body)
	return err
}

func searchErr(body any) error {
	_, err := parseSearchInput(body)
	return err
}

func weatherErr(body any) error {
	_, err := parseWeatherInput(body)
	return err
}
