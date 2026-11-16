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
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gopher-launch/curu/internal/activity"
	"github.com/gopher-launch/curu/internal/auth"
	"github.com/gopher-launch/curu/internal/authorization"
	"github.com/gopher-launch/curu/internal/session"
)

//go:embed templates/*.html
var templateFiles embed.FS

type handler struct {
	logger    *slog.Logger
	ready     func() bool
	auth      requestAuthenticator
	policy    authorization.Policy
	library   activityLibrary
	sessions  sessionPlanner
	browser   BrowserConfig
	templates *template.Template
}

type requestAuthenticator interface {
	AuthenticateRequest(context.Context, []string) (auth.Principal, error)
}

type activityLibrary interface {
	List(context.Context, authorization.Principal, string) ([]activity.Summary, error)
	ListArchived(context.Context, authorization.Principal, string) ([]activity.Summary, error)
	Detail(context.Context, authorization.Principal, string) (activity.Activity, error)
	ArchivedDetail(context.Context, authorization.Principal, string) (activity.Activity, error)
	Create(context.Context, authorization.Principal, activity.Input) (activity.Activity, error)
	Update(context.Context, authorization.Principal, string, int64, activity.Input) (activity.Activity, error)
	Archive(context.Context, authorization.Principal, string, int64) (activity.Activity, error)
	Restore(context.Context, authorization.Principal, string, int64) (activity.Activity, error)
	ListRecoveryDrafts(context.Context, authorization.Principal) ([]activity.RecoveryDraftSummary, error)
	RecoveryDraft(context.Context, authorization.Principal, string) (activity.RecoveryDraft, error)
	SaveRecoveryDraft(context.Context, authorization.Principal, string, int64, activity.Input) (activity.RecoveryDraft, error)
	DiscardRecoveryDraft(context.Context, authorization.Principal, string) error
}

