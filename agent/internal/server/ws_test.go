package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

// wsClient is a minimal protocol client used by the tests.
type wsClient struct {
	t     *testing.T
	c     *websocket.Conn
	out   strings.Builder
	seqs  []uint64
	ctrl  []map[string]any
	ended error
}

func (ts *testServer) newSession(t *testing.T, token string) string {
	t.Helper()
	defaultShellAvailable(t)
	var created sessionResp
	if resp := ts.call(t, "POST", "/api/v1/sessions", token, `{"type":"persistent"`+testShellField+`}`, &created); resp.StatusCode != 201 {
		t.Fatalf("create session = %d", resp.StatusCode)
	}
	return created.Session.ID
}

func (ts *testServer) ticket(t *testing.T, token, sessionID string) string {
	t.Helper()
	var tk struct {
		Ticket    string `json:"ticket"`
		ExpiresAt string `json:"expires_at"`
	}
	if resp := ts.call(t, "POST", "/api/v1/sessions/"+sessionID+"/connection-ticket", token, "", &tk); resp.StatusCode != 200 || tk.Ticket == "" || tk.ExpiresAt == "" {
		t.Fatalf("ticket = %d %+v", resp.StatusCode, tk)
	}
	return tk.Ticket
}

func (ts *testServer) dialRaw(sessionID, ticket string) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &http.Client{Transport: insecureClient().Transport}
	return websocket.Dial(ctx, "wss://"+ts.url[len("https://"):]+"/api/v1/sessions/"+sessionID+"/terminal?ticket="+ticket,
		&websocket.DialOptions{HTTPClient: client})
}

func (ts *testServer) dial(t *testing.T, token, sessionID string) *wsClient {
	t.Helper()
	c, resp, err := ts.dialRaw(sessionID, ts.ticket(t, token, sessionID))
	if err != nil {
		t.Fatalf("dial: %v (status %v)", err, resp)
	}
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return &wsClient{t: t, c: c}
}

// next reads one message and records it.
func (w *wsClient) next() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data, err := w.c.Read(ctx)
	if err != nil {
		w.ended = err
		return err
	}
	if typ == websocket.MessageBinary {
		if len(data) < frameHeader || data[0] != frameOutput {
			w.t.Fatalf("bad output frame % x", data)
		}
		w.seqs = append(w.seqs, binary.BigEndian.Uint64(data[1:frameHeader]))
		w.out.Write(data[frameHeader:])
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		w.t.Fatalf("bad control message %q", data)
	}
	w.ctrl = append(w.ctrl, m)
	return nil
}

func (w *wsClient) control(typ string) map[string]any {
	for _, m := range w.ctrl {
		if m["type"] == typ {
			return m
		}
	}
	return nil
}

// waitControl reads until a control message of the given type has arrived.
func (w *wsClient) waitControl(typ string) map[string]any {
	w.t.Helper()
	for w.control(typ) == nil {
		if err := w.next(); err != nil {
			w.t.Fatalf("waiting for %s: %v (got %v)", typ, err, w.ctrl)
		}
	}
	return w.control(typ)
}

func (w *wsClient) waitOutput(want string) {
	w.t.Helper()
	for !strings.Contains(w.out.String(), want) {
		if err := w.next(); err != nil {
			w.t.Fatalf("waiting for output %q: %v (got %q)", want, err, w.out.String())
		}
	}
}

func (w *wsClient) sendJSON(v any) {
	w.t.Helper()
	b, _ := json.Marshal(v)
	if err := w.c.Write(context.Background(), websocket.MessageText, b); err != nil {
		w.t.Fatal(err)
	}
}

func (w *wsClient) sendInput(s string) {
	w.t.Helper()
	buf := append(make([]byte, frameHeader), s...)
	buf[0] = frameInput
	if err := w.c.Write(context.Background(), websocket.MessageBinary, buf); err != nil {
		w.t.Fatal(err)
	}
}

func (w *wsClient) resume(last uint64) map[string]any {
	w.t.Helper()
	w.waitControl("session.ready")
	w.sendJSON(map[string]any{"type": "session.resume", "last_output_sequence": last})
	return w.waitControl("session.resume.result")
}

func TestTerminalIO(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	w := ts.dial(t, tok, id)

	ready := w.waitControl("session.ready")
	if ready["session_id"] != id {
		t.Errorf("ready = %v", ready)
	}
	term := ready["terminal"].(map[string]any)
	if term["cols"].(float64) != 120 || term["rows"].(float64) != 40 {
		t.Errorf("terminal size = %v", term)
	}
	w.resume(0)
	w.sendInput(calcInput)
	w.waitOutput(calcWant)

	for i, s := range w.seqs {
		if s != uint64(i+1) {
			t.Fatalf("output sequence %v is not 1,2,3...", w.seqs)
		}
	}
}

