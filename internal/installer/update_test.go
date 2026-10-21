package installer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	// (json/os/filepath are used by the report tests below)
)

func TestLatestTagParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer srv.Close()
	tag, err := latestTag(context.Background(), srv.Client(), srv.URL, "o", "r")
	if err != nil {
		t.Fatalf("latestTag: %v", err)
	}
	if tag != "v9.9.9" {
		t.Errorf("tag = %q, want v9.9.9", tag)
	}
}

func TestLatestTagFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := latestTag(context.Background(), srv.Client(), srv.URL, "o", "r"); err == nil {
		t.Error("non-200 must fail fast")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := latestTag(ctx, nil, "http://127.0.0.1:1", "o", "r"); err == nil {
		t.Error("unreachable API must fail fast")
	}
}

// --- the report: two halves, four statuses, one exit code ---

// withCacheRoot points the cache at a temp dir and restores it after.
func withCacheRoot(t *testing.T, root string) {
	t.Helper()
	old := userCacheDir
	userCacheDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { userCacheDir = old })
}

// withVersion pins the "installed" version for one test (the binary's value is
// a link-time variable, so the tests set it the way the linker would).
func withVersion(t *testing.T, v string) {
	t.Helper()
	old := Version
	Version = v
	t.Cleanup(func() { Version = old })
}

// writeCacheFixture writes a cache entry for the known-next release, checked at
// the given time.
func writeCacheFixture(t *testing.T, checkedAt time.Time) {
	t.Helper()
	if at := writeUpdateCacheAt(templateRepoRef, "v0.54.0", checkedAt); at == "" {
		t.Fatal("cache root not writable")
	}
}

