package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/storage"
)

type listResp struct {
	Sessions []sessionJSON `json:"sessions"`
}

func (ts *testServer) findSession(t *testing.T, token, id string) (sessionJSON, bool) {
	t.Helper()
	var list listResp
	ts.call(t, "GET", "/api/v1/sessions", token, "", &list)
	for _, s := range list.Sessions {
		if s.ID == id {
			return s, true
		}
	}
	return sessionJSON{}, false
}

func (ts *testServer) waitStatus(t *testing.T, token, id, want string) sessionJSON {
	t.Helper()
	// PowerShell can take several seconds to start on a CI runner before it
	// reads the input that makes it exit.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if s, ok := ts.findSession(t, token, id); ok && s.Status == want {
			return s
		}
		if time.Now().After(deadline) {
			s, ok := ts.findSession(t, token, id)
			t.Fatalf("session %s never became %q (found=%v %+v)", id, want, ok, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionThatExitedStaysVisibleUntilDismissed(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)

	sess, err := ts.sessions.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Terminal.Write([]byte(exitInput)); err != nil {
		t.Fatal(err)
	}

	ended := ts.waitStatus(t, tok, id, "exited")
	if ended.EndReason != "process_exited" || ended.ExitCode == nil || *ended.ExitCode != 3 || ended.EndedAt == "" {
		t.Fatalf("ended session = %+v", ended)
	}
	var got sessionResp
	if resp := ts.call(t, "GET", "/api/v1/sessions/"+id, tok, "", &got); resp.StatusCode != 200 || got.Session.Status != "exited" {
		t.Fatalf("GET ended session = %d %+v", resp.StatusCode, got.Session)
	}
	// An ended session cannot be connected to: it is gone, not unknown.
	if c := errCode(t, ts, "POST", "/api/v1/sessions/"+id+"/connection-ticket", tok, "", http.StatusGone); c != codeSessionClosed {
		t.Errorf("ticket for ended session = %s", c)
	}

	// Dismissing removes it from the list for good.
	var ok map[string]bool
	if resp := ts.call(t, "DELETE", "/api/v1/sessions/"+id, tok, "", &ok); resp.StatusCode != 200 || !ok["success"] {
		t.Fatalf("dismiss = %d %v", resp.StatusCode, ok)
	}
	if _, found := ts.findSession(t, tok, id); found {
		t.Error("dismissed session still listed")
	}
	if c := errCode(t, ts, "DELETE", "/api/v1/sessions/"+id, tok, "", 404); c != codeSessionNotFound {
		t.Errorf("dismissing twice = %s", c)
	}
}

func TestSessionClosedByUserLeavesNoTrace(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	if resp := ts.call(t, "DELETE", "/api/v1/sessions/"+id, tok, "", nil); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	time.Sleep(200 * time.Millisecond) // let the exit watcher run
	if _, found := ts.findSession(t, tok, id); found {
		t.Error("a session closed on purpose is listed as ended")
	}
	if c := errCode(t, ts, "GET", "/api/v1/sessions/"+id, tok, "", 404); c != codeSessionNotFound {
		t.Errorf("GET = %s", c)
	}
}

func TestExpiredTemporarySessionIsReported(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	defaultShellAvailable(t)
	var created sessionResp
	ts.call(t, "POST", "/api/v1/sessions", tok, `{"type":"temporary"`+testShellField+`}`, &created)
	id := created.Session.ID

	if got := ts.sessions.ExpireIdle(time.Now().Add(2*time.Hour), time.Hour); len(got) != 1 {
		t.Fatalf("ExpireIdle = %v", got)
	}
	s := ts.waitStatus(t, tok, id, "expired")
	if s.EndReason != "idle_timeout" || s.ExitCode != nil {
		t.Fatalf("expired session = %+v", s)
	}
}

func TestSessionsFromAPreviousRunAreReportedAsEnded(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken

	// What the agent database holds after a crash or restart: a session still
	// recorded as running whose process no longer exists.
	err := ts.db.InsertSession(storage.SessionRecord{
		ID: "ses_deadbeefdeadbeef", Name: "before restart", Type: "persistent", Shell: "zsh", CreatedAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found := ts.findSession(t, tok, "ses_deadbeefdeadbeef"); found {
		t.Fatal("a session recorded as running was listed before the restart handling")
	}
	if _, err := ts.db.MarkRunningSessionsLost(time.Now()); err != nil {
		t.Fatal(err)
	}

	s := ts.waitStatus(t, tok, "ses_deadbeefdeadbeef", "exited")
	if s.EndReason != "agent_restarted" || s.ExitCode != nil {
		t.Fatalf("session = %+v", s)
	}
	if c := errCode(t, ts, "POST", "/api/v1/sessions/ses_deadbeefdeadbeef/connection-ticket", tok, "", http.StatusGone); c != codeSessionClosed {
		t.Errorf("ticket = %s", c)
	}
	// New sessions keep working normally next to it.
	id := ts.newSession(t, tok)
	if s, ok := ts.findSession(t, tok, id); !ok || s.Status != "running" {
		t.Errorf("new session after restart = %+v (found %v)", s, ok)
	}
}

func TestOldEndedSessionsAreNotListed(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	if err := ts.db.InsertSession(storage.SessionRecord{ID: "ses_old", Name: "old", Type: "persistent", Shell: "sh", CreatedAt: time.Now().Add(-72 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := ts.db.EndSession("ses_old", storage.EndExited, nil, time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, found := ts.findSession(t, tok, "ses_old"); found {
		t.Error("a session that ended before the retention period is listed")
	}
	if c := errCode(t, ts, "GET", "/api/v1/sessions/ses_old", tok, "", 404); c != codeSessionNotFound {
		t.Errorf("GET = %s", c)
	}
}
