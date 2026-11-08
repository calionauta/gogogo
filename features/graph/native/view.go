// Native (gogpu/ui) twin of the graph page: lists the panorama nodes the
// web canvas draws as a graph. Builders run on the UI thread and read a
// snapshot document, never live state — same rule as cmd/gui/views.go.
package native

import (
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"

	graphservice "github.com/calionauta/gogogo/internal/graph"
)

func init() {
	graphservice.Register(graphservice.Provider{ID: "graph", Build: View})
}

// UI metrics (mirror cmd/gui/views.go names; kept local so the native
// provider carries no import of cmd/gui).
const (
	titleSize  = 28
	bodySize   = 14
	cardPad    = 24
	rowGapSize = 8
)

// View renders the panorama as a simple ranked list (kind groups). The web
// canvas draws the same Document as a graph; this is the native reading of
// the same data, so both frontends answer "what is installed" identically.
func View(doc graphservice.Document) widget.Widget {
	children := make([]widget.Widget, 0, 2+len(doc.Nodes))
	children = append(children,
		primitives.Text("System panorama").FontSize(titleSize).Bold(),
		primitives.Text(doc.Source+" · "+itoa(len(doc.Nodes))+" nodes").FontSize(bodySize),
	)
	for _, n := range doc.Nodes {
		children = append(children,
			primitives.Text(n.Kind+" · "+n.Label).FontSize(bodySize),
		)
	}
	return primitives.VBox(children...).Padding(cardPad).Gap(rowGapSize)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
