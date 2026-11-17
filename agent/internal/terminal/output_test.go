package terminal

import (
	"bytes"
	"testing"
	"time"
)

func seqs(fs []Frame) []uint64 {
	out := make([]uint64, len(fs))
	for i, f := range fs {
		out[i] = f.Seq
	}
	return out
}

func TestOutputSequencesAndReplay(t *testing.T) {
	b := NewOutputBuffer(1024)
	for _, s := range []string{"a", "b", "c"} {
		b.Append([]byte(s))
	}
	if b.LastSeq() != 3 {
		t.Fatalf("LastSeq = %d, want 3", b.LastSeq())
	}
	frames, truncated, _, _ := b.Snapshot(0)
	if truncated || len(frames) != 3 || frames[0].Seq != 1 || string(frames[2].Data) != "c" {
		t.Fatalf("Snapshot(0) = %v truncated=%v", seqs(frames), truncated)
	}
	frames, truncated, _, _ = b.Snapshot(2)
	if truncated || len(frames) != 1 || frames[0].Seq != 3 {
		t.Fatalf("Snapshot(2) = %v truncated=%v", seqs(frames), truncated)
	}
	if frames, truncated, _, _ = b.Snapshot(3); len(frames) != 0 || truncated {
		t.Fatalf("Snapshot(3) = %v truncated=%v", seqs(frames), truncated)
	}
}

func TestOutputSplitsLargeAppends(t *testing.T) {
	b := NewOutputBuffer(10 * MaxFramePayload)
	data := bytes.Repeat([]byte("x"), 2*MaxFramePayload+10)
	b.Append(data)
	frames, _, _, _ := b.Snapshot(0)
	if len(frames) != 3 || len(frames[0].Data) != MaxFramePayload || len(frames[2].Data) != 10 {
		t.Fatalf("frames = %d (first %d bytes)", len(frames), len(frames[0].Data))
	}
}

func TestOutputEvictsOldestAndReportsTruncation(t *testing.T) {
	b := NewOutputBuffer(10)
	for i := 0; i < 5; i++ {
		b.Append([]byte("0123456789")) // 10 bytes each: only the newest survives
	}
	frames, truncated, _, _ := b.Snapshot(0)
	if !truncated || len(frames) != 1 || frames[0].Seq != 5 {
		t.Fatalf("Snapshot(0) = %v truncated=%v, want only seq 5 and truncated", seqs(frames), truncated)
	}
	// A client that already has seq 4 lost nothing.
	frames, truncated, _, _ = b.Snapshot(4)
	if truncated || len(frames) != 1 || frames[0].Seq != 5 {
		t.Fatalf("Snapshot(4) = %v truncated=%v", seqs(frames), truncated)
	}
	// Sequence numbers keep increasing after eviction.
	b.Append([]byte("z"))
	if b.LastSeq() != 6 {
		t.Errorf("LastSeq = %d, want 6", b.LastSeq())
	}
}

func TestOutputWakeAndClose(t *testing.T) {
	b := NewOutputBuffer(0)
	_, _, closed, wake := b.Snapshot(0)
	if closed {
		t.Fatal("new buffer is closed")
	}
	select {
	case <-wake:
		t.Fatal("woke without an append")
	default:
	}
	b.Append([]byte("x"))
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("append did not wake waiters")
	}
	_, _, _, wake = b.Snapshot(1)
	b.Close()
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("close did not wake waiters")
	}
	if _, _, closed, _ = b.Snapshot(1); !closed {
		t.Fatal("Snapshot does not report closed")
	}
	b.Close() // idempotent
}
