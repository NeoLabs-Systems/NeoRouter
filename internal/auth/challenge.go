package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// challengeTTL is how long a sign-in challenge stays usable.
const challengeTTL = 10 * time.Minute

type challengeEntry struct {
	userID    string
	expiresAt time.Time
}

// challengeStore holds the login challenges between the password step and the
// code step: a token names an account for ten minutes. It is in memory, so a
// restart simply asks people to enter their password again.
type challengeStore struct {
	mu   sync.Mutex
	live map[string]challengeEntry
}

// createChallenge mints a challenge for userID, dropping expired ones.
func (s *Service) createChallenge(userID string) (string, error) {
	now := time.Now()
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: challenge token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	s.challenges.mu.Lock()
	defer s.challenges.mu.Unlock()
	for live, entry := range s.challenges.live {
		if !entry.expiresAt.After(now) {
			delete(s.challenges.live, live)
		}
	}
	if s.challenges.live == nil {
		s.challenges.live = make(map[string]challengeEntry)
	}
	s.challenges.live[token] = challengeEntry{userID: userID, expiresAt: now.Add(challengeTTL)}
	return token, nil
}

// consumeChallenge names the account a challenge belongs to. It does not use the
// challenge up: finishChallenge does that once the code was verified, so a wrong
// code can be retried within the challenge's life.
func (s *Service) consumeChallenge(token string) (string, error) {
	now := time.Now()
	s.challenges.mu.Lock()
	defer s.challenges.mu.Unlock()
	entry, ok := s.challenges.live[token]
	if !ok || !entry.expiresAt.After(now) {
		delete(s.challenges.live, token)
		return "", httpserver.New(http.StatusUnauthorized, "TWO_FACTOR_EXPIRED",
			"The sign-in took too long. Enter your password again.")
	}
	return entry.userID, nil
}

// finishChallenge uses up a challenge so the same one cannot sign in twice.
func (s *Service) finishChallenge(token string) {
	s.challenges.mu.Lock()
	defer s.challenges.mu.Unlock()
	delete(s.challenges.live, token)
}
