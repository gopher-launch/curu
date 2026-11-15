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

	"github.com/gopher-launch/curu/internal/activity"
	"github.com/gopher-launch/curu/internal/auth"
	"github.com/gopher-launch/curu/internal/authorization"
	"github.com/gopher-launch/curu/internal/session"
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

func TestActivityLibraryListSearchAndEmptyStates(t *testing.T) {
	t.Parallel()

	duration := 300
	library := &fakeActivityLibrary{items: []activity.Summary{{
		ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Title: "Evasion Gauntlet", Summary: "Footwork and acceleration", DurationSeconds: &duration,
	}}}
	handler := newTestHandlerWithLibrary(t, library)
	request := httptest.NewRequest(http.MethodGet, "/activities?q=Evasion%20%26%20%3Cscript%3E", nil)
	request.Header.Set("Authorization", "Bearer owner")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %q", response.Code, response.Body.String())
	}
	if library.search != "Evasion & <script>" || library.principal.OwnerID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("library input = principal %+v, search %q", library.principal, library.search)
	}
	body := response.Body.String()
	if !strings.Contains(body, `href="/activities/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"`) || !strings.Contains(body, "Evasion Gauntlet") {
		t.Errorf("list omitted stable link or known activity: %q", body)
	}
	if strings.Contains(body, "<script>Evasion") || !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("search was not safely rendered: %q", body)
	}

	library.items = nil
	empty := httptest.NewRecorder()
	emptyRequest := httptest.NewRequest(http.MethodGet, "/activities", nil)
	emptyRequest.Header.Set("Authorization", "Bearer owner")
	handler.ServeHTTP(empty, emptyRequest)
	if !strings.Contains(empty.Body.String(), "activity library is empty") {
		t.Errorf("empty library state missing: %q", empty.Body.String())
	}

	noMatch := httptest.NewRecorder()
	noMatchRequest := httptest.NewRequest(http.MethodGet, "/activities?q=unknown", nil)
	noMatchRequest.Header.Set("Authorization", "Bearer owner")
	handler.ServeHTTP(noMatch, noMatchRequest)
	if !strings.Contains(noMatch.Body.String(), "No current activities match") {
		t.Errorf("empty search state missing: %q", noMatch.Body.String())
	}
}

