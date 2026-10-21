// SCOPE:plugin — installer engine: latest-release check (advisory, read-only)
//
// The check answers ONE question with TWO halves, because they drift
// independently: the binary's release and the project's recorded template
// revision. A tree scaffolded months ago on a current binary is the common
// case, and comparing only the binary reports it as up to date.
//
// Nothing here mutates a tree or a binary. Updating stays a human decision
// (re-run install.sh, or `gogogo migrate` for the project). It is also never
// automatic: there is no background call at boot, so a machine without
// network, a CI runner and an agent session all behave identically — and no
// notifier opt-out variable is needed, because nothing ever nags.
package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// githubAPI is the release source. Overridable in tests (httptest).
var githubAPI = "https://api.github.com"

// updateCacheTTL is how long a resolved release tag stays usable. A day
// matches what `gh` and npm's update-notifier settled on: fresh enough to
// inform, rare enough to stay far below the API's 60 requests/hour for
// anonymous callers — which a shared host or a chatty agent would otherwise
// reach in minutes.
const updateCacheTTL = 24 * time.Hour

// Permissions for the cache entry. It holds no secret, but a read-only check
// has no business writing a world-readable file either.
const (
	cacheDirMode  = 0o755
	cacheFileMode = 0o600
)

// Seams for tests: the clock and the cache root.
var (
	nowFunc      = time.Now
	userCacheDir = os.UserCacheDir
)

// latestTag asks the releases API for the newest tag.
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
	// GitHub rejects requests without a User-Agent. No credential here on
	// purpose: the cache means at most one anonymous request per machine per
	// day, far under the 60/hour ceiling, and this repo routes every secret
	// through config rather than ad-hoc env reads (check-secrets-env enforces
	// that, and it was right to flag this).
	req.Header.Set("User-Agent", "gogogo/"+Version)
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

// updateCache is what a successful lookup leaves on disk. The repo is part of
// the entry because a project scaffolded from a FORK must be compared against
// that fork's releases, not upstream's — a single-slot cache would answer the
// fork's question with the template's tag, which is worse than "unknown".
type updateCache struct {
	Repo      string `json:"repo"`
	Tag       string `json:"tag"`
	CheckedAt string `json:"checkedAt"`
}

// updateCachePath is where the resolved tag lives, or "" when the OS gives no
// cache directory (then every call is a live lookup: slower, still correct).
func updateCachePath() string {
	root, err := userCacheDir()
	if err != nil || root == "" {
		return ""
	}
	return filepath.Join(root, "gogogo", "update-check.json")
}

