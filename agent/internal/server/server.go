package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Version is the agent version reported by /api/v1/health.
const Version = "0.1.0"

const (
	maxBodyBytes    = 1 << 20
	shutdownTimeout = 5 * time.Second
)

// Options configures a Server.
type Options struct {
	// Listen is the IP to listen on. Empty means the Tailscale address.
	Listen string
	Port   int

	TLSCertFile string
	TLSKeyFile  string

	Logger *slog.Logger
}

// Server is the agent's HTTPS server.
type Server struct {
	addr string
	log  *slog.Logger
	http *http.Server
}

// New validates the options and prepares the server. It fails if the listen
// address is not private or the TLS certificate cannot be used.
func New(opts Options) (*Server, error) {
	addr, err := ResolveListenAddr(opts.Listen, opts.Port)
	if err != nil {
		return nil, err
	}
	certs, err := newCertReloader(opts.TLSCertFile, opts.TLSKeyFile)
	if err != nil {
		return nil, err
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	s := &Server{addr: addr, log: log}
	s.http = &http.Server{
		Handler:           s.handler(),
		TLSConfig:         certs.tlsConfig(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return s, nil
}

// Addr returns the address the server listens on.
func (s *Server) Addr() string { return s.addr }

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, codeNotFound, "The requested resource does not exist.")
	})
	return s.recoverPanics(limitBody(mux))
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic in handler", "path", r.URL.Path, "panic", fmt.Sprint(v))
				writeError(w, http.StatusInternalServerError, codeInternal, "Unexpected agent error.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
}

// Run listens on the configured address and serves HTTPS until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve serves HTTPS on ln until ctx is done, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.addr = ln.Addr().String()
	s.log.Info("https server listening", "addr", s.addr)

	errc := make(chan error, 1)
	go func() { errc <- s.http.ServeTLS(ln, "", "") }()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			_ = s.http.Close()
			return err
		}
		return nil
	}
}
