// Package sessionlog persists terminal session lifecycle events in SQLite.
package sessionlog

import (
	"log/slog"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/storage"
	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

// Recorder implements terminal.Recorder on top of the agent database.
// Persistence failures are logged and never affect running terminals.
type Recorder struct {
	db  *storage.DB
	log *slog.Logger
	now func() time.Time
}

func New(db *storage.DB, log *slog.Logger) *Recorder {
	return &Recorder{db: db, log: log, now: time.Now}
}

func (r *Recorder) Started(i terminal.SessionInfo) {
	err := r.db.InsertSession(storage.SessionRecord{
		ID: i.ID, Name: i.Name, Type: string(i.Type), Shell: i.Shell, CreatedAt: i.CreatedAt,
	})
	if err != nil {
		r.log.Warn("cannot record session start", "session_id", i.ID, "error", err)
	}
}

func (r *Recorder) Ended(id string, reason terminal.EndReason, exitCode int) {
	var code *int
	if exitCode >= 0 {
		code = &exitCode
	}
	stored := storage.EndExited
	if reason == terminal.EndedExpired {
		stored = storage.EndExpired
	}
	if err := r.db.EndSession(id, stored, code, r.now()); err != nil {
		r.log.Warn("cannot record session end", "session_id", id, "error", err)
	}
}

func (r *Recorder) Removed(id string) {
	if _, err := r.db.DeleteSession(id); err != nil {
		r.log.Warn("cannot delete session record", "session_id", id, "error", err)
	}
}
