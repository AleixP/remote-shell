package server

import (
	"errors"
	"net/http"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AleixP/remote-shell/agent/internal/storage"
	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

const maxSessionNameLen = 64

type sessionJSON struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	LastActivityAt string `json:"last_activity_at"`
	// Only for sessions that have ended.
	EndedAt   string `json:"ended_at,omitempty"`
	EndReason string `json:"end_reason,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}

// endedToJSON describes a session that is no longer running. After an agent
// restart the process is gone, so the session is reported as exited.
func endedToJSON(r storage.SessionRecord) sessionJSON {
	status, reason := "exited", "process_exited"
	switch r.EndReason {
	case storage.EndExpired:
		status, reason = "expired", "idle_timeout"
	case storage.EndAgentRestart:
		reason = "agent_restarted"
	}
	j := sessionJSON{
		ID: r.ID, Name: r.Name, Type: r.Type, Status: status, EndReason: reason, ExitCode: r.ExitCode,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
	if r.EndedAt != nil {
		j.EndedAt = r.EndedAt.UTC().Format(time.RFC3339)
		j.LastActivityAt = j.EndedAt
	}
	return j
}

// endedSession looks up a session that has ended but is still remembered.
func (s *Server) endedSession(id string) (storage.SessionRecord, bool) {
	if s.history == nil {
		return storage.SessionRecord{}, false
	}
	r, err := s.history.GetEndedSession(id, time.Now().Add(-s.retention))
	return r, err == nil
}

func toSessionJSON(s *terminal.Session) sessionJSON {
	return sessionJSON{
		ID:             s.ID,
		Name:           s.Name,
		Type:           string(s.Type),
		Status:         s.Status(),
		CreatedAt:      s.CreatedAt.UTC().Format(time.RFC3339),
		LastActivityAt: s.Terminal.LastActivity().Format(time.RFC3339),
	}
}

func validDisplayName(name string) bool {
	if utf8.RuneCountInString(name) > maxSessionNameLen {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (s *Server) handleListSessions(w http.ResponseWriter, _ *http.Request) {
	list := s.sessions.List()
	out := make([]sessionJSON, 0, len(list))
	for _, sess := range list {
		out = append(out, toSessionJSON(sess))
	}
	if s.history != nil {
		ended, err := s.history.ListEndedSessions(time.Now().Add(-s.retention))
		if err != nil {
			s.log.Error("list ended sessions failed", "error", err)
		}
		for _, r := range ended {
			out = append(out, endedToJSON(r))
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if !s.allowOp(w, s.sessionLimit, deviceFrom(r.Context()).ID) {
		return
	}
	var req struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Shell string `json:"shell"`
		Cols  int    `json:"cols"`
		Rows  int    `json:"rows"`
	}
	if !decodeJSON(r, &req) {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "Invalid request data.")
		return
	}
	typ := terminal.SessionType(req.Type)
	if typ != terminal.Persistent && typ != terminal.Temporary {
		writeError(w, http.StatusUnprocessableEntity, codeInvalidRequest, "type must be \"persistent\" or \"temporary\".")
		return
	}
	if !validDisplayName(req.Name) {
		writeError(w, http.StatusUnprocessableEntity, codeInvalidRequest, "Invalid session name.")
		return
	}
	if req.Cols < 0 || req.Rows < 0 || req.Cols > terminal.MaxCols || req.Rows > terminal.MaxRows {
		writeError(w, http.StatusUnprocessableEntity, codeInvalidRequest, "Invalid terminal dimensions.")
		return
	}
	// Clients pick an allowlisted shell identifier, never a path.
	shellName, shellPath, err := s.shells.Resolve(req.Shell)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, codeShellNotAvail, "The requested shell is not available.")
		return
	}
	name := req.Name
	if name == "" {
		name = shellName
	}

	sess, err := s.sessions.Create(terminal.CreateOptions{
		Name:    name,
		Type:    typ,
		Options: terminal.Options{Shell: shellPath, Cols: req.Cols, Rows: req.Rows},
	})
	switch {
	case err == nil:
		s.log.Info("session created", "session_id", sess.ID, "type", sess.Type, "by", deviceFrom(r.Context()).ID)
		writeJSON(w, http.StatusCreated, map[string]any{"session": toSessionJSON(sess)})
	case errors.Is(err, terminal.ErrTooManySessions):
		writeError(w, http.StatusConflict, codeSessionLimit, "The maximum number of sessions has been reached.")
	default:
		s.log.Error("create session failed", "error", err)
		writeError(w, http.StatusInternalServerError, codeTerminalError, "The terminal could not be started.")
	}
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.sessions.Get(id)
	if err != nil {
		if rec, ok := s.endedSession(id); ok {
			writeJSON(w, http.StatusOK, map[string]any{"session": endedToJSON(rec)})
			return
		}
		writeError(w, http.StatusNotFound, codeSessionNotFound, "The requested session does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": toSessionJSON(sess)})
}

func (s *Server) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.sessions.Close(id); err != nil {
		// An ended session is dismissed by deleting its record.
		if _, ok := s.endedSession(id); ok {
			if _, derr := s.history.DeleteSession(id); derr == nil {
				writeJSON(w, http.StatusOK, map[string]bool{"success": true})
				return
			}
		}
		writeError(w, http.StatusNotFound, codeSessionNotFound, "The requested session does not exist.")
		return
	}
	s.log.Info("session closed", "session_id", id, "by", deviceFrom(r.Context()).ID)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
