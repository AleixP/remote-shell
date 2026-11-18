package doctor

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var (
	tsAddr  = netip.MustParseAddr("100.101.102.103")
	lanAddr = netip.MustParseAddr("192.168.1.50")
	host    = "mac.tail1234.ts.net"
)

type pki struct {
	certFile, keyFile string
	pool              *x509.CertPool
	pair              tls.Certificate
}

func newPKI(t *testing.T, dns []string, notAfter time.Time) pki {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}, DNSNames: dns,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	dir := t.TempDir()
	p := pki{certFile: filepath.Join(dir, "c.crt"), keyFile: filepath.Join(dir, "c.key")}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(p.certFile, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	p.pool = x509.NewCertPool()
	p.pool.AddCert(cert)
	p.pair, _ = tls.X509KeyPair(certPEM, keyPEM)
	return p
}

// tlsServer starts a TLS listener standing in for the agent.
func tlsServer(t *testing.T, p pki) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{p.pair}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = c.(*tls.Conn).Handshake(); _ = c.Close() }()
		}
	}()
	return ln.Addr().String()
}

// options builds a healthy setup. Connections to the agent's Tailscale address
// reach `agentAt` (empty: nothing is listening); every other address is refused
// unless listed in `open`.
func options(t *testing.T, p pki, agentAt string, open ...netip.Addr) Options {
	t.Helper()
	dataDir := t.TempDir()
	_ = os.Chmod(dataDir, 0o700)
	return Options{
		CertFile: p.certFile, KeyFile: p.keyFile, DataDir: dataDir, Port: 8443, RootCAs: p.pool,
		TailscaleAddr: func() (netip.Addr, error) { return tsAddr, nil },
		LocalAddrs: func() ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1"), tsAddr, lanAddr, netip.MustParseAddr("fe80::1")}, nil
		},
		LookupHost: func(context.Context, string) ([]string, error) { return []string{tsAddr.String()}, nil },
		Dial: func(_ context.Context, _, a string) (net.Conn, error) {
			if a == net.JoinHostPort(tsAddr.String(), "8443") && agentAt != "" {
				return net.Dial("tcp", agentAt)
			}
			for _, o := range open {
				if a == net.JoinHostPort(o.String(), "8443") {
					c1, _ := net.Pipe()
					return c1, nil
				}
			}
			return nil, errors.New("connection refused")
		},
	}
}

func find(rs []Result, name string) (Result, bool) {
	for _, r := range rs {
		if r.Name == name {
			return r, true
		}
	}
	return Result{}, false
}

func status(t *testing.T, rs []Result, name string) Status {
	t.Helper()
	r, ok := find(rs, name)
	if !ok {
		t.Fatalf("no %q result in %+v", name, rs)
	}
	return r.Status
}

func TestHealthySetupPassesEveryCheck(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	rs := Run(context.Background(), options(t, p, tlsServer(t, p)))
	for _, r := range rs {
		if r.Status != OK {
			t.Errorf("%s: %v %s", r.Name, r.Status, r.Detail)
		}
	}
	for _, name := range []string{"Privileges", "Tailscale", "TLS certificate", "MagicDNS name", "File permissions", "Agent", "Exposure", "TLS handshake"} {
		if _, ok := find(rs, name); !ok {
			t.Errorf("missing check %q", name)
		}
	}
	var out bytes.Buffer
	if code := Print(&out, rs); code != 0 || !strings.Contains(out.String(), "✓ Tailscale") {
		t.Errorf("Print = %d\n%s", code, out.String())
	}
}

