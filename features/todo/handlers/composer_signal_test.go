// SCOPE:feature — todo composer: the box owns the bound signal, not the response.

package handlers

import (
	"net/http"
	"testing"
)

// TestCreateResponseDoesNotWriteTheComposerSignal pins the third instance of
// the client-owned class, found only after datastar-lint learned to read
// constant map keys (v0.14.2): the create response cleared $newTitle from the
// server, and $newTitle is bound two-way to the composer. Type immediately
// after pressing Enter and the response wipes what you typed — reproduced in
// the browser (input went "" within 200ms) and worked around in the smoke by
// waiting between creates, which is how it stayed hidden.
//
// The box owns the signal: the client clears it after the request succeeds
// (and only if it still holds the value that was sent), so no response may
// write it. The rows must still be listed, and $loading must still clear.
//
// Red-proof: put `"newTitle": ""` back into the create merge and this fails.
func TestCreateResponseDoesNotWriteTheComposerSignal(t *testing.T) {
	f := newHandlersFixture(t)
	wireBM25(t, f)
	cookie := f.signup("composer@example.com")

	body := f.do(http.MethodPost, "/api/todos", "title=composer+probe&filter=all", cookie).Body.String()
	if contains(body, `"newTitle"`) {
		t.Errorf("the create response wrote the client-owned newTitle signal; a "+
			"response landing after the user typed would erase it:\n%s", body)
	}
	// Silence about the signal is not a licence to skip the rest of the job.
	if !contains(body, "composer probe") {
		t.Errorf("the response stopped listing the created row:\n%s", body)
	}
	if !contains(body, `"loading"`) {
		t.Errorf("the response stopped clearing $loading, which would leave the "+
			"submit button disabled:\n%s", body)
	}
}
