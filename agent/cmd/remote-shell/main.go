package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AleixP/remote-shell/agent/internal/config"
	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Args[1:], os.Stderr)
	if err != nil {
		return 2
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sessions := terminal.NewManager()
	log.Info("agent started")

	<-ctx.Done()
	log.Info("shutting down")
	sessions.CloseAll()
	log.Info("agent stopped")
	return 0
}
