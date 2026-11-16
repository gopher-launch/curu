package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gopher-launch/curu/internal/session"
)

func TestSessionReorderRetainsBrowserFocusAndViewport(t *testing.T) {
	browserPath := findChrome(t)
	const (
		sessionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		blockID   = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
		targetID  = "88888888-8888-4888-8888-888888888888"
	)
	placements := []session.Placement{
		{ID: "11111111-1111-4111-8111-111111111111", Title: "Activity 1"},
		{ID: "22222222-2222-4222-8222-222222222222", Title: "Activity 2"},
		{ID: "33333333-3333-4333-8333-333333333333", Title: "Activity 3"},
		{ID: "44444444-4444-4444-8444-444444444444", Title: "Activity 4"},
		{ID: "55555555-5555-4555-8555-555555555555", Title: "Activity 5"},
		{ID: "66666666-6666-4666-8666-666666666666", Title: "Activity 6"},
		{ID: "77777777-7777-4777-8777-777777777777", Title: "Activity 7"},
		{ID: targetID, Title: "Activity 8"},
		{ID: "99999999-9999-4999-8999-999999999999", Title: "Activity 9"},
		{ID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Title: "Activity 10"},
	}
	planner := &fakeSessionPlanner{item: session.Session{
		ID: sessionID, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Long phone plan", Revision: 9,
		Blocks: []session.Block{{ID: blockID, Name: "Ten-activity warmup", Placements: placements}},
	}}
	application := newTestHandlerWithSessions(t, planner)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer owner")
		if r.Method == http.MethodPost {
			r.Header.Set("Origin", "https://curu.example")
		}
		application.ServeHTTP(w, r)
	}))
	defer server.Close()

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.WindowSize(390, 844),
	)
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAllocator()
	browser, cancelBrowser := chromedp.NewContext(allocator)
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browser, 20*time.Second)
	defer cancel()

	selector := `button[name="move_placement_up"][value="` + targetID + `"]`
	var hash, activeID string
	var targetInViewport bool
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL+"/sessions/"+sessionID+"/edit"),
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit phone-width reorder: %v", err)
	}
	if err := waitForBrowserHash(ctx, "#item-"+targetID); err != nil {
		t.Fatalf("wait for reorder navigation: %v", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.location.hash`, &hash),
		chromedp.Evaluate(`document.activeElement && document.activeElement.id`, &activeID),
		chromedp.Evaluate(`(function(){var r=document.getElementById("item-`+targetID+`").getBoundingClientRect();return r.bottom>0&&r.top<window.innerHeight})()`, &targetInViewport),
	); err != nil {
		t.Fatalf("inspect phone-width reorder context: %v", err)
	}
	if hash != "#item-"+targetID || activeID != "item-"+targetID || !targetInViewport {
		t.Fatalf("reorder context hash=%q active=%q inViewport=%t", hash, activeID, targetInViewport)
	}
	if planner.action.Kind != session.MovePlacementUpAction || planner.action.PlacementID != targetID {
		t.Fatalf("reorder action = %+v", planner.action)
	}
}

func TestLargeSessionConflictTransfersRecoverySnapshotWithoutPostingItOrdinarily(t *testing.T) {
	browserPath := findChrome(t)
	const sessionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	const blockID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	placements := make([]session.Placement, 8)
	for i := range placements {
		placements[i] = session.Placement{ID: fmt.Sprintf("dddddddd-dddd-4ddd-8ddd-%012d", i), Title: fmt.Sprintf("Large snapshot %d", i), ContentMarkdown: strings.Repeat("x", 100000)}
	}
	planner := &fakeSessionPlanner{item: session.Session{ID: sessionID, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Large conflict plan", Revision: 7, Blocks: []session.Block{{ID: blockID, Name: "Warmup", Placements: placements}}}, updateErr: session.ErrConflict}
	application := newTestHandlerWithSessions(t, planner)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer owner")
		if r.Method == http.MethodPost {
			r.Header.Set("Origin", "https://curu.example")
		}
		application.ServeHTTP(w, r)
	}))
	defer server.Close()

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(browserPath), chromedp.Flag("ignore-certificate-errors", true))
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAllocator()
	browser, cancelBrowser := chromedp.NewContext(allocator)
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browser, 20*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/sessions/"+sessionID+"/edit"),
		chromedp.WaitVisible(`button[type="submit"]:not([formaction])`, chromedp.ByQuery),
		chromedp.Click(`button[type="submit"]:not([formaction])`, chromedp.ByQuery),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		t.Fatalf("submit large stale editor: %v", err)
	}
	var name string
	var valueLength int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById("recovery-snapshot").name`, &name),
		chromedp.Evaluate(`document.getElementById("recovery-snapshot").value.length`, &valueLength),
	); err != nil {
		t.Fatalf("inspect transferred recovery snapshot: %v", err)
	}
	if planner.operation != "update" || name != "recovery_snapshot" || valueLength <= maxRequestBody {
		t.Fatalf("large conflict transfer operation=%q name=%q length=%d", planner.operation, name, valueLength)
	}
}

