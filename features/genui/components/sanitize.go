// SCOPE:plugin — model-HTML sanitizer for the html_block component.
package components

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// maxHTMLBytes caps one html_block payload: model markup past the cap is
// rejected at parse time (a half-rendered generative region is worse
// than an error toast), and it bounds the parse work itself.
const maxHTMLBytes = 8192

// allowedElements is the closed tag set for model-authored markup:
// structure + text + tables + links. No scripts, no styles, no frames,
// no forms, no inputs, no media (an <img> is a tracking beacon and an
// unbounded download). Anything else is dropped with its subtree,
// except plain text (which survives, escaped).
var allowedElements = map[string]bool{
	"div": true, "span": true, "p": true,
	"h1": true, "h2": true, "h3": true, "h4": true,
	"ul": true, "ol": true, "li": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
	"strong": true, "em": true, "b": true, "i": true, "u": true,
	"code": true, "pre": true, "blockquote": true,
	"br": true, "hr": true, "a": true,
}

// dropSubtree holds tags whose content must die with them: active
// content, metadata, and frames. Any other unknown tag is skipped but
// its children are still walked, so plain text survives (escaped)
// while nested active content keeps getting filtered by recursion.
var dropSubtree = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true,
	"embed": true, "link": true, "meta": true, "base": true,
	"title": true, "noscript": true, "template": true, "slot": true,
}

// SanitizeHTML reduces untrusted model markup to the allowlist above.
// `class` passes through untouched (Tailwind/DaisyUI theme classes are
// how the model styles invented components; unknown classes are inert).
// `href` survives only on <a> with an http(s)/mailto/same-origin target.
// Everything else — event handlers, inline styles, comments, disallowed
// tags — is dropped. Text nodes are escaped by the renderer, never raw.
func SanitizeHTML(raw string) (string, error) {
	if len(raw) > maxHTMLBytes {
		return "", fmt.Errorf("genui: html_block exceeds %d bytes", maxHTMLBytes)
	}
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("genui: html_block requires html")
	}
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("genui: parse html_block: %w", err)
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(html.EscapeString(n.Data))
		case html.ElementNode:
			renderNode(&b, n, walk)
		}
	}
	// Render the fragment, not the implied document: Parse wraps bare
	// markup in html/head/body, which must not leak into the bubble.
	body := findBody(doc)
	if body == nil {
		return "", errors.New("genui: html_block has no renderable content")
	}
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", errors.New("genui: html_block sanitized to nothing")
	}
	return out, nil
}

// renderNode emits one element whitelisted by name, with filtered
// attributes, recursing into children. Unknown tags die (dropSubtree)
// or unwrap (children walked, text surviving escaped).
func renderNode(b *strings.Builder, n *html.Node, walk func(*html.Node)) {
	if !allowedElements[n.Data] {
		if dropSubtree[n.Data] {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		return
	}
	b.WriteString("<" + n.Data)
	writeAttrs(b, n)
	b.WriteString(">")
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
	if n.Data != "br" && n.Data != "hr" {
		b.WriteString("</" + n.Data + ">")
	}
}

// writeAttrs emits the surviving attributes: open class vocabulary,
// href fenced to safe targets on anchors. Everything else dies here.
func writeAttrs(b *strings.Builder, n *html.Node) {
	for _, a := range n.Attr {
		switch {
		case a.Key == "class":
			b.WriteString(` class="` + html.EscapeString(a.Val) + `"`)
		case a.Key == "href" && n.Data == "a" && safeHref(a.Val):
			b.WriteString(` href="` + html.EscapeString(a.Val) + `"`)
		}
	}
}

// findBody locates the implied (or explicit) body element of a parse
// tree. A nil body means the input held no elements at all.
func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "body" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if b := findBody(c); b != nil {
			return b
		}
	}
	return nil
}

// safeHref admits http(s), mailto, and same-origin paths. Protocol-
// relative URLs, schemes with whitespace tricks, and bare fragments
// that could smuggle behavior never reach the anchor.
func safeHref(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" {
		return false
	}
	lower := strings.ToLower(u)
	for _, p := range []string{"http://", "https://", "mailto:"} {
		if strings.HasPrefix(lower, p) {
			return !strings.ContainsAny(u, " \t\n")
		}
	}
	if strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") {
		return !strings.ContainsAny(u, " \t\n")
	}
	return false
}
