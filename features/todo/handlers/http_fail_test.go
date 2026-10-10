package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// fail must return the public message with the given status and never
// leak the internal error text.
func TestFail_WritesPublicMessageWithStatus(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	c := &core.RequestEvent{}
	c.Response = rec
	c.Request = mustCtxReq(http.MethodPost, "/api/todos")

	if err := fail(c, "todo: save failed", errors.New("sqlite: busy"), "save failed"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if rec.Code != statusInternal {
		t.Fatalf("status=%d, want %d", rec.Code, statusInternal)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "save failed") {
		t.Fatalf("body must contain public message; got %q", body)
	}
	if strings.Contains(body, "sqlite: busy") {
		t.Fatalf("body must not leak internal error; got %q", body)
	}
}
