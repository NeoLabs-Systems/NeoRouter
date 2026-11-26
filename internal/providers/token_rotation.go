package providers

import (
	"context"
	"sync"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
)

// tokenRotation spends each single-use refresh token once. Concurrent callers
// share one refresh, and a caller that still holds a secret read before the
// rotation was stored gets the rotated credentials instead of spending the old
// token again.
type tokenRotation[T any] struct {
	flight transport.Flight[T]

	mu       sync.Mutex
	accounts map[string]*rotatedTokens[T]
}

// rotatedTokens is what is known about one connection's refreshes.
type rotatedTokens[T any] struct {
	spent  map[string]bool
	latest T
	stored bool
}

// rotateRequest is one caller's wish for valid credentials.
type rotateRequest[T any] struct {
	account string
	// token is the refresh token in the secret the caller holds.
	token   string
	current T
	// usable reports whether credentials can still be used without a refresh.
	usable func(T) bool
	spend  func(ctx context.Context, from T) (T, error)
	// refreshToken is the token a refresh of these credentials would spend.
	refreshToken func(T) string
	// store persists new credentials; nil stores nothing.
	store func(T) error
}

func (r *tokenRotation[T]) do(ctx context.Context, request rotateRequest[T]) (T, error) {
	return r.flight.Do(ctx, request.account+"\x00"+request.token, func() (T, error) {
		// A started refresh is finished and stored even if its caller leaves.
		ctx := context.WithoutCancel(ctx)
		var zero T
		r.mu.Lock()
		known := r.accounts[request.account]
		r.mu.Unlock()
		from := request.current
		if known != nil && known.spent[request.token] {
			if request.usable(known.latest) {
				return known.latest, r.persist(request, known)
			}
			from = known.latest
		} else {
			known = nil
		}
		next, err := request.spend(ctx, from)
		if err != nil {
			return zero, err
		}
		record := &rotatedTokens[T]{spent: map[string]bool{request.token: true, request.refreshToken(from): true}, latest: next}
		if known != nil {
			for token := range known.spent {
				record.spent[token] = true
			}
		}
		r.mu.Lock()
		if r.accounts == nil {
			r.accounts = map[string]*rotatedTokens[T]{}
		}
		r.accounts[request.account] = record
		r.mu.Unlock()
		return next, r.persist(request, record)
	})
}

// persist stores the rotated credentials unless that already succeeded; a failed
// store is retried by the next caller.
func (r *tokenRotation[T]) persist(request rotateRequest[T], record *rotatedTokens[T]) error {
	r.mu.Lock()
	stored := record.stored
	r.mu.Unlock()
	if stored || request.store == nil {
		return nil
	}
	if err := request.store(record.latest); err != nil {
		return err
	}
	r.mu.Lock()
	record.stored = true
	r.mu.Unlock()
	return nil
}
