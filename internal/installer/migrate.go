// SCOPE:plugin — installer engine: the migration range as data
//
// `gogogo migrate` answers "what changed between the revision this project
// records and the latest release?", and nothing else. It never edits the tree.
//
// WHY DATA AND NOT A MERGE. A scaffolded project is a copy: it renames the
// module, trims units, and edits freely. Every file-merge design for that
// shape ends the same way — copier's maintainers exclude deleted paths by
// hand, cruft's users discover there is no common ancestor to merge against
// and abandon the update, and Rails' per-file overwrite prompt tracks nothing
// at all. The mechanism that survives arbitrary user edits is a transform the
// user (or an agent) applies deliberately, with the repo's own gates proving
// the result. So this command produces the LIST, and `--check` / the make
// gates produce the PROOF; there is no `migrate --apply`, on purpose.
//
// WHERE THE LIST COMES FROM. CHANGELOG.md, which release-please already
// generates and nobody hand-writes. That is the point: a curated migration
// document would be a second thing to maintain and the first thing to rot,
// while the changelog is a release artifact that is correct by construction.
package installer

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// rawBase serves the changelog as plain text at a pinned ref. Overridable in
// tests (httptest).
var rawBase = "https://raw.githubusercontent.com"

// maxChangelogBytes caps the fetched changelog: a release file is tens of
// kilobytes, and an unbounded read of a redirect target is not worth the trust.
const maxChangelogBytes = 4 << 20

// migrationGates are the checks that prove a migrated tree actually conforms.
// Fixed strings rather than generated advice: they are the repo's own
// documented tiers, and a gate list that drifts from local-ci.md is worse than
// no list.
var migrationGates = []string{
	"gogogo --check --dir .",
	"make check-scope && make check-docs",
	"make ci-local-fast",
}

// migrationRelease is one version section of the changelog.
type migrationRelease struct {
	Version string   `json:"version"`
	Date    string   `json:"date,omitempty"`
	Entries []string `json:"entries"`
}

// migrationNotesIndex is the curated index of release notes for migrations that
// change structure rather than add a feature. Read at the tag being migrated to.
const migrationNotesIndex = "docs/migrations/index.md"

// errRawNotFound marks a raw fetch that answered 404, which for an optional
// file is an answer rather than a failure.
var errRawNotFound = errors.New("not found")

// migrationNote points a migrating project at the curated note for a release
// whose changes a changelog bullet cannot carry.
type migrationNote struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Summary string `json:"summary,omitempty"`
}

// migrationReport is the whole answer, in the shape an agent can act on.
type migrationReport struct {
	From     string             `json:"from"`
	To       string             `json:"to"`
	Repo     string             `json:"repo"`
	Source   string             `json:"source"`
	Releases []migrationRelease `json:"releases"`
	Notes    []migrationNote    `json:"notes,omitempty"`
	Gates    []string           `json:"gates"`
	Note     string             `json:"note,omitempty"`
}

// releaseHeadingRE matches a version section header, anchored to the version
// itself because release-please also emits "## What's Changed" inside every
// section — matching any H2 would turn that heading into a release.
//
// It accepts BOTH shapes this changelog has used, because a parser that knows
// only the current one silently drops history: a real CHANGELOG.md here has
// "## 0.53.0 (2026-10-21)" (release-please, github type) AND 81 sections of
// "## [0.43.0] - 2026-10-10" (the older Keep-a-Changelog form), plus
// "## [0.1.0] - Initial release" with no date at all. The trailing group is
// deliberately loose; releaseDateRE pulls a date out of it when there is one.
var releaseHeadingRE = regexp.MustCompile(
	`^##[ \t]+\[?v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)\]?[ \t]*(.*)$`)

// releaseDateRE finds the date inside a heading's remainder, whichever
// separator the section uses.
var releaseDateRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// parseChangelog extracts the version sections, newest first (file order).
func parseChangelog(text string) []migrationRelease {
	var out []migrationRelease
	var cur *migrationRelease
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := releaseHeadingRE.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out = append(out, migrationRelease{Version: m[1], Date: releaseDateRE.FindString(m[2])})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		entry := strings.TrimSpace(line)
		// Only bullets. The "**Full Changelog**" line and the
		// "## What's Changed" sub-heading are release chrome, not changes.
		if !strings.HasPrefix(entry, "* ") && !strings.HasPrefix(entry, "- ") {
			continue
		}
		cur.Entries = append(cur.Entries, strings.TrimSpace(entry[2:]))
	}
	return out
}

// notesIndexRE reads one line of the curated index: "- v0.44.0 — summary". The
// version may carry a v, the separator is optional, and everything after it is
// the summary a human wrote.
var notesIndexRE = regexp.MustCompile(`^[-*][ \t]+v?(\d+\.\d+\.\d+)[ \t]*[-—–:]?[ \t]*(.*)$`)

