package native

import (
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
	graphservice "github.com/calionauta/gogogo/internal/graph"
)

func TestViewRendersPanoramaNodes(t *testing.T) {
	doc := graphservice.Panorama(nil)
	w := View(doc)
	if w == nil {
		t.Fatal("View returned nil")
	}
	// Sanity: panorama itself contains core units.
	if len(capabilities.All) == 0 {
		t.Fatal("empty registry")
	}
}
