// SCOPE:layer=feature,removal=feature — Notes sync-protocol tests: base
// revisions, stale refusal + retry, session visibility. Split out of
// notes_presence_test.go to stay within the 500-line budget.
package notes_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// TestNotesStreamSendsSessionEvent pins session visibility: a tab open
// for days outlives its cookie, and without this event an expired session
// looks identical to a healthy one until names degrade to hashes.
func TestNotesStreamSendsSessionEvent(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-session"

	got, closeStream := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeStream()

	waitNoteEvent(t, got, func(msg map[string]any) bool {
		authed, _ := msg["authed"].(bool)
		return msg["type"] == "session" && authed
	})
}

// TestNotesOpReportsAuthed pins both branches: authed ops report true,
// anonymous ops still apply (warn-only, house parity with whiteboard)
// but report false so the UI can say so instead of failing silently.
func TestNotesOpReportsAuthed(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-authedflag"

	_, out := postOp(t, client, baseURL, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}]}`)
	if authed, _ := out["authed"].(bool); !authed {
		t.Fatalf("authed op reports authed = %v", out["authed"])
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	anon := &http.Client{Jar: jar}
	code, anonOut, err := postOpRaw(anon, baseURL, docID, "anonX", `{"base":1,"ops":[{"t":"ins","i":2,"s":"!"}]}`)
	if err != nil {
		t.Fatalf("anon op transport: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("anon op status = %d (warn-only must stay open)", code)
	}
	if authed, _ := anonOut["authed"].(bool); authed {
		t.Fatalf("anon op reports authed = true")
	}
}

// TestNotesFragmentListsDocs pins the live-index half: a note created via
// /notes/new lands in the PB collection, and the fragment endpoint renders
// its id (the index page re-fetches it on PB realtime events, same pattern
// as the whiteboard list).
func TestNotesFragmentListsDocs(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)

	docID := createNoteForTest(t, client, baseURL)

	resp := doGet(t, client, baseURL+"/api/notes/fragment")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fragment status = %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("fragment read: %v", err)
	}
	if !strings.Contains(buf.String(), docID) {
		t.Fatalf("fragment missing created doc %q", docID)
	}
}

// doGet is the noctx-clean GET helper (revive/unused-parameter and the
// no-direct-Get rule both fire on inline client.Get in tests).
func doGet(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("get req: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return resp
}

func createNoteForTest(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { client.CheckRedirect = nil }()
	resp := doGet(t, client, baseURL+"/notes/new")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("new note status = %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	id := strings.TrimPrefix(loc, "/notes/")
	if id == "" || strings.Contains(id, "/") {
		t.Fatalf("new note Location = %q", loc)
	}
	return id
}

// TestNotesRevChainMonotonic pins the happy path a well-behaved client
// walks: every batch carries the base learned from the previous answer,
// so revisions advance 1-by-1 with no spurious 409 in between. This is
// the server half of bidirectional sync (the reported "works one way"
// bug was clients diverging, then mispositioning on a stale base).
func TestNotesRevChainMonotonic(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-chain"

	want := []struct {
		body string
		rev  float64
		text string
	}{
		{`{"base":0,"ops":[{"t":"ins","i":0,"s":"a"}]}`, 1, "a"},
		{`{"base":1,"ops":[{"t":"ins","i":1,"s":"b"}]}`, 2, "ab"},
		{`{"base":2,"ops":[{"t":"ins","i":2,"s":"c"}]}`, 3, "abc"},
	}
	for _, w := range want {
		code, out := postOp(t, client, baseURL, docID, "cliA", w.body)
		if code != http.StatusOK {
			t.Fatalf("chain op %v: status = %d", w.body, code)
		}
		if rev, _ := out["rev"].(float64); rev != w.rev {
			t.Fatalf("chain op %v: rev = %v, want %v", w.body, rev, w.rev)
		}
		if out["text"] != w.text {
			t.Fatalf("chain op %v: text = %v, want %v", w.body, out["text"], w.text)
		}
	}
}

// TestNotesStreamCarriesRev pins passive base advancement: a client that
// only listens learns the current revision from stream events, so its
// next batch positions against fresh state even if it never POSTed.
func TestNotesStreamCarriesRev(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-streamrev"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()

	postOp(t, client, baseURL, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}]}`)
	msg := waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "note-text"
	})
	rev, _ := msg["rev"].(float64)
	if rev < 1 {
		t.Fatalf("stream event rev = %v, want >= 1", msg["rev"])
	}

	// B, having learned rev from the stream, appends without a 409.
	body := `{"base":1,"ops":[{"t":"ins","i":2,"s":"!"}]}`
	code, out := postOp(t, client, baseURL, docID, "cliB", body)
	if code != http.StatusOK {
		t.Fatalf("informed append: status = %d: %v", code, out)
	}
	if out["text"] != "hi!" {
		t.Fatalf("informed append: text = %v, want hi!", out["text"])
	}
}

// TestNotesStaleBaseRejected pins the anti-loss contract: an op batch
// computed against an old base must NOT apply (positions would be wrong)
// — the server answers 409 with the authoritative text + revision so the
// client re-syncs instead of silently corrupting.
func TestNotesStaleBaseRejected(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-stale"

	code, out := postOp(t, client, baseURL, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hello"}]}`)
	if code != http.StatusOK {
		t.Fatalf("first op: status = %d: %v", code, out)
	}
	if rev, _ := out["rev"].(float64); rev != 1 {
		t.Fatalf("first op: rev = %v, want 1", out["rev"])
	}

	code, out = postOp(t, client, baseURL, docID, "cliB", `{"base":0,"ops":[{"t":"ins","i":0,"s":"XYZ "}]}`)
	if code != http.StatusConflict {
		t.Fatalf("stale base: status = %d, want 409", code)
	}
	if out["text"] != "hello" {
		t.Fatalf("stale base: authoritative text = %v, want hello", out["text"])
	}
	if rev, _ := out["rev"].(float64); rev != 1 {
		t.Fatalf("stale base: rev = %v, want 1", out["rev"])
	}
}

// TestNotesConcurrentBatchesMerge pins convergence through the
// reject-and-retry loop: B's stale batch is refused, B replays against
// the fresh base, both edits survive.
func TestNotesConcurrentBatchesMerge(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-race"

	postOp(t, client, baseURL, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hello"}]}`)
	code, _ := postOp(t, client, baseURL, docID, "cliB",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"XYZ "}]}`)
	if code != http.StatusConflict {
		t.Fatalf("race: stale batch status = %d, want 409", code)
	}
	_, out := postOp(t, client, baseURL, docID, "cliB", `{"base":1,"ops":[{"t":"ins","i":0,"s":"XYZ "}]}`)
	text, _ := out["text"].(string)
	if !strings.Contains(text, "hello") || !strings.Contains(text, "XYZ") {
		t.Fatalf("replayed merge lost an edit: %q", text)
	}
	if rev, _ := out["rev"].(float64); rev != 2 {
		t.Fatalf("replayed merge: rev = %v, want 2", out["rev"])
	}
}
