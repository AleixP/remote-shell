package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// lockedBuffer is a goroutine-safe log sink.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestPairingLockout(t *testing.T) {
	ts := startServerWith(t, 8, func(o *Options) { o.AuthMaxFailures = 3 })
	valid, _ := ts.auth.NewPairingCode(time.Minute)
	for i := 0; i < 3; i++ {
		if c := errCode(t, ts, "POST", "/api/v1/pair", "", `{"pairing_code":"WRONG","device_name":"x"}`, 401); c != codePairingExpired {
			t.Fatalf("attempt %d = %s", i, c)
		}
	}
	// Now even the valid code is refused until the lockout ends.
	resp := ts.call(t, "POST", "/api/v1/pair", "", `{"pairing_code":"`+valid+`","device_name":"x"}`, nil)
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("locked-out pairing = %d (Retry-After %q)", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if c := errCode(t, ts, "POST", "/api/v1/pair", "", `{"pairing_code":"`+valid+`","device_name":"x"}`, 429); c != codeRateLimited {
		t.Errorf("code = %s", c)
	}
}

func TestAuthFailureLockout(t *testing.T) {
	ts := startServerWith(t, 8, func(o *Options) { o.AuthMaxFailures = 3 })
	good := ts.pair(t, "phone").AccessToken
	for i := 0; i < 3; i++ {
		errCode(t, ts, "GET", "/api/v1/devices", "bad-token", "", 401)
	}
	if c := errCode(t, ts, "GET", "/api/v1/devices", good, "", 429); c != codeRateLimited {
		t.Errorf("request from a locked-out client = %s", c)
	}
}

func TestTicketAndSessionCreationAreRateLimited(t *testing.T) {
	defaultShellAvailable(t)
	ts := startServerWith(t, 50, func(o *Options) { o.OpsPerMinute = 3 }) // burst of 3
	tok := ts.pair(t, "phone").AccessToken

	var ids []string
	for i := 0; i < 3; i++ {
		var created sessionResp
		if resp := ts.call(t, "POST", "/api/v1/sessions", tok, `{"type":"temporary"}`, &created); resp.StatusCode != 201 {
			t.Fatalf("session %d = %d", i, resp.StatusCode)
		}
		ids = append(ids, created.Session.ID)
	}
	if c := errCode(t, ts, "POST", "/api/v1/sessions", tok, `{"type":"temporary"}`, 429); c != codeRateLimited {
		t.Errorf("fourth session = %s", c)
	}

	for i := 0; i < 3; i++ {
		ts.ticket(t, tok, ids[0])
	}
	if c := errCode(t, ts, "POST", "/api/v1/sessions/"+ids[0]+"/connection-ticket", tok, "", 429); c != codeRateLimited {
		t.Errorf("fourth ticket = %s", c)
	}
	if n := len(ts.sessions.List()); n != 3 {
		t.Errorf("%d sessions exist, want 3", n)
	}
}

func TestBadTicketsLockOutTerminalConnections(t *testing.T) {
	ts := startServerWith(t, 8, func(o *Options) { o.AuthMaxFailures = 3 })
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	for i := 0; i < 3; i++ {
		if _, resp, err := ts.dialRaw(id, "guess"); err == nil || resp == nil || resp.StatusCode != 401 {
			t.Fatalf("guess %d: err=%v resp=%v", i, err, resp)
		}
	}
	if _, resp, err := ts.dialRaw(id, "guess"); err == nil || resp == nil || resp.StatusCode != 429 {
		t.Fatalf("after lockout: err=%v resp=%v", err, resp)
	}
}

func TestTerminalNeverAvailableWithoutTicket(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	// Neither the device token nor an empty ticket opens the terminal.
	if _, resp, err := ts.dialRaw(id, ""); err == nil || resp.StatusCode != 401 {
		t.Fatalf("empty ticket: err=%v resp=%v", err, resp)
	}
	if _, resp, err := ts.dialRaw(id, tok); err == nil || resp.StatusCode != 401 {
		t.Fatalf("access token used as ticket: err=%v resp=%v", err, resp)
	}
}

func TestOversizedMessagesAreRejected(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken

	// HTTP bodies are bounded.
	big := `{"pairing_code":"x","device_name":"` + strings.Repeat("a", maxBodyBytes+1) + `"}`
	if c := errCode(t, ts, "POST", "/api/v1/pair", "", big, 400); c != codeInvalidRequest {
		t.Errorf("oversized body = %s", c)
	}

	// WebSocket messages are bounded: the connection is closed.
	id := ts.newSession(t, tok)
	w := ts.dial(t, tok, id)
	w.resume(0)
	payload := append(make([]byte, frameHeader), bytes.Repeat([]byte("x"), maxWSMessage)...)
	payload[0] = frameInput
	_ = w.c.Write(context.Background(), websocket.MessageBinary, payload)
	for w.ended == nil {
		_ = w.next()
	}
	if websocket.CloseStatus(w.ended) != websocket.StatusMessageTooBig {
		t.Errorf("close status = %v, want message too big", websocket.CloseStatus(w.ended))
	}
	// The session itself survives the misbehaving client.
	if _, err := ts.sessions.Get(id); err != nil {
		t.Errorf("session lost: %v", err)
	}
}

func TestLogsNeverContainSecrets(t *testing.T) {
	var logs lockedBuffer
	ts := startServerWith(t, 8, func(o *Options) {
		o.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})

	code, _ := ts.auth.NewPairingCode(time.Minute)
	var pr pairResp
	ts.call(t, "POST", "/api/v1/pair", "", `{"pairing_code":"`+code+`","device_name":"phone"}`, &pr)
	ts.call(t, "POST", "/api/v1/pair", "", `{"pairing_code":"BADCODEBADCODE","device_name":"x"}`, nil)
	ts.call(t, "GET", "/api/v1/devices", "bad-access-token", "", nil)
	id := ts.newSession(t, pr.AccessToken)
	tk := ts.ticket(t, pr.AccessToken, id)
	c, _, err := ts.dialRaw(id, tk)
	if err != nil {
		t.Fatal(err)
	}
	w := &wsClient{t: t, c: c}
	c.SetReadLimit(1 << 20)
	w.resume(0)
	w.sendInput(calcInput)
	w.waitOutput(calcWant)
	_ = c.CloseNow()
	ts.call(t, "DELETE", "/api/v1/devices/"+pr.DeviceID, pr.AccessToken, "", nil)
	time.Sleep(100 * time.Millisecond)

	out := logs.String()
	if !strings.Contains(out, "device paired") {
		t.Fatalf("expected log output, got %q", out)
	}
	for name, secret := range map[string]string{
		"pairing code": code, "access token": pr.AccessToken, "bad token": "bad-access-token",
		"bad code": "BADCODEBADCODE", "ticket": tk, "terminal output": calcWant, "terminal input": "echo WS",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("logs contain the %s", name)
		}
	}
}

func TestErrorsDoNotLeakInternals(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/v1/sessions/..%2F..%2Fetc%2Fpasswd", ""},
		{"POST", "/api/v1/sessions", `{"type":"persistent","shell":"/etc/passwd"}`},
		{"GET", "/nope", ""},
	} {
		req, _ := http.NewRequest(tc.method, ts.url+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := insecureClient().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		for _, leak := range []string{"/Users", "/home", "/etc/passwd", "goroutine", ".go:", "agent.db"} {
			if strings.Contains(string(b), leak) {
				t.Errorf("%s %s leaks %q: %s", tc.method, tc.path, leak, b)
			}
		}
	}
}
