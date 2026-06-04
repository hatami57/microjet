package host

import (
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

