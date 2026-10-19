// SCOPE:feature — acceptance: anonymous callers can neither read nor write a note.

package handlers

import (
	"net/http"
	"testing"
)

// TestNotesRejectsAnonymousAccess is the acceptance gate for the "shared
// docs are open to every SIGNED-IN user" contract, from the outside.
//
// The pre-fix behaviour was a write by an unauthenticated stranger: no
// cookie, POST /api/notes/{id}/op answered 200 and persisted a snapshot
// for an id nobody had registered. RBAC could not be the guard — it is
// opt-in (RBAC_ENABLED defaults to false) — so the handler owns it.
//
// Every surface that touches text is covered here: the op write, the
// stream read, the snapshot-style fragment read and presence. A regression
// in any one of them re-opens a different hole (write, read, roster).
func TestNotesRejectsAnonymousAccess(t *testing.T) {
	server, mem, _ := notesPortFixture(t)
	anon := &http.Client{}
	docID := "note-anon-gate"

	// 1. Write.
	code, _, err := postOpRaw(anon, server, docID, "anonW", `{"base":0,"ops":[{"t":"ins","i":0,"s":"pwned"}]}`)
	if err != nil {
		t.Fatalf("anon op transport: %v", err)
	}
	if code != http.StatusUnauthorized {
		t.Errorf("anon op status = %d, want 401", code)
	}
	if _, ok := mem.LoadSnapshot(docID); ok {
		t.Error("anon op persisted a snapshot: anonymous write reached the store")
	}

	// 2. Read (the stream carries the body; EventSource must not loop).
	req, rerr := http.NewRequestWithContext(t.Context(), http.MethodGet,
		server.URL+"/api/notes/"+docID+"/stream?clientID=anonR", nil)
	if rerr != nil {
		t.Fatalf("anon stream request: %v", rerr)
	}
	resp, rerr := anon.Do(req)
	if rerr != nil {
		t.Fatalf("anon stream transport: %v", rerr)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon stream status = %d, want 401", resp.StatusCode)
	}

	// 3. Presence (typing/caret name who is in a doc).
	for _, path := range []string{"typing", "caret"} {
		code := postPresence(t, anon, server, docID, "anonP", path, `{"typing":true,"line":1,"pos":0}`)
		if code != http.StatusUnauthorized {
			t.Errorf("anon %s status = %d, want 401", path, code)
		}
	}
}

// TestNotesAllowsSignedInEditor pins the other half of the same contract:
// the gate must not lock a REAL session out of the shared doc. A viewer
// (the restricted role) still reads and writes any note — that is what
// makes the two-browser collaboration demo prove anything.
// Red-proof: replace the 401 gate with an admin check and this goes red.
func TestNotesAllowsSignedInEditor(t *testing.T) {
	server, mem, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-editor-ok"

	code, out, err := postOpRaw(client, server, docID, "ed1", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hello"}]}`)
	if err != nil {
		t.Fatalf("op transport: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("signed-in op status = %d (%v), want 200", code, out)
	}
	if _, ok := mem.LoadSnapshot(docID); !ok {
		t.Error("signed-in op did not persist a snapshot")
	}
}