func TestSessionValidationThenConflictSavesCompleteRecoveryCopy(t *testing.T) {
	browserPath := findChrome(t)
	const (
		sessionID   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		blockID     = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
		placementID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	)
	duration := 75
	planner := &fakeSessionPlanner{
		item: session.Session{
			ID: sessionID, OwnerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Validation race", Revision: 7,
			Blocks: []session.Block{{ID: blockID, Name: "Warmup", Placements: []session.Placement{{
				ID: placementID, Title: "Copied activity", ContentMarkdown: "Exact copied instructions", DurationSeconds: &duration, LocalNotes: "Original note",
				Progressions: []session.PlacementProgression{{Title: "Harder", ContentMarkdown: "Exact progression"}},
			}}}},
		},
		updateErr: session.ErrConflict,
	}
	application := newTestHandlerWithSessions(t, planner)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer owner")
		if r.Method == http.MethodPost {
			r.Header.Set("Origin", "https://curu.example")
		}
		application.ServeHTTP(w, r)
	}))
	defer server.Close()

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(browserPath), chromedp.Flag("ignore-certificate-errors", true))
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAllocator()
	browser, cancelBrowser := chromedp.NewContext(allocator)
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browser, 20*time.Second)
	defer cancel()

	const saveButton = `button[type="submit"]:not([formaction])`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/sessions/"+sessionID+"/edit"),
		chromedp.WaitVisible(saveButton, chromedp.ByQuery),
		chromedp.Evaluate(`(function(){var input=document.getElementById("session-allocation");input.removeAttribute("max");input.value="86401"})()`, nil),
		chromedp.Click(saveButton, chromedp.ByQuery),
		chromedp.WaitVisible(`p.error`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit invalid editor: %v", err)
	}
	var validationSnapshotName, validationSnapshotValue string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById("recovery-snapshot").name`, &validationSnapshotName),
		chromedp.Evaluate(`document.getElementById("recovery-snapshot").value`, &validationSnapshotValue),
		chromedp.SetValue(`#session-allocation`, "900", chromedp.ByQuery),
		chromedp.Click(saveButton, chromedp.ByQuery),
		chromedp.WaitVisible(`button[formaction$="/recovery-draft"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("correct invalid editor and submit stale revision: %v", err)
	}
	if validationSnapshotName != "" || validationSnapshotValue == "" {
		t.Fatalf("validation editor snapshot name=%q empty=%t", validationSnapshotName, validationSnapshotValue == "")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`button[formaction$="/recovery-draft"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`form[action^="/session-recovery-drafts/"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("save recovery after validation and conflict: %v", err)
	}
	if planner.operation != "save-session-draft" || len(planner.savedSnapshot.Blocks) != 1 || len(planner.savedSnapshot.Blocks[0].Placements) != 1 {
		t.Fatalf("saved recovery operation=%q snapshot=%+v", planner.operation, planner.savedSnapshot)
	}
	if planner.input.AllocationSeconds == nil || *planner.input.AllocationSeconds != 900 {
		t.Fatalf("saved recovery allocation = %v", planner.input.AllocationSeconds)
	}
	got := planner.savedSnapshot.Blocks[0].Placements[0]
	if got.ContentMarkdown != "Exact copied instructions" || got.DurationSeconds == nil || *got.DurationSeconds != duration || len(got.Progressions) != 1 || got.Progressions[0].ContentMarkdown != "Exact progression" {
		t.Fatalf("saved recovery placement = %+v", got)
	}
}

func waitForBrowserHash(ctx context.Context, wanted string) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		var hash string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.location.hash`, &hash)); err == nil && hash == wanted {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return context.DeadlineExceeded
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func findChrome(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("Chrome or Chromium is required for the browser regression")
	return ""
}
