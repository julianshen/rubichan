package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileReadCache_Basic(t *testing.T) {
	cache := NewFileReadCache()

	// Create a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// First read: cache miss
	_, hit := cache.Get(tmpFile)
	if hit {
		t.Error("expected cache miss on first read")
	}

	// Store in cache
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	cache.Put(tmpFile, info, "hello")

	// Second read: cache hit
	content, hit := cache.Get(tmpFile)
	if !hit {
		t.Error("expected cache hit")
	}
	if content != "hello" {
		t.Errorf("expected 'hello', got %q", content)
	}

	// Modify file: cache should detect staleness
	if err := os.WriteFile(tmpFile, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	_, hit = cache.Get(tmpFile)
	if hit {
		t.Error("expected cache miss after file modification")
	}
}

func TestFileReadCache_Invalidate(t *testing.T) {
	cache := NewFileReadCache()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(tmpFile)
	cache.Put(tmpFile, info, "hello")

	// Invalidate and verify miss
	cache.Invalidate(tmpFile)
	_, hit := cache.Get(tmpFile)
	if hit {
		t.Error("expected cache miss after invalidate")
	}
}

func TestFileReadCache_FileDisappears(t *testing.T) {
	cache := NewFileReadCache()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(tmpFile)
	cache.Put(tmpFile, info, "hello")

	// Delete file
	os.Remove(tmpFile)

	_, hit := cache.Get(tmpFile)
	if hit {
		t.Error("expected cache miss after file deletion")
	}
}
