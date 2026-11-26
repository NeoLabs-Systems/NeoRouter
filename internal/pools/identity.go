package pools

// Each person taking part in a pool has a signing key pair (Ed25519). Partners
// recognise each other by the public key, not by the address, and every control
// message (join, introduction, member list, leaving) is signed with it. A
// second key pair (X25519) seals what travels through a pool's host
// (relay_seal.go); its public half is signed with the pool key, so partners
// know it belongs to this person. Both private keys are stored sealed with the
// instance's master key.

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

// boxKeyLabel prefixes the text a member signs to vouch for its sealing key.
const boxKeyLabel = "neorouter-pool-box:"

// boxKeyText is the shape of a base64 X25519 public key as partners see it.
var boxKeyText = regexp.MustCompile(`^[A-Za-z0-9+/=]{40,120}$`)

// identity is one person's pool identity.
type identity struct {
	UserID        string
	MemberID      string
	PublicKey     string // base64 DER (spki) Ed25519
	PrivateKey    ed25519.PrivateKey
	BoxKey        string // base64 DER (spki) X25519, "" before relaying existed
	BoxPrivateKey *ecdh.PrivateKey
}

// identityOf reads one person's identity, nil when they have none yet.
func (s *Service) identityOf(ctx context.Context, userID string) (*identity, error) {
	s.mu.Lock()
	cached, ok := s.identities[userID]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	row := s.DB.R.QueryRowContext(ctx, `SELECT user_id, member_id, public_key, private_key_encrypted,
		box_public_key, box_private_key_encrypted FROM pool_identities WHERE user_id = ?`, userID)
	record, err := s.scanIdentity(row.Scan)
	if err != nil {
		if errors.Is(err, errNoRows) {
			return nil, nil
		}
		return nil, err
	}
	s.mu.Lock()
	s.identities[userID] = record
	s.mu.Unlock()
	return record, nil
}

// errNoRows marks a lookup that found nothing, without importing database/sql
// at every call site.
var errNoRows = errors.New("no rows")

// scanIdentity reads one pool_identities row.
func (s *Service) scanIdentity(scan func(...any) error) (*identity, error) {
	var (
		record               identity
		boxKey, boxEncrypted *string
		encrypted            string
	)
	if err := scan(&record.UserID, &record.MemberID, &record.PublicKey, &encrypted, &boxKey, &boxEncrypted); err != nil {
		if isNoRows(err) {
			return nil, errNoRows
		}
		return nil, fmt.Errorf("pools: read pool identity: %w", err)
	}
	plain, err := s.unseal(encrypted)
	if err != nil {
		return nil, err
	}
	record.PrivateKey, err = ed25519PrivateKeyFrom(plain)
	if err != nil {
		return nil, err
	}
	if boxKey != nil && *boxKey != "" {
		record.BoxKey = *boxKey
	}
	if boxEncrypted != nil && *boxEncrypted != "" {
		boxPlain, err := s.unseal(*boxEncrypted)
		if err != nil {
			return nil, err
		}
		record.BoxPrivateKey, err = x25519PrivateKeyFrom(boxPlain)
		if err != nil {
			return nil, err
		}
	}
	return &record, nil
}

