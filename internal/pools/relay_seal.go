package pools

// What travels through a pool's host is sealed between the two members, so the
// host passes it along without being able to read or change it.
//
//   - Each pair of members shares a key: X25519 between their sealing keys,
//     stretched with HKDF and bound to the pool and both member ids.
//   - Each stream (one request and its answer) gets two keys from it, one per
//     direction: the request's from the asker's random stream id, the answer's
//     from that and fresh randomness of the answerer's own.
//   - Every frame is AES-256-GCM with its sequence number as the nonce, and the
//     pool, sender, receiver, stream, number and frame type as associated data.
//
// The seal is not neolabs-go's secretbox or filebox layout (it is a hand-rolled
// X25519 + HKDF + AES-256-GCM with sequence-number nonces and per-direction
// keys), so it is written here on the standard library, and the frame bytes
// must stay identical for partners on any version.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	sealKeyBytes = 32
	sealTagBytes = 16
	// hkdfPairLabel binds the pair key to the two members; hkdfDirLabel to one
	// direction of one stream.
	hkdfPairLabel = "neorouter pool relay v1|"
	hkdfDirLabel  = "direction|"
	// errSealAuth is the error when a frame does not open. The text reaches a
	// member's error message, so partners on any version expect it.
	errSealAuth = "Unsupported state or unable to authenticate data"
	// sealPairCacheLimit bounds the derived pair keys kept in memory.
	sealPairCacheLimit = 1000
)

// sealingKey is one member's own half of the X25519 pair (ensureIdentity).
type sealingKey struct {
	PrivateKey  *ecdh.PrivateKey
	OwnBoxKey   string
	OwnMemberID string
}

// sealPartner is the other member's public sealing key.
type sealPartner struct {
	MemberID string
	BoxKey   string
}

// sealingPublicKeyFrom parses a member's base64 DER (spki) sealing key.
func sealingPublicKeyFrom(boxKey string) (*ecdh.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(boxKey))
	if err != nil {
		return nil, errors.New("Not a sealing key.")
	}
	parsed, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, errors.New("Not a sealing key.")
	}
	key, ok := parsed.(*ecdh.PublicKey)
	if !ok {
		return nil, errors.New("Not a sealing key.")
	}
	return key, nil
}

// pairKey is the key two members share in one pool: the same on both sides.
func (s *Service) pairKey(own sealingKey, partner sealPartner, poolID string) ([]byte, error) {
	id := strings.Join([]string{poolID, own.OwnMemberID, own.OwnBoxKey, partner.MemberID, partner.BoxKey}, "|")
	s.sealMu.Lock()
	if cached, ok := s.sealPairs[id]; ok {
		s.sealMu.Unlock()
		return cached, nil
	}
	s.sealMu.Unlock()

	public, err := sealingPublicKeyFrom(partner.BoxKey)
	if err != nil {
		return nil, err
	}
	shared, err := own.PrivateKey.ECDH(public)
	if err != nil {
		return nil, fmt.Errorf("pool relay: agree on the pair key: %w", err)
	}
	ids := []string{strings.ToLower(own.OwnMemberID), strings.ToLower(partner.MemberID)}
	sort.Strings(ids)
	key, err := hkdf.Key(sha256.New, shared, []byte(poolID), hkdfPairLabel+strings.Join(ids, "|"), sealKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("pool relay: derive the pair key: %w", err)
	}
	s.sealMu.Lock()
	if len(s.sealPairs) > sealPairCacheLimit {
		s.sealPairs = map[string][]byte{}
	}
	s.sealPairs[id] = key
	s.sealMu.Unlock()
	return key, nil
}

// nonceFor is a frame's sequence number as the GCM nonce.
func nonceFor(seq uint64) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], seq)
	return nonce
}

// direction seals and opens one direction of one stream. from and to are member
// ids; seq starts at 0 and goes up by one with every frame.
type direction struct {
	aead   cipher.AEAD
	prefix string
	sent   uint64
	got    uint64
}

// directionFor derives the key of one direction of one stream.
func directionFor(pair []byte, poolID, streamID, from, to, salt string) (*direction, error) {
	key, err := hkdf.Key(sha256.New, pair,
		[]byte(streamID+"|"+salt), hkdfDirLabel+from+">"+to, sealKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("pool relay: derive a direction key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("pool relay: open the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("pool relay: open the cipher: %w", err)
	}
	return &direction{aead: aead, prefix: poolID + "|" + from + "|" + to + "|" + streamID + "|"}, nil
}

// aad is the associated data of one frame: where it belongs and what it is.
func (d *direction) aad(seq uint64, frameType string) []byte {
	return []byte(d.prefix + strconv.FormatUint(seq, 10) + "|" + frameType)
}

// sealFrame seals payload as a frame of frameType and returns its sequence
// number and base64 body.
func (d *direction) sealFrame(frameType string, payload []byte) (uint64, string) {
	seq := d.sent
	d.sent++
	body := d.aead.Seal(nil, nonceFor(seq), payload, d.aad(seq, frameType))
	return seq, base64.StdEncoding.EncodeToString(body)
}

// openFrame opens frame seq of frameType. Frames that are replayed, reordered,
// moved to another stream or partner, or cut off before the sealed end do not
// open.
func (d *direction) openFrame(frameType string, seq uint64, data string) ([]byte, error) {
	if seq != d.got {
		return nil, errors.New("A relayed frame is out of order.")
	}
	if len(data) < sealTagBytes {
		return nil, errors.New("A relayed frame is malformed.")
	}
	body, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(body) < sealTagBytes {
		return nil, errors.New("A relayed frame is malformed.")
	}
	plain, err := d.aead.Open(nil, nonceFor(seq), body, d.aad(seq, frameType))
	if err != nil {
		return nil, errors.New(errSealAuth)
	}
	d.got++
	return plain, nil
}
