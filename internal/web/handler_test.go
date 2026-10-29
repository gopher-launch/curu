package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gopher-launch/curu/internal/auth"
	"github.com/gopher-launch/curu/internal/authorization"
)

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool
	ready.Store(true)
	handler := newTestHandler(t, ready.Load)

	tests := []struct {
		path string
		body string
	}{
		{path: "/health/live", body: "ok\n"},
		{path: "/health/ready", body: "ready\n"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Body.String() != tt.body {
				t.Errorf("response = (%d, %q)", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestReadinessFailsClosedBeforeServerStarts(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool
	handler := newTestHandler(t, ready.Load)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "not ready\n" {
		t.Errorf("response = (%d, %q)", response.Code, response.Body.String())
	}
}

func TestUnknownRouteUsesEscapedLayout(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool
	handler := newTestHandler(t, ready.Load)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/%3Cscript%3E", nil)
	request.Header.Set("Authorization", "Bearer owner")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Curu") || strings.Contains(body, "<script>") {
		t.Errorf("body is missing layout or contains unescaped input: %q", body)
	}
}

func TestProductLikeMethodsAreUnavailable(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool
	handler := newTestHandler(t, ready.Load)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader("private=data"))
	request.Header.Set("Authorization", "Bearer owner")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestPrivateRoutesDistinguishAuthenticationAndAuthorization(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool
	handler := newTestHandler(t, ready.Load)
	tests := []struct {
		name      string
		token     string
		status    int
		challenge string
	}{
		{name: "missing", status: http.StatusUnauthorized, challenge: "Bearer"},
		{name: "invalid", token: "invalid", status: http.StatusUnauthorized, challenge: "Bearer"},
		{name: "dependency unavailable", token: "outage", status: http.StatusServiceUnavailable},
		{name: "authenticated stranger", token: "stranger", status: http.StatusForbidden},
		{name: "owner", token: "owner", status: http.StatusNotFound},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/activities/guessed", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if got := response.Header().Get("WWW-Authenticate"); got != test.challenge {
				t.Errorf("WWW-Authenticate = %q, want %q", got, test.challenge)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			if test.token != "" && strings.Contains(response.Body.String(), test.token) {
				t.Errorf("response exposes credential: %q", response.Body.String())
			}
		})
	}
}

func TestEveryNonHealthRouteUsesOwnerBoundary(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool
	handler := newTestHandler(t, ready.Load)
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/activities/guessed"},
		{http.MethodPost, "/activities"},
		{http.MethodPatch, "/activities/guessed"},
		{http.MethodPost, "/activities/guessed/archive"},
		{http.MethodGet, "/recovery-drafts/guessed"},
		{http.MethodGet, "/htmx/activities"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Header.Set("HX-Request", "true")
		request.Header.Set("Authorization", "Bearer stranger")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403", test.method, test.path, response.Code)
		}
	}
}

func TestSecurityFailureLogsContainOnlyReasonAndCorrelation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		token  string
		reason string
	}{
		{name: "unauthenticated", token: "private-token", reason: "authentication_failed"},
		{name: "unauthorized", token: "stranger", reason: "authorization_denied"},
		{name: "authentication unavailable", token: "outage", reason: "authentication_unavailable"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			handler := newTestHandlerWithLogger(t, func() bool { return false }, slog.New(slog.NewJSONHandler(&logs, nil)))
			request := httptest.NewRequest(http.MethodPost, "/activities/secret-object?private=query", strings.NewReader("private=form"))
			request.Header.Set("Authorization", "Bearer "+test.token)
			request.Header.Set("Cookie", "private=cookie")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			decoder := json.NewDecoder(&logs)
			var record map[string]any
			if err := decoder.Decode(&record); err != nil {
				t.Fatalf("decode security log: %v; log = %q", err, logs.String())
			}
			if decoder.More() {
				t.Fatalf("unexpected additional log record: %q", logs.String())
			}
			if record["msg"] != "security failure" || record["reason"] != test.reason {
				t.Errorf("security log = %#v", record)
			}
			requestID, ok := record["request_id"].(string)
			if !ok || requestID == "" || len(requestID) > 64 {
				t.Errorf("request_id = %#v", record["request_id"])
			}
			for _, forbidden := range []string{"method", "path", "status", "duration_ms", "issuer", "subject", "token", "cookie", "form"} {
				if _, present := record[forbidden]; present {
					t.Errorf("security log contains %q: %#v", forbidden, record)
				}
			}
			for _, private := range []string{test.token, "secret-object", "private=query", "private=form", "private=cookie"} {
				if strings.Contains(logs.String(), private) {
					t.Errorf("security log exposes private value %q: %q", private, logs.String())
				}
			}
			var extra map[string]any
			if err := decoder.Decode(&extra); err != io.EOF {
				t.Errorf("second log decode error = %v; record = %#v", err, extra)
			}
		})
	}
}

type fakeAuthenticator struct{}

func (fakeAuthenticator) AuthenticateRequest(_ context.Context, values []string) (auth.Principal, error) {
	if len(values) != 1 {
		return auth.Principal{}, auth.ErrInvalidCredentials
	}
	switch values[0] {
	case "Bearer owner":
		return auth.Principal{Issuer: "https://issuer.example/tenant", Subject: "coach-123"}, nil
	case "Bearer stranger":
		return auth.Principal{Issuer: "https://issuer.example/tenant", Subject: "someone-else"}, nil
	case "Bearer outage":
		return auth.Principal{}, auth.ErrKeyService
	default:
		return auth.Principal{}, auth.ErrInvalidCredentials
	}
}

func newTestHandler(t *testing.T, ready func() bool) http.Handler {
	t.Helper()
	return newTestHandlerWithLogger(t, ready, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func newTestHandlerWithLogger(t *testing.T, ready func() bool, logger *slog.Logger) http.Handler {
	t.Helper()
	policy, err := authorization.NewPolicy(
		authorization.ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"},
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler, err := NewHandler(logger, ready, fakeAuthenticator{}, policy)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}
