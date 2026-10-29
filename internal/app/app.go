package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/gopher-launch/curu/internal/auth"
	"github.com/gopher-launch/curu/internal/authorization"
	"github.com/gopher-launch/curu/internal/config"
	"github.com/gopher-launch/curu/internal/web"
)

type App struct {
	config config.Config
	logger *slog.Logger
	ready  atomic.Bool
	auth   *auth.Authenticator
	server *http.Server
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	authenticator, err := auth.New(auth.Config{
		Profile: cfg.JWT.Profile, Issuer: cfg.JWT.Issuer, Audience: cfg.JWT.Audience,
		JWKSURL: cfg.JWT.JWKSURL, Algorithms: cfg.JWT.Algorithms,
		ClockSkew: cfg.JWT.ClockSkew, MaxLifetime: cfg.JWT.MaxLifetime,
		FetchTimeout: cfg.JWT.JWKSFetchTimeout, DefaultCacheTTL: cfg.JWT.JWKSDefaultCacheTTL,
		UnknownKIDRefreshInterval: cfg.JWT.JWKSUnknownKIDRefreshRate,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("create authenticator: %w", err)
	}
	policy, err := authorization.NewPolicy(authorization.ExternalIdentity{
		Issuer: cfg.Owner.Issuer, Subject: cfg.Owner.Subject,
	}, cfg.Owner.ID)
	if err != nil {
		return nil, fmt.Errorf("create authorization policy: %w", err)
	}
	application := &App{config: cfg, logger: logger, auth: authenticator}
	handler, err := web.NewHandler(logger, func() bool { return application.ready.Load() && application.auth.Ready() }, authenticator, policy)
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
	a.logger.Info("server listening", "address", listener.Addr().String())
	go a.auth.Maintain(ctx, func() {
		a.logger.Warn("authentication key refresh failed", "reason", "jwks_refresh_failed")
	})
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
