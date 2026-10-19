// SCOPE:plugin — the app's real data as prompt context for the model.
//
// WHY THIS EXISTS. A panel renders the user's rows into the ANSWER, but it
// cannot help the model REASON about them. Asked to "plan my days from to-dos",
// the model must know what the todos are before it can write a plan — and
// without them the only thing it can do is invent titles (the live report). The
// panel prompt rule forbids inventing; this gives the model something true to
// use instead. The two are complementary and both are needed: the rule stops
// fabrication, the context makes the honest answer possible.
//
// This is NOT the panel's render path and not a second source of truth: it is a
// short, bounded, read-only digest fetched at answer time from the same
// provider that fills the panel, so a change to the store shows up in both.
package components

import (
	"context"
	"strings"
)

// PanelSummarizer is the OPTIONAL half of a panel's contract: a provider that
// implements it contributes a short factual digest of the viewer's data to the
// model's prompt.
//
// Optional on purpose. A panel that renders is complete without it; a summary
// is only worth adding when the model can do something useful with the
// contents (planning, prioritizing, summarizing), and a panel whose data is
// too large or too sensitive to digest simply does not implement this. The
// type assertion in DataContext is the whole registration mechanism, so a
// provider opts in by adding one method.
//
// The digest MUST be bounded (the caller caps it) and factual — no
// instructions, no framing, because it is concatenated into a prompt.
type PanelSummarizer interface {
	// Summary returns a short digest of what this panel would render for
	// owner, or "" when there is nothing to say (an empty store is a fact the
	// caller omits rather than states).
	Summary(ctx context.Context, owner string) (string, error)
}

// maxDataContextBytes bounds the digest carried into a prompt.
//
// 2 KiB is about 30 short todo titles: enough for the model to reason about a
// realistic list, small enough that it cannot dominate the prompt or the bill
// (the digest rides EVERY message, and the model leg's cost is per token). A
// list longer than this is truncated, and the header says so — a silent
// truncation would let the model plan for a subset while believing it saw all.
const maxDataContextBytes = 2048

// DataContext returns one prompt block describing the viewer's own data, or ""
// when no installed panel can summarize (or every one of them is empty).
//
// It reads the process registry, like Hydrate: the panels the model was told
// about are the panels that can contribute, so the prompt and the vocabulary
// cannot disagree. A provider error is skipped rather than fatal — the answer
// is still worth making with the panels that worked, and Hydrate reports the
// same failure to the user.
func DataContext(ctx context.Context, owner string) (string, error) {
	names := panelNames()
	if len(names) == 0 || owner == "" {
		return "", nil
	}
	var b strings.Builder
	for _, name := range names {
		provider := panelProvider(name)
		if provider == nil {
			continue
		}
		summarizer, ok := provider.(PanelSummarizer)
		if !ok {
			continue
		}
		summary, err := summarizer.Summary(ctx, owner)
		if err != nil {
			return b.String(), err
		}
		if strings.TrimSpace(summary) == "" {
			continue
		}
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(summary)
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "", nil
	}
	return truncateDataContext(b.String()), nil
}

// truncateDataContext applies the byte cap, cutting at a line boundary so the
// digest never ends mid-title, and marks the cut explicitly.
func truncateDataContext(s string) string {
	if len(s) <= maxDataContextBytes {
		return s
	}
	cut := s[:maxDataContextBytes]
	if nl := strings.LastIndexByte(cut, '\n'); nl > 0 {
		cut = cut[:nl]
	}
	return cut + "\n(truncated — more rows exist than are listed here)\n"
}
