// SCOPE:layer=feature,removal=feature — Headless render tests for the native views.
package main

import (
	"image"
	"testing"

	"github.com/gogpu/ui/offscreen"
)

// The views must BUILD without a window or GPU: buildRoot executes
// headless and offscreen accepts the tree. These are panic-guards,
// not visual tests — the offscreen CPU rasterizer draws nothing on a
// headless machine (even the package's own text example renders
// blank here: no font/SDF path without GPU), so no pixel assertion
// can hold in CI. Real visual proof is `make run-gui` on a GPU
// machine. What these catch: widget-API misuse that panics at build
// time (nil children, invalid config) — which unit tests on state
// alone would miss.
func renderRoot(t *testing.T, s *windowState) image.Image {
	t.Helper()
	deps := uiDeps{
		s:         s,
		refreshUI: func() {},
		onSignOut: func() { s.signOut() },
	}
	r := offscreen.NewRenderer(windowWidth, windowHeight)
	r.Render(buildRoot(deps))
	img := r.Image()
	if img == nil {
		t.Fatal("offscreen render produced no image")
	}
	return img
}

func renderSize(t *testing.T, s *windowState) (w, h int) {
	t.Helper()
	img := renderRoot(t, s)
	return img.Bounds().Dx(), img.Bounds().Dy()
}

func TestViewsRenderLoginWhenSignedOut(t *testing.T) {
	s := newNativeState(t)
	w, h := renderSize(t, s)
	if w <= 0 || h <= 0 {
		t.Errorf("login render size = %dx%d, want positive", w, h)
	}
}

func TestViewsRenderTodoListWhenSignedIn(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)
	s.setDraft("render me")
	s.add()
	w, h := renderSize(t, s)
	if w <= 0 || h <= 0 {
		t.Errorf("todo render size = %dx%d, want positive", w, h)
	}
}

func TestViewsRenderErrorState(t *testing.T) {
	s := newNativeState(t)
	s.setEmail(nativeEmail)
	s.setPassword("wrong")
	s.signIn() // leaves authErr set, still signed out
	if snap := s.snapshot(); snap.authErr == "" {
		t.Fatal("setup: expected an auth error")
	}
	w, h := renderSize(t, s)
	if w <= 0 || h <= 0 {
		t.Errorf("error render size = %dx%d, want positive", w, h)
	}
}
