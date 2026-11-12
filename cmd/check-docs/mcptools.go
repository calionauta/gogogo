package main

// The MCP tool tables are GENERATED from the catalog the server serves.
//
// Why: the tool set used to live in three places — the runtime description
// (what an agent reads at call time), the table in docs/mcp.md, and the table
// in cmd/gogogo-mcp/README.md. Three copies of one prose drift, and they did:
// #405 taught `recipe_describe` to publish a recipe's metrics and outcomes,
// and the docs row still promised only the verbs. One source, generated
// downstream, makes that class impossible.
//
// The catalog lives in a SEPARATE module (cmd/gogogo-mcp has its own go.mod,
// so the MCP SDK never enters the web binary's graph), so this reads it as
// SOURCE, the same way the subcommand list is read from the installer
// entrypoint: go/parser, not a regex, so a description split across
// concatenated string literals is read whole.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// mcpTool is one tool as the catalog declares it.
type mcpTool struct {
	name   string
	desc   string
	params string
	effect string
}

// mcpToolsFile is the catalog's path, relative to the module root.
const mcpToolsFile = "cmd/gogogo-mcp/catalog.go"

// The generated blocks. The markers are the contract: a file without them
// cannot be checked, and a file with a table outside them is not the
// generated one.
const (
	mcpToolsStart = "<!-- MCP:TOOLS:START -->"
	mcpToolsEnd   = "<!-- MCP:TOOLS:END -->"
)

// mcpToolsDocs are the files carrying a generated table, each with the shape
// it needs: the docs page carries the full description (a reference of the
// served surface), the module README carries the short verdict (a README
// cell that holds a paragraph is a README nobody reads).
type mcpToolsDoc struct {
	file   string
	render func([]mcpTool) string
}

var mcpToolsDocs = []mcpToolsDoc{
	{"docs/mcp.md", mcpToolsDocsTable},
	{"cmd/gogogo-mcp/README.md", mcpToolsReadmeTable},
}

// mcpTools reads the catalog and returns its tools in declaration order.
func mcpTools(root string) ([]mcpTool, error) {
	path := filepath.Join(root, filepath.FromSlash(mcpToolsFile))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", mcpToolsFile, err)
	}
	var out []mcpTool
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "toolCatalog" {
				continue
			}
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				return nil, fmt.Errorf("%s: toolCatalog is not a composite literal", mcpToolsFile)
			}
			for _, elem := range lit.Elts {
				tool, err := parseMCPTool(elem)
				if err != nil {
					return nil, err
				}
				out = append(out, tool)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no tools found — the catalog parser broke", mcpToolsFile)
	}
	return out, nil
}

// parseMCPTool reads one catalog entry: the keyed fields the docs need.
func parseMCPTool(elem ast.Expr) (mcpTool, error) {
	lit, ok := elem.(*ast.CompositeLit)
	if !ok {
		return mcpTool{}, fmt.Errorf("%s: a tool entry is not a composite literal", mcpToolsFile)
	}
	var tool mcpTool
	for _, kv := range lit.Elts {
		pair, ok := kv.(*ast.KeyValueExpr)
		if !ok {
			continue // the `add` closure
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "name", "desc", "params", "effect":
			value, ok := literalString(pair.Value)
			if !ok {
				return mcpTool{}, fmt.Errorf("%s: tool field %q is not a literal string", mcpToolsFile, key.Name)
			}
			switch key.Name {
			case "name":
				tool.name = value
			case "desc":
				tool.desc = value
			case "params":
				tool.params = value
			case "effect":
				tool.effect = value
			}
		}
	}
	return tool, nil
}

// literalString reads a string literal, joining the concatenated pieces a
// long description is written as.
func literalString(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		left, lok := literalString(v.X)
		right, rok := literalString(v.Y)
		if !lok || !rok {
			return "", false
		}
		return left + right, true
	}
	return "", false
}

