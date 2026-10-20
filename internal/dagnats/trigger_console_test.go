// SCOPE:plugin — regression test for the DagNats console's first-trigger
// path (upstream #745, fixed in v0.0.27).
package dagnats

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestConsoleCreatesFirstTriggerOnEmptyBucket pins the upstream fix this
// template used to work around by seeding a disabled placeholder trigger:
// the console's Create action lists the triggers bucket before writing, and
// on a fresh install that bucket has no keys. DagNats v0.0.24 returned
// jetstream.ErrNoKeysFound from that list, so the console answered 500 and
// the first trigger could never be created.
//
// The engine boots against a fresh data dir (no seed, no bootstrap) and the
// create is driven through the real console form endpoint. A regression here
// means the workaround is needed again — see upstream danmestas/dagnats#745.
func TestConsoleCreatesFirstTriggerOnEmptyBucket(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "dagnats-fresh")
	_, httpAddr := startTestServerWithAddr(t, dataDir)
	base := "http://" + httpAddr

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	create, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/console/triggers", strings.NewReader(url.Values{
			"id":          {"first-cron"},
			"workflow_id": {"onboarding"},
			"type":        {"cron"},
			"config":      {"0 3 * * *"},
		}.Encode()))
	if err != nil {
		t.Fatalf("build create request: %v", err)
	}
	create.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(create)
	if err != nil {
		t.Fatalf("create first trigger: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create first trigger: status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("create first trigger: unexpected body: %s", body)
	}

	list, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/console/triggers", nil)
	if err != nil {
		t.Fatalf("build list request: %v", err)
	}
	listResp, err := http.DefaultClient.Do(list)
	if err != nil {
		t.Fatalf("list triggers: %v", err)
	}
	defer listResp.Body.Close()
	listBody, _ := io.ReadAll(listResp.Body)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list triggers: status %d: %s", listResp.StatusCode, listBody)
	}
	if !strings.Contains(string(listBody), "first-cron") {
		t.Fatalf("created trigger missing from the console list: %s", listBody)
	}
}
