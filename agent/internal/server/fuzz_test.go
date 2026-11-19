package server

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// Run one with, for example: go test -run=^$ -fuzz=FuzzParseInputFrame -fuzztime=30s ./internal/server
// Without -fuzz these run only their seed inputs, as ordinary tests.

func FuzzParseInputFrame(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{frameInput})
	f.Add(append([]byte{frameInput, 0, 0, 0, 0, 0, 0, 0, 0}, "ls\n"...))
	f.Add(append([]byte{frameOutput, 0, 0, 0, 0, 0, 0, 0, 1}, "x"...))
	f.Add(append([]byte{frameInput, 0, 0, 0, 0, 0, 0, 0, 1}, "x"...))
	f.Fuzz(func(t *testing.T, data []byte) {
		payload, ok := parseInputFrame(data)
		if !ok {
			if payload != nil {
				t.Fatal("a rejected frame returned a payload")
			}
			return
		}
		if len(data) < frameHeader || data[0] != frameInput || binary.BigEndian.Uint64(data[1:frameHeader]) != 0 {
			t.Fatalf("accepted a malformed frame: % x", data)
		}
		if string(payload) != string(data[frameHeader:]) {
			t.Fatal("payload is not what follows the header")
		}
	})
}

func FuzzParseControl(f *testing.F) {
	f.Add([]byte(`{"type":"session.resume","last_output_sequence":5}`))
	f.Add([]byte(`{"type":"terminal.resize","cols":80,"rows":24}`))
	f.Add([]byte(`{"type":"x","last_output_sequence":18446744073709551615}`))
	f.Add([]byte(`{"cols":-1,"rows":99999999999999999999}`))
	f.Add([]byte(`[1,2,3]`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, data []byte) {
		msg, ok := parseControl(data)
		if !ok && (msg != controlMessage{}) {
			t.Fatal("a rejected message returned data")
		}
	})
}

func FuzzValidDisplayName(f *testing.F) {
	for _, s := range []string{"", "zsh", "build server", "a\x00b", "tab\t", "日本語", strings.Repeat("x", 65)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if !validDisplayName(name) {
			return
		}
		if utf8.RuneCountInString(name) > maxSessionNameLen {
			t.Fatalf("accepted a name of %d characters", utf8.RuneCountInString(name))
		}
		for _, r := range name {
			if unicode.IsControl(r) {
				t.Fatalf("accepted a control character %U", r)
			}
		}
	})
}

// request builds a request from arbitrary parts, skipping what cannot be one.
func request(t *testing.T, method, path, authorization, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "https://agent.test"+path, strings.NewReader(body))
	if err != nil {
		t.Skip()
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return req
}

// Without a valid token nothing but health and pairing is ever served, and
// nothing ever fails with a server error, whatever arrives.
func FuzzUnauthenticatedRequests(f *testing.F) {
	ts := startServer(f)
	h := ts.srv.handler()
	for _, m := range []string{"GET", "POST", "DELETE", "PUT", "OPTIONS"} {
		f.Add(m, "/api/v1/sessions", "", "")
		f.Add(m, "/api/v1/devices", "Bearer abc", "")
		f.Add(m, "/api/v1/pair", "", `{"pairing_code":"x","device_name":"p"}`)
		f.Add(m, "/api/v1/sessions/ses_1/connection-ticket", "Bearer ", `{}`)
	}
	f.Add("GET", "/api/v1/sessions/ses_1/terminal?ticket=abc", "", "")
	f.Add("POST", "/api/v1/pair", "", `{"pairing_code":"`+strings.Repeat("A", 70000)+`"}`)
	f.Fuzz(func(t *testing.T, method, path, authorization, body string) {
		req := request(t, method, path, authorization, body)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code >= 500 {
			t.Fatalf("%s %q -> %d: %s", method, path, rec.Code, rec.Body.String())
		}
		public := strings.HasSuffix(req.URL.Path, "/health") || strings.HasSuffix(req.URL.Path, "/pair")
		if !public && rec.Code >= 200 && rec.Code < 300 {
			t.Fatalf("%s %q served to an unauthenticated client (%d)", method, path, rec.Code)
		}
	})
}

// With a valid token, arbitrary session ids never cause a server error and
// never reach a session that does not exist.
func FuzzSessionIDs(f *testing.F) {
	ts := startServer(f)
	h := ts.srv.handler()
	code, _ := ts.auth.NewPairingCode(time.Hour)
	_, token, err := ts.auth.Pair(code, "fuzz")
	if err != nil {
		f.Fatal(err)
	}
	for _, id := range []string{"ses_1", "", "../../etc/passwd", "ses_\x00", "%", strings.Repeat("a", 5000), "日本"} {
		f.Add("GET", id)
		f.Add("DELETE", id)
		f.Add("POST", id)
	}
	f.Fuzz(func(t *testing.T, method, id string) {
		var path string
		switch method {
		case "GET":
			path = "/api/v1/sessions/" + url.PathEscape(id)
		case "DELETE":
			path = "/api/v1/sessions/" + url.PathEscape(id)
		case "POST":
			path = "/api/v1/sessions/" + url.PathEscape(id) + "/connection-ticket"
		default:
			t.Skip()
		}
		req := request(t, method, path, "Bearer "+token, "")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code >= 500 {
			t.Fatalf("%s %q -> %d: %s", method, path, rec.Code, rec.Body.String())
		}
		if rec.Code >= 200 && rec.Code < 300 {
			t.Fatalf("%s %q reached a session that does not exist (%d)", method, path, rec.Code)
		}
	})
}