// writeUpdateCacheAt is the test-side writer: the production writer stamps
// nowFunc, which is exactly what a "stale entry" fixture must not do.
func writeUpdateCacheAt(repo, tag string, checkedAt time.Time) string {
	path := updateCachePath()
	if path == "" {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ""
	}
	raw, err := json.Marshal(updateCache{
		Repo: repo, Tag: tag, CheckedAt: checkedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return ""
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return ""
	}
	return checkedAt.UTC().Format(time.RFC3339)
}

// TestCheckUpdateUsesFreshCacheWithoutNetwork is the point of the cache: the
// shared-host case, where an anonymous caller gets 60 requests/hour and an
// agent session could exhaust that in minutes.
func TestCheckUpdateUsesFreshCacheWithoutNetwork(t *testing.T) {
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.53.0")
	oldAPI := githubAPI
	githubAPI = "http://127.0.0.1:1" // any network call would fail loudly
	t.Cleanup(func() { githubAPI = oldAPI })
	writeCacheFixture(t, time.Now())

	rep := checkUpdate(context.Background(), "")
	if rep.Source != "cache" {
		t.Errorf("source = %q, want cache", rep.Source)
	}
	if rep.Latest != "v0.54.0" {
		t.Errorf("latest = %q, want v0.54.0", rep.Latest)
	}
	if rep.Status != statusBehind {
		t.Errorf("status = %q, want behind", rep.Status)
	}
	if rep.Error != "" {
		t.Errorf("unexpected error: %s", rep.Error)
	}
}

// TestCheckUpdateFallsBackToStaleCacheOffline: a stale entry beats "unknown"
// offline, as long as it is labelled as stale.
func TestCheckUpdateFallsBackToStaleCacheOffline(t *testing.T) {
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.53.0")
	oldAPI := githubAPI
	githubAPI = "http://127.0.0.1:1"
	t.Cleanup(func() { githubAPI = oldAPI })
	writeCacheFixture(t, time.Now().Add(-48*time.Hour))

	rep := checkUpdate(context.Background(), "")
	if rep.Source != "cache-stale" {
		t.Errorf("source = %q, want cache-stale", rep.Source)
	}
	if rep.Latest != "v0.54.0" || rep.Status != statusBehind {
		t.Errorf("stale fallback = %+v", rep)
	}
}

// TestCheckUpdateWithoutNetworkOrCacheIsUnknown: the read-only check must not
// invent a verdict when it cannot see the releases.
func TestCheckUpdateWithoutNetworkOrCacheIsUnknown(t *testing.T) {
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.53.0")
	oldAPI := githubAPI
	githubAPI = "http://127.0.0.1:1"
	t.Cleanup(func() { githubAPI = oldAPI })

	rep := checkUpdate(context.Background(), "")
	if rep.Status != statusUnknown {
		t.Errorf("status = %q, want unknown", rep.Status)
	}
	if rep.updateExitCode() != 0 {
		t.Errorf("unknown must exit 0, got %d", rep.updateExitCode())
	}
}

// TestCheckUpdateComparesTheProjectToo is the bug the marker exists to fix: a
// current binary used to report a months-old tree as current.
func TestCheckUpdateComparesTheProjectToo(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFixture(t, dir, "calionauta/gogogo@v0.50.0")
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.54.0")
	writeCacheFixture(t, time.Now())

	rep := checkUpdate(context.Background(), dir)
	if rep.Status != statusCurrent {
		t.Errorf("binary status = %q, want current", rep.Status)
	}
	if rep.TreeStatus != statusBehind {
		t.Errorf("tree status = %q, want behind", rep.TreeStatus)
	}
	if code := rep.updateExitCode(); code != 3 {
		t.Errorf("exit code = %d, want 3 (tree behind)", code)
	}
}

// TestCheckUpdateCurrentTreeNeedsNothing guards the other direction: no false
// alarm on a project that already records the latest release.
func TestCheckUpdateCurrentTreeNeedsNothing(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFixture(t, dir, "calionauta/gogogo@v0.54.0")
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.54.0")
	writeCacheFixture(t, time.Now())

	rep := checkUpdate(context.Background(), dir)
	if rep.TreeStatus != statusCurrent {
		t.Errorf("tree status = %q, want current", rep.TreeStatus)
	}
	if code := rep.updateExitCode(); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

// TestCheckUpdateJSONIsTheAgentContract: --format json is the only surface an
// agent consumes, so it must parse and carry both halves.
func TestCheckUpdateJSONIsTheAgentContract(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFixture(t, dir, "calionauta/gogogo@v0.50.0")
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.54.0")
	writeCacheFixture(t, time.Now())

	var buf strings.Builder
	err := printUpdateCheck(context.Background(), &buf, planFormatJSON, dir)
	if ExitCode(err) != 3 {
		t.Fatalf("exit code = %d, want 3", ExitCode(err))
	}
	var rep updateReport
	if uerr := json.Unmarshal([]byte(buf.String()), &rep); uerr != nil {
		t.Fatalf("json output does not parse: %v\n%s", uerr, buf.String())
	}
	if rep.Installed == "" || rep.Latest == "" || rep.Tree == "" {
		t.Errorf("json is missing a half: %+v", rep)
	}
	if rep.TreeStatus != statusBehind {
		t.Errorf("treeStatus = %q, want behind", rep.TreeStatus)
	}
}

// TestCheckUpdateTextSaysNothingToCompare keeps the quiet case quiet: on a
// non-scaffolded path the text says so instead of implying drift.
func TestCheckUpdateTextSaysNothingToCompare(t *testing.T) {
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.54.0")
	writeCacheFixture(t, time.Now())

	var buf strings.Builder
	if err := printUpdateCheck(context.Background(), &buf, planFormatText, t.TempDir()); err != nil {
		t.Fatalf("unexpected exit: %v", err)
	}
	if !strings.Contains(buf.String(), "nothing to compare") {
		t.Errorf("text should name the missing marker:\n%s", buf.String())
	}
}

// writeMarkerFixture writes the smallest AGENTS.md carrying a base marker.
func writeMarkerFixture(t *testing.T, dir, ref string) {
	t.Helper()
	body := "# AGENTS\n\n## Trim provenance\n\nscaffolded-from: " + ref +
		"\ntemplate-commit: unknown\nremoved: none\nremoved-paths: none\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCheckUpdateComparesAForkAgainstItsOwnRepo is the gap a single-slot cache
// and a hardcoded upstream would hide: a project scaffolded from a FORK must be
// judged against that fork's releases. Comparing it to upstream's produces a
// confident wrong answer, the one thing a version check must never do.
func TestCheckUpdateComparesAForkAgainstItsOwnRepo(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFixture(t, dir, "someone/their-fork@v1.0.0")
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.54.0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Template repo: 0.54.0 (so the binary is current). Fork: 2.0.0.
		tag := "v0.54.0"
		if strings.HasPrefix(r.URL.Path, "/repos/someone/their-fork/") {
			tag = "v2.0.0"
		}
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
	defer srv.Close()
	oldAPI := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = oldAPI })

	rep := checkUpdate(context.Background(), dir)
	if rep.Status != statusCurrent {
		t.Errorf("binary status = %q, want current", rep.Status)
	}
	if rep.TreeRepo != "someone/their-fork" || rep.TreeLatest != "v2.0.0" {
		t.Fatalf("fork half not resolved: %+v", rep)
	}
	if rep.TreeStatus != statusBehind {
		t.Errorf("tree status = %q, want behind (base v1.0.0, fork latest v2.0.0)", rep.TreeStatus)
	}
	if rep.updateExitCode() != 3 {
		t.Errorf("exit code = %d, want 3", rep.updateExitCode())
	}
}

// TestCheckUpdateForkWithoutReleasesIsUnknown: a fork may have no releases at
// all. That is "unknown", never "current" — and it must say why.
func TestCheckUpdateForkWithoutReleasesIsUnknown(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFixture(t, dir, "someone/quiet-fork@v1.0.0")
	withCacheRoot(t, t.TempDir())
	withVersion(t, "0.54.0")
	writeCacheFixture(t, time.Now())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/someone/quiet-fork/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.54.0"}`))
	}))
	defer srv.Close()
	oldAPI := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = oldAPI })

	rep := checkUpdate(context.Background(), dir)
	if rep.TreeStatus != statusUnknown {
		t.Errorf("tree status = %q, want unknown", rep.TreeStatus)
	}
	if !strings.Contains(rep.Note, "someone/quiet-fork") {
		t.Errorf("the note must name the repo it could not read: %q", rep.Note)
	}
	if rep.updateExitCode() != 0 {
		t.Errorf("unknown must not ask for an update, got exit %d", rep.updateExitCode())
	}
}

// TestCacheEntryIsPerRepo pins the other half of the fork gap: a template entry
// must never answer a fork's lookup.
func TestCacheEntryIsPerRepo(t *testing.T) {
	withCacheRoot(t, t.TempDir())
	writeCacheFixture(t, time.Now()) // template repo only
	if _, fresh := readUpdateCache("someone/their-fork"); fresh {
		t.Error("a template cache entry answered a fork lookup")
	}
	if _, fresh := readUpdateCache(templateRepoRef); !fresh {
		t.Error("the template's own entry must still be fresh")
	}
}
