package localmodels

// audio.go is server/services/local_models/audio.js: audio for the local speech
// models — what comes in becomes 16 kHz mono samples (what speech recognition
// reads), what a voice produces becomes a WAV file. Without ffmpeg only WAV
// input can be read.

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os/exec"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/audiox"
)

// SampleRate is the rate speech recognition reads.
const SampleRate = 16000

// ffmpegTimeoutMs bounds one decode.
const ffmpegTimeoutMs = 120_000

// FfmpegAvailable reports whether ffmpeg is installed, which is what decides
// between decoding any format and reading WAV only.
func FfmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// ToSamples turns audio into 16 kHz mono samples, or reports false when the
// audio cannot be read.
func ToSamples(audio []byte) ([]float32, bool) {
	if FfmpegAvailable() {
		if samples, ok := ffmpegSamples(audio); ok {
			return samples, true
		}
	}
	return samplesFromWav(audio)
}

// ffmpegSamples decodes through ffmpeg into 32-bit float mono at 16 kHz.
func ffmpegSamples(audio []byte) ([]float32, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeoutMs*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-f", "f32le", "-ac", "1", "-ar", "16000", "pipe:1")
	command.Stdin = bytes.NewReader(audio)
	raw, err := command.Output()
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	// A trailing partial sample is dropped, like the JS slice.
	usable := len(raw) - len(raw)%4
	samples := make([]float32, usable/4)
	for index := 0; index < len(samples); index++ {
		bits := binary.LittleEndian.Uint32(raw[index*4 : index*4+4])
		samples[index] = math.Float32frombits(bits)
	}
	return samples, true
}

// samplesFromWav reads 16-bit PCM WAV: it resamples to 16 kHz and mixes down to
// mono. Anything else (another bit depth, a compressed format) is unreadable.
func samplesFromWav(audio []byte) ([]float32, bool) {
	pcm, format, err := audiox.ParseWAV(audio)
	if err != nil || format.BitsPerSample != 16 {
		return nil, false
	}
	channels := format.Channels
	if channels <= 0 || format.SampleRate <= 0 {
		return nil, false
	}
	frames := len(pcm) / (2 * channels)
	ratio := float64(format.SampleRate) / float64(SampleRate)
	count := int(float64(frames) / ratio)
	if count <= 0 {
		return nil, false
	}
	samples := make([]float32, count)
	for index := 0; index < count; index++ {
		frame := int(float64(index) * ratio)
		if frame > frames-1 {
			frame = frames - 1
		}
		sum := 0.0
		for channel := 0; channel < channels; channel++ {
			offset := (frame*channels + channel) * 2
			if offset+1 >= len(pcm) {
				break
			}
			sum += float64(int16(binary.LittleEndian.Uint16(pcm[offset : offset+2])))
		}
		samples[index] = float32(sum / float64(channels) / 32768)
	}
	return samples, true
}

// ToWav packs 16-bit mono samples into a WAV file.
func ToWav(samples []float32, sampleRate int) []byte {
	pcm := make([]byte, len(samples)*2)
	for index, sample := range samples {
		value := float64(sample)
		if value > 1 {
			value = 1
		}
		if value < -1 {
			value = -1
		}
		scaled := math.Round(value * 32767)
		if value < 0 {
			scaled = math.Round(value * 32768)
		}
		if scaled > 32767 {
			scaled = 32767
		}
		if scaled < -32768 {
			scaled = -32768
		}
		binary.LittleEndian.PutUint16(pcm[index*2:index*2+2], uint16(int16(scaled)))
	}
	return audiox.WrapWAV(pcm, audiox.PCMFormat{SampleRate: sampleRate, Channels: 1, BitsPerSample: 16})
}
