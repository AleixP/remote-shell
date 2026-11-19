package auth

import (
	"testing"
	"unicode"
	"unicode/utf8"
)

// Run one with, for example: go test -run=^$ -fuzz=FuzzAuthenticate -fuzztime=30s ./internal/auth

func FuzzAuthenticate(f *testing.F) {
	for _, s := range []string{"", "Bearer", "abc", "rs_" + "A", "\x00", "日本語", string(make([]byte, 10000))} {
		f.Add(s)
	}
	svc, _ := newService(f)
	f.Fuzz(func(t *testing.T, token string) {
		if _, err := svc.Authenticate(token); err == nil {
			t.Fatalf("accepted an unknown token %q", token)
		}
	})
}

func FuzzPairRejectsUnknownCodes(f *testing.F) {
	for _, s := range []string{"", "AAAA", "00000000000000000000000000", "日本語"} {
		f.Add(s, "phone")
	}
	svc, _ := newService(f)
	f.Fuzz(func(t *testing.T, code, name string) {
		if _, token, err := svc.Pair(code, name); err == nil {
			t.Fatalf("paired with an unissued code %q (token %d bytes)", code, len(token))
		}
	})
}

func FuzzValidName(f *testing.F) {
	for _, s := range []string{"", "iPhone", "a\x00", "x‮y", string(make([]byte, 200))} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if !validName(name) {
			return
		}
		if n := utf8.RuneCountInString(name); n < 1 || n > maxDeviceNameLen {
			t.Fatalf("accepted a name of %d characters", n)
		}
		for _, r := range name {
			if unicode.IsControl(r) {
				t.Fatalf("accepted a control character %U", r)
			}
		}
	})
}
