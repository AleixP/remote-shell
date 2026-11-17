package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Client talks to a running agent through its control socket.
type Client struct {
	http *http.Client
}

// NewClient returns a client for the control socket at socketPath.
func NewClient(socketPath string) *Client {
	return &Client{http: &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
	}}
}

// ErrAgentNotRunning is returned when the control socket cannot be reached.
var ErrAgentNotRunning = errors.New("cannot reach the agent: is it running?")

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://agent"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return ErrAgentNotRunning
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var body struct{ Error string }
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Error == "" {
			body.Error = resp.Status
		}
		return errors.New(body.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// NewPairingCode asks the agent for a new pairing QR payload.
func (c *Client) NewPairingCode(ctx context.Context) (PairingInfo, error) {
	var info PairingInfo
	err := c.do(ctx, http.MethodPost, "/pairing-codes", &info)
	return info, err
}

// ListDevices returns the authorized devices.
func (c *Client) ListDevices(ctx context.Context) ([]DeviceInfo, error) {
	var body struct{ Devices []DeviceInfo }
	if err := c.do(ctx, http.MethodGet, "/devices", &body); err != nil {
		return nil, err
	}
	return body.Devices, nil
}

// RevokeDevice revokes a device by ID.
func (c *Client) RevokeDevice(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("device id is required")
	}
	return c.do(ctx, http.MethodDelete, "/devices/"+url.PathEscape(id), nil)
}