// parseNotesIndex builds version -> note from the index. An index that lists
// nothing yields an empty map, which is the normal state: only releases that
// change structure get a note.
func parseNotesIndex(text string) map[string]migrationNote {
	out := make(map[string]migrationNote)
	for line := range strings.SplitSeq(text, "\n") {
		m := notesIndexRE.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		out[m[1]] = migrationNote{
			Version: m[1],
			Path:    "docs/migrations/v" + m[1] + ".md",
			Summary: strings.TrimSpace(m[2]),
		}
	}
	return out
}

// notesInRange attaches the curated notes that fall inside the range. The index
// is optional, so a 404 is silence and an unreadable index becomes a sentence in
// the report instead of a failure: the release list is still a true answer.
func notesInRange(
	ctx context.Context, repo, tag, localPath string, releases []migrationRelease,
) ([]migrationNote, string) {
	if len(releases) == 0 {
		return nil, ""
	}
	text, err := readNotesIndex(ctx, repo, tag, localPath)
	if err != nil {
		if errors.Is(err, errRawNotFound) {
			return nil, ""
		}
		return nil, "migration notes could not be read (" + err.Error() + ")"
	}
	index := parseNotesIndex(text)
	notes := make([]migrationNote, 0, len(releases))
	for _, r := range releases {
		if n, ok := index[r.Version]; ok {
			notes = append(notes, n)
		}
	}
	return notes, ""
}

// readNotesIndex reads the index from --notes when given, else from the pinned
// tag, so an offline migration can carry its own copy.
func readNotesIndex(ctx context.Context, repo, tag, path string) (string, error) {
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return fetchRaw(ctx, repo, tag, migrationNotesIndex)
}

// migrationRange keeps the releases strictly newer than from and no newer
// than to, newest first. An incomparable bound (a branch name, "dev") filters
// nothing rather than silently dropping everything.
//
// SORTED, not file order. The file is normally newest-first, but this repo's
// own changelog has a tail section out of place ("## [0.1.0] - Initial
// release" sits above 0.24.10), so a range that trusted file order would print
// a migration list in an order nobody can read.
func migrationRange(releases []migrationRelease, from, to string) []migrationRelease {
	out := make([]migrationRelease, 0, len(releases))
	for _, r := range releases {
		if cmp, ok := versionOrder(r.Version, from); ok && cmp <= 0 {
			continue
		}
		if to != "" {
			if cmp, ok := versionOrder(r.Version, to); ok && cmp > 0 {
				continue
			}
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		cmp, ok := versionOrder(out[i].Version, out[j].Version)
		return ok && cmp > 0
	})
	return out
}

