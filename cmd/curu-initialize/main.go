package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/gopher-launch/curu/internal/database"
	"github.com/gopher-launch/curu/internal/initialization"
	"github.com/gopher-launch/curu/internal/seed"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	mode := flag.String("mode", "", "required initialization mode: seed or empty")
	flag.Parse()
	if flag.NArg() != 0 || (*mode != string(initialization.KindSeed) && *mode != string(initialization.KindEmpty)) {
		logger.Error("initialization configuration invalid", "error", "exactly one --mode=seed or --mode=empty is required")
		os.Exit(2)
	}

	dsn := os.Getenv("CURU_INITIALIZATION_DATABASE_URL")
	if dsn == "" {
		logger.Error("initialization configuration invalid", "error", "CURU_INITIALIZATION_DATABASE_URL is required")
		os.Exit(2)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, dsn)
	if err != nil {
		logger.Error("initialization database connection failed", "error", err)
		os.Exit(1)
	}
	defer database.Close(db)
	if err := database.CheckCompatibility(ctx, db); err != nil {
		logger.Error("initialization database is incompatible", "error", err)
		os.Exit(1)
	}

	var result initialization.Result
	if *mode == string(initialization.KindEmpty) {
		result, err = initialization.InitializeEmpty(ctx, db)
	} else {
		ownerID := os.Getenv("CURU_OWNER_ID")
		if ownerID == "" {
			logger.Error("initialization configuration invalid", "error", "CURU_OWNER_ID is required for seed mode")
			os.Exit(2)
		}
		library, loadErr := seed.Load()
		if loadErr != nil {
			logger.Error("seed asset invalid", "error", loadErr)
			os.Exit(1)
		}
		result, err = initialization.InitializeSeed(ctx, db, ownerID, library)
	}
	if err != nil {
		logger.Error("dataset initialization failed", "error", err)
		os.Exit(1)
	}
	logger.Info("dataset initialization complete", "outcome", result.Outcome, "kind", result.Kind,
		"seed_identity", result.SeedIdentity, "seed_version", result.SeedVersion)
}
