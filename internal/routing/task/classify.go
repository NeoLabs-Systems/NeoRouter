package task

import (
	"math"
	"sort"
	"strings"
)

func minf(a, b float64) float64    { return math.Min(a, b) }
func mathMin(a, b float64) float64 { return math.Min(a, b) }
func mathMax(a, b float64) float64 { return math.Max(a, b) }

func indexByte(s string, b byte) int { return strings.IndexByte(s, b) }

// clamp01 is Math.max(0, Math.min(1, value)).
func clamp01(value float64) float64 { return math.Max(0, math.Min(1, value)) }

// logScale maps a value onto 0..1 on a log scale; ok is false where the Node
// code's logScale returns null (not finite, or not positive).
func logScale(value, low, high float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, false
	}
	return clamp01((math.Log(value) - math.Log(low)) / (math.Log(high) - math.Log(low))), true
}

// lengthRange is the length band difficultyOf scales the ask length with.
var lengthRange = [2]float64{8, 3000}

// What a chat request asks for: a mix of task kinds (kinds.go), the skills
// that mix calls on, and how hard it looks.
const minKindScore = 0.15

// How hard a request looks, from the dimensions of NVIDIA's prompt complexity
// classifier that rules can see: how long the ask is, what kind of task it is,
// words of hard work, how many conditions the answer has to meet and how
// specialist the vocabulary is. Conditions and vocabulary only add to the first
// three, so a plain ask keeps its level. A passage pasted after the ask makes
// it longer to read, not harder to answer.
var difficultyWeights = []struct {
	name   string
	weight float64
}{
	{"length", 0.35},
	{"kind", 0.4},
	{"hard", 0.25},
	{"constraints", 0.1},
	{"jargon", 0.1},
}

// scoredKind is one kind with its score.
type scoredKind struct {
	id    string
	score float64
}

func kindByID(id string) *Kind {
	for i := range Kinds {
		if Kinds[i].ID == id {
			return &Kinds[i]
		}
	}
	return nil
}

func difficultyOf(signals *Signals, ranked []scoredKind) float64 {
	length := 0.0
	if scaled, ok := logScale(math.Max(1, math.Ceil(float64(utf16Len(signals.Ask))/4)+float64(utf16Len(signals.Passage))/40), lengthRange[0], lengthRange[1]); ok {
		length = scaled
	}
	scored := make([]scoredKind, 0, len(ranked))
	for _, item := range ranked {
		if item.id != "chat" {
			scored = append(scored, item)
		}
	}
	total := 0.0
	for _, item := range scored {
		total += item.score
	}
	kindPrior := 0.3
	if total != 0 {
		sum := 0.0
		for _, item := range scored {
			if kind := kindByID(item.id); kind != nil {
				sum += item.score * kind.Difficulty
			}
		}
		kindPrior = sum / total
	}
	parts := map[string]float64{
		"length":      length,
		"kind":        kindPrior,
		"hard":        saturate(float64(hardWords.hits(signals.Ask)), 2),
		"constraints": saturate(float64(signals.Constraints)+float64(signals.Examples)/2, 5),
		// Specialist words per word of the ask, saturating at one in six.
		"jargon": 0,
	}
	if signals.AskWords != 0 {
		parts["jargon"] = math.Min(1, (6*float64(signals.Jargon))/float64(signals.AskWords))
	}
	deep := 0.0
	if signals.Turns > 8 {
		deep = 0.1
	}
	value := deep
	for _, part := range difficultyWeights {
		value += part.weight * parts[part.name]
	}
	// Small talk stays easy whatever else the request carries.
	if len(ranked) > 0 && ranked[0].id == "chat" && ranked[0].score >= 1 {
		return math.Min(value, 0.1)
	}
	return clamp01(value)
}

// Result is what Classify returns.
type Result struct {
	Primary    string
	Kinds      *orderedMap
	Skills     *orderedMap
	Difficulty float64
	Hinted     bool
}

