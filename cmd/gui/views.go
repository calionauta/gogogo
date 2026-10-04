// SCOPE:layer=feature,removal=feature — gogpu/ui view builders for the POC
//
// Plain widgets only: Text, TextField, Button, Checkbox, Box. No canvas,
// no custom drawing — the point of the POC is the backend wiring, and a
// hand-rolled canvas would test gogpu's renderer instead.
//
// THREADING: builders run on the UI thread (event callbacks and the
// serialized refreshUI in main.go) and read a windowSnapshot — never
// live state — so a background refresh cannot tear a frame mid-build.
// Callbacks mutate via the locked methods (signIn/add/toggle/...) and
// then call refreshUI; they never touch fields directly.
package main

import (
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"

	"github.com/calionauta/gogogo/features/todo"
)

// UI metrics: every widget size in one place so a visual pass touches
// constants, not call sites (and mnd stays quiet).
const (
	titleFontSize  = 28
	errorFontSize  = 14
	cardPadding    = 32
	contentPadding = 24
	cornerRadius   = 12
	stackGap       = 12
	rowGap         = 8
)

// UI palette entries (vars, not consts: color values are composite).
//
//nolint:mnd // color channels are inherently numeric — no name is clearer than 255.
var (
	inkText   = widget.RGBA8(33, 33, 33, 255)
	cardBg    = widget.RGBA8(255, 255, 255, 255)
	contentBg = widget.RGBA8(245, 245, 245, 255)
	errorRed  = widget.RGBA8(176, 0, 32, 255)
)

// uiDeps is the minimal surface views.go needs from main.go: the mutable
// state plus a way to ask for a re-render after mutating it. gogpu/ui is
// retained-mode, so after signIn/add/toggle/remove we rebuild the root
// (SetRoot) and request a redraw — the frame then reflects the new state.
type uiDeps struct {
	s         *windowState
	refreshUI func()
	// onSignOut runs the full sign-out: state reset (views could call
	// s.signOut() directly, but session-file removal lives in main so
	// the filesystem stays out of the view layer).
	onSignOut func()
}

func buildRoot(d uiDeps) widget.Widget {
	snap := d.s.snapshot()
	if !snap.signedIn {
		return loginView(d, snap)
	}
	return todoView(d, snap)
}

func loginView(d uiDeps, snap windowSnapshot) widget.Widget {
	s := d.s

	emailSig := state.NewSignal(snap.email)
	passSig := state.NewSignal("")

	children := []widget.Widget{
		primitives.Text("gogogo — native").FontSize(titleFontSize).Bold().
			Color(inkText),
		textfield.New(
			textfield.Placeholder("demo@demo.app"),
			textfield.InputTypeOpt(textfield.TypeEmail),
			textfield.ValueSignal(emailSig),
			textfield.OnChange(func(v string) { s.setEmail(v) }),
			textfield.OnSubmit(func(v string) {
				s.setEmail(v)
				s.setPassword(passSig.Get())
				s.signIn()
				d.refreshUI()
			}),
		),
		textfield.New(
			textfield.Placeholder("password"),
			textfield.InputTypeOpt(textfield.TypePassword),
			textfield.ValueSignal(passSig),
			textfield.OnChange(func(v string) { s.setPassword(v) }),
			textfield.OnSubmit(func(v string) {
				s.setPassword(v)
				s.setEmail(emailSig.Get())
				s.signIn()
				d.refreshUI()
			}),
		),
		button.New(
			button.TextOpt("Sign in"),
			button.OnClick(func() {
				s.setEmail(emailSig.Get())
				s.setPassword(passSig.Get())
				s.signIn()
				d.refreshUI()
			}),
		),
	}
	if snap.authErr != "" {
		children = append(children, errorView(snap.authErr))
	}

	return primitives.Box(children...).
		Padding(cardPadding).
		Gap(stackGap).
		Background(cardBg).
		Rounded(cornerRadius).
		ShadowLevel(2)
}

func todoView(d uiDeps, snap windowSnapshot) widget.Widget {
	s := d.s

	draftSig := state.NewSignal(snap.draft)

	header := primitives.HBox(
		primitives.Text("Todos").FontSize(titleFontSize).Bold().
			Color(inkText),
		button.New(
			button.TextOpt("Sign out"),
			button.OnClick(func() {
				d.onSignOut()
				d.refreshUI()
			}),
		),
	).Gap(stackGap)

	composer := primitives.HBox(
		textfield.New(
			textfield.Placeholder("What needs doing?"),
			textfield.ValueSignal(draftSig),
			textfield.OnChange(func(v string) { s.setDraft(v) }),
			textfield.OnSubmit(func(v string) {
				s.setDraft(v)
				s.add()
				draftSig.Set("")
				d.refreshUI()
			}),
		),
		button.New(
			button.TextOpt("Add"),
			button.OnClick(func() {
				s.setDraft(draftSig.Get())
				s.add()
				draftSig.Set("")
				d.refreshUI()
			}),
		),
	).Gap(rowGap)

	rows := make([]widget.Widget, 0, len(snap.items))
	for _, it := range snap.items {
		rows = append(rows, todoRow(d, it))
	}
	list := primitives.VBox(rows...).Gap(rowGap)

	content := []widget.Widget{header, composer, list}
	if snap.err != "" {
		content = append(content, errorView(snap.err))
	}

	return primitives.VBox(content...).
		Padding(contentPadding).
		Gap(stackGap).
		Background(contentBg)
}

func todoRow(d uiDeps, it todo.Todo) widget.Widget {
	s := d.s
	title := it.Title
	if it.Completed {
		title = "✓ " + title
	}
	return primitives.HBox(
		checkbox.New(
			checkbox.LabelOpt(title),
			checkbox.Checked(it.Completed),
			checkbox.OnToggle(func(bool) {
				s.toggle(it.ID)
				d.refreshUI()
			}),
		),
		button.New(
			button.TextOpt("×"),
			button.OnClick(func() {
				s.remove(it.ID)
				d.refreshUI()
			}),
		),
	).Gap(rowGap)
}

func errorView(msg string) widget.Widget {
	return primitives.Text(msg).FontSize(errorFontSize).
		Color(errorRed)
}
