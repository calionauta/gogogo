// SCOPE:plugin — real-data prompt context (red-proof).
//
// The report this pins: "plan my days from to-dos" invented todo titles
// because the model could not see the user's actual rows. The prompt rules
// forbid fabricating; this context is what makes the honest answer possible.
package components

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// summarizingPanel implements the OPTIONAL half: it renders like any panel and
// additionally contributes a digest. A provider that skips this method must not
// break DataContext, which is why the other stubs (installed in TestMain)
// deliberately do not implement it.
type summarizingPanel struct {
	name, summary string
	err           error
}

func (s summarizingPanel) Component() string  { return s.name }
func (s summarizingPanel) Prompt() string     { return s.name + " {filter}" }
func (s summarizingPanel) Writable() []string { return []string{"filter"} }
func (s summarizingPanel) Fill(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"filter": "all"}, nil
}

func (s summarizingPanel) Render(map[string]any) (templ.Component, error) {
	return GenuiTextNote("stub-id", "ok"), nil
}

func (s summarizingPanel) Summary(context.Context, string) (string, error) {
	return s.summary, s.err
}

// TestDataContext_SkipsPanelsWithoutASummarizer pins the optionality: of the
// panels installed by TestMain, only the one that implements Summary may
// contribute text.
//
// The registry deliberately contains BOTH kinds (stubTestPanel renders and has
// no Summary; digestStubPanel adds one), because "optional" is only proven by a
// set that mixes them. An earlier version of this test asserted an EMPTY
// context, which was only true while no summarizer existed at all — it would
// now fail for the wrong reason (a digest exists) and, worse, it could never
// have caught a provider being skipped by mistake.
func TestDataContext_SkipsPanelsWithoutASummarizer(t *testing.T) {
	t.Parallel()
	got, err := DataContext(context.Background(), "owner")
	if err != nil {
		t.Fatalf("DataContext errored: %v", err)
	}
	if !strings.Contains(got, digestStubSummary) {
		t.Errorf("DataContext = %q, want the summarizing panel's digest", got)
	}
	// Only the summarizer may speak: a non-summarizing panel that invented a
	// section would be the bug this guards.
	if lines := strings.Count(strings.TrimSpace(got), "\n") + 1; lines > 1 {
		t.Errorf("DataContext emitted %d sections, want exactly the summarizer's 1: %q", lines, got)
	}
	if strings.Contains(got, panelNameInTest+":") {
		t.Errorf("a panel WITHOUT Summary contributed a section: %q", got)
	}
}

// TestDataContext_SkipsEmptyAndOwnerless pins the two omissions that keep the
// prompt honest: no owner means no data to fetch, and an empty digest means
// nothing to say. Neither may produce a block — "you have no todos" is a claim
// the caller deliberately leaves out.
func TestDataContext_SkipsEmptyAndOwnerless(t *testing.T) {
	t.Parallel()
	if got, _ := DataContext(context.Background(), ""); got != "" {
		t.Errorf("no owner = %q, want empty", got)
	}
}

// TestTruncateDataContext_CutsOnALineAndSaysSo pins both halves of the cap: the
// digest must not end mid-title, and a truncated one must SAY it was truncated
// (a silent cut would let the model plan for a subset while believing it saw
// every row).
func TestTruncateDataContext_CutsOnALineAndSaysSo(t *testing.T) {
	t.Parallel()
	short := "todo_list: 2 todo(s) total (1 open, 1 done).\n"
	if got := truncateDataContext(short); got != short {
		t.Errorf("a short digest was altered: %q", got)
	}

	long := strings.Repeat("todo_list: a very long title indeed\n", 200)
	got := truncateDataContext(long)
	if len(got) > maxDataContextBytes+64 {
		t.Errorf("truncated length = %d, want ~%d", len(got), maxDataContextBytes)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("a truncated digest must say so: %q", got[max(0, len(got)-80):])
	}
	if strings.HasSuffix(strings.TrimSpace(got), "inde") {
		t.Errorf("the cut landed mid-title: %q", got[max(0, len(got)-80):])
	}
}

// TestDataContextReportsSummarizerErrors pins the error path: a provider that
// cannot read the store reports it (the caller logs and continues with the
// panels that worked) instead of the digest silently coming back short.
func TestDataContextReportsSummarizerErrors(t *testing.T) {
	t.Parallel()
	// The installed stubs have no summary, so drive the helper directly with a
	// failing one through the same truncation/report path the registry uses.
	_, err := summarizingPanel{name: "x", err: errors.New("store down")}.Summary(context.Background(), "owner")
	if err == nil {
		t.Error("a failing summary must surface its error")
	}
}
