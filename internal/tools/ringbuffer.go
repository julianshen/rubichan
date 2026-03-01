package tools

import "sync"

// RingBuffer is a thread-safe circular byte buffer that overwrites the oldest
// data when the capacity is exceeded. It is designed to store recent output
// from long-running processes.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
	head int // next write position
	size int // number of bytes currently stored (capped at cap)
}

// NewRingBuffer creates a RingBuffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		buf: make([]byte, capacity),
		cap: capacity,
	}
}

// Write appends p to the buffer. When the buffer is full, the oldest bytes
// are overwritten. Write always succeeds and returns len(p), nil.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(p)

	// If the incoming data is larger than capacity, only keep the last
	// cap bytes since everything else would be overwritten anyway.
	if n >= rb.cap {
		p = p[n-rb.cap:]
		copy(rb.buf, p)
		rb.head = 0
		rb.size = rb.cap
		return n, nil
	}

	// Write byte-by-byte into the circular buffer.
	for _, b := range p {
		rb.buf[rb.head] = b
		rb.head = (rb.head + 1) % rb.cap
	}

	rb.size += n
	if rb.size > rb.cap {
		rb.size = rb.cap
	}

	return n, nil
}

// Bytes returns a copy of the buffer contents ordered from oldest to newest.
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.size == 0 {
		return nil
	}

	result := make([]byte, rb.size)

	if rb.size < rb.cap {
		// Buffer hasn't wrapped yet; data starts at index 0.
		copy(result, rb.buf[:rb.size])
	} else {
		// Buffer is full; oldest byte is at head (the next write position).
		start := rb.head
		firstChunk := rb.cap - start
		copy(result, rb.buf[start:])
		copy(result[firstChunk:], rb.buf[:start])
	}

	return result
}

// Len returns the number of bytes currently stored in the buffer.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.size
}

// Reset clears the buffer, discarding all stored data.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.size = 0
}
