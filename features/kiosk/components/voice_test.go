// The kiosk page's voice button exists for one reason: the recipe is the
// microphone demo. Two regressions made it inert, and both are invisible to a
// markup-only assertion, so the test reads the SCRIPT.
//
//  1. The wiring ran at parse time from <head>, where getElementById returns
//     null for every id in the body, so the guard returned and no handler was
//     ever attached. The button rendered and did nothing.
//  2. A browser without SpeechRecognition kept the button and said nothing, so
//     "no microphone option" and "a dead microphone option" looked the same.
package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderKioskHead(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := kioskHead().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render kioskHead: %v", err)
	}
	return buf.String()
}

// TestKioskVoiceWiringWaitsForTheDOM is the regression: the component renders
// into <head>, so wiring that runs immediately cannot see the body.
func TestKioskVoiceWiringWaitsForTheDOM(t *testing.T) {
	script := renderKioskHead(t)
	if !strings.Contains(script, "DOMContentLoaded") {
		t.Fatalf("kiosk head script never waits for the DOM; it runs in <head> "+
			"where the body does not exist yet:\n%s", script)
	}
	if !strings.Contains(script, "initKioskVoice") {
		t.Error("the wiring is not behind a named function, so nothing can defer it")
	}
	// The element reads must live inside the deferred function, not at the top
	// level — that placement IS the bug.
	if !strings.Contains(script, "initKioskVoice()") {
		t.Fatal("no call site for the wiring")
	}
	before, _, ok := strings.Cut(script, "function initKioskVoice")
	if !ok {
		t.Fatal("the wiring is not a named function")
	}
	if strings.Contains(before, `getElementById("kiosk-mic")`) {
		t.Error("the mic lookup runs before DOMContentLoaded; it will always be null")
	}
}

// TestKioskMicrophoneIsHonestWithoutSpeech keeps the two states distinguishable:
// with a recognizer the button is offered, without one it is removed and the
// page says why. A silent, unclickable button is what the operator reported.
func TestKioskMicrophoneIsHonestWithoutSpeech(t *testing.T) {
	script := renderKioskHead(t)
	if !strings.Contains(script, "mic.hidden = true") {
		t.Error("a browser with no SpeechRecognition keeps a button that can never work")
	}
	if !strings.Contains(script, "type the order") {
		t.Error("the no-recognizer path does not tell the user what to do instead")
	}
	// The recognizer is still used when it exists, and still on device only.
	for _, want := range []string{"SpeechRecognition", "processLocally"} {
		if !strings.Contains(script, want) {
			t.Errorf("script no longer contains %q — the demo stopped being an on-device recognizer", want)
		}
	}
	// No remote recognizer ever sneaks in: the page's whole privacy claim.
	if strings.Contains(script, "fetch(") && strings.Contains(script, "speech") {
		t.Error("the page appears to call a remote speech endpoint; recognition must stay on device")
	}
}

// TestKioskClickHasExactlyOneHandler is the regression that made the button
// look dead even after it was wired: the markup's data-on:click and an
// addEventListener both toggled, so one click started and stopped recognition in
// the same tick and the label never stuck on "Stop".
func TestKioskClickHasExactlyOneHandler(t *testing.T) {
	script := renderKioskHead(t)
	if strings.Contains(script, `addEventListener("click"`) {
		t.Error("the script adds a click handler on top of the markup's data-on:click; the two toggles cancel out")
	}
	if !strings.Contains(script, "window.__kioskVoice") {
		t.Error("the script no longer publishes __kioskVoice, so the markup's click has nothing to call")
	}
	var body bytes.Buffer
	if err := kioskBody(PageData{MenuLine: "Espresso $3.50"}).Render(context.Background(), &body); err != nil {
		t.Fatalf("render kioskBody: %v", err)
	}
	if !strings.Contains(body.String(), "__kioskVoice") {
		t.Error("the mic button does not call __kioskVoice")
	}
}

// TestKioskMicButtonIsBoundToTheWiring pins the markup half: the button the
// operator clicks must be the one the script drives.
func TestKioskMicButtonIsBoundToTheWiring(t *testing.T) {
	var buf bytes.Buffer
	if err := kioskBody(PageData{MenuLine: "Espresso $3.50"}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render kioskBody: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`id="kiosk-mic"`,
		`id="kiosk-voice"`,
		`id="kiosk-text"`,
		`id="kiosk-voice-status"`,
		"__kioskVoice",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("kiosk body is missing %q; the script's guard would return early", want)
		}
	}
}
