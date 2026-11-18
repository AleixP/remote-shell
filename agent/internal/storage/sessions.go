package storage

import (
	"database/sql"
	"errors"
	"time"
)

// End reasons stored for a session that is no longer running.
const (
	EndExited       = "exited"        // the shell process ended on its own
	EndExpired      = "expired"       // a temporary session timed out
	EndAgentRestart = "agent_restart" // the agent stopped while it was running
)

// SessionRecord is the persisted metadata of a session.
type SessionRecord struct {
	ID        string
	Name      string
	Type      string
	Shell     string
	CreatedAt time.Time
	EndedAt   *time.Time // nil while the session may still be running
	EndReason string
	ExitCode  *int
}

// InsertSession records a new running session.
func (db *DB) InsertSession(r SessionRecord) error {
	_, err := db.sql.Exec(
		`INSERT INTO sessions (id, name, type, shell, created_at) VALUES (?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.Type, r.Shell, r.CreatedAt.UTC().Format(timeFmt))
	return err
}

// EndSession marks a running session as ended. exitCode may be nil if unknown.
func (db *DB) EndSession(id, reason string, exitCode *int, at time.Time) error {
	var code sql.NullInt64
	if exitCode != nil {
		code = sql.NullInt64{Int64: int64(*exitCode), Valid: true}
	}
	_, err := db.sql.Exec(
		`UPDATE sessions SET ended_at = ?, end_reason = ?, exit_code = ? WHERE id = ? AND ended_at IS NULL`,
		at.UTC().Format(timeFmt), reason, code, id)
	return err
}

// DeleteSession removes a session record. It reports whether one existed.
func (db *DB) DeleteSession(id string) (bool, error) {
	res, err := db.sql.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkRunningSessionsLost ends every session still recorded as running. It is
// called when the agent starts: processes do not survive an agent restart, so
// a session recorded as running cannot be alive.
func (db *DB) MarkRunningSessionsLost(at time.Time) (int64, error) {
	res, err := db.sql.Exec(
		`UPDATE sessions SET ended_at = ?, end_reason = ? WHERE ended_at IS NULL`,
		at.UTC().Format(timeFmt), EndAgentRestart)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanSession(s scanner) (SessionRecord, error) {
	var r SessionRecord
	var created string
	var ended, reason sql.NullString
	var code sql.NullInt64
	if err := s.Scan(&r.ID, &r.Name, &r.Type, &r.Shell, &created, &ended, &reason, &code); err != nil {
		return SessionRecord{}, err
	}
	r.CreatedAt, _ = time.Parse(timeFmt, created)
	if ended.Valid {
		t, _ := time.Parse(timeFmt, ended.String)
		r.EndedAt = &t
	}
	r.EndReason = reason.String
	if code.Valid {
		c := int(code.Int64)
		r.ExitCode = &c
	}
	return r, nil
}

const sessionCols = `id, name, type, shell, created_at, ended_at, end_reason, exit_code`

// ListEndedSessions returns sessions that ended at or after since, oldest first.
func (db *DB) ListEndedSessions(since time.Time) ([]SessionRecord, error) {
	rows, err := db.sql.Query(
		`SELECT `+sessionCols+` FROM sessions WHERE ended_at IS NOT NULL AND ended_at >= ? ORDER BY created_at, id`,
		since.UTC().Format(timeFmt))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionRecord
	for rows.Next() {
		r, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetEndedSession returns an ended session that ended at or after since.
func (db *DB) GetEndedSession(id string, since time.Time) (SessionRecord, error) {
	r, err := scanSession(db.sql.QueryRow(
		`SELECT `+sessionCols+` FROM sessions WHERE id = ? AND ended_at IS NOT NULL AND ended_at >= ?`,
		id, since.UTC().Format(timeFmt)))
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, ErrNotFound
	}
	return r, err
}

// PurgeEndedSessions deletes sessions that ended before the given time.
func (db *DB) PurgeEndedSessions(before time.Time) (int64, error) {
	res, err := db.sql.Exec(
		`DELETE FROM sessions WHERE ended_at IS NOT NULL AND ended_at < ?`, before.UTC().Format(timeFmt))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
