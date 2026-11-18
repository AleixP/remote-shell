package main

import (
	"os"
	"path/filepath"
)

const maxLogBytes = 10 << 20

// openLogFile opens the log file for appending, keeping one previous file
// (path + ".1") once it grows past maxLogBytes. Logs hold no secrets, but the
// file is still private to the user.
func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
