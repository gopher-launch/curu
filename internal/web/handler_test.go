package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
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
	if !strings.Contains(body, "Curu") || strings.Contains(body, "/<script>") || !strings.Contains(body, "/&lt;script&gt;") {
		t.Errorf("body is missing layout or contains unescaped input: %q", body)
	}
}

func TestProductLikeMethodsAreUnavailable(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool
	handler := newTestHandler(t, ready.Load)
	cookie := csrfCookie(t, handler)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader("private=data"))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("X-CSRF-Token", cookie.Value)
	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestBrowserProtectionRejectsCrossOriginAndInvalidCSRF(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, func() bool { return true })
	cookie := csrfCookie(t, handler)
	for _, test := range []struct {
		name, origin, token, fetchSite string
		status                         int
	}{
		{name: "missing origin", token: cookie.Value, status: http.StatusForbidden},
		{name: "cross origin", origin: "https://attacker.example", token: cookie.Value, status: http.StatusForbidden},
		{name: "cross-site fetch", origin: "https://curu.example", token: cookie.Value, fetchSite: "cross-site", status: http.StatusForbidden},
		{name: "missing token", origin: "https://curu.example", status: http.StatusUnsupportedMediaType},
		{name: "wrong token", origin: "https://curu.example", token: mustCSRFToken(t), status: http.StatusForbidden},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader("title=entered"))
			request.Header.Set("Authorization", "Bearer owner")
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.token != "" {
				request.Header.Set("X-CSRF-Token", test.token)
			}
			if test.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			}
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Errorf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestSafePageReusesValidCSRFTokenAndConfiguresHTMX(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, func() bool { return true })
	cookie := csrfCookie(t, handler)
	request := httptest.NewRequest(http.MethodGet, "/edit", nil)
	request.Header.Set("Authorization", "Bearer owner")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if setCookies := response.Result().Cookies(); len(setCookies) != 0 {
		t.Errorf("valid CSRF cookie was rotated: %+v", setCookies)
	}
	body := response.Body.String()
	if !strings.Contains(body, `name="curu-csrf-token" content="`+cookie.Value+`"`) || !strings.Contains(body, `headers["X-CSRF-Token"]`) {
		t.Errorf("safe page does not configure HTMX CSRF: %q", body)
	}
}

func TestFormMutationIsBoundedAndPreservesValidationInput(t *testing.T) {
	t.Parallel()

	handler := newValidationFixtureHandler(t)
	cookie := csrfCookie(t, handler)
	value := `<script>alert("private")</script>`
	body := "csrf_token=" + cookie.Value + "&title=" + value
	request := httptest.NewRequest(http.MethodPost, "/edit", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "<script>") || !strings.Contains(response.Body.String(), template.HTMLEscapeString(value)) {
		t.Errorf("validation response did not safely preserve input: %q", response.Body.String())
	}

	oversized := httptest.NewRequest(http.MethodPost, "/edit", strings.NewReader(strings.Repeat("x", maxRequestBody+1)))
	oversized.Header.Set("Authorization", "Bearer owner")
	oversized.Header.Set("Origin", "https://curu.example")
	oversized.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	oversized.AddCookie(cookie)
	tooLarge := httptest.NewRecorder()
	handler.ServeHTTP(tooLarge, oversized)
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized status = %d, want 413", tooLarge.Code)
	}
}