type sessionPlanner interface {
	List(context.Context, authorization.Principal, bool) ([]session.Summary, error)
	Detail(context.Context, authorization.Principal, string, bool) (session.Session, error)
	Create(context.Context, authorization.Principal, session.Input) (session.Session, error)
	Update(context.Context, authorization.Principal, string, int64, session.Input) (session.Session, error)
	Archive(context.Context, authorization.Principal, string, int64, bool) (session.Session, error)
	ApplyPlacementAction(context.Context, authorization.Principal, string, int64, session.Input, session.PlacementAction) (session.Session, error)
	SaveRecoveryDraft(context.Context, authorization.Principal, string, int64, session.Session, session.Input, session.PlacementAction) (session.RecoveryDraft, error)
	ListRecoveryDrafts(context.Context, authorization.Principal) ([]session.RecoveryDraftSummary, error)
	RecoveryDraft(context.Context, authorization.Principal, string) (session.RecoveryDraft, error)
	DiscardRecoveryDraft(context.Context, authorization.Principal, string) error
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

func NewHandler(logger *slog.Logger, ready func() bool, authenticator requestAuthenticator, policy authorization.Policy, library activityLibrary, sessions sessionPlanner, browser BrowserConfig) (http.Handler, error) {
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
	if sessions == nil {
		return nil, fmt.Errorf("session planner is required")
	}
	h := &handler{logger: logger, ready: ready, auth: authenticator, policy: policy, library: library, sessions: sessions, browser: browser, templates: templates}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", h.live)
	mux.HandleFunc("GET /health/ready", h.readiness)
	mux.HandleFunc("GET /{$}", h.activityIndexRedirect)
	mux.HandleFunc("GET /activities", h.listActivities)
	mux.HandleFunc("GET /activities/archived", h.listArchivedActivities)
	mux.HandleFunc("GET /activities/{id}/archived", h.archivedActivityDetail)
	mux.HandleFunc("POST /activities/{id}/archive", h.archiveActivity)
	mux.HandleFunc("POST /activities/{id}/restore", h.restoreActivity)
	mux.HandleFunc("GET /activities/new", h.newActivity)
	mux.HandleFunc("POST /activities", h.createActivity)
	mux.HandleFunc("GET /activities/{id}/edit", h.editActivity)
	mux.HandleFunc("POST /activities/{id}/recovery-draft", h.saveRecoveryDraft)
	mux.HandleFunc("POST /activities/{id}", h.updateActivity)
	mux.HandleFunc("GET /activities/{id}", h.activityDetail)
	mux.HandleFunc("GET /recovery-drafts", h.listRecoveryDrafts)
	mux.HandleFunc("GET /recovery-drafts/{id}", h.recoveryDraft)
	mux.HandleFunc("POST /recovery-drafts/{id}/discard", h.discardRecoveryDraft)
	mux.HandleFunc("GET /sessions", h.listSessions)
	mux.HandleFunc("GET /sessions/archived", h.listArchivedSessions)
	mux.HandleFunc("GET /sessions/new", h.newSession)
	mux.HandleFunc("POST /sessions", h.createSession)
	mux.HandleFunc("GET /sessions/{id}/edit", h.editSession)
	mux.HandleFunc("POST /sessions/{id}", h.updateSession)
	mux.HandleFunc("POST /sessions/{id}/placements", h.changeSessionPlacements)
	mux.HandleFunc("POST /sessions/{id}/recovery-draft", h.saveSessionRecoveryDraft)
	mux.HandleFunc("GET /session-recovery-drafts", h.listSessionRecoveryDrafts)
	mux.HandleFunc("GET /session-recovery-drafts/{id}", h.sessionRecoveryDraft)
	mux.HandleFunc("POST /session-recovery-drafts/{id}/discard", h.discardSessionRecoveryDraft)
	mux.HandleFunc("POST /sessions/{id}/archive", h.archiveSession)
	mux.HandleFunc("POST /sessions/{id}/restore", h.restoreSession)
	mux.HandleFunc("GET /sessions/{id}/archived", h.archivedSessionDetail)
	mux.HandleFunc("GET /sessions/{id}", h.sessionDetail)
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
	Archived   bool
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
	Phase        string
	Theme        string
	Tags         []string
	Progressions []progressionView
	Revision     int64
	Archived     bool
}

type progressionFormView struct {
	Title, ContentMarkdown, DurationSeconds string
}

type activityFormData struct {
	pageData
	Heading, Action, SaveLabel, Status, ErrorSummary string
	ID, Title, Summary, ContentMarkdown              string
	DurationSeconds, Phase, Theme, Tags, Revision    string
	Progressions                                     []progressionFormView
	Conflict, RecoveryDraft                          bool
	DraftAction, DraftID, SourceActivityID           string
}

type recoveryDraftListData struct {
	pageData
	Drafts []recoveryDraftSummaryView
}

type recoveryDraftSummaryView struct {
	ID, ActivityID, Title, Updated string
}

func (h *handler) activityIndexRedirect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/activities", http.StatusSeeOther)
}

func (h *handler) listActivities(w http.ResponseWriter, r *http.Request) {
	h.listActivityState(w, r, false)
}

func (h *handler) listArchivedActivities(w http.ResponseWriter, r *http.Request) {
	h.listActivityState(w, r, true)
}

func (h *handler) listActivityState(w http.ResponseWriter, r *http.Request, archived bool) {
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
	var items []activity.Summary
	var err error
	if archived {
		items, err = h.library.ListArchived(r.Context(), principal, search)
	} else {
		items, err = h.library.List(r.Context(), principal, search)
	}
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
	title := "Activities · Curu"
	if archived {
		title = "Archived activities · Curu"
	}
	h.render(w, "activity-list", activityListData{
		pageData: h.page(r, title), Activities: views, Search: search, Archived: archived,
	})
}

func (h *handler) activityDetail(w http.ResponseWriter, r *http.Request) {
	h.activityDetailState(w, r, false)
}

func (h *handler) archivedActivityDetail(w http.ResponseWriter, r *http.Request) {
	h.activityDetailState(w, r, true)
}

func (h *handler) activityDetailState(w http.ResponseWriter, r *http.Request, archived bool) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	var item activity.Activity
	var err error
	if archived {
		item, err = h.library.ArchivedDetail(r.Context(), principal, r.PathValue("id"))
	} else {
		item, err = h.library.Detail(r.Context(), principal, r.PathValue("id"))
	}
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
		Phase: item.Phase, Theme: item.Theme, Tags: item.Tags,
		Progressions: progressions, Revision: item.Revision, Archived: archived,
	})
}

