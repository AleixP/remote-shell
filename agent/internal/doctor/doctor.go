// Package doctor checks that an agent installation is set up safely: it is
// only reachable through Tailscale, its TLS certificate is valid and about to
// expire or not, and its files are private to the user.
package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Status is the outcome of one check.
type Status int

const (
	OK Status = iota
	Info
	Warn
	Fail
)

func (s Status) mark() string {
	return [...]string{"✓", "·", "!", "✗"}[s]
}

// Result is one line of the report.
type Result struct {
	Status Status
	Name   string
	Detail string
}

// CertWarnDays is how close to expiry a certificate starts to be reported.
const CertWarnDays = 30

// Options configures a run. Everything that touches the system can be replaced
// so the checks can be tested.
type Options struct {
	CertFile string
	KeyFile  string
	DataDir  string
	Port     int
	Elevated bool

	Now           func() time.Time
	TailscaleAddr func() (netip.Addr, error)
	LocalAddrs    func() ([]netip.Addr, error)
	LookupHost    func(ctx context.Context, host string) ([]string, error)
	Dial          func(ctx context.Context, network, addr string) (net.Conn, error)
	// AgentRunning reports whether the agent's control interface answers.
	AgentRunning func(ctx context.Context) bool
	// RootCAs verifies the served certificate (nil: the system roots, which
	// is what a phone uses).
	RootCAs *x509.CertPool
}

func (o *Options) defaults() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.LookupHost == nil {
		o.LookupHost = net.DefaultResolver.LookupHost
	}
	if o.Dial == nil {
		o.Dial = (&net.Dialer{Timeout: 700 * time.Millisecond}).DialContext
	}
	if o.LocalAddrs == nil {
		o.LocalAddrs = systemAddrs
	}
	if o.AgentRunning == nil {
		o.AgentRunning = func(context.Context) bool { return false }
	}
}

func systemAddrs() ([]netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
				out = append(out, ip.Unmap())
			}
		}
	}
	return out, nil
}