func TestSessionCreateEditConflictPreservesCompleteSubmission(t *testing.T) {
	t.Parallel()
	const id = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	planner := &fakeSessionPlanner{item: session.Session{ID: id, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Draft", Revision: 4}}
	handler := newTestHandlerWithSessions(t, planner)
	cookie := csrfCookie(t, handler)
	values := "title=Future+session&date=&group_context=U12&intent=Support&block_id=&block_name=Arrival&block_intent=Explore&block_notes=Choose+freely&block_observation_lens=Scanning&revision=4&csrf_token=" + cookie.Value
	request := httptest.NewRequest(http.MethodPost, "/sessions/"+id, strings.NewReader(values))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || planner.operation != "update" || planner.revision != 4 || planner.input.Date != nil || len(planner.input.Blocks) != 1 {
		t.Fatalf("response/update = %d/%q/%d %+v", response.Code, planner.operation, planner.revision, planner.input)
	}
	planner.err = session.ErrConflict
	stale := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/sessions/"+id, strings.NewReader(values))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	handler.ServeHTTP(stale, request)
	body := stale.Body.String()
	for _, wanted := range []string{"Future session", "U12", "Arrival", "Choose freely", "Scanning", "newer revision exists"} {
		if stale.Code != http.StatusConflict || !strings.Contains(body, wanted) {
			t.Fatalf("conflict omitted %q: status %d body %q", wanted, stale.Code, body)
		}
	}
	planner.err = session.ErrArchivedConflict
	archivedConflict := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/sessions/"+id, strings.NewReader(values))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	handler.ServeHTTP(archivedConflict, request)
	body = archivedConflict.Body.String()
	for _, wanted := range []string{"Future session", "U12", "Arrival", "Choose freely", "Scanning", "was archived after you opened", `href="/sessions/` + id + `/archived"`} {
		if archivedConflict.Code != http.StatusConflict || !strings.Contains(body, wanted) {
			t.Fatalf("archived conflict omitted %q: status %d body %q", wanted, archivedConflict.Code, body)
		}
	}
}

func TestSessionDetailAllowsBlocksWithoutActivitiesAndArchiveLifecycle(t *testing.T) {
	t.Parallel()
	const id = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	planner := &fakeSessionPlanner{item: session.Session{ID: id, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Undated plan", Revision: 2, Blocks: []session.Block{{Name: "Warmup", Notes: "Freeform"}}}}
	handler := newTestHandlerWithSessions(t, planner)
	request := httptest.NewRequest(http.MethodGet, "/sessions/"+id, nil)
	request.Header.Set("Authorization", "Bearer owner")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	for _, wanted := range []string{"Undated plan", "No date set", "Warmup", "No activities are required"} {
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), wanted) {
			t.Fatalf("detail omitted %q: %q", wanted, response.Body.String())
		}
	}
	cookie := csrfCookie(t, handler)
	archive := httptest.NewRequest(http.MethodPost, "/sessions/"+id+"/archive", strings.NewReader("revision=2&csrf_token="+cookie.Value))
	archive.Header.Set("Authorization", "Bearer owner")
	archive.Header.Set("Origin", "https://curu.example")
	archive.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	archive.AddCookie(cookie)
	archived := httptest.NewRecorder()
	handler.ServeHTTP(archived, archive)
	if archived.Code != http.StatusSeeOther || archived.Header().Get("Location") != "/sessions/"+id+"/archived" || planner.operation != "archive" {
		t.Fatalf("archive = %d %q %q", archived.Code, archived.Header().Get("Location"), planner.operation)
	}
}

func TestSessionDetailRendersPlacementSnapshotWithoutLibraryLookup(t *testing.T) {
	t.Parallel()
	duration := 75
	planDuration := 60
	planner := &fakeSessionPlanner{item: session.Session{
		ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Snapshot plan", Revision: 3,
		Blocks: []session.Block{{Name: "Warmup", Placements: []session.Placement{{
			Title: "Copied activity", Summary: "Saved summary", ContentMarkdown: "Use **cones**.\n\n<script>unsafe</script>", RecommendedDurationSeconds: &duration, DurationSeconds: &planDuration, LocalNotes: "Watch the defensive line",
			Phase: "Arrival", Theme: "Scanning", Tags: []string{"ball"},
			Progressions: []session.PlacementProgression{{Title: "Harder", ContentMarkdown: "Add pressure."}},
		}}}},
	}}
	library := &fakeActivityLibrary{err: errors.New("library must not be queried")}
	policy, err := authorization.NewPolicy(authorization.ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"}, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), func() bool { return true }, fakeAuthenticator{}, policy, library, planner, BrowserConfig{PublicOrigin: "https://curu.example"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/sessions/"+planner.item.ID, nil)
	request.Header.Set("Authorization", "Bearer owner")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	for _, wanted := range []string{"Snapshot plan", "Copied activity", "Saved summary", "Plan duration:</strong> 1 minute", "Recommended duration: 75 seconds", "Plan-local notes:</strong> Watch the defensive line", "Arrival", "Scanning", "ball", "<strong>cones</strong>", "Harder", "Add pressure."} {
		if response.Code != http.StatusOK || !strings.Contains(body, wanted) {
			t.Fatalf("snapshot detail omitted %q: status %d body %q", wanted, response.Code, body)
		}
	}
	if library.calls != 0 || strings.Contains(body, "<script>unsafe</script>") {
		t.Fatalf("snapshot detail used library or rendered unsafe HTML: calls=%d body=%q", library.calls, body)
	}
}

func TestSessionEditorSelectsAndChangesPlacementsWithoutLosingEnteredData(t *testing.T) {
	t.Parallel()
	const (
		sessionID   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		blockID     = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
		placementID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
		activityID  = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	)
	planner := &fakeSessionPlanner{item: session.Session{ID: sessionID, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Plan", Revision: 6, Blocks: []session.Block{{ID: blockID, Name: "Warmup", Placements: []session.Placement{{ID: placementID, Title: "Old snapshot", LocalNotes: "Watch feet"}}}}}}
	library := &fakeActivityLibrary{items: []activity.Summary{{ID: activityID, Title: "Evasion game", Summary: "Find space"}}}
	policy, err := authorization.NewPolicy(authorization.ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"}, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), func() bool { return true }, fakeAuthenticator{}, policy, library, planner, BrowserConfig{PublicOrigin: "https://curu.example"})
	if err != nil {
		t.Fatal(err)
	}
	edit := httptest.NewRequest(http.MethodGet, "/sessions/"+sessionID+"/edit", nil)
	edit.Header.Set("Authorization", "Bearer owner")
	shown := httptest.NewRecorder()
	handler.ServeHTTP(shown, edit)
	for _, wanted := range []string{"Old snapshot", "Watch feet", "Evasion game", "Find an activity", "retaining plan-local notes and duration", "event.submitter", "submitter.formAction"} {
		if shown.Code != http.StatusOK || !strings.Contains(shown.Body.String(), wanted) {
			t.Fatalf("editor omitted %q: status=%d body=%q", wanted, shown.Code, shown.Body.String())
		}
	}
	cookie := csrfCookie(t, handler)
	values := "title=Unsaved+title&date=&group_context=U14&intent=Scan&revision=6&block_id=" + blockID + "&block_name=Warmup+edited&block_intent=Move&block_notes=Keep&block_observation_lens=Space&placement_id_" + blockID + "=" + placementID + "&placement_notes_" + blockID + "=Unsaved+placement+note&placement_id=" + placementID + "&activity_id=" + activityID + "&target_block_id=" + blockID + "&placement_action=replace&csrf_token=" + cookie.Value
	request := httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID+"/placements", strings.NewReader(values))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/sessions/"+sessionID+"/edit?placement=replaced" || planner.action.Kind != session.ReplacePlacementAction || planner.action.ActivityID != activityID || planner.input.Title != "Unsaved title" || planner.input.Blocks[0].Placements[0].LocalNotes != "Unsaved placement note" {
		t.Fatalf("placement action response=%d location=%q planner=%+v", response.Code, response.Header().Get("Location"), planner)
	}
	planner.err = session.ErrConflict
	conflict := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID+"/placements", strings.NewReader(values))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	handler.ServeHTTP(conflict, request)
	for _, wanted := range []string{"Unsaved title", "Warmup edited", "Unsaved placement note", "no structural action was applied"} {
		if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), wanted) {
			t.Fatalf("conflict omitted %q: status=%d body=%q", wanted, conflict.Code, conflict.Body.String())
		}
	}
}

func TestActivityDetailRendersCompleteSafeContent(t *testing.T) {
	t.Parallel()

	duration := 305
	library := &fakeActivityLibrary{item: activity.Activity{
		ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Title: "Known activity", Summary: "Useful summary", DurationSeconds: &duration,
		ContentMarkdown: "## Setup\nUse **cones**.\n\n<script>private</script>\n\n[unsafe](javascript:alert(1))",
		Progressions:    []activity.Progression{{Position: 0, Title: "Harder", ContentMarkdown: "Release hands."}},
	}}
	handler := newTestHandlerWithLibrary(t, library)
	request := httptest.NewRequest(http.MethodGet, "/activities/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", nil)
	request.Header.Set("Authorization", "Bearer owner")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %q", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, wanted := range []string{"Known activity", "Useful summary", "305 seconds", "<h2>Setup</h2>", "<strong>cones</strong>", "Progressions", "Harder", "Release hands."} {
		if !strings.Contains(body, wanted) {
			t.Errorf("detail omitted %q: %q", wanted, body)
		}
	}
	if strings.Contains(body, "<script>private</script>") || strings.Contains(body, `href="javascript:`) {
		t.Errorf("detail rendered unsafe Markdown: %q", body)
	}
}

