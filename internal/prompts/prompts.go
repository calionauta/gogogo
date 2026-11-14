// Package prompts owns every model-facing instruction as data.
//
// A prompt is a file, not a Go string: `files/<domain>/<name>.prompt`, in the
// Dotprompt shape (YAML frontmatter between `---` fences, template body below).
// The format is Google's Dotprompt (github.com/google/dotprompt), which Genkit
// loads from a prompts directory; we adopt the file shape, not its runtime, so
// the same files stay readable by GEPA-style optimizers and by any prompt
// registry that understands frontmatter + `{{variable}}` templates.
//
// Why files at all:
//
//   - a prompt change diffs like content, not like code, and rolls back with
//     one revert;
//   - instrumentation can name the prompt (id + version) instead of logging a
//     hash of a concatenation buried in a handler;
//   - an optimizer (GEPA, promptfoo, a registry) can read, rewrite and score a
//     file without parsing Go.
//
// The gate that keeps this honest is `go run ./cmd/check-prompts`: it validates
// every file here and fails when a prompt-shaped literal appears in Go code.
package prompts

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// OverrideDirEnv names the directory whose files shadow the embedded library.
//
// It is the seam an optimizer needs: a candidate prompt tree on disk, the same
// binary, no rebuild. A file missing there falls back to the embedded copy, so
// an override tree may hold only the prompts it changes. Unset (the default)
// reads the embedded tree alone.
const OverrideDirEnv = "GOGOGO_PROMPTS_DIR"

// readOverride reads one prompt from the override directory. The second return
// value says whether an override file was found at all.
//
// The read goes through os.Root so an id can never name a file above the
// override directory: `../` is refused by the kernel-level root, not by string
// trimming that a future edit could weaken.
func readOverride(id string) (Prompt, bool, error) {
	dir := os.Getenv(OverrideDirEnv)
	if dir == "" {
		return Prompt{}, false, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Prompt{}, false, fmt.Errorf("prompts: %s: override %s: %w", id, dir, err)
	}
	defer func() { _ = root.Close() }()
	raw, err := root.ReadFile(filepath.FromSlash(id + ".prompt"))
	if errors.Is(err, fs.ErrNotExist) {
		return Prompt{}, false, nil
	}
	if err != nil {
		return Prompt{}, false, fmt.Errorf("prompts: %s: override %s: %w", id, dir, err)
	}
	pr, err := Parse(id, raw)
	if err != nil {
		return Prompt{}, false, err
	}
	return pr, true, nil
}

// Dir is the prompt library's path inside a tree, so a caller that writes a
// prompt (the scaffolder) and a caller that reads one (the loader) agree on
// where it lives.
const Dir = "internal/prompts/files"

// files is the prompt library. One directory per domain (chat, genui, review,
// installer) keeps the tree scannable as prompts grow; the id is the path
// inside `files/` without the extension, e.g. `genui/envelope`.
//
//go:embed files
var files embed.FS

// Prompt is one parsed prompt file: the metadata a tool needs plus the body the
// model reads.
type Prompt struct {
	// ID is the file path inside `files/` without `.prompt`, e.g. "review/maker-code".
	ID string
	// Description is one sentence saying when the prompt is used.
	Description string
	// Version is an integer bumped when the body changes in a way that makes an
	// evaluation result incomparable with the previous one.
	Version int
	// Model is an optional hint (e.g. "googleai/gemini-2.5-flash"). Empty means
	// the caller's configured model, which is the norm in this template.
	Model string
	// Output is "", "json" or "text": what the caller expects back.
	Output string
	// Variables lists the `{{name}}` placeholders the body uses, in declaration
	// order. The gate rejects a file whose body and declaration disagree.
	Variables []string
	// Body is the template text below the frontmatter.
	Body string
}

// frontmatter is the file's YAML header. Fields are a deliberate subset of
// Dotprompt's: name/description/version/output/variables are what this template
// instruments and gates; model stays optional so the app's configured model
// wins unless a file pins one.
type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     int      `yaml:"version"`
	Model       string   `yaml:"model"`
	Output      string   `yaml:"output"`
	Variables   []string `yaml:"variables"`
}

// varRE matches one `{{name}}` placeholder. Dotprompt uses Handlebars; this
// loader supports the one construct the template needs (a variable), and the
// gate rejects anything else so a file never silently keeps a placeholder the
// renderer ignores.
var varRE = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// cache holds parsed prompts: the files are embedded and immutable, so a
// request path never re-reads or re-parses YAML. Errors are not cached.
var cache sync.Map // id -> Prompt

