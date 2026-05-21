package core

import (
	"log/slog"
	"os"
	"strings"
)

func NewLogger(config *LogConfig) *slog.Logger {
	level := slog.LevelInfo
	if config != nil && config.Level != "" {
		switch strings.ToLower(config.Level) {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if config != nil && config.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
