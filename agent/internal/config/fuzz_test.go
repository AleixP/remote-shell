package config

import (
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzValidComputerName(f *testing.F) {
	for _, s := range []string{"", "My Mac", "a\x00", string(make([]byte, 200)), "日本語"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if !validComputerName(name) {
			return
		}
		if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
			t.Fatalf("accepted a name of %d characters", n)
		}
		for _, r := range name {
			if unicode.IsControl(r) {
				t.Fatalf("accepted a control character %U", r)
			}
		}
	})
}
