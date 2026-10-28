package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
)

func TestStandardProfileAcceptsMediaTypesCaseInsensitively(t *testing.T) {
	fixture := newFixture(t, []string{"RS256"})
	for _, typ := range []string{"at+jwt", "application/at+jwt", "AT+JWT", "Application/AT+JWT"} {
		t.Run(typ, func(t *testing.T) {
			token := fixture.signRSA(t, fixture.claims(), typ, "rsa-1", fixture.rsaKey)
			principal, err := fixture.auth.Validate(context.Background(), token)
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if principal != (Principal{Issuer: fixture.issuer, Subject: "coach-123"}) {
				t.Errorf("principal = %+v", principal)
			}
		})
	}
}

func TestStandardProfileRejectsWrongPurposeClaimsAndTrust(t *testing.T) {
	fixture := newFixture(t, []string{"RS256"})
	now := fixture.now
	tests := map[string]func() string{
		"missing type":         func() string { return fixture.signRSA(t, fixture.claims(), "", "rsa-1", fixture.rsaKey) },
		"same issuer ID token": func() string { return fixture.signRSA(t, fixture.claims(), "JWT", "rsa-1", fixture.rsaKey) },
		"wrong issuer": func() string {
			claims := fixture.claims()
			claims["iss"] = "https://other.example"
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"wrong audience": func() string {
			claims := fixture.claims()
			claims["aud"] = "other-resource"
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"missing client ID": func() string {
			claims := fixture.claims()
			delete(claims, "client_id")
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"missing issued at": func() string {
			claims := fixture.claims()
			delete(claims, "iat")
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"missing expiry": func() string {
			claims := fixture.claims()
			delete(claims, "exp")
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"malformed JWT ID": func() string {
			claims := fixture.claims()
			claims["jti"] = 42
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"expired": func() string {
			claims := fixture.claims()
			claims["iat"] = now.Add(-20 * time.Minute).Unix()
			claims["exp"] = now.Add(-5 * time.Minute).Unix()
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"excessive lifetime": func() string {
			claims := fixture.claims()
			claims["iat"] = now.Unix()
			claims["exp"] = now.Add(16 * time.Minute).Unix()
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"future issued at": func() string {
			claims := fixture.claims()
			claims["iat"] = now.Add(2 * time.Minute).Unix()
			claims["exp"] = now.Add(10 * time.Minute).Unix()
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"future not before": func() string {
			claims := fixture.claims()
			claims["nbf"] = now.Add(2 * time.Minute).Unix()
			return fixture.signRSA(t, claims, "at+jwt", "rsa-1", fixture.rsaKey)
		},
		"unknown key": func() string { return fixture.signRSA(t, fixture.claims(), "at+jwt", "unknown", fixture.rsaKey) },
		"substituted key": func() string {
			other, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			return fixture.signRSA(t, fixture.claims(), "at+jwt", "rsa-1", other)
		},
	}
	for name, makeToken := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := fixture.auth.Validate(context.Background(), makeToken()); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Validate() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestConfiguredES256KeyIsAccepted(t *testing.T) {
	fixture := newFixture(t, []string{"ES256"})
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture.keys.set(t, key.Public(), "ec-1")
	token := sign(t, fixture.claims(), "at+jwt", "ec-1", jwa.ES256(), key)
	if _, err := fixture.auth.Validate(context.Background(), token); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestReadinessRequiresFreshUsableKeys(t *testing.T) {
	fixture := newFixture(t, []string{"ES256"})
	if fixture.auth.Ready() {
		t.Fatal("Ready() before fetch = true")
	}
	if err := fixture.auth.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() accepted an RSA-only set for ES256 configuration")
	}
	if fixture.auth.Ready() {
		t.Fatal("Ready() with unusable set = true")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture.keys.set(t, key.Public(), "ec-1")
	if err := fixture.auth.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !fixture.auth.Ready() {
		t.Fatal("Ready() with fresh usable set = false")
	}
}

func TestCacheFreshnessDefaultsCapsAndRefusesNoCache(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	if got := cacheTTL(http.Header{}, now, 5*time.Minute); got != 5*time.Minute {
		t.Errorf("default cache TTL = %v", got)
	}
	if got := cacheTTL(http.Header{"Cache-Control": {"public, max-age=3600"}}, now, 5*time.Minute); got != 15*time.Minute {
		t.Errorf("capped cache TTL = %v", got)
	}
	if got := cacheTTL(http.Header{"Cache-Control": {"max-age=300"}, "Age": {"60"}}, now, 5*time.Minute); got != 4*time.Minute {
		t.Errorf("aged cache TTL = %v", got)
	}
	if got := cacheTTL(http.Header{"Cache-Control": {"no-store"}}, now, 5*time.Minute); got != 0 {
		t.Errorf("no-store cache TTL = %v", got)
	}
}

func TestRejectsDisallowedAlgorithmDuplicateClaimsAndFallback(t *testing.T) {
	fixture := newFixture(t, []string{"RS256"})
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	disallowed := sign(t, fixture.claims(), "at+jwt", "ec-1", jwa.ES256(), ecKey)
	if _, err := fixture.auth.Validate(context.Background(), disallowed); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("ES256 Validate() error = %v", err)
	}

	payload := []byte(`{"iss":"` + fixture.issuer + `","iss":"` + fixture.issuer + `","sub":"coach-123","aud":"curu-resource","client_id":"gateway","jti":"duplicate","iat":` + jsonNumber(fixture.now.Unix()) + `,"exp":` + jsonNumber(fixture.now.Add(10*time.Minute).Unix()) + `}`)
	duplicate := signPayload(t, payload, "at+jwt", "rsa-1", jwa.RS256(), fixture.rsaKey)
	if _, err := fixture.auth.Validate(context.Background(), duplicate); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("duplicate claim Validate() error = %v", err)
	}

	badProfile := fixture.config
	badProfile.Profile = "generic-jwt"
	if _, err := New(badProfile, fixture.server.Client()); err == nil {
		t.Fatal("New() accepted an unsupported fallback profile")
	}
}

func TestRotationOverlapAndUnknownKIDRefresh(t *testing.T) {
	fixture := newFixture(t, []string{"RS256"})
	oldToken := fixture.signRSA(t, fixture.claims(), "at+jwt", "rsa-1", fixture.rsaKey)
	if _, err := fixture.auth.Validate(context.Background(), oldToken); err != nil {
		t.Fatalf("initial Validate() error = %v", err)
	}
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fixture.keys.set(t, fixture.rsaKey.Public(), "rsa-1", newKey.Public(), "rsa-2")
	newToken := fixture.signRSA(t, fixture.claims(), "at+jwt", "rsa-2", newKey)
	if _, err := fixture.auth.Validate(context.Background(), newToken); err != nil {
		t.Fatalf("rotated Validate() error = %v", err)
	}
	if _, err := fixture.auth.Validate(context.Background(), oldToken); err != nil {
		t.Fatalf("overlap Validate() error = %v", err)
	}
}

func TestRefreshFailureUsesOnlyFreshKnownKeys(t *testing.T) {
	fixture := newFixture(t, []string{"RS256"})
	known := fixture.signRSA(t, fixture.claims(), "at+jwt", "rsa-1", fixture.rsaKey)
	if _, err := fixture.auth.Validate(context.Background(), known); err != nil {
		t.Fatal(err)
	}
	fixture.keys.fail = true
	unknown := fixture.signRSA(t, fixture.claims(), "at+jwt", "rsa-2", fixture.rsaKey)
	if _, err := fixture.auth.Validate(context.Background(), unknown); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("fresh unknown-key error = %v", err)
	}
	if _, err := fixture.auth.Validate(context.Background(), known); err != nil {
		t.Fatalf("fresh cached key error = %v", err)
	}
	fixture.auth.mu.Lock()
	fixture.auth.keysExpireAt = fixture.now.Add(-time.Second)
	fixture.auth.mu.Unlock()
	if _, err := fixture.auth.Validate(context.Background(), known); !errors.Is(err, ErrKeyService) {
		t.Fatalf("expired cache error = %v, want ErrKeyService", err)
	}
}

func TestAuthenticateRequestRequiresOneBearerHeader(t *testing.T) {
	fixture := newFixture(t, []string{"RS256"})
	token := fixture.signRSA(t, fixture.claims(), "at+jwt", "rsa-1", fixture.rsaKey)
	if _, err := fixture.auth.AuthenticateRequest(context.Background(), []string{"bearer " + token}); err != nil {
		t.Fatalf("AuthenticateRequest() error = %v", err)
	}
	for _, values := range [][]string{nil, {"Basic abc"}, {"Bearer"}, {"Bearer " + token, "Bearer " + token}} {
		if _, err := fixture.auth.AuthenticateRequest(context.Background(), values); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("AuthenticateRequest(%q) error = %v", values, err)
		}
	}
}

func TestMalformedUnsignedAndRepeatedUnknownTokensFailClosed(t *testing.T) {
	fixture := newFixture(t, []string{"RS256"})
	for _, token := range []string{"not.a.jwt", "eyJhbGciOiJub25lIiwia2lkIjoieCIsInR5cCI6ImF0K2p3dCJ9.e30.x"} {
		if _, err := fixture.auth.Validate(context.Background(), token); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("Validate(%q) error = %v", token, err)
		}
	}
	known := fixture.signRSA(t, fixture.claims(), "at+jwt", "rsa-1", fixture.rsaKey)
	if _, err := fixture.auth.Validate(context.Background(), known); err != nil {
		t.Fatal(err)
	}
	fixture.auth.config.UnknownKIDRefreshInterval = time.Minute
	unknown := fixture.signRSA(t, fixture.claims(), "at+jwt", "missing", fixture.rsaKey)
	for range 2 {
		if _, err := fixture.auth.Validate(context.Background(), unknown); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("unknown-key error = %v", err)
		}
	}
	fixture.keys.mu.Lock()
	requests := fixture.keys.requests
	fixture.keys.mu.Unlock()
	if requests != 2 {
		t.Errorf("JWKS requests = %d, want initial fetch plus one rate-limited refresh", requests)
	}
}

type authFixture struct {
	auth   *Authenticator
	config Config
	server *httptest.Server
	keys   *keyServer
	rsaKey *rsa.PrivateKey
	now    time.Time
	issuer string
}

type keyServer struct {
	mu       sync.Mutex
	body     []byte
	fail     bool
	requests int
}

func newFixture(t *testing.T, algorithms []string) *authFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keys := &keyServer{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		keys.mu.Lock()
		defer keys.mu.Unlock()
		keys.requests++
		if keys.fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(keys.body)
	}))
	t.Cleanup(server.Close)
	keys.set(t, key.Public(), "rsa-1")
	now := time.Unix(1_800_000_000, 0).UTC()
	config := Config{
		Profile: "rfc9068", Issuer: server.URL + "/issuer", Audience: "curu-resource",
		JWKSURL: server.URL, Algorithms: algorithms, ClockSkew: time.Minute,
		MaxLifetime: 15 * time.Minute, FetchTimeout: time.Second,
		DefaultCacheTTL: 5 * time.Minute, UnknownKIDRefreshInterval: time.Nanosecond,
	}
	authenticator, err := New(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	authenticator.now = func() time.Time { return now }
	return &authFixture{auth: authenticator, config: config, server: server, keys: keys, rsaKey: key, now: now, issuer: config.Issuer}
}

func (f *authFixture) claims() map[string]any {
	return map[string]any{
		"iss": f.issuer, "sub": "coach-123", "aud": []string{"other", f.config.Audience},
		"client_id": "gateway", "jti": "token-1", "iat": f.now.Unix(), "exp": f.now.Add(10 * time.Minute).Unix(),
	}
}

func (f *authFixture) signRSA(t *testing.T, claims map[string]any, typ, kid string, key *rsa.PrivateKey) string {
	return sign(t, claims, typ, kid, jwa.RS256(), key)
}

func (s *keyServer) set(t *testing.T, values ...any) {
	t.Helper()
	set := jwk.NewSet()
	for index := 0; index < len(values); index += 2 {
		key, err := jwk.Import(values[index])
		if err != nil {
			t.Fatal(err)
		}
		if err := key.Set("kid", values[index+1].(string)); err != nil {
			t.Fatal(err)
		}
		if err := key.Set("use", "sig"); err != nil {
			t.Fatal(err)
		}
		if err := set.AddKey(key); err != nil {
			t.Fatal(err)
		}
	}
	body, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.body = body
	s.mu.Unlock()
}

func sign(t *testing.T, claims map[string]any, typ, kid string, algorithm jwa.SignatureAlgorithm, key any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return signPayload(t, payload, typ, kid, algorithm, key)
}

func signPayload(t *testing.T, payload []byte, typ, kid string, algorithm jwa.SignatureAlgorithm, key any) string {
	t.Helper()
	headers := jws.NewHeaders()
	if typ != "" {
		if err := headers.Set("typ", typ); err != nil {
			t.Fatal(err)
		}
	}
	if err := headers.Set("kid", kid); err != nil {
		t.Fatal(err)
	}
	compact, err := jws.Sign(payload, jws.WithKey(algorithm, key, jws.WithProtectedHeaders(headers)))
	if err != nil {
		t.Fatal(err)
	}
	return string(compact)
}

func jsonNumber(value int64) string { return strconv.FormatInt(value, 10) }