// Get loads and validates one prompt by id.
func Get(id string) (Prompt, error) {
	// An override directory shadows the embedded copy: a file there is what the
	// caller asked for, so it is read fresh every time (a candidate tree
	// changes between runs, and the cache would hide that). With no override
	// the function reads the env var once per call and then the cache — cheap
	// enough that the override needs no init-time global to test.
	if pr, ok, err := readOverride(id); ok || err != nil {
		return pr, err
	}
	if cached, ok := cache.Load(id); ok {
		if pr, isPrompt := cached.(Prompt); isPrompt {
			return pr, nil
		}
	}
	raw, err := files.ReadFile(path.Join("files", id+".prompt"))
	if err != nil {
		return Prompt{}, fmt.Errorf("prompts: %s: %w", id, err)
	}
	pr, err := Parse(id, raw)
	if err != nil {
		return Prompt{}, err
	}
	cache.Store(id, pr)
	return pr, nil
}

// All loads every prompt in the library, sorted by id. The gate and tests use
// it; runtime code loads the one file it needs.
func All() ([]Prompt, error) {
	var out []Prompt
	err := fs.WalkDir(files, "files", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".prompt") {
			return nil
		}
		id := strings.TrimSuffix(strings.TrimPrefix(p, "files/"), ".prompt")
		raw, readErr := files.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		pr, parseErr := Parse(id, raw)
		if parseErr != nil {
			return parseErr
		}
		out = append(out, pr)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Parse reads one prompt file. id is the path-derived name and must match the
// frontmatter's `name`, so a renamed file cannot keep advertising the old id.
func Parse(id string, raw []byte) (Prompt, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	body, header, err := splitFrontmatter(text)
	if err != nil {
		return Prompt{}, fmt.Errorf("prompts: %s: %w", id, err)
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return Prompt{}, fmt.Errorf("prompts: %s: frontmatter: %w", id, err)
	}
	pr := Prompt{
		ID:          id,
		Description: strings.TrimSpace(fm.Description),
		Version:     fm.Version,
		Model:       strings.TrimSpace(fm.Model),
		Output:      strings.TrimSpace(fm.Output),
		Variables:   fm.Variables,
		Body:        strings.TrimSpace(body),
	}
	if fm.Name != id {
		return Prompt{}, fmt.Errorf("prompts: %s: frontmatter name is %q; must match the file id", id, fm.Name)
	}
	if err := Validate(pr); err != nil {
		return Prompt{}, err
	}
	return pr, nil
}

// Validate enforces the file contract the gate and the loader share.
func Validate(p Prompt) error {
	if p.ID == "" {
		return errors.New("prompts: empty id")
	}
	if p.Description == "" {
		return fmt.Errorf("prompts: %s: description is required", p.ID)
	}
	if p.Version < 1 {
		return fmt.Errorf("prompts: %s: version must be >= 1", p.ID)
	}
	switch p.Output {
	case "", "json", "text":
	default:
		return fmt.Errorf("prompts: %s: output %q must be json or text", p.ID, p.Output)
	}
	used := Variables(p.Body)
	if unknown := difference(used, p.Variables); len(unknown) > 0 {
		return fmt.Errorf("prompts: %s: body uses undeclared variables %v", p.ID, unknown)
	}
	if unused := difference(p.Variables, used); len(unused) > 0 {
		return fmt.Errorf("prompts: %s: declared variables %v are never used", p.ID, unused)
	}
	if strings.Contains(RemoveVars(p.Body), "{{") {
		return fmt.Errorf("prompts: %s: unsupported template construct (only {{variable}} is rendered)", p.ID)
	}
	if strings.TrimSpace(p.Body) == "" {
		return fmt.Errorf("prompts: %s: body is empty", p.ID)
	}
	return nil
}

// Variables returns the placeholder names a body uses, in first-use order.
func Variables(body string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range varRE.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// RemoveVars strips supported placeholders so the caller can detect leftover
// template syntax.
func RemoveVars(body string) string { return varRE.ReplaceAllString(body, "") }

// Render fills a prompt's variables. An undeclared or missing variable is an
// error: a half-filled prompt reaching a model is worse than a failed request.
func Render(id string, vars map[string]string) (string, error) {
	p, err := Get(id)
	if err != nil {
		return "", err
	}
	return RenderPrompt(p, vars)
}

// RenderPrompt fills an already-loaded prompt (the gate uses this form).
func RenderPrompt(p Prompt, vars map[string]string) (string, error) {
	var missing []string
	out := varRE.ReplaceAllStringFunc(p.Body, func(match string) string {
		name := varRE.FindStringSubmatch(match)[1]
		value, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return match
		}
		return value
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("prompts: %s: missing variables %v", p.ID, missing)
	}
	return out, nil
}

// RenderMeta is Render plus the prompt's address, for a call site that reports
// which revision produced a model request.
func RenderMeta(id string, vars map[string]string) (string, Meta, error) {
	p, err := Get(id)
	if err != nil {
		return "", Meta{}, err
	}
	out, err := RenderPrompt(p, vars)
	if err != nil {
		return "", Meta{}, err
	}
	return out, Meta{ID: p.ID, Version: p.Version}, nil
}

// MustRender is Render for call sites whose prompt id and variables are fixed
// at compile time and whose files are embedded: a failure means the tree is
// broken, and tests plus `make check-prompts` are what catch it.
func MustRender(id string, vars map[string]string) string {
	out, err := Render(id, vars)
	if err != nil {
		panic(err)
	}
	return out
}

// Text is MustRender with no variables, for a prompt that is pure prose.
func Text(id string) string { return MustRender(id, nil) }

// Meta is a rendered prompt's address: the file id and the version it declared.
// It is the two fields an evaluation or a trace needs to group results by
// prompt revision — without them a score cannot be attributed to a file.
type Meta struct {
	ID      string
	Version int
}

// Composer assembles one model request out of prompt files and records WHICH
// files (and which versions) participated. A composed prompt is several files:
// the Chat answer, for example, is the envelope plus whichever guidance bands
// the surface set enabled. Logging a bare digest would say "something changed";
// the parts say what the model was actually told.
//
// It is not safe for concurrent use: one composition belongs to one request.
type Composer struct {
	parts []string
	metas []Meta
	// raw holds the indexes of parts appended with Append (no separator).
	raw []int
}

// Text appends a pure-prose prompt.
func (c *Composer) Text(id string) string { return c.MustRender(id, nil) }

// MustRender appends a rendered prompt. A malformed embedded file panics, like
// the package-level helper: the tests and `make check-prompts` catch it.
func (c *Composer) MustRender(id string, vars map[string]string) string {
	p, err := Get(id)
	if err != nil {
		panic(err)
	}
	out, err := RenderPrompt(p, vars)
	if err != nil {
		panic(err)
	}
	c.parts = append(c.parts, out)
	c.metas = append(c.metas, Meta{ID: p.ID, Version: p.Version})
	return out
}

// Add appends already-rendered text without recording a file: used for
// fragments a unit contributes (a panel's own prompt line) whose id is not in
// this library.
func (c *Composer) Add(text string) {
	if text == "" {
		return
	}
	c.parts = append(c.parts, text)
}

// Append joins text with NO separator, for a tail that already carries its own
// leading newline (the user request). Keeping it separate from Add is what lets
// String join the instruction fragments with a space without also inserting one
// before that newline.
func (c *Composer) Append(text string) {
	if text == "" {
		return
	}
	c.parts = append(c.parts, text)
	c.raw = append(c.raw, len(c.parts)-1)
}

// String joins the parts with one space, except the raw tail, which follows
// the previous part directly. That is the shape every leg has always used:
// sentences that belong together stay adjacent, a disabled surface removes its
// sentence, and the request starts on its own line.
func (c *Composer) String() string {
	raw := make(map[int]bool, len(c.raw))
	for _, i := range c.raw {
		raw[i] = true
	}
	var b strings.Builder
	first := true
	for i, p := range c.parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if !first && !raw[i] {
			b.WriteByte(' ')
		}
		b.WriteString(p)
		first = false
	}
	return b.String()
}

// Parts returns the prompts that participated, in composition order.
func (c *Composer) Parts() []Meta {
	out := make([]Meta, len(c.metas))
	copy(out, c.metas)
	return out
}

// splitFrontmatter returns (body, yaml) for a file whose first line is `---`.
func splitFrontmatter(text string) (body, header string, err error) {
	if !strings.HasPrefix(text, "---\n") {
		return "", "", errors.New("missing `---` frontmatter fence on line 1")
	}
	rest := text[len("---\n"):]
	before, after, found := strings.Cut(rest, "\n---\n")
	if !found {
		return "", "", errors.New("frontmatter is not closed by a `---` line")
	}
	return after, before, nil
}

func difference(a, b []string) []string {
	has := make(map[string]bool, len(b))
	for _, s := range b {
		has[s] = true
	}
	var out []string
	for _, s := range a {
		if !has[s] {
			out = append(out, s)
		}
	}
	return out
}
