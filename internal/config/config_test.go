package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(lookupWithDatabase(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddress != "127.0.0.1:8080" {
		t.Errorf("ListenAddress = %q", cfg.ListenAddress)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v", cfg.ShutdownTimeout)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"CURU_DATABASE_URL":        "postgres://curu_runtime:secret@db.example/curu?sslmode=require",
		"CURU_LISTEN_ADDRESS":      "0.0.0.0:9000",
		"CURU_SHUTDOWN_TIMEOUT":    "20s",
		"CURU_READ_HEADER_TIMEOUT": "7s",
		"CURU_IDLE_TIMEOUT":        "2m",
	}
	cfg, err := Load(func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddress != values["CURU_LISTEN_ADDRESS"] || cfg.ShutdownTimeout != 20*time.Second || cfg.ReadHeaderTimeout != 7*time.Second || cfg.IdleTimeout != 2*time.Minute {
		t.Errorf("Load() = %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := map[string]map[string]string{
		"missing database":    {},
		"bad database scheme": {"CURU_DATABASE_URL": "mysql://curu@db.example/curu"},
		"missing port":        {"CURU_LISTEN_ADDRESS": "localhost"},
		"out of range port":   {"CURU_LISTEN_ADDRESS": "localhost:70000"},
		"empty address":       {"CURU_LISTEN_ADDRESS": ""},
		"bad duration":        {"CURU_IDLE_TIMEOUT": "soon"},
		"zero duration":       {"CURU_SHUTDOWN_TIMEOUT": "0s"},
	}
	for name, values := range tests {
		values := values
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if name != "missing database" {
				values["CURU_DATABASE_URL"] = valueOr(values, "CURU_DATABASE_URL", "postgres://curu@db.example/curu")
			}
			_, err := Load(func(key string) (string, bool) {
				value, ok := values[key]
				return value, ok
			})
			if err == nil || strings.TrimSpace(err.Error()) == "" {
				t.Fatalf("Load() error = %v, want validation error", err)
			}
		})
	}
}

func lookupWithDatabase(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if value, ok := values[name]; ok {
			return value, true
		}
		if name == "CURU_DATABASE_URL" {
			return "postgres://curu@db.example/curu", true
		}
		return "", false
	}
}

func valueOr(values map[string]string, key, fallback string) string {
	if value, ok := values[key]; ok {
		return value
	}
	return fallback
}
