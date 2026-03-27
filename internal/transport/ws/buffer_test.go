package ws

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRingBuffer_Push_And_Since(t *testing.T) {
	buf := NewRingBuffer(5)

	for i := int64(1); i <= 5; i++ {
		buf.Push(BufferEntry{Seq: i, Payload: []byte(fmt.Sprintf(`{"seq":%d}`, i))})
	}

	assert.Equal(t, 5, buf.Len())

	// All entries since seq 0.
	all := buf.Since(0)
	require.Len(t, all, 5)
	for i, e := range all {
		assert.Equal(t, int64(i+1), e.Seq)
	}

	// Entries since seq 3.
	recent := buf.Since(3)
	require.Len(t, recent, 2)
	assert.Equal(t, int64(4), recent[0].Seq)
	assert.Equal(t, int64(5), recent[1].Seq)

	// Entries since seq 5 — nothing newer.
	empty := buf.Since(5)
	assert.Nil(t, empty)
}

func TestRingBuffer_Overwrite(t *testing.T) {
	buf := NewRingBuffer(3)

	// Push 5 entries into a size-3 buffer.
	for i := int64(1); i <= 5; i++ {
		buf.Push(BufferEntry{Seq: i, Payload: []byte(fmt.Sprintf("%d", i))})
	}

	assert.Equal(t, 3, buf.Len())
	assert.Equal(t, int64(3), buf.OldestSeq())
	assert.Equal(t, int64(5), buf.NewestSeq())

	all := buf.Since(0)
	require.Len(t, all, 3)
	assert.Equal(t, int64(3), all[0].Seq)
	assert.Equal(t, int64(4), all[1].Seq)
	assert.Equal(t, int64(5), all[2].Seq)
}

func TestRingBuffer_Empty(t *testing.T) {
	buf := NewRingBuffer(10)

	assert.Equal(t, 0, buf.Len())
	assert.Equal(t, int64(0), buf.OldestSeq())
	assert.Equal(t, int64(0), buf.NewestSeq())
	assert.Nil(t, buf.Since(0))
}

func TestRingBuffer_SingleCapacity(t *testing.T) {
	buf := NewRingBuffer(1)

	buf.Push(BufferEntry{Seq: 1, Payload: []byte("a")})
	assert.Equal(t, 1, buf.Len())
	assert.Equal(t, int64(1), buf.OldestSeq())

	buf.Push(BufferEntry{Seq: 2, Payload: []byte("b")})
	assert.Equal(t, 1, buf.Len())
	assert.Equal(t, int64(2), buf.OldestSeq())

	all := buf.Since(0)
	require.Len(t, all, 1)
	assert.Equal(t, int64(2), all[0].Seq)
}

func TestRingBuffer_ZeroCapacity_Clamped(t *testing.T) {
	buf := NewRingBuffer(0)
	assert.Equal(t, 0, buf.Len())

	buf.Push(BufferEntry{Seq: 1, Payload: []byte("x")})
	assert.Equal(t, 1, buf.Len())
}

func TestRingBuffer_NegativeCapacity_Clamped(t *testing.T) {
	buf := NewRingBuffer(-5)
	buf.Push(BufferEntry{Seq: 1, Payload: []byte("x")})
	assert.Equal(t, 1, buf.Len())
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	buf := NewRingBuffer(100)
	var wg sync.WaitGroup

	// 10 concurrent writers.
	for g := range 10 {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := range 100 {
				seq := int64(offset*100 + i + 1)
				buf.Push(BufferEntry{Seq: seq, Payload: []byte(fmt.Sprintf("%d", seq))})
			}
		}(g)
	}

	// Concurrent reader.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = buf.Since(0)
			_ = buf.Len()
			_ = buf.OldestSeq()
			_ = buf.NewestSeq()
		}
	}()

	wg.Wait()

	// Buffer should be full (100 entries).
	assert.Equal(t, 100, buf.Len())
}

func TestRingBuffer_Since_PartialOverlap(t *testing.T) {
	buf := NewRingBuffer(5)

	// Push seq 10..14.
	for i := int64(10); i <= 14; i++ {
		buf.Push(BufferEntry{Seq: i, Payload: []byte(fmt.Sprintf("%d", i))})
	}

	// Request since seq 5 — all entries are newer.
	all := buf.Since(5)
	require.Len(t, all, 5)
	assert.Equal(t, int64(10), all[0].Seq)

	// Request since seq 12.
	partial := buf.Since(12)
	require.Len(t, partial, 2)
	assert.Equal(t, int64(13), partial[0].Seq)
	assert.Equal(t, int64(14), partial[1].Seq)

	// Request since seq 99 — nothing newer.
	assert.Nil(t, buf.Since(99))
}
