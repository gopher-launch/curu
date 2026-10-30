package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gopher-launch/curu/internal/database"
	"github.com/gopher-launch/curu/internal/recovery"
)

func main() {
	expectPath := flag.String("expect", "", "compare the restored database with a previously captured manifest")
	flag.Parse()
	if flag.NArg() != 0 {
		fail(2, "unexpected positional arguments")
	}
	dsn := os.Getenv("CURU_RECOVERY_DATABASE_URL")
	if dsn == "" {
		fail(2, "CURU_RECOVERY_DATABASE_URL is required")
	}

	ctx := context.Background()
	db, err := database.Open(ctx, dsn)
	if err != nil {
		fail(1, "recovery database connection failed: %v", err)
	}
	defer database.Close(db)
	manifest, err := recovery.Capture(ctx, db)
	if err != nil {
		fail(1, "recovery validation failed: %v", err)
	}

	if *expectPath != "" {
		file, err := os.Open(*expectPath)
		if err != nil {
			fail(1, "read expected manifest: %v", err)
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
		if err != nil {
			fail(1, "read expected manifest: %v", err)
		}
		if len(content) > 64*1024 {
			fail(1, "decode expected manifest: file exceeds 64 KiB")
		}
		var expected recovery.Manifest
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&expected); err != nil {
			fail(1, "decode expected manifest: %v", err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			fail(1, "decode expected manifest: trailing or oversized data")
		}
		if expected != manifest {
			fail(1, "restored application data does not match the expected manifest")
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(manifest); err != nil {
		fail(1, "write recovery manifest: %v", err)
	}
}

func fail(code int, format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(code)
}
