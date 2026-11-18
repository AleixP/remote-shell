package storage

import (
	"testing"
	"time"
)

func newDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func rec(id string) SessionRecord {
	return SessionRecord{ID: id, Name: "n-" + id, Type: "persistent", Shell: "zsh", CreatedAt: time.Now()}
}

func TestSessionLifecycleRecords(t *testing.T) {
	db := newDB(t)
	if err := db.InsertSession(rec("ses_1")); err != nil {
		t.Fatal(err)
	}
	// A running session is not part of the ended history.
	if got, _ := db.ListEndedSessions(time.Now().Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("running session listed as ended: %v", got)
	}

	code := 3
	if err := db.EndSession("ses_1", EndExited, &code, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListEndedSessions(time.Now().Add(-time.Hour))
	if err != nil || len(got) != 1 {
		t.Fatalf("ListEndedSessions = %v, %v", got, err)
	}
	r := got[0]
	if r.EndReason != EndExited || r.ExitCode == nil || *r.ExitCode != 3 || r.EndedAt == nil || r.Shell != "zsh" {
		t.Fatalf("record = %+v", r)
	}

	// Ending again must not overwrite how it really ended.
	if err := db.EndSession("ses_1", EndExpired, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if again, _ := db.GetEndedSession("ses_1", time.Now().Add(-time.Hour)); again.EndReason != EndExited {
		t.Errorf("end reason overwritten: %q", again.EndReason)
	}
}

func TestEndedSessionWithUnknownExitCode(t *testing.T) {
	db := newDB(t)
	_ = db.InsertSession(rec("ses_1"))
	_ = db.EndSession("ses_1", EndExpired, nil, time.Now())
	r, err := db.GetEndedSession("ses_1", time.Now().Add(-time.Hour))
	if err != nil || r.ExitCode != nil || r.EndReason != EndExpired {
		t.Fatalf("record = %+v, %v", r, err)
	}
}

func TestMarkRunningSessionsLost(t *testing.T) {
	db := newDB(t)
	_ = db.InsertSession(rec("running"))
	_ = db.InsertSession(rec("done"))
	_ = db.EndSession("done", EndExited, nil, time.Now())

	n, err := db.MarkRunningSessionsLost(time.Now())
	if err != nil || n != 1 {
		t.Fatalf("MarkRunningSessionsLost = %d, %v; want 1", n, err)
	}
	r, err := db.GetEndedSession("running", time.Now().Add(-time.Hour))
	if err != nil || r.EndReason != EndAgentRestart {
		t.Fatalf("record = %+v, %v", r, err)
	}
	if d, _ := db.GetEndedSession("done", time.Now().Add(-time.Hour)); d.EndReason != EndExited {
		t.Errorf("an already ended session was changed: %q", d.EndReason)
	}
}

func TestRetentionPurgeAndSince(t *testing.T) {
	db := newDB(t)
	_ = db.InsertSession(rec("old"))
	_ = db.InsertSession(rec("recent"))
	_ = db.EndSession("old", EndExited, nil, time.Now().Add(-48*time.Hour))
	_ = db.EndSession("recent", EndExited, nil, time.Now().Add(-time.Hour))

	list, _ := db.ListEndedSessions(time.Now().Add(-24 * time.Hour))
	if len(list) != 1 || list[0].ID != "recent" {
		t.Fatalf("list within 24h = %v", list)
	}
	if _, err := db.GetEndedSession("old", time.Now().Add(-24*time.Hour)); err != ErrNotFound {
		t.Errorf("old session visible: %v", err)
	}
	n, err := db.PurgeEndedSessions(time.Now().Add(-24 * time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("purge = %d, %v; want 1", n, err)
	}
	if _, err := db.GetEndedSession("recent", time.Now().Add(-24*time.Hour)); err != nil {
		t.Errorf("recent session purged: %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	db := newDB(t)
	_ = db.InsertSession(rec("ses_1"))
	if ok, err := db.DeleteSession("ses_1"); err != nil || !ok {
		t.Fatalf("delete = %v, %v", ok, err)
	}
	if ok, _ := db.DeleteSession("ses_1"); ok {
		t.Error("deleted twice")
	}
}
