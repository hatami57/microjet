package host

import (
	"fmt"

	libhttp "github.com/hatami57/microjet/http"
)

// WithHTTPServer creates the HTTP server and runs the optional setup handlers
// (typically route registration). Errors are deferred to Run/MustRun/Err.
func (a *App) WithHTTPServer(setup ...HandlerFunc) *App {
	if a.err != nil {
		return a
	}
	a.HTTPServer = libhttp.NewServer(libhttp.ServerConfig{
		Host:  a.Config.Server.Host,
		Port:  a.Config.Server.Port,
		Debug: a.Config.App.Debug,
	}, a.Logger)

	for _, s := range setup {
		if s == nil {
			continue
		}
		if err := s(a); err != nil {
			return a.fail(err)
		}
	}
	return a
}

// StartHTTP starts the HTTP server and blocks until it stops. It returns an
// error if the server is not configured or fails to bind/serve.
func (a *App) StartHTTP() error {
	if a.HTTPServer == nil {
		return fmt.Errorf("http server not configured; call WithHTTPServer first")
	}
	a.Logger.Info("Starting HTTP server...", "addr", a.HTTPServer.Addr())
	return a.HTTPServer.Start()
}
