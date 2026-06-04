package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// NewLogger constructs a *slog.Logger from LogConfig.
// Console output is always enabled unless config.Console.Enabled=false.
// A second file output is added when config.File.Enabled=true and config.File.Path is set.
// Each output has its own level and format, falling back to config.Level and config.Format.
func NewLogger(config *LogConfig) *slog.Logger {
	defaultLevel := "info"
	defaultFormat := "text"
	if config != nil {
		if config.Level != "" {
			defaultLevel = config.Level
		}
		if config.Format != "" {
			defaultFormat = config.Format
		}
	}

	var handlers []slog.Handler

	consoleEnabled := true
	consoleLevel := defaultLevel
	consoleFormat := defaultFormat
	if config != nil && config.Console != nil {
		consoleEnabled = config.Console.Enabled
		if config.Console.Level != "" {
			consoleLevel = config.Console.Level
		}
		if config.Console.Format != "" {
			consoleFormat = config.Console.Format
		}
	}
	if consoleEnabled {
		handlers = append(handlers, newSlogHandler(os.Stdout, consoleLevel, consoleFormat))
	}

	if config != nil && config.File != nil && config.File.Enabled && config.File.Path != "" {
		fileLevel := defaultLevel
		fileFormat := defaultFormat
		if config.File.Level != "" {
			fileLevel = config.File.Level
		}
		if config.File.Format != "" {
			fileFormat = config.File.Format
		}
		if f, err := openLogFile(config.File.Path); err == nil {
			handlers = append(handlers, newSlogHandler(f, fileLevel, fileFormat))
		}
	}

	if len(handlers) == 0 {
		handlers = append(handlers, newSlogHandler(os.Stdout, defaultLevel, defaultFormat))
	}
	if len(handlers) == 1 {
		return slog.New(handlers[0])
	}
	return slog.New(&multiHandler{handlers: handlers})
}

func newSlogHandler(w io.Writer, levelStr, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: parseSlogLevel(levelStr)}
	if strings.ToLower(format) == "json" {
		return slog.NewJSONHandler(w, opts)
	}
	return &plainTextHandler{w: w, opts: opts}
}

func parseSlogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug", "trace":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "fatal", "panic":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// plainTextHandler emits lines in the form:
//
//	2006-01-02T15:04:05.000Z [INF] message key=value …
type plainTextHandler struct {
	w    io.Writer
	opts *slog.HandlerOptions
	mu   sync.Mutex
	pre  []slog.Attr
	grp  string
}

func (h *plainTextHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.opts.Level.Level()
}

func (h *plainTextHandler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s [%s] %s", r.Time.UTC().Format("2006-01-02T15:04:05.000")+"Z", levelLabel(r.Level), r.Message)
	writeAttrs(&buf, h.grp, h.pre)
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&buf, h.grp, a)
		return true
	})
	buf.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf.Bytes())
	return err
}

func (h *plainTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := &plainTextHandler{w: h.w, opts: h.opts, grp: h.grp}
	n.pre = append(n.pre, h.pre...)
	n.pre = append(n.pre, attrs...)
	return n
}

func (h *plainTextHandler) WithGroup(name string) slog.Handler {
	prefix := name
	if h.grp != "" {
		prefix = h.grp + "." + name
	}
	return &plainTextHandler{w: h.w, opts: h.opts, pre: h.pre, grp: prefix}
}

func levelLabel(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERR"
	case l >= slog.LevelWarn:
		return "WRN"
	case l >= slog.LevelInfo:
		return "INF"
	default:
		return "DBG"
	}
}

func writeAttrs(buf *bytes.Buffer, grp string, attrs []slog.Attr) {
	for _, a := range attrs {
		writeAttr(buf, grp, a)
	}
}

func writeAttr(buf *bytes.Buffer, grp string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		sub := grp
		if a.Key != "" {
			if sub != "" {
				sub += "."
			}
			sub += a.Key
		}
		for _, ga := range a.Value.Group() {
			writeAttr(buf, sub, ga)
		}
		return
	}
	key := a.Key
	if grp != "" {
		key = grp + "." + key
	}
	val := fmtValue(a.Value)
	if strings.ContainsAny(val, " \t\n\"=") {
		fmt.Fprintf(buf, " %s=%q", key, val)
	} else {
		fmt.Fprintf(buf, " %s=%s", key, val)
	}
}

func fmtValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	case slog.KindDuration:
		return v.Duration().String()
	default:
		return fmt.Sprintf("%v", v.Any())
	}
}

func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}

type multiHandler struct {
	handlers []slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, hh := range h.handlers {
		if hh.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, hh := range h.handlers {
		if hh.Enabled(ctx, r.Level) {
			if err := hh.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, hh := range h.handlers {
		handlers[i] = hh.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, hh := range h.handlers {
		handlers[i] = hh.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}
