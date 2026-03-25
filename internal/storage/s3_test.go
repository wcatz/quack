package storage

import (
	"testing"
)

func TestContentHash(t *testing.T) {
	data := []byte("hello world")
	hash := ContentHash(data)

	if len(hash) != 12 {
		t.Errorf("ContentHash() len = %d, want 12", len(hash))
	}

	// Same data should produce same hash
	hash2 := ContentHash(data)
	if hash != hash2 {
		t.Errorf("ContentHash() not deterministic: %q != %q", hash, hash2)
	}

	// Different data should produce different hash
	hash3 := ContentHash([]byte("different data"))
	if hash == hash3 {
		t.Error("ContentHash() same for different data")
	}
}

func TestObjectKey(t *testing.T) {
	tests := []struct {
		hash string
		ext  string
		want string
	}{
		{"abc123def456", ".jpg", "images/abc123def456.jpg"},
		{"abc123def456", ".png", "images/abc123def456.png"},
		{"abc123def456", ".webp", "images/abc123def456.webp"},
		{"abc123def456", ".gif", "gifs/abc123def456.gif"},
	}

	for _, tt := range tests {
		got := ObjectKey(tt.hash, tt.ext)
		if got != tt.want {
			t.Errorf("ObjectKey(%q, %q) = %q, want %q", tt.hash, tt.ext, got, tt.want)
		}
	}
}
