package pools

// The wire format between pool partners (protocol.js). Every control message is
// an envelope { body, signature }:
//
//	body = { v, type, from, to, ts, nonce, data }
//	signature = Ed25519 over the canonical JSON of body (sorted keys)
//
// A message is accepted only with the right type and version, a timestamp within
// a few minutes and a nonce not seen before (replays fail), and a signature by
// the key the receiver expects.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// ProtocolVersion is the pool protocol version both sides speak.
const ProtocolVersion = 2

// maxSkewMS is how far a message's timestamp may be from ours.
const maxSkewMS = 5 * 60_000

// peerEnvelope is one signed control message.
type peerEnvelope struct {
	Body      json.RawMessage `json:"body"`
	Signature string          `json:"signature"`
}

// peerBody is the checked envelope body.
type peerBody struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	From    string          `json:"from"`
	To      *string         `json:"to"`
	TS      int64           `json:"ts"`
	Nonce   string          `json:"nonce"`
	Data    json.RawMessage `json:"data"`
	raw     map[string]any
	decoded map[string]any
}

// data decodes the body's data object, an empty one when it carries none.
func (b *peerBody) data() map[string]any {
	if b.decoded != nil {
		return b.decoded
	}
	b.decoded = map[string]any{}
	if len(b.Data) > 0 && string(b.Data) != "null" {
		decoded, err := decodeJSON(b.Data)
		if err == nil {
			if object, ok := decoded.(map[string]any); ok {
				b.decoded = object
			}
		}
	}
	return b.decoded
}

// canonical is JSON with object keys sorted at every level, so both sides sign
// the same bytes.
func canonical(value any) (string, error) {
	var b strings.Builder
	if err := writeCanonical(&b, value); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writeCanonical(b *strings.Builder, value any) error {
	switch typed := value.(type) {
	case nil:
		b.WriteString("null")
		return nil
	case []any:
		b.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		return nil
	case map[string]any:
		// JSON.stringify drops undefined keys, never null; a decoded body has no
		// undefined, so every key is written.
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsQuote(key))
			b.WriteByte(':')
			if err := writeCanonical(b, typed[key]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		return nil
	case string:
		b.WriteString(jsQuote(typed))
		return nil
	case bool:
		if typed {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		return nil
	case json.Number:
		b.WriteString(jsNumber(string(typed)))
		return nil
	case float64:
		b.WriteString(jsNumber(strconv.FormatFloat(typed, 'g', -1, 64)))
		return nil
	case int:
		b.WriteString(strconv.Itoa(typed))
		return nil
	case int64:
		b.WriteString(strconv.FormatInt(typed, 10))
		return nil
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		decoded, err := decodeJSON(encoded)
		if err != nil {
			return err
		}
		return writeCanonical(b, decoded)
	}
}

// jsQuote renders a string the way JSON.stringify does: quotes, backslashes and
// control characters escaped, everything else as it is (Go's encoder would also
// escape <, > and &, which Node leaves alone).
func jsQuote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsNumber renders a number literal the way JSON.stringify does: no fraction or
// exponent for a whole number, shortest form otherwise.
func jsNumber(literal string) string {
	if value, err := strconv.ParseInt(literal, 10, 64); err == nil {
		return strconv.FormatInt(value, 10)
	}
	value, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return literal
	}
	if value == math.Trunc(value) && math.Abs(value) < 1e21 {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// decodeJSON decodes JSON with numbers kept as written.
func decodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// envelope signs one control message for one recipient.
func (s *Service) envelope(ctx context.Context, userID, messageType string, data any, to *string) (*peerEnvelope, error) {
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("pools: make a message nonce: %w", err)
	}
	body := map[string]any{
		"v":     ProtocolVersion,
		"type":  messageType,
		"from":  record.MemberID,
		"to":    to,
		"ts":    s.nowMS(),
		"nonce": base64.RawURLEncoding.EncodeToString(nonce),
		"data":  data,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("pools: encode a pool message: %w", err)
	}
	decoded, err := decodeJSON(encoded)
	if err != nil {
		return nil, fmt.Errorf("pools: encode a pool message: %w", err)
	}
	text, err := canonical(decoded)
	if err != nil {
		return nil, err
	}
	signature, err := s.sign(ctx, userID, text)
	if err != nil {
		return nil, err
	}
	return &peerEnvelope{Body: encoded, Signature: signature}, nil
}

// rememberNonce records a message nonce, false when it was seen before.
func (s *Service) rememberNonce(nonce string, now int64) bool {
	s.nonceMu.Lock()
	defer s.nonceMu.Unlock()
	for value, at := range s.nonces {
		if now-at > maxSkewMS*2 {
			delete(s.nonces, value)
		}
	}
	if _, ok := s.nonces[nonce]; ok {
		return false
	}
	s.nonces[nonce] = now
	return true
}

// reject is the error a message that fails its check answers with.
func reject(message string) *HttpError {
	return fail(http.StatusUnauthorized, "POOL_SIGNATURE_INVALID", message)
}

// openEnvelope checks an envelope: version, type, freshness, nonce and signature
// (by publicKey), and returns the body.
func (s *Service) openEnvelope(envelope *peerEnvelope, messageType, publicKey string, now int64) (*peerBody, error) {
	if envelope == nil || len(envelope.Body) == 0 {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	raw, err := decodeJSON(envelope.Body)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	var body peerBody
	if err := json.Unmarshal(envelope.Body, &body); err != nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	if body.V != ProtocolVersion {
		return nil, fail(http.StatusBadRequest, "POOL_VERSION_MISMATCH",
			"The other NeoRouter speaks another pool protocol version. Update both to the latest release.")
	}
	if body.Type != messageType {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "Unexpected message type.")
	}
	if diff := now - body.TS; diff > maxSkewMS || diff < -maxSkewMS {
		return nil, reject("The message is too old, or the clocks of the two servers differ by more than five minutes.")
	}
	text, err := canonical(object)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	if !verifySignature(publicKey, text, envelope.Signature) {
		return nil, reject("The signature does not match.")
	}
	if body.Nonce == "" || !s.rememberNonce(body.Nonce, now) {
		return nil, reject("This message was already received.")
	}
	body.raw = object
	return &body, nil
}

// signObject signs a plain object (introduction tickets, member lists).
func (s *Service) signObject(ctx context.Context, userID string, value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("pools: encode a signed object: %w", err)
	}
	decoded, err := decodeJSON(encoded)
	if err != nil {
		return "", fmt.Errorf("pools: encode a signed object: %w", err)
	}
	text, err := canonical(decoded)
	if err != nil {
		return "", err
	}
	return s.sign(ctx, userID, text)
}

// verifyObject reports whether signature over value was made by publicKey's
// holder.
func verifyObject(publicKey string, value any, signature string) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	decoded, err := decodeJSON(encoded)
	if err != nil {
		return false
	}
	text, err := canonical(decoded)
	if err != nil {
		return false
	}
	return verifySignature(publicKey, text, signature)
}
