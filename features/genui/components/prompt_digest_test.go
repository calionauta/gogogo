// SCOPE:plugin — the prompt that reaches the model carries the user's real data.
//
// The todo feature proves a ROW reaches the digest (features/todo/handlers).
// This proves the WIRING: composePrompt concatenates that digest into the
// prompt each leg sends. Without it, a refactor could drop the concatenation and
// every other test would stay green while Ask and Chat silently went back to
// inventing the user's data — the original reported bug.
package components

import (
	"context"
	"strings"
	"testing"
)

// TestBothLegsComposeRealDataIntoThePrompt is the wiring proof.
//
// It calls the SAME composePrompt each Respond calls, so removing the
// `+ dataContextBlock(...)` from either leg fails this test.
func TestBothLegsComposeRealDataIntoThePrompt(t *testing.T) {
	// The summarizing stub is installed once in TestMain (the registry is
	// install-once), so its digest is a known constant here.
	const summary = digestStubSummary
	ctx := context.Background()
	const owner = "wiring-owner"

	ask := llmResponder{owner: owner}.composePrompt(ctx, "plan my day")
	if !strings.Contains(ask, "Current data from this app") {
		t.Errorf("Ask prompt has no data section:\n%s", ask)
	}
	if !strings.Contains(ask, summary) {
		t.Errorf("Ask prompt lost the digest text:\n%s", ask)
	}

	chat := chatLLMResponder{owner: owner}.composePrompt(ctx, "plan my day")
	if !strings.Contains(chat, "Current data from this app") {
		t.Errorf("Chat prompt has no data section:\n%s", chat)
	}
	if !strings.Contains(chat, summary) {
		t.Errorf("Chat prompt lost the digest text:\n%s", chat)
	}
}

// TestComposedPromptWithoutOwnerHasNoDataSection pins the omission: no owner
// means nothing to fetch, so the block is absent rather than an empty heading
// that would tell the model the user has no data at all.
func TestComposedPromptWithoutOwnerHasNoDataSection(t *testing.T) {
	got := llmResponder{}.composePrompt(context.Background(), "hi")
	if strings.Contains(got, "Current data from this app") {
		t.Errorf("ownerless prompt carries a data section:\n%s", got)
	}
}
