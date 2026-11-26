package pools

// The frames on a relay connection (JSON text over a WebSocket), and the limits
// both ends hold each other to (relay/frames.js).
//
// Logging in (between a member and the host):
//
//	host → member   { t: 'challenge', nonce }
//	member → host   { t: 'hello', pool, member, sig }
//	host → member   { t: 'ready', online: [memberId] }  then { t: 'presence', member, online }
//
// A stream (one request and its answer), passed along by the host:
//
//	asker → host → answerer   { t: 'open', s, to | from, seq, d }
//	answerer → asker          { t: 'head', s, seq, d, r } then 'data' / 'end'
//	either side               { t: 'cancel', s } / { t: 'fail', s, code }
//
// d is always sealed between the two members (relay_seal.go); the host only
// reads t, s, to/from and seq.

import (
	"encoding/json"
	"errors"
)

// relayLimits mirrors LIMITS of relay/frames.js.
var relayLimits = struct {
	FrameBytes           int
	RequestBodyBytes     int
	ChunkBytes           int
	StreamsPerConnection int
	StreamIdleMS         int64
	RequestSkewMS        int64
	BufferedBytes        int
	HelloBytes           int
	HelloMS              int64
	HeartbeatMS          int64
}{
	FrameBytes:           12 * 1024 * 1024,
	RequestBodyBytes:     8 * 1024 * 1024,
	ChunkBytes:           64 * 1024,
	StreamsPerConnection: 64,
	StreamIdleMS:         10 * 60_000,
	RequestSkewMS:        5 * 60_000,
	BufferedBytes:        16 * 1024 * 1024,
	HelloBytes:           4096,
	HelloMS:              10_000,
	HeartbeatMS:          25_000,
}

var (
	streamTypes  = []string{"open", "head", "data", "end", "cancel", "fail"}
	sealedTypes  = []string{"open", "head", "data", "end"}
	streamIDText = mustRegexp(`^[A-Za-z0-9_-]{22}$`)
	codeText     = mustRegexp(`^[a-z_]{1,40}$`)
)

// relayFrame is one parsed frame. It is a decoded JSON object, so an unknown
// field is carried along and a wrongly typed one is refused where it is read.
type relayFrame map[string]any

// helloText is the text a member signs to log in.
func helloText(nonce, poolID, memberID, hostMemberID string) string {
	return "neorouter-pool-relay-hello|" + nonce + "|" + poolID + "|" + memberID + "|" + hostMemberID
}

// parseRelayFrame decodes one text frame, or reports false when it is not a
// frame at all.
func parseRelayFrame(text string) (relayFrame, bool) {
	if len(text) > relayLimits.FrameBytes {
		return nil, false
	}
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return nil, false
	}
	frame, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	if _, ok := frame["t"].(string); !ok {
		return nil, false
	}
	return relayFrame(frame), true
}

// typ is the frame kind.
func (f relayFrame) typ() string {
	value, _ := f["t"].(string)
	return value
}

// text is one string field, reporting false when it is absent or not a string.
func (f relayFrame) text(key string) (string, bool) {
	value, ok := f[key].(string)
	return value, ok
}

// has reports whether the field is present at all.
func (f relayFrame) has(key string) bool {
	_, ok := f[key]
	return ok
}

// integer is one whole-number field; a fractional or out-of-range value is not
// one.
func (f relayFrame) integer(key string) (int64, bool) {
	value, ok := f[key].(float64)
	if !ok {
		// A caller that built the frame in Go hands over an int.
		switch typed := f[key].(type) {
		case int:
			return int64(typed), true
		case int64:
			return typed, true
		}
		return 0, false
	}
	if value != float64(int64(value)) {
		return 0, false
	}
	return int64(value), true
}

// boolean is one true/false field.
func (f relayFrame) boolean(key string) bool {
	value, ok := f[key].(bool)
	return ok && value
}

