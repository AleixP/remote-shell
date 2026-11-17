package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPersistsAcrossReopenAndMigratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "agent.db")

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertDevice(Device{ID: "dev_1", Name: "phone", TokenHash: "h1", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path) // runs migrations again: must be a no-op
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ds, err := db.ListDevices()
	if err != nil || len(ds) != 1 || ds[0].ID != "dev_1" {
		t.Fatalf("ListDevices = %v, %v", ds, err)
	}
}

func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := filepath.Join(t.TempDir(), "data")
	db, err := Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "agent.db"): 0o600} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
}

func TestDuplicateTokenHashRejected(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := Device{ID: "a", Name: "n", TokenHash: "same", CreatedAt: time.Now()}
	if err := db.InsertDevice(d); err != nil {
		t.Fatal(err)
	}
	d.ID = "b"
	if err := db.InsertDevice(d); err == nil {
		t.Fatal("duplicate token hash accepted")
	}
}
