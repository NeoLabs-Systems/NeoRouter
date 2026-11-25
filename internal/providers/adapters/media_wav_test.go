package adapters

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The WAV helpers keep wav.js's byte layout: the header a player and the
// gateway's converter read, the streamed file whose data size is unset, and the
// join of a text spoken in parts.

// mediaWAVSamples builds the WAV file one speech part is.
func mediaWAVSamples(sampleRate int, samples []byte) []byte {
	out := make([]byte, 0, 44+len(samples))
	out = append(out, WAVHeader(WAVFormat{Format: 1, Channels: 1, SampleRate: sampleRate, BitsPerSample: 16}, len(samples))...)
	return append(out, samples...)
}

// TestWAVHeaderLayout pins the 44 bytes the header is.
func TestWAVHeaderLayout(t *testing.T) {
	header := WAVHeader(WAVFormat{Channels: 1, SampleRate: 24000, BitsPerSample: 16}, 8)
	if len(header) != 44 {
		t.Fatalf("header length = %d", len(header))
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" || string(header[12:16]) != "fmt " || string(header[36:40]) != "data" {
		t.Fatalf("header tags = %q", header)
	}
	if got := binary.LittleEndian.Uint32(header[4:]); got != 44 {
		t.Fatalf("RIFF size = %d", got)
	}
	if got := binary.LittleEndian.Uint16(header[20:]); got != 1 {
		t.Fatalf("format = %d, want PCM", got)
	}
	if got := binary.LittleEndian.Uint32(header[28:]); got != 48000 {
		t.Fatalf("byte rate = %d", got)
	}
	if got := binary.LittleEndian.Uint16(header[32:]); got != 2 {
		t.Fatalf("block align = %d", got)
	}
	if got := binary.LittleEndian.Uint32(header[40:]); got != 8 {
		t.Fatalf("data size = %d", got)
	}
}

// TestParseWAVReadsFormatAndSamples: a plain file round-trips.
func TestParseWAVReadsFormatAndSamples(t *testing.T) {
	file := mediaWAVSamples(24000, []byte{1, 0, 2, 0})
	parsed, ok := ParseWAV(file)
	if !ok {
		t.Fatal("a WAV file was refused")
	}
	if parsed.Format != (WAVFormat{Format: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 16}) {
		t.Fatalf("format = %+v", parsed.Format)
	}
	if !bytes.Equal(parsed.Data, []byte{1, 0, 2, 0}) {
		t.Fatalf("data = %v", parsed.Data)
	}
}

// TestParseWAVStreamedDataSize: a streamed file leaves the data size unset, and
// its samples run to the end.
func TestParseWAVStreamedDataSize(t *testing.T) {
	for _, size := range []uint32{0, 0xFFFFFFFF} {
		file := mediaWAVSamples(16000, []byte{7, 0, 8, 0})
		binary.LittleEndian.PutUint32(file[40:], size)
		parsed, ok := ParseWAV(file)
		if !ok {
			t.Fatalf("size %d was refused", size)
		}
		if !bytes.Equal(parsed.Data, []byte{7, 0, 8, 0}) {
			t.Fatalf("size %d data = %v", size, parsed.Data)
		}
	}
}

// TestParseWAVKeepsTheFormatItDoesNotKnow: only PCM is spoken, but a provider's
// float WAV is still a WAV whose format the join compares.
func TestParseWAVKeepsTheFormatItDoesNotKnow(t *testing.T) {
	file := WAVHeader(WAVFormat{Format: 3, Channels: 2, SampleRate: 44100, BitsPerSample: 32}, 4)
	file = append(file, 1, 2, 3, 4)
	parsed, ok := ParseWAV(file)
	if !ok {
		t.Fatal("a float WAV was refused")
	}
	if parsed.Format.Format != 3 || parsed.Format.Channels != 2 || parsed.Format.BitsPerSample != 32 {
		t.Fatalf("format = %+v", parsed.Format)
	}
}

// TestParseWAVRefusesWhatIsNotWAV covers the shapes a provider may answer with
// instead of audio.
func TestParseWAVRefusesWhatIsNotWAV(t *testing.T) {
	cases := map[string][]byte{
		"empty":       nil,
		"short":       []byte("RIFF"),
		"wrong magic": append([]byte("RIFX....WAVE"), WAVHeader(WAVFormat{Channels: 1, SampleRate: 8000, BitsPerSample: 16}, 0)...),
		"no fmt":      append([]byte("RIFF\x00\x00\x00\x00WAVE"), []byte("data\x02\x00\x00\x00\x01\x00")...),
		// A short fmt chunk cannot say what the samples are.
		"short fmt": append(append([]byte("RIFF\x00\x00\x00\x00WAVE"), []byte("fmt \x04\x00\x00\x00\x01\x00\x01\x00")...),
			[]byte("data\x02\x00\x00\x00\x01\x00")...),
	}
	for name, file := range cases {
		if _, ok := ParseWAV(file); ok {
			t.Fatalf("%s was read as WAV", name)
		}
	}
}

