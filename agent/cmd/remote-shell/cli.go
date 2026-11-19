package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/AleixP/remote-shell/agent/internal/config"
	"github.com/AleixP/remote-shell/agent/internal/control"
)

// cliClient parses the shared -data-dir flag, plus any flags a command adds
// through extra, and returns a control client.
func cliClient(name string, args []string, stderr io.Writer, extra ...func(*flag.FlagSet)) (*control.Client, *flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data-dir", "", "agent data directory (default: the user's config directory)")
	for _, add := range extra {
		add(fs)
	}
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	dir := *dataDir
	if dir == "" {
		var err error
		if dir, err = config.DefaultDataDir(); err != nil {
			return nil, nil, err
		}
	}
	return control.NewClient(filepath.Join(dir, control.SocketName)), fs, nil
}

func runPair(args []string, stdout, stderr io.Writer) int {
	var showLink bool
	client, _, err := cliClient("pair", args, stderr, func(fs *flag.FlagSet) {
		fs.BoolVar(&showLink, "link", false, "also print the pairing link, to paste into the app when its camera cannot be used")
	})
	if err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := client.NewPairingCode(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	qr, err := qrcode.New(info.URL, qrcode.Medium)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Scan this QR code with the Remote Shell app:")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, qr.ToSmallString(false))
	fmt.Fprintf(stdout, "Valid until %s and usable only once.\n", info.ExpiresAt.Local().Format("15:04:05"))
	// The link holds the same secret as the QR code. It is only printed when
	// asked for, so it does not end up on screen or in the scrollback by default.
	if showLink {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Or, in the app, choose \"Or paste the pairing link\" and paste this. Anyone who sees it can pair until it expires:")
		fmt.Fprintln(stdout, info.URL)
	}
	return 0
}

func runDevices(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "list" && args[0] != "revoke") {
		fmt.Fprintln(stderr, "usage: remote-shell devices list|revoke <id> [-data-dir dir]")
		return 2
	}
	sub, rest := args[0], args[1:]

	var id string
	if sub == "revoke" {
		if len(rest) == 0 || len(rest[0]) == 0 || rest[0][0] == '-' {
			fmt.Fprintln(stderr, "usage: remote-shell devices revoke <id> [-data-dir dir]")
			return 2
		}
		id, rest = rest[0], rest[1:]
	}
	client, _, err := cliClient("devices "+sub, rest, stderr)
	if err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if sub == "revoke" {
		if err := client.RevokeDevice(ctx, id); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Device %s revoked.\n", id)
		return 0
	}

	devices, err := client.ListDevices(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(devices) == 0 {
		fmt.Fprintln(stdout, "No authorized devices.")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tCREATED\tLAST USED")
	for _, d := range devices {
		last := "never"
		if d.LastUsedAt != nil {
			last = d.LastUsedAt.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.ID, d.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"), last)
	}
	return exitFlush(tw)
}

func exitFlush(tw *tabwriter.Writer) int {
	if err := tw.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
