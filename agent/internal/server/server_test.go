package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/auth"
	"github.com/AleixP/remote-shell/agent/internal/sessionlog"
	"github.com/AleixP/remote-shell/agent/internal/storage"
	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

// writeCert writes a self-signed certificate and key into dir and returns
// their paths.
func writeCert(t *testing.T, dir string, serial int64, dns []string, notAfter time.Time) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "test"},
		DNSNames:     dns,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, "test.crt")
	keyFile = filepath.Join(dir, "test.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

type testServer struct {
	db       *storage.DB
	sessions *terminal.Manager
	auth     *auth.Service
	srv      *Server
	url      string
	certFile string
	keyFile  string
	dir      string
	cancel   context.CancelFunc
	done     chan error
}

func startServer(t *testing.T) *testServer { return startServerMax(t, 8) }

func startServerMax(t *testing.T, maxSessions int) *testServer {
	return startServerOpts(t, maxSessions, 0)
}

func startServerOpts(t *testing.T, maxSessions int, ticketTTL time.Duration) *testServer {
	return startServerWith(t, maxSessions, func(o *Options) { o.TicketTTL = ticketTTL })
}

// startServerWith starts a server with generous limits; mutate can change any option.
func startServerWith(t *testing.T, maxSessions int, mutate func(*Options)) *testServer {
	t.Helper()
	dir := t.TempDir()
	certFile, keyFile := writeCert(t, dir, 1, []string{"localhost"}, time.Now().Add(time.Hour))
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	authSvc := auth.New(db)
	sessions := terminal.NewManager(maxSessions)
	sessions.SetRecorder(sessionlog.New(db, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(sessions.CloseAll)
	opts := Options{Listen: "127.0.0.1", Port: 8443, TLSCertFile: certFile, TLSKeyFile: keyFile,
		Auth: authSvc, History: db, Sessions: sessions, Shells: terminal.DetectShells(),
		AuthMaxFailures: 1000, OpsPerMinute: 6000,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if mutate != nil {
		mutate(&opts)
	}
	srv, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ts := &testServer{db: db, sessions: sessions, auth: authSvc, srv: srv, certFile: certFile, keyFile: keyFile, dir: dir, cancel: cancel, done: make(chan error, 1)}
	go func() { ts.done <- srv.Serve(ctx, ln) }()
	ts.url = "https://" + ln.Addr().String()
	t.Cleanup(func() { cancel() })
	return ts
}

// client trusts any certificate: tests check what the server presents, not
// chain validation (which is the OS/Tailscale's job).
func insecureClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
	}
}

func TestResolveListenAddr(t *testing.T) {
	ok := map[string]string{
		"127.0.0.1":         "127.0.0.1:8443",
		"100.120.42.44":     "100.120.42.44:8443",
		"::1":               "[::1]:8443",
		"fd7a:115c:a1e0::1": "[fd7a:115c:a1e0::1]:8443",
	}
	for in, want := range ok {
		got, err := ResolveListenAddr(in, 8443)
		if err != nil || got != want {
			t.Errorf("ResolveListenAddr(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"0.0.0.0", "::", "8.8.8.8", "192.168.1.10", "10.0.0.1", "example.com", "100.128.0.1"} {
		if _, err := ResolveListenAddr(in, 8443); err == nil {
			t.Errorf("ResolveListenAddr(%q) succeeded, want error", in)
		}
	}
	if _, err := ResolveListenAddr("127.0.0.1", 0); err == nil {
		t.Error("port 0 accepted")
	}
}

func TestNewRejectsBadCertificates(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	authSvc := auth.New(db)
	opts := func(c, k string) Options {
		return Options{Listen: "127.0.0.1", Port: 8443, TLSCertFile: c, TLSKeyFile: k, Auth: authSvc,
			Sessions: terminal.NewManager(), Shells: terminal.DetectShells()}
	}

	if _, err := New(opts(filepath.Join(dir, "no.crt"), filepath.Join(dir, "no.key"))); err == nil {
		t.Error("missing certificate accepted")
	}
	c, k := writeCert(t, dir, 1, []string{"localhost"}, time.Now().Add(-time.Minute))
	if _, err := New(opts(c, k)); err == nil {
		t.Error("expired certificate accepted")
	}
	c, k = writeCert(t, dir, 2, nil, time.Now().Add(time.Hour))
	if _, err := New(opts(c, k)); err == nil {
		t.Error("certificate without DNS names accepted")
	}
	wild := opts(c, k)
	wild.Listen = "0.0.0.0"
	if _, err := New(wild); err == nil {
		t.Error("wildcard listen address accepted")
	}
}

func TestHealth(t *testing.T) {
	ts := startServer(t)
	resp, err := insecureClient().Get(ts.url + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["version"] != Version {
		t.Errorf("body = %v", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestUnknownPathIsJSON404(t *testing.T) {
	ts := startServer(t)
	resp, err := insecureClient().Get(ts.url + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body errorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound || body.Error.Code != codeNotFound {
		t.Errorf("status=%d body=%+v", resp.StatusCode, body)
	}
}

func TestPlainHTTPIsNotServed(t *testing.T) {
	ts := startServer(t)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + ts.url[len("https://"):] + "/api/v1/health")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatal("health served over plain HTTP")
		}
	}
}

func TestCertificateIsReloaded(t *testing.T) {
	ts := startServer(t)

	serial := func() int64 {
		conn, err := tls.Dial("tcp", ts.url[len("https://"):], &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		return conn.ConnectionState().PeerCertificates[0].SerialNumber.Int64()
	}
	if got := serial(); got != 1 {
		t.Fatalf("serial = %d, want 1", got)
	}

	writeCert(t, ts.dir, 2, []string{"localhost"}, time.Now().Add(2*time.Hour))
	future := time.Now().Add(time.Minute)
	for _, f := range []string{ts.certFile, ts.keyFile} {
		if err := os.Chtimes(f, future, future); err != nil {
			t.Fatal(err)
		}
	}
	if got := serial(); got != 2 {
		t.Fatalf("serial after renewal = %d, want 2", got)
	}
}

func TestGracefulShutdown(t *testing.T) {
	ts := startServer(t)
	ts.cancel()
	select {
	case err := <-ts.done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}
