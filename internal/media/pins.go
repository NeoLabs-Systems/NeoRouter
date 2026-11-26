package media

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Vectors from two embedding models, or one model at two sizes, live in
// different spaces: mixing them silently breaks every similarity search. So
// "auto" embeddings pin a key to the first model (and dimension) it used, and
// fallback then only moves between providers serving that same model.

// pin is one key's pinned embedding model.
type pin struct {
	CanonicalID string
	Dimensions  *int
}

// embeddingPin reads the key's pinned embedding model, nil when it has none.
func (s *Service) embeddingPin(ctx context.Context, keyID string) (*pin, error) {
	var canonicalID string
	var dimensions sql.NullInt64
	err := s.db.R.QueryRowContext(ctx,
		`SELECT canonical_id, dimensions FROM embedding_pins WHERE router_key_id = ?`, keyID).
		Scan(&canonicalID, &dimensions)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("media: read embedding pin: %w", err)
	}
	found := &pin{CanonicalID: canonicalID}
	if dimensions.Valid {
		value := int(dimensions.Int64)
		found.Dimensions = &value
	}
	return found, nil
}

// setEmbeddingPin pins the key to the first model (and size) an auto request
// used. A key that is already pinned keeps its pin.
func (s *Service) setEmbeddingPin(ctx context.Context, keyID, canonicalID string, dimensions *int) error {
	var value any
	if dimensions != nil {
		value = *dimensions
	}
	if _, err := s.db.W.ExecContext(ctx,
		`INSERT INTO embedding_pins (router_key_id, canonical_id, dimensions, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(router_key_id) DO NOTHING`, keyID, canonicalID, value, s.now().UnixMilli()); err != nil {
		return fmt.Errorf("media: pin embedding model: %w", err)
	}
	return nil
}

// clearEmbeddingPin releases the key's pin, reporting whether it had one.
func (s *Service) clearEmbeddingPin(ctx context.Context, keyID string) (bool, error) {
	result, err := s.db.W.ExecContext(ctx, `DELETE FROM embedding_pins WHERE router_key_id = ?`, keyID)
	if err != nil {
		return false, fmt.Errorf("media: clear embedding pin: %w", err)
	}
	cleared, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("media: clear embedding pin: %w", err)
	}
	return cleared > 0, nil
}

// embeddingPinMismatch is the 400 an auto request gets when its requested size
// differs from the size the key is pinned to.
func embeddingPinMismatch(pinned *pin) error {
	dimensions := 0
	if pinned.Dimensions != nil {
		dimensions = *pinned.Dimensions
	}
	return httpserver.New(http.StatusBadRequest, "EMBEDDING_PIN_MISMATCH",
		fmt.Sprintf("This key is pinned to %s at %d dimensions; vectors of another size would not be comparable. Ask for %d dimensions or reset the pin.",
			pinned.CanonicalID, dimensions, dimensions))
}

// embeddingPinUnavailable is the 503 an auto request gets when nothing can serve
// the model the key is pinned to.
func embeddingPinUnavailable(pinned *pin) error {
	return httpserver.New(http.StatusServiceUnavailable, "EMBEDDING_PIN_UNAVAILABLE",
		fmt.Sprintf("This key is pinned to the embedding model %s, and no provider can serve it right now. Mixing in another model would make your vectors incomparable; reset the pin to switch models (and re-embed your data).", pinned.CanonicalID))
}