func TestAgentAnsweringOutsideTailscaleIsAFailure(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	rs := Run(context.Background(), options(t, p, tlsServer(t, p), lanAddr))
	r, _ := find(rs, "Exposure")
	if r.Status != Fail || !strings.Contains(r.Detail, "192.168.1.50:8443") {
		t.Fatalf("exposure = %+v", r)
	}
	var out bytes.Buffer
	if code := Print(&out, rs); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestExposureIgnoresTailscaleLoopbackAndLinkLocalAddresses(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	dialed := map[string]bool{}
	o := options(t, p, tlsServer(t, p))
	inner := o.Dial
	o.Dial = func(ctx context.Context, n, a string) (net.Conn, error) { dialed[a] = true; return inner(ctx, n, a) }
	Run(context.Background(), o)
	for a := range dialed {
		if strings.HasPrefix(a, "127.") || strings.Contains(a, "fe80") {
			t.Errorf("exposure check dialed %s", a)
		}
	}
	if !dialed["192.168.1.50:8443"] {
		t.Error("the LAN address was not checked")
	}
}

func TestAgentNotRunning(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	rs := Run(context.Background(), options(t, p, ""))
	if status(t, rs, "Agent") != Info {
		t.Errorf("agent = %+v", rs)
	}
	for _, n := range []string{"Exposure", "TLS handshake"} {
		if _, ok := find(rs, n); ok {
			t.Errorf("%q checked although the agent is not running", n)
		}
	}
	// A running agent that does not answer on the expected address is a warning.
	o := options(t, p, "")
	o.AgentRunning = func(context.Context) bool { return true }
	if status(t, Run(context.Background(), o), "Agent") != Warn {
		t.Error("running agent on another address should warn")
	}
}

func TestCertificateStates(t *testing.T) {
	cases := []struct {
		name  string
		dns   []string
		after time.Time
		want  Status
	}{
		{"valid", []string{host}, time.Now().Add(60 * 24 * time.Hour), OK},
		{"expiring soon", []string{host}, time.Now().Add(10 * 24 * time.Hour), Warn},
		{"expired", []string{host}, time.Now().Add(-time.Hour), Fail},
		{"no DNS names", nil, time.Now().Add(90 * 24 * time.Hour), Fail},
	}
	for _, c := range cases {
		p := newPKI(t, c.dns, c.after)
		rs := Run(context.Background(), options(t, p, ""))
		if got := status(t, rs, "TLS certificate"); got != c.want {
			r, _ := find(rs, "TLS certificate")
			t.Errorf("%s: %v, want %v (%s)", c.name, got, c.want, r.Detail)
		}
	}
}

func TestMissingOrMismatchedCertificateFiles(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	o := options(t, p, "")
	o.CertFile = filepath.Join(t.TempDir(), "nope.crt")
	if status(t, Run(context.Background(), o), "TLS certificate") != Fail {
		t.Error("missing certificate accepted")
	}
	other := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	o = options(t, p, "")
	o.KeyFile = other.keyFile // a key that does not belong to the certificate
	if status(t, Run(context.Background(), o), "TLS certificate") != Fail {
		t.Error("mismatched key accepted")
	}
}

func TestHostnameChecks(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	o := options(t, p, "")
	o.LookupHost = func(context.Context, string) ([]string, error) { return []string{"10.1.1.1"}, nil }
	if status(t, Run(context.Background(), o), "MagicDNS name") != Warn {
		t.Error("a name resolving to another address should warn")
	}
	o.LookupHost = func(context.Context, string) ([]string, error) { return nil, errors.New("no such host") }
	if status(t, Run(context.Background(), o), "MagicDNS name") != Warn {
		t.Error("an unresolvable name should warn")
	}
}

func TestMissingTailscaleIsAFailure(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	o := options(t, p, "")
	o.TailscaleAddr = func() (netip.Addr, error) { return netip.Addr{}, errors.New("no Tailscale address found") }
	rs := Run(context.Background(), o)
	if status(t, rs, "Tailscale") != Fail {
		t.Error("missing Tailscale not reported")
	}
	if _, ok := find(rs, "Agent"); ok {
		t.Error("agent checked without a Tailscale address")
	}
}

func TestUntrustedCertificateFailsTheHandshake(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	o := options(t, p, tlsServer(t, p))
	o.RootCAs = x509.NewCertPool() // the client trusts nobody
	r, _ := find(Run(context.Background(), o), "TLS handshake")
	if r.Status != Fail || !strings.Contains(r.Detail, "reject") {
		t.Fatalf("handshake = %+v", r)
	}
}

func TestElevatedPrivilegesFail(t *testing.T) {
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	o := options(t, p, "")
	o.Elevated = true
	if status(t, Run(context.Background(), o), "Privileges") != Fail {
		t.Error("elevated run not reported")
	}
}

func TestWorldReadableKeyWarns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	p := newPKI(t, []string{host}, time.Now().Add(90*24*time.Hour))
	if err := os.Chmod(p.keyFile, 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := find(Run(context.Background(), options(t, p, "")), "File permissions")
	if r.Status != Warn || !strings.Contains(r.Detail, "TLS private key") {
		t.Fatalf("permissions = %+v", r)
	}
}
