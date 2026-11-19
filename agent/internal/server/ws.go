package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/AleixP/remote-shell/agent/internal/safego"
	"github.com/AleixP/remote-shell/agent/internal/terminal"
	"github.com/AleixP/remote-shell/agent/internal/ticket"
)

const (
	frameInput  byte = 0x01
	frameOutput byte = 0x02
	frameHeader      = 9 // type (1) + sequence (8)

	maxWSMessage  = 64 << 10
	resumeTimeout = 5 * time.Second
	writeTimeout  = 10 * time.Second
	pingInterval  = 30 * time.Second
	pingTimeout   = 10 * time.Second
	exitWait      = 5 * time.Second
)

// connRegistry tracks open terminal connections per device so they can be
// closed when the device is revoked.
type connRegistry struct {
	mu       sync.Mutex
	byDevice map[string]map[*connEntry]struct{}
}

type connEntry struct{ close func() }

func newConnRegistry() *connRegistry {
	return &connRegistry{byDevice: make(map[string]map[*connEntry]struct{})}
}

func (r *connRegistry) add(deviceID string, closeFn func()) (remove func()) {
	e := &connEntry{close: closeFn}
	r.mu.Lock()
	if r.byDevice[deviceID] == nil {
		r.byDevice[deviceID] = make(map[*connEntry]struct{})
	}
	r.byDevice[deviceID][e] = struct{}{}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.byDevice[deviceID], e)
		if len(r.byDevice[deviceID]) == 0 {
			delete(r.byDevice, deviceID)
		}
		r.mu.Unlock()
	}
}

func (r *connRegistry) closeDevice(deviceID string) {
	r.mu.Lock()
	entries := make([]*connEntry, 0, len(r.byDevice[deviceID]))
	for e := range r.byDevice[deviceID] {
		entries = append(entries, e)
	}
	r.mu.Unlock()
	for _, e := range entries {
		e.close()
	}
}

func (s *Server) handleConnectionTicket(w http.ResponseWriter, r *http.Request) {
	if !s.allowOp(w, s.ticketLimit, deviceFrom(r.Context()).ID) {
		return
	}
	id := r.PathValue("id")
	if _, err := s.sessions.Get(id); err != nil {
		s.writeSessionGone(w, id)
		return
	}
	tk, exp := s.tickets.Issue(deviceFrom(r.Context()).ID, id)
	writeJSON(w, http.StatusOK, map[string]string{
		"ticket":     tk,
		"expires_at": exp.UTC().Format(time.RFC3339),
	})
}

// writeSessionGone answers for a session that is not running: 410 if it ended
// and is still remembered, 404 if it is unknown.
func (s *Server) writeSessionGone(w http.ResponseWriter, id string) {
	if _, ok := s.endedSession(id); ok {
		writeError(w, http.StatusGone, codeSessionClosed, "The session has ended.")
		return
	}
	writeError(w, http.StatusNotFound, codeSessionNotFound, "The requested session does not exist.")
}

// wsConn wraps a WebSocket connection with the protocol's message helpers.
type wsConn struct {
	c   *websocket.Conn
	ctx context.Context
}

func (w *wsConn) write(typ websocket.MessageType, b []byte) error {
	ctx, cancel := context.WithTimeout(w.ctx, writeTimeout)
	defer cancel()
	return w.c.Write(ctx, typ, b)
}

func (w *wsConn) sendJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.write(websocket.MessageText, b)
}

func (w *wsConn) sendError(code, message string) error {
	return w.sendJSON(map[string]string{"type": "error", "code": code, "message": message})
}

func (w *wsConn) sendOutput(f terminal.Frame) error {
	buf := make([]byte, frameHeader+len(f.Data))
	buf[0] = frameOutput
	binary.BigEndian.PutUint64(buf[1:frameHeader], f.Seq)
	copy(buf[frameHeader:], f.Data)
	return w.write(websocket.MessageBinary, buf)
}

// handleTerminal upgrades to a WebSocket attached to a session. The single-use
// ticket is validated before the upgrade.
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ip := clientIP(r)
	if s.authBlocked(w, r) || !s.allowOp(w, s.connectLimit, ip) {
		return
	}
	deviceID, res := s.tickets.Consume(r.URL.Query().Get("ticket"), id)
	if res != ticket.OK {
		s.authFails.Fail(ip)
	}
	switch res {
	case ticket.Expired:
		writeError(w, http.StatusGone, codeTicketExpired, "The connection ticket has expired.")
		return
	case ticket.Invalid:
		writeError(w, http.StatusUnauthorized, codeInvalidTicket, "The connection ticket is invalid.")
		return
	}
	sess, err := s.sessions.Get(id)
	if err != nil {
		s.writeSessionGone(w, id)
		return
	}
	if !sess.TryAttach() {
		writeError(w, http.StatusConflict, codeAlreadyConnected, "Another client is connected to this session.")
		return
	}
	defer sess.Detach()

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Native app webviews send these origins; the ticket already
		// protects against cross-site connection attempts.
		OriginPatterns: []string{"capacitor://localhost", "ionic://localhost", "localhost", "localhost:*"},
	})
	if err != nil {
		return // Accept already replied
	}
	conn.SetReadLimit(maxWSMessage)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ws := &wsConn{c: conn, ctx: ctx}
	remove := s.conns.add(deviceID, func() {
		cancel()
		_ = conn.Close(websocket.StatusPolicyViolation, "device revoked")
	})
	defer remove()

	s.log.Info("terminal connected", "session_id", id, "device_id", deviceID)
	defer s.log.Info("terminal disconnected", "session_id", id, "device_id", deviceID)

	s.serveTerminal(ctx, cancel, ws, sess)
}

