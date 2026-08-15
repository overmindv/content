package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	app "github.com/overmindv/content/internal/app/content"
	"github.com/overmindv/content/internal/config"
	"github.com/overmindv/content/internal/pkg/logger"
)

func main() {
	cfg := config.Load()
	log := logger.New(cfg.Service.Environment)

	runtime, err := app.NewRuntime(cfg, log)
	if err != nil {
		log.Error("failed to initialize runtime", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runtime.Run(ctx); err != nil {
		log.Error("runtime stopped with error", "error", err)
		os.Exit(1)
	}
}