// tagRef normalizes a version to the tag form the repo actually uses.
func tagRef(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// fetchChangelog reads the changelog at a pinned tag.
func fetchChangelog(ctx context.Context, repo, tag string) (string, error) {
	return fetchRaw(ctx, repo, tag, "CHANGELOG.md")
}

// fetchRaw reads one file from the repo at a pinned tag. A 404 comes back
// wrapped in errRawNotFound so callers can tell "this file does not exist" from
// "the network or the ref is unusable".
func fetchRaw(ctx context.Context, repo, tag, path string) (string, error) {
	url := strings.TrimSuffix(rawBase, "/") + "/" + repo + "/" + tag + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "gogogo/"+Version)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s: %w", url, errRawNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChangelogBytes))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// resolveMigrationBase picks the base revision: an explicit --from wins, and
// otherwise the tree's own marker is used — which is the whole point of
// recording it.
func resolveMigrationBase(dir, from string) (string, string, error) {
	if strings.TrimSpace(from) != "" {
		return from, templateOwner + "/" + templateRepo, nil
	}
	if _, err := os.Stat(dir); err != nil {
		return "", "", &ExitError{code: 1, msg: fmt.Sprintf(
			"gogogo: migrate: directory %s not found — nothing to read a base from", dir)}
	}
	m := readTemplateMarker(dir)
	if !m.Found {
		return "", "", &ExitError{code: 1, msg: "gogogo: migrate needs a base revision — " +
			"pass --from TAG, or run it inside a project scaffolded by a version that " +
			"records scaffolded-from: in AGENTS.md"}
	}
	repo, tag, ok := strings.Cut(m.Ref, "@")
	if !ok || tag == "" || tag == "dev" {
		return "", "", &ExitError{code: 1, msg: fmt.Sprintf(
			"gogogo: migrate: the recorded base %q names no release tag "+
				"(dev build) — pass --from TAG", m.Ref)}
	}
	return tag, repo, nil
}

// runMigrate is the subcommand entry point.
func runMigrate(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gogogo migrate", flag.ContinueOnError)
	from := fs.String("from", "", "base revision to start after (default: the project's recorded scaffolded-from tag)")
	to := fs.String("to", "", "newest revision to include (default: the latest release)")
	dir := fs.String("dir", ".", "project checkout to read the base marker from")
	format := fs.String("format", planFormatText, "output format: text|json")
	changelogPath := fs.String("changelog", "", "read the changelog from this file instead of the network (offline)")
	notesPath := fs.String("notes", "", "read the migration-notes index from this file instead of the network (offline)")
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintln(stdout, `gogogo migrate — what changed between your project's base and latest.

Read-only: it prints the releases and the gates that prove conformance.
There is no --apply; the template never merges into your tree.

  gogogo migrate --dir .
  gogogo migrate --from v0.50.0 --to v0.53.0 --format json
  gogogo migrate --dir . --changelog ./CHANGELOG.md
  gogogo migrate --dir . --notes ./docs/migrations/index.md`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, repo, err := resolveMigrationBase(*dir, *from)
	if err != nil {
		return err
	}
	target := strings.TrimSpace(*to)
	if target == "" {
		// The latest of the repo this project came FROM: a fork's migration
		// range must not default to upstream's tag, or the changelog URL and
		// the range would disagree.
		tag, _, _, lerr := latestTagCached(ctx, repo)
		if lerr != nil {
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"gogogo: migrate: could not resolve the latest release (%v) — "+
					"pass --to TAG", lerr)}
		}
		target = tag
	}

	text, source, err := readChangelogText(ctx, repo, target, *changelogPath)
	if err != nil {
		return err
	}
	rep := migrationReport{
		From:     strings.TrimPrefix(base, "v"),
		To:       target,
		Repo:     repo,
		Source:   source,
		Releases: migrationRange(parseChangelog(text), base, target),
		Gates:    migrationGates,
	}
	var notesDiag string
	rep.Notes, notesDiag = notesInRange(ctx, repo, tagRef(target), *notesPath, rep.Releases)
	if notesDiag != "" {
		rep.Note = strings.TrimSpace(rep.Note + " " + notesDiag)
	}
	if len(rep.Releases) == 0 {
		// Name the source: with --changelog pointing at a stale file, "you are
		// current" would be a claim about the file, not about upstream.
		rep.Note = "no newer releases between " + rep.From + " and " + target +
			" in " + source
	}
	if *format == planFormatJSON {
		raw, merr := json.MarshalIndent(rep, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Fprintln(stdout, string(raw))
		return nil
	}
	printMigrationText(stdout, rep)
	return nil
}

// readChangelogText reads the changelog from --changelog when given (offline,
// or a local clone), else from the pinned tag on the network.
func readChangelogText(ctx context.Context, repo, target, path string) (text, source string, err error) {
	if path != "" {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return "", "", &ExitError{code: 1, msg: "gogogo: migrate: " + rerr.Error()}
		}
		return string(raw), "file:" + path, nil
	}
	tag := tagRef(target)
	text, err = fetchChangelog(ctx, repo, tag)
	if err != nil {
		return "", "", &ExitError{code: 1, msg: fmt.Sprintf(
			"gogogo: migrate: could not read %s/CHANGELOG.md at %s (%v) — "+
				"retry online, or pass --changelog PATH", repo, tag, err)}
	}
	return text, repo + "@" + tag, nil
}

// printMigrationText renders the report for a human: newest release first, one
// bullet per change, and the proof commands last (they are the point).
func printMigrationText(w io.Writer, rep migrationReport) {
	if len(rep.Releases) == 0 {
		fmt.Fprintf(w, "gogogo: %s\n", rep.Note)
		return
	}
	entries := 0
	for _, r := range rep.Releases {
		entries += len(r.Entries)
	}
	fmt.Fprintf(w, "gogogo: %s → %s — %d release(s), %d change(s), from %s\n",
		rep.From, strings.TrimPrefix(rep.To, "v"), len(rep.Releases), entries, rep.Source)
	for _, r := range rep.Releases {
		header := r.Version
		if r.Date != "" {
			header += " (" + r.Date + ")"
		}
		fmt.Fprintf(w, "\n%s\n", header)
		for _, e := range r.Entries {
			fmt.Fprintf(w, "  * %s\n", e)
		}
	}
	if len(rep.Notes) > 0 {
		fmt.Fprintln(w, "\nstructural notes in this range:")
		for _, n := range rep.Notes {
			line := "  " + tagRef(n.Version) + "  " + n.Path
			if n.Summary != "" {
				line += " — " + n.Summary
			}
			fmt.Fprintln(w, line)
		}
	}
	if rep.Note != "" {
		fmt.Fprintf(w, "\ngogogo: %s\n", rep.Note)
	}
	fmt.Fprintln(w, "\nprove the result with:")
	for _, g := range rep.Gates {
		fmt.Fprintf(w, "  %s\n", g)
	}
}
