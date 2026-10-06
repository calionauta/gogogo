package todo_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// requestTimeout caps any individual HTTP call in integration tests.
//
// MUST exceed the SQLite busy_timeout set in the DSN (10s, see
// features/todo/fixture_test.go + db/pocketbase.go): under write-lock
// contention SQLite blocks up to busy_timeout before returning, so a request
// timeout shorter than that cancels the request while the DB is still
// legitimately waiting — the intermittent "POST /login: context deadline
// exceeded" flake. Keep this strictly greater than busy_timeout plus handler
// overhead; it still fails a genuinely stuck handler instead of hanging.
const requestTimeout = 20 * time.Second

// TestIntegration_CreateListDelete is the canonical happy-path E2E
// for the todo feature: create → list → delete, exercising the real
// PocketBase CRUD path, the real goqite enqueue path, and the real
// HTTP layer.
func TestIntegration_CreateListDelete(t *testing.T) {
	t.Parallel()
	base, _, app, _, cleanup := testFixture(t)
	defer cleanup()

	ctx := newTestCtx(t)
	mustPost(ctx, t, base, "/api/todos", url.Values{titleField: {buyMilk}})

	records, err := app.FindRecordsByFilter("todos", "", "", 0, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record after create, got %d (err=%v)", len(records), err)
	}
	id := records[0].Id

	mustPost(ctx, t, base, "/api/todos/"+id+"/delete", nil)

	records, err = app.FindRecordsByFilter("todos", "", "", 0, 0)
	if err != nil {
		t.Fatalf("find after delete: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records after delete, got %d", len(records))
	}
}

// TestIntegration_ToggleFlipsCompleted exercises the toggle handler
// through the real HTTP layer and verifies the boolean field mutates
// in PocketBase.
func TestIntegration_ToggleFlipsCompleted(t *testing.T) {
	t.Parallel()
	base, _, app, _, cleanup := testFixture(t)
	defer cleanup()

	ctx := newTestCtx(t)
	mustPost(ctx, t, base, "/api/todos", url.Values{titleField: {"dishes"}})

	records, err := app.FindRecordsByFilter("todos", "", "", 0, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record, got %d (err=%v)", len(records), err)
	}
	id := records[0].Id
	if records[0].GetBool("completed") {
		t.Fatal("newly created todo should not be completed")
	}

	mustPost(ctx, t, base, "/api/todos/"+id+"/toggle", nil)

	records, err = app.FindRecordsByFilter("todos", "", "", 0, 0)
	if err != nil || !records[0].GetBool("completed") {
		t.Fatalf("toggle did not flip completed to true (err=%v)", err)
	}

	mustPost(ctx, t, base, "/api/todos/"+id+"/toggle", nil)

	records, err = app.FindRecordsByFilter("todos", "", "", 0, 0)
	if err != nil || records[0].GetBool("completed") {
		t.Fatalf("toggle did not flip completed back to false (err=%v)", err)
	}
}

// TestIntegration_DeleteEmitsInfoToast verifies the delete handler
// emits an info-type toast (different alert class from the create
// success toast) and contains the deleted title in the message.
func TestIntegration_DeleteEmitsInfoToast(t *testing.T) {
	t.Parallel()
	base, _, app, _, cleanup := testFixture(t)
	defer cleanup()

	ctx := newTestCtx(t)
	mustPost(ctx, t, base, "/api/todos", url.Values{titleField: {"trash me"}})

	records, err := app.FindRecordsByFilter("todos", "", "", 0, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record, got %d (err=%v)", len(records), err)
	}

	client := loginClient(ctx, t, base)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/api/todos/"+records[0].Id+"/delete", strings.NewReader(""))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d", resp.StatusCode)
	}
	body := readBody(t, resp)

	if !strings.Contains(body, "alert-info") {
		t.Fatalf("delete response missing alert-info class (should be info, not success): %s", body)
	}
	if !strings.Contains(body, "trash me") {
		t.Fatalf("delete response missing deleted title in toast: %s", body)
	}
}

// TestIntegration_ClearCompletedRemovesOnlyDone seeds two todos, marks
// one complete via direct DB write, hits the bulk-delete endpoint, and
// verifies only the active one remains.
func TestIntegration_ClearCompletedRemovesOnlyDone(t *testing.T) {
	t.Parallel()
	base, _, app, _, cleanup := testFixture(t)
	defer cleanup()

	ctx := newTestCtx(t)
	for _, title := range []string{"active", "done"} {
		mustPost(ctx, t, base, "/api/todos", url.Values{titleField: {title}})
	}

	records, err := app.FindRecordsByFilter("todos", "title='done'", "", 0, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 'done' record, got %d (err=%v)", len(records), err)
	}
	records[0].Set("completed", true)
	if saveErr := app.Save(records[0]); saveErr != nil {
		t.Fatalf("mark done: %v", saveErr)
	}

	mustPost(ctx, t, base, "/api/todos/completed/delete", nil)

	remaining, err := app.FindRecordsByFilter("todos", "", "", 0, 0)
	if err != nil {
		t.Fatalf("find after clear: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(remaining))
	}
	if remaining[0].GetString(titleField) != "active" {
		t.Fatalf("wrong record remaining: %s", remaining[0].GetString(titleField))
	}
}

// newTestCtx returns a fresh context per-test. Bound to requestTimeout
// so any hung handler aborts the test cleanly via the context's
// cancellation rather than the test framework's per-test timeout.
func newTestCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	t.Cleanup(cancel)
	return ctx
}

// mustPost sends a form-encoded POST and asserts the status code. Used
// by integration tests that don't care about the response body.
//
// It authenticates as the demo user first: since RequireOwner, anonymous
// mutations fail closed with a 303 to /login (they used to proceed with
// an empty owner — the fail-open this suite now guards against in
// owner_require_test.go). Tests for the anonymous path assert the 303
// explicitly; the happy-path tests below run authenticated.
func mustPost(ctx context.Context, t *testing.T, base, path string, values url.Values) {
	t.Helper()
	mustPostCtx(ctx, t, loginClient(ctx, t, base), base, path, values)
}

// postForm wraps http.PostForm with a context-aware client so the
// noctx linter sees a context in the request lifecycle.
func postForm(ctx context.Context, url string, values url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return http.DefaultClient.Do(req)
}
