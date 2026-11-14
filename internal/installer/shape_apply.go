// Scaffold-time shape application: the clone ships every target, and the
// declared shape is what turns it into a web app, an API, a desktop shell, or
// the full template.
package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	shapekit "github.com/calionauta/gogogo/internal/shape"
)

// shapeEntrypoints are the dirs a shape may keep. They are the only targets the
// clone carries, so "remove what the shape does not ship" is a closed walk.
var shapeEntrypoints = []string{"cmd/web", "cmd/desktop", "cmd/gui", "cmd/mobile"}

// applyShapeIfNeeded prunes entrypoints the chosen shape does not ship and
// records it. `full` (the default) is a no-op: a template clone already ships
// every target, and writing a record that only restates the default would make
// every tree look like a decision was taken.
func applyShapeIfNeeded(opt options, stdout io.Writer) error {
	if opt.shape == "" || shapekit.Shape(opt.shape) == shapekit.Full {
		return nil
	}
	cfg, err := applyShape(opt.dir, opt.shape)
	if err != nil {
		return err
	}
	// `api` is the one shape whose name promises more than the entrypoint prune
	// delivers, so the operator hears it from the tool, not from a doc.
	if cfg.Shape == shapekit.API {
		fmt.Fprintln(stdout, "gogogo: shape api — entrypoints pruned; page routes are NOT removed")
		fmt.Fprintln(stdout, "        (the server.PageRoutes seam does not exist yet; see docs/shape.md)")
	}
	return nil
}

func shapeNames() string {
	names := make([]string, 0, len(shapekit.All))
	for _, s := range shapekit.All {
		names = append(names, string(s))
	}
	return strings.Join(names, "|")
}

// applyShape prunes the entrypoints the shape does not ship and writes the
// record. It returns the config it wrote, so the plan and the report print the
// same answer the gate will read.
//
// A shape never deletes non-entrypoint code: `web` drops cmd/desktop|gui|mobile
// but keeps features/ and internal/. Page routes are not conditional on a shape
// yet (`api` needs the server.PageRoutes seam), so the caller warns about that
// instead of pretending the shape removed them.
func applyShape(root, raw string) (shapekit.Config, error) {
	cfg, err := shapekit.Parse(shapekit.Config{Shape: shapekit.Shape(raw)})
	if err != nil {
		return shapekit.Config{}, err
	}
	keep := map[string]bool{}
	for _, ep := range cfg.Entrypoints {
		keep[ep] = true
	}
	var removed []string
	for _, ep := range shapeEntrypoints {
		if keep[ep] {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(ep))
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			continue
		}
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return shapekit.Config{}, fmt.Errorf("shape %s: remove %s: %w", cfg.Shape, ep, rmErr)
		}
		removed = append(removed, ep)
	}
	cfg.Removed = removed
	if err := shapekit.Write(root, cfg); err != nil {
		return shapekit.Config{}, err
	}
	return cfg, nil
}
