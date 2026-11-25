package media

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

func TestTimestamp(t *testing.T) {
	cases := []struct {
		seconds   float64
		separator byte
		want      string
	}{
		{0, '.', "00:00:00.000"},
		{1.2, ',', "00:00:01,200"},
		{3661.5, '.', "01:01:01.500"},
		{59.9999, ',', "00:01:00,000"},
		{-5, '.', "00:00:00.000"},
		{36000, '.', "10:00:00.000"},
	}
	for _, testCase := range cases {
		if got := timestamp(testCase.seconds, testCase.separator); got != testCase.want {
			t.Errorf("timestamp(%v) = %q, want %q", testCase.seconds, got, testCase.want)
		}
	}
}

func TestSubtitlesBuildCues(t *testing.T) {
	segments := []providers.Segment{{Start: 0, End: 1.2, Text: "hello"}, {Start: 1.2, End: 2.5, Text: " world "}}
	duration := 2.5
	srt := subtitles("hello world", &duration, segments, "srt")
	want := "1\n00:00:00,000 --> 00:00:01,200\nhello\n\n2\n00:00:01,200 --> 00:00:02,500\nworld\n"
	if srt != want {
		t.Fatalf("srt =\n%q\nwant\n%q", srt, want)
	}
	vtt := subtitles("hello world", &duration, segments, "vtt")
	wantVTT := "WEBVTT\n\n00:00:00.000 --> 00:00:01.200\nhello\n\n00:00:01.200 --> 00:00:02.500\nworld\n"
	if vtt != wantVTT {
		t.Fatalf("vtt =\n%q\nwant\n%q", vtt, wantVTT)
	}
}

func TestSubtitlesFallBackToTheWholeText(t *testing.T) {
	duration := 3.5
	if got := subtitles("all of it", &duration, nil, "srt"); got != "1\n00:00:00,000 --> 00:00:03,500\nall of it\n" {
		t.Fatalf("srt = %q", got)
	}
	if got := subtitles("all of it", nil, nil, "vtt"); got != "WEBVTT\n\n00:00:00.000 --> 00:00:00.000\nall of it\n" {
		t.Fatalf("vtt without a duration = %q", got)
	}
}

func TestFloat32Base64(t *testing.T) {
	encoded := float32Base64([]float64{1, 0.5})
	// Float32Array bytes are little-endian on every host the JS server ran on.
	want := make([]byte, 8)
	binary.LittleEndian.PutUint32(want[0:], math.Float32bits(1))
	binary.LittleEndian.PutUint32(want[4:], math.Float32bits(0.5))
	if encoded != base64.StdEncoding.EncodeToString(want) {
		t.Fatalf("encoded = %q", encoded)
	}
	if float32Base64(nil) != "" {
		t.Fatalf("no vector is an empty string")
	}
}
