package media

import (
	"fmt"
	"regexp"
	"strings"
)

// The request bodies of the media endpoints, validated in the order their zod
// schema declares them (server/capabilities/media/handlers.js).

// embeddingInput is one /v1/embeddings request.
type embeddingInput struct {
	Model          string
	Input          []string
	Dimensions     *int
	EncodingFormat string
	// InputType is the retrieval hint: passage-ish values embed as documents,
	// everything else as a query.
	InputType *string
}

// imageInput is one /v1/images/generations request.
type imageInput struct {
	Model          string
	Prompt         string
	N              int
	Size           *string
	Quality        *string
	ResponseFormat string
}

// speechInput is one /v1/audio/speech request.
type speechInput struct {
	Model        string
	Input        string
	Voice        *string
	Format       string
	Speed        *float64
	Instructions *string
}

// transcriptionInput is one /v1/audio/transcriptions request.
type transcriptionInput struct {
	Model          string
	Language       *string
	Prompt         *string
	ResponseFormat string
}

// moderationInput is one /v1/moderations request.
type moderationInput struct {
	Model  string
	Inputs []string
	Check  string
	Policy *string
}

// decisionInput is one /v1/systemone request.
type decisionInput struct {
	Model     string
	State     any
	Questions any
}

// videoInput is one POST /v1/videos request.
type videoInput struct {
	Model   string
	Prompt  string
	Seconds int
	Size    *string
}

// SearchInput is one web search: the /v1/search body, or what chat's
// web-search tool asks for.
type SearchInput struct {
	Model          string
	Query          string
	MaxResults     int
	IncludeDomains []string
	ExcludeDomains []string
	Recency        *string
}

// weatherInput is one /v1/weather request.
type weatherInput struct {
	Model    string
	Location any
	Current  bool
	Hourly   bool
	Daily    bool
	Days     int
	Units    string
}

var imageSizePattern = regexp.MustCompile(`^(\d{2,4}x\d{2,4}|auto)$`)

// parseEmbeddingInput reads the embedding schema.
func parseEmbeddingInput(body any) (embeddingInput, error) {
	f := newFields(body)
	in := embeddingInput{Model: f.textOr("model", 0, 300, "auto")}
	in.Input = parseEmbeddingTexts(f)
	if dimensions := f.positive("dimensions", 8192); dimensions != nil {
		value := int(*dimensions)
		in.Dimensions = &value
	}
	in.EncodingFormat = f.enumOr("encoding_format", []string{"float", "base64"}, "float")
	in.InputType = f.enum("input_type", []string{"query", "passage", "document", "search_query", "search_document"})
	// The user field is accepted and dropped, as the JS schema does.
	_ = f.str("user")
	if err := f.err(); err != nil {
		return embeddingInput{}, err
	}
	return in, nil
}

// parseEmbeddingTexts is the input union: one text, or a non-empty list of them.
func parseEmbeddingTexts(f *fields) []string {
	value, present := f.optional("input")
	if !present {
		f.fail(&issue{path: "input", message: "Invalid input"})
		return nil
	}
	if text, ok := value.(string); ok {
		if found := textIssue("input", text, 1, 0); found != nil {
			f.fail(found)
			return nil
		}
		return []string{text}
	}
	list, ok := value.([]any)
	if !ok {
		f.fail(&issue{path: "input", message: "Invalid input"})
		return nil
	}
	if found := arrayIssue("input", len(list), 1, maxEmbeddingInputs); found != nil {
		f.fail(found)
		return nil
	}
	texts := make([]string, 0, len(list))
	for index, item := range list {
		text, isText := item.(string)
		if !isText {
			// An item of another type fails the union, not just the item.
			f.fail(&issue{path: "input", message: "Invalid input"})
			return nil
		}
		if found := textIssue(fmt.Sprintf("input.%d", index), text, 1, 0); found != nil {
			f.fail(found)
			return nil
		}
		texts = append(texts, text)
	}
	return texts
}

