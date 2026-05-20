package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/skorfmann/jot/internal/auth"
	"github.com/skorfmann/jot/internal/server"
	"github.com/skorfmann/jot/internal/storage"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", os.Getenv("JOT_CONFIG"), "Path to jot.yaml. Defaults to $JOT_CONFIG or ./jot.yaml.")
	flag.Parse()
	if configPath == "" {
		if _, err := os.Stat("jot.yaml"); err == nil {
			configPath = "jot.yaml"
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()
	cfg, err := server.LoadConfig(configPath)
	if err != nil {
		logger.Error("config error", "event_type", "error", "error", err)
		os.Exit(1)
	}
	store, err := storage.New(ctx, cfg.Storage)
	if err != nil {
		logger.Error("storage error", "event_type", "error", "error", err)
		os.Exit(1)
	}
	authenticator, err := auth.New(ctx, cfg.Auth, cfg.Server.BaseURL)
	if err != nil {
		logger.Error("auth error", "event_type", "error", "error", err)
		os.Exit(1)
	}
	if cfg.Auth.Mode == "dev" {
		logger.Warn("auth dev mode enabled; never use this in production", "event_type", "auth")
	}
	s := server.New(cfg, store, authenticator, logger)
	s.StartBackground(ctx)
	logger.Info("jot-server listening", "event_type", "startup", "addr", cfg.Server.Addr)
	if err := http.ListenAndServe(cfg.Server.Addr, s.Handler()); err != nil {
		logger.Error("server stopped", "event_type", "error", "error", err)
		os.Exit(1)
	}
}
