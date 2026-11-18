package terminal

import (
	"sync"
	"testing"
	"time"
)

type event struct {
	kind     string
	id       string
	reason   EndReason
	exitCode int
	shell    string
}

type fakeRecorder struct {
	mu     sync.Mutex
	events []event
}

func (f *fakeRecorder) add(e event) {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
}
func (f *fakeRecorder) Started(i SessionInfo) {
	f.add(event{kind: "started", id: i.ID, shell: i.Shell})
}
func (f *fakeRecorder) Ended(id string, r EndReason, code int) {
	f.add(event{kind: "ended", id: id, reason: r, exitCode: code})
}
func (f *fakeRecorder) Removed(id string) { f.add(event{kind: "removed", id: id}) }

func (f *fakeRecorder) kinds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.events))
	for i, e := range f.events {
		out[i] = e.kind
	}
	return out
}

func (f *fakeRecorder) waitFor(t *testing.T, kind string) event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, e := range f.events {
			if e.kind == kind {
				f.mu.Unlock()
				return e
			}
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %q event; got %v", kind, f.kinds())
	return event{}
}

func TestRecorderSeesStartAndNaturalExit(t *testing.T) {
	m := NewManager()
	rec := &fakeRecorder{}
	m.SetRecorder(rec)
	defer m.CloseAll()
	s := newTyped(t, m, Persistent)

	started := rec.waitFor(t, "started")
	if started.id != s.ID || started.shell == "" {
		t.Fatalf("started = %+v", started)
	}
	if _, err := s.Terminal.Write([]byte(testExit3)); err != nil {
		t.Fatal(err)
	}
	ended := rec.waitFor(t, "ended")
	if ended.id != s.ID || ended.reason != EndedExited || ended.exitCode != 3 {
		t.Fatalf("ended = %+v", ended)
	}
}

func TestRecorderSeesExpiryAndExplicitClose(t *testing.T) {
	m := NewManager()
	rec := &fakeRecorder{}
	m.SetRecorder(rec)
	defer m.CloseAll()

	temp := newTyped(t, m, Temporary)
	m.ExpireIdle(time.Now().Add(2*time.Hour), time.Hour)
	ended := rec.waitFor(t, "ended")
	if ended.id != temp.ID || ended.reason != EndedExpired || ended.exitCode != -1 {
		t.Fatalf("expiry event = %+v", ended)
	}

	closed := newTyped(t, m, Persistent)
	if err := m.Close(closed.ID); err != nil {
		t.Fatal(err)
	}
	removed := rec.waitFor(t, "removed")
	if removed.id != closed.ID {
		t.Fatalf("removed = %+v", removed)
	}
	// Give the exit watchers time: neither case may also be reported as a
	// natural exit.
	time.Sleep(200 * time.Millisecond)
	endedCount := 0
	for _, k := range rec.kinds() {
		if k == "ended" {
			endedCount++
		}
	}
	if endedCount != 1 {
		t.Errorf("%d ended events, want 1 (only the expiry): %v", endedCount, rec.kinds())
	}
}

func TestShutdownIsNotRecordedAsEnded(t *testing.T) {
	m := NewManager()
	rec := &fakeRecorder{}
	m.SetRecorder(rec)
	newTyped(t, m, Persistent)
	m.CloseAll()
	time.Sleep(200 * time.Millisecond)
	// The record stays "running": the next start marks it as lost.
	for _, k := range rec.kinds() {
		if k == "ended" || k == "removed" {
			t.Fatalf("shutdown produced %v", rec.kinds())
		}
	}
}
