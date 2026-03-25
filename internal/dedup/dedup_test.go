package dedup

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, err := Open(dbPath, logger)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSeenAndMark(t *testing.T) {
	store := testStore(t)

	if store.Seen("twitter", "123") {
		t.Error("Seen() = true for unmarked item")
	}

	if err := store.Mark("twitter", "123", "images/abc123.jpg"); err != nil {
		t.Fatalf("Mark() error: %v", err)
	}

	if !store.Seen("twitter", "123") {
		t.Error("Seen() = false after Mark()")
	}

	// Different source, same ID — should not be seen
	if store.Seen("reddit", "123") {
		t.Error("Seen() = true for different source")
	}
}

func TestCounts(t *testing.T) {
	store := testStore(t)

	images, gifs := store.Counts()
	if images != 0 || gifs != 0 {
		t.Errorf("initial Counts() = (%d, %d), want (0, 0)", images, gifs)
	}

	for i := 0; i < 3; i++ {
		if err := store.IncrementCount("images"); err != nil {
			t.Fatalf("IncrementCount(images) error: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := store.IncrementCount("gifs"); err != nil {
			t.Fatalf("IncrementCount(gifs) error: %v", err)
		}
	}

	images, gifs = store.Counts()
	if images != 3 {
		t.Errorf("images = %d, want 3", images)
	}
	if gifs != 2 {
		t.Errorf("gifs = %d, want 2", gifs)
	}
}

func TestAllKeys(t *testing.T) {
	store := testStore(t)

	keys := store.AllKeys()
	if len(keys) != 0 {
		t.Errorf("initial AllKeys() len = %d, want 0", len(keys))
	}

	store.Mark("src", "1", "images/aaa.jpg")
	store.Mark("src", "2", "gifs/bbb.gif")
	store.Mark("src", "3", "images/ccc.png")

	keys = store.AllKeys()
	if len(keys) != 3 {
		t.Errorf("AllKeys() len = %d, want 3", len(keys))
	}
}

func TestDuplicateMark(t *testing.T) {
	store := testStore(t)

	store.Mark("src", "1", "images/aaa.jpg")
	// Re-marking same source+id with same key should not error
	if err := store.Mark("src", "1", "images/aaa.jpg"); err != nil {
		t.Errorf("duplicate Mark() error: %v", err)
	}

	// Still only one key in index
	keys := store.AllKeys()
	if len(keys) != 1 {
		t.Errorf("AllKeys() len = %d after duplicate Mark, want 1", len(keys))
	}
}
