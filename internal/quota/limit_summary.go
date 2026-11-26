package quota

// The limits of one provider account in a form a person can read at a glance.
// Source order, strongest first, is the order sources are reported in.
var summarySourceOrder = []string{SourceManual, SourceHeader, SourceLearned, SourceCurated}

// SummaryModel is one model of a provider account with the windows it was read
// from.
type SummaryModel struct {
	Windows []Detail
}

// SummaryGroup is one provider account: the shared pool windows and its models.
type SummaryGroup struct {
	Pool   []Detail
	Models []SummaryModel
}

// SummaryRow is one metric's limits: what the account shares and what each
// model gets.
type SummaryRow struct {
	Metric   string           `json:"metric"`
	Shared   *SummaryShared   `json:"shared"`
	PerModel *SummaryPerModel `json:"perModel"`
}

// SummaryShared is the account's shared limit for one metric.
type SummaryShared struct {
	Limit  float64 `json:"limit"`
	Source string  `json:"source"`
}

// SummaryPerModel is the range of limits across a group's models.
type SummaryPerModel struct {
	Min     float64  `json:"min"`
	Max     float64  `json:"max"`
	Models  int      `json:"models"`
	Sources []string `json:"sources"`
}

// isLimit reports whether a window is a real limit. A window without a
// positive limit is only an "out until later" marker.
func isLimit(window Detail) bool {
	return window.Limit != nil && *window.Limit > 0 && window.Source != SourceError
}

func sourcesOf(windows []Detail) []string {
	seen := map[string]bool{}
	for _, window := range windows {
		seen[window.Source] = true
	}
	out := []string{}
	for _, source := range summarySourceOrder {
		if seen[source] {
			out = append(out, source)
		}
	}
	return out
}

// Summarize is summarizeLimits: one row per metric that carries a limit, with
// the shared account limit and the per-model range reported separately.
func Summarize(group SummaryGroup) []SummaryRow {
	rows := []SummaryRow{}
	for _, metric := range Metrics {
		var shared *Detail
		for i := range group.Pool {
			if group.Pool[i].Metric == metric && isLimit(group.Pool[i]) {
				shared = &group.Pool[i]
				break
			}
		}
		own := []Detail{}
		for _, model := range group.Models {
			for _, window := range model.Windows {
				if window.Metric == metric && isLimit(window) {
					own = append(own, window)
				}
			}
		}
		if shared == nil && len(own) == 0 {
			continue
		}
		row := SummaryRow{Metric: metric}
		if shared != nil {
			row.Shared = &SummaryShared{Limit: *shared.Limit, Source: shared.Source}
		}
		if len(own) > 0 {
			min, max := *own[0].Limit, *own[0].Limit
			for _, window := range own[1:] {
				if *window.Limit < min {
					min = *window.Limit
				}
				if *window.Limit > max {
					max = *window.Limit
				}
			}
			row.PerModel = &SummaryPerModel{Min: min, Max: max, Models: len(own), Sources: sourcesOf(own)}
		}
		rows = append(rows, row)
	}
	return rows
}
