// Command logging demonstrates MicroJet's structured logging (core/logx). The
// framework logs through the standard library's log/slog; logx.NewLogger builds
// a configured *slog.Logger from a LogConfig (level + text/json format, with
// optional per-output overrides). The host wires this up for you from the [log]
// config section — this example constructs it directly so you can see the effect
// of each setting.
//
// Run it with:
//
//	go run .
package main

import (
	"github.com/hatami57/microjet/core/logx"
)

func main() {
	// 1. JSON format at info level — the production default. debug is below the
	// threshold, so it is dropped; structured key-value attrs become JSON fields.
	jsonLog := logx.NewLogger(&logx.LogConfig{Level: "info", Format: "json"}, false)
	jsonLog.Debug("this debug line is filtered out (level=info)")
	jsonLog.Info("user signed in", "userID", 42, "tenant", "acme")
	jsonLog.Warn("cache miss", "key", "user:42")
	jsonLog.Error("payment failed", "orderID", 1001, "reason", "card_declined")

	// 2. Text format — easier to read during local development.
	textLog := logx.NewLogger(&logx.LogConfig{Level: "debug", Format: "text"}, false)
	textLog.Info("--- switching to text format, level=debug ---")
	textLog.Debug("now debug lines show", "step", "init")

	// 3. forceDebug lowers the effective level to debug regardless of config —
	// the host passes app.debug here so turning on debug mode makes logs verbose
	// without editing the [log] section.
	forced := logx.NewLogger(&logx.LogConfig{Level: "warn", Format: "text"}, true)
	forced.Debug("visible because forceDebug=true overrides level=warn")

	// 4. A child logger with bound attributes — every line it emits carries them,
	// which is how the HTTP middleware attaches a request_id to all request logs.
	reqLog := jsonLog.With("request_id", "req-abc123", "route", "/orders")
	reqLog.Info("handling request")
	reqLog.Info("request complete", "status", 200)
}
