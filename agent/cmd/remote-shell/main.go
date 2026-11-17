package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/AleixP/remote-shell/agent/internal/auth"
	"github.com/AleixP/remote-shell/agent/internal/config"
	"github.com/AleixP/remote-shell/agent/internal/control"
	"github.com/AleixP/remote-shell/agent/internal/server"
	"github.com/AleixP/remote-shell/agent/internal/storage"
	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches the CLI subcommands; anything else starts the agent.
func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "pair":
			return runPair(args[1:], os.Stdout, os.Stderr)
		case "devices":
			return runDevices(args[1:], os.Stdout, os.Stderr)
		}
	}
	return runAgent(args)
}

func runAgent(args []string) int {
	cfg, err := config.Load(args, os.Stderr)
	if err != nil {
		return 2
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := storage.Open(filepath.Join(cfg.DataDir, "agent.db"))
	if err != nil {
		log.Error("cannot open database", "error", err)
		return 1
	}
	defer db.Close()
	authSvc := auth.New(db)

	shells := terminal.DetectShells()
	if cfg.DefaultShell != "" {
		if err := shells.SetDefault(cfg.DefaultShell); err != nil {
			log.Error("invalid -shell", "error", err)
			return 1
		}
	}
	if shells.Default() == "" {
		log.Error("no usable shell found; use -shell")
		return 1
	}
	sessions := terminal.NewManager(cfg.MaxSessions)
	defer sessions.CloseAll()

	srv, err := server.New(server.Options{
		Listen:      cfg.Listen,
		Port:        cfg.Port,
		TLSCertFile: cfg.TLSCertFile,
		TLSKeyFile:  cfg.TLSKeyFile,
		Auth:        authSvc,
		Sessions:    sessions,
		Shells:      shells,
		Logger:      log,
	})
	if err != nil {
		log.Error("cannot start agent", "error", err)
		return 1
	}

	computerName, _ := os.Hostname()
	ctl, err := control.NewServer(control.Options{
		SocketPath:   filepath.Join(cfg.DataDir, control.SocketName),
		Auth:         authSvc,
		Host:         srv.Host,
		ComputerName: computerName,
		PairingTTL:   cfg.PairingTTL,
		Logger:       log,
	})
	if err != nil {
		log.Error("cannot start control interface", "error", err)
		return 1
	}

	log.Info("agent started", "shells", shells.Names(), "default_shell", shells.Default())

	// Run both servers; if either fails, stop the other.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var failed bool
	var mu sync.Mutex
	for name, run := range map[string]func(context.Context) error{"https server": srv.Run, "control interface": ctl.Run} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := run(ctx); err != nil {
				log.Error(name+" failed", "error", err)
				mu.Lock()
				failed = true
				mu.Unlock()
				cancel()
			}
		}()
	}
	wg.Wait()
	log.Info("agent stopped")
	if failed {
		return 1
	}
	return 0
}