func TestActivityArchiveViewRestoreAndStaleAction(t *testing.T) {
	t.Parallel()
	const id = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	library := &fakeActivityLibrary{
		items: []activity.Summary{{ID: id, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Retained activity"}},
		item:  activity.Activity{ID: id, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Retained activity", Revision: 7},
	}
	handler := newTestHandlerWithLibrary(t, library)

	archivedList := httptest.NewRequest(http.MethodGet, "/activities/archived?q=retained", nil)
	archivedList.Header.Set("Authorization", "Bearer owner")
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, archivedList)
	if listResponse.Code != http.StatusOK || library.operation != "list-archived" || library.search != "retained" || !strings.Contains(listResponse.Body.String(), "/activities/"+id+"/archived") {
		t.Fatalf("archived list response/operation/search = %d/%q/%q; body %q", listResponse.Code, library.operation, library.search, listResponse.Body.String())
	}

	archivedDetail := httptest.NewRequest(http.MethodGet, "/activities/"+id+"/archived", nil)
	archivedDetail.Header.Set("Authorization", "Bearer owner")
	detailResponse := httptest.NewRecorder()
	handler.ServeHTTP(detailResponse, archivedDetail)
	for _, wanted := range []string{"Retained activity", "hidden from the current library", `action="/activities/` + id + `/restore"`, `name="revision" value="7"`} {
		if detailResponse.Code != http.StatusOK || !strings.Contains(detailResponse.Body.String(), wanted) {
			t.Fatalf("archived detail omitted %q: status %d body %q", wanted, detailResponse.Code, detailResponse.Body.String())
		}
	}

	cookie := csrfCookie(t, handler)
	archiveRequest := httptest.NewRequest(http.MethodPost, "/activities/"+id+"/archive", strings.NewReader("revision=7&csrf_token="+cookie.Value))
	archiveRequest.Header.Set("Authorization", "Bearer owner")
	archiveRequest.Header.Set("Origin", "https://curu.example")
	archiveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	archiveRequest.AddCookie(cookie)
	archiveResponse := httptest.NewRecorder()
	handler.ServeHTTP(archiveResponse, archiveRequest)
	if archiveResponse.Code != http.StatusSeeOther || archiveResponse.Header().Get("Location") != "/activities/"+id+"/archived" || library.operation != "archive" || library.revision != 7 {
		t.Fatalf("archive response/operation/revision = %d/%q/%d, location %q", archiveResponse.Code, library.operation, library.revision, archiveResponse.Header().Get("Location"))
	}

	library.archiveErr = activity.ErrConflict
	staleRequest := httptest.NewRequest(http.MethodPost, "/activities/"+id+"/restore", strings.NewReader("revision=7&csrf_token="+cookie.Value))
	staleRequest.Header.Set("Authorization", "Bearer owner")
	staleRequest.Header.Set("Origin", "https://curu.example")
	staleRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	staleRequest.AddCookie(cookie)
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, staleRequest)
	if staleResponse.Code != http.StatusConflict || !strings.Contains(staleResponse.Body.String(), "changed") {
		t.Fatalf("stale restore response = %d, body %q", staleResponse.Code, staleResponse.Body.String())
	}

	library.archiveErr = nil
	restoreRequest := httptest.NewRequest(http.MethodPost, "/activities/"+id+"/restore", strings.NewReader("revision=8&csrf_token="+cookie.Value))
	restoreRequest.Header.Set("Authorization", "Bearer owner")
	restoreRequest.Header.Set("Origin", "https://curu.example")
	restoreRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	restoreRequest.AddCookie(cookie)
	restoreResponse := httptest.NewRecorder()
	handler.ServeHTTP(restoreResponse, restoreRequest)
	if restoreResponse.Code != http.StatusSeeOther || restoreResponse.Header().Get("Location") != "/activities/"+id || library.operation != "restore" || library.revision != 8 {
		t.Fatalf("restore response/operation/revision = %d/%q/%d, location %q", restoreResponse.Code, library.operation, library.revision, restoreResponse.Header().Get("Location"))
	}
}

func TestActivityRoutesDoNotReachLibraryForDeniedIdentity(t *testing.T) {
	t.Parallel()

	library := &fakeActivityLibrary{}
	handler := newTestHandlerWithLibrary(t, library)
	requests := []struct{ method, path string }{
		{http.MethodGet, "/activities"},
		{http.MethodGet, "/activities/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"},
		{http.MethodGet, "/activities/archived"},
		{http.MethodGet, "/activities/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/archived"},
		{http.MethodPost, "/activities/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/archive"},
		{http.MethodPost, "/activities/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/restore"},
	}
	for _, test := range requests {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Header.Set("Authorization", "Bearer stranger")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403", test.method, test.path, response.Code)
		}
	}
	if library.calls != 0 {
		t.Fatalf("denied requests reached library %d times", library.calls)
	}
}