func (s *Server) serveTerminal(ctx context.Context, cancel context.CancelFunc, ws *wsConn, sess *terminal.Session) {
	cols, rows := sess.Terminal.Size()
	if err := ws.sendJSON(map[string]any{
		"type":                 "session.ready",
		"session_id":           sess.ID,
		"terminal":             map[string]int{"cols": cols, "rows": rows},
		"last_output_sequence": sess.Output.LastSeq(),
	}); err != nil {
		_ = ws.c.CloseNow()
		return
	}

	resume := make(chan uint64, 1)
	safego.Go("ws-read", func() { s.readLoop(ctx, cancel, ws, sess, resume) })
	safego.Go("ws-keepalive", func() { keepAlive(ctx, cancel, ws.c) })

	var from uint64
	select {
	case from = <-resume:
	case <-time.After(resumeTimeout):
	case <-ctx.Done():
		_ = ws.c.CloseNow()
		return
	}

	if !s.streamOutput(ctx, ws, sess, from) {
		return
	}
	// The output stream ended: report how the process finished.
	select {
	case <-sess.Terminal.Done():
	case <-time.After(exitWait):
	case <-ctx.Done():
	}
	_ = ws.sendJSON(map[string]any{"type": "session.exited", "exit_code": sess.Terminal.ExitCode()})
	_ = ws.c.Close(websocket.StatusNormalClosure, "")
}

// streamOutput replays buffered output after `from`, reports the result, then
// follows live output. Replay and live output share one ordered loop, so they
// can never interleave. It returns true when the process output ended.
func (s *Server) streamOutput(ctx context.Context, ws *wsConn, sess *terminal.Session, from uint64) bool {
	frames, truncated, _, _ := sess.Output.Snapshot(from)
	sent := from
	for _, f := range frames {
		if ws.sendOutput(f) != nil {
			_ = ws.c.CloseNow()
			return false
		}
		sent = f.Seq
	}
	result := map[string]any{
		"type":             "session.resume.result",
		"replayed_from":    from + 1,
		"replayed_to":      sent,
		"buffer_truncated": truncated,
	}
	if len(frames) > 0 {
		result["replayed_from"] = frames[0].Seq
	}
	if ws.sendJSON(result) != nil {
		_ = ws.c.CloseNow()
		return false
	}

	for {
		frames, truncated, closed, wake := sess.Output.Snapshot(sent)
		if truncated {
			// The client fell behind the buffer: make it reconnect so
			// that the resume result reports the loss.
			_ = ws.c.Close(websocket.StatusTryAgainLater, "output buffer overrun")
			return false
		}
		for _, f := range frames {
			if ws.sendOutput(f) != nil {
				_ = ws.c.CloseNow()
				return false
			}
			sent = f.Seq
		}
		if closed && len(frames) == 0 {
			return true
		}
		if len(frames) > 0 {
			continue
		}
		select {
		case <-wake:
		case <-ctx.Done():
			_ = ws.c.CloseNow()
			return false
		}
	}
}

func (s *Server) readLoop(ctx context.Context, cancel context.CancelFunc, ws *wsConn, sess *terminal.Session, resume chan<- uint64) {
	defer cancel()
	for {
		typ, data, err := ws.c.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			payload, ok := parseInputFrame(data)
			if !ok {
				_ = ws.sendError(codeInvalidRequest, "Invalid terminal input frame.")
				continue
			}
			if _, err := sess.Terminal.Write(payload); err != nil {
				return
			}
		case websocket.MessageText:
			s.handleControl(ws, sess, data, resume)
		}
	}
}

// parseInputFrame returns the payload of a terminal input frame sent by the
// client: [type 1B = input][sequence 8B = 0][payload]. Input frames carry no
// sequence number, so a non-zero one is rejected.
func parseInputFrame(data []byte) (payload []byte, ok bool) {
	if len(data) < frameHeader || data[0] != frameInput || binary.BigEndian.Uint64(data[1:frameHeader]) != 0 {
		return nil, false
	}
	return data[frameHeader:], true
}

type controlMessage struct {
	Type               string `json:"type"`
	LastOutputSequence uint64 `json:"last_output_sequence"`
	Cols               int    `json:"cols"`
	Rows               int    `json:"rows"`
}

// parseControl decodes a JSON control message from the client.
func parseControl(data []byte) (controlMessage, bool) {
	var msg controlMessage
	if json.Unmarshal(data, &msg) != nil {
		return controlMessage{}, false
	}
	return msg, true
}

func (s *Server) handleControl(ws *wsConn, sess *terminal.Session, data []byte, resume chan<- uint64) {
	msg, ok := parseControl(data)
	if !ok {
		_ = ws.sendError(codeInvalidRequest, "Invalid control message.")
		return
	}
	switch msg.Type {
	case "session.resume":
		select {
		case resume <- msg.LastOutputSequence:
		default: // only the first resume is honored
		}
	case "terminal.resize":
		if err := sess.Terminal.Resize(msg.Cols, msg.Rows); err != nil {
			_ = ws.sendError(codeInvalidRequest, "Invalid terminal dimensions.")
		}
	}
	// Unknown optional message types are ignored.
}

// keepAlive pings the client and drops the connection if it stops answering.
func keepAlive(ctx context.Context, cancel context.CancelFunc, c *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, pingTimeout)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}