// mcpToolsDocsTable renders the docs page's table: the tool, whether it
// changes anything, its parameters, and the tool's own headline — the first
// sentence of the description the server advertises at call time.
//
// The headline, not the whole description: a table cell that holds a
// paragraph is a cell that cannot wrap (a bare URL has no break
// opportunity), and the site-layout gate measures exactly that. The complete
// description is one click away — `gogogo-mcp --help` prints it — and the
// catalog holds it.
func mcpToolsDocsTable(tools []mcpTool) string {
	var b strings.Builder
	b.WriteString("| Tool | Changes anything? | Params | What it does |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
			t.name, t.effect, t.params, tableCell(headline(t.desc)))
	}
	return b.String()
}

// headline is the description's first sentence: the part that says what the
// tool IS. The rest is how it behaves, and it lives in the description the
// server hands to the agent.
func headline(desc string) string {
	desc = strings.Join(strings.Fields(desc), " ")
	if i := strings.Index(desc, ". "); i > 0 {
		return desc[:i+1]
	}
	return desc
}

// mcpToolsReadmeTable renders the module README's table: the tool, its
// parameters and the short verdict. The full description lives in the docs
// page and in `catalog.go`, which is the source.
func mcpToolsReadmeTable(tools []mcpTool) string {
	var b strings.Builder
	b.WriteString("| Tool | Params | Effect |\n")
	b.WriteString("|---|---|---|\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", t.name, t.params, tableCell(t.effect))
	}
	return b.String()
}

// tableCell makes one description fit a table cell: a single line, with the
// pipe escaped so a cell can never break the table.
func tableCell(desc string) string {
	desc = strings.Join(strings.Fields(desc), " ")
	return strings.ReplaceAll(desc, "|", `\|`)
}

// mcpToolsContent is the generated content: the table, between the markers.
// The leading newline belongs to the marker line, so the content starts on
// the table's first row.
func mcpToolsContent(tools []mcpTool, render func([]mcpTool) string) string {
	return "\n" + render(tools)
}

// checkMCPTools reports every generated table that no longer matches the
// catalog. The gate is blocking: a description that changed in the catalog
// and not in the docs is drift the reader would never see.
func checkMCPTools(root string) []string {
	tools, err := mcpTools(root)
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for _, doc := range mcpToolsDocs {
		want := mcpToolsContent(tools, doc.render)
		raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(doc.file)))
		if rerr != nil {
			out = append(out, doc.file+": "+rerr.Error())
			continue
		}
		got, ok := extractBlock(string(raw))
		if !ok {
			out = append(out, doc.file+": the MCP tool table is not inside "+mcpToolsStart+" … "+mcpToolsEnd)
			continue
		}
		if got != want {
			out = append(out, doc.file+": the MCP tool table is stale — the catalog changed (run `make docs-index`)")
		}
	}
	return out
}

// writeMCPTools rewrites the generated blocks in place.
func writeMCPTools(root string) error {
	tools, err := mcpTools(root)
	if err != nil {
		return err
	}
	for _, doc := range mcpToolsDocs {
		want := mcpToolsContent(tools, doc.render)
		path := filepath.Join(root, filepath.FromSlash(doc.file))
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		got, ok := extractBlock(string(raw))
		if !ok {
			return fmt.Errorf("%s: no %s … %s block to rewrite", doc.file, mcpToolsStart, mcpToolsEnd)
		}
		updated := strings.Replace(string(raw), got, want, 1)
		if updated == string(raw) {
			continue
		}
		if werr := os.WriteFile(path, []byte(updated), 0o644); werr != nil {
			return werr
		}
	}
	return nil
}

// extractBlock returns the content between the markers, markers excluded.
func extractBlock(raw string) (string, bool) {
	start := strings.Index(raw, mcpToolsStart)
	if start < 0 {
		return "", false
	}
	end := strings.Index(raw[start:], mcpToolsEnd)
	if end < 0 {
		return "", false
	}
	from := start + len(mcpToolsStart)
	return raw[from : start+end], true
}
