package host

import (
	libhttp "github.com/hatami57/microjet/http"
)

func (a *App) WithHTTPServer(setup HandlerFunc) *App {
	a.HTTPServer = libhttp.NewServer(libhttp.ServerConfig{
		Host:  a.Config.Server.Host,
		Port:  a.Config.Server.Port,
		Debug: a.Config.App.Debug,
	}, a.Logger)

	if setup != nil {
		a.MustRun(setup)
	}

	return a
}

func (a *App) StartHTTP() {
	a.Logger.Info("Starting HTTP server...", "addr", a.HTTPServer.Addr())
	if err := a.HTTPServer.Start(); err != nil {
		a.Logger.Error("HTTP server failed", "error", err)
	}
}
