package web

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/gopher-launch/curu/internal/auth"
	"github.com/gopher-launch/curu/internal/authorization"
)

//go:embed templates/*.html
var templateFiles embed.FS

type handler struct {
	logger    *slog.Logger
	ready     func() bool
	auth      requestAuthenticator
	policy    authorization.Policy
	templates *template.Template
}

type requestAuthenticator interface {
	AuthenticateRequest(context.Context, []string) (auth.Principal, error)
}

type principalContextKey struct{}
type requestLogContextKey struct{}

type requestLogContext struct {
	correlationID  string
	securityDenied bool
}

// PrincipalFromContext returns the internal owner principal established by
// the shared authentication and authorization boundary.
func PrincipalFromContext(ctx context.Context) (authorization.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(authorization.Principal)
	return principal, ok
}

func NewHandler(logger *slog.Logger, ready func() bool, authenticator requestAuthenticator, policy authorization.Policy) (http.Handler, error) {
	templates, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	if authenticator == nil {
		return nil, fmt.Errorf("authenticator is required")
	}
	h := &handler{logger: logger, ready: ready, auth: authenticator, policy: policy, templates: templates}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", h.live)
	mux.HandleFunc("GET /health/ready", h.readiness)
	mux.HandleFunc("GET /", h.notFound)
	return h.logRequests(h.authorizePrivateRequests(mux)), nil
}

func (h *handler) authorizePrivateRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}

		external, err := h.auth.AuthenticateRequest(r.Context(), r.Header.Values("Authorization"))
		if err != nil {
			if errors.Is(err, auth.ErrKeyService) {
				h.deny(w, r, http.StatusServiceUnavailable, "authentication_unavailable")
				return
			}
			w.Header().Set("WWW-Authenticate", "Bearer")
			h.deny(w, r, http.StatusUnauthorized, "authentication_failed")
			return
		}
		principal, err := h.policy.PrincipalFor(authorization.ExternalIdentity{Issuer: external.Issuer, Subject: external.Subject})
		if err != nil {
			h.deny(w, r, http.StatusForbidden, "authorization_denied")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
	})
}

func (h *handler) deny(w http.ResponseWriter, r *http.Request, status int, reason string) {
	requestLog, _ := r.Context().Value(requestLogContextKey{}).(*requestLogContext)
	if requestLog == nil {
		requestLog = &requestLogContext{correlationID: rand.Text()}
	}
	requestLog.securityDenied = true
	h.logger.Warn("security failure", "reason", reason, "request_id", requestLog.correlationID)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(http.StatusText(status) + "\n"))
}

func (h *handler) live(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (h *handler) readiness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !h.ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready\n"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}

func (h *handler) notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	data := struct{ Path string }{Path: r.URL.Path}
	if err := h.templates.ExecuteTemplate(w, "not-found", data); err != nil {
		h.logger.Error("render response failed", "error", err)
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (h *handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		requestLog := &requestLogContext{correlationID: rand.Text()}
		next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), requestLogContextKey{}, requestLog)))
		if requestLog.securityDenied {
			return
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		h.logger.Info("HTTP request", "request_id", requestLog.correlationID, "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds())
	})
}
