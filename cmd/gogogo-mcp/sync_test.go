package main

import (
	"context"
	"strings"
	"testing"
)

// TestSyncPrerequisites pins the shared guidance: the tool answers without a
// tree, names the leaf listener and the token, and points at the doc. A tool
// that silently returned an empty string would still pass a "no error" check.
func TestSyncPrerequisites(t *testing.T) {
	res, _, err := handleSyncPrerequisites(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(t, res)
	for _, want := range []string{"NATS_LEAFNODE_LISTEN", "NATS_TOKEN", "CLOUD_SYNC_URL", "desktop-mobile.md#cloud-sync"} {
		if !strings.Contains(text, want) {
			t.Errorf("guidance missing %q", want)
		}
	}
}
