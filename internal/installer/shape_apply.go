// Scaffold-time shape application: the clone ships every target, and the
// declared shape is what turns it into a web app, an API, a desktop shell, or
// the full template.
package installer

import (
	"fmt"
	"io"
	"os"
	"path"
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
}

// evolutionEntrypoints are the builder's two surfaces, by the names they carry
// in the TEMPLATE. A generated product never ships them: `cmd/gogogo` and
// `cmd/gogogo-mcp` rewrite a source tree (`scaffold_apply`, `recipe_install`)
// that a product's users do not have.
//
// Pruned BEFORE the rename, deliberately. After it, the pair is
// `cmd/<product>` and `cmd/<product>-mcp`, and that second name is exactly the
// consumer MCP's new name — pruning then cannot tell them apart. Before it, the
// two names are distinct and unambiguous.
//
// They are NOT recorded in `removed:`. That list is the shape's decision
// ("web keeps cmd/web, drops the native shells"); the builder is removed from
// every shape, so listing it would restate a constant as if it were a choice —
// and the paths would name nothing on disk once the rename had run.
var evolutionEntrypoints = []string{"cmd/gogogo", "cmd/gogogo-mcp"}

// applyShapeIfNeeded prunes entrypoints the chosen shape does not ship and
// records it. This runs on the SCAFFOLD path only, so `full` is not a no-op:
// a generated product named `full` still must not carry the evolution pair
// (`cmd/gogogo`, `cmd/gogogo-mcp`), which rewrites source trees its users do not
// have. The template's own tree never passes through here.
func applyShapeIfNeeded(opt options, stdout io.Writer) error {
	if opt.shape == "" {
		opt.shape = string(shapekit.Full)
	}
	cfg, err := pruneShapeEntrypoints(opt.dir, opt.shape)
	if err != nil {
		return err
	}
	// `api` is the one shape with a runtime consequence, so the operator hears
	// how it is enforced: the same cmd/web answers 404 for page paths.
	if cfg.Shape == shapekit.API {
		fmt.Fprintln(stdout, "gogogo: shape api — entrypoints pruned; page routes answer 404 at runtime")
		fmt.Fprintln(stdout, "        (set APP_SHAPE=api for a deployed binary; the record is read otherwise)")
	}
	// Recorded here, not by the caller: the record is the contract the gate
	// reads, and a caller that pruned without recording would leave the tree
	// claiming `full` while three entrypoints were gone — exactly the failure
	// `make check-shape` exists to catch.
	return writeShapeRecord(opt.dir, cfg)
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
// pruneShapeEntrypoints removes the dirs the shape does not ship and returns
// what it removed, WITHOUT writing the record. `writeShapeRecord` does that
// later, after the rename: the record must name the paths the tree has NOW, so
// `product` is the name the consumer MCP carries once scaffolding finishes.
func pruneShapeEntrypoints(root, raw string) (shapekit.Config, error) {
	cfg, err := shapekit.Parse(shapekit.Config{Shape: shapekit.Shape(raw)})
	if err != nil {
		return shapekit.Config{}, err
	}
	// The record may name the consumer MCP by its PLACEHOLDER
	// (`cmd/product-mcp`, written by shapekit.Parse) or by the product's own
	// name, depending on whether this runs before or after the rename. Compare
	// by ROLE, not by string: a literal comparison silently kept nothing and
	// then reported the survivor in `removed:`, which left the same path in
	// `entrypoints:` and `removed:` at once.
	keep := map[string]bool{}
	for _, ep := range cfg.Entrypoints {
		keep[ep] = true
		if sameEntrypointRole(ep, shapekit.CmdProductMCP) {
			keep[shapekit.CmdProductMCP] = true
		}
	}
	// The builder is never part of a product, whatever the shape.
	if err := removeAllPresent(root, evolutionEntrypoints); err != nil {
		return shapekit.Config{}, err
	}
	var removed []string
	for _, ep := range shapeEntrypoints {
		if keep[ep] || keep[placeholderOf(ep)] {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(ep))
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			continue
		}
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return shapekit.Config{}, fmt.Errorf("shape %s: remove %s: %w", cfg.Shape, ep, rmErr)
		}
		removed = append(removed, placeholderOf(ep))
	}
	cfg.Removed = removed
	return cfg, nil
}

// writeShapeRecord persists the shape the tree actually ended up with. Called
// after the rename, so the paths it records are the ones on disk.
func writeShapeRecord(root string, cfg shapekit.Config) error {
	return shapekit.Write(root, cfg)
}

// applyShape is prune-then-record, for callers that are not scaffolding a
// product (a shape applied to an already-renamed tree).
func applyShape(root, raw string) (shapekit.Config, error) {
	cfg, err := pruneShapeEntrypoints(root, raw)
	if err != nil {
		return shapekit.Config{}, err
	}
	if err := writeShapeRecord(root, cfg); err != nil {
		return shapekit.Config{}, err
	}
	return cfg, nil
}

// sameEntrypointRole reports whether two entrypoint paths address the consumer
// MCP, whatever name each carries: `cmd/product-mcp` before the rename,
// `cmd/<product>-mcp` after.
func sameEntrypointRole(a, b string) bool {
	return strings.HasSuffix(a, "-mcp") && path.Base(a) != "gogogo-mcp" &&
		strings.HasSuffix(b, "-mcp")
}

// placeholderOf normalizes an entrypoint path to the template's own spelling,
// so the record a scaffold writes never names the product's variant.
func placeholderOf(ep string) string {
	if sameEntrypointRole(ep, shapekit.CmdProductMCP) {
		return shapekit.CmdProductMCP
	}
	return ep
}

// removeAllPresent deletes the paths that exist and ignores the rest.
func removeAllPresent(root string, paths []string) error {
	for _, ep := range paths {
		dir := filepath.Join(root, filepath.FromSlash(ep))
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", ep, err)
		}
	}
	return nil
}

// removeEvolutionPair deletes the builder's two surfaces from a tree about to
// become a product. Called on the scaffold path, before the rename.
func removeEvolutionPair(root string) error {
	return removeAllPresent(root, evolutionEntrypoints)
}