func (h *handler) archiveActivity(w http.ResponseWriter, r *http.Request) {
	h.changeArchiveState(w, r, true)
}

func (h *handler) restoreActivity(w http.ResponseWriter, r *http.Request) {
	h.changeArchiveState(w, r, false)
}

func (h *handler) changeArchiveState(w http.ResponseWriter, r *http.Request, archive bool) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	revision, ok := h.parseActionRevision(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var item activity.Activity
	var err error
	if archive {
		item, err = h.library.Archive(r.Context(), principal, id, revision)
	} else {
		item, err = h.library.Restore(r.Context(), principal, id, revision)
	}
	if errors.Is(err, activity.ErrInvalidInput) {
		h.rejectInput(w, http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, activity.ErrConflict) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "The activity changed. Reload it before trying again.", http.StatusConflict)
		return
	}
	if errors.Is(err, activity.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "change activity archive state", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	target := "/activities/" + item.ID
	if archive {
		target = "/activities/" + item.ID + "/archived"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *handler) parseActionRevision(w http.ResponseWriter, r *http.Request) (int64, bool) {
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || len(parameters) != 0 {
		h.rejectInput(w, http.StatusUnsupportedMediaType)
		return 0, false
	}
	if err := r.ParseForm(); err != nil {
		h.rejectInput(w, http.StatusBadRequest)
		return 0, false
	}
	values := r.PostForm["revision"]
	if len(values) != 1 {
		h.rejectInput(w, http.StatusUnprocessableEntity)
		return 0, false
	}
	revision, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || revision < 1 {
		h.rejectInput(w, http.StatusUnprocessableEntity)
		return 0, false
	}
	return revision, true
}

func (h *handler) newActivity(w http.ResponseWriter, r *http.Request) {
	h.render(w, "activity-form", activityFormData{pageData: h.page(r, "New activity · Curu"), Heading: "New activity", Action: "/activities", SaveLabel: "Create activity", Progressions: []progressionFormView{{}}})
}

func (h *handler) editActivity(w http.ResponseWriter, r *http.Request) {
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
		h.serverError(w, "read activity editor", err)
		return
	}
	data := formDataFromActivity(h.page(r, "Edit "+item.Title+" · Curu"), item)
	if r.URL.Query().Get("saved") == "1" {
		data.Status = "Saved."
	}
	h.render(w, "activity-form", data)
}

func (h *handler) createActivity(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	input, data, ok := h.parseActivityForm(w, r, "New activity", "/activities", "Create activity")
	if !ok {
		return
	}
	item, err := h.library.Create(r.Context(), principal, input)
	if errors.Is(err, activity.ErrInvalidInput) {
		data.ErrorSummary = "Fix the invalid values and save again."
		h.renderStatus(w, "activity-form", data, http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "create activity", err)
		return
	}
	h.redirectSaved(w, r, item.ID)
}

func (h *handler) updateActivity(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	id := r.PathValue("id")
	input, data, ok := h.parseActivityForm(w, r, "Edit activity", "/activities/"+id, "Save changes")
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(data.Revision, 10, 64)
	if err != nil || revision < 1 {
		data.ErrorSummary = "This editor has an invalid revision. Reload the activity before saving."
		h.renderStatus(w, "activity-form", data, http.StatusUnprocessableEntity)
		return
	}
	item, err := h.library.Update(r.Context(), principal, id, revision, input)
	if errors.Is(err, activity.ErrInvalidInput) {
		data.ErrorSummary = "Fix the invalid values and save again."
		h.renderStatus(w, "activity-form", data, http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, activity.ErrConflict) {
		data.ErrorSummary = "A newer revision exists. Your submitted values remain below. Save them as a separate recovery draft before deliberately opening the latest activity."
		data.Conflict = true
		data.DraftAction = "/activities/" + id + "/recovery-draft"
		data.SourceActivityID = id
		h.renderStatus(w, "activity-form", data, http.StatusConflict)
		return
	}
	if errors.Is(err, activity.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "update activity", err)
		return
	}
	h.redirectSaved(w, r, item.ID)
}

