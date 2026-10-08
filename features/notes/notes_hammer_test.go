// SCOPE:layer=feature,removal=feature — Notes concurrency hammer test.
package notes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// postOpRaw is the goroutine-safe postOp: no t.Fatalf inside workers.
func postOpRaw(client *http.Client, baseURL, docID, clientID, body string) (int, map[string]any, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		baseURL+"/api/notes/"+docID+"/op?clientID="+clientID,
		strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if dErr := json.NewDecoder(resp.Body).Decode(&out); dErr != nil {
		return resp.StatusCode, map[string]any{}, nil
	}
	return resp.StatusCode, out, nil
}

// TestNotesConcurrentHammer hammers one doc from N writers at once (each
// tracking rev through 200/409 like a real tab) and asserts every marker
// survives. This is the concurrency acceptance test: revsMu + doc mutex
// under -race, merge completeness under contention.
func TestNotesConcurrentHammer(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-hammer"

	const writers = 8
	const perWriter = 5
	errCh := make(chan string, writers*perWriter*4)
	// Buffer for the worst case (every attempt reports): no receiver runs
	// until wg.Wait returns, so an undersized channel deadlocks the very
	// contention this test exists to create.
	revCh := make(chan uint64, writers*perWriter*30)
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			who := fmt.Sprintf("cliH%d", w)
			var rev uint64
			for b := range perWriter {
				marker := fmt.Sprintf("w%db%d;", w, b)
				body := fmt.Sprintf(`{"base":%d,"ops":[{"t":"ins","i":0,"s":%q}]}`, rev, marker)
				ok := false
				deadline := time.Now().Add(30 * time.Second)
				attempts := 0
				for !ok && time.Now().Before(deadline) {
					code, out, err := postOpRaw(client, baseURL, docID, who, body)
					if err != nil {
						errCh <- fmt.Sprintf("w%d b%d transport: %v", w, b, err)
						return
					}
					if r, _ := out["rev"].(float64); r > 0 {
						rev = uint64(r)
						revCh <- rev
					}
					switch code {
					case http.StatusOK:
						ok = true
					case http.StatusConflict:
						body = fmt.Sprintf(`{"base":%d,"ops":[{"t":"ins","i":0,"s":%q}]}`, rev, marker)
						// Back off so a slow worker stops losing every race
						// to hotter spinners (a fixed attempt count starves
						// under contention — found the flaky way).
						attempts++
						time.Sleep(time.Duration(min(attempts*2, 20)) * time.Millisecond)
					default:
						errCh <- fmt.Sprintf("w%d b%d status = %d", w, b, code)
						return
					}
				}
				if !ok {
					errCh <- fmt.Sprintf("w%d b%d never accepted", w, b)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	close(revCh)
	for e := range errCh {
		t.Fatalf("hammer: %s", e)
	}
	var maxRev uint64
	for r := range revCh {
		if r > maxRev {
			maxRev = r
		}
	}
	code, out := postOp(t, client, baseURL, docID, "cliFinal",
		fmt.Sprintf(`{"base":%d,"ops":[{"t":"ins","i":0,"s":"END"}]}`, maxRev))
	if code != http.StatusOK {
		t.Fatalf("final op: status = %d: %v", code, out)
	}
	text, _ := out["text"].(string)
	for w := range writers {
		for b := range perWriter {
			if marker := fmt.Sprintf("w%db%d;", w, b); !strings.Contains(text, marker) {
				t.Fatalf("hammer lost %q in %q", marker, text)
			}
		}
	}
}
