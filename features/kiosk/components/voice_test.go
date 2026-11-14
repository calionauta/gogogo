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

// TestKioskLanguagePackDownloadCannotHang is the bug the operator hit: Chrome's
// SpeechRecognition.install() stays pending for minutes on a slow link and never
// rejects, so the page said "Downloading…" forever with no instruction and no way
// back. Every browser call now has a bound and a way out.
func TestKioskLanguagePackDownloadCannotHang(t *testing.T) {
	script := renderKioskHead(t)
	for _, want := range []string{
		"installWithTimeout",
		"availableWithTimeout",
		"setTimeout",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the voice script has no bounded %q; a pending install would hang the label", want)
		}
	}
	// The "already downloading" case must tell the user to retry rather than
	// stacking a second install on top of the first.
	if !strings.Contains(script, "still downloading") {
		t.Error("no guidance for the already-downloading state")
	}
	// The user always keeps the typed path.
	if !strings.Contains(script, "type the order") {
		t.Error("the fallback instruction is gone")
	}
}

// TestKioskClearForgetsTheRecognizedTranscript is the reported regression:
// clearing emptied the field, but continuous recognition keeps every final
// result for the whole session, so the next word rebuilt the field from index 0
// and the old order came back. The field must start at event.resultIndex and the
// transcript buffer must be dropped on the same event the markup handles.
func TestKioskClearForgetsTheRecognizedTranscript(t *testing.T) {
	script := renderKioskHead(t)
	if strings.Contains(script, "i = 0; i < event.results.length") {
		t.Error("onresult rebuilds from result 0; a continuous session replays every cleared word")
	}
	if !strings.Contains(script, "event.resultIndex") {
		t.Error("onresult ignores event.resultIndex, so it cannot tell new words from committed ones")
	}
	_, clearBody, ok := strings.Cut(script, `addEventListener("kiosk-clear"`)
	if !ok {
		t.Fatal("the voice wiring does not listen for kiosk-clear; the recognizer buffer keeps the cleared order")
	}
	listener, _, ok := strings.Cut(clearBody, "});")
	if !ok || !strings.Contains(listener, `committed = ""`) {
		t.Error("the clear listener does not empty the committed transcript")
	}
	// The utterance still in flight is also cancelled: dropping only the
	// finished text would let a phrase spoken before the clear land in the
	// field after it.
	if !strings.Contains(listener, "lastFinal = resultCount") {
		t.Error("a clear keeps the unfinished utterance; it reappears when it finalizes")
	}
}

// TestKioskMicSurvivesStopAndRestart is the reported bug: after one Stop, the
// next tap left the browser capturing audio while the page did nothing, the
// button still said "Microphone" and the status claimed "Listening on device…".
// The cause was reusing the recognizer that had just stopped: its onend ran
// after the restart and reset the UI of a live session. Each session gets a new
// recognizer, and every discarded session's events are ignored.
func TestKioskMicSurvivesStopAndRestart(t *testing.T) {
	script := renderKioskHead(t)
	if strings.Contains(script, "if (recognition) return recognition;") {
		t.Error("sessions share one recognizer; the stopped session's onend races the restart")
	}
	if !strings.Contains(script, "var rec = new SR();") {
		t.Error("prepare() does not build a recognizer per session")
	}
	if !strings.Contains(script, "if (recognition !== rec) return;") {
		t.Error("a discarded session's events are not ignored; they reset the live session's UI")
	}
	// Stop must forget the session before aborting it, and it must abort: a
	// request to stop that leaves capture running is the same bug from the
	// other side.
	_, stopBody, ok := strings.Cut(script, "function stop()")
	if !ok {
		t.Fatal("stop() is missing")
	}
	stopBody, _, _ = strings.Cut(stopBody, "\n\t\t\t}")
	if !strings.Contains(stopBody, "recognition = null") {
		t.Error("stop() keeps the recognizer reference; the stale session can still drive the button")
	}
	if !strings.Contains(stopBody, "rec.abort()") {
		t.Error("stop() does not abort the session, so the microphone can stay busy")
	}
}

