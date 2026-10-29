// SCOPE:core - The vendored Pele bundle's version is pinned by test.
package resources

import (
	"strings"
	"testing"
)

// pinnedPele is the Pele version this repo vendors for Mermaid diagrams.
// Bump it in the same commit that replaces web/resources/static/pele.global.js.
const pinnedPele = "1.12.0"

// TestPeleVersionIsPinned keeps the asset and the constant in step: the
// bundle's own banner is the source of truth, replacing the file without
// the version (or the reverse) is a red build.
func TestPeleVersionIsPinned(t *testing.T) {
	t.Parallel()
	raw, err := StaticFS().Open("pele.global.js")
	if err != nil {
		t.Fatalf("open embedded pele.global.js: %v", err)
	}
	defer func() { _ = raw.Close() }()
	buf := make([]byte, 64)
	n, _ := raw.Read(buf)
	banner := string(buf[:n])
	if !strings.Contains(banner, "Pele "+pinnedPele) {
		t.Errorf("vendored Pele is not %s:\n banner: %q\nUpdate pinnedPele together with the file.",
			pinnedPele, banner)
	}
}

// TestDiagramHelperPresent pins the mount helper the diagram component
// depends on: the template emits <pre class="mermaid"> and diagram.js owns
// the Pele.mount call plus the MutationObserver for SSE patches.
func TestDiagramHelperPresent(t *testing.T) {
	t.Parallel()
	data, err := readStatic("diagram.js")
	if err != nil {
		t.Fatalf("read diagram.js: %v", err)
	}
	for _, want := range []string{"pre.mermaid", "pele.mount", "MutationObserver"} {
		if !strings.Contains(data, want) {
			t.Errorf("diagram.js missing %q — the diagram component will not mount", want)
		}
	}
}