func TestProductLikeMethodsAreUnavailable(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool
	handler := newTestHandler(t, ready.Load)
	cookie := csrfCookie(t, handler)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/templates", strings.NewReader("private=data"))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("X-CSRF-Token", cookie.Value)
	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestActivityCreateExplicitSaveAndEditReopen(t *testing.T) {
	t.Parallel()
	library := &fakeActivityLibrary{item: activity.Activity{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Renamed", Revision: 1}}
	handler := newTestHandlerWithLibrary(t, library)
	cookie := csrfCookie(t, handler)
	form := "csrf_token=" + cookie.Value + "&title=Renamed&summary=Useful&duration_seconds=95&phase=Warmup&theme=Passing&tags=ball%2C+handling&content_markdown=%23%23+Setup&progression_title=Harder&progression_content_markdown=Add+pressure&progression_duration_seconds=30"
	request := httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader(form))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/activities/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/edit?saved=1" {
		t.Fatalf("create response = %d, location %q, body %q", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	if library.input.Title != "Renamed" || library.input.DurationSeconds == nil || *library.input.DurationSeconds != 95 || len(library.input.Progressions) != 1 || len(library.input.Tags) != 2 {
		t.Fatalf("create input = %+v", library.input)
	}

	reopen := httptest.NewRequest(http.MethodGet, response.Header().Get("Location"), nil)
	reopen.Header.Set("Authorization", "Bearer owner")
	reopened := httptest.NewRecorder()
	handler.ServeHTTP(reopened, reopen)
	for _, wanted := range []string{"Saved.", `value="Renamed"`, `name="revision" value="1"`, "Unsaved changes."} {
		if !strings.Contains(reopened.Body.String(), wanted) {
			t.Errorf("reopened editor omitted %q: %q", wanted, reopened.Body.String())
		}
	}
}

func TestActivityFormUsesPreservationAwareSavePath(t *testing.T) {
	t.Parallel()

	library := &fakeActivityLibrary{}
	handler := newTestHandlerWithLibrary(t, library)
	pageRequest := httptest.NewRequest(http.MethodGet, "/activities/new", nil)
	pageRequest.Header.Set("Authorization", "Bearer owner")
	pageResponse := httptest.NewRecorder()
	handler.ServeHTTP(pageResponse, pageRequest)
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("activity form status = %d", pageResponse.Code)
	}
	body := pageResponse.Body.String()
	for _, wanted := range []string{
		`data-preserve-on-rejection="true"`, `addEventListener("submit"`, "event.preventDefault()",
		`"Content-Type": "application/x-www-form-urlencoded"`, `"HX-Request": "true"`,
		`"X-CSRF-Token"`, `response.status === 401`, `response.status === 403`,
		`CustomEvent("curu:authentication-required")`, `CustomEvent("curu:save-rejected")`,
		"Your unsaved changes remain", "Sign in again through the application gateway in a new tab", "Open a fresh editor in a new tab",
	} {
		if !strings.Contains(body, wanted) {
			t.Errorf("activity form omitted preservation mechanism %q", wanted)
		}
	}

	form := "title=Entered+title&content_markdown=Unsaved+content&progression_title=Harder&progression_content_markdown=Unsaved+progression&progression_duration_seconds="
	expired := httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader(form))
	expired.Header.Set("Authorization", "Bearer expired")
	expired.Header.Set("Origin", "https://curu.example")
	expired.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	expired.Header.Set("HX-Request", "true")
	expiredResponse := httptest.NewRecorder()
	handler.ServeHTTP(expiredResponse, expired)
	if expiredResponse.Code != http.StatusUnauthorized || expiredResponse.Header().Get("HX-Reswap") != "none" || expiredResponse.Header().Get("HX-Trigger") != htmxAuthEventName {
		t.Fatalf("expired save response = %d, reswap %q, trigger %q", expiredResponse.Code, expiredResponse.Header().Get("HX-Reswap"), expiredResponse.Header().Get("HX-Trigger"))
	}
	if strings.Contains(expiredResponse.Body.String(), "Entered title") || library.calls != 0 {
		t.Fatalf("expired save exposed input or reached library: body %q, calls %d", expiredResponse.Body.String(), library.calls)
	}

	validCookie := csrfCookie(t, handler)
	for _, test := range []struct {
		name   string
		cookie *http.Cookie
	}{
		{name: "missing cookie"},
		{name: "replaced cookie", cookie: validCookie},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader(form))
			request.Header.Set("Authorization", "Bearer owner")
			request.Header.Set("Origin", "https://curu.example")
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("HX-Request", "true")
			request.Header.Set("X-CSRF-Token", mustCSRFToken(t))
			if test.cookie != nil {
				request.AddCookie(test.cookie)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || response.Header().Get("HX-Reswap") != "none" || response.Header().Get("HX-Trigger") != htmxRejectedEventName {
				t.Fatalf("CSRF rejection = %d, reswap %q, trigger %q", response.Code, response.Header().Get("HX-Reswap"), response.Header().Get("HX-Trigger"))
			}
			if strings.Contains(response.Body.String(), "Unsaved content") {
				t.Errorf("CSRF rejection exposed submitted content: %q", response.Body.String())
			}
		})
	}
}

