package web

import (
	"context"
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
	policy, err := authorization.NewPolicy(
		authorization.ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"},
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler, err := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), ready, fakeAuthenticator{}, policy)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}
