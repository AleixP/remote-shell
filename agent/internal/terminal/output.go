package terminal

import "sync"

const (
	// DefaultOutputBufferBytes bounds the output retained per session.
	DefaultOutputBufferBytes = 1 << 20
	// MaxFramePayload bounds a single output frame.
	MaxFramePayload = 16 << 10
)

// Frame is one chunk of terminal output with its sequence number. Sequence
// numbers start at 1 and increase monotonically for the lifetime of a session.
type Frame struct {
	Seq  uint64
	Data []byte
}

// OutputBuffer keeps a bounded window of recent terminal output so a client
// that reconnects can recover what it missed.
type OutputBuffer struct {
	mu      sync.Mutex
	max     int
	size    int
	frames  []Frame
	nextSeq uint64
	closed  bool
	wake    chan struct{} // closed (and replaced) on every append or close
}

func NewOutputBuffer(maxBytes int) *OutputBuffer {
	if maxBytes <= 0 {
		maxBytes = DefaultOutputBufferBytes
	}
	return &OutputBuffer{max: maxBytes, nextSeq: 1, wake: make(chan struct{})}
}

func (b *OutputBuffer) signalLocked() {
	close(b.wake)
	b.wake = make(chan struct{})
}

// Append stores output, splitting it into frames of at most MaxFramePayload
// bytes, and evicts the oldest frames beyond the byte limit.
func (b *OutputBuffer) Append(p []byte) {
	if len(p) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(p) > 0 {
		n := min(len(p), MaxFramePayload)
		b.frames = append(b.frames, Frame{Seq: b.nextSeq, Data: append([]byte(nil), p[:n]...)})
		b.nextSeq++
		b.size += n
		p = p[n:]
	}
	for b.size > b.max && len(b.frames) > 1 {
		b.size -= len(b.frames[0].Data)
		b.frames[0] = Frame{}
		b.frames = b.frames[1:]
	}
	b.signalLocked()
}

// Close marks the end of the output stream (the process is gone).
func (b *OutputBuffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		b.signalLocked()
	}
}

// LastSeq returns the sequence number of the latest frame (0 if none yet).
func (b *OutputBuffer) LastSeq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nextSeq - 1
}

// Snapshot atomically returns the frames after seq, whether some frames after
// seq were already evicted (truncated), whether the stream has ended, and a
// channel that is closed on the next append or close. Waiting on that channel
// after processing the snapshot cannot miss output.
func (b *OutputBuffer) Snapshot(seq uint64) (frames []Frame, truncated, closed bool, wake <-chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	wake, closed = b.wake, b.closed
	if len(b.frames) == 0 {
		return nil, false, closed, wake
	}
	first := b.frames[0].Seq
	truncated = seq+1 < first
	start := 0
	if seq >= first {
		start = int(seq - first + 1)
	}
	if start >= len(b.frames) {
		return nil, truncated, closed, wake
	}
	return append([]Frame(nil), b.frames[start:]...), truncated, closed, wake
}
