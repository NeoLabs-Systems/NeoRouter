// Package localmodels is NeoRouter's local model support: the catalog of models
// a server can run itself, the hardware it has to run them on, installing,
// starting and stopping one process per model, who may use them, and the entry
// point the router reaches running models through.
//
// It also serves the admin routes for them. Catalog data, memory and fit
// arithmetic, hardware thresholds, engine builds, state names, error texts and
// the JSON shape are what the admin page reads.
package localmodels

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Model kinds, the same values provider models use.
const (
	KindChat          = "chat"
	KindModeration    = "moderation"
	KindEmbedding     = "embedding"
	KindTranscription = "transcription"
	KindSpeech        = "speech"
)

// Engines a catalog entry can name.
const (
	EngineLlamaCpp     = "llama_cpp"
	EngineTransformers = "transformers"
)

// Text is an entry's summary in both languages of the dashboard.
type Text struct {
	En string `json:"en"`
	De string `json:"de"`
}

// Source is the Hugging Face repository an entry's files come from.
type Source struct {
	Repo string `json:"repo"`
	// Include lists the exact paths or * globs to fetch, resolved by HFFiles.
	Include []string `json:"include"`
	// Projector is llama.cpp's vision projector, which lets a text model read
	// images.
	Projector string `json:"projector,omitempty"`
}

// Features says what a model can do besides answering: the same capabilities
// the router lists for provider models.
type Features struct {
	Tools     bool `json:"tools"`
	Vision    bool `json:"vision"`
	Reasoning bool `json:"reasoning"`
	// Translate marks speech recognition that can also translate into English.
	Translate bool `json:"translate"`
}

// TransformersTask is the transformers.js task and weight precision a speech
// model loads with.
type TransformersTask struct {
	Task  string `json:"task"`
	Dtype string `json:"dtype,omitempty"`
}

// Voice is one voice a speech model offers: a file in the repository, or a
// single built-in one.
type Voice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	File string `json:"file,omitempty"`
}

// Entry is one model of the catalog. Entries are data only: where the files
// come from, which engine runs them, what they are good for. What a model needs
// from the machine is worked out from its size (fit.go), so adding a model is
// adding an entry.
type Entry struct {
	// ID is stable and is also the model id clients use (local/<id>): lower
	// case, digits, dots and dashes.
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Engine  string `json:"engine"`
	Summary Text   `json:"summary"`
	Source  Source `json:"source"`
	// DownloadMB is the download size (1 MB = 1024 × 1024 bytes, like memory
	// and disk), shown before the exact file list is fetched.
	DownloadMB float64 `json:"downloadMb"`
	// MemoryMB is what the model needs while running, where the size alone
	// says too little.
	MemoryMB float64 `json:"memoryMb,omitempty"`
	// Languages is "many" or the two-letter codes the model covers.
	Languages Languages `json:"languages"`
	License   string    `json:"license"`
	// Recommended marks a good first choice in its kind.
	Recommended bool     `json:"recommended"`
	Features    Features `json:"features"`
	// Transformers carries the pipeline task for engine "transformers".
	Transformers *TransformersTask `json:"transformers,omitempty"`
	// Voices are the voices of a speech model.
	Voices []Voice `json:"voices,omitempty"`
}

// Languages is either the word "many" (the JSON string) or two-letter codes.
type Languages struct {
	Many  bool
	Codes []string
}

// AllLanguages is entry data's `languages: 'many'`.
func AllLanguages() Languages { return Languages{Many: true} }

// CodesOf builds a Languages from two-letter codes.
func CodesOf(codes ...string) Languages { return Languages{Codes: codes} }

// MarshalJSON writes "many" or the codes array, as the dashboard expects.
func (l Languages) MarshalJSON() ([]byte, error) {
	if l.Many {
		return json.Marshal("many")
	}
	codes := l.Codes
	if codes == nil {
		codes = []string{}
	}
	return json.Marshal(codes)
}

// UnmarshalJSON reads either form.
func (l *Languages) UnmarshalJSON(data []byte) error {
	var word string
	if err := json.Unmarshal(data, &word); err == nil {
		l.Many = word == "many"
		l.Codes = nil
		return nil
	}
	var codes []string
	if err := json.Unmarshal(data, &codes); err != nil {
		return err
	}
	l.Many = false
	l.Codes = codes
	return nil
}

