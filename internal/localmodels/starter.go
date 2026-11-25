package localmodels

// starter.go is server/services/local_models/starter.js: a first set for an
// admin who does not know where to start — a text model, speech recognition and
// a voice that run well together on this machine. From the catalog's
// recommendations and the fit; no model is named here.

import "sort"

// starterKinds are the kinds a starter set covers, in the order they are chosen.
var starterKinds = []string{KindChat, KindTranscription, KindSpeech}

// chatShare is the share of the memory the text model may take, so the others
// fit beside it.
const chatShare = 0.6

// runsWell are the ratings a starter model must have.
var runsWell = map[string]bool{RatingGreat: true, RatingGood: true}

// StarterSuggestion is one model of a starter set.
type StarterSuggestion struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	DownloadMB float64 `json:"downloadMb"`
	MemoryMB   int64   `json:"memoryMb"`
}

// SuggestStarter returns the models that fit together, or none when nothing
// does.
func SuggestStarter(machine Machine, support func(engine string, machine Machine) Support) []StarterSuggestion {
	budgetMB := RoomMB(machine)
	chosen := []StarterSuggestion{}
	usedMB := int64(0)
	for _, kind := range starterKinds {
		room := budgetMB - usedMB
		if kind == KindChat {
			room = int64(roundHalfUp(float64(budgetMB) * chatShare))
		}
		found, ok := pickStarter(kind, machine, room, support)
		if !ok {
			continue
		}
		usedMB += found.MemoryMB
		chosen = append(chosen, found)
	}
	return chosen
}

// pickStarter is starter.pick: the recommended model first, then the most
// capable (largest) that still fits.
func pickStarter(kind string, machine Machine, roomMB int64, support func(engine string, machine Machine) Support) (StarterSuggestion, bool) {
	type candidate struct {
		entry      *Entry
		assessment Assessment
	}
	candidates := []candidate{}
	for _, entry := range Catalog() {
		if entry.Kind != kind {
			continue
		}
		assessed := Assess(entry, machine, AssessOptions{Support: support})
		if !runsWell[assessed.Rating] || !assessed.DiskOK || assessed.MemoryMB > roomMB {
			continue
		}
		candidates = append(candidates, candidate{entry: entry, assessment: assessed})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		leftRecommended := boolToInt(left.entry.Recommended)
		rightRecommended := boolToInt(right.entry.Recommended)
		if leftRecommended != rightRecommended {
			return leftRecommended > rightRecommended
		}
		return left.entry.DownloadMB > right.entry.DownloadMB
	})
	if len(candidates) == 0 {
		return StarterSuggestion{}, false
	}
	best := candidates[0]
	return StarterSuggestion{
		ID:         best.entry.ID,
		Name:       best.entry.Name,
		Kind:       kind,
		DownloadMB: best.assessment.DownloadMB,
		MemoryMB:   best.assessment.MemoryMB,
	}, true
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
