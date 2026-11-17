package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kelvins-io/eino-bot/internal/store"
)

func TestFactDedup(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	ctx := context.Background()

	r1, err := svc.UpsertFact(ctx, "u1", "skill", "熟悉 Go 与 Eino", "chat", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Inserted || r1.Duplicate {
		t.Fatalf("first insert should succeed: %+v", r1)
	}

	r2, err := svc.UpsertFact(ctx, "u1", "skill", "熟悉 go 与 eino。", "chat", 1)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Inserted || !r2.Duplicate {
		t.Fatalf("normalized duplicate should be skipped: %+v", r2)
	}

	p, err := svc.GetProfile(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(p.Facts))
	}
}
