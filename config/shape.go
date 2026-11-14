// The product shape at runtime.
//
// A shape is a scaffold-time record, but `api` has a runtime consequence: the
// same cmd/web must stop serving page routes. APP_SHAPE is the deploy override;
// reading .gogogo/shape.yml is the zero-config path for a developer running the
// tree from its root. A binary deployed without either is `full`, which keeps
// every existing tree working.
package config

import (
	"log/slog"
	"slices"
	"strings"

	shapekit "github.com/calionauta/gogogo/internal/shape"
)

// loadShapeEnv fills AppShape. It is called from loadPluginEnv so the top-level
// Load keeps its statement budget.
func (c *Config) loadShapeEnv() {
	raw := strings.TrimSpace(getEnv("APP_SHAPE", ""))
	if raw == "" {
		if record, err := shapekit.Load("."); err == nil && record.Declared {
			raw = string(record.Shape)
		}
	}
	if raw == "" {
		raw = string(shapekit.Full)
	}
	shape := shapekit.Shape(raw)
	if slices.Contains(shapekit.All, shape) {
		c.AppShape = raw
		return
	}
	slog.Warn("config: unknown APP_SHAPE, using full", "shape", raw)
	c.AppShape = string(shapekit.Full)
}
