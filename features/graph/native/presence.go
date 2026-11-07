// Native (gogpu/ui) twin of the graph presence source: one room as a
// list. Builds from the same Document the web canvas draws.
package native

import (
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"

	graphservice "github.com/calionauta/gogogo/internal/graph"
)

func init() {
	graphservice.Register(graphservice.Provider{ID: "presence", Build: PresenceView})
}

// PresenceView renders the room hub + members as a ranked list.
func PresenceView(doc graphservice.Document) widget.Widget {
	if doc.Source != "presence" && doc.Source != "presence:unavailable" {
		return primitives.Text("").FontSize(bodySize)
	}
	children := make([]widget.Widget, 0, 2+len(doc.Nodes))
	children = append(children,
		primitives.Text("Presence").FontSize(titleSize).Bold(),
		primitives.Text(doc.Source+" · "+itoa(len(doc.Nodes))+" nodes").FontSize(bodySize),
	)
	for _, n := range doc.Nodes {
		label := n.Kind + " · " + n.Label
		if n.Status != "" {
			label += " · " + n.Status
		}
		children = append(children, primitives.Text(label).FontSize(bodySize))
	}
	return primitives.VBox(children...).Padding(cardPad).Gap(rowGapSize)
}