// parseImageInput reads the image generation schema.
func parseImageInput(body any) (imageInput, error) {
	f := newFields(body)
	in := imageInput{Model: f.textOr("model", 0, 300, "auto")}
	in.Prompt = f.script("prompt", 1, 32000)
	if count := f.count("n", 1, 4); count != nil {
		in.N = int(*count)
	} else {
		in.N = 1
	}
	if size := f.pattern("size", imageSizePattern); size != "" {
		in.Size = &size
	}
	in.Quality = f.text("quality", 0, 20)
	in.ResponseFormat = f.enumOr("response_format", []string{"url", "b64_json"}, "b64_json")
	_ = f.str("user")
	if err := f.err(); err != nil {
		return imageInput{}, err
	}
	return in, nil
}

// parseSpeechInput reads the speech schema.
func parseSpeechInput(body any) (speechInput, error) {
	f := newFields(body)
	in := speechInput{Model: f.textOr("model", 0, 300, "auto")}
	in.Input = f.script("input", 1, 4096)
	in.Voice = f.text("voice", 0, 60)
	in.Format = f.enumOr("response_format", []string{"mp3", "opus", "aac", "flac", "wav", "pcm"}, "mp3")
	in.Speed = f.number("speed", 0.25, 4)
	in.Instructions = f.text("instructions", 0, 4000)
	if err := f.err(); err != nil {
		return speechInput{}, err
	}
	return in, nil
}

// parseTranscriptionInput reads the transcription schema (the multipart fields,
// or the playground's JSON members beside the audio).
func parseTranscriptionInput(body any) (transcriptionInput, error) {
	f := newFields(body)
	in := transcriptionInput{Model: f.textOr("model", 0, 300, "auto")}
	in.Language = f.text("language", 0, 20)
	in.Prompt = f.text("prompt", 0, 4000)
	in.ResponseFormat = f.enumOr("response_format", []string{"json", "text", "srt", "verbose_json", "vtt"}, "json")
	// Temperature is accepted and dropped, as the JS schema does: no
	// transcription provider here takes one.
	_ = f.coerce("temperature", 0, 1)
	if err := f.err(); err != nil {
		return transcriptionInput{}, err
	}
	return in, nil
}

// parseModerationInput reads the moderation schema.
func parseModerationInput(body any) (moderationInput, error) {
	f := newFields(body)
	in := moderationInput{Model: f.textOr("model", 0, 300, "auto")}
	in.Inputs = parseModerationInputs(f)
	in.Check = f.enumOr("check", []string{"safety", "prompt_injection", "topic"}, "safety")
	in.Policy = f.text("policy", 0, 20000)
	if err := f.err(); err != nil {
		return moderationInput{}, err
	}
	return in, nil
}

// parseModerationInputs is the input union: one text, or a list of texts and
// OpenAI-shaped {type, text} blocks.
func parseModerationInputs(f *fields) []string {
	value, present := f.optional("input")
	if !present {
		f.fail(&issue{path: "input", message: "Invalid input"})
		return nil
	}
	if text, ok := value.(string); ok {
		if found := textIssue("input", text, 1, 0); found != nil {
			f.fail(found)
			return nil
		}
		return []string{text}
	}
	list, ok := value.([]any)
	if !ok {
		f.fail(&issue{path: "input", message: "Invalid input"})
		return nil
	}
	if found := arrayIssue("input", len(list), 1, 32); found != nil {
		f.fail(found)
		return nil
	}
	texts := make([]string, 0, len(list))
	for index, item := range list {
		switch block := item.(type) {
		case string:
			if found := textIssue(fmt.Sprintf("input.%d", index), block, 1, 0); found != nil {
				f.fail(found)
				return nil
			}
			texts = append(texts, block)
		case map[string]any:
			if _, ok := block["type"].(string); !ok {
				f.fail(&issue{path: "input", message: "Invalid input"})
				return nil
			}
			text := ""
			if raw, present := block["text"]; present && raw != nil {
				value, isText := raw.(string)
				if !isText {
					f.fail(&issue{path: "input", message: "Invalid input"})
					return nil
				}
				text = value
			}
			if text != "" {
				texts = append(texts, text)
			}
		default:
			f.fail(&issue{path: "input", message: "Invalid input"})
			return nil
		}
	}
	return texts
}