// Catalog returns every entry, in the order the dashboard lists them: by group
// in name order (embeddings, safety, speech, text, transcription).
func Catalog() []*Entry {
	out := make([]*Entry, len(catalogEntries))
	copy(out, catalogEntries)
	return out
}

// CatalogEntry looks one model up by id.
func CatalogEntry(id string) (*Entry, bool) {
	entry, ok := catalogIndex[id]
	return entry, ok
}

var catalogEntries = buildCatalog()

var catalogIndex = func() map[string]*Entry {
	index := make(map[string]*Entry, len(catalogEntries))
	for _, entry := range catalogEntries {
		index[entry.ID] = entry
	}
	return index
}()

// buildCatalog returns the catalog in its documented order. The entries are
// frozen literals: the entry rules (unique ids, one GGUF per llama.cpp model, a
// task for transformers models, voices for speech models, the id and repo
// patterns) is asserted by TestCatalogIsValid.
func buildCatalog() []*Entry {
	entries := []*Entry{}
	entries = append(entries, embeddingEntries()...)
	entries = append(entries, safetyEntries()...)
	entries = append(entries, speechEntries()...)
	entries = append(entries, textEntries()...)
	entries = append(entries, transcriptionEntries()...)
	return entries
}

// qwen35 is one Qwen3.5 chat model: a single GGUF plus the vision projector.
func qwen35(size string, downloadMB float64, summary Text, extra ...func(*Entry)) *Entry {
	entry := &Entry{
		ID:         "qwen3.5-" + strings.ToLower(size),
		Name:       "Qwen3.5 " + size,
		Kind:       KindChat,
		Engine:     EngineLlamaCpp,
		Summary:    summary,
		Source:     Source{Repo: "unsloth/Qwen3.5-" + size + "-GGUF", Include: []string{"Qwen3.5-" + size + "-Q4_K_M.gguf"}, Projector: "mmproj-F16.gguf"},
		DownloadMB: downloadMB,
		Languages:  AllLanguages(),
		License:    "Apache-2.0",
		Features:   Features{Tools: true, Vision: true, Reasoning: true},
	}
	for _, apply := range extra {
		apply(entry)
	}
	return entry
}

// gemma4 is one Gemma 4 chat model.
func gemma4(size, repoSize, file string, downloadMB float64, summary Text, extra ...func(*Entry)) *Entry {
	entry := &Entry{
		ID:         "gemma-4-" + strings.ReplaceAll(strings.ToLower(size), " ", "-"),
		Name:       "Gemma 4 " + size,
		Kind:       KindChat,
		Engine:     EngineLlamaCpp,
		Summary:    summary,
		Source:     Source{Repo: "unsloth/gemma-4-" + repoSize + "-it-GGUF", Include: []string{file}, Projector: "mmproj-F16.gguf"},
		DownloadMB: downloadMB,
		Languages:  AllLanguages(),
		License:    "Apache-2.0",
		Features:   Features{Tools: true, Vision: true, Reasoning: true},
	}
	for _, apply := range extra {
		apply(entry)
	}
	return entry
}

func recommended(entry *Entry) { entry.Recommended = true }

