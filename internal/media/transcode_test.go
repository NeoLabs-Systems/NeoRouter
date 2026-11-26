package media

import "testing"

func TestFormatOf(t *testing.T) {
	cases := []struct{ mime, want string }{
		{"audio/mpeg", "mp3"},
		{"audio/mp3", "mp3"},
		{"audio/wav", "wav"},
		{"audio/x-wav", "wav"},
		{"audio/wave", "wav"},
		{"audio/ogg", "opus"},
		{"audio/ogg; codecs=opus", "opus"},
		{"audio/aac", "aac"},
		{"audio/flac", "flac"},
		{"audio/pcm", "pcm"},
		{"audio/l16", "pcm"},
		{"audio/x-m4a", ""},
		{"", ""},
	}
	for _, testCase := range cases {
		if got := formatOf(testCase.mime); got != testCase.want {
			t.Errorf("formatOf(%q) = %q, want %q", testCase.mime, got, testCase.want)
		}
	}
}

func TestToFormatWithoutFfmpegKeepsTheOriginal(t *testing.T) {
	original := hasFfmpeg
	hasFfmpeg = func() bool { return false }
	t.Cleanup(func() { hasFfmpeg = original })

	converted := toFormat([]byte("wav-bytes"), "audio/wav", "mp3")
	if string(converted.Audio) != "wav-bytes" || converted.Format != "wav" || converted.Mime != "audio/wav" {
		t.Fatalf("converted = %+v", converted)
	}
	same := toFormat([]byte("mp3-bytes"), "audio/mpeg", "mp3")
	if same.Format != "mp3" {
		t.Fatalf("same format = %+v", same)
	}
	unknown := toFormat([]byte("??"), "", "")
	if unknown.Format != "unknown" || unknown.Mime != "application/octet-stream" {
		t.Fatalf("unknown = %+v", unknown)
	}
	// A target the gateway cannot produce is not attempted.
	unsupported := toFormat([]byte("??"), "audio/wav", "m4a")
	if unsupported.Format != "wav" {
		t.Fatalf("unsupported target = %+v", unsupported)
	}
}

// TestToFormatWithFfmpegFallsBackWhenItFails keeps the promise of the JS module:
// a conversion that fails answers with the original audio, never an error.
func TestToFormatWithFfmpegFallsBackWhenItFails(t *testing.T) {
	original := hasFfmpeg
	hasFfmpeg = func() bool { return true }
	t.Cleanup(func() { hasFfmpeg = original })

	converted := toFormat([]byte("not audio at all"), "audio/wav", "mp3")
	if converted.Format != "wav" || string(converted.Audio) != "not audio at all" {
		t.Fatalf("converted = %+v", converted)
	}
	if audioMime["mp3"] != "audio/mpeg" || ffmpegArgs["pcm"][0] != "-f" {
		t.Fatal("the container tables are the JS ones")
	}
}