var (
	tailscaleV4 = netip.MustParsePrefix("100.64.0.0/10")
	tailscaleV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

func isTailscale(ip netip.Addr) bool { return tailscaleV4.Contains(ip) || tailscaleV6.Contains(ip) }

// Run performs every check and returns the results in report order.
func Run(ctx context.Context, o Options) []Result {
	o.defaults()
	var rs []Result
	add := func(s Status, name, detail string) { rs = append(rs, Result{s, name, detail}) }

	if o.Elevated {
		add(Fail, "Privileges", "running as root/administrator: the agent refuses to start this way")
	} else {
		add(OK, "Privileges", "running as a normal user")
	}

	var tsAddr netip.Addr
	if o.TailscaleAddr != nil {
		var err error
		if tsAddr, err = o.TailscaleAddr(); err != nil {
			add(Fail, "Tailscale", err.Error())
		} else {
			add(OK, "Tailscale", "this computer is on the tailnet at "+tsAddr.String())
		}
	}

	leaf, ok := checkCertificate(o, &rs)
	var host string
	if ok && len(leaf.DNSNames) > 0 {
		host = leaf.DNSNames[0]
	}
	if host != "" && tsAddr.IsValid() {
		checkHostname(ctx, o, host, tsAddr, &rs)
	}

	checkFiles(o, &rs)

	if tsAddr.IsValid() {
		running := checkAgentReachable(ctx, o, tsAddr, &rs)
		if running {
			checkExposure(ctx, o, tsAddr, &rs)
			if host != "" {
				checkHandshake(ctx, o, tsAddr, host, &rs)
			}
		}
	}
	return rs
}

func checkCertificate(o Options, rs *[]Result) (*x509.Certificate, bool) {
	add := func(s Status, detail string) { *rs = append(*rs, Result{s, "TLS certificate", detail}) }
	pair, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
	if err != nil {
		add(Fail, "cannot load the certificate and key: "+err.Error())
		return nil, false
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		add(Fail, "cannot parse the certificate: "+err.Error())
		return nil, false
	}
	if len(leaf.DNSNames) == 0 {
		add(Fail, "the certificate has no DNS names, so no client can validate it")
		return leaf, false
	}
	left := leaf.NotAfter.Sub(o.Now())
	days := int(left.Hours() / 24)
	switch {
	case left <= 0:
		add(Fail, fmt.Sprintf("%s expired on %s; renew it with `tailscale cert`", leaf.DNSNames[0], leaf.NotAfter.Format("2006-01-02")))
	case days < CertWarnDays:
		add(Warn, fmt.Sprintf("%s expires in %d days (%s); renew it with `tailscale cert`", leaf.DNSNames[0], days, leaf.NotAfter.Format("2006-01-02")))
	default:
		add(OK, fmt.Sprintf("%s is valid for %d more days (until %s)", leaf.DNSNames[0], days, leaf.NotAfter.Format("2006-01-02")))
	}
	return leaf, left > 0
}

func checkHostname(ctx context.Context, o Options, host string, ts netip.Addr, rs *[]Result) {
	name := "MagicDNS name"
	addrs, err := o.LookupHost(ctx, host)
	if err != nil {
		*rs = append(*rs, Result{Warn, name, fmt.Sprintf("%s does not resolve here (%v); is MagicDNS enabled?", host, err)})
		return
	}
	for _, a := range addrs {
		if ip, err := netip.ParseAddr(a); err == nil && ip.Unmap() == ts {
			*rs = append(*rs, Result{OK, name, host + " resolves to this computer's Tailscale address"})
			return
		}
	}
	*rs = append(*rs, Result{Warn, name, fmt.Sprintf("%s resolves to %s, not to %s", host, strings.Join(addrs, ", "), ts)})
}

func checkFiles(o Options, rs *[]Result) {
	if runtime.GOOS == "windows" {
		*rs = append(*rs, Result{Info, "File permissions", "not checked on Windows (the data directory lives in the user profile)"})
		return
	}
	var problems []string
	for label, path := range map[string]string{"TLS private key": o.KeyFile, "database": filepath.Join(o.DataDir, "agent.db")} {
		fi, err := os.Stat(path)
		if err != nil {
			continue // not created yet, or not readable: nothing to judge
		}
		if fi.Mode().Perm()&0o077 != 0 {
			problems = append(problems, fmt.Sprintf("%s is accessible to other users (mode %o)", label, fi.Mode().Perm()))
		}
	}
	if fi, err := os.Stat(o.DataDir); err == nil && fi.Mode().Perm()&0o077 != 0 {
		problems = append(problems, fmt.Sprintf("data directory is accessible to other users (mode %o)", fi.Mode().Perm()))
	}
	if len(problems) > 0 {
		*rs = append(*rs, Result{Warn, "File permissions", strings.Join(problems, "; ")})
		return
	}
	*rs = append(*rs, Result{OK, "File permissions", "the key, database and data directory are private to this user"})
}

func addr(ip netip.Addr, port int) string { return net.JoinHostPort(ip.String(), strconv.Itoa(port)) }

func canConnect(ctx context.Context, o Options, a string) bool {
	c, err := o.Dial(ctx, "tcp", a)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func checkAgentReachable(ctx context.Context, o Options, ts netip.Addr, rs *[]Result) bool {
	if canConnect(ctx, o, addr(ts, o.Port)) {
		*rs = append(*rs, Result{OK, "Agent", "listening on " + addr(ts, o.Port)})
		return true
	}
	if o.AgentRunning(ctx) {
		*rs = append(*rs, Result{Warn, "Agent", fmt.Sprintf("running, but nothing answers on %s (different port or address?)", addr(ts, o.Port))})
		return false
	}
	*rs = append(*rs, Result{Info, "Agent", "not running: start it to check exposure and the TLS handshake"})
	return false
}

// checkExposure tries to reach the agent on every address of this computer
// other than its Tailscale one. Any answer means it is exposed.
func checkExposure(ctx context.Context, o Options, ts netip.Addr, rs *[]Result) {
	name := "Exposure"
	local, err := o.LocalAddrs()
	if err != nil {
		*rs = append(*rs, Result{Warn, name, "cannot list this computer's addresses: " + err.Error()})
		return
	}
	var exposed []string
	checked := 0
	for _, ip := range local {
		if ip == ts || isTailscale(ip) || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			continue
		}
		checked++
		if canConnect(ctx, o, addr(ip, o.Port)) {
			exposed = append(exposed, addr(ip, o.Port))
		}
	}
	if len(exposed) > 0 {
		*rs = append(*rs, Result{Fail, name, "the agent answers outside Tailscale on " + strings.Join(exposed, ", ")})
		return
	}
	*rs = append(*rs, Result{OK, name, fmt.Sprintf("refuses connections on the other %d address(es) of this computer", checked)})
}

// checkHandshake connects like a phone does: to the Tailscale address, with the
// certificate's name, validating the chain.
func checkHandshake(ctx context.Context, o Options, ts netip.Addr, host string, rs *[]Result) {
	name := "TLS handshake"
	conn, err := o.Dial(ctx, "tcp", addr(ts, o.Port))
	if err != nil {
		*rs = append(*rs, Result{Warn, name, "cannot connect: " + err.Error()})
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(o.Now().Add(5 * time.Second))
	c := tls.Client(conn, &tls.Config{ServerName: host, RootCAs: o.RootCAs, MinVersion: tls.VersionTLS12})
	if err := c.HandshakeContext(ctx); err != nil {
		*rs = append(*rs, Result{Fail, name, fmt.Sprintf("a client validating %s would reject the agent: %v", host, err)})
		return
	}
	peer := c.ConnectionState().PeerCertificates[0]
	*rs = append(*rs, Result{OK, name, fmt.Sprintf("a client validating %s accepts it (certificate valid until %s)", host, peer.NotAfter.Format("2006-01-02"))})
}

// Print writes the report and returns the process exit code: 1 if any check
// failed, 0 otherwise.
func Print(w io.Writer, rs []Result) int {
	code := 0
	for _, r := range rs {
		fmt.Fprintf(w, "%s %-17s %s\n", r.Status.mark(), r.Name, r.Detail)
		if r.Status == Fail {
			code = 1
		}
	}
	return code
}