func TestActivityMutationRejectsUnsupportedMediaTypesWithHeaderCSRF(t *testing.T) {
	t.Parallel()

	for _, contentType := range []string{"application/json", "multipart/form-data; boundary=curu-test"} {
		t.Run(contentType, func(t *testing.T) {
			library := &fakeActivityLibrary{}
			handler := newTestHandlerWithLibrary(t, library)
			cookie := csrfCookie(t, handler)
			request := httptest.NewRequest(http.MethodPost, "/activities", strings.NewReader(`{"title":"unsupported"}`))
			request.Header.Set("Authorization", "Bearer owner")
			request.Header.Set("Origin", "https://curu.example")
			request.Header.Set("Content-Type", contentType)
			request.Header.Set("X-CSRF-Token", cookie.Value)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("content type %q status = %d, want 415; body %q", contentType, response.Code, response.Body.String())
			}
			if library.calls != 0 {
				t.Fatalf("unsupported content type reached library %d times", library.calls)
			}
		})
	}
}

func TestActivityValidationAndConflictPreserveSubmittedValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		create      bool
		err         error
		status      int
		path, extra string
	}{
		{name: "validation", create: true, err: activity.ValidationErrors{"title": "required"}, status: http.StatusUnprocessableEntity, path: "/activities"},
		{name: "stale update", err: activity.ErrConflict, status: http.StatusConflict, path: "/activities/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", extra: "&revision=3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			library := &fakeActivityLibrary{item: activity.Activity{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}}
			if test.create {
				library.createErr = test.err
			} else {
				library.updateErr = test.err
			}
			handler := newTestHandlerWithLibrary(t, library)
			cookie := csrfCookie(t, handler)
			form := "csrf_token=" + cookie.Value + "&title=Submitted&summary=%3Cscript%3Eprivate%3C%2Fscript%3E&duration_seconds=&content_markdown=My+unsaved+notes" + test.extra
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(form))
			request.Header.Set("Authorization", "Bearer owner")
			request.Header.Set("Origin", "https://curu.example")
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body %q", response.Code, test.status, response.Body.String())
			}
			body := response.Body.String()
			for _, wanted := range []string{"Submitted", "My unsaved notes", "&lt;script&gt;private&lt;/script&gt;"} {
				if !strings.Contains(body, wanted) {
					t.Errorf("preserved editor omitted %q: %q", wanted, body)
				}
			}
			if strings.Contains(body, "<script>private</script>") {
				t.Errorf("preserved editor rendered unsafe input: %q", body)
			}
		})
	}
}

