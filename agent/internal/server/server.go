package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/auth"
	"github.com/AleixP/remote-shell/agent/internal/ratelimit"
	"github.com/AleixP/remote-shell/agent/internal/storage"
	"github.com/AleixP/remote-shell/agent/internal/terminal"
	"github.com/AleixP/remote-shell/agent/internal/ticket"
)

// Version is the agent version reported by /api/v1/health. Release builds set
// it with -ldflags "-X github.com/AleixP/remote-shell/agent/internal/server.Version=1.2.3".
var Version = "dev"

const (
	maxBodyBytes    = 64 << 10
	shutdownTimeout = 5 * time.Second
)

// History gives access to the sessions that have ended. It is optional: without
// it, an ended session simply stops existing.
type History interface {
	ListEndedSessions(since time.Time) ([]storage.SessionRecord, error)
	GetEndedSession(id string, since time.Time) (storage.SessionRecord, error)
	DeleteSession(id string) (bool, error)
}

// Options configures a Server.
type Options struct {
	// Listen is the IP to listen on. Empty means the Tailscale address.
	Listen string
	Port   int

	TLSCertFile string
	TLSKeyFile  string

	Auth    *auth.Service
	History History
	// HistoryRetention is how long ended sessions stay visible (default 24h).
	HistoryRetention time.Duration
	Sessions         *terminal.Manager
	Shells           *terminal.Shells
	// TicketTTL is how long a WebSocket connection ticket stays valid
	// (default 60s).
	TicketTTL time.Duration

	// AuthMaxFailures failed authentications or pairings from one client
	// within a minute lock it out for AuthLockout (defaults: 10 and 5m).
	AuthMaxFailures int
	AuthLockout     time.Duration
	// OpsPerMinute bounds ticket requests, session creation and WebSocket
	// connection attempts per device or client (default 30).
	OpsPerMinute int

	Logger *slog.Logger
}

// Server is the agent's HTTPS server.
type Server struct {
	addr      string
	port      int
	certs     *certReloader
	auth      *auth.Service
	history   History
	retention time.Duration
	sessions  *terminal.Manager
	shells    *terminal.Shells
	tickets   *ticket.Store
	conns     *connRegistry

	authFails    *ratelimit.Lockout // failed auth/ticket attempts per client IP
	pairFails    *ratelimit.Lockout // failed pairings per client IP
	pairFailsAll *ratelimit.Lockout // failed pairings from anyone
	ticketLimit  *ratelimit.Bucket  // per device
	sessionLimit *ratelimit.Bucket  // per device
	connectLimit *ratelimit.Bucket  // per client IP
	log          *slog.Logger
	http         *http.Server
}

// New validates the options and prepares the server. It fails if the listen
// address is not private or the TLS certificate cannot be used.
func New(opts Options) (*Server, error) {
	if opts.Auth == nil || opts.Sessions == nil || opts.Shells == nil {
		return nil, errors.New("server: auth service, session manager and shells are required")
	}
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

	ttl := opts.TicketTTL
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	maxFails, lockout, ops := opts.AuthMaxFailures, opts.AuthLockout, opts.OpsPerMinute
	if maxFails <= 0 {
		maxFails = defaultAuthMaxFailures
	}
	if lockout <= 0 {
		lockout = defaultAuthLockout
	}
	if ops <= 0 {
		ops = defaultOpsPerMinute
	}
	rate, burst := float64(ops)/60, max(ops/3, 3)
	s := &Server{
		authFails:    ratelimit.NewLockout(maxFails, failureWindow, lockout),
		pairFails:    ratelimit.NewLockout(maxFails, failureWindow, lockout),
		pairFailsAll: ratelimit.NewLockout(globalPairingFailures, failureWindow, lockout),
		ticketLimit:  ratelimit.NewBucket(rate, burst),
		sessionLimit: ratelimit.NewBucket(rate, burst),
		connectLimit: ratelimit.NewBucket(rate, burst),
	}
	s.tickets, s.conns = ticket.NewStore(ttl), newConnRegistry()
	s.addr, s.port, s.certs, s.auth = addr, opts.Port, certs, opts.Auth
	s.sessions, s.shells, s.log = opts.Sessions, opts.Shells, log
	s.history, s.retention = opts.History, opts.HistoryRetention
	if s.retention <= 0 {
		s.retention = 24 * time.Hour
	}
	// A revoked device loses its tickets and its open terminal connections.
	opts.Auth.OnRevoke(func(deviceID string) {
		s.tickets.RevokeDevice(deviceID)
		s.conns.closeDevice(deviceID)
	})
	s.http = &http.Server{
		Handler:           s.handler(),
		TLSConfig:         certs.tlsConfig(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	return s, nil
}

// Host returns the host:port clients use to reach the agent: the first DNS name
// of the TLS certificate (the MagicDNS name) and the configured port.
func (s *Server) Host() string {
	cert, err := s.certs.load()
	if err != nil || len(cert.Leaf.DNSNames) == 0 {
		return s.addr
	}
	return net.JoinHostPort(cert.Leaf.DNSNames[0], strconv.Itoa(s.port))
}

// Addr returns the address the server listens on.
func (s *Server) Addr() string { return s.addr }

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("POST /api/v1/pair", s.handlePair)
	mux.Handle("GET /api/v1/devices", s.requireAuth(s.handleListDevices))
	mux.Handle("DELETE /api/v1/devices/{id}", s.requireAuth(s.handleRevokeDevice))
	mux.Handle("GET /api/v1/sessions", s.requireAuth(s.handleListSessions))
	mux.Handle("POST /api/v1/sessions", s.requireAuth(s.handleCreateSession))
	mux.Handle("GET /api/v1/sessions/{id}", s.requireAuth(s.handleGetSession))
	mux.Handle("DELETE /api/v1/sessions/{id}", s.requireAuth(s.handleCloseSession))
	mux.Handle("POST /api/v1/sessions/{id}/connection-ticket", s.requireAuth(s.handleConnectionTicket))
	mux.HandleFunc("GET /api/v1/sessions/{id}/terminal", s.handleTerminal)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, codeNotFound, "The requested resource does not exist.")
	})
	return s.recoverPanics(noStore(limitBody(mux)))
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
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