// textEntries are chat models (GGUF, run by llama.cpp). Q4_K_M keeps quality
// close to the original at about a third of the size; MoE models answer much
// faster than their size suggests.
func textEntries() []*Entry {
	return []*Entry{
		qwen35("0.8B", 704, Text{
			En: "Tiny and quick. Fine for short answers, sorting and simple tasks on almost any machine.",
			De: "Winzig und schnell. Reicht für kurze Antworten, Sortieren und einfache Aufgaben auf fast jedem Rechner.",
		}),
		qwen35("2B", 1859, Text{
			En: "Small all-rounder for everyday questions when memory is tight.",
			De: "Kleiner Allrounder für Alltagsfragen, wenn wenig Speicher frei ist.",
		}),
		qwen35("4B", 3255, Text{
			En: "Good everyday assistant: writes, summarises, uses tools and reads images.",
			De: "Guter Assistent für jeden Tag: schreibt, fasst zusammen, nutzt Werkzeuge und liest Bilder.",
		}, recommended),
		qwen35("9B", 6293, Text{
			En: "Noticeably smarter for longer texts, code and harder questions.",
			De: "Spürbar klüger bei längeren Texten, Code und schwierigeren Fragen.",
		}),
		gemma4("E2B", "E2B", "gemma-4-E2B-it-Q4_K_M.gguf", 3903, Text{
			En: "Google’s efficient small model, made for laptops and small servers.",
			De: "Googles sparsames kleines Modell, gemacht für Laptops und kleine Server.",
		}),
		gemma4("E4B", "E4B", "gemma-4-E4B-it-Q4_K_M.gguf", 5691, Text{
			En: "Google’s efficient model with more knowledge; strong in many languages.",
			De: "Googles sparsames Modell mit mehr Wissen; stark in vielen Sprachen.",
		}),
		gemma4("12B", "12b", "gemma-4-12b-it-Q4_K_M.gguf", 6959, Text{
			En: "Balanced and capable; a good main model for a server with a graphics card.",
			De: "Ausgewogen und fähig; ein gutes Hauptmodell für einen Server mit Grafikkarte.",
		}, recommended),
		gemma4("26B A4B", "26B-A4B", "gemma-4-26B-A4B-it-UD-Q4_K_M.gguf", 17301, Text{
			En: "Large but fast: only a small part of it works on each word.",
			De: "Groß, aber schnell: pro Wort arbeitet nur ein kleiner Teil davon.",
		}),
		gemma4("31B", "31B", "gemma-4-31B-it-Q4_K_M.gguf", 18619, Text{
			En: "Google’s largest open model, for demanding work on strong hardware.",
			De: "Googles größtes offenes Modell, für anspruchsvolle Arbeit auf starker Hardware.",
		}),
		{
			ID:     "qwen3.8-27b",
			Name:   "Qwen3.8 27B",
			Kind:   KindChat,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "Very capable for code, analysis and long documents.",
				De: "Sehr fähig bei Code, Analysen und langen Dokumenten.",
			},
			Source:     Source{Repo: "unsloth/Qwen3.8-27B-GGUF", Include: []string{"Qwen3.8-27B-UD-Q4_K_M.gguf"}, Projector: "mmproj-F16.gguf"},
			DownloadMB: 16586,
			Languages:  AllLanguages(),
			License:    "Apache-2.0",
			Features:   Features{Tools: true, Vision: true, Reasoning: true},
		},
		{
			ID:     "qwen3.6-35b-a3b",
			Name:   "Qwen3.6 35B A3B",
			Kind:   KindChat,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "Big knowledge, quick replies: only a small part of it works on each word.",
				De: "Viel Wissen, schnelle Antworten: pro Wort arbeitet nur ein kleiner Teil davon.",
			},
			Source:     Source{Repo: "unsloth/Qwen3.6-35B-A3B-GGUF", Include: []string{"Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"}, Projector: "mmproj-F16.gguf"},
			DownloadMB: 21967,
			Languages:  AllLanguages(),
			License:    "Apache-2.0",
			Features:   Features{Tools: true, Vision: true, Reasoning: true},
		},
		{
			ID:     "gpt-oss-20b",
			Name:   "gpt-oss 20B",
			Kind:   KindChat,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "OpenAI’s open model: thinks before it answers, good with tools.",
				De: "OpenAIs offenes Modell: denkt vor der Antwort nach, gut mit Werkzeugen.",
			},
			Source:     Source{Repo: "ggml-org/gpt-oss-20b-GGUF", Include: []string{"gpt-oss-20b-MXFP4.gguf"}},
			DownloadMB: 11549,
			Languages:  AllLanguages(),
			License:    "Apache-2.0",
			Features:   Features{Tools: true, Reasoning: true},
		},
		{
			ID:     "gpt-oss-120b",
			Name:   "gpt-oss 120B",
			Kind:   KindChat,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "OpenAI’s largest open model, for servers with a lot of memory.",
				De: "OpenAIs größtes offenes Modell, für Server mit sehr viel Speicher.",
			},
			Source:     Source{Repo: "ggml-org/gpt-oss-120b-GGUF", Include: []string{"gpt-oss-120b-MXFP4.gguf"}},
			DownloadMB: 60451,
			Languages:  AllLanguages(),
			License:    "Apache-2.0",
			Features:   Features{Tools: true, Reasoning: true},
		},
	}
}

