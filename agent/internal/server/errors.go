package server

import (
	"encoding/json"
	"net/http"
)

func decodeJSON(r *http.Request, v any) bool {
	return json.NewDecoder(r.Body).Decode(v) == nil
}

// Error codes shared with docs/WEBSOCKET_PROTOCOL.md.
const (
	codeInvalidRequest  = "INVALID_REQUEST"
	codeUnauthorized    = "UNAUTHORIZED"
	codeDeviceRevoked   = "DEVICE_REVOKED"
	codePairingExpired  = "PAIRING_EXPIRED"
	codeDeviceNotFound  = "DEVICE_NOT_FOUND"
	codeSessionNotFound = "SESSION_NOT_FOUND"
	codeShellNotAvail   = "SHELL_NOT_AVAILABLE"
	codeSessionLimit    = "SESSION_LIMIT_REACHED"
	codeTerminalError   = "TERMINAL_ERROR"
	codeNotFound        = "NOT_FOUND"
	codeInternal        = "INTERNAL_ERROR"
)

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var b errorBody
	b.Error.Code = code
	b.Error.Message = message
	writeJSON(w, status, b)
}
