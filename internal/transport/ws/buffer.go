package ws

import "sync"

// RingBuffer stores the last N envelopes for reconnection replay.
// It is safe for concurrent use.
type RingBuffer struct {
	entries []BufferEntry
	cap     int
	count   int
	head    int // next write position
	mu      sync.RWMutex
}

// BufferEntry is a single buffered envelope with its sequence number.
type BufferEntry struct {
	Seq     int64
	Payload []byte // pre-marshaled JSON envelope
}

// NewRingBuffer creates a ring buffer with the given capacity.
// Capacity must be > 0; it is clamped to 1 if zero or negative.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &RingBuffer{
		entries: make([]BufferEntry, capacity),
		cap:     capacity,
	}
}

// Push appends an entry to the buffer, overwriting the oldest if full.
func (b *RingBuffer) Push(entry BufferEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries[b.head] = entry
	b.head = (b.head + 1) % b.cap
	if b.count < b.cap {
		b.count++
	}
}

// Since returns all buffered entries with Seq > lastSeq, in order.
// Returns nil if no entries match.
func (b *RingBuffer) Since(lastSeq int64) []BufferEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return nil
	}

	// Calculate the start index of the oldest entry.
	start := (b.head - b.count + b.cap) % b.cap

	var result []BufferEntry
	for i := range b.count {
		idx := (start + i) % b.cap
		if b.entries[idx].Seq > lastSeq {
			result = append(result, b.entries[idx])
		}
	}
	return result
}

// Len returns the number of entries currently in the buffer.
func (b *RingBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

// OldestSeq returns the sequence number of the oldest buffered entry.
// Returns 0 if the buffer is empty.
func (b *RingBuffer) OldestSeq() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return 0
	}
	start := (b.head - b.count + b.cap) % b.cap
	return b.entries[start].Seq
}

// NewestSeq returns the sequence number of the newest buffered entry.
// Returns 0 if the buffer is empty.
func (b *RingBuffer) NewestSeq() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return 0
	}
	newest := (b.head - 1 + b.cap) % b.cap
	return b.entries[newest].Seq
}
