package server

import (
	"net"
	"net/http"
	"strconv"
	"time"
)

const codeRateLimited = "RATE_LIMITED"

// Defaults for the configurable limits (see Options).
const (
	defaultAuthMaxFailures = 10
	defaultAuthLockout     = 5 * time.Minute
	defaultOpsPerMinute    = 30
	failureWindow          = time.Minute
	globalPairingFailures  = 30 // across all clients
)

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	secs := int(retryAfter.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, codeRateLimited, "Too many requests. Try again later.")
}

// authBlocked rejects clients locked out after repeated authentication
// failures. It reports whether the request was rejected.
func (s *Server) authBlocked(w http.ResponseWriter, r *http.Request) bool {
	if blocked, wait := s.authFails.Blocked(clientIP(r)); blocked {
		writeRateLimited(w, wait)
		return true
	}
	return false
}

// allowOp applies a token bucket to a sensitive operation.
func (s *Server) allowOp(w http.ResponseWriter, b interface {
	Allow(string) (bool, time.Duration)
}, key string) bool {
	if ok, wait := b.Allow(key); !ok {
		writeRateLimited(w, wait)
		return false
	}
	return true
}
