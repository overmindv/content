package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	app "github.com/overmindv/content/internal/app/content"
	"github.com/overmindv/parker"
)

// main запускает content на каркасе parker.
func main() {
	os.Exit(mainArgs(os.Args[1:]))
}

func mainArgs(args []string) int {
	if len(args) > 0 && args[0] == "backfill-material-times" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		return runMaterialTimeBackfill(ctx, args[1:], os.Getenv("DATABASE_URL"), os.Getenv("KAFKA_CONTENT_MATERIAL_TOPIC"), os.Stdout, os.Stderr)
	}

	return parker.MainArgs(run, args, parker.WithAppName("content"))
}

// run регистрирует бизнес-логику content на каркас parker.
func run(application *parker.App) error {
	return app.Build(application)
}
