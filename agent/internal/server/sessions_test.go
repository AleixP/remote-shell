package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

type sessionResp struct {
	Session sessionJSON `json:"session"`
}

func defaultShellAvailable(t *testing.T) {
	t.Helper()
	if terminal.DetectShells().Default() == "" {
		t.Skip("no shell available")
	}
}

func TestSessionLifecycle(t *testing.T) {
	defaultShellAvailable(t)
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken

	var created sessionResp
	resp := ts.call(t, "POST", "/api/v1/sessions", tok, `{"name":"Main","type":"persistent","cols":100,"rows":30}`, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	s := created.Session
	if !strings.HasPrefix(s.ID, "ses_") || s.Name != "Main" || s.Type != "persistent" || s.Status != "running" || s.CreatedAt == "" {
		t.Fatalf("created = %+v", s)
	}

	var got sessionResp
	if resp := ts.call(t, "GET", "/api/v1/sessions/"+s.ID, tok, "", &got); resp.StatusCode != 200 || got.Session.ID != s.ID {
		t.Fatalf("get = %d %+v", resp.StatusCode, got)
	}
	var list struct{ Sessions []sessionJSON }
	if resp := ts.call(t, "GET", "/api/v1/sessions", tok, "", &list); resp.StatusCode != 200 || len(list.Sessions) != 1 {
		t.Fatalf("list = %d %+v", resp.StatusCode, list)
	}

	var ok map[string]bool
	if resp := ts.call(t, "DELETE", "/api/v1/sessions/"+s.ID, tok, "", &ok); resp.StatusCode != 200 || !ok["success"] {
		t.Fatalf("delete = %d %v", resp.StatusCode, ok)
	}
	if c := errCode(t, ts, "GET", "/api/v1/sessions/"+s.ID, tok, "", 404); c != codeSessionNotFound {
		t.Errorf("get closed = %s", c)
	}
	if c := errCode(t, ts, "DELETE", "/api/v1/sessions/"+s.ID, tok, "", 404); c != codeSessionNotFound {
		t.Errorf("delete closed = %s", c)
	}
	if n := len(ts.sessions.List()); n != 0 {
		t.Errorf("%d sessions left in the manager", n)
	}
}

func TestSessionDefaultName(t *testing.T) {
	defaultShellAvailable(t)
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	var created sessionResp
	ts.call(t, "POST", "/api/v1/sessions", tok, `{"type":"temporary"}`, &created)
	if created.Session.Name != terminal.DetectShells().Default() || created.Session.Type != "temporary" {
		t.Errorf("created = %+v", created.Session)
	}
}

func TestSessionEndpointsRequireAuth(t *testing.T) {
	ts := startServer(t)
	for _, tc := range [][2]string{
		{"GET", "/api/v1/sessions"}, {"POST", "/api/v1/sessions"},
		{"GET", "/api/v1/sessions/ses_x"}, {"DELETE", "/api/v1/sessions/ses_x"},
	} {
		if c := errCode(t, ts, tc[0], tc[1], "", `{"type":"persistent"}`, 401); c != codeUnauthorized {
			t.Errorf("%v without token = %s", tc, c)
		}
	}
	if n := len(ts.sessions.List()); n != 0 {
		t.Fatalf("unauthenticated request created %d sessions", n)
	}
}

func TestCreateSessionValidation(t *testing.T) {
	defaultShellAvailable(t)
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken

	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"malformed json", `{`, 400, codeInvalidRequest},
		{"missing type", `{}`, 422, codeInvalidRequest},
		{"bad type", `{"type":"forever"}`, 422, codeInvalidRequest},
		{"long name", `{"type":"persistent","name":"` + strings.Repeat("a", 65) + `"}`, 422, codeInvalidRequest},
		{"control char in name", `{"type":"persistent","name":"a\u0000b"}`, 422, codeInvalidRequest},
		{"negative cols", `{"type":"persistent","cols":-1}`, 422, codeInvalidRequest},
		{"huge rows", `{"type":"persistent","rows":100000}`, 422, codeInvalidRequest},
		{"shell path", `{"type":"persistent","shell":"/bin/sh"}`, 422, codeShellNotAvail},
		{"windows shell path", `{"type":"persistent","shell":"C:\\Windows\\System32\\cmd.exe"}`, 422, codeShellNotAvail},
		{"unknown shell", `{"type":"persistent","shell":"evilsh"}`, 422, codeShellNotAvail},
		{"shell with args", `{"type":"persistent","shell":"sh -c id"}`, 422, codeShellNotAvail},
	}
	for _, tc := range cases {
		if got := errCode(t, ts, "POST", "/api/v1/sessions", tok, tc.body, tc.status); got != tc.code {
			t.Errorf("%s: code = %s, want %s", tc.name, got, tc.code)
		}
	}
	if n := len(ts.sessions.List()); n != 0 {
		t.Errorf("invalid requests created %d sessions", n)
	}
}

func TestSessionLimit(t *testing.T) {
	defaultShellAvailable(t)
	ts := startServerMax(t, 1)
	tok := ts.pair(t, "phone").AccessToken
	body := `{"type":"persistent"}`
	if resp := ts.call(t, "POST", "/api/v1/sessions", tok, body, nil); resp.StatusCode != 201 {
		t.Fatalf("first session status = %d", resp.StatusCode)
	}
	if c := errCode(t, ts, "POST", "/api/v1/sessions", tok, body, 409); c != codeSessionLimit {
		t.Errorf("over limit code = %s", c)
	}
}
