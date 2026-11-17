package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/auth"
	"github.com/AleixP/remote-shell/agent/internal/control"
	"github.com/AleixP/remote-shell/agent/internal/storage"
)

// startControl runs a control server in a short data dir and returns the dir.
func startControl(t *testing.T) (string, *auth.Service) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := auth.New(db)
	srv, err := control.NewServer(control.Options{
		SocketPath: filepath.Join(dir, control.SocketName), Auth: a,
		Host: func() string { return "mac.example.ts.net:8443" }, ComputerName: "Mac",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()

	client := control.NewClient(filepath.Join(dir, control.SocketName))
	for i := 0; i < 500; i++ {
		if _, err := client.ListDevices(ctx); err == nil {
			return dir, a
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("control server did not start")
	return "", nil
}

func TestPairCommandShowsQR(t *testing.T) {
	dir, _ := startControl(t)
	var out, errOut bytes.Buffer
	if code := runPair([]string{"-data-dir", dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Scan this QR code") || !strings.ContainsAny(out.String(), "█▀▄") {
		t.Errorf("output has no QR code:\n%s", out.String())
	}
	// The secret only travels inside the QR, never as printed text.
	if strings.Contains(out.String(), "code=") || strings.Contains(out.String(), "remoteshell://") {
		t.Errorf("pairing secret printed as text:\n%s", out.String())
	}
}

func TestDevicesCommands(t *testing.T) {
	dir, a := startControl(t)
	code, _ := a.NewPairingCode(time.Minute)
	dev, _, err := a.Pair(code, "iPhone")
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if rc := runDevices([]string{"list", "-data-dir", dir}, &out, &errOut); rc != 0 || !strings.Contains(out.String(), dev.ID) || !strings.Contains(out.String(), "iPhone") {
		t.Fatalf("list rc=%d out=%q err=%q", rc, out.String(), errOut.String())
	}
	out.Reset()
	if rc := runDevices([]string{"revoke", dev.ID, "-data-dir", dir}, &out, &errOut); rc != 0 || !strings.Contains(out.String(), "revoked") {
		t.Fatalf("revoke rc=%d out=%q err=%q", rc, out.String(), errOut.String())
	}
	out.Reset()
	if rc := runDevices([]string{"list", "-data-dir", dir}, &out, &errOut); rc != 0 || !strings.Contains(out.String(), "No authorized devices") {
		t.Fatalf("list after revoke rc=%d out=%q", rc, out.String())
	}
	if rc := runDevices([]string{"revoke", dev.ID, "-data-dir", dir}, &out, &errOut); rc != 1 {
		t.Errorf("revoking twice rc=%d, want 1", rc)
	}
}

func TestCommandsFailWhenAgentIsNotRunning(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if rc := runPair([]string{"-data-dir", dir}, &out, &errOut); rc != 1 || !strings.Contains(errOut.String(), "is it running") {
		t.Errorf("pair rc=%d err=%q", rc, errOut.String())
	}
}

func TestDevicesUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	for _, args := range [][]string{nil, {"nope"}, {"revoke"}} {
		if rc := runDevices(args, &out, &errOut); rc != 2 {
			t.Errorf("runDevices(%v) rc=%d, want 2", args, rc)
		}
	}
}
