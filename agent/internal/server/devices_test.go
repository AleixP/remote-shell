package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// call sends a request and decodes the JSON response into out (if not nil).
func (ts *testServer) call(t *testing.T, method, path, token, body string, out any) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.url+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := insecureClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return resp
}

type pairResp struct {
	DeviceID    string `json:"device_id"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

func (ts *testServer) pair(t *testing.T, name string) pairResp {
	t.Helper()
	code, _ := ts.auth.NewPairingCode(time.Minute)
	var pr pairResp
	resp := ts.call(t, "POST", "/api/v1/pair", "", `{"pairing_code":"`+code+`","device_name":"`+name+`"}`, &pr)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("pair status = %d", resp.StatusCode)
	}
	return pr
}

func errCode(t *testing.T, ts *testServer, method, path, token, body string, wantStatus int) string {
	t.Helper()
	var eb errorBody
	resp := ts.call(t, method, path, token, body, &eb)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d (%+v)", method, path, resp.StatusCode, wantStatus, eb)
	}
	return eb.Error.Code
}

func TestPairFlow(t *testing.T) {
	ts := startServer(t)
	pr := ts.pair(t, "iPhone")
	if pr.TokenType != "Bearer" || pr.AccessToken == "" || !strings.HasPrefix(pr.DeviceID, "dev_") {
		t.Fatalf("pair response = %+v", pr)
	}
	var list struct {
		Devices []deviceJSON `json:"devices"`
	}
	resp := ts.call(t, "GET", "/api/v1/devices", pr.AccessToken, "", &list)
	if resp.StatusCode != 200 || len(list.Devices) != 1 || !list.Devices[0].Current || list.Devices[0].Name != "iPhone" {
		t.Fatalf("list = %d %+v", resp.StatusCode, list)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Error("responses must not be cacheable")
	}
}

func TestPairErrors(t *testing.T) {
	ts := startServer(t)
	code, _ := ts.auth.NewPairingCode(time.Minute)

	if c := errCode(t, ts, "POST", "/api/v1/pair", "", `not json`, 400); c != codeInvalidRequest {
		t.Errorf("malformed body code = %s", c)
	}
	if c := errCode(t, ts, "POST", "/api/v1/pair", "", `{"device_name":"x"}`, 400); c != codeInvalidRequest {
		t.Errorf("missing code = %s", c)
	}
	if c := errCode(t, ts, "POST", "/api/v1/pair", "", `{"pairing_code":"`+code+`","device_name":""}`, 422); c != codeInvalidRequest {
		t.Errorf("empty name = %s", c)
	}
	if c := errCode(t, ts, "POST", "/api/v1/pair", "", `{"pairing_code":"WRONG","device_name":"x"}`, 401); c != codePairingExpired {
		t.Errorf("wrong code = %s", c)
	}
	// The valid code still works after the failed attempts above.
	ts.call(t, "POST", "/api/v1/pair", "", `{"pairing_code":"`+code+`","device_name":"ok"}`, nil)
	if c := errCode(t, ts, "POST", "/api/v1/pair", "", `{"pairing_code":"`+code+`","device_name":"again"}`, 401); c != codePairingExpired {
		t.Errorf("reused code = %s", c)
	}
}

func TestProtectedEndpointsRequireAuth(t *testing.T) {
	ts := startServer(t)
	for _, tc := range [][2]string{{"GET", "/api/v1/devices"}, {"DELETE", "/api/v1/devices/dev_x"}} {
		if c := errCode(t, ts, tc[0], tc[1], "", "", 401); c != codeUnauthorized {
			t.Errorf("%v without token = %s", tc, c)
		}
		if c := errCode(t, ts, tc[0], tc[1], "wrong-token", "", 401); c != codeUnauthorized {
			t.Errorf("%v with bad token = %s", tc, c)
		}
	}
	// A non-Bearer scheme is not accepted.
	pr := ts.pair(t, "p")
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/devices", nil)
	req.Header.Set("Authorization", "Basic "+pr.AccessToken)
	resp, err := insecureClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("Basic scheme status = %d", resp.StatusCode)
	}
}

func TestRevokeDevice(t *testing.T) {
	ts := startServer(t)
	a := ts.pair(t, "A")
	b := ts.pair(t, "B")

	var ok map[string]bool
	if resp := ts.call(t, "DELETE", "/api/v1/devices/"+b.DeviceID, a.AccessToken, "", &ok); resp.StatusCode != 200 || !ok["success"] {
		t.Fatalf("revoke B = %d %v", resp.StatusCode, ok)
	}
	if c := errCode(t, ts, "GET", "/api/v1/devices", b.AccessToken, "", 401); c != codeDeviceRevoked {
		t.Errorf("revoked device code = %s", c)
	}
	if c := errCode(t, ts, "DELETE", "/api/v1/devices/"+b.DeviceID, a.AccessToken, "", 404); c != codeDeviceNotFound {
		t.Errorf("revoke twice code = %s", c)
	}
	// A is unaffected and can revoke itself.
	if resp := ts.call(t, "GET", "/api/v1/devices", a.AccessToken, "", nil); resp.StatusCode != 200 {
		t.Fatalf("A affected by B's revocation: %d", resp.StatusCode)
	}
	if resp := ts.call(t, "DELETE", "/api/v1/devices/"+a.DeviceID, a.AccessToken, "", nil); resp.StatusCode != 200 {
		t.Fatalf("self revoke = %d", resp.StatusCode)
	}
	if c := errCode(t, ts, "GET", "/api/v1/devices", a.AccessToken, "", 401); c != codeDeviceRevoked {
		t.Errorf("self-revoked device code = %s", c)
	}
}
