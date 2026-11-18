-- Metadata of terminal sessions. Running processes are never stored here:
-- this table only remembers that a session existed and how it ended, so the
-- app can tell an ended session apart from one that is still running.
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL,
    shell      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    ended_at   TEXT,
    end_reason TEXT,
    exit_code  INTEGER
);

CREATE INDEX sessions_ended_at ON sessions (ended_at);