// parseDecisionInput reads the System One decision schema. State is free-form
// (z.unknown()) and questions are an object of them or a list.
func parseDecisionInput(body any) (decisionInput, error) {
	f := newFields(body)
	in := decisionInput{Model: f.textOr("model", 0, 300, "auto")}
	if state, present := f.members["state"]; present {
		in.State = state
	}
	switch questions := f.members["questions"].(type) {
	case map[string]any:
		in.Questions = questions
	case []any:
		in.Questions = questions
	default:
		f.fail(&issue{path: "questions", message: "Invalid input"})
	}
	if err := f.err(); err != nil {
		return decisionInput{}, err
	}
	return in, nil
}

// parseVideoInput reads the video creation schema.
func parseVideoInput(body any) (videoInput, error) {
	f := newFields(body)
	in := videoInput{Model: f.textOr("model", 0, 300, "auto")}
	in.Prompt = f.script("prompt", 1, 32000)
	in.Seconds = f.coerceInt("seconds", 1, 60, 4)
	if size := f.text("size", 0, 20); size != nil {
		in.Size = size
	}
	if err := f.err(); err != nil {
		return videoInput{}, err
	}
	return in, nil
}

// parseSearchInput reads the web search schema.
func parseSearchInput(body any) (SearchInput, error) {
	f := newFields(body)
	in := SearchInput{Model: f.textOr("model", 0, 300, "auto")}
	in.Query = f.trimmed("query", 1, 400)
	if count := f.count("max_results", 1, 20); count != nil {
		in.MaxResults = int(*count)
	} else {
		in.MaxResults = 5
	}
	in.IncludeDomains = f.texts("include_domains", 1, 253, 0, 50)
	in.ExcludeDomains = f.texts("exclude_domains", 1, 253, 0, 50)
	in.Recency = f.enum("recency", []string{"day", "week", "month", "year"})
	if err := f.err(); err != nil {
		return SearchInput{}, err
	}
	return in, nil
}

// parseWeatherInput reads the weather schema, including the refinement that at
// least one section is asked for.
func parseWeatherInput(body any) (weatherInput, error) {
	f := newFields(body)
	in := weatherInput{Model: f.textOr("model", 0, 300, "auto")}
	in.Location = parseWeatherLocation(f)
	in.Current = f.flag("current", true)
	in.Hourly = f.flag("hourly", false)
	in.Daily = f.flag("daily", true)
	if days := f.count("days", 1, 16); days != nil {
		in.Days = int(*days)
	} else {
		in.Days = 3
	}
	in.Units = f.enumOr("units", []string{"metric", "imperial"}, "metric")
	if f.issue == nil && !in.Current && !in.Hourly && !in.Daily {
		f.fail(&issue{path: "current", message: "ask for current, hourly or daily weather", dirty: true})
	}
	if err := f.err(); err != nil {
		return weatherInput{}, err
	}
	return in, nil
}

// parseWeatherLocation is the location union: a place name, or coordinates.
func parseWeatherLocation(f *fields) any {
	value, present := f.optional("location")
	if !present {
		f.fail(&issue{path: "location", message: "Invalid input"})
		return nil
	}
	if text, ok := value.(string); ok {
		place := strings.TrimSpace(text)
		if found := textIssue("location", place, 1, 200); found != nil {
			f.fail(found)
			return nil
		}
		return place
	}
	point, ok := value.(map[string]any)
	if !ok {
		f.fail(&issue{path: "location", message: "Invalid input"})
		return nil
	}
	latitude, found := coordinate("location.latitude", point, "latitude", -90, 90)
	if found != nil {
		f.fail(found)
		return nil
	}
	longitude, found := coordinate("location.longitude", point, "longitude", -180, 180)
	if found != nil {
		f.fail(found)
		return nil
	}
	return map[string]any{"latitude": latitude, "longitude": longitude}
}

// coordinate reads one required latitude or longitude: a number in range. A
// member that is missing or not a number makes the object branch invalid for its
// type, which the union reports as its own "Invalid input".
func coordinate(path string, point map[string]any, name string, minValue, maxValue float64) (float64, *issue) {
	raw, present := point[name]
	if !present || raw == nil {
		return 0, &issue{path: "location", message: "Invalid input"}
	}
	number, ok := raw.(float64)
	if !ok {
		return 0, &issue{path: "location", message: "Invalid input"}
	}
	if found := rangeIssue(path, number, minValue, maxValue); found != nil {
		return 0, found
	}
	return number, nil
}
