// RingBuffer is a fixed-capacity circular buffer that automatically overwrites
// the oldest element when full. It is NOT safe for concurrent use — the caller
// must provide external synchronisation (e.g. a mutex).
package usage

// RingBuffer provides a bounded FIFO buffer.
// When at capacity, Push overwrites the oldest entry before advancing.
type RingBuffer[T any] struct {
	buf  []T
	head int // index of the next write slot
	size int // number of elements currently stored
}

// NewRingBuffer creates a RingBuffer with the given capacity.
// Capacity must be >= 1; otherwise it is silently clamped to 1.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer[T]{buf: make([]T, capacity)}
}

// Push adds an element to the buffer, overwriting the oldest entry when full.
func (rb *RingBuffer[T]) Push(item T) {
	rb.buf[rb.head] = item
	rb.head = (rb.head + 1) % len(rb.buf)
	if rb.size < len(rb.buf) {
		rb.size++
	}
}

// Len returns the current number of elements in the buffer.
func (rb *RingBuffer[T]) Len() int { return rb.size }

// Cap returns the maximum capacity of the buffer.
func (rb *RingBuffer[T]) Cap() int { return len(rb.buf) }

// Snapshot returns a copy of the buffer contents in temporal order
// (oldest first). The returned slice is independent of the buffer.
func (rb *RingBuffer[T]) Snapshot() []T {
	if rb.size == 0 {
		return nil
	}
	out := make([]T, rb.size)
	start := rb.head - rb.size
	if start < 0 {
		start += len(rb.buf)
	}
	for i := 0; i < rb.size; i++ {
		out[i] = rb.buf[(start+i)%len(rb.buf)]
	}
	return out
}
