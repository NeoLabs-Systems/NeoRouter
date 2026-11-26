package task

import "sort"

// Skills a model can be strong or weak at, and the benchmarks that measure
// them. The metadata service turns each model's benchmark scores into a score
// per skill on the same scale as its overall intelligence; routing then weighs
// the skills a request calls on (classify.go).
var Skills = []string{"coding", "math", "reasoning", "knowledge", "instruction", "agentic", "terminal", "long_context"}

// BenchmarkSkills maps an Artificial Analysis benchmark to the skills it
// measures. Browser and computer use have no benchmark there yet; browser work
// calls on agentic.
var BenchmarkSkills = map[string][]string{
	"livecodebench":      {"coding"},
	"scicode":            {"coding"},
	"terminalbench_hard": {"coding", "agentic", "terminal"},
	"aime_25":            {"math"},
	"aime":               {"math"},
	"math_500":           {"math"},
	"gpqa":               {"reasoning"},
	"hle":                {"reasoning"},
	"mmlu_pro":           {"knowledge"},
	"ifbench":            {"instruction"},
	"tau2":               {"agentic"},
	"lcr":                {"long_context"},
}

// How far a known skill moves the estimate away from overall intelligence when
// the request's skills are fully covered by benchmarks.
const specificShare = 0.6

// SkillsFromBenchmarks turns benchmark scores already mapped to the
// intelligence scale into skill scores.
func SkillsFromBenchmarks(mapped map[string]float64) map[string]float64 {
	sums := map[string]struct{ total, count float64 }{}
	benchmarks := make([]string, 0, len(mapped))
	for benchmark := range mapped {
		benchmarks = append(benchmarks, benchmark)
	}
	sort.Strings(benchmarks)
	for _, benchmark := range benchmarks {
		value := mapped[benchmark]
		for _, skill := range BenchmarkSkills[benchmark] {
			sum := sums[skill]
			sum.total += value
			sum.count++
			sums[skill] = sum
		}
	}
	out := make(map[string]float64, len(sums))
	for skill, sum := range sums {
		out[skill] = roundTo(mathMin(1, sum.total/sum.count), 4)
	}
	return out
}

// TaskIntelligence is the model's intelligence for this task: its overall
// score, moved towards its scores on the skills the task calls on, as far as
// benchmarks cover them.
func TaskIntelligence(general float64, modelSkills, taskSkills map[string]float64) float64 {
	if modelSkills == nil || taskSkills == nil {
		return general
	}
	total, covered, sum := 0.0, 0.0, 0.0
	// Iterate in a fixed order; the arithmetic is order-independent up to
	// floating point.
	for _, skill := range Skills {
		weight, ok := taskSkills[skill]
		if !ok {
			continue
		}
		total += weight
		value, ok := modelSkills[skill]
		if !ok {
			continue
		}
		covered += weight
		sum += weight * value
	}
	if covered == 0 {
		return general
	}
	share := specificShare * (covered / total)
	return (1-share)*general + share*(sum/covered)
}

// NeededRange is the intelligence band a task's difficulty maps onto: a
// greeting needs little, a hard proof a frontier model.
var NeededRange = [2]float64{0.3, 0.95}

// NeededIntelligence is how capable a model has to be for a task.
func NeededIntelligence(difficulty *float64) *float64 {
	if difficulty == nil {
		return nil
	}
	low, high := NeededRange[0], NeededRange[1]
	value := low + (high-low)*clamp01(*difficulty)
	return &value
}

// EnoughIntelligence is intelligence as the router weighs it: full credit up
// to what the task needs, and beyond that only as far as the style cares about
// intelligence (headroom, 0 to 1). Models that are good enough then compete on
// price, speed and reliability, as in Azure's model router and RouteLLM.
func EnoughIntelligence(intelligence float64, need *float64, headroom float64) float64 {
	if need == nil || *need == 0 {
		return intelligence
	}
	share := clamp01(headroom)
	return (1-share)*(mathMin(intelligence, *need) / *need) + share*intelligence
}

// SkillsOfKind is the skills a kind of task calls on.
func SkillsOfKind(id string) map[string]float64 {
	out := map[string]float64{}
	kind := kindByID(id)
	if kind == nil {
		return out
	}
	for _, skill := range kind.Skills {
		out[skill.Name] = skill.Weight
	}
	return out
}

// SkillsByKind is every kind's skills, keyed by kind id.
func SkillsByKind() map[string]map[string]float64 {
	out := make(map[string]map[string]float64, len(Kinds))
	for _, kind := range Kinds {
		out[kind.ID] = SkillsOfKind(kind.ID)
	}
	return out
}
