package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/gopher-launch/curu/internal/config"
	"github.com/gopher-launch/curu/internal/web"
)

type App struct {
	config config.Config
	logger *slog.Logger
	ready  atomic.Bool
	server *http.Server
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	application := &App{config: cfg, logger: logger}
	handler, err := web.NewHandler(logger, &application.ready)
	if err != nil {
		return nil, fmt.Errorf("create HTTP handler: %w", err)
	}
	application.server = &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           handler,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
	return application, nil
}

func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.config.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.config.ListenAddress, err)
	}

	serveResult := make(chan error, 1)
	a.ready.Store(true)
	a.logger.Info("server ready", "address", listener.Addr().String())
	go func() {
		serveResult <- a.server.Serve(listener)
	}()

	select {
	case err := <-serveResult:
		a.ready.Store(false)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	a.ready.Store(false)
	a.logger.Info("server shutdown started")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.config.ShutdownTimeout)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}
	a.logger.Info("server stopped")
	return nil
}