func TestSafeRequestBodyIsBounded(t *testing.T) {
	t.Parallel()

	handler := newBodyReadingFixtureHandler(t)
	for _, test := range []struct {
		name   string
		size   int
		status int
	}{
		{name: "at limit", size: maxRequestBody, status: http.StatusNoContent},
		{name: "over limit", size: maxRequestBody + 1, status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/read-body", strings.NewReader(strings.Repeat("x", test.size)))
			request.Header.Set("Authorization", "Bearer owner")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Errorf("safe request status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestHTMXAuthenticationFailurePreservesEditorDOM(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, func() bool { return true })
	request := httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader("title=unsaved private value"))
	request.Header.Set("Authorization", "Bearer expired")
	request.Header.Set("HX-Request", "true")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("response = %d, challenge %q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
	if response.Header().Get("HX-Reswap") != "none" || response.Header().Get("HX-Trigger") != htmxAuthEventName {
		t.Errorf("HTMX preservation headers = reswap %q, trigger %q", response.Header().Get("HX-Reswap"), response.Header().Get("HX-Trigger"))
	}
	if strings.Contains(response.Body.String(), "unsaved private value") {
		t.Errorf("response exposes submitted value: %q", response.Body.String())
	}
}

func TestTrustedProxyAddressRequiresConfiguredImmediatePeer(t *testing.T) {
	t.Parallel()

	fixture := newClientAddressFixtureHandler(t)
	for _, test := range []struct {
		name, remote, forwarded, want string
	}{
		{name: "trusted peer", remote: "10.0.0.4:1234", forwarded: "203.0.113.7", want: "203.0.113.7"},
		{name: "untrusted peer", remote: "192.0.2.4:1234", forwarded: "203.0.113.7", want: "192.0.2.4"},
		{name: "appended chain ignored", remote: "10.0.0.4:1234", forwarded: "203.0.113.7, 192.0.2.8", want: "10.0.0.4"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/address", nil)
		request.RemoteAddr = test.remote
		request.Header.Set("Authorization", "Bearer owner")
		request.Header.Set("X-Forwarded-For", test.forwarded)
		response := httptest.NewRecorder()
		fixture.ServeHTTP(response, request)
		if strings.TrimSpace(response.Body.String()) != test.want {
			t.Errorf("%s address = %q, want %q", test.name, response.Body.String(), test.want)
		}
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
	handler, err := NewHandler(logger, ready, fakeAuthenticator{}, policy, BrowserConfig{PublicOrigin: "https://curu.example"})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func csrfCookie(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/edit", nil)
	request.Header.Set("Authorization", "Bearer owner")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" {
				t.Fatalf("CSRF cookie attributes = %+v", cookie)
			}
			return cookie
		}
	}
	t.Fatal("CSRF cookie not issued")
	return nil
}

func mustCSRFToken(t *testing.T) string {
	t.Helper()
	token, err := newCSRFToken()
	if err != nil {
		t.Fatalf("newCSRFToken() error = %v", err)
	}
	return token
}

func newValidationFixtureHandler(t *testing.T) http.Handler {
	t.Helper()
	h := newFixtureBoundary(t, BrowserConfig{PublicOrigin: "https://curu.example"})
	fixture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`<input name="title" value="` + template.HTMLEscapeString(r.PostForm.Get("title")) + `">`))
	})
	return h.logRequests(h.authorizePrivateRequests(h.protectBrowserRequests(fixture)))
}

func newClientAddressFixtureHandler(t *testing.T) http.Handler {
	t.Helper()
	prefix := netip.MustParsePrefix("10.0.0.0/24")
	h := newFixtureBoundary(t, BrowserConfig{PublicOrigin: "https://curu.example", TrustedProxyCIDRs: []netip.Prefix{prefix}})
	fixture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address, _ := ClientAddressFromRequest(r)
		_, _ = w.Write([]byte(address.String()))
	})
	return h.logRequests(h.authorizePrivateRequests(h.protectBrowserRequests(fixture)))
}

func newBodyReadingFixtureHandler(t *testing.T) http.Handler {
	t.Helper()
	h := newFixtureBoundary(t, BrowserConfig{PublicOrigin: "https://curu.example"})
	fixture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return h.logRequests(h.authorizePrivateRequests(h.protectBrowserRequests(fixture)))
}

func newFixtureBoundary(t *testing.T, browser BrowserConfig) *handler {
	t.Helper()
	policy, err := authorization.NewPolicy(
		authorization.ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"},
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	)
	if err != nil {
		t.Fatal(err)
	}
	return &handler{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), ready: func() bool { return true }, auth: fakeAuthenticator{}, policy: policy, browser: browser}
}
