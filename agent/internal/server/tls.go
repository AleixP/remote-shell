package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// certReloader serves a certificate from disk and reloads it when the files
// change, so a renewed Tailscale certificate is picked up without a restart.
type certReloader struct {
	certFile, keyFile string

	mu      sync.Mutex
	cert    *tls.Certificate
	certMod time.Time
	keyMod  time.Time
}

func newCertReloader(certFile, keyFile string) (*certReloader, error) {
	r := &certReloader{certFile: certFile, keyFile: keyFile}
	if _, err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

// load (re)reads the key pair if the files changed and validates it.
func (r *certReloader) load() (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ci, err := os.Stat(r.certFile)
	if err != nil {
		return nil, fmt.Errorf("tls certificate: %w", err)
	}
	ki, err := os.Stat(r.keyFile)
	if err != nil {
		return nil, fmt.Errorf("tls key: %w", err)
	}
	if r.cert != nil && ci.ModTime().Equal(r.certMod) && ki.ModTime().Equal(r.keyMod) {
		if time.Now().Before(r.cert.Leaf.NotAfter) {
			return r.cert, nil
		}
		// Expired and unchanged on disk: fall through to report it.
	}

	pair, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls key pair: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse tls certificate: %w", err)
	}
	if time.Now().After(leaf.NotAfter) {
		return nil, fmt.Errorf("tls certificate expired on %s", leaf.NotAfter.Format(time.RFC3339))
	}
	if len(leaf.DNSNames) == 0 {
		return nil, errors.New("tls certificate has no DNS names")
	}
	pair.Leaf = leaf
	r.cert, r.certMod, r.keyMod = &pair, ci.ModTime(), ki.ModTime()
	return r.cert, nil
}

func (r *certReloader) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.load()
}

func (r *certReloader) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: r.getCertificate,
	}
}
