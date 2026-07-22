package gormx

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gormLogger "gorm.io/gorm/logger"
)

// NewGormLogger builds a GORM logger that writes through the host's slog logger.
// levelOverride, when non-empty, sets the GORM log level explicitly; otherwise
// the level is derived from what the slog logger has enabled. This lets a
// database's SQL logging be tuned independently of the global log level — e.g.
// keep the app at debug while silencing per-query SQL traces with
// logLevel = "warn" in the [database] section. Valid overrides: debug (or info),
// warn, error, silent.
//
// Built-in drivers pass Config.LogLevel here; custom Driver implementations can
// do the same to honor the per-database override.
func NewGormLogger(sl *slog.Logger, levelOverride string) gormLogger.Interface {
	return gormLogger.New(
		&slogWriter{logger: sl},
		gormLogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  resolveGormLevel(sl, levelOverride),
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}

// resolveGormLevel maps the optional override string to a GORM log level, falling
// back to the level implied by the slog logger when no override is set.
func resolveGormLevel(sl *slog.Logger, override string) gormLogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(override)) {
	case "debug", "trace", "info":
		return gormLogger.Info
	case "warn", "warning":
		return gormLogger.Warn
	case "error", "fatal", "panic":
		return gormLogger.Error
	case "silent", "none", "off":
		return gormLogger.Silent
	}
	ctx := context.Background()
	switch {
	case !sl.Enabled(ctx, slog.LevelInfo) && sl.Enabled(ctx, slog.LevelWarn):
		return gormLogger.Warn
	case !sl.Enabled(ctx, slog.LevelWarn):
		return gormLogger.Error
	default:
		return gormLogger.Info
	}
}

type slogWriter struct{ logger *slog.Logger }

func (w *slogWriter) Printf(format string, args ...any) {
	w.logger.Debug(strings.TrimRight(fmt.Sprintf(format, args...), "\n"))
}
