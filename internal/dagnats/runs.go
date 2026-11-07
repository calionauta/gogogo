// Live-run reads for the DagNats REST API: GET /runs and GET /runs/{id}.
//
// The graph unit overlays these onto the static workflow defs, so the canvas
// shows which step is running/completed/failed right now instead of the DAG
// shape alone. Types here mirror the wire (snake_case); the graph domain
// converts them into its own statuses.
package dagnats

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// RunStepStatus is one step's live state inside a run.
type RunStepStatus struct {
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
	Error    string `json:"error,omitempty"`
}

// RunSummary is one row of GET /runs: identity, run status, and per-step
// statuses keyed by step id.
//
//nolint:tagliatelle // DagNats wire is snake_case.
type RunSummary struct {
	RunID      string                   `json:"run_id"`
	WorkflowID string                   `json:"workflow_id"`
	Status     string                   `json:"status"`
	CreatedAt  string                   `json:"created_at"`
	Steps      map[string]RunStepStatus `json:"steps"`
}

// ListRuns fetches the newest runs (optionally filtered by workflow name).
// limit <= 0 asks the engine for its default page. The engine caps the row
// count server-side, so a missing limit never scans the whole store.
func (c *Client) ListRuns(ctx context.Context, workflow string, limit int) ([]RunSummary, error) {
	q := url.Values{}
	if workflow != "" {
		q.Set("workflow", workflow)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	urlStr := c.baseURL + "/runs"
	if len(q) > 0 {
		urlStr += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("dagnats: build list runs: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dagnats: list runs: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dagnats: list runs: status %d: %s", resp.StatusCode, string(body))
	}
	var runs []RunSummary
	if err := json.Unmarshal(body, &runs); err != nil {
		return nil, fmt.Errorf("dagnats: decode runs: %w", err)
	}
	return runs, nil
}

// GetRunSummary returns one run with per-step statuses, for overlaying a
// single run's steps onto its def. Superseded by ListRuns for the graph
// (which wants every recent run at once), but kept for callers that already
// hold a run id.
func (c *Client) GetRunSummary(ctx context.Context, runID string) (RunSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/runs/"+runID, nil)
	if err != nil {
		return RunSummary{}, fmt.Errorf("dagnats: build get run: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return RunSummary{}, fmt.Errorf("dagnats: get run: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return RunSummary{}, fmt.Errorf("dagnats: get run: status %d: %s", resp.StatusCode, string(body))
	}
	var run RunSummary
	if err := json.Unmarshal(body, &run); err != nil {
		return RunSummary{}, fmt.Errorf("dagnats: decode run: %w", err)
	}
	return run, nil
}