func TestTicketIsSingleUseAndBoundToSession(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id1 := ts.newSession(t, tok)
	id2 := ts.newSession(t, tok)

	tk := ts.ticket(t, tok, id1)
	if _, resp, err := ts.dialRaw(id2, tk); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("ticket used on another session: err=%v resp=%v", err, resp)
	}
	c, _, err := ts.dialRaw(id1, tk) // a wrong-session attempt does not burn it
	if err != nil {
		t.Fatalf("valid ticket rejected: %v", err)
	}
	_ = c.CloseNow()
	if _, resp, err := ts.dialRaw(id1, tk); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("ticket reused: err=%v resp=%v", err, resp)
	}
	if _, resp, err := ts.dialRaw(id1, "garbage"); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("garbage ticket: err=%v resp=%v", err, resp)
	}
	if _, resp, err := ts.dialRaw("ses_missing", ts.ticket(t, tok, id1)); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("ticket for missing session: err=%v resp=%v", err, resp)
	}
}

func TestExpiredTicket(t *testing.T) {
	ts := startServerOpts(t, 8, time.Millisecond)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	tk := ts.ticket(t, tok, id)
	time.Sleep(50 * time.Millisecond)
	if _, resp, err := ts.dialRaw(id, tk); err == nil || resp == nil || resp.StatusCode != http.StatusGone {
		t.Fatalf("expired ticket: err=%v resp=%v", err, resp)
	}
}

func TestTicketEndpointRules(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	if c := errCode(t, ts, "POST", "/api/v1/sessions/ses_x/connection-ticket", "", "", 401); c != codeUnauthorized {
		t.Errorf("no auth = %s", c)
	}
	if c := errCode(t, ts, "POST", "/api/v1/sessions/ses_x/connection-ticket", tok, "", 404); c != codeSessionNotFound {
		t.Errorf("unknown session = %s", c)
	}
}

