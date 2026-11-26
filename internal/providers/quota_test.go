package providers

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestQuotaParserParity replays every probe answer the JS parsers read and
// requires the same windows.
func TestQuotaParserParity(t *testing.T) {
	cases := loadFixture(t).Quota
	if len(cases) == 0 {
		t.Fatal("no quota fixtures")
	}
	for _, testCase := range cases {
		t.Run(testCase.Parser, func(t *testing.T) {
			parser, ok := quotaParsers[testCase.Parser]
			if !ok {
				t.Fatalf("no Go parser named %q", testCase.Parser)
			}
			var want []QuotaWindow
			if err := json.Unmarshal(testCase.Expect, &want); err != nil {
				t.Fatalf("decode expectation: %v", err)
			}
			got := parser(testCase.Body)
			if len(got) == 0 && len(want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("body %v: got %+v, want %+v", testCase.Body, got, want)
			}
		})
	}
}

// TestQuotaParsersAreReachable: a definition that names a probe parser gets the
// Go function, so the quota worker can call it without a lookup of its own.
func TestQuotaParsersAreReachable(t *testing.T) {
	registry, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, id := range []string{"openrouter", "deepseek", "siliconflow", "synthetic", "minimax_plan", "opencode_go"} {
		definition, ok := registry.Get(id)
		if !ok {
			t.Fatalf("%s: no definition", id)
		}
		if definition.Quota.Probe == nil || definition.Quota.Probe.Parse == nil {
			t.Fatalf("%s: no quota probe", id)
		}
		if definition.Quota.Probe.Path == "" {
			t.Fatalf("%s: no probe path", id)
		}
	}
	// A registration probe the provider answers with nothing yields no windows.
	deepseek, _ := registry.Get("deepseek")
	if windows := deepseek.Quota.Probe.Parse(map[string]any{}); len(windows) != 0 {
		t.Fatalf("empty body = %+v", windows)
	}
}

// TestMinimaxWindowsKeepsTheTightestModel: MiniMax reports one window per model
// and the plan is only as free as its tightest model.
func TestMinimaxWindowsKeepsTheTightestModel(t *testing.T) {
	windows := minimaxWindows(map[string]any{"model_remains": []any{
		map[string]any{"model_name": "MiniMax-M3", "current_interval_remaining_percent": 80, "current_weekly_remaining_percent": 90, "end_time": 1762500000, "weekly_end_time": 1762900000},
		map[string]any{"model_name": "MiniMax-M3-highspeed", "current_interval_remaining_percent": 15, "current_weekly_remaining_percent": 40, "end_time": 1762500100, "weekly_end_time": 1762900100},
		map[string]any{"model_name": "hailuo-video", "current_interval_remaining_percent": 1, "current_weekly_remaining_percent": 1},
	}})
	if len(windows) != 2 {
		t.Fatalf("windows = %+v", windows)
	}
	if windows[0].Metric != "plan_5h" || *windows[0].Remaining != 15 || *windows[0].ResetAt != 1762500100 {
		t.Fatalf("5h window = %+v", windows[0])
	}
	if windows[1].Metric != "plan_week" || *windows[1].Remaining != 40 {
		t.Fatalf("week window = %+v", windows[1])
	}
	if *windows[0].Limit != 100 {
		t.Fatalf("limit = %v", *windows[0].Limit)
	}
}

// TestOpencodeGoWindowsRounds like Math.round: the meters are percentages and
// what is left is the complement.
func TestOpencodeGoWindowsRounds(t *testing.T) {
	windows := opencodeGoWindows(map[string]any{"usage": map[string]any{
		"rolling": map[string]any{"percent": 33.4},
		"weekly":  map[string]any{"usagePercent": 66.6},
		"monthly": map[string]any{"percent": 120},
	}})
	if len(windows) != 3 {
		t.Fatalf("windows = %+v", windows)
	}
	if *windows[0].Remaining != 67 {
		t.Fatalf("rolling = %v", *windows[0].Remaining)
	}
	if *windows[1].Remaining != 33 {
		t.Fatalf("weekly = %v", *windows[1].Remaining)
	}
	if *windows[2].Remaining != 0 {
		t.Fatalf("monthly = %v", *windows[2].Remaining)
	}
}

// TestSyntheticPlanClampsAtZero: a plan can be over its limit, which leaves
// nothing rather than a negative amount.
func TestSyntheticPlanClampsAtZero(t *testing.T) {
	windows := syntheticPlan(map[string]any{"subscription": map[string]any{"limit": 100, "requests": 140, "renewsAt": "2026-11-08T00:00:00Z"}})
	if len(windows) != 1 || *windows[0].Remaining != 0 {
		t.Fatalf("windows = %+v", windows)
	}
	if windows[0].ResetAt == nil {
		t.Fatal("no reset time")
	}
}