// readUpdateCache returns a cached tag and whether it is still fresh.
func readUpdateCache(repo string) (updateCache, bool) {
	path := updateCachePath()
	if path == "" {
		return updateCache{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return updateCache{}, false
	}
	var c updateCache
	if uerr := json.Unmarshal(raw, &c); uerr != nil || c.Tag == "" || c.Repo != repo {
		return updateCache{}, false
	}
	at, err := time.Parse(time.RFC3339, c.CheckedAt)
	if err != nil {
		return updateCache{}, false
	}
	return c, nowFunc().Sub(at) < updateCacheTTL
}

// writeUpdateCache records a resolved tag. Best effort: an unwritable cache
// directory must never fail a read-only check.
func writeUpdateCache(repo, tag string) string {
	at := nowFunc().UTC().Format(time.RFC3339)
	path := updateCachePath()
	if path == "" {
		return at
	}
	if err := os.MkdirAll(filepath.Dir(path), cacheDirMode); err != nil {
		return at
	}
	raw, err := json.Marshal(updateCache{Repo: repo, Tag: tag, CheckedAt: at})
	if err != nil {
		return at
	}
	_ = os.WriteFile(path, raw, cacheFileMode)
	return at
}

// latestTagCached resolves the latest release, serving a fresh cache entry when
// there is one and falling back to a STALE entry when the network is gone.
//
// The stale fallback is deliberate: "0.54.0 was the latest when I last looked"
// beats "unknown" offline, as long as the source field says so — which is why
// the caller gets the source, not just the tag.
func latestTagCached(ctx context.Context, repo string) (tag, source, checkedAt string, err error) {
	if c, fresh := readUpdateCache(repo); fresh {
		return c.Tag, "cache", c.CheckedAt, nil
	}
	// A STALE entry is still an entry: readUpdateCache's bool is freshness, not
	// existence, and conflating the two made the offline fallback dead code
	// (the test that pins this is TestCheckUpdateFallsBackToStaleCacheOffline).
	cached, _ := readUpdateCache(repo)
	owner, name, _ := strings.Cut(repo, "/")
	tag, err = latestTag(ctx, nil, githubAPI, owner, name)
	if err != nil {
		if cached.Tag != "" {
			return cached.Tag, "cache-stale", cached.CheckedAt, nil
		}
		return "", "none", "", err
	}
	return tag, "github", writeUpdateCache(repo, tag), nil
}

// updateReport is the machine-readable verdict. Both halves carry their own
// status because they answer different questions: installed vs latest is "is my
// tool current?", tree vs latest is "is my project's scaffold current?".
type updateReport struct {
	Installed  string `json:"installed"`
	Latest     string `json:"latest,omitempty"`
	Status     string `json:"binaryStatus"`
	Tree       string `json:"tree,omitempty"`
	TreeRepo   string `json:"treeRepo,omitempty"`
	TreeCommit string `json:"treeCommit,omitempty"`
	TreeLatest string `json:"treeLatest,omitempty"`
	TreeStatus string `json:"treeStatus,omitempty"`
	Note       string `json:"note,omitempty"`
	Source     string `json:"source"`
	CheckedAt  string `json:"checkedAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

// splitRepo splits an "owner/repo@tag" marker into its repo and tag halves.
func splitRepo(ref string) (repo, tag string) {
	repo, tag, ok := strings.Cut(ref, "@")
	if !ok {
		return ref, ""
	}
	return repo, tag
}

// templateRepoRef is the tool's own repo, the fixed half of every comparison.
const templateRepoRef = templateOwner + "/" + templateRepo

// checkUpdate builds the report. dir is the project to inspect, or "" when the
// caller only wants the binary compared.
func checkUpdate(ctx context.Context, dir string) updateReport {
	rep := updateReport{Installed: Version}
	treeRepo, treeTag := "", ""
	if dir != "" {
		if m := readTemplateMarker(dir); m.Found {
			rep.Tree, rep.TreeCommit = m.Ref, m.Commit
			treeRepo, treeTag = splitRepo(m.Ref)
		}
	}
	// The binary half is the tool's own release train, always.
	tag, source, checked, err := latestTagCached(ctx, templateRepoRef)
	rep.Source, rep.CheckedAt = source, checked
	rep.Latest = tag
	if err != nil {
		rep.Error = err.Error()
		rep.Status = statusUnknown
		return rep
	}
	rep.Status = versionStatus(rep.Installed, tag)
	rep.TreeStatus = treeStatusOf(ctx, &rep, treeRepo, treeTag, tag)
	return rep
}

// treeStatusOf compares the recorded base against the latest release of the
// repo that base came from. A project scaffolded from a fork is NOT behind
// because upstream shipped — comparing it against the template's releases
// would produce a confident wrong answer, which is the one outcome a version
// check must never produce.
func treeStatusOf(ctx context.Context, rep *updateReport, repo, tag, binaryLatest string) string {
	if tag == "" {
		return ""
	}
	if repo == "" || repo == templateRepoRef {
		rep.TreeRepo = ""
		return versionStatus(tag, binaryLatest)
	}
	latest, _, _, err := latestTagCached(ctx, repo)
	if err != nil {
		// Two causes share this path (no releases at all, or the API said no),
		// so the note carries the error instead of guessing which one it was.
		rep.Note = "recorded base is " + repo + "; its latest release could not be " +
			"read (" + err.Error() + ") — compare against that repo by hand"
		return statusUnknown
	}
	rep.TreeRepo, rep.TreeLatest = repo, latest
	return versionStatus(tag, latest)
}

// behind reports whether either half is older than the latest release.
func (r updateReport) behind() bool {
	return r.Status == statusBehind || r.TreeStatus == statusBehind
}

// updateExitCode is the code the CLI exits with: 3 when an update exists, and
// 0 for every other verdict — including "could not check", because a read-only
// advisory must not fail a pipeline when the network is down. 3 rather than 2
// because this repo already spends 2 on "the proof build failed".
func (r updateReport) updateExitCode() int {
	if r.behind() {
		return 3
	}
	return 0
}

// printUpdateCheck renders the report and applies the exit code.
func printUpdateCheck(ctx context.Context, w io.Writer, format, dir string) error {
	rep := checkUpdate(ctx, dir)
	if format == planFormatJSON {
		raw, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(raw))
	} else {
		printUpdateText(w, rep)
	}
	if code := rep.updateExitCode(); code != 0 {
		return &ExitError{code: code}
	}
	return nil
}

// printUpdateText is the human rendering: one line per half, plus the next step
// when there is one. Deliberately short — a version check that needs reading is
// a version check nobody reads.
func printUpdateText(w io.Writer, rep updateReport) {
	if rep.Error != "" {
		fmt.Fprintf(w, "gogogo: %s installed; could not reach the release API (%s)\n",
			rep.Installed, rep.Error)
		if rep.Tree != "" {
			fmt.Fprintf(w, "gogogo: this project records %s — comparison unavailable offline\n", rep.Tree)
		}
		return
	}
	fmt.Fprintf(w, "gogogo: binary %s, latest release %s — %s\n",
		rep.Installed, rep.Latest, rep.Status)
	if rep.Tree == "" {
		fmt.Fprintln(w, "gogogo: no scaffolded-from marker here (not a scaffolded "+
			"project, or a tree that predates the marker) — nothing to compare")
		return
	}
	fmt.Fprintf(w, "gogogo: project scaffolded from %s — %s", rep.Tree, rep.TreeStatus)
	if rep.Note != "" {
		fmt.Fprintf(w, "\ngogogo: %s", rep.Note)
	}
	if rep.TreeLatest != "" {
		fmt.Fprintf(w, " (latest in %s: %s)", rep.TreeRepo, rep.TreeLatest)
	}
	if rep.TreeStatus == statusBehind {
		fmt.Fprint(w, " (run `gogogo migrate` to see what changed since)")
	}
	fmt.Fprintln(w)
}
