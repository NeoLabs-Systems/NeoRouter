package media

import (
	"context"
	"testing"
)

func TestEmbeddingPins(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	pinned, err := env.svc.embeddingPin(ctx, env.key.ID)
	if err != nil || pinned != nil {
		t.Fatalf("a fresh key has no pin: %v %v", pinned, err)
	}
	if err := env.svc.setEmbeddingPin(ctx, env.key.ID, "openai/text-embedding-3-small", intPtr(4)); err != nil {
		t.Fatalf("pin: %v", err)
	}
	pinned, err = env.svc.embeddingPin(ctx, env.key.ID)
	if err != nil {
		t.Fatalf("read pin: %v", err)
	}
	if pinned == nil || pinned.CanonicalID != "openai/text-embedding-3-small" || pinned.Dimensions == nil || *pinned.Dimensions != 4 {
		t.Fatalf("pin = %+v", pinned)
	}
	// A second pin never replaces the first: the key stays on one model.
	if err := env.svc.setEmbeddingPin(ctx, env.key.ID, "other/model", intPtr(8)); err != nil {
		t.Fatalf("pin again: %v", err)
	}
	pinned, _ = env.svc.embeddingPin(ctx, env.key.ID)
	if pinned.CanonicalID != "openai/text-embedding-3-small" {
		t.Fatalf("pin = %+v, want the first model", pinned)
	}
	cleared, err := env.svc.clearEmbeddingPin(ctx, env.key.ID)
	if err != nil || !cleared {
		t.Fatalf("clear = %v %v", cleared, err)
	}
	if again, _ := env.svc.clearEmbeddingPin(ctx, env.key.ID); again {
		t.Fatal("clearing a key without a pin reports false")
	}
	// A pin without a size is stored too.
	if err := env.svc.setEmbeddingPin(ctx, env.key.ID, "other/model", nil); err != nil {
		t.Fatalf("pin without size: %v", err)
	}
	pinned, _ = env.svc.embeddingPin(ctx, env.key.ID)
	if pinned == nil || pinned.Dimensions != nil {
		t.Fatalf("pin = %+v, want no size", pinned)
	}
}

func TestEmbeddingPinErrors(t *testing.T) {
	mismatch := embeddingPinMismatch(&pin{CanonicalID: "openai/text-embedding-3-small", Dimensions: intPtr(4)})
	if got := mismatch.Error(); got != "This key is pinned to openai/text-embedding-3-small at 4 dimensions; vectors of another size would not be comparable. Ask for 4 dimensions or reset the pin." {
		t.Fatalf("mismatch = %q", got)
	}
	unavailable := embeddingPinUnavailable(&pin{CanonicalID: "openai/text-embedding-3-small"})
	if got := unavailable.Error(); got != "This key is pinned to the embedding model openai/text-embedding-3-small, and no provider can serve it right now. Mixing in another model would make your vectors incomparable; reset the pin to switch models (and re-embed your data)." {
		t.Fatalf("unavailable = %q", got)
	}
}