func (h *handler) saveRecoveryDraft(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	id := r.PathValue("id")
	input, data, ok := h.parseActivityForm(w, r, "Recovery draft", "/activities/"+id+"/recovery-draft", "Save recovery draft")
	if !ok {
		return
	}
	data.RecoveryDraft, data.SourceActivityID = true, id
	revision, err := strconv.ParseInt(data.Revision, 10, 64)
	if err != nil || revision < 1 {
		data.ErrorSummary = "This recovery draft has an invalid source revision. Return to the open conflicting editor."
		h.renderStatus(w, "activity-form", data, http.StatusUnprocessableEntity)
		return
	}
	draft, err := h.library.SaveRecoveryDraft(r.Context(), principal, id, revision, input)
	if errors.Is(err, activity.ErrInvalidInput) {
		data.ErrorSummary = "Fix the invalid values and save the recovery draft again."
		h.renderStatus(w, "activity-form", data, http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, activity.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.logger.Error("request failed", "operation", "save recovery draft", "error", err)
		data.ErrorSummary = "The recovery draft was not saved. Your submitted values remain below; retry deliberately."
		h.renderStatus(w, "activity-form", data, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/recovery-drafts/"+draft.ID+"?saved=1", http.StatusSeeOther)
}

func (h *handler) listRecoveryDrafts(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	items, err := h.library.ListRecoveryDrafts(r.Context(), principal)
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "list recovery drafts", err)
		return
	}
	views := make([]recoveryDraftSummaryView, len(items))
	for index, item := range items {
		views[index] = recoveryDraftSummaryView{ID: item.ID, ActivityID: item.ActivityID, Title: item.Title, Updated: item.UpdatedAt.UTC().Format("2006-01-02 15:04 UTC")}
	}
	h.render(w, "recovery-draft-list", recoveryDraftListData{pageData: h.page(r, "Recovery drafts · Curu"), Drafts: views})
}

func (h *handler) recoveryDraft(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	draft, err := h.library.RecoveryDraft(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, activity.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "read recovery draft", err)
		return
	}
	data := formDataFromInput(h.page(r, "Recovery draft · Curu"), draft.Input)
	data.Heading, data.Action, data.SaveLabel = "Recovery draft", "/activities/"+draft.ActivityID+"/recovery-draft", "Save recovery draft"
	data.Revision = strconv.FormatInt(draft.BaseRevision, 10)
	data.RecoveryDraft, data.DraftID, data.SourceActivityID = true, draft.ID, draft.ActivityID
	if r.URL.Query().Get("saved") == "1" {
		data.Status = "Recovery draft saved. The current activity was not changed."
	}
	h.render(w, "activity-form", data)
}

func (h *handler) discardRecoveryDraft(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || len(parameters) != 0 {
		h.rejectInput(w, http.StatusUnsupportedMediaType)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.rejectInput(w, http.StatusBadRequest)
		return
	}
	err = h.library.DiscardRecoveryDraft(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, activity.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "discard recovery draft", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/recovery-drafts", http.StatusSeeOther)
}

func (h *handler) parseActivityForm(w http.ResponseWriter, r *http.Request, heading, action, label string) (activity.Input, activityFormData, bool) {
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || len(parameters) != 0 {
		h.rejectInput(w, http.StatusUnsupportedMediaType)
		return activity.Input{}, activityFormData{}, false
	}
	if err := r.ParseForm(); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			h.rejectInput(w, http.StatusRequestEntityTooLarge)
			return activity.Input{}, activityFormData{}, false
		}
		h.rejectInput(w, http.StatusBadRequest)
		return activity.Input{}, activityFormData{}, false
	}
	data := activityFormData{pageData: h.page(r, heading+" · Curu"), Heading: heading, Action: action, SaveLabel: label,
		Title: r.PostForm.Get("title"), Summary: r.PostForm.Get("summary"), ContentMarkdown: r.PostForm.Get("content_markdown"),
		DurationSeconds: r.PostForm.Get("duration_seconds"), Phase: r.PostForm.Get("phase"), Theme: r.PostForm.Get("theme"), Tags: r.PostForm.Get("tags"), Revision: r.PostForm.Get("revision")}
	titles, contents, durations := r.PostForm["progression_title"], r.PostForm["progression_content_markdown"], r.PostForm["progression_duration_seconds"]
	if len(titles) != len(contents) || len(titles) != len(durations) || len(titles) > activity.MaxProgressions {
		h.rejectInput(w, http.StatusBadRequest)
		return activity.Input{}, data, false
	}
	input := activity.Input{Title: data.Title, Summary: data.Summary, ContentMarkdown: data.ContentMarkdown, Phase: data.Phase, Theme: data.Theme}
	if strings.TrimSpace(data.Tags) != "" {
		input.Tags = strings.Split(data.Tags, ",")
	}
	input.DurationSeconds, err = parseOptionalSeconds(data.DurationSeconds)
	if err != nil {
		data.ErrorSummary = "Duration must be whole seconds, or blank."
	}
	for index := range titles {
		view := progressionFormView{Title: titles[index], ContentMarkdown: contents[index], DurationSeconds: durations[index]}
		data.Progressions = append(data.Progressions, view)
		if strings.TrimSpace(view.Title) == "" && strings.TrimSpace(view.ContentMarkdown) == "" && strings.TrimSpace(view.DurationSeconds) == "" {
			continue
		}
		duration, durationErr := parseOptionalSeconds(view.DurationSeconds)
		if durationErr != nil {
			data.ErrorSummary = "Progression durations must be whole seconds, or blank."
		}
		input.Progressions = append(input.Progressions, activity.ProgressionInput{Title: view.Title, ContentMarkdown: view.ContentMarkdown, DurationSeconds: duration})
	}
	if len(data.Progressions) == 0 {
		data.Progressions = []progressionFormView{{}}
	}
	if data.ErrorSummary != "" {
		h.renderStatus(w, "activity-form", data, http.StatusUnprocessableEntity)
		return activity.Input{}, data, false
	}
	return input, data, true
}

