package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
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
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/%3Cscript%3E", nil))
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
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader("private=data")))
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func newTestHandler(t *testing.T, ready func() bool) http.Handler {
	t.Helper()
	handler, err := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), ready)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}
