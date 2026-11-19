package terminal

import (
	"testing"
)

// Run one with, for example: go test -run=^$ -fuzz=FuzzOutputBuffer -fuzztime=30s ./internal/terminal

func FuzzOutputBuffer(f *testing.F) {
	f.Add([]byte("hello world, this is some output"), uint64(0), uint8(7))
	f.Add([]byte("x"), uint64(1<<63), uint8(1))
	f.Add([]byte("0123456789"), ^uint64(0), uint8(3))
	f.Add([]byte{}, uint64(5), uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, seq uint64, chunk uint8) {
		b := NewOutputBuffer(64)
		size := int(chunk%32) + 1
		for i := 0; i < len(data); i += size {
			end := i + size
			if end > len(data) {
				end = len(data)
			}
			b.Append(data[i:end])
		}
		all, _, _, _ := b.Snapshot(0)
		frames, truncated, _, _ := b.Snapshot(seq)
		for i, fr := range frames {
			if fr.Seq <= seq {
				t.Fatalf("replayed frame %d to a client that has %d", fr.Seq, seq)
			}
			if i > 0 && fr.Seq != frames[i-1].Seq+1 {
				t.Fatalf("frames are not contiguous: %d then %d", frames[i-1].Seq, fr.Seq)
			}
		}
		if len(frames) == 0 {
			return
		}
		want := seq + 1
		if first := all[0].Seq; first > want {
			want = first
			if !truncated {
				t.Fatalf("output before frame %d was evicted but the client was not told", first)
			}
		}
		if frames[0].Seq != want {
			t.Fatalf("replay starts at %d, want %d", frames[0].Seq, want)
		}
	})
}

func FuzzValidateSize(f *testing.F) {
	f.Add(80, 24)
	f.Add(0, 0)
	f.Add(-1, 5)
	f.Add(1<<62, 1<<62)
	f.Fuzz(func(t *testing.T, cols, rows int) {
		err := validateSize(cols, rows)
		ok := cols >= 1 && rows >= 1 && cols <= MaxCols && rows <= MaxRows
		if (err == nil) != ok {
			t.Fatalf("validateSize(%d, %d) = %v", cols, rows, err)
		}
	})
}
