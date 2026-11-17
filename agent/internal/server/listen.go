// Package server implements the agent's HTTPS API.
package server

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

var (
	tailscaleV4 = netip.MustParsePrefix("100.64.0.0/10")
	tailscaleV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// ErrNoTailscale is returned when no Tailscale address can be found.
var ErrNoTailscale = errors.New("no Tailscale address found; is Tailscale running?")

func isTailscale(ip netip.Addr) bool {
	return tailscaleV4.Contains(ip) || tailscaleV6.Contains(ip)
}

// allowedListenIP reports whether the agent may listen on ip: only Tailscale
// addresses, or loopback (local development and tests). Wildcard and public
// addresses are never allowed.
func allowedListenIP(ip netip.Addr) bool {
	return isTailscale(ip) || ip.IsLoopback()
}

// TailscaleAddr returns the machine's Tailscale address, preferring IPv4.
func TailscaleAddr() (netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return netip.Addr{}, err
	}
	var v6 netip.Addr
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(ipn.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		switch {
		case ip.Is4() && isTailscale(ip):
			return ip, nil
		case ip.Is6() && isTailscale(ip):
			v6 = ip
		}
	}
	if v6.IsValid() {
		return v6, nil
	}
	return netip.Addr{}, ErrNoTailscale
}

// ResolveListenAddr returns the host:port the agent must listen on. An empty
// listen address means "the Tailscale address of this machine".
func ResolveListenAddr(listen string, port int) (string, error) {
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid port %d", port)
	}
	var ip netip.Addr
	if listen == "" {
		var err error
		if ip, err = TailscaleAddr(); err != nil {
			return "", err
		}
	} else {
		var err error
		if ip, err = netip.ParseAddr(listen); err != nil {
			return "", fmt.Errorf("invalid listen address %q: must be an IP address", listen)
		}
		ip = ip.Unmap()
		if !allowedListenIP(ip) {
			return "", fmt.Errorf("listen address %s is not a Tailscale or loopback address", ip)
		}
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(port)), nil
}
