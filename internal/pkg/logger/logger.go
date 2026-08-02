package logger

import (
	"log/slog"
	"os"
)

func New(environment string) *slog.Logger {
	level := slog.LevelInfo
	if environment == "local" {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
