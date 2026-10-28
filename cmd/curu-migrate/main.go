package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/gopher-launch/curu/internal/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	dsn := os.Getenv("CURU_MIGRATION_DATABASE_URL")
	runtimeRole := os.Getenv("CURU_RUNTIME_DATABASE_ROLE")
	if dsn == "" || runtimeRole == "" {
		logger.Error("migration configuration invalid", "error", "CURU_MIGRATION_DATABASE_URL and CURU_RUNTIME_DATABASE_ROLE are required")
		os.Exit(2)
	}

	db, err := database.Open(context.Background(), dsn)
	if err != nil {
		logger.Error("migration database connection failed", "error", err)
		os.Exit(1)
	}
	defer database.Close(db)
	if err := database.Migrate(context.Background(), db, runtimeRole); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations complete", "schema_version", database.LatestSchemaVersion)
}
