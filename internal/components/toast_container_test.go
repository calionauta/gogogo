// SCOPE:core — ToastContainer render test.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestToastContainerContract pins what every toast-patching page needs:
// the #toast-container target plus the entrance/progress CSS the toast
// component depends on. A page rendering toasts without this renders
// errors instead (PatchElementsNoTargetsFound, silent to the user).
func TestToastContainerContract(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := components.ToastContainer().Render(context.Background(), &buf); err != nil {
		t.Fatalf("ToastContainer: %v", err)
	}
	for _, want := range []string{
		`id="toast-container"`,
		`.toast-msg`,
		`.toast-timer-bar`,
		`toast-shrink`,
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("ToastContainer missing %q", want)
		}
	}
}
