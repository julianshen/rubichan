package tools

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRingBufferWriteAndRead(t *testing.T) {
	rb := NewRingBuffer(64)
	n, err := rb.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, []byte("hello"), rb.Bytes())
}

func TestRingBufferWrapAround(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte("abcdefgh")) // fills exactly
	rb.Write([]byte("ij"))       // overwrites first 2 bytes
	got := rb.Bytes()
	assert.Equal(t, []byte("cdefghij"), got)
}

func TestRingBufferOverflow(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]byte("abcdefghij")) // 10 bytes into 4-byte buffer
	got := rb.Bytes()
	assert.Equal(t, []byte("ghij"), got) // only last 4 bytes survive
}

func TestRingBufferLen(t *testing.T) {
	rb := NewRingBuffer(16)
	assert.Equal(t, 0, rb.Len())
	rb.Write([]byte("hello"))
	assert.Equal(t, 5, rb.Len())
	rb.Write([]byte("worldworld12")) // 12 more, total 17 > cap 16
	assert.Equal(t, 16, rb.Len())
}

func TestRingBufferReset(t *testing.T) {
	rb := NewRingBuffer(16)
	rb.Write([]byte("data"))
	rb.Reset()
	assert.Equal(t, 0, rb.Len())
	assert.Empty(t, rb.Bytes())
}

// TestRingBufferConcurrentAccess exercises the mutex under contention.
// Run with -race to verify thread safety.
func TestRingBufferConcurrentAccess(t *testing.T) {
	rb := NewRingBuffer(64)
	var wg sync.WaitGroup
	const goroutines = 8
	const writes = 100

	// Multiple writers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				rb.Write([]byte("data"))
			}
		}()
	}

	// Concurrent readers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				_ = rb.Bytes()
				_ = rb.Len()
			}
		}()
	}

	wg.Wait()

	// Buffer should contain data and not have panicked.
	assert.True(t, rb.Len() > 0)
	assert.True(t, rb.Len() <= 64)
}
