package components

import (
	"bytes"
	"context"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/calionauta/gogogo/features/todo"
)

// TestPendingRowPrototypeMatchesTodoItem is the drift guard for the optimistic
// offline row.
//
// The pending row used to be built element by element in
// web/resources/static/todo-offline.js, restating classes, the id scheme and
// the data-show filters by hand. That markup has to mirror TodoItem or the
// placeholder looks foreign while pending and then MOVES when the replay
// re-renders the server row — the bug that made an offline add jump from last
// to first. Nothing could catch it: the JS strings were outside every gate.
//
// The row now comes from a Templ <template> prototype, so this test can hold
// the two together. It fails when either side drifts, which is the whole point.
func TestPendingRowPrototypeMatchesTodoItem(t *testing.T) {
	t.Parallel()

	fixture := todo.Todo{ID: "abc123", Title: "server row", CanDelete: true}
	itemHTML := render(t, TodoItem(fixture, pageRowActions(fixture)))
	protoHTML := render(t, TodoListRegion(todo.Signals{}))

	item := mustFirstByClass(t, itemHTML, "todo-item")
	proto := mustFirstInTemplate(t, protoHTML, "todo-pending-row")

	// 1) The row itself: same base classes, plus the pending dimming.
	itemClasses := classSet(item)
	protoClasses := classSet(proto)
	delete(protoClasses, "opacity-70") // the pending-only marker
	if diff := setDiff(itemClasses, protoClasses); diff != "" {
		t.Errorf("pending row classes drifted from TodoItem:\n%s\nitem:  %v\nproto: %v",
			diff, sortedSet(itemClasses), sortedSet(protoClasses))
	}

	// 2) The pending marker is on the prototype and never on a server row.
	if got := attr(proto, "data-pending"); got != "true" {
		t.Errorf(`pending row data-pending = %q, want "true"`, got)
	}
	if got := attr(item, "data-pending"); got != "" {
		t.Errorf("server row carries data-pending=%q; the pending state belongs to the clone", got)
	}

	// 3) The visibility contract: the server row carries the date filter, the
	// prototype must carry that clause AND the client-only ones the server
	// expresses as separate queries (filter tab, scope, active search).
	itemShow := attr(item, "data-show")
	protoShow := attr(proto, "data-show")
	if itemShow == "" || protoShow == "" {
		t.Fatalf("missing data-show: item=%q proto=%q", itemShow, protoShow)
	}
	for _, clause := range []string{itemShow, "$filter", "$searchQuery", "$scope"} {
		if !strings.Contains(protoShow, clause) {
			t.Errorf("pending row data-show is missing %q; got %q", clause, protoShow)
		}
	}

	// 4) The title span keeps the exact class list the server uses, so a row
	// never changes typography when the replay replaces it.
	itemTitle := mustFirstByClass(t, item, "todo-title")
	protoTitle := mustFirstByClass(t, proto, "todo-title")
	if diff := setDiff(classSet(itemTitle), classSet(protoTitle)); diff != "" {
		t.Errorf("pending title classes drifted from TodoItem:\n%s", diff)
	}

	// 5) Controls are present, same classes, and disabled until the replay.
	itemBox := mustFirstByTag(t, item, atom.Input)
	protoBox := mustFirstByTag(t, proto, atom.Input)
	if diff := setDiff(classSet(itemBox), classSet(protoBox)); diff != "" {
		t.Errorf("pending checkbox classes drifted from TodoItem:\n%s", diff)
	}
	if !hasAttr(protoBox, "disabled") {
		t.Error("pending checkbox must be disabled until the replay lands")
	}
	protoDelete := firstByAttr(proto, "title", "Delete todo")
	if protoDelete == nil {
		t.Fatal("pending row has no delete affordance")
	}
	itemDelete := firstByAttr(item, "title", "Delete todo")
	if itemDelete == nil {
		t.Fatal("server row has no delete affordance")
	}
	if diff := setDiff(classSet(itemDelete), classSet(protoDelete)); diff != "" {
		t.Errorf("pending delete classes drifted from TodoItem:\n%s", diff)
	}
	if !hasAttr(protoDelete, "disabled") {
		t.Error("pending delete button must be disabled until the replay lands")
	}
	// The icon-only button needs an accessible name of its own: `title` is a
	// tooltip, not a label, and a disabled control is still read by AT.
	if attr(protoDelete, "aria-label") != "Delete todo" {
		t.Errorf("pending delete button aria-label = %q, want %q", attr(protoDelete, "aria-label"), "Delete todo")
	}

	// 6) The shared badge rides the prototype, hidden: the clone unhides it.
	badge := firstByAttr(proto, "data-pending-shared", "1")
	if badge == nil {
		t.Fatal("pending row has no shared badge slot")
	}
	if !hasAttr(badge, "hidden") {
		t.Error("the shared badge must start hidden; only a shared create shows it")
	}
	if got := attr(badge, "title"); got != "Shared: every login sees this todo" {
		t.Errorf("shared badge title = %q, want the TodoItem copy", got)
	}

	// 7) The clone owns its id: a prototype that shipped one would make every
	// pending row share it (and the replay would replace the wrong node).
	if got := attr(proto, "id"); got != "" {
		t.Errorf("pending row prototype must not carry an id (the clone sets it), got %q", got)
	}
}

