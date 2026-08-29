package main

import (
	"os"

	app "github.com/overmindv/content/internal/app/content"
	"github.com/overmindv/parker"
)

// main запускает content на каркасе parker.
func main() {
	os.Exit(parker.Main(run, parker.WithAppName("content")))
}

// run регистрирует бизнес-логику content на каркас parker.
func run(application *parker.App) error {
	return app.Build(application)
}