func TestActivityConflictRecoveryDraftSaveReopenAndDiscard(t *testing.T) {
	t.Parallel()
	activityID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	draftID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	library := &fakeActivityLibrary{
		updateErr: activity.ErrConflict,
		draft: activity.RecoveryDraft{ID: draftID, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ActivityID: activityID, BaseRevision: 3,
			Input: activity.Input{Title: "Submitted", Summary: "Private summary", ContentMarkdown: "Exact unsaved notes", Progressions: []activity.ProgressionInput{{Title: "Harder", ContentMarkdown: "Recovered progression"}}}},
	}
	handler := newTestHandlerWithLibrary(t, library)
	cookie := csrfCookie(t, handler)
	form := "csrf_token=" + cookie.Value + "&title=Submitted&summary=Private+summary&duration_seconds=&content_markdown=Exact+unsaved+notes&revision=3&progression_title=Harder&progression_content_markdown=Recovered+progression&progression_duration_seconds="

	conflict := newOwnerMutation(t, cookie, "/activities/"+activityID, form)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflict)
	if conflictResponse.Code != http.StatusConflict || !strings.Contains(conflictResponse.Body.String(), "Save separate recovery draft") || !strings.Contains(conflictResponse.Body.String(), "/activities/"+activityID+"/recovery-draft") {
		t.Fatalf("conflict response = %d, body %q", conflictResponse.Code, conflictResponse.Body.String())
	}

	save := newOwnerMutation(t, cookie, "/activities/"+activityID+"/recovery-draft", form)
	saveResponse := httptest.NewRecorder()
	handler.ServeHTTP(saveResponse, save)
	if saveResponse.Code != http.StatusSeeOther || saveResponse.Header().Get("Location") != "/recovery-drafts/"+draftID+"?saved=1" {
		t.Fatalf("draft save response = %d, location %q, body %q", saveResponse.Code, saveResponse.Header().Get("Location"), saveResponse.Body.String())
	}
	if library.operation != "save-draft" || library.id != activityID || library.revision != 3 || library.input.ContentMarkdown != "Exact unsaved notes" || len(library.input.Progressions) != 1 || library.input.Progressions[0].ContentMarkdown != "Recovered progression" {
		t.Fatalf("saved draft call = %q/%q/%d/%+v", library.operation, library.id, library.revision, library.input)
	}

	reopen := httptest.NewRequest(http.MethodGet, saveResponse.Header().Get("Location"), nil)
	reopen.Header.Set("Authorization", "Bearer owner")
	reopenResponse := httptest.NewRecorder()
	handler.ServeHTTP(reopenResponse, reopen)
	for _, wanted := range []string{"Recovery draft saved", "current activity was not changed", "Submitted", "Exact unsaved notes", "Recovered progression", "Discard recovery draft"} {
		if !strings.Contains(reopenResponse.Body.String(), wanted) {
			t.Errorf("reopened draft omitted %q: %q", wanted, reopenResponse.Body.String())
		}
	}

	discard := newOwnerMutation(t, cookie, "/recovery-drafts/"+draftID+"/discard", "csrf_token="+cookie.Value)
	discardResponse := httptest.NewRecorder()
	handler.ServeHTTP(discardResponse, discard)
	if discardResponse.Code != http.StatusSeeOther || discardResponse.Header().Get("Location") != "/recovery-drafts" || library.operation != "discard-draft" || library.id != draftID {
		t.Fatalf("discard response/call = %d/%q/%q/%q", discardResponse.Code, discardResponse.Header().Get("Location"), library.operation, library.id)
	}
}

