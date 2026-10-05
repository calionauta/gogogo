// SCOPE:layer=infra,removal=plugin — installer engine: latest-release check (update nag, no auto-update)
package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// githubAPI is the release source. Overridable in tests (httptest).
var githubAPI = "https://api.github.com"

// latestTag asks the releases API for the newest tag (v-prefixed).
// Pure read: it never downloads or installs anything — updating stays a
// human decision (install.sh re-run or a new binary).
func latestTag(ctx context.Context, client *http.Client, api, owner, repo string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	url := strings.TrimSuffix(api, "/") + "/repos/" + owner + "/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("releases API: %s", resp.Status)
	}
	var doc struct {
		Tag string `json:"tag_name"` //nolint:tagliatelle // GitHub API uses snake_case
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", err
	}
	if doc.Tag == "" {
		return "", errors.New("releases API: empty tag_name")
	}
	return doc.Tag, nil
}

// printUpdateCheck compares the running binary against the latest release.
// Behind --check-update: read-only, no filesystem touched.
func printUpdateCheck(ctx context.Context, w io.Writer) error {
	latest, err := latestTag(ctx, nil, githubAPI, "calionauta", "gogogo")
	if err != nil {
		return &ExitError{code: 1, msg: fmt.Sprintf("gogogo: update check failed (%v)", err)}
	}
	mine := strings.TrimSpace(Version)
	if mine == "" || mine == "dev" {
		fmt.Fprintf(w, "gogogo: running %s; latest release is %s\n", mine, latest)
		return nil
	}
	if strings.TrimPrefix(mine, "v") == strings.TrimPrefix(latest, "v") {
		fmt.Fprintf(w, "gogogo: %s — up to date\n", mine)
		return nil
	}
	fmt.Fprintf(w, "gogogo: %s installed, %s available — re-run install.sh or download the release\n", mine, latest)
	return nil
}