// Classify reads what a chat request asks for. hint is a task kind the client
// named (X-NeoRouter-Task), which wins.
func Classify(request Request, inputTokens int, hint string) *Result {
	signals := ReadSignals(request, inputTokens)
	scores := make(map[string]float64, len(Kinds))
	for _, kind := range Kinds {
		scores[kind.ID] = clamp01(kind.Detect(signals))
	}
	_, hinted := scores[hint]
	if hint == "" {
		hinted = false
	}
	if hinted {
		for id, score := range scores {
			if id == hint {
				scores[id] = 1
			} else {
				scores[id] = score * 0.5
			}
		}
	}
	ranked := make([]scoredKind, 0, len(scores))
	for _, kind := range Kinds { // KINDS order keeps ties stable
		if score := scores[kind.ID]; score >= minKindScore {
			ranked = append(ranked, scoredKind{id: kind.ID, score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	// A short message that is nothing else ("ok, do it") is light
	// conversation, with tools on offer too, unless it asks for something to be
	// done.
	anyOther := false
	for _, item := range ranked {
		if item.id != "agent" || item.score > agentBaseline {
			anyOther = true
			break
		}
	}
	if !anyOther && signals.LastUserTokens < shortMessageTokens && signals.ToolResults == 0 {
		ranked = append([]scoredKind{{id: "chat", score: 0.5}}, ranked...)
	}
	// The skills the mix calls on, each kind in proportion to how clearly it
	// shows.
	skills := newOrderedMap()
	for _, item := range ranked {
		kind := kindByID(item.id)
		if kind == nil {
			continue
		}
		for _, skill := range kind.Skills {
			skills.add(skill.Name, item.score*skill.Weight)
		}
	}
	top := skills.maxValue()
	skills.each(func(key string, value float64) {
		skills.set(key, roundTo(value/top, 3))
	})
	kinds := newOrderedMap()
	for _, item := range ranked {
		kinds.set(item.id, roundTo(item.score, 3))
	}
	primary := "general"
	if len(ranked) > 0 {
		primary = ranked[0].id
	}
	return &Result{
		Primary:    primary,
		Kinds:      kinds,
		Skills:     skills,
		Difficulty: roundTo(difficultyOf(signals, ranked), 3),
		Hinted:     hinted,
	}
}

// roundTo is Number(value.toFixed(digits)).
func roundTo(value float64, digits int) float64 {
	scale := math.Pow(10, float64(digits))
	return math.Round(value*scale) / scale
}

// TaskKinds are the ids Classify can return as primary.
func TaskKinds() []string {
	ids := make([]string, 0, len(Kinds))
	for _, kind := range Kinds {
		ids = append(ids, kind.ID)
	}
	return ids
}

// MarshalJSON writes the fields in the Node code's order: primary, kinds,
// skills, difficulty, hinted.
func (r *Result) MarshalJSON() ([]byte, error) {
	if r.Kinds == nil {
		r.Kinds = newOrderedMap()
	}
	if r.Skills == nil {
		r.Skills = newOrderedMap()
	}
	var b strings.Builder
	b.WriteString(`{"primary":`)
	primary, err := jsonMarshal(r.Primary)
	if err != nil {
		return nil, err
	}
	b.Write(primary)
	b.WriteString(`,"kinds":`)
	kinds, err := r.Kinds.MarshalJSON()
	if err != nil {
		return nil, err
	}
	b.Write(kinds)
	b.WriteString(`,"skills":`)
	skills, err := r.Skills.MarshalJSON()
	if err != nil {
		return nil, err
	}
	b.Write(skills)
	b.WriteString(`,"difficulty":`)
	difficulty, err := jsonMarshal(r.Difficulty)
	if err != nil {
		return nil, err
	}
	b.Write(difficulty)
	b.WriteString(`,"hinted":`)
	hinted, err := jsonMarshal(r.Hinted)
	if err != nil {
		return nil, err
	}
	b.Write(hinted)
	b.WriteString("}")
	return []byte(b.String()), nil
}

// SkillWeights is the skills map as plain numbers, for scoring.
func (r *Result) SkillWeights() map[string]float64 {
	out := make(map[string]float64, r.Skills.len())
	r.Skills.each(func(key string, value float64) { out[key] = value })
	return out
}
