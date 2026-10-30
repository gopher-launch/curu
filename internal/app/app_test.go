package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/gopher-launch/curu/internal/config"
	"gorm.io/gorm"
)

func TestRunStopsAfterCancellation(t *testing.T) {
	t.Parallel()

	application, err := New(newTestConfig("127.0.0.1:0"), slog.New(slog.NewTextHandler(io.Discard, nil)), &gorm.DB{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := application.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunReportsBindFailure(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	application, err := New(newTestConfig(listener.Addr().String()), slog.New(slog.NewTextHandler(io.Discard, nil)), &gorm.DB{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := application.Run(ctx); err == nil {
		t.Fatal("Run() error = nil, want bind failure")
	}
}

func newTestConfig(address string) config.Config {
	return config.Config{
		ListenAddress:     address,
		DatabaseURL:       "postgres://curu@db.example/curu",
		ShutdownTimeout:   time.Second,
		ReadHeaderTimeout: time.Second,
		IdleTimeout:       time.Second,
		JWT: config.JWTConfig{
			Profile: "rfc9068", Issuer: "https://issuer.example", Audience: "curu-resource",
			JWKSURL: "https://keys.example/jwks.json", Algorithms: []string{"RS256"},
			ClockSkew: time.Minute, MaxLifetime: 15 * time.Minute, JWKSFetchTimeout: time.Second,
			JWKSDefaultCacheTTL: 5 * time.Minute, JWKSUnknownKIDRefreshRate: time.Minute,
		},
		Owner: config.OwnerConfig{
			Issuer: "https://issuer.example", Subject: "coach-123",
			ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		},
		Browser: config.BrowserConfig{PublicOrigin: "https://curu.example"},
	}
}
