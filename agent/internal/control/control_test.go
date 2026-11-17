package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/auth"
	"github.com/AleixP/remote-shell/agent/internal/storage"
)

// shortDir returns a short temp dir: Unix socket paths are limited to ~100 bytes.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

type env struct {
	auth   *auth.Service
	client *Client
	socket string
	cancel context.CancelFunc
	done   chan error
}

func newServer(t *testing.T, socket string, a *auth.Service) (*Server, error) {
	t.Helper()
	return NewServer(Options{
		SocketPath:   socket,
		Auth:         a,
		Host:         func() string { return "mac.example.ts.net:8443" },
		ComputerName: "My Mac",
		PairingTTL:   time.Minute,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func start(t *testing.T) *env {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := auth.New(db)
	socket := filepath.Join(shortDir(t), SocketName)
	srv, err := newServer(t, socket, a)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &env{auth: a, client: NewClient(socket), socket: socket, cancel: cancel, done: make(chan error, 1)}
	go func() { e.done <- srv.Run(ctx) }()
	t.Cleanup(cancel)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := e.client.ListDevices(context.Background()); err == nil {
			return e
		}
		if time.Now().After(deadline) {
			t.Fatal("control server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPairingCodeRoundTrip(t *testing.T) {
	e := start(t)
	info, err := e.client.NewPairingCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(info.URL)
	if err != nil || u.Scheme != "remoteshell" || u.Host != "pair" {
		t.Fatalf("bad pairing URL %q (%v)", info.URL, err)
	}
	q := u.Query()
	if q.Get("v") != "1" || q.Get("host") != "mac.example.ts.net:8443" || q.Get("name") != "My Mac" || len(q.Get("code")) != 26 {
		t.Fatalf("bad query %v", q)
	}
	if !info.ExpiresAt.After(time.Now()) {
		t.Errorf("ExpiresAt %v not in the future", info.ExpiresAt)
	}

	// The code in the QR pairs a device exactly once.
	if _, _, err := e.auth.Pair(q.Get("code"), "iPhone"); err != nil {
		t.Fatalf("pair with QR code: %v", err)
	}
	if _, _, err := e.auth.Pair(q.Get("code"), "again"); !errors.Is(err, auth.ErrInvalidPairing) {
		t.Errorf("reuse = %v", err)
	}
}

func TestListAndRevokeDevices(t *testing.T) {
	e := start(t)
	code, _ := e.auth.NewPairingCode(time.Minute)
	dev, _, err := e.auth.Pair(code, "iPhone")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	devices, err := e.client.ListDevices(ctx)
	if err != nil || len(devices) != 1 || devices[0].ID != dev.ID || devices[0].Name != "iPhone" {
		t.Fatalf("ListDevices = %+v, %v", devices, err)
	}
	if err := e.client.RevokeDevice(ctx, dev.ID); err != nil {
		t.Fatal(err)
	}
	if devices, _ := e.client.ListDevices(ctx); len(devices) != 0 {
		t.Errorf("revoked device still listed: %+v", devices)
	}
	if err := e.client.RevokeDevice(ctx, dev.ID); err == nil {
		t.Error("revoking twice succeeded")
	}
	if err := e.client.RevokeDevice(ctx, ""); err == nil {
		t.Error("empty id accepted")
	}
}

func TestAgentNotRunning(t *testing.T) {
	c := NewClient(filepath.Join(shortDir(t), SocketName))
	if _, err := c.ListDevices(context.Background()); !errors.Is(err, ErrAgentNotRunning) {
		t.Fatalf("err = %v, want ErrAgentNotRunning", err)
	}
}

func TestSecondServerRefusesBusySocket(t *testing.T) {
	e := start(t)
	srv, err := newServer(t, e.socket, e.auth)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Run(context.Background()); err == nil {
		t.Fatal("second server took over a socket in use")
	}
	// The first one keeps working.
	if _, err := e.client.ListDevices(context.Background()); err != nil {
		t.Fatalf("first server broken: %v", err)
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	socket := filepath.Join(shortDir(t), SocketName)
	if err := os.WriteFile(socket, nil, 0o600); err != nil { // leftover file, nobody listening
		t.Fatal(err)
	}
	ln, err := listen(socket)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	_ = ln.Close()
}

func TestSocketPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	e := start(t)
	fi, err := os.Stat(e.socket)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode = %o, want 600", perm)
	}
}

func TestSocketRemovedOnShutdown(t *testing.T) {
	e := start(t)
	e.cancel()
	select {
	case err := <-e.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not shut down")
	}
	if _, err := os.Stat(e.socket); !os.IsNotExist(err) {
		t.Errorf("socket file left behind: %v", err)
	}
}
