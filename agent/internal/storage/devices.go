package storage

import (
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("storage: not found")

// Device is an authorized mobile device. TokenHash is never exposed outside
// the agent.
type Device struct {
	ID         string
	Name       string
	TokenHash  string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

const timeFmt = time.RFC3339Nano

func (db *DB) InsertDevice(d Device) error {
	_, err := db.sql.Exec(
		`INSERT INTO devices (id, name, token_hash, created_at) VALUES (?, ?, ?, ?)`,
		d.ID, d.Name, d.TokenHash, d.CreatedAt.UTC().Format(timeFmt))
	return err
}

type scanner interface{ Scan(...any) error }

func scanDevice(s scanner) (Device, error) {
	var d Device
	var created string
	var last, revoked sql.NullString
	if err := s.Scan(&d.ID, &d.Name, &d.TokenHash, &created, &last, &revoked); err != nil {
		return Device{}, err
	}
	d.CreatedAt, _ = time.Parse(timeFmt, created)
	if last.Valid {
		t, _ := time.Parse(timeFmt, last.String)
		d.LastUsedAt = &t
	}
	if revoked.Valid {
		t, _ := time.Parse(timeFmt, revoked.String)
		d.RevokedAt = &t
	}
	return d, nil
}

const deviceCols = `id, name, token_hash, created_at, last_used_at, revoked_at`

// DeviceByTokenHash returns the device owning a token, including revoked ones.
func (db *DB) DeviceByTokenHash(hash string) (Device, error) {
	d, err := scanDevice(db.sql.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE token_hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// ListDevices returns the non-revoked devices, oldest first.
func (db *DB) ListDevices() ([]Device, error) {
	rows, err := db.sql.Query(`SELECT ` + deviceCols + ` FROM devices WHERE revoked_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RevokeDevice marks a non-revoked device as revoked.
func (db *DB) RevokeDevice(id string, at time.Time) error {
	res, err := db.sql.Exec(`UPDATE devices SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, at.UTC().Format(timeFmt), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) TouchDevice(id string, at time.Time) error {
	_, err := db.sql.Exec(`UPDATE devices SET last_used_at = ? WHERE id = ?`, at.UTC().Format(timeFmt), id)
	return err
}