func TestActivityRecoveryDraftFailureRetainsOpenSubmission(t *testing.T) {
	t.Parallel()
	activityID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	library := &fakeActivityLibrary{draftErr: errors.New("database unavailable")}
	handler := newTestHandlerWithLibrary(t, library)
	cookie := csrfCookie(t, handler)
	form := "csrf_token=" + cookie.Value + "&title=Still+open&summary=&duration_seconds=&content_markdown=Do+not+lose+this&revision=3"
	request := newOwnerMutation(t, cookie, "/activities/"+activityID+"/recovery-draft", form)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("draft failure status = %d, body %q", response.Code, response.Body.String())
	}
	for _, wanted := range []string{"not saved", "Still open", "Do not lose this", "retry deliberately"} {
		if !strings.Contains(response.Body.String(), wanted) {
			t.Errorf("draft failure omitted %q: %q", wanted, response.Body.String())
		}
	}
}

func newOwnerMutation(t *testing.T, cookie *http.Cookie, path, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Origin", "https://curu.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	return request
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

type fakeActivityLibrary struct {
	items                []activity.Summary
	item                 activity.Activity
	err                  error
	principal            authorization.Principal
	search               string
	id                   string
	calls                int
	input                activity.Input
	revision             int64
	operation            string
	createErr, updateErr error
	archiveErr           error
	drafts               []activity.RecoveryDraftSummary
	draft                activity.RecoveryDraft
	draftErr             error
}

func (l *fakeActivityLibrary) ListRecoveryDrafts(_ context.Context, principal authorization.Principal) ([]activity.RecoveryDraftSummary, error) {
	l.calls++
	l.operation, l.principal = "list-drafts", principal
	return l.drafts, l.draftErr
}

func (l *fakeActivityLibrary) RecoveryDraft(_ context.Context, principal authorization.Principal, id string) (activity.RecoveryDraft, error) {
	l.calls++
	l.operation, l.principal, l.id = "find-draft", principal, id
	return l.draft, l.draftErr
}

func (l *fakeActivityLibrary) SaveRecoveryDraft(_ context.Context, principal authorization.Principal, id string, revision int64, input activity.Input) (activity.RecoveryDraft, error) {
	l.calls++
	l.operation, l.principal, l.id, l.revision, l.input = "save-draft", principal, id, revision, input
	return l.draft, l.draftErr
}

func (l *fakeActivityLibrary) DiscardRecoveryDraft(_ context.Context, principal authorization.Principal, id string) error {
	l.calls++
	l.operation, l.principal, l.id = "discard-draft", principal, id
	return l.draftErr
}

func (l *fakeActivityLibrary) Create(_ context.Context, principal authorization.Principal, input activity.Input) (activity.Activity, error) {
	l.calls++
	l.principal, l.input = principal, input
	if l.createErr != nil {
		return activity.Activity{}, l.createErr
	}
	return l.item, nil
}

func (l *fakeActivityLibrary) Update(_ context.Context, principal authorization.Principal, id string, revision int64, input activity.Input) (activity.Activity, error) {
	l.calls++
	l.principal, l.id, l.revision, l.input = principal, id, revision, input
	if l.updateErr != nil {
		return activity.Activity{}, l.updateErr
	}
	return l.item, nil
}

func (l *fakeActivityLibrary) List(_ context.Context, principal authorization.Principal, search string) ([]activity.Summary, error) {
	l.calls++
	l.operation = "list-current"
	l.principal, l.search = principal, search
	return l.items, l.err
}

func (l *fakeActivityLibrary) ListArchived(_ context.Context, principal authorization.Principal, search string) ([]activity.Summary, error) {
	l.calls++
	l.operation, l.principal, l.search = "list-archived", principal, search
	return l.items, l.err
}

func (l *fakeActivityLibrary) Detail(_ context.Context, principal authorization.Principal, id string) (activity.Activity, error) {
	l.calls++
	l.operation = "detail-current"
	l.principal, l.id = principal, id
	if l.err != nil {
		return activity.Activity{}, l.err
	}
	if l.item.ID == "" {
		return activity.Activity{}, activity.ErrNotFound
	}
	return l.item, nil
}

func (l *fakeActivityLibrary) ArchivedDetail(_ context.Context, principal authorization.Principal, id string) (activity.Activity, error) {
	l.calls++
	l.operation, l.principal, l.id = "detail-archived", principal, id
	if l.err != nil {
		return activity.Activity{}, l.err
	}
	if l.item.ID == "" {
		return activity.Activity{}, activity.ErrNotFound
	}
	return l.item, nil
}

func (l *fakeActivityLibrary) Archive(_ context.Context, principal authorization.Principal, id string, revision int64) (activity.Activity, error) {
	l.calls++
	l.operation, l.principal, l.id, l.revision = "archive", principal, id, revision
	if l.archiveErr != nil {
		return activity.Activity{}, l.archiveErr
	}
	return l.item, nil
}

func (l *fakeActivityLibrary) Restore(_ context.Context, principal authorization.Principal, id string, revision int64) (activity.Activity, error) {
	l.calls++
	l.operation, l.principal, l.id, l.revision = "restore", principal, id, revision
	if l.archiveErr != nil {
		return activity.Activity{}, l.archiveErr
	}
	return l.item, nil
}

type fakeSessionPlanner struct {
	items         []session.Summary
	item          session.Session
	err           error
	archiveErr    error
	operation, id string
	principal     authorization.Principal
	revision      int64
	input         session.Input
	action        session.PlacementAction
}

func (p *fakeSessionPlanner) List(_ context.Context, principal authorization.Principal, archived bool) ([]session.Summary, error) {
	p.operation, p.principal = "list", principal
	if archived {
		p.operation = "list-archived"
	}
	return p.items, p.err
}
func (p *fakeSessionPlanner) Detail(_ context.Context, principal authorization.Principal, id string, archived bool) (session.Session, error) {
	p.operation, p.principal, p.id = "detail", principal, id
	if archived {
		p.operation = "detail-archived"
	}
	if p.err != nil {
		return session.Session{}, p.err
	}
	if p.item.ID == "" {
		return session.Session{}, session.ErrNotFound
	}
	return p.item, nil
}
func (p *fakeSessionPlanner) Create(_ context.Context, principal authorization.Principal, input session.Input) (session.Session, error) {
	p.operation, p.principal, p.input = "create", principal, input
	return p.item, p.err
}
func (p *fakeSessionPlanner) Update(_ context.Context, principal authorization.Principal, id string, revision int64, input session.Input) (session.Session, error) {
	p.operation, p.principal, p.id, p.revision, p.input = "update", principal, id, revision, input
	return p.item, p.err
}
func (p *fakeSessionPlanner) Archive(_ context.Context, principal authorization.Principal, id string, revision int64, archived bool) (session.Session, error) {
	p.operation, p.principal, p.id, p.revision = "restore", principal, id, revision
	if archived {
		p.operation = "archive"
	}
	if p.archiveErr != nil {
		return session.Session{}, p.archiveErr
	}
	return p.item, p.err
}
func (p *fakeSessionPlanner) ApplyPlacementAction(_ context.Context, principal authorization.Principal, id string, revision int64, input session.Input, action session.PlacementAction) (session.Session, error) {
	p.operation, p.principal, p.id, p.revision, p.input, p.action = "placement", principal, id, revision, input, action
	return p.item, p.err
}

func newTestHandlerWithSessions(t *testing.T, planner sessionPlanner) http.Handler {
	t.Helper()
	policy, err := authorization.NewPolicy(authorization.ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"}, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), func() bool { return true }, fakeAuthenticator{}, policy, &fakeActivityLibrary{}, planner, BrowserConfig{PublicOrigin: "https://curu.example"})
	if err != nil {
		t.Fatal(err)
	}
	return handler
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
	handler, err := NewHandler(logger, ready, fakeAuthenticator{}, policy, &fakeActivityLibrary{}, &fakeSessionPlanner{}, BrowserConfig{PublicOrigin: "https://curu.example"})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func newTestHandlerWithLibrary(t *testing.T, library activityLibrary) http.Handler {
	t.Helper()
	policy, err := authorization.NewPolicy(
		authorization.ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"},
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), func() bool { return true }, fakeAuthenticator{}, policy, library, &fakeSessionPlanner{}, BrowserConfig{PublicOrigin: "https://curu.example"})
	if err != nil {
		t.Fatal(err)
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
