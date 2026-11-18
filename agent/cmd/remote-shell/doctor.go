package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/config"
	"github.com/AleixP/remote-shell/agent/internal/control"
	"github.com/AleixP/remote-shell/agent/internal/doctor"
	"github.com/AleixP/remote-shell/agent/internal/server"
)

// runDoctor checks the installation: Tailscale, certificate, file permissions
// and, if the agent is running, that it is not exposed outside Tailscale.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	certFile := fs.String("tls-cert", "", "path to the TLS certificate")
	keyFile := fs.String("tls-key", "", "path to the TLS private key")
	port := fs.Int("port", 8443, "HTTPS port the agent uses")
	dataDir := fs.String("data-dir", "", "agent data directory (default: the user's config directory)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *certFile == "" || *keyFile == "" {
		fmt.Fprintln(stderr, "usage: remote-shell doctor -tls-cert <file> -tls-key <file> [-port N] [-data-dir dir]")
		return 2
	}
	dir := *dataDir
	if dir == "" {
		var err error
		if dir, err = config.DefaultDataDir(); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results := doctor.Run(ctx, doctor.Options{
		CertFile:      *certFile,
		KeyFile:       *keyFile,
		DataDir:       dir,
		Port:          *port,
		Elevated:      isElevated(),
		TailscaleAddr: server.TailscaleAddr,
		AgentRunning: func(ctx context.Context) bool {
			_, err := control.NewClient(filepath.Join(dir, control.SocketName)).ListDevices(ctx)
			return err == nil
		},
	})
	return doctor.Print(stdout, results)
}
