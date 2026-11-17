package main

import (
	"log/slog"
	"strings"
)

// sensitiveKeys are attribute names that must never reach the logs. Handlers
// do not log secrets; this is a safety net in case one ever does.
var sensitiveKeys = []string{"token", "ticket", "code", "password", "secret", "authorization", "key", "input", "output"}

// redact masks the value of sensitive log attributes.
func redact(_ []string, a slog.Attr) slog.Attr {
	k := strings.ToLower(a.Key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}
	return a
}
