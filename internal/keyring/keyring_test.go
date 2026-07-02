package keyring

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryKeyringRoundTrip(t *testing.T) {
	ctx := context.Background()
	var kr Keyring = NewMemory()

	if _, err := kr.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := kr.Set(ctx, "sk-brevitas-123"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := kr.Get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "sk-brevitas-123" {
		t.Fatalf("got %q", got)
	}

	if err := kr.Delete(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := kr.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