// members is a list of member ids.
func (f relayFrame) members(key string) []string {
	items, ok := f[key].([]any)
	if !ok {
		return nil
	}
	list := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			list = append(list, text)
		}
	}
	return list
}

func isUUID(value string) bool { return relayUUID.MatchString(value) }

// validStreamFrame reports whether a stream frame is well formed (ids, sequence
// number, sealed data where needed).
func validStreamFrame(frame relayFrame) error {
	kind := frame.typ()
	if !containsString(streamTypes, kind) {
		return errors.New("unexpected frame type")
	}
	stream, ok := frame.text("s")
	if !ok || !streamIDText.MatchString(stream) {
		return errors.New("bad stream id")
	}
	if to, present := frame.text("to"); present && !isUUID(to) {
		return errors.New("bad to")
	} else if frame.has("to") && !present {
		return errors.New("bad to")
	}
	if from, present := frame.text("from"); present && !isUUID(from) {
		return errors.New("bad from")
	} else if frame.has("from") && !present {
		return errors.New("bad from")
	}
	if kind == "head" {
		if salt, ok := frame.text("r"); !ok || !streamIDText.MatchString(salt) {
			return errors.New("bad r")
		}
	}
	if containsString(sealedTypes, kind) {
		seq, ok := frame.integer("seq")
		if !ok || seq < 0 || seq >= 1<<31 {
			return errors.New("bad seq")
		}
		if _, ok := frame.text("d"); !ok {
			return errors.New("bad d")
		}
		return nil
	}
	if code, present := frame.text("code"); present && !codeText.MatchString(code) {
		return errors.New("bad code")
	} else if frame.has("code") && !present {
		return errors.New("bad code")
	}
	return nil
}

// Outgoing frames. Each mirrors one object literal of the Node code, so the
// JSON both ends exchange is the same.

type challengeFrame struct {
	T     string `json:"t"`
	Nonce string `json:"nonce"`
}

type readyFrame struct {
	T      string   `json:"t"`
	Online []string `json:"online"`
}

type presenceFrame struct {
	T      string `json:"t"`
	Member string `json:"member"`
	Online bool   `json:"online"`
}

type helloFrame struct {
	T      string `json:"t"`
	Pool   string `json:"pool"`
	Member string `json:"member"`
	Sig    string `json:"sig"`
}

// streamFrame is one sealed stream frame ('open', 'head', 'data', 'end').
type streamFrame struct {
	T    string `json:"t"`
	S    string `json:"s"`
	To   string `json:"to,omitempty"`
	From string `json:"from,omitempty"`
	Seq  uint64 `json:"seq"`
	D    string `json:"d"`
	R    string `json:"r,omitempty"`
}

// endFrame ends a stream ('cancel', 'fail'), in the clear.
type endFrame struct {
	T    string `json:"t"`
	S    string `json:"s"`
	Code string `json:"code,omitempty"`
}

// encodeFrame renders a frame as the text a WebSocket carries.
func encodeFrame(frame any) string {
	encoded, err := json.Marshal(frame)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// forwardedFrame rebuilds the frame the host passes on to the other side of a
// stream: the sealed fields for a sealed type, the clear ones for an end.
func forwardedFrame(frame relayFrame) (any, error) {
	kind := frame.typ()
	stream, _ := frame.text("s")
	if kind == "cancel" || kind == "fail" {
		out := endFrame{T: kind, S: stream}
		if code, ok := frame.text("code"); ok {
			out.Code = code
		}
		return out, nil
	}
	seq, ok := frame.integer("seq")
	if !ok {
		return nil, errors.New("pool relay: frame has no sequence number")
	}
	d, ok := frame.text("d")
	if !ok {
		return nil, errors.New("pool relay: frame has no sealed body")
	}
	out := streamFrame{T: kind, S: stream, Seq: uint64(seq), D: d}
	if salt, ok := frame.text("r"); ok {
		out.R = salt
	}
	return out, nil
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
