package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactMasksSensitiveAttributes(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: redact}))
	log.Info("event",
		"access_token", "tok-secret", "ticket", "tk-secret", "pairing_code", "code-secret",
		"Authorization", "Bearer x-secret", "terminal_output", "out-secret",
		"device_id", "dev_visible", "session_id", "ses_visible")
	out := buf.String()
	for _, secret := range []string{"tok-secret", "tk-secret", "code-secret", "x-secret", "out-secret"} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaks %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "dev_visible") || !strings.Contains(out, "ses_visible") {
		t.Errorf("non-sensitive attributes were masked: %s", out)
	}
}
