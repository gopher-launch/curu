package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gopher-launch/curu/internal/authorization"
)

const (
	defaultListenAddress     = "127.0.0.1:8080"
	defaultShutdownTimeout   = 10 * time.Second
	defaultReadHeaderTimeout = 5 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultJWTClockSkew      = 60 * time.Second
	defaultJWTMaxLifetime    = 15 * time.Minute
	defaultJWKSFetchTimeout  = 5 * time.Second
	defaultJWKSCacheTTL      = 5 * time.Minute
	defaultJWKSRefreshLimit  = time.Minute
)

// Config contains the settings owned by the current HTTP runtime. Additional
// security and identity settings arrive with their owning roadmap tasks.
type Config struct {
	ListenAddress     string
	DatabaseURL       string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	JWT               JWTConfig
	Owner             OwnerConfig
	Browser           BrowserConfig
}

// BrowserConfig defines the provider-neutral public browser and gateway
// boundary. PublicOrigin is deliberately independent of request headers.
type BrowserConfig struct {
	PublicOrigin      string
	TrustedProxyCIDRs []netip.Prefix
	LoginURL          string
	LogoutURL         string
}

// OwnerConfig binds one exact external identity to the stable dataset owner.
type OwnerConfig struct {
	Issuer  string
	Subject string
	ID      string
}

// JWTConfig is the deployment-selected trust boundary for access tokens.
// Only the standard OAuth JWT access-token profile is currently supported.
type JWTConfig struct {
	Profile                   string
	Issuer                    string
	Audience                  string
	JWKSURL                   string
	Algorithms                []string
	ClockSkew                 time.Duration
	MaxLifetime               time.Duration
	JWKSFetchTimeout          time.Duration
	JWKSDefaultCacheTTL       time.Duration
	JWKSUnknownKIDRefreshRate time.Duration
}

// Load reads and validates runtime configuration. lookup has the same contract
// as os.LookupEnv so tests do not need to mutate process-wide environment.
func Load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		ListenAddress:     valueOrDefault(lookup, "CURU_LISTEN_ADDRESS", defaultListenAddress),
		DatabaseURL:       valueOrDefault(lookup, "CURU_DATABASE_URL", ""),
		ShutdownTimeout:   defaultShutdownTimeout,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		IdleTimeout:       defaultIdleTimeout,
		JWT: JWTConfig{
			Profile:                   valueOrDefault(lookup, "CURU_JWT_PROFILE", ""),
			Issuer:                    valueOrDefault(lookup, "CURU_JWT_ISSUER", ""),
			Audience:                  valueOrDefault(lookup, "CURU_JWT_AUDIENCE", ""),
			JWKSURL:                   valueOrDefault(lookup, "CURU_JWKS_URL", ""),
			ClockSkew:                 defaultJWTClockSkew,
			MaxLifetime:               defaultJWTMaxLifetime,
			JWKSFetchTimeout:          defaultJWKSFetchTimeout,
			JWKSDefaultCacheTTL:       defaultJWKSCacheTTL,
			JWKSUnknownKIDRefreshRate: defaultJWKSRefreshLimit,
		},
		Owner: OwnerConfig{
			Issuer:  valueOrDefault(lookup, "CURU_OWNER_ISSUER", ""),
			Subject: valueOrDefault(lookup, "CURU_OWNER_SUBJECT", ""),
			ID:      valueOrDefault(lookup, "CURU_OWNER_ID", ""),
		},
		Browser: BrowserConfig{
			PublicOrigin: valueOrDefault(lookup, "CURU_PUBLIC_ORIGIN", ""),
			LoginURL:     valueOrDefault(lookup, "CURU_LOGIN_URL", ""),
			LogoutURL:    valueOrDefault(lookup, "CURU_LOGOUT_URL", ""),
		},
	}
	cfg.JWT.Algorithms = splitComma(valueOrDefault(lookup, "CURU_JWT_ALGORITHMS", ""))
	proxyCIDRs := splitComma(valueOrDefault(lookup, "CURU_TRUSTED_PROXY_CIDRS", ""))
	for _, value := range proxyCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.String() != value {
			return Config{}, fmt.Errorf("CURU_TRUSTED_PROXY_CIDRS: %q must be a canonical IP CIDR", value)
		}
		cfg.Browser.TrustedProxyCIDRs = append(cfg.Browser.TrustedProxyCIDRs, prefix)
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
	if cfg.JWT.ClockSkew, err = nonNegativeDuration(lookup, "CURU_JWT_CLOCK_SKEW", cfg.JWT.ClockSkew); err != nil {
		return Config{}, err
	}
	if cfg.JWT.MaxLifetime, err = duration(lookup, "CURU_JWT_MAX_LIFETIME", cfg.JWT.MaxLifetime); err != nil {
		return Config{}, err
	}
	if cfg.JWT.JWKSFetchTimeout, err = duration(lookup, "CURU_JWKS_FETCH_TIMEOUT", cfg.JWT.JWKSFetchTimeout); err != nil {
		return Config{}, err
	}
	if cfg.JWT.JWKSDefaultCacheTTL, err = duration(lookup, "CURU_JWKS_DEFAULT_CACHE_TTL", cfg.JWT.JWKSDefaultCacheTTL); err != nil {
		return Config{}, err
	}
	if cfg.JWT.JWKSUnknownKIDRefreshRate, err = duration(lookup, "CURU_JWKS_UNKNOWN_KID_REFRESH_INTERVAL", cfg.JWT.JWKSUnknownKIDRefreshRate); err != nil {
		return Config{}, err
	}
	if err := validateListenAddress(cfg.ListenAddress); err != nil {
		return Config{}, fmt.Errorf("CURU_LISTEN_ADDRESS: %w", err)
	}
	if err := validateDatabaseURL(cfg.DatabaseURL); err != nil {
		return Config{}, fmt.Errorf("CURU_DATABASE_URL: %w", err)
	}
	if err := validateJWT(cfg.JWT); err != nil {
		return Config{}, err
	}
	if err := validateOwner(cfg.Owner, cfg.JWT.Issuer); err != nil {
		return Config{}, err
	}
	if err := validateBrowser(cfg.Browser); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func validateBrowser(browser BrowserConfig) error {
	if err := validateHTTPSOrigin(browser.PublicOrigin); err != nil {
		return fmt.Errorf("CURU_PUBLIC_ORIGIN: %w", err)
	}
	for name, value := range map[string]string{
		"CURU_LOGIN_URL": browser.LoginURL, "CURU_LOGOUT_URL": browser.LogoutURL,
	} {
		if value == "" {
			continue
		}
		if err := validateHTTPSURL(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func validateHTTPSOrigin(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("must be an absolute HTTPS origin without user information, path, query, or fragment")
	}
	hostname := parsed.Hostname()
	if hostname == "" || hostname != strings.ToLower(hostname) || !isASCII(hostname) || strings.Contains(hostname, "%") {
		return fmt.Errorf("host must use its lowercase canonical ASCII spelling")
	}
	canonicalHost := hostname
	if address, err := netip.ParseAddr(hostname); err == nil {
		address = address.Unmap()
		if hostname != address.String() {
			return fmt.Errorf("IP address must use its canonical spelling")
		}
		if address.Is6() {
			canonicalHost = "[" + hostname + "]"
		}
	} else if strings.Contains(hostname, ":") {
		return fmt.Errorf("host must be a valid DNS name or IP address")
	}
	port := parsed.Port()
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return fmt.Errorf("port must be a number from 1 through 65535")
		}
		if strconv.Itoa(portNumber) != port || portNumber == 443 {
			return fmt.Errorf("port must use canonical non-default spelling")
		}
		canonicalHost += ":" + port
	}
	if parsed.Host != canonicalHost {
		return fmt.Errorf("authority must use canonical host and port spelling")
	}
	if parsed.String() != value {
		return fmt.Errorf("must use its canonical URL spelling")
	}
	return nil
}

