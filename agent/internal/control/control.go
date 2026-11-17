// Package control is the agent's local control interface: an HTTP API served
// over a Unix domain socket that only the user running the agent can open. The
// CLI (pair, devices) uses it; it is never reachable from the network.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/auth"
)

// SocketName is the control socket file name inside the data directory.
const SocketName = "control.sock"

// PairingInfo is returned when a pairing code is created.
type PairingInfo struct {
	// URL is the QR payload.
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// DeviceInfo describes an authorized device.
type DeviceInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Options configures the control server.
type Options struct {
	SocketPath string
	Auth       *auth.Service
	// Host returns the agent's public host:port (MagicDNS name and port) put in
	// the pairing QR code.
	Host func() string
	// ComputerName is the display name put in the pairing QR code.
	ComputerName string
	PairingTTL   time.Duration
	Logger       *slog.Logger
}

// PairingURL builds the pairing QR payload. It never contains a permanent
// credential, only the short-lived pairing code.
func PairingURL(host, code, name string) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("host", host)
	q.Set("code", code)
	q.Set("name", name)
	return (&url.URL{Scheme: "remoteshell", Host: "pair", RawQuery: q.Encode()}).String()
}

// Server serves the control API.
type Server struct {
	opts Options
	log  *slog.Logger
	http *http.Server
}

func NewServer(opts Options) (*Server, error) {
	if opts.Auth == nil || opts.Host == nil || opts.SocketPath == "" {
		return nil, errors.New("control: auth, host and socket path are required")
	}
	if opts.PairingTTL <= 0 {
		opts.PairingTTL = 5 * time.Minute
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	s := &Server{opts: opts, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pairing-codes", s.handlePairingCode)
	mux.HandleFunc("GET /devices", s.handleListDevices)
	mux.HandleFunc("DELETE /devices/{id}", s.handleRevoke)
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return s, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handlePairingCode(w http.ResponseWriter, _ *http.Request) {
	code, exp := s.opts.Auth.NewPairingCode(s.opts.PairingTTL)
	s.log.Info("pairing code created", "expires_at", exp.Format(time.RFC3339))
	writeJSON(w, http.StatusCreated, PairingInfo{
		URL:       PairingURL(s.opts.Host(), code, s.opts.ComputerName),
		ExpiresAt: exp.UTC(),
	})
}

func (s *Server) handleListDevices(w http.ResponseWriter, _ *http.Request) {
	devices, err := s.opts.Auth.List()
	if err != nil {
		s.log.Error("list devices failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]DeviceInfo, len(devices))
	for i, d := range devices {
		out[i] = DeviceInfo{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt, LastUsedAt: d.LastUsedAt}
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch err := s.opts.Auth.Revoke(id); {
	case err == nil:
		s.log.Info("device revoked locally", "device_id", id)
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	case errors.Is(err, auth.ErrDeviceNotFound):
		writeErr(w, http.StatusNotFound, "device not found")
	default:
		s.log.Error("revoke failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

// listen creates the socket, replacing a stale one but refusing to take over a
// socket that another agent is still serving.
func listen(path string) (net.Listener, error) {
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = c.Close()
		return nil, fmt.Errorf("another agent is already running (control socket %s is in use)", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Only the owner may connect (the data directory is 0700 as well).
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// Run serves the control API until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ln, err := listen(s.opts.SocketPath)
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	defer os.Remove(s.opts.SocketPath)

	errc := make(chan error, 1)
	go func() { errc <- s.http.Serve(ln) }()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
		return nil
	}
}