func render(t *testing.T, c templ.Component) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.Bytes()
}

func parseDoc(t *testing.T, b []byte) *html.Node {
	t.Helper()
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc
}

func mustFirstByClass(t *testing.T, src any, class string) *html.Node {
	t.Helper()
	var root *html.Node
	switch v := src.(type) {
	case []byte:
		root = parseDoc(t, v)
	case *html.Node:
		root = v
	default:
		t.Fatalf("firstByClass: unsupported source %T", src)
	}
	var found *html.Node
	walk(root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && hasClass(n, class) {
			found = n
			return false
		}
		return true
	})
	if found == nil {
		t.Fatalf("no element with class %q", class)
	}
	return found
}

// mustFirstInTemplate finds the element with id=id, then searches INSIDE its
// template content. html.Parse keeps <template> children in a separate
// fragment, so a plain walk over the document does not reach them.
func mustFirstInTemplate(t *testing.T, src []byte, id string) *html.Node {
	t.Helper()
	doc := parseDoc(t, src)
	var tmpl *html.Node
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == atom.Template && attr(n, "id") == id {
			tmpl = n
			return false
		}
		return true
	})
	if tmpl == nil {
		t.Fatalf("no <template id=%q> in the rendered list region; the optimistic row is not server-owned", id)
	}
	var row *html.Node
	walk(tmpl, func(n *html.Node) bool {
		if n.Type == html.ElementNode && hasClass(n, "todo-item") {
			row = n
			return false
		}
		return true
	})
	if row == nil {
		t.Fatalf("<template id=%q> carries no .todo-item row", id)
	}
	return row
}

func mustFirstByTag(t *testing.T, root *html.Node, a atom.Atom) *html.Node {
	t.Helper()
	var found *html.Node
	walk(root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == a {
			found = n
			return false
		}
		return true
	})
	if found == nil {
		t.Fatalf("no <%s> element", a)
	}
	return found
}

func firstByAttr(root *html.Node, key, val string) *html.Node {
	if root == nil {
		return nil
	}
	var found *html.Node
	walk(root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && attr(n, key) == val {
			found = n
			return false
		}
		return true
	})
	return found
}

func walk(n *html.Node, fn func(*html.Node) bool) {
	if n == nil || !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attr(n, "class")), class)
}

func classSet(n *html.Node) map[string]bool {
	set := map[string]bool{}
	for c := range strings.FieldsSeq(attr(n, "class")) {
		set[c] = true
	}
	return set
}

func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	if n == nil {
		return false
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func setDiff(a, b map[string]bool) string {
	var missing, extra []string
	for k := range a {
		if !b[k] {
			missing = append(missing, k)
		}
	}
	for k := range b {
		if !a[k] {
			extra = append(extra, k)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return "  missing from prototype: " + strings.Join(missing, " ") +
		"\n  extra on prototype:     " + strings.Join(extra, " ")
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
