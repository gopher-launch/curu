package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gopher-launch/curu/internal/activity"
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
	library   activityLibrary
	browser   BrowserConfig
	templates *template.Template
}

type requestAuthenticator interface {
	AuthenticateRequest(context.Context, []string) (auth.Principal, error)
}

type activityLibrary interface {
	List(context.Context, authorization.Principal, string) ([]activity.Summary, error)
	Detail(context.Context, authorization.Principal, string) (activity.Activity, error)
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

func NewHandler(logger *slog.Logger, ready func() bool, authenticator requestAuthenticator, policy authorization.Policy, library activityLibrary, browser BrowserConfig) (http.Handler, error) {
	templates, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	if authenticator == nil {
		return nil, fmt.Errorf("authenticator is required")
	}
	if browser.PublicOrigin == "" {
		return nil, fmt.Errorf("public origin is required")
	}
	if library == nil {
		return nil, fmt.Errorf("activity library is required")
	}
	h := &handler{logger: logger, ready: ready, auth: authenticator, policy: policy, library: library, browser: browser, templates: templates}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", h.live)
	mux.HandleFunc("GET /health/ready", h.readiness)
	mux.HandleFunc("GET /{$}", h.activityIndexRedirect)
	mux.HandleFunc("GET /activities", h.listActivities)
	mux.HandleFunc("GET /activities/{id}", h.activityDetail)
	mux.HandleFunc("GET /", h.notFound)
	return h.logRequests(h.authorizePrivateRequests(h.protectBrowserRequests(mux))), nil
}

type pageData struct {
	PageTitle string
	CSRFToken string
	LoginURL  string
	LogoutURL string
}

type activityListData struct {
	pageData
	Activities []activitySummaryView
	Search     string
}

type activitySummaryView struct {
	ID       string
	Title    string
	Summary  string
	Duration string
}

type progressionView struct {
	Position int
	Title    string
	Content  template.HTML
	Duration string
}

type activityDetailData struct {
	pageData
	ID           string
	Title        string
	Summary      string
	Content      template.HTML
	Duration     string
	Progressions []progressionView
}

func (h *handler) activityIndexRedirect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/activities", http.StatusSeeOther)
}

func (h *handler) listActivities(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if len(r.URL.Query()["q"]) > 1 {
		h.rejectInput(w, http.StatusBadRequest)
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := h.library.List(r.Context(), principal, search)
	if errors.Is(err, activity.ErrInvalidQuery) {
		h.rejectInput(w, http.StatusBadRequest)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "list activities", err)
		return
	}
	views := make([]activitySummaryView, len(items))
	for index, item := range items {
		views[index] = activitySummaryView{
			ID: item.ID, Title: item.Title, Summary: item.Summary,
			Duration: formatDuration(item.DurationSeconds),
		}
	}
	h.render(w, "activity-list", activityListData{
		pageData: h.page(r, "Activities · Curu"), Activities: views, Search: search,
	})
}

func (h *handler) activityDetail(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	item, err := h.library.Detail(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, activity.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "read activity", err)
		return
	}
	content, err := RenderMarkdown(item.ContentMarkdown)
	if err != nil {
		h.serverError(w, "render activity", err)
		return
	}
	progressions := make([]progressionView, len(item.Progressions))
	for index, progression := range item.Progressions {
		rendered, err := RenderMarkdown(progression.ContentMarkdown)
		if err != nil {
			h.serverError(w, "render activity progression", err)
			return
		}
		progressions[index] = progressionView{
			Position: progression.Position + 1, Title: progression.Title,
			Content: rendered, Duration: formatDuration(progression.DurationSeconds),
		}
	}
	h.render(w, "activity-detail", activityDetailData{
		pageData: h.page(r, item.Title+" · Curu"), ID: item.ID, Title: item.Title,
		Summary: item.Summary, Content: content, Duration: formatDuration(item.DurationSeconds),
		Progressions: progressions,
	})
}

func (h *handler) page(r *http.Request, title string) pageData {
	return pageData{PageTitle: title, CSRFToken: CSRFTokenFromRequest(r), LoginURL: h.browser.LoginURL, LogoutURL: h.browser.LogoutURL}
}

func (h *handler) render(w http.ResponseWriter, name string, data any) {
	var body bytes.Buffer
	if err := h.templates.ExecuteTemplate(&body, name, data); err != nil {
		h.serverError(w, "render response", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = body.WriteTo(w)
}

func (h *handler) serverError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error("request failed", "operation", operation, "error", err)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func formatDuration(seconds *int) string {
	if seconds == nil {
		return ""
	}
	if *seconds%60 == 0 {
		minutes := *seconds / 60
		unit := "minutes"
		if minutes == 1 {
			unit = "minute"
		}
		return strconv.Itoa(minutes) + " " + unit
	}
	return strconv.Itoa(*seconds) + " seconds"
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
	if status == http.StatusUnauthorized && r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Reswap", "none")
		w.Header().Set("HX-Trigger", htmxAuthEventName)
	}
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
	data := struct {
		pageData
		Path string
	}{pageData: h.page(r, "Not found · Curu"), Path: r.URL.Path}
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
