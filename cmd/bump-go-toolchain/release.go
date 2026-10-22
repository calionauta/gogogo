// SCOPE:core — the tool's whole network edge: the release feed and the
// per-release tarball.
//
// Both live here, behind `online`, so the rest of the tool does not know a
// network exists and the test can point the edge at an httptest server. That
// matters because the scheduled workflow's detect step is the part most worth
// testing — it runs unattended once a week — and it cannot be tested through a
// hardcoded URL. No package globals: the struct is built in main and passed
// down, so two tests can run in parallel without fighting over an endpoint.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// feedURL is the Go release feed: no token, no API budget, and the same
	// endpoint install.sh already reads, so there is one convention for "the
	// newest Go" in this repo.
	feedURL = "https://go.dev/dl/?mode=json"

	// releaseURL is one release's SOURCE tarball. It is arch-independent, so a
	// HEAD on it answers the question this tool actually has — "does this
	// version exist?" — for any line, not just the newest patch the feed
	// still lists. A `--to` that names a version nobody published would
	// otherwise pin the tree to a toolchain every CI job then fails to
	// download.
	releaseURL = "https://go.dev/dl/go%s.src.tar.gz"

	// feedTimeout bounds each request.
	feedTimeout = 30 * time.Second
)

// online is the network edge. The fields are the endpoints, not settings the
// user can move: a test overrides them to an httptest server, and nothing else
// does.
type online struct {
	client  *http.Client
	feed    string
	release string
}

// newOnline returns the edge pointed at go.dev.
func newOnline() *online {
	return &online{
		client:  &http.Client{Timeout: feedTimeout},
		feed:    feedURL,
		release: releaseURL,
	}
}

// latestPatch returns the newest published patch of the given release line
// ("1.27"), or "" when the line has none.
func (o *online) latestPatch(ctx context.Context, line string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.feed, nil)
	if err != nil {
		return "", fmt.Errorf("build the release-feed request: %w", err)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("read the Go release feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the Go release feed answered %s", resp.Status)
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read the Go release feed: %w", err)
	}
	return parseLatestPatch(payload, line)
}

// checkPublished fails unless the release exists, so a typo in an explicit
// target stops before the first byte is written instead of pinning the tree to
// a toolchain that cannot be downloaded.
//
// HEAD on the source tarball, not a lookup in the feed: the feed only lists the
// newest patches of the recent lines, so it cannot answer for a deliberate pin
// to an older patch, and a 404 is the honest answer to "does this exist?".
func (o *online) checkPublished(ctx context.Context, version string) error {
	url := fmt.Sprintf(o.release, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return fmt.Errorf("build the release request: %w", err)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot confirm Go %s is published, so nothing was written: %w", version, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("%s is not a published Go release (%s answers 404) — nothing was written; "+
			"-detect prints the newest patch of the current line", version, url)
	default:
		return fmt.Errorf("go.dev answered %s for Go %s, so the release is unconfirmed — nothing was written",
			resp.Status, version)
	}
}

// goRelease is one entry of the go.dev release feed.
type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// parseLatestPatch picks the newest patch release of the given release line
// out of the feed. It is pure, so most of the test never touches the network.
//
// The feed names a release "go<major>.<minor>.<patch>", so the line "1.27"
// matches the prefix "go1.27.".
func parseLatestPatch(payload []byte, line string) (string, error) {
	var releases []goRelease
	if err := json.Unmarshal(payload, &releases); err != nil {
		return "", fmt.Errorf("parse the Go release feed: %w", err)
	}

	prefix := "go" + line + "."
	best := ""
	bestPatch := -1
	for _, r := range releases {
		if !strings.HasPrefix(r.Version, prefix) {
			continue
		}
		rest := strings.TrimPrefix(r.Version, prefix)
		patch, err := strconv.Atoi(rest)
		if err != nil {
			continue // not a plain patch release (a rc, a beta)
		}
		if patch > bestPatch {
			bestPatch = patch
			best = line + "." + strconv.Itoa(patch)
		}
	}
	return best, nil
}
