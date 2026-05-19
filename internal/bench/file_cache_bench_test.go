package bench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
)

func BenchmarkFileReadCache_Hit(b *testing.B) {
	tmpDir := b.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(tmpFile, []byte("hello world"), 0o644)

	cache := tools.NewFileReadCache()
	info, _ := os.Stat(tmpFile)
	cache.Put(tmpFile, info, "hello world")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(tmpFile)
	}
}

func BenchmarkFileReadCache_Miss(b *testing.B) {
	tmpDir := b.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(tmpFile, []byte("hello world"), 0o644)

	cache := tools.NewFileReadCache()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(tmpFile)
	}
}

func BenchmarkFileReadCache_Stale(b *testing.B) {
	tmpDir := b.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(tmpFile, []byte("hello world"), 0o644)

	cache := tools.NewFileReadCache()
	info, _ := os.Stat(tmpFile)
	cache.Put(tmpFile, info, "hello world")

	// Modify file to invalidate cache.
	_ = os.WriteFile(tmpFile, []byte("modified content"), 0o644)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(tmpFile)
	}
}
