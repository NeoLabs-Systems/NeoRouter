package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Speech providers produce different formats (Gemini WAV, Groq WAV, others MP3).
// When ffmpeg is installed the gateway converts to the format the client asked
// for; without it the original format is returned and named in the Content-Type
// and X-NeoRouter-Audio-Format headers
// (server/capabilities/media/transcode.js + server/utils/ffmpeg.js).

// audioMime is the content type of each container the API can answer with.
var audioMime = map[string]string{
	"mp3": "audio/mpeg", "opus": "audio/ogg", "aac": "audio/aac",
	"flac": "audio/flac", "wav": "audio/wav", "pcm": "audio/pcm",
}

// ffmpegArgs converts from whatever the provider produced to one container.
var ffmpegArgs = map[string][]string{
	"mp3":  {"-f", "mp3", "-c:a", "libmp3lame", "-b:a", "128k"},
	"opus": {"-f", "ogg", "-c:a", "libopus", "-b:a", "64k"},
	"aac":  {"-f", "adts", "-c:a", "aac", "-b:a", "128k"},
	"flac": {"-f", "flac"},
	"wav":  {"-f", "wav"},
	"pcm":  {"-f", "s16le", "-ac", "1", "-ar", "24000"},
}

var (
	mpegFormat = regexp.MustCompile(`mpeg|mp3`)
	wavFormat  = regexp.MustCompile(`wav|wave`)
	opusFormat = regexp.MustCompile(`ogg|opus`)
	aacFormat  = regexp.MustCompile(`aac`)
	flacFormat = regexp.MustCompile(`flac`)
	pcmFormat  = regexp.MustCompile(`pcm|l16`)
)

// formatOf names the container a content type carries, "" when unknown.
func formatOf(mime string) string {
	value := strings.ToLower(mime)
	switch {
	case mpegFormat.MatchString(value):
		return "mp3"
	case wavFormat.MatchString(value):
		return "wav"
	case opusFormat.MatchString(value):
		return "opus"
	case aacFormat.MatchString(value):
		return "aac"
	case flacFormat.MatchString(value):
		return "flac"
	case pcmFormat.MatchString(value):
		return "pcm"
	}
	return ""
}

// convertedAudio is audio in the format the response reports.
type convertedAudio struct {
	Audio  []byte
	Format string
	Mime   string
}

// toFormat returns the audio in the requested format when ffmpeg can make it,
// and the original otherwise.
func toFormat(audio []byte, mime, target string) convertedAudio {
	source := formatOf(mime)
	original := convertedAudio{Audio: audio, Format: source, Mime: mime}
	if original.Format == "" {
		original.Format = "unknown"
	}
	if original.Mime == "" {
		original.Mime = "application/octet-stream"
	}
	if target == "" || source == target || ffmpegArgs[target] == nil || !hasFfmpeg() {
		return original
	}
	converted, err := runFfmpeg(audio, ffmpegArgs[target])
	if err != nil {
		return original
	}
	return convertedAudio{Audio: converted, Format: target, Mime: audioMime[target]}
}

// ffmpegProbe caches the one ffmpeg check the process makes.
var ffmpegProbe struct {
	once      sync.Once
	available bool
}

// hasFfmpeg reports whether this host can convert audio. The answer is probed
// once per process, as utils/ffmpeg.js caches it; tests replace it.
var hasFfmpeg = probeFfmpeg

// probeFfmpeg runs the one ffmpeg check.
func probeFfmpeg() bool {
	ffmpegProbe.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ffmpegProbe.available = exec.CommandContext(ctx, "ffmpeg", "-version").Run() == nil
	})
	return ffmpegProbe.available
}

// runFfmpeg runs ffmpeg with args between the input and the output pipe: audio
// in on stdin, the result from stdout.
func runFfmpeg(input []byte, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	full := append([]string{"-hide_banner", "-loglevel", "error", "-i", "pipe:0"}, args...)
	full = append(full, "pipe:1")
	command := exec.CommandContext(ctx, "ffmpeg", full...)
	command.Stdin = bytes.NewReader(input)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("media: ffmpeg: %w", err)
	}
	if output.Len() == 0 {
		return nil, errors.New("media: ffmpeg produced no output")
	}
	return output.Bytes(), nil
}
