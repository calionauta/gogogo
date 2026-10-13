// SCOPE:feature — genui worker error-surfacing contract test.
//
// The worker used to replace any responder failure with "Ask failed: model
// unavailable", so a missing key, a dead provider and a malformed answer all
// looked identical — and the one thing worth reading (the responder's own
// message) was the one thing thrown away. This pins the fix: that message has
// to reach the stream.
package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlersGenuiAskSurfacesRealError(t *testing.T) {
	const responderErr = "genui: no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)"
	mux := genuiFixture(t, stubResponder{err: errors.New(responderErr)})
	cookie := genuiSignup(t, mux, "genui-err@example.com")

	askRec := httptest.NewRecorder()
	askReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/genui/ask?clientID=genui-err-1",
		strings.NewReader("prompt=show+something"))
	askReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	askReq.AddCookie(cookie)
	mux.ServeHTTP(askRec, askReq)
	if askRec.Code != http.StatusOK {
		t.Fatalf("ask status = %d (%s)", askRec.Code, askRec.Body.String())
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/genui/stream?clientID=genui-err-1", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 16384)
	type readResult struct {
		n   int
		err error
	}
	var got string
	deadline := time.Now().Add(7 * time.Second)
	for !strings.Contains(got, responderErr) && time.Now().Before(deadline) {
		readCh := make(chan readResult, 1)
		go func() {
			n, rerr := resp.Body.Read(buf)
			readCh <- readResult{n, rerr}
		}()
		select {
		case res := <-readCh:
			got += string(buf[:res.n])
		case <-time.After(2 * time.Second):
		}
	}
	if !strings.Contains(got, responderErr) {
		t.Fatalf("stream never surfaced the responder's own error (got %d bytes):\n%s",
			len(got), got)
	}
	// And the fixed string that hid it must not be what the user reads.
	if strings.Contains(got, "model unavailable") {
		t.Fatalf("the generic error string still stands in for the real one:\n%s", got)
	}
}