// TestKioskVoiceStartsOnArrival is the requested behavior: opening the counter
// page is the intent to speak, so the microphone starts without a second tap
// (permission is still the browser's prompt). It stays one attempt: a refused
// prompt is a message, not a retry loop.
func TestKioskVoiceStartsOnArrival(t *testing.T) {
	script := renderKioskHead(t)
	if !strings.Contains(script, "autoStart();") {
		t.Fatal("the page never starts listening on its own; the operator has to tap first")
	}
	_, autoBody, ok := strings.Cut(script, "function autoStart()")
	if !ok {
		t.Fatal("autoStart is not a named function, so it cannot be bounded")
	}
	autoBody, _, _ = strings.Cut(autoBody, "\n\t\t\t}")
	if !strings.Contains(autoBody, "if (!localSupport()) return;") {
		t.Error("autoStart runs on a browser without on-device recognition; it would only print an error")
	}
	if strings.Contains(autoBody, "setTimeout") || strings.Contains(autoBody, "setInterval") {
		t.Error("autoStart retries; a refused permission would prompt on a loop")
	}
}

// TestKioskAsksForTheMicrophoneBeforeAnythingElse is the reported bug: clicking
// the button said "Microphone: not-allowed" without ever prompting. Chrome does
// not show the permission dialog for SpeechRecognition.start() — the recognizer
// expects an already-granted stream — so the page must call getUserMedia itself,
// and it must do so BEFORE the slow language-pack step or the click looks dead.
func TestKioskAsksForTheMicrophoneBeforeAnythingElse(t *testing.T) {
	script := renderKioskHead(t)
	if !strings.Contains(script, "getUserMedia") {
		t.Fatal("the script never asks for the microphone; recognition will fail with not-allowed")
	}
	// Look inside start() only: askMicrophone is DEFINED earlier in the file, so
	// the first Index would find the definition, not the call.
	_, startBody, ok := strings.Cut(script, "function start()")
	if !ok {
		t.Fatal("start() is missing")
	}
	mic := strings.Index(startBody, "askMicrophone().then")
	pack := strings.Index(startBody, "availableWithTimeout(options)")
	if mic < 0 {
		t.Fatal("start() never asks for the microphone")
	}
	if pack < 0 {
		t.Fatal("the language-pack step disappeared")
	}
	if mic > pack {
		t.Error("permission is requested after the language-pack download; the prompt may never appear")
	}
	// A refusal must be its own message: "denied" and "did not catch that" are
	// different problems with different fixes.
	if !strings.Contains(script, "permission was denied") {
		t.Error("a denied prompt has no distinct message")
	}
	// The tracks are released immediately: holding a second stream would keep
	// the microphone indicator on for no reason.
	if !strings.Contains(script, "t.stop()") {
		t.Error("the permission stream is not released")
	}
}

// TestKioskCanStartOver covers the missing affordance: an order begun by voice
// had no way to be abandoned, so a customer who changed their mind was stuck
// editing text.
func TestKioskCanStartOver(t *testing.T) {
	var body bytes.Buffer
	if err := kioskBody(PageData{MenuLine: "Espresso $3.50"}).Render(context.Background(), &body); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := body.String()
	if !strings.Contains(html, `id="kiosk-clear"`) {
		t.Error("no reset control on the page")
	}
	// ONE definition of "clear": the button and the spoken command both dispatch
	// this event, and a single Datastar handler empties the form.
	if !strings.Contains(html, "kiosk-clear") {
		t.Error("reset is not wired through the shared event")
	}
	// The handler lives on the FORM: it is the common ancestor of the button and
	// the input, and a bubbling event from a sibling never reaches the input.
	formStart := strings.Index(html, `id="kiosk-form"`)
	if formStart < 0 {
		t.Fatal("the kiosk form is missing")
	}
	formEnd := strings.Index(html[formStart:], "</form>")
	if formEnd < 0 {
		t.Fatal("the form is not closed")
	}
	form := html[formStart : formStart+formEnd]
	if !strings.Contains(form, "kiosk-clear") {
		t.Error("the clear handler is not on the form (the button's event would not reach it)")
	}
	if !strings.Contains(form, "$kioskDrink = ''") && !strings.Contains(form, "$kioskDrink = &#39;&#39;") {
		t.Error("the clear handler does not empty the preview signals")
	}
	// A spoken command must reach the same handler. The recognizer lives in the
	// HEAD component, so it is rendered separately.
	script := renderKioskHead(t)
	if !strings.Contains(script, "kiosk-clear") {
		t.Error("the recognizer does not dispatch the reset event")
	}
	if !strings.Contains(script, "resetPhrases") {
		t.Error("no spoken reset phrase is recognized")
	}
}