// TestParseWAVSkipsPaddedChunks: chunks are padded to an even length, and the
// samples come after them.
func TestParseWAVSkipsPaddedChunks(t *testing.T) {
	file := []byte("RIFF\x00\x00\x00\x00WAVE")
	// An odd-sized chunk with its pad byte, then the format and the samples.
	file = append(file, []byte("LIST\x03\x00\x00\x00abc\x00")...)
	file = append(file, WAVHeader(WAVFormat{Channels: 1, SampleRate: 8000, BitsPerSample: 16}, 2)[12:36]...)
	file = append(file, []byte("data\x02\x00\x00\x00\x05\x00")...)
	parsed, ok := ParseWAV(file)
	if !ok {
		t.Fatal("the padded file was refused")
	}
	if parsed.Format.SampleRate != 8000 || !bytes.Equal(parsed.Data, []byte{5, 0}) {
		t.Fatalf("parsed = %+v %v", parsed.Format, parsed.Data)
	}
}

// TestJoinWAV: the parts' samples go in order under one header.
func TestJoinWAV(t *testing.T) {
	joined, err := JoinWAV([][]byte{
		mediaWAVSamples(24000, []byte{1, 0, 1, 0}),
		mediaWAVSamples(24000, []byte{2, 0, 2, 0}),
		mediaWAVSamples(24000, []byte{3, 0}),
	})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if got := binary.LittleEndian.Uint32(joined[4:]); int(got) != len(joined)-8 {
		t.Fatalf("RIFF size = %d, length %d", got, len(joined))
	}
	parsed, ok := ParseWAV(joined)
	if !ok {
		t.Fatal("the joined file is not a WAV")
	}
	if parsed.Format.SampleRate != 24000 {
		t.Fatalf("sample rate = %d", parsed.Format.SampleRate)
	}
	if want := []byte{1, 0, 1, 0, 2, 0, 2, 0, 3, 0}; !bytes.Equal(parsed.Data, want) {
		t.Fatalf("samples = %v, want %v", parsed.Data, want)
	}
}

// TestJoinWAVRefusals keeps the two messages the speech handler shows.
func TestJoinWAVRefusals(t *testing.T) {
	_, err := JoinWAV([][]byte{mediaWAVSamples(24000, []byte{1, 0}), []byte("mp3-bytes")})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindInvalidOutput {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "The provider returned audio that is not WAV, so its parts cannot be joined." {
		t.Fatalf("message = %q", got)
	}
	_, err = JoinWAV([][]byte{
		mediaWAVSamples(24000, []byte{1, 0}),
		WAVHeader(WAVFormat{Channels: 2, SampleRate: 24000, BitsPerSample: 16}, 2),
	})
	if got := mediaErrorText(err); got != "The provider returned audio parts in different formats." {
		t.Fatalf("message = %q", got)
	}
}

// TestPCMToWAV wraps raw PCM at the rate the content type named.
func TestPCMToWAV(t *testing.T) {
	file := PCMToWAV([]byte{1, 0, 2, 0}, mediaPCMRate("audio/pcm;rate=16000"))
	parsed, ok := ParseWAV(file)
	if !ok {
		t.Fatal("the wrapped PCM is not a WAV")
	}
	if parsed.Format.SampleRate != 16000 || parsed.Format.Channels != 1 || parsed.Format.BitsPerSample != 16 {
		t.Fatalf("format = %+v", parsed.Format)
	}
	if !bytes.Equal(parsed.Data, []byte{1, 0, 2, 0}) {
		t.Fatalf("samples = %v", parsed.Data)
	}
}

// TestPCMSampleRate reads the rate a content type names, and the 24 kHz default.
func TestPCMSampleRate(t *testing.T) {
	cases := map[string]int{
		"audio/pcm":                       24000,
		"audio/pcm;rate=16000":            16000,
		"audio/L16;rate=8000":             8000,
		"audio/pcm;channels=1;rate=48000": 48000,
		"audio/pcm;rate=0":                24000,
	}
	for mime, want := range cases {
		if got := mediaPCMRate(mime); got != want {
			t.Fatalf("mediaPCMRate(%q) = %d, want %d", mime, got, want)
		}
	}
}
