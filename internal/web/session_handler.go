package web

import (
	"context"
	"errors"
	"html/template"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gopher-launch/curu/internal/activity"
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
	Allocation, Timing                   string
	TimingComplete                       bool
	TimingWarnings                       []string
	Placements                           []sessionPlacementView
}
type sessionPlacementView struct {
	Position                                                  int
	Title, Summary, RecommendedDuration, Duration, LocalNotes string
	Phase, Theme                                              string
	Content                                                   template.HTML
	Tags                                                      []string
	Progressions                                              []progressionView
}
type sessionDetailData struct {
	pageData
	ID, Title, Date, GroupContext, Intent string
	Allocation, Timing                    string
	TimingComplete                        bool
	TimingWarnings                        []string
	Revision                              int64
	Archived                              bool
	Blocks                                []sessionBlockView
}
type sessionPlacementFormView struct {
	ID, Title, LocalNotes, Duration string
	CanMoveUp, CanMoveDown          bool
}
type sessionBlockFormView struct {
	ID, Name, Intent, Notes, ObservationLens string
	Allocation, Timing                       string
	TimingComplete                           bool
	TimingWarnings                           []string
	Placements                               []sessionPlacementFormView
	CanMoveUp, CanMoveDown                   bool
}
type sessionFormData struct {
	pageData
	Heading, Action, SaveLabel, Status, ErrorSummary, ID, Title, Date, GroupContext, Intent, Revision string
	Allocation, Timing                                                                                string
	TimingComplete                                                                                    bool
	TimingWarnings                                                                                    []string
	Conflict                                                                                          bool
	CurrentURL                                                                                        string
	Blocks                                                                                            []sessionBlockFormView
	Activities                                                                                        []activitySummaryView
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
	timing := session.SummarizeTiming(item)
	data := sessionDetailData{pageData: h.page(r, item.Title+" · Curu"), ID: item.ID, Title: item.Title, GroupContext: item.GroupContext, Intent: item.Intent, Allocation: formatDuration(item.AllocationSeconds), Revision: item.Revision, Archived: archived, Timing: formatTimingTotal(timing), TimingComplete: timing.Complete, TimingWarnings: timing.Warnings, Blocks: make([]sessionBlockView, len(item.Blocks))}
	if item.Date != nil {
		data.Date = item.Date.Format("2006-01-02")
	}
	for i, block := range item.Blocks {
		blockTiming := timing.Blocks[i]
		view := sessionBlockView{Position: i + 1, Name: block.Name, Intent: block.Intent, Notes: block.Notes, ObservationLens: block.ObservationLens, Allocation: formatDuration(block.AllocationSeconds), Timing: formatBlockTiming(blockTiming), TimingComplete: blockTiming.Complete, TimingWarnings: blockTiming.Warnings, Placements: make([]sessionPlacementView, len(block.Placements))}
		for j, placement := range block.Placements {
			content, renderErr := RenderMarkdown(placement.ContentMarkdown)
			if renderErr != nil {
				h.serverError(w, "render placement snapshot", renderErr)
				return
			}
			placementView := sessionPlacementView{Position: j + 1, Title: placement.Title, Summary: placement.Summary, Content: content, RecommendedDuration: formatDuration(placement.RecommendedDurationSeconds), Duration: formatDuration(placement.DurationSeconds), LocalNotes: placement.LocalNotes, Phase: placement.Phase, Theme: placement.Theme, Tags: placement.Tags, Progressions: make([]progressionView, len(placement.Progressions))}
			for k, progression := range placement.Progressions {
				rendered, renderErr := RenderMarkdown(progression.ContentMarkdown)
				if renderErr != nil {
					h.serverError(w, "render placement progression snapshot", renderErr)
					return
				}
				placementView.Progressions[k] = progressionView{Position: k + 1, Title: progression.Title, Content: rendered, Duration: formatDuration(progression.RecommendedDurationSeconds)}
			}
			view.Placements[j] = placementView
		}
		data.Blocks[i] = view
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
	activities, err := h.library.List(r.Context(), principal, "")
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "list activities for session editor", err)
		return
	}
	data.Activities = activitySummaryViews(activities)
	if r.URL.Query().Get("saved") == "1" {
		data.Status = "Saved."
	}
	switch r.URL.Query().Get("placement") {
	case "added":
		data.Status = "Activity added and session changes saved."
	case "replaced":
		data.Status = "Activity content replaced; retained notes may need review."
	case "moved":
		data.Status = "Activity moved and session changes saved."
	case "removed":
		data.Status = "Activity removed from this plan; the library activity is unchanged."
	case "reordered":
		data.Status = "Session order changed and entered fields saved."
	}
	if data.Status != "" && len(data.TimingWarnings) > 0 {
		data.Status = strings.TrimSpace(data.Status + " Saved with timing warnings; review the non-blocking feedback below.")
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

func (h *handler) changeSessionPlacements(w http.ResponseWriter, r *http.Request) {
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
	activities, listErr := h.sessionEditorActivities(r.Context(), principal)
	if listErr != nil {
		h.serverError(w, "list activities for submitted session editor", listErr)
		return
	}
	data.Activities = activities
	revision, err := strconv.ParseInt(data.Revision, 10, 64)
	if err != nil || revision < 1 {
		data.ErrorSummary = "This editor has an invalid revision. Reload the session before changing activities."
		h.renderStatus(w, "session-form", data, http.StatusUnprocessableEntity)
		return
	}
	action := session.PlacementAction{
		Kind:          session.PlacementActionKind(r.PostForm.Get("placement_action")),
		PlacementID:   r.PostForm.Get("placement_id"),
		TargetBlockID: r.PostForm.Get("target_block_id"),
		ActivityID:    r.PostForm.Get("activity_id"),
	}
	reorderFields := []struct {
		field string
		kind  session.PlacementActionKind
	}{{"move_block_up", session.MoveBlockUpAction}, {"move_block_down", session.MoveBlockDownAction}, {"move_placement_up", session.MovePlacementUpAction}, {"move_placement_down", session.MovePlacementDownAction}}
	actionKindSet := action.Kind != ""
	for _, candidate := range reorderFields {
		field, kind := candidate.field, candidate.kind
		if id := r.PostForm.Get(field); id != "" {
			if actionKindSet {
				action.Kind = "multiple"
				continue
			}
			actionKindSet = true
			action.Kind = kind
			if kind == session.MoveBlockUpAction || kind == session.MoveBlockDownAction {
				action.BlockID = id
			} else {
				action.PlacementID = id
			}
		}
	}
	item, err := h.sessions.ApplyPlacementAction(r.Context(), principal, id, revision, input, action)
	if errors.Is(err, session.ErrInvalidInput) {
		data.ErrorSummary = "Choose a valid action for this saved block or activity, then try again. Your entered changes remain below."
		h.renderStatus(w, "session-form", data, http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, session.ErrArchivedConflict) {
		data.ErrorSummary = "This session was archived after you opened the editor. Your entered session, blocks, and activity notes remain below."
		data.Conflict, data.CurrentURL = true, "/sessions/"+id+"/archived"
		h.renderStatus(w, "session-form", data, http.StatusConflict)
		return
	}
	if errors.Is(err, session.ErrConflict) {
		data.ErrorSummary = "A newer revision exists. Your entered session, blocks, and activity notes remain below; no structural action was applied."
		data.Conflict, data.CurrentURL = true, "/sessions/"+id+"/edit"
		h.renderStatus(w, "session-form", data, http.StatusConflict)
		return
	}
	if errors.Is(err, session.ErrNotFound) || errors.Is(err, activity.ErrNotFound) {
		data.ErrorSummary = "The selected activity or plan item is no longer available. Your entered changes remain below."
		h.renderStatus(w, "session-form", data, http.StatusConflict)
		return
	}
	if errors.Is(err, authorization.ErrDenied) {
		h.deny(w, r, http.StatusForbidden, "authorization_denied")
		return
	}
	if err != nil {
		h.serverError(w, "change session placements", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	result := map[session.PlacementActionKind]string{
		session.AddPlacementAction: "added", session.ReplacePlacementAction: "replaced",
		session.MovePlacementAction: "moved", session.RemovePlacementAction: "removed",
		session.MoveBlockUpAction: "reordered", session.MoveBlockDownAction: "reordered",
		session.MovePlacementUpAction: "reordered", session.MovePlacementDownAction: "reordered",
	}[action.Kind]
	target := "/sessions/" + item.ID + "/edit?placement=" + result
	if action.Kind == session.MoveBlockUpAction || action.Kind == session.MoveBlockDownAction {
		target += "#item-" + action.BlockID
	}
	if action.Kind == session.MovePlacementUpAction || action.Kind == session.MovePlacementDownAction {
		target += "#item-" + action.PlacementID
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *handler) sessionEditorActivities(ctx context.Context, principal authorization.Principal) ([]activitySummaryView, error) {
	items, err := h.library.List(ctx, principal, "")
	if err != nil {
		return nil, err
	}
	return activitySummaryViews(items), nil
}

func activitySummaryViews(items []activity.Summary) []activitySummaryView {
	views := make([]activitySummaryView, len(items))
	for i, item := range items {
		views[i] = activitySummaryView{ID: item.ID, Title: item.Title, Summary: item.Summary, Duration: formatDuration(item.DurationSeconds)}
	}
	return views
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
	data := sessionFormData{pageData: h.page(r, heading+" · Curu"), Heading: heading, Action: action, SaveLabel: label, Title: r.PostForm.Get("title"), Date: r.PostForm.Get("date"), GroupContext: r.PostForm.Get("group_context"), Intent: r.PostForm.Get("intent"), Revision: r.PostForm.Get("revision"), Allocation: r.PostForm.Get("allocation_seconds")}
	ids, names, intents, notes, lenses, allocations := r.PostForm["block_id"], r.PostForm["block_name"], r.PostForm["block_intent"], r.PostForm["block_notes"], r.PostForm["block_observation_lens"], r.PostForm["block_allocation_seconds"]
	// Empty IDs are valid for newly added blocks, but an edit must submit one ID
	// field per block. Otherwise an old or malformed editor could replace stable
	// blocks and cascade their placement snapshots.
	if len(ids) == 0 && len(names) > 0 && data.Revision == "" {
		ids = make([]string, len(names))
	}
	if len(allocations) == 0 && len(names) > 0 {
		allocations = make([]string, len(names))
	}
	if len(ids) != len(names) || len(names) != len(intents) || len(names) != len(notes) || len(names) != len(lenses) || len(names) != len(allocations) || len(names) > session.MaxBlocks {
		h.rejectInput(w, http.StatusBadRequest)
		return session.Input{}, data, false
	}
	input := session.Input{Title: data.Title, GroupContext: data.GroupContext, Intent: data.Intent}
	input.AllocationSeconds = parseOptionalTiming(data.Allocation, "Session allocation", &data.ErrorSummary)
	if strings.TrimSpace(data.Date) != "" {
		parsed, dateErr := time.Parse("2006-01-02", data.Date)
		if dateErr != nil {
			data.ErrorSummary = "Date must be a valid calendar date, or blank."
		} else {
			input.Date = &parsed
		}
	}
	for i := range names {
		view := sessionBlockFormView{ID: ids[i], Name: names[i], Intent: intents[i], Notes: notes[i], ObservationLens: lenses[i], Allocation: allocations[i]}
		placementIDs := r.PostForm["placement_id_"+view.ID]
		placementNotes := r.PostForm["placement_notes_"+view.ID]
		placementDurations := r.PostForm["placement_duration_seconds_"+view.ID]
		if len(placementDurations) == 0 && len(placementIDs) > 0 {
			placementDurations = make([]string, len(placementIDs))
		}
		if len(placementIDs) != len(placementNotes) || len(placementIDs) != len(placementDurations) || len(placementIDs) > session.MaxPlacementsPerBlock {
			h.rejectInput(w, http.StatusBadRequest)
			return session.Input{}, data, false
		}
		blockInput := session.BlockInput{ID: view.ID, Name: view.Name, Intent: view.Intent, Notes: view.Notes, ObservationLens: view.ObservationLens}
		blockInput.AllocationSeconds = parseOptionalTiming(view.Allocation, "Block allocation", &data.ErrorSummary)
		for j := range placementIDs {
			placementView := sessionPlacementFormView{ID: placementIDs[j], LocalNotes: placementNotes[j], Duration: placementDurations[j]}
			duration := parseOptionalTiming(placementView.Duration, "Activity duration", &data.ErrorSummary)
			view.Placements = append(view.Placements, placementView)
			blockInput.Placements = append(blockInput.Placements, session.PlacementInput{ID: placementView.ID, LocalNotes: placementView.LocalNotes, DurationSeconds: duration})
		}
		data.Blocks = append(data.Blocks, view)
		input.Blocks = append(input.Blocks, blockInput)
	}
	applyTimingFormSummary(&data, input)
	annotateSessionFormOrder(data.Blocks)
	if data.ErrorSummary != "" {
		h.renderStatus(w, "session-form", data, http.StatusUnprocessableEntity)
		return session.Input{}, data, false
	}
	return input, data, true
}
func sessionFormFromItem(page pageData, item session.Session) sessionFormData {
	data := sessionFormData{pageData: page, Heading: "Edit session", Action: "/sessions/" + item.ID, SaveLabel: "Save changes", ID: item.ID, Title: item.Title, GroupContext: item.GroupContext, Intent: item.Intent, Revision: strconv.FormatInt(item.Revision, 10), Allocation: timingInput(item.AllocationSeconds)}
	if item.Date != nil {
		data.Date = item.Date.Format("2006-01-02")
	}
	for _, b := range item.Blocks {
		view := sessionBlockFormView{ID: b.ID, Name: b.Name, Intent: b.Intent, Notes: b.Notes, ObservationLens: b.ObservationLens, Allocation: timingInput(b.AllocationSeconds)}
		for _, placement := range b.Placements {
			view.Placements = append(view.Placements, sessionPlacementFormView{ID: placement.ID, Title: placement.Title, LocalNotes: placement.LocalNotes, Duration: timingInput(placement.DurationSeconds)})
		}
		data.Blocks = append(data.Blocks, view)
	}
	applyTimingFormItemSummary(&data, item)
	annotateSessionFormOrder(data.Blocks)
	return data
}

func parseOptionalTiming(raw, label string, errorSummary *string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || value > session.MaxTimingSeconds {
		if *errorSummary == "" {
			*errorSummary = label + " must be whole seconds from 0 through 86400, or blank."
		}
		return nil
	}
	return &value
}

func timingInput(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func formatTimingTotal(summary session.TimingSummary) string {
	if !summary.HasTiming {
		return ""
	}
	return formatDuration(&summary.TotalSeconds)
}

func formatBlockTiming(summary session.BlockTiming) string {
	if !summary.HasValue {
		return ""
	}
	return formatDuration(&summary.Seconds)
}

func applyTimingFormItemSummary(data *sessionFormData, item session.Session) {
	timing := session.SummarizeTiming(item)
	data.Timing, data.TimingComplete, data.TimingWarnings = formatTimingTotal(timing), timing.Complete, timing.Warnings
	for i := range data.Blocks {
		data.Blocks[i].Timing = formatBlockTiming(timing.Blocks[i])
		data.Blocks[i].TimingComplete = timing.Blocks[i].Complete
		data.Blocks[i].TimingWarnings = timing.Blocks[i].Warnings
	}
}

func applyTimingFormSummary(data *sessionFormData, input session.Input) {
	item := session.Session{AllocationSeconds: input.AllocationSeconds, Blocks: make([]session.Block, len(input.Blocks))}
	for i, block := range input.Blocks {
		item.Blocks[i].AllocationSeconds = block.AllocationSeconds
		for _, placement := range block.Placements {
			item.Blocks[i].Placements = append(item.Blocks[i].Placements, session.Placement{DurationSeconds: placement.DurationSeconds})
		}
	}
	applyTimingFormItemSummary(data, item)
}

func annotateSessionFormOrder(blocks []sessionBlockFormView) {
	for i := range blocks {
		blocks[i].CanMoveUp = blocks[i].ID != "" && i > 0
		blocks[i].CanMoveDown = blocks[i].ID != "" && i+1 < len(blocks)
		for j := range blocks[i].Placements {
			blocks[i].Placements[j].CanMoveUp = j > 0
			blocks[i].Placements[j].CanMoveDown = j+1 < len(blocks[i].Placements)
		}
	}
}
func (h *handler) redirectSessionSaved(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/sessions/"+id+"/edit?saved=1", http.StatusSeeOther)
}
