package status

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestPowerOfTwoBucket pins the bands, including the sub-unit ones and the
// unknown case.
func TestPowerOfTwoBucket(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0, "unknown"},
		{-1, "unknown"},
		{1, "0.5-1"},
		{3, "2-4"},
		{6, "4-8"},
		{8, "4-8"},
		{15.5, "8-16"},
		{16, "8-16"},
		{17, "16-32"},
		{1.5e10, "8589934592-17179869184"},
	}
	for _, c := range cases {
		if got := powerOfTwoBucket(c.value); got != c.want {
			t.Fatalf("powerOfTwoBucket(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}

// TestFormatNumber keeps the band text free of exponents.
func TestFormatNumber(t *testing.T) {
	cases := map[float64]string{0.25: "0.25", 8: "8", 17179869184: "17179869184", 0.5: "0.5"}
	for value, want := range cases {
		if got := FormatNumber(value); got != want {
			t.Fatalf("FormatNumber(%v) = %q, want %q", value, got, want)
		}
	}
}

// TestHostSnapshotIsCoarse: the report carries bands, never identities.
func TestHostSnapshotIsCoarse(t *testing.T) {
	snap := Host{Version: "9.9.9", StartedAt: time.Now()}.Snapshot()
	if snap.OS == "" || snap.RAMGiBRange == "" || snap.CPUCoresRange == "" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.AppVersion != "9.9.9" {
		t.Fatalf("app_version = %q", snap.AppVersion)
	}
	if snap.UptimeHoursRange != "unknown" && snap.UptimeHoursRange == "" {
		t.Fatalf("uptime_hours_range = %q", snap.UptimeHoursRange)
	}
	if snap.Architecture == "" {
		t.Fatal("architecture is empty")
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, forbidden := range []string{"hostname", `"ip"`, "user", "path"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("snapshot leaks %q: %s", forbidden, body)
		}
	}
}

// TestReportHostRespectsOptOut: nothing is sent when analytics are opted out,
// one report is sent when they are on, and a failing send is not fatal.
func TestReportHostRespectsOptOut(t *testing.T) {
	snap := Host{Version: "1.0.0", StartedAt: time.Now()}.Snapshot()
	var forms []url.Values
	post := func(_ context.Context, endpoint string, form url.Values) error {
		if endpoint != CountlyEndpoint {
			t.Fatalf("endpoint = %q", endpoint)
		}
		forms = append(forms, form)
		return nil
	}

	if err := ReportHost(context.Background(), nil, snap, post); err != nil {
		t.Fatalf("opted-out report returned %v", err)
	}
	if len(forms) != 0 {
		t.Fatalf("opted-out report sent %d requests", len(forms))
	}

	if err := ReportHost(context.Background(), DefaultConfig(), snap, post); err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(forms) != 1 {
		t.Fatalf("requests = %d, want 1", len(forms))
	}
	form := forms[0]
	if form.Get("app_key") != CountlyAppKey {
		t.Fatalf("app_key = %q", form.Get("app_key"))
	}
	if form.Get("device_id") != "aggregate-server" {
		t.Fatalf("device_id = %q", form.Get("device_id"))
	}
	var events []HostEvent
	if err := json.Unmarshal([]byte(form.Get("events")), &events); err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].Key != "host-snapshot" || events[0].Count != 1 {
		t.Fatalf("events = %+v", events)
	}
	segmentation, err := json.Marshal(events[0].Segmentation)
	if err != nil {
		t.Fatalf("segmentation: %v", err)
	}
	for _, forbidden := range []string{"hostname", `"id"`} {
		if strings.Contains(string(segmentation), forbidden) {
			t.Fatalf("segmentation leaks %q: %s", forbidden, segmentation)
		}
	}

	failing := func(context.Context, string, url.Values) error { return errors.New("unavailable") }
	if err := ReportHost(context.Background(), DefaultConfig(), snap, failing); err == nil {
		t.Fatal("a failed report must be reported to the caller")
	}
}
