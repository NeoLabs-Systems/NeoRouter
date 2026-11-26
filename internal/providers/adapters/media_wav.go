package adapters

import (
	"encoding/binary"

	"github.com/NeoLabs-Systems/neolabs-go/audiox"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// WAV helpers for speech. Some providers cap the text of one request (Groq's
// Orpheus), so a longer text is spoken in parts whose WAV files are joined into
// one. Others answer raw PCM, which is wrapped into a WAV file so that players
// and the gateway's converter can read it.

// WAVFormat is the fmt chunk of a WAV file.
type WAVFormat struct {
	// Format is the compression code: 1 is uncompressed PCM.
	Format        int
	Channels      int
	SampleRate    int
	BitsPerSample int
}

// WAV is a parsed WAV file: its sample format and its samples.
type WAV struct {
	Format WAVFormat
	Data   []byte
}

// WAVHeader builds the 44-byte canonical WAV header for dataLength bytes of
// samples in the given format.
func WAVHeader(format WAVFormat, dataLength int) []byte {
	if format.Format == 0 {
		format.Format = 1
	}
	blockAlign := format.Channels * (format.BitsPerSample / 8)
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+dataLength))
	copy(header[8:], "WAVE")
	copy(header[12:], "fmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], uint16(format.Format))
	binary.LittleEndian.PutUint16(header[22:], uint16(format.Channels))
	binary.LittleEndian.PutUint32(header[24:], uint32(format.SampleRate))
	binary.LittleEndian.PutUint32(header[28:], uint32(format.SampleRate*blockAlign))
	binary.LittleEndian.PutUint16(header[32:], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:], uint16(format.BitsPerSample))
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(dataLength))
	return header
}

// ParseWAV reads a WAV file's format and samples, or reports false when the
// bytes are not one. A streamed WAV leaves the data size unset (0 or
// 0xFFFFFFFF): its samples run to the end of the file.
func ParseWAV(buffer []byte) (WAV, bool) {
	if len(buffer) < 12 || string(buffer[0:4]) != "RIFF" || string(buffer[8:12]) != "WAVE" {
		return WAV{}, false
	}
	var format WAVFormat
	haveFormat := false
	offset := 12
	for offset+8 <= len(buffer) {
		id := string(buffer[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(buffer[offset+4:]))
		start := offset + 8
		if id == "fmt " && start+16 <= len(buffer) {
			format = WAVFormat{
				Format:        int(binary.LittleEndian.Uint16(buffer[start:])),
				Channels:      int(binary.LittleEndian.Uint16(buffer[start+2:])),
				SampleRate:    int(binary.LittleEndian.Uint32(buffer[start+4:])),
				BitsPerSample: int(binary.LittleEndian.Uint16(buffer[start+14:])),
			}
			haveFormat = true
		}
		if id == "data" {
			if !haveFormat {
				return WAV{}, false
			}
			end := len(buffer)
			if size > 0 && start+size <= len(buffer) {
				end = start + size
			}
			return WAV{Format: format, Data: buffer[start:end]}, true
		}
		// Chunks are padded to an even length.
		offset = start + size + size%2
	}
	return WAV{}, false
}

// JoinWAV builds one WAV file from several with the same sample format, their
// samples in order.
func JoinWAV(buffers [][]byte) ([]byte, error) {
	parts := make([]WAV, 0, len(buffers))
	for _, buffer := range buffers {
		part, ok := ParseWAV(buffer)
		if !ok {
			return nil, providers.NewError(providers.KindInvalidOutput,
				"The provider returned audio that is not WAV, so its parts cannot be joined.", providers.ErrorOptions{})
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return nil, providers.NewError(providers.KindEmpty, "The provider returned no audio.", providers.ErrorOptions{})
	}
	first := parts[0]
	for _, part := range parts[1:] {
		if part.Format != first.Format {
			return nil, providers.NewError(providers.KindInvalidOutput,
				"The provider returned audio parts in different formats.", providers.ErrorOptions{})
		}
	}
	data := make([]byte, 0, len(first.Data)*len(parts))
	for _, part := range parts {
		data = append(data, part.Data...)
	}
	out := make([]byte, 0, 44+len(data))
	out = append(out, WAVHeader(first.Format, len(data))...)
	return append(out, data...), nil
}

// PCMToWAV wraps 16-bit mono PCM into a WAV file. The header is
// neolabs-go/audiox's canonical one.
func PCMToWAV(pcm []byte, sampleRate int) []byte {
	return audiox.WrapWAV(pcm, audiox.PCMFormat{SampleRate: sampleRate, Channels: 1, BitsPerSample: 16})
}
