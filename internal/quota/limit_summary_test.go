package quota

import (
	"encoding/json"
	"testing"
)

func window(metric string, limit float64, source string) Detail {
	return Detail{Metric: metric, Limit: floatPtr(limit), Source: source}
}

func TestSummarizeReportsSharedAndPerModelLimits(t *testing.T) {
	rows := Summarize(SummaryGroup{
		Pool: []Detail{window(RPM, 20, SourceCurated)},
		Models: []SummaryModel{
			{Windows: []Detail{window(RPM, 3, SourceLearned), window(RPD, 250, SourceCurated)}},
			{Windows: []Detail{window(RPM, 100, SourceHeader), window(RPD, 1000, SourceCurated)}},
		},
	})
	if len(rows) != 2 || rows[0].Metric != RPM || rows[1].Metric != RPD {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Shared == nil || rows[0].Shared.Limit != 20 || rows[0].Shared.Source != SourceCurated {
		t.Fatalf("shared = %+v", rows[0].Shared)
	}
	want := SummaryPerModel{Min: 3, Max: 100, Models: 2, Sources: []string{SourceHeader, SourceLearned}}
	if rows[0].PerModel == nil || rows[0].PerModel.Min != want.Min || rows[0].PerModel.Max != want.Max ||
		rows[0].PerModel.Models != want.Models || len(rows[0].PerModel.Sources) != 2 ||
		rows[0].PerModel.Sources[0] != SourceHeader || rows[0].PerModel.Sources[1] != SourceLearned {
		t.Fatalf("perModel = %+v", rows[0].PerModel)
	}
	if rows[1].Shared != nil {
		t.Fatalf("rpd shared = %+v", rows[1].Shared)
	}
	if rows[1].PerModel == nil || rows[1].PerModel.Min != 250 || rows[1].PerModel.Max != 1000 {
		t.Fatalf("rpd perModel = %+v", rows[1].PerModel)
	}
	if len(rows[1].PerModel.Sources) != 1 || rows[1].PerModel.Sources[0] != SourceCurated {
		t.Fatalf("rpd sources = %+v", rows[1].PerModel.Sources)
	}

	encoded, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want0 = `{"metric":"rpm","shared":{"limit":20,"source":"curated"},"perModel":{"min":3,"max":100,"models":2,"sources":["header","learned"]}}`
	if string(encoded) != want0 {
		t.Fatalf("json = %s", encoded)
	}
}

func TestSummarizeIgnoresUsedUpMarkers(t *testing.T) {
	rows := Summarize(SummaryGroup{
		Pool: []Detail{{Metric: RPD, Source: SourceError}},
		Models: []SummaryModel{{Windows: []Detail{
			window(RPM, 0, SourceError),
			{Metric: TPM, Source: SourceCurated},
		}}},
	})
	if len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestSummarizeKeepsAFixedMetricOrder(t *testing.T) {
	rows := Summarize(SummaryGroup{Pool: []Detail{window(TPD, 5, SourceCurated), window(RPM, 1, SourceCurated)}})
	if len(rows) != 2 || rows[0].Metric != RPM || rows[1].Metric != TPD {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].PerModel != nil || rows[0].Shared == nil {
		t.Fatalf("row = %+v", rows[0])
	}
}
