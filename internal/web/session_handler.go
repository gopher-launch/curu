package web

import (
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gopher-launch/curu/internal/authorization"
	"github.com/gopher-launch/curu/internal/session"
)

type sessionListData struct {
	pageData
	Sessions []sessionSummaryView
	Archived bool
}
type sessionSummaryView struct{ ID, Title, Date string }
type sessionBlockView struct {
	Position                             int
	Name, Intent, Notes, ObservationLens string
}
type sessionDetailData struct {
	pageData
	ID, Title, Date, GroupContext, Intent string
	Revision                              int64
	Archived                              bool
	Blocks                                []sessionBlockView
}
type sessionBlockFormView struct{ Name, Intent, Notes, ObservationLens string }
type sessionFormData struct {
	pageData
	Heading, Action, SaveLabel, Status, ErrorSummary, ID, Title, Date, GroupContext, Intent, Revision string
	Conflict                                                                                          bool
	CurrentURL                                                                                        string
	Blocks                                                                                            []sessionBlockFormView
}

func (h *handler) listSessions(w http.ResponseWriter, r *http.Request) {
	h.listSessionState(w, r, false)
}
func (h *handler) listArchivedSessions(w http.ResponseWriter, r *http.Request) {
	h.listSessionState(w, r, true)
}
func (h *handler) listSessionState(w http.ResponseWriter, r *http.Request, archived bool) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	items, err := h.sessions.List(r.Context(), principal, archived)
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "list sessions", err)
		return
	}
	views := make([]sessionSummaryView, len(items))
	for i, item := range items {
		views[i] = sessionSummaryView{ID: item.ID, Title: item.Title}
		if item.Date != nil {
			views[i].Date = item.Date.Format("2006-01-02")
		}
	}
	title := "Sessions · Curu"
	if archived {
		title = "Archived sessions · Curu"
	}
	h.render(w, "session-list", sessionListData{pageData: h.page(r, title), Sessions: views, Archived: archived})
}
func (h *handler) sessionDetail(w http.ResponseWriter, r *http.Request) {
	h.sessionDetailState(w, r, false)
}
func (h *handler) archivedSessionDetail(w http.ResponseWriter, r *http.Request) {
	h.sessionDetailState(w, r, true)
}
func (h *handler) sessionDetailState(w http.ResponseWriter, r *http.Request, archived bool) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	item, err := h.sessions.Detail(r.Context(), principal, r.PathValue("id"), archived)
	if errors.Is(err, session.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "read session", err)
		return
	}
	data := sessionDetailData{pageData: h.page(r, item.Title+" · Curu"), ID: item.ID, Title: item.Title, GroupContext: item.GroupContext, Intent: item.Intent, Revision: item.Revision, Archived: archived, Blocks: make([]sessionBlockView, len(item.Blocks))}
	if item.Date != nil {
		data.Date = item.Date.Format("2006-01-02")
	}
	for i, block := range item.Blocks {
		data.Blocks[i] = sessionBlockView{Position: i + 1, Name: block.Name, Intent: block.Intent, Notes: block.Notes, ObservationLens: block.ObservationLens}
	}
	h.render(w, "session-detail", data)
}
func (h *handler) newSession(w http.ResponseWriter, r *http.Request) {
	h.render(w, "session-form", sessionFormData{pageData: h.page(r, "New session · Curu"), Heading: "New session", Action: "/sessions", SaveLabel: "Create session"})
}
func (h *handler) editSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	item, err := h.sessions.Detail(r.Context(), principal, r.PathValue("id"), false)
	if errors.Is(err, session.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "read session editor", err)
		return
	}
	data := sessionFormFromItem(h.page(r, "Edit "+item.Title+" · Curu"), item)
	if r.URL.Query().Get("saved") == "1" {
		data.Status = "Saved."
	}
	h.render(w, "session-form", data)
}
func (h *handler) createSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	input, data, ok := h.parseSessionForm(w, r, "New session", "/sessions", "Create session")
	if !ok {
		return
	}
	item, err := h.sessions.Create(r.Context(), principal, input)
	if errors.Is(err, session.ErrInvalidInput) {
		data.ErrorSummary = "Fix the invalid values and save again."
		h.renderStatus(w, "session-form", data, http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "create session", err)
		return
	}
	h.redirectSessionSaved(w, r, item.ID)
}
func (h *handler) updateSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	id := r.PathValue("id")
	input, data, ok := h.parseSessionForm(w, r, "Edit session", "/sessions/"+id, "Save changes")
	if !ok {
		return
	}
	data.ID = id
	revision, err := strconv.ParseInt(data.Revision, 10, 64)
	if err != nil || revision < 1 {
		data.ErrorSummary = "This editor has an invalid revision. Reload the session before saving."
		h.renderStatus(w, "session-form", data, http.StatusUnprocessableEntity)
		return
	}
	item, err := h.sessions.Update(r.Context(), principal, id, revision, input)
	if errors.Is(err, session.ErrInvalidInput) {
		data.ErrorSummary = "Fix the invalid values and save again."
		h.renderStatus(w, "session-form", data, http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, session.ErrArchivedConflict) {
		data.ErrorSummary = "This session was archived after you opened the editor. Your submitted session and blocks remain below. Open the archived session in a new tab before deciding what to copy."
		data.Conflict = true
		data.CurrentURL = "/sessions/" + id + "/archived"
		h.renderStatus(w, "session-form", data, http.StatusConflict)
		return
	}
	if errors.Is(err, session.ErrConflict) {
		data.ErrorSummary = "A newer revision exists. Your submitted session and blocks remain below. Open the latest session in a new tab before deciding what to copy."
		data.Conflict = true
		data.CurrentURL = "/sessions/" + id + "/edit"
		h.renderStatus(w, "session-form", data, http.StatusConflict)
		return
	}
	if errors.Is(err, session.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "update session", err)
		return
	}
	h.redirectSessionSaved(w, r, item.ID)
}
func (h *handler) archiveSession(w http.ResponseWriter, r *http.Request) {
	h.changeSessionArchiveState(w, r, true)
}
func (h *handler) restoreSession(w http.ResponseWriter, r *http.Request) {
	h.changeSessionArchiveState(w, r, false)
}
func (h *handler) changeSessionArchiveState(w http.ResponseWriter, r *http.Request, archived bool) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	revision, ok := h.parseActionRevision(w, r)
	if !ok {
		return
	}
	item, err := h.sessions.Archive(r.Context(), principal, r.PathValue("id"), revision, archived)
	if errors.Is(err, session.ErrConflict) {
		http.Error(w, "The session changed. Reload it before trying again.", http.StatusConflict)
		return
	}
	if errors.Is(err, session.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "change session archive state", err)
		return
	}
	target := "/sessions/" + item.ID
	if archived {
		target += "/archived"
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusSeeOther)
}
func (h *handler) parseSessionForm(w http.ResponseWriter, r *http.Request, heading, action, label string) (session.Input, sessionFormData, bool) {
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || len(parameters) != 0 {
		h.rejectInput(w, http.StatusUnsupportedMediaType)
		return session.Input{}, sessionFormData{}, false
	}
	if err := r.ParseForm(); err != nil {
		h.rejectInput(w, http.StatusBadRequest)
		return session.Input{}, sessionFormData{}, false
	}
	data := sessionFormData{pageData: h.page(r, heading+" · Curu"), Heading: heading, Action: action, SaveLabel: label, Title: r.PostForm.Get("title"), Date: r.PostForm.Get("date"), GroupContext: r.PostForm.Get("group_context"), Intent: r.PostForm.Get("intent"), Revision: r.PostForm.Get("revision")}
	names, intents, notes, lenses := r.PostForm["block_name"], r.PostForm["block_intent"], r.PostForm["block_notes"], r.PostForm["block_observation_lens"]
	if len(names) != len(intents) || len(names) != len(notes) || len(names) != len(lenses) || len(names) > session.MaxBlocks {
		h.rejectInput(w, http.StatusBadRequest)
		return session.Input{}, data, false
	}
	input := session.Input{Title: data.Title, GroupContext: data.GroupContext, Intent: data.Intent}
	if strings.TrimSpace(data.Date) != "" {
		parsed, dateErr := time.Parse("2006-01-02", data.Date)
		if dateErr != nil {
			data.ErrorSummary = "Date must be a valid calendar date, or blank."
		} else {
			input.Date = &parsed
		}
	}
	for i := range names {
		view := sessionBlockFormView{Name: names[i], Intent: intents[i], Notes: notes[i], ObservationLens: lenses[i]}
		data.Blocks = append(data.Blocks, view)
		input.Blocks = append(input.Blocks, session.BlockInput{Name: view.Name, Intent: view.Intent, Notes: view.Notes, ObservationLens: view.ObservationLens})
	}
	if data.ErrorSummary != "" {
		h.renderStatus(w, "session-form", data, http.StatusUnprocessableEntity)
		return session.Input{}, data, false
	}
	return input, data, true
}
func sessionFormFromItem(page pageData, item session.Session) sessionFormData {
	data := sessionFormData{pageData: page, Heading: "Edit session", Action: "/sessions/" + item.ID, SaveLabel: "Save changes", ID: item.ID, Title: item.Title, GroupContext: item.GroupContext, Intent: item.Intent, Revision: strconv.FormatInt(item.Revision, 10)}
	if item.Date != nil {
		data.Date = item.Date.Format("2006-01-02")
	}
	for _, b := range item.Blocks {
		data.Blocks = append(data.Blocks, sessionBlockFormView{Name: b.Name, Intent: b.Intent, Notes: b.Notes, ObservationLens: b.ObservationLens})
	}
	return data
}
func (h *handler) redirectSessionSaved(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/sessions/"+id+"/edit?saved=1", http.StatusSeeOther)
}
