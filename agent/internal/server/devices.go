package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/auth"
)

type deviceCtxKey struct{}

// deviceFrom returns the authenticated device of a request.
func deviceFrom(ctx context.Context) auth.Device {
	d, _ := ctx.Value(deviceCtxKey{}).(auth.Device)
	return d
}

func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// requireAuth rejects requests without a valid, non-revoked device token.
func (s *Server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authBlocked(w, r) {
			return
		}
		dev, err := s.auth.Authenticate(bearerToken(r))
		switch {
		case err == nil:
			next(w, r.WithContext(context.WithValue(r.Context(), deviceCtxKey{}, dev)))
		case errors.Is(err, auth.ErrRevoked):
			s.authFails.Fail(clientIP(r))
			writeError(w, http.StatusUnauthorized, codeDeviceRevoked, "Device authorization revoked.")
		case errors.Is(err, auth.ErrUnauthorized):
			s.authFails.Fail(clientIP(r))
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "Authentication required.")
		default:
			s.log.Error("authentication failed", "error", err)
			writeError(w, http.StatusInternalServerError, codeInternal, "Unexpected agent error.")
		}
	})
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if blocked, wait := s.pairFails.Blocked(ip); blocked {
		writeRateLimited(w, wait)
		return
	}
	if blocked, wait := s.pairFailsAll.Blocked("*"); blocked {
		writeRateLimited(w, wait)
		return
	}
	var req struct {
		PairingCode string `json:"pairing_code"`
		DeviceName  string `json:"device_name"`
	}
	if !decodeJSON(r, &req) || req.PairingCode == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "Invalid request data.")
		return
	}
	dev, token, err := s.auth.Pair(req.PairingCode, req.DeviceName)
	switch {
	case err == nil:
		s.log.Info("device paired", "device_id", dev.ID)
		writeJSON(w, http.StatusCreated, map[string]string{
			"device_id":    dev.ID,
			"access_token": token,
			"token_type":   "Bearer",
		})
	case errors.Is(err, auth.ErrInvalidName):
		writeError(w, http.StatusUnprocessableEntity, codeInvalidRequest, "Invalid device name.")
	case errors.Is(err, auth.ErrInvalidPairing):
		s.pairFails.Fail(ip)
		s.pairFailsAll.Fail("*")
		s.log.Warn("pairing failed")
		writeError(w, http.StatusUnauthorized, codePairingExpired, "The pairing code is invalid or has expired.")
	default:
		s.log.Error("pairing failed", "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Unexpected agent error.")
	}
}

type deviceJSON struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	Current    bool    `json:"current"`
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	current := deviceFrom(r.Context())
	devices, err := s.auth.List()
	if err != nil {
		s.log.Error("list devices failed", "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Unexpected agent error.")
		return
	}
	out := make([]deviceJSON, 0, len(devices))
	for _, d := range devices {
		j := deviceJSON{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339), Current: d.ID == current.ID}
		if d.LastUsedAt != nil {
			t := d.LastUsedAt.UTC().Format(time.RFC3339)
			j.LastUsedAt = &t
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch err := s.auth.Revoke(id); {
	case err == nil:
		s.log.Info("device revoked", "device_id", id, "by", deviceFrom(r.Context()).ID)
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	case errors.Is(err, auth.ErrDeviceNotFound):
		writeError(w, http.StatusNotFound, codeDeviceNotFound, "The device does not exist or is already revoked.")
	default:
		s.log.Error("revoke failed", "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Unexpected agent error.")
	}
}
