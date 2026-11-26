package media

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Subtitles and base64 vectors, formatted exactly as the transcription and
// embedding responses of server/capabilities/media/handlers.js format them.

// timestamp renders a subtitle cue time: HH:MM:SS,mmm for SRT and HH:MM:SS.mmm
// for WebVTT. Whole milliseconds, so 1.2 s is 00:00:01,200 and not .199.
func timestamp(seconds float64, separator byte) string {
	value := seconds
	if math.IsNaN(value) || value < 0 {
		value = 0
	}
	ms := int64(math.Round(value * 1000))
	pad := func(number int64, size int) string {
		text := strconv.FormatInt(number, 10)
		for len(text) < size {
			text = "0" + text
		}
		return text
	}
	return fmt.Sprintf("%s:%s:%s%c%s",
		pad(ms/3600000, 2), pad((ms%3600000)/60000, 2), pad((ms%60000)/1000, 2),
		separator, pad(ms%1000, 3))
}

// subtitles builds an SRT or WebVTT document from a transcript. Without
// segments the whole text is one cue.
func subtitles(text string, duration *float64, segments []providers.Segment, format string) string {
	if len(segments) == 0 {
		end := 0.0
		if duration != nil && *duration != 0 {
			end = *duration
		}
		segments = []providers.Segment{{Start: 0, End: end, Text: text}}
	}
	separator := byte('.')
	if format == "srt" {
		separator = ','
	}
	cues := make([]string, 0, len(segments))
	for index, segment := range segments {
		cue := timestamp(segment.Start, separator) + " --> " + timestamp(segment.End, separator)
		body := strings.TrimSpace(segment.Text)
		if format == "srt" {
			cues = append(cues, strconv.Itoa(index+1)+"\n"+cue+"\n"+body+"\n")
			continue
		}
		cues = append(cues, cue+"\n"+body+"\n")
	}
	document := strings.Join(cues, "\n")
	if format == "vtt" {
		return "WEBVTT\n\n" + document
	}
	return document
}

// float32Base64 is the embedding vector as base64 of its float32 bytes, the way
// Buffer.from(new Float32Array(vector).buffer).toString('base64') renders it.
func float32Base64(vector []float64) string {
	buffer := make([]byte, 4*len(vector))
	for index, value := range vector {
		binary.LittleEndian.PutUint32(buffer[index*4:], math.Float32bits(float32(value)))
	}
	return base64.StdEncoding.EncodeToString(buffer)
}
