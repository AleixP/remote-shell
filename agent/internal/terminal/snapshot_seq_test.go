package terminal

import (
	"math"
	"testing"
)

// The resume sequence comes from the client, so any uint64 can arrive.
func TestSnapshotWithAnySequenceNeverPanics(t *testing.T) {
	b := NewOutputBuffer(1024)
	for i := 0; i < 5; i++ {
		b.Append([]byte("data"))
	}
	for _, seq := range []uint64{0, 1, 4, 5, 6, 100, math.MaxInt64, math.MaxInt64 + 1, math.MaxUint64 - 1, math.MaxUint64} {
		frames, _, _, _ := b.Snapshot(seq)
		for i, f := range frames {
			if f.Seq <= seq {
				t.Errorf("Snapshot(%d) replayed frame %d, which the client already has", seq, f.Seq)
			}
			if i > 0 && f.Seq != frames[i-1].Seq+1 {
				t.Errorf("Snapshot(%d) frames are not contiguous", seq)
			}
		}
	}
}

func TestSnapshotFromTheFutureIsNotTruncatedAndReplaysNothing(t *testing.T) {
	b := NewOutputBuffer(1024)
	b.Append([]byte("a"))
	b.Append([]byte("b"))
	for _, seq := range []uint64{2, 3, 1 << 40, math.MaxUint64} {
		frames, truncated, _, _ := b.Snapshot(seq)
		if len(frames) != 0 || truncated {
			t.Errorf("Snapshot(%d) = %d frames, truncated=%v; want nothing and not truncated", seq, len(frames), truncated)
		}
	}
}
