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
//
// The evolution pair is in this list so a PRODUCT never ships it. `cmd/gogogo`
// and `cmd/gogogo-mcp` rewrite a source tree — `scaffold_apply`, `recipe_install`
// — and a generated product's users have no checkout to rewrite. Shipping them
// is the inversion the consumer surface exists to fix: an agent driving a
// deployed product would need the builder installed. The pair is never in a
// shape's keep list, so every shape prunes it.
var shapeEntrypoints = []string{
	"cmd/web", "cmd/desktop", "cmd/gui", "cmd/mobile", shapekit.CmdProductMCP,
	"cmd/gogogo", "cmd/gogogo-mcp",
}

// applyShapeIfNeeded prunes entrypoints the chosen shape does not ship and
// records it. This runs on the SCAFFOLD path only, so `full` is not a no-op:
// a generated product named `full` still must not carry the evolution pair
// (`cmd/gogogo`, `cmd/gogogo-mcp`), which rewrites source trees its users do not
// have. The template's own tree never passes through here.
func applyShapeIfNeeded(opt options, stdout io.Writer) error {
	if opt.shape == "" {
		opt.shape = string(shapekit.Full)
	}
	cfg, err := applyShape(opt.dir, opt.shape)
	if err != nil {
		return err
	}
	// `api` is the one shape with a runtime consequence, so the operator hears
	// how it is enforced: the same cmd/web answers 404 for page paths.
	if cfg.Shape == shapekit.API {
		fmt.Fprintln(stdout, "gogogo: shape api — entrypoints pruned; page routes answer 404 at runtime")
		fmt.Fprintln(stdout, "        (set APP_SHAPE=api for a deployed binary; the record is read otherwise)")
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
