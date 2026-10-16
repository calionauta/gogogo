// SCOPE:plugin — the model-facing instructions for both answer shapes.
// Split from catalog.go for the 500-line file gate: the registry decides
// what RENDERS, this file decides what the model is TOLD.
package components

// PromptForCatalog generates the model instructions from the registry
// (OpenUI pattern): the prompt advertises exactly the names that
// resolve, so the model is never invited to emit components that fail
// closed at parse time.
func PromptForCatalog() string {
	return "Answer with a single JSON object shaped " +
		`{"components":[{"type":"<name>","props":{...}}]} ` +
		"using ONLY these components: " +
		"text_note {text}, " +
		"plan_cards {plans:[{title, detail}]}, " +
		"data_table {headers:[], rows:[[]]}, " +
		"bar_chart {title, bars:[{label, value>=0}]}, " +
		"action_buttons {actions:[{label, method:POST|GET, url:/same/origin/path, fields:{}}]}, " +
		"section {title, children:[<component>]} holding nested components. " +
		"No prose outside the JSON object."
}

// PromptForChat is the chat affordance, and it is deliberately not the Ask
// prompt: Ask demonstrates answers shaped by a fixed catalog, chat answers
// with a component invented for THIS request. The model composes
// Tailwind/DaisyUI markup itself — sanitized server-side, so scripts,
// styles, handlers and media never reach the browser — and reaches for the
// catalog only where markup cannot act (a form posting to this app) or
// would render a fact worse than the catalog already does.
//
// html_block is advertised FIRST on purpose: that order is the difference
// between the two features. With the catalog listed first the model answers
// with the nearest catalog component and chat reads exactly like Ask, which
// is what a user reported. Ask's prompt never names html_block at all.
func PromptForChat() string {
	return "Answer with a single JSON object shaped " +
		`{"components":[{"type":"<name>","props":{...}}]} ` +
		"using ONLY these components: " +
		"html_block {html: markup you invent for this answer}, " +
		"text_note {text}, " +
		"action_buttons {actions:[{label, method:POST|GET, url:/same/origin/path, fields:{}}]}, " +
		"data_table {headers:[], rows:[[]]}, " +
		"plan_cards {plans:[{title, detail}]}, " +
		"bar_chart {title, bars:[{label, value>=0}]}, " +
		"section {title, children:[<component>]} holding nested components. " +
		"Lead with html_block: build the visual this request asks for out of " +
		"plain elements (div, span, p, headings, lists, table) and these " +
		"classes ONLY -- a class outside this list renders unstyled: " +
		ModelHTMLVocabulary() + ". Prefer that visual over reaching for the " +
		"nearest catalog component, and over answering in prose. Only " +
		"structure and classes survive rendering: " +
		"script, style, inline style attributes, event handlers, forms, media, " +
		"svg and images are stripped, so put every style in a class. Use the " +
		"catalog where markup cannot act: action_buttons to post to this app, " +
		"text_note for a single line of text. " +
		"No prose outside the JSON object."
}
