package logx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestWithMinLevel(t *testing.T) {
	// A base logger that itself allows everything from debug up.
	newBase := func(buf *bytes.Buffer) *slog.Logger {
		return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	t.Run("filters records below the floor", func(t *testing.T) {
		var buf bytes.Buffer
		log := WithMinLevel(newBase(&buf), "warn")

		log.Info("dropped")
		log.Warn("kept")
		log.Error("kept")

		out := buf.String()
		if strings.Contains(out, "dropped") {
			t.Fatalf("info line should be filtered, got: %q", out)
		}
		if strings.Count(out, "kept") != 2 {
			t.Fatalf("warn and error should pass, got: %q", out)
		}
	})

	t.Run("empty or unknown level returns logger unchanged", func(t *testing.T) {
		for _, level := range []string{"", "bogus"} {
			base := newBase(&bytes.Buffer{})
			if got := WithMinLevel(base, level); got != base {
				t.Fatalf("level %q: expected same logger instance", level)
			}
		}
	})

	t.Run("nil logger is returned as-is", func(t *testing.T) {
		if got := WithMinLevel(nil, "warn"); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("Enabled reflects the raised floor", func(t *testing.T) {
		log := WithMinLevel(newBase(&bytes.Buffer{}), "error")
		ctx := context.Background()
		if log.Enabled(ctx, slog.LevelWarn) {
			t.Fatal("warn should be disabled when floor is error")
		}
		if !log.Enabled(ctx, slog.LevelError) {
			t.Fatal("error should be enabled when floor is error")
		}
	})

	t.Run("preserves attrs added after wrapping", func(t *testing.T) {
		var buf bytes.Buffer
		log := WithMinLevel(newBase(&buf), "warn").With("component", "http")
		log.Warn("hi")
		if !strings.Contains(buf.String(), "component=http") {
			t.Fatalf("expected attr to survive filtering, got: %q", buf.String())
		}
	})
}