func TestOneConnectionPerSession(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	first := ts.dial(t, tok, id)
	first.waitControl("session.ready")

	_, resp, err := ts.dialRaw(id, ts.ticket(t, tok, id))
	if err == nil || resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("second connection: err=%v resp=%v", err, resp)
	}

	_ = first.c.CloseNow()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, _, err := ts.dialRaw(id, ts.ticket(t, tok, id))
		if err == nil {
			_ = c.CloseNow()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("could not reconnect after the first client left: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReconnectReplaysOnlyMissedOutput(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)

	w1 := ts.dial(t, tok, id)
	w1.resume(0)
	w1.sendInput(calcInput)
	w1.waitOutput(calcWant)
	last := w1.seqs[len(w1.seqs)-1]
	_ = w1.c.CloseNow()

	// The process keeps running and the session is reported as disconnected.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var got sessionResp
		ts.call(t, "GET", "/api/v1/sessions/"+id, tok, "", &got)
		if got.Session.Status == "disconnected" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %s, want disconnected", got.Session.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Reconnecting with the last sequence replays nothing old, and no new shell starts.
	var w2 *wsClient
	for deadline := time.Now().Add(5 * time.Second); ; {
		c, _, err := ts.dialRaw(id, ts.ticket(t, tok, id))
		if err == nil {
			c.SetReadLimit(1 << 20)
			t.Cleanup(func() { _ = c.CloseNow() })
			w2 = &wsClient{t: t, c: c}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reconnect: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	res := w2.resume(last)
	// Only output produced after `last` (for example the next prompt) is replayed.
	if res["buffer_truncated"] != false || res["replayed_from"].(float64) != float64(last+1) {
		t.Fatalf("resume result = %v (last %d)", res, last)
	}
	if strings.Contains(w2.out.String(), calcWant) {
		t.Fatal("old output replayed although the client already had it")
	}
	if n := len(ts.sessions.List()); n != 1 {
		t.Fatalf("%d sessions after reconnect, want 1", n)
	}
	w2.sendInput(calcInput)
	w2.waitOutput(calcWant)
	if w2.seqs[0] <= last {
		t.Errorf("new output sequence %d does not continue after %d", w2.seqs[0], last)
	}

	// A client with nothing replays everything retained.
	_ = w2.c.CloseNow()
	var w3 *wsClient
	for deadline := time.Now().Add(5 * time.Second); ; {
		c, _, err := ts.dialRaw(id, ts.ticket(t, tok, id))
		if err == nil {
			c.SetReadLimit(1 << 20)
			t.Cleanup(func() { _ = c.CloseNow() })
			w3 = &wsClient{t: t, c: c}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second reconnect: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	res = w3.resume(0)
	if res["replayed_from"].(float64) != 1 || res["buffer_truncated"] != false {
		t.Fatalf("full replay result = %v", res)
	}
	if !strings.Contains(w3.out.String(), calcWant) {
		t.Error("full replay does not contain earlier output")
	}
}

func TestResumeReportsLostOutput(t *testing.T) {
	ts := startServer(t)
	const bufferBytes = 512
	ts.sessions.SetOutputBufferBytes(bufferBytes)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)

	w1 := ts.dial(t, tok, id)
	w1.resume(0)
	// Produce several times more output than the buffer holds, reading it as
	// it arrives so the live connection itself never falls behind. How much
	// output a command makes depends on the shell, so loop on the byte count.
	for i := 1; w1.out.Len() < 8*bufferBytes; i++ {
		if i > 300 {
			t.Fatalf("only %d bytes of output after %d commands", w1.out.Len(), i)
		}
		w1.sendInput(calcInput)
		for strings.Count(w1.out.String(), calcWant) < i {
			if err := w1.next(); err != nil {
				t.Fatalf("command %d: %v", i, err)
			}
		}
	}
	_ = w1.c.CloseNow()

	var w2 *wsClient
	for deadline := time.Now().Add(5 * time.Second); ; {
		c, _, err := ts.dialRaw(id, ts.ticket(t, tok, id))
		if err == nil {
			c.SetReadLimit(1 << 20)
			t.Cleanup(func() { _ = c.CloseNow() })
			w2 = &wsClient{t: t, c: c}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reconnect: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if res := w2.resume(0); res["buffer_truncated"] != true {
		t.Fatalf("resume result = %v, want buffer_truncated=true", res)
	}
}

func TestInvalidMessagesAreRejectedSafely(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	w := ts.dial(t, tok, id)
	w.resume(0)

	// Short frame, wrong type, non-zero input sequence, bad resize, bad JSON.
	for _, frame := range [][]byte{{frameInput}, {0x7f, 0, 0, 0, 0, 0, 0, 0, 0, 'x'}, {frameInput, 0, 0, 0, 0, 0, 0, 0, 1, 'x'}} {
		if err := w.c.Write(context.Background(), websocket.MessageBinary, frame); err != nil {
			t.Fatal(err)
		}
	}
	w.sendJSON(map[string]any{"type": "terminal.resize", "cols": 0, "rows": 10})
	if err := w.c.Write(context.Background(), websocket.MessageText, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	w.sendJSON(map[string]any{"type": "future.extension"}) // ignored

	for errs := 0; errs < 5; {
		if err := w.next(); err != nil {
			t.Fatalf("connection dropped after invalid messages: %v", err)
		}
		errs = 0
		for _, m := range w.ctrl {
			if m["type"] == "error" {
				errs++
			}
		}
	}

	// A valid resize works and the connection is still usable.
	w.sendJSON(map[string]any{"type": "terminal.resize", "cols": 100, "rows": 30})
	w.sendInput(calcInput)
	w.waitOutput(calcWant)
	if cols, rows := mustSession(t, ts, id).Terminal.Size(); cols != 100 || rows != 30 {
		t.Errorf("size = %dx%d, want 100x30", cols, rows)
	}
}

func TestProcessExitIsReported(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	w := ts.dial(t, tok, id)
	w.resume(0)
	w.sendInput(exitInput)

	exited := w.waitControl("session.exited")
	if exited["exit_code"].(float64) != 3 {
		t.Errorf("exited = %v", exited)
	}
	for w.ended == nil { // the server closes the connection afterwards
		_ = w.next()
	}
	if websocket.CloseStatus(w.ended) != websocket.StatusNormalClosure {
		t.Errorf("close status = %v", websocket.CloseStatus(w.ended))
	}
}

func TestClosingSessionEndsConnection(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)
	w := ts.dial(t, tok, id)
	w.resume(0)

	if resp := ts.call(t, "DELETE", "/api/v1/sessions/"+id, tok, "", nil); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	w.waitControl("session.exited")
}

func TestRevokingDeviceClosesItsConnections(t *testing.T) {
	ts := startServer(t)
	a := ts.pair(t, "A")
	b := ts.pair(t, "B")
	id := ts.newSession(t, a.AccessToken)

	wa := ts.dial(t, a.AccessToken, id)
	wa.resume(0)
	pending := ts.ticket(t, a.AccessToken, id) // issued before the revocation

	if resp := ts.call(t, "DELETE", "/api/v1/devices/"+a.DeviceID, b.AccessToken, "", nil); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	for wa.ended == nil {
		_ = wa.next()
	}
	if _, resp, err := ts.dialRaw(id, pending); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("pending ticket of revoked device accepted: err=%v resp=%v", err, resp)
	}
	// The other device keeps working.
	wb := ts.dial(t, b.AccessToken, id)
	wb.resume(0)
}

func mustSession(t *testing.T, ts *testServer, id string) *terminal.Session {
	t.Helper()
	s, err := ts.sessions.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestSimultaneousConnectionsAreIsolated(t *testing.T) {
	ts := startServer(t)
	tok := ts.pair(t, "phone").AccessToken

	const n = 3
	var clients [n]*wsClient
	for i := range clients {
		clients[i] = ts.dial(t, tok, ts.newSession(t, tok))
		clients[i].resume(0)
	}
	for i, w := range clients {
		in, _ := calcN(i)
		w.sendInput(in)
	}
	for i, w := range clients {
		_, want := calcN(i)
		w.waitOutput(want)
	}
	for i, w := range clients {
		for j := range clients {
			if i == j {
				continue
			}
			if _, other := calcN(j); strings.Contains(w.out.String(), other) {
				t.Errorf("connection %d received output of connection %d", i, j)
			}
		}
	}
}
