package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// mtebAPI is the Hugging Face datasets-server, over the mteb/results dataset.
const mtebAPI = "https://datasets-server.huggingface.co/filter"

// mtebTasks are the fixed set of common English retrieval and similarity
// tasks: models are tested on very different task sets, so only these are
// averaged.
var mtebTasks = []string{"ArguAna", "SciFact", "NFCorpus", "FiQA2018", "SCIDOCS", "TRECCOVID",
	"STSBenchmark", "SICK-R", "STS12", "STS13", "STS14", "STS15", "STS16", "BIOSSES",
	"SprintDuplicateQuestions", "TwitterSemEval2015"}

// mtebMinTasks is how many of them a score needs.
const mtebMinTasks = 6

// mtebMaxAge keeps a score for a week; the service is occasionally flaky, so
// failures keep the previous score.
const mtebMaxAge = 7 * 24 * time.Hour

// mtebOrgs maps provider id prefixes onto the names models are published under
// on Hugging Face, in matching order.
var mtebOrgs = []struct{ prefix, org string }{
	{"gemini", "google"},
	{"text-embedding", "openai"},
	{"mistral-embed", "mistralai"},
	{"embed-", "Cohere"},
	{"arctic-embed", "Snowflake"},
	{"bge-", "BAAI"},
	{"nomic-embed", "nomic-ai"},
	{"e5-", "intfloat"},
	{"gte-", "Alibaba-NLP"},
}

// MTEBScore is one embedding model's MTEB average.
type MTEBScore struct {
	Score *float64 `json:"score"`
	Tasks int      `json:"tasks,omitempty"`
	Model string   `json:"model,omitempty"`
	At    int64    `json:"at,omitempty"`
}

type mtebRow struct {
	TaskName string   `json:"task_name"`
	Subset   string   `json:"subset"`
	Score    *float64 `json:"score"`
}

type mtebSource struct {
	baseURL string
	client  *http.Client
	// sleep is the retry delay between attempts.
	sleep func(time.Duration)
}

// mtebNames returns the Hugging Face names a provider model id may be
// published under.
func mtebNames(modelID string) []string {
	cleaned := strings.TrimPrefix(modelID, "@cf/")
	cleaned = strings.TrimSuffix(cleaned, ":free")
	parts := strings.Split(cleaned, "/")
	base := parts[len(parts)-1]
	parts = parts[:len(parts)-1]
	names := []string{}
	if len(parts) > 0 {
		names = append(names, parts[len(parts)-1]+"/"+base)
	}
	for _, org := range mtebOrgs {
		if strings.HasPrefix(base, org.prefix) || strings.Contains(base, org.prefix) {
			names = append(names, org.org+"/"+base)
		}
	}
	if strings.HasPrefix(base, "arctic-embed") {
		names = append(names, "Snowflake/snowflake-"+base)
	}
	return uniqueStrings(names)
}

// query asks the filter endpoint for one model's task scores. The index
// sometimes needs a moment to load, so it is retried.
func (s *mtebSource) query(ctx context.Context, modelName string) []mtebRow {
	conditions := make([]string, 0, len(mtebTasks))
	for _, task := range mtebTasks {
		conditions = append(conditions, `"task_name"='`+task+`'`)
	}
	where := `"model_name"='` + strings.ReplaceAll(modelName, "'", "''") + `' AND "split"='test' AND (` +
		strings.Join(conditions, " OR ") + `)`
	url := s.baseURL + "?dataset=mteb%2Fresults&config=default&split=train&length=100&where=" + encodeURIComponent(where)
	for attempt := 0; attempt < 4; attempt++ {
		rows, ok := s.fetchRows(ctx, url)
		if ok {
			return rows
		}
		if attempt < 3 {
			s.sleep(10 * time.Second)
		}
	}
	return nil
}

func (s *mtebSource) fetchRows(ctx context.Context, url string) ([]mtebRow, bool) {
	status, body, err := httpGet(ctx, s.client, url, nil)
	if err != nil || status < 200 || status > 299 {
		return nil, false
	}
	var data struct {
		Rows []struct {
			Row mtebRow `json:"row"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, false
	}
	rows := make([]mtebRow, 0, len(data.Rows))
	for _, item := range data.Rows {
		rows = append(rows, item.Row)
	}
	return rows, true
}

// mtebScoreOf is the mean over the fixed tasks (English subsets only), or nil.
func mtebScoreOf(rows []mtebRow) *MTEBScore {
	byTask := map[string][]float64{}
	order := []string{}
	for _, row := range rows {
		if row.Score == nil {
			continue
		}
		if row.Subset != "" && !containsString([]string{"default", "en", "en-en", "eng-Latn"}, row.Subset) {
			continue
		}
		if _, seen := byTask[row.TaskName]; !seen {
			order = append(order, row.TaskName)
		}
		byTask[row.TaskName] = append(byTask[row.TaskName], *row.Score)
	}
	if len(byTask) < mtebMinTasks {
		return nil
	}
	total := 0.0
	for _, task := range order {
		scores := byTask[task]
		sum := 0.0
		for _, score := range scores {
			sum += score
		}
		total += sum / float64(len(scores))
	}
	score := total / float64(len(order))
	return &MTEBScore{Score: &score, Tasks: len(order)}
}

// fetchScores refreshes the scores of the given embedding model ids and
// returns the updated map.
func (s *mtebSource) fetchScores(ctx context.Context, modelIDs []string, previous map[string]MTEBScore, now time.Time) map[string]MTEBScore {
	result := make(map[string]MTEBScore, len(previous)+len(modelIDs))
	for id, score := range previous {
		result[id] = score
	}
	for _, modelID := range modelIDs {
		if known, ok := result[modelID]; ok && now.Sub(time.Unix(known.At, 0)) < mtebMaxAge {
			continue
		}
		var found *MTEBScore
		for _, name := range mtebNames(modelID) {
			rows := s.query(ctx, name)
			if rows == nil {
				continue
			}
			score := mtebScoreOf(rows)
			if score != nil {
				score.Model = name
				score.At = now.UnixNano() / int64(time.Millisecond)
				found = score
				break
			}
		}
		if found == nil {
			found = &MTEBScore{At: now.UnixNano() / int64(time.Millisecond)}
		}
		result[modelID] = *found
	}
	return result
}

// mtebIntelligence maps an MTEB average onto the 0..1 scale: ~0.45 is weak
// today, ~0.75 is state of the art.
func mtebIntelligence(score float64) float64 {
	value := (score - 0.45) / 0.3
	return mathMin(1, mathMax(0.1, value))
}

// encodeURIComponent percent-encodes, leaving !'()*-_.~ alone and encoding a
// space as %20.
func encodeURIComponent(value string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out.WriteByte(c)
		case strings.IndexByte("!'()*-_.~", c) >= 0:
			out.WriteByte(c)
		default:
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&0x0f])
		}
	}
	return out.String()
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