func isASCII(value string) bool {
	for _, character := range value {
		if character > 127 {
			return false
		}
	}
	return true
}

func validateOwner(owner OwnerConfig, trustedIssuer string) error {
	if err := validateHTTPSURL(owner.Issuer); err != nil {
		return fmt.Errorf("CURU_OWNER_ISSUER: %w", err)
	}
	if owner.Issuer != trustedIssuer {
		return fmt.Errorf("CURU_OWNER_ISSUER: must exactly match CURU_JWT_ISSUER")
	}
	if strings.TrimSpace(owner.Subject) != owner.Subject || owner.Subject == "" {
		return fmt.Errorf("CURU_OWNER_SUBJECT: must be non-empty without surrounding whitespace")
	}
	if _, err := authorization.NormalizeUUID(owner.ID); err != nil {
		return fmt.Errorf("CURU_OWNER_ID: %w", err)
	}
	return nil
}

func validateJWT(cfg JWTConfig) error {
	if cfg.Profile != "rfc9068" {
		return fmt.Errorf("CURU_JWT_PROFILE: must be rfc9068")
	}
	if err := validateHTTPSURL(cfg.Issuer); err != nil {
		return fmt.Errorf("CURU_JWT_ISSUER: %w", err)
	}
	if strings.TrimSpace(cfg.Audience) != cfg.Audience || cfg.Audience == "" {
		return fmt.Errorf("CURU_JWT_AUDIENCE: must be non-empty without surrounding whitespace")
	}
	if err := validateHTTPSURL(cfg.JWKSURL); err != nil {
		return fmt.Errorf("CURU_JWKS_URL: %w", err)
	}
	if len(cfg.Algorithms) == 0 {
		return fmt.Errorf("CURU_JWT_ALGORITHMS: must select RS256 and/or ES256")
	}
	seen := make(map[string]bool, len(cfg.Algorithms))
	for _, algorithm := range cfg.Algorithms {
		if algorithm != "RS256" && algorithm != "ES256" {
			return fmt.Errorf("CURU_JWT_ALGORITHMS: unsupported algorithm %q", algorithm)
		}
		if seen[algorithm] {
			return fmt.Errorf("CURU_JWT_ALGORITHMS: duplicate algorithm %q", algorithm)
		}
		seen[algorithm] = true
	}
	if cfg.ClockSkew > time.Minute {
		return fmt.Errorf("CURU_JWT_CLOCK_SKEW: must not exceed 1m")
	}
	if cfg.MaxLifetime > 15*time.Minute {
		return fmt.Errorf("CURU_JWT_MAX_LIFETIME: must not exceed 15m")
	}
	if cfg.JWKSDefaultCacheTTL > 15*time.Minute {
		return fmt.Errorf("CURU_JWKS_DEFAULT_CACHE_TTL: must not exceed 15m")
	}
	return nil
}

func validateHTTPSURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("must be an absolute HTTPS URL without user information or fragment")
	}
	return nil
}

func validateDatabaseURL(value string) error {
	if value == "" {
		return fmt.Errorf("is required")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("must be a valid PostgreSQL URL")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("scheme must be postgres or postgresql")
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return fmt.Errorf("must include a database user")
	}
	if parsed.Host == "" || strings.TrimPrefix(parsed.Path, "/") == "" {
		return fmt.Errorf("must include a host and database name")
	}
	return nil
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

func nonNegativeDuration(lookup func(string) (string, bool), name string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(name)
	if !ok {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: must be a duration: %w", name, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s: must not be negative", name)
	}
	return parsed, nil
}

func splitComma(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
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