// ensureIdentity is this person's identity, made on first use.
func (s *Service) ensureIdentity(ctx context.Context, userID string) (*identity, error) {
	existing, err := s.identityOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.withBoxKey(ctx, existing)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pools: generate the pool key: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return nil, fmt.Errorf("pools: encode the pool key: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, fmt.Errorf("pools: encode the pool key: %w", err)
	}
	encrypted, err := s.seal(base64.StdEncoding.EncodeToString(privateDER))
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO pool_identities (user_id, member_id, public_key, private_key_encrypted) VALUES (?, ?, ?, ?)`,
		userID, uuid.NewString(), base64.StdEncoding.EncodeToString(publicDER), encrypted); err != nil {
		return nil, fmt.Errorf("pools: store the pool identity: %w", err)
	}
	s.mu.Lock()
	delete(s.identities, userID)
	s.mu.Unlock()
	added, err := s.identityOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.withBoxKey(ctx, added)
}

// withBoxKey adds the sealing key pair to an identity made before it existed.
func (s *Service) withBoxKey(ctx context.Context, record *identity) (*identity, error) {
	if record.BoxKey != "" && record.BoxPrivateKey != nil {
		return record, nil
	}
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pools: generate the sealing key: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(private.PublicKey())
	if err != nil {
		return nil, fmt.Errorf("pools: encode the sealing key: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, fmt.Errorf("pools: encode the sealing key: %w", err)
	}
	encrypted, err := s.seal(base64.StdEncoding.EncodeToString(privateDER))
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`UPDATE pool_identities SET box_public_key = ?, box_private_key_encrypted = ? WHERE user_id = ? AND box_public_key IS NULL`,
		base64.StdEncoding.EncodeToString(publicDER), encrypted, record.UserID); err != nil {
		return nil, fmt.Errorf("pools: store the sealing key: %w", err)
	}
	s.mu.Lock()
	delete(s.identities, record.UserID)
	s.mu.Unlock()
	reloaded, err := s.identityOf(ctx, record.UserID)
	if err != nil {
		return nil, err
	}
	if reloaded == nil {
		return nil, errors.New("pools: pool identity vanished")
	}
	return reloaded, nil
}

// userForMember is whose identity a member id belongs to, "" when nobody's.
func (s *Service) userForMember(ctx context.Context, memberID string) (string, error) {
	var userID string
	err := s.DB.R.QueryRowContext(ctx, `SELECT user_id FROM pool_identities WHERE member_id = ?`, memberID).Scan(&userID)
	if err != nil {
		if isNoRows(err) {
			return "", nil
		}
		return "", fmt.Errorf("pools: read the member's account: %w", err)
	}
	return userID, nil
}

// sign signs bytes with this person's pool key.
func (s *Service) sign(ctx context.Context, userID, text string) (string, error) {
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(record.PrivateKey, []byte(text))), nil
}

// verify reports whether signature over text was made by publicKey's holder.
func verifySignature(publicKey, text, signature string) bool {
	raw, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return false
	}
	parsed, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return false
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(key, []byte(text), sig)
}

// boxCard is this person's sealing key and its signature by their pool key, as
// partners receive them.
func (s *Service) boxCard(ctx context.Context, userID string) (boxKey, boxSig string, err error) {
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return "", "", err
	}
	signature, err := s.sign(ctx, userID, boxKeyLabel+record.BoxKey)
	if err != nil {
		return "", "", err
	}
	return record.BoxKey, signature, nil
}

// verifyBoxKey reports whether boxSig shows that boxKey belongs to the holder
// of publicKey.
func verifyBoxKey(publicKey, boxKey, boxSig string) bool {
	if !boxKeyText.MatchString(boxKey) {
		return false
	}
	return verifySignature(publicKey, boxKeyLabel+boxKey, boxSig)
}

// seal and unseal protect a private key at rest.
func (s *Service) seal(plain string) (string, error) {
	if s.Seal == nil {
		return "", errors.New("pools: no sealer configured")
	}
	return s.Seal(plain)
}

func (s *Service) unseal(sealed string) (string, error) {
	if s.Unseal == nil {
		return "", errors.New("pools: no sealer configured")
	}
	return s.Unseal(sealed)
}

// ed25519PrivateKeyFrom parses a base64 PKCS8 DER Ed25519 private key, the form
// pool_identities stores, including rows from earlier releases.
func ed25519PrivateKeyFrom(encoded string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("pools: decode the pool key: %w", err)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("pools: parse the pool key: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("pools: not a pool key")
	}
	return key, nil
}

// x25519PrivateKeyFrom parses a base64 PKCS8 DER X25519 private key.
func x25519PrivateKeyFrom(encoded string) (*ecdh.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("pools: decode the sealing key: %w", err)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("pools: parse the sealing key: %w", err)
	}
	key, ok := parsed.(*ecdh.PrivateKey)
	if !ok {
		return nil, errors.New("pools: not a sealing key")
	}
	return key, nil
}
