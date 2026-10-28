package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddress     = "127.0.0.1:8080"
	defaultShutdownTimeout   = 10 * time.Second
	defaultReadHeaderTimeout = 5 * time.Second
	defaultIdleTimeout       = 60 * time.Second
)

// Config contains the settings owned by the minimal HTTP runtime. Security,
// identity, and database settings arrive with the roadmap tasks that implement
// those boundaries.
type Config struct {
	ListenAddress     string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
}

// Load reads and validates runtime configuration. lookup has the same contract
// as os.LookupEnv so tests do not need to mutate process-wide environment.
func Load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		ListenAddress:     valueOrDefault(lookup, "CURU_LISTEN_ADDRESS", defaultListenAddress),
		ShutdownTimeout:   defaultShutdownTimeout,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}

	var err error
	if cfg.ShutdownTimeout, err = duration(lookup, "CURU_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.ReadHeaderTimeout, err = duration(lookup, "CURU_READ_HEADER_TIMEOUT", cfg.ReadHeaderTimeout); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = duration(lookup, "CURU_IDLE_TIMEOUT", cfg.IdleTimeout); err != nil {
		return Config{}, err
	}
	if err := validateListenAddress(cfg.ListenAddress); err != nil {
		return Config{}, fmt.Errorf("CURU_LISTEN_ADDRESS: %w", err)
	}

	return cfg, nil
}

func valueOrDefault(lookup func(string) (string, bool), name, fallback string) string {
	if value, ok := lookup(name); ok {
		return value
	}
	return fallback
}

func duration(lookup func(string) (string, bool), name string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(name)
	if !ok {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: must be a duration: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s: must be greater than zero", name)
	}
	return parsed, nil
}

func validateListenAddress(address string) error {
	if strings.TrimSpace(address) != address || address == "" {
		return fmt.Errorf("must be a non-empty host:port without surrounding whitespace")
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("must be a valid host:port: %w", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return fmt.Errorf("port must be a number from 0 through 65535")
	}
	return nil
}