// embeddingEntries are embedding models (GGUF, run by llama.cpp in embedding
// mode). Small, so they run well even on CPUs.
func embeddingEntries() []*Entry {
	return []*Entry{
		{
			ID:     "embeddinggemma-300m",
			Name:   "EmbeddingGemma 300M",
			Kind:   KindEmbedding,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "Tiny and accurate, for search over your own documents in many languages.",
				De: "Winzig und genau, für die Suche in eigenen Dokumenten in vielen Sprachen.",
			},
			Source:      Source{Repo: "ggml-org/embeddinggemma-300M-GGUF", Include: []string{"embeddinggemma-300M-Q8_0.gguf"}},
			DownloadMB:  314,
			Languages:   AllLanguages(),
			License:     "Gemma",
			Recommended: true,
		},
		{
			ID:     "qwen3-embedding-0.6b",
			Name:   "Qwen3 Embedding 0.6B",
			Kind:   KindEmbedding,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "Strong all-round embeddings that also handle long texts and code.",
				De: "Starke Embeddings für alles, auch für lange Texte und Code.",
			},
			Source:     Source{Repo: "Qwen/Qwen3-Embedding-0.6B-GGUF", Include: []string{"Qwen3-Embedding-0.6B-Q8_0.gguf"}},
			DownloadMB: 609,
			Languages:  AllLanguages(),
			License:    "Apache-2.0",
		},
		{
			ID:     "qwen3-embedding-4b",
			Name:   "Qwen3 Embedding 4B",
			Kind:   KindEmbedding,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "Top search quality when the server has memory to spare.",
				De: "Beste Suchqualität, wenn der Server genug Speicher frei hat.",
			},
			Source:     Source{Repo: "Qwen/Qwen3-Embedding-4B-GGUF", Include: []string{"Qwen3-Embedding-4B-Q4_K_M.gguf"}},
			DownloadMB: 2381,
			Languages:  AllLanguages(),
			License:    "Apache-2.0",
		},
		{
			ID:     "nomic-embed-text-v1.5",
			Name:   "Nomic Embed Text v1.5",
			Kind:   KindEmbedding,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "Very light English embeddings, fast even on small machines.",
				De: "Sehr leichte englische Embeddings, schnell auch auf kleinen Rechnern.",
			},
			Source:     Source{Repo: "nomic-ai/nomic-embed-text-v1.5-GGUF", Include: []string{"nomic-embed-text-v1.5.Q8_0.gguf"}},
			DownloadMB: 139,
			Languages:  CodesOf("en"),
			License:    "Apache-2.0",
		},
	}
}

// safetyEntries are System One checks (POST /v1/moderations), models that judge
// whether a text is safe instead of answering it. Their ids keep the family
// name, which is how System One knows how to ask them.
func safetyEntries() []*Entry {
	return []*Entry{
		{
			ID:     "llama-guard-3-1b",
			Name:   "Llama Guard 3 1B",
			Kind:   KindModeration,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "Small safety check: flags harmful requests and answers in a fraction of a second.",
				De: "Kleine Sicherheitsprüfung: erkennt schädliche Anfragen in Sekundenbruchteilen.",
			},
			Source:      Source{Repo: "RichardErkhov/alpindale_-_Llama-Guard-3-1B-gguf", Include: []string{"Llama-Guard-3-1B.Q4_K_M.gguf"}},
			DownloadMB:  911,
			Languages:   AllLanguages(),
			License:     "Llama 3.2 Community",
			Recommended: true,
		},
		{
			ID:     "llama-guard-3-8b",
			Name:   "Llama Guard 3 8B",
			Kind:   KindModeration,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "More careful safety check that catches more subtle cases.",
				De: "Gründlichere Sicherheitsprüfung, die auch feinere Fälle erkennt.",
			},
			Source:     Source{Repo: "mradermacher/Llama-Guard-3-8B-GGUF", Include: []string{"Llama-Guard-3-8B.Q4_K_M.gguf"}},
			DownloadMB: 4693,
			Languages:  AllLanguages(),
			License:    "Llama 3.1 Community",
		},
		{
			ID:     "gpt-oss-safeguard-20b",
			Name:   "gpt-oss-safeguard 20B",
			Kind:   KindModeration,
			Engine: EngineLlamaCpp,
			Summary: Text{
				En: "Follows your own written rules, e.g. which topics are allowed.",
				De: "Folgt deinen eigenen Regeln, z. B. welche Themen erlaubt sind.",
			},
			Source:     Source{Repo: "unsloth/gpt-oss-safeguard-20b-GGUF", Include: []string{"gpt-oss-safeguard-20b-Q4_K_M.gguf"}},
			DownloadMB: 11086,
			Languages:  AllLanguages(),
			License:    "Apache-2.0",
			Features:   Features{Reasoning: true},
		},
	}
}