func parseOptionalSeconds(value string) (*int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return nil, err
	}
	return &seconds, nil
}

func formDataFromActivity(page pageData, item activity.Activity) activityFormData {
	input := activity.Input{Title: item.Title, Summary: item.Summary, ContentMarkdown: item.ContentMarkdown, DurationSeconds: item.DurationSeconds, Phase: item.Phase, Theme: item.Theme, Tags: item.Tags}
	for _, progression := range item.Progressions {
		input.Progressions = append(input.Progressions, activity.ProgressionInput{Title: progression.Title, ContentMarkdown: progression.ContentMarkdown, DurationSeconds: progression.DurationSeconds})
	}
	data := formDataFromInput(page, input)
	data.Heading, data.Action, data.SaveLabel, data.ID = "Edit activity", "/activities/"+item.ID, "Save changes", item.ID
	data.Revision = strconv.FormatInt(item.Revision, 10)
	return data
}

func formDataFromInput(page pageData, input activity.Input) activityFormData {
	data := activityFormData{pageData: page, Title: input.Title, Summary: input.Summary, ContentMarkdown: input.ContentMarkdown, Phase: input.Phase, Theme: input.Theme, Tags: strings.Join(input.Tags, ", ")}
	if input.DurationSeconds != nil {
		data.DurationSeconds = strconv.Itoa(*input.DurationSeconds)
	}
	for _, progression := range input.Progressions {
		view := progressionFormView{Title: progression.Title, ContentMarkdown: progression.ContentMarkdown}
		if progression.DurationSeconds != nil {
			view.DurationSeconds = strconv.Itoa(*progression.DurationSeconds)
		}
		data.Progressions = append(data.Progressions, view)
	}
	if len(data.Progressions) == 0 {
		data.Progressions = []progressionFormView{{}}
	}
	return data
}

func (h *handler) redirectSaved(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/activities/"+id+"/edit?saved=1", http.StatusSeeOther)
}

func (h *handler) renderStatus(w http.ResponseWriter, name string, data any, status int) {
	var body bytes.Buffer
	if err := h.templates.ExecuteTemplate(&body, name, data); err != nil {
		h.serverError(w, "render response", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = body.WriteTo(w)
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
	if r.Header.Get("HX-Request") == "true" &&
		(status == http.StatusUnauthorized || (!isSafeMethod(r.Method) && reason == "csrf_rejected")) {
		w.Header().Set("HX-Reswap", "none")
		if status == http.StatusUnauthorized {
			w.Header().Set("HX-Trigger", htmxAuthEventName)
		} else {
			w.Header().Set("HX-Trigger", htmxRejectedEventName)
		}
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