// supertonicVoices is the ten built-in voices of the Supertonic TTS model.
func supertonicVoices() []Voice {
	ids := []string{"F1", "F2", "F3", "F4", "F5", "M1", "M2", "M3", "M4", "M5"}
	voices := make([]Voice, 0, len(ids))
	for _, id := range ids {
		gender := "Male"
		if id[0] == 'F' {
			gender = "Female"
		}
		voices = append(voices, Voice{
			ID:   strings.ToLower(id),
			Name: gender + " " + id[1:],
			File: "voices/" + id + ".bin",
		})
	}
	return voices
}

// mmsVoice is one Meta MMS voice: one per language, small, licensed for
// non-commercial use only.
func mmsVoice(language, code, name string, summary Text) *Entry {
	return &Entry{
		ID:           "mms-tts-" + code,
		Name:         "MMS " + name,
		Kind:         KindSpeech,
		Engine:       EngineTransformers,
		Summary:      summary,
		Source:       Source{Repo: "Xenova/mms-tts-" + code, Include: []string{"*.json", "onnx/model_quantized.onnx"}},
		DownloadMB:   37,
		Languages:    CodesOf(language),
		License:      "CC-BY-NC-4.0",
		Transformers: &TransformersTask{Task: "text-to-speech", Dtype: "q8"},
		Voices:       []Voice{{ID: "default", Name: name}},
	}
}

// speechEntries are text to speech (ONNX, run by the transformers engine).
func speechEntries() []*Entry {
	return []*Entry{
		{
			ID:     "supertonic-tts",
			Name:   "Supertonic",
			Kind:   KindSpeech,
			Engine: EngineTransformers,
			Summary: Text{
				En: "Natural English voices, ten to choose from, faster than real time on a CPU.",
				De: "Natürliche englische Stimmen, zehn zur Auswahl, schneller als Echtzeit auf einer CPU.",
			},
			Source:       Source{Repo: "onnx-community/Supertonic-TTS-ONNX", Include: []string{"*.json", "onnx/*", "voices/*.bin"}},
			DownloadMB:   251,
			Languages:    CodesOf("en"),
			License:      "OpenRAIL",
			Recommended:  true,
			Transformers: &TransformersTask{Task: "text-to-speech"},
			Voices:       supertonicVoices(),
		},
		mmsVoice("en", "eng", "English", Text{
			En: "Simple, very light English voice. Non-commercial use only.",
			De: "Einfache, sehr leichte englische Stimme. Nur für nicht-kommerzielle Nutzung.",
		}),
		mmsVoice("de", "deu", "Deutsch", Text{
			En: "Simple, very light German voice. Non-commercial use only.",
			De: "Einfache, sehr leichte deutsche Stimme. Nur für nicht-kommerzielle Nutzung.",
		}),
	}
}

// transcriptionEntries are speech to text (ONNX, run by the transformers
// engine). Quantised weights: a quarter of the size with nearly the same
// accuracy. Whisper's tokenizer also needs merges.txt.
func transcriptionEntries() []*Entry {
	shared := []string{"*.json", "*.txt"}
	whisper := func(id, name string, downloadMB float64, summary Text) *Entry {
		return &Entry{
			ID:           id,
			Name:         name,
			Kind:         KindTranscription,
			Engine:       EngineTransformers,
			Summary:      summary,
			Source:       Source{Repo: "onnx-community/" + id, Include: append(append([]string{}, shared...), "onnx/encoder_model_quantized.onnx", "onnx/decoder_model_merged_quantized.onnx")},
			DownloadMB:   downloadMB,
			Languages:    AllLanguages(),
			License:      "MIT",
			Transformers: &TransformersTask{Task: "automatic-speech-recognition", Dtype: "q8"},
			Features:     Features{Translate: true},
		}
	}
	small := whisper("whisper-small", "Whisper Small", 241, Text{
		En: "More accurate with accents and background noise; still quick.",
		De: "Genauer bei Akzenten und Hintergrundgeräuschen; trotzdem flott.",
	})
	small.Recommended = true
	return []*Entry{
		whisper("whisper-base", "Whisper Base", 78, Text{
			En: "Fast transcription in many languages; fine for clear recordings.",
			De: "Schnelle Transkription in vielen Sprachen; gut für klare Aufnahmen.",
		}),
		small,
		whisper("whisper-large-v3-turbo", "Whisper Large v3 Turbo", 1040, Text{
			En: "Best accuracy for meetings, interviews and difficult audio.",
			De: "Beste Genauigkeit für Meetings, Interviews und schwierige Aufnahmen.",
		}),
		{
			ID:     "moonshine-base",
			Name:   "Moonshine Base",
			Kind:   KindTranscription,
			Engine: EngineTransformers,
			Summary: Text{
				En: "Very fast English transcription for short voice messages and commands.",
				De: "Sehr schnelle englische Transkription für kurze Sprachnachrichten und Befehle.",
			},
			Source:       Source{Repo: "onnx-community/moonshine-base-ONNX", Include: []string{"*.json", "onnx/encoder_model_quantized.onnx", "onnx/decoder_model_merged_quantized.onnx"}},
			DownloadMB:   64,
			Languages:    CodesOf("en"),
			License:      "MIT",
			Transformers: &TransformersTask{Task: "automatic-speech-recognition", Dtype: "q8"},
		},
	}
}

// validateEntry is the schema of one catalog entry: TestCatalogIsValid runs it
// over the whole catalog.
func validateEntry(entry *Entry) error {
	if !validID(entry.ID) {
		return fmt.Errorf("id %q: must be a lower case id", entry.ID)
	}
	if entry.Name == "" {
		return fmt.Errorf("%s: needs a name", entry.ID)
	}
	switch entry.Kind {
	case KindChat, KindModeration, KindEmbedding, KindTranscription, KindSpeech:
	default:
		return fmt.Errorf("%s: unknown kind %q", entry.ID, entry.Kind)
	}
	switch entry.Engine {
	case EngineLlamaCpp, EngineTransformers:
	default:
		return fmt.Errorf("%s: unknown engine %q", entry.ID, entry.Engine)
	}
	if entry.Summary.En == "" || entry.Summary.De == "" {
		return fmt.Errorf("%s: needs a summary in both languages", entry.ID)
	}
	if !validRepo(entry.Source.Repo) {
		return fmt.Errorf("%s: repo %q is not owner/name", entry.ID, entry.Source.Repo)
	}
	if len(entry.Source.Include) == 0 {
		return fmt.Errorf("%s: needs at least one file to include", entry.ID)
	}
	for _, pattern := range entry.Source.Include {
		if pattern == "" {
			return fmt.Errorf("%s: empty include pattern", entry.ID)
		}
	}
	if entry.DownloadMB <= 0 {
		return fmt.Errorf("%s: downloadMb must be positive", entry.ID)
	}
	if entry.MemoryMB < 0 {
		return fmt.Errorf("%s: memoryMb must be positive", entry.ID)
	}
	if !entry.Languages.Many {
		if len(entry.Languages.Codes) == 0 {
			return fmt.Errorf("%s: languages must be many or a list", entry.ID)
		}
		for _, code := range entry.Languages.Codes {
			if len(code) != 2 {
				return fmt.Errorf("%s: language %q is not two letters", entry.ID, code)
			}
		}
	}
	if entry.License == "" {
		return fmt.Errorf("%s: needs a license", entry.ID)
	}
	if entry.Engine == EngineTransformers && entry.Transformers == nil {
		return fmt.Errorf("%s: transformers models need a task", entry.ID)
	}
	if entry.Transformers != nil {
		switch entry.Transformers.Task {
		case "automatic-speech-recognition", "text-to-speech":
		default:
			return fmt.Errorf("%s: unknown task %q", entry.ID, entry.Transformers.Task)
		}
	}
	if entry.Kind == KindSpeech && len(entry.Voices) == 0 {
		return fmt.Errorf("%s: speech models need voices", entry.ID)
	}
	return nil
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	for index, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '.' || r == '-') && index > 0:
		default:
			return false
		}
	}
	return true
}

// validRepo is the schema's /^[\w.-]+\/[\w.-]+$/. One slash, word characters,
// dots and dashes on both sides.
func validRepo(repo string) bool {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, r := range part {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			default:
				return false
			}
		}
	}
	return true
}
