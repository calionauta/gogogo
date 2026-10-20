// SCOPE:feature — Room patch payload contract.
//
// The room's SSE payload is produced in ONE place (sendRosterPatch) and read
// by two audiences: the stream (broadcast to peers) and the POST answer (the
// clicker). Both must carry the lock as a JSON OBJECT.
//
// This file exists because of a shipped bug: RoomLockSignals used to return
// a JSON STRING, which MarshalAndPatchSignals then serialized again — the
// wire carried "{\"roomHolder\":\"x\"}" (a quoted string) instead of
// {"roomHolder":"x"}. Datastar merges a string as a no-op, so $roomHolder
// never moved: the roster updated while every lock button stayed frozen on
// its previous visibility, and the lock looked unresponsive to clicks that
// had in fact succeeded. Nothing on the server looked wrong, which is exactly
// why the assertion is on the BYTES.
package handlers

import (
	"context"
	"strings"
	"testing"

	appgoakt "github.com/calionauta/gogogo/internal/goakt"
)

// TestRoomRosterPatchSignalsAreObjects pins that the pushed signals frame
// carries an object, not a doubly-encoded string.
//
// RED-PROOFED: make roomcomponents.RoomLockSignals return its JSON text as a
// string again and this fails on the `data: signals "{\"` assertion.
func TestRoomRosterPatchSignalsAreObjects(t *testing.T) {
	roster := appgoakt.Roster{Presenter: "ana@example.com", Generation: 3}
	payload, err := rosterPatch(context.Background(), "lobby", roster)
	if err != nil {
		t.Fatalf("roster patch: %v", err)
	}
	frame := string(payload)

	if !strings.Contains(frame, "datastar-patch-signals") {
		t.Fatalf("patch carries no signals frame:\n%s", frame)
	}
	// The dataline must start an OBJECT. A quoted object here is the bug.
	if strings.Contains(frame, `data: signals "{\"`) {
		t.Fatalf("signals are double-encoded (a quoted string, which Datastar merges as a no-op):\n%s", frame)
	}
	if !strings.Contains(frame, `data: signals {"roomHolder":"ana@example.com"}`) {
		t.Fatalf("signals frame does not carry the holder as an object:\n%s", frame)
	}
}

// TestRoomRosterPatchSignalsPrecedeElements pins the frame ORDER the client
// depends on: Datastar evaluates events in arrival order, and the card's
// data-show/data-text expressions are evaluated as part of the element patch,
// so signals must already be merged when the card is applied.
//
// RED-PROOFED: swap the two Send calls and the button affordances render one
// push behind (the clicker's own POST shows stale buttons).
func TestRoomRosterPatchSignalsPrecedeElements(t *testing.T) {
	payload, err := rosterPatch(context.Background(), "lobby", appgoakt.Roster{Presenter: "ana@example.com"})
	if err != nil {
		t.Fatalf("roster patch: %v", err)
	}
	frame := string(payload)
	sig := strings.Index(frame, "datastar-patch-signals")
	elem := strings.Index(frame, "datastar-patch-elements")
	if sig < 0 || elem < 0 {
		t.Fatalf("patch is missing one of its frames (signals=%d elements=%d):\n%s", sig, elem, frame)
	}
	if sig > elem {
		t.Fatalf("elements frame precedes signals; the card would render against a stale $roomHolder:\n%s", frame)
	}
}

// TestRoomBroadcastPatchCarriesNoViewerIdentity pins the per-recipient rule:
// a broadcast has ONE body for every tab, so it must never carry $roomMe (a
// peer's push would otherwise overwrite each tab's own identity). The
// clicker's own patch DOES carry it, which is how it seeds its own row.
func TestRoomBroadcastPatchCarriesNoViewerIdentity(t *testing.T) {
	roster := appgoakt.Roster{Presenter: "ana@example.com"}

	broadcast, err := rosterPatch(context.Background(), "lobby", roster)
	if err != nil {
		t.Fatalf("broadcast patch: %v", err)
	}
	// Look at the SIGNALS frame only: the card markup legitimately MENTIONS
	// $roomMe inside its data-show/data-text expressions (that is how each
	// tab derives "am I the holder?" locally). What must never appear is a
	// signals merge that WRITES roomMe, which would overwrite a peer's
	// identity with someone else's.
	if sigs := signalsLine(string(broadcast)); strings.Contains(sigs, "roomMe") {
		t.Fatalf("broadcast signals write $roomMe; a peer push would overwrite each tab's identity:\n%s", sigs)
	}

	own, err := rosterPatchFor(context.Background(), "lobby", roster, "ana@example.com")
	if err != nil {
		t.Fatalf("own patch: %v", err)
	}
	if sigs := signalsLine(string(own)); !strings.Contains(sigs, `"roomMe":"ana@example.com"`) {
		t.Fatalf("the clicker's own patch does not seed $roomMe:\n%s", sigs)
	}
}

// signalsLine extracts the datastar-patch-signals data line from a patch
// frame, so assertions target the merged signals rather than the card markup
// (which legitimately mentions signal NAMES in its expressions).
func signalsLine(frame string) string {
	for line := range strings.SplitSeq(frame, "\n") {
		if strings.HasPrefix(line, "data: signals ") {
			return line
		}
	}
	return ""
}

// TestRoomRosterPatchTargetsTheContentRegion pins the patch selector: the
// region must be the inner content element, so the container that holds the
// signal-bound buttons survives the patch. Patching the card itself replaced
// the buttons, and a replaced button loses its data-show effect.
func TestRoomRosterPatchTargetsTheContentRegion(t *testing.T) {
	payload, err := rosterPatch(context.Background(), "lobby", appgoakt.Roster{})
	if err != nil {
		t.Fatalf("roster patch: %v", err)
	}
	frame := string(payload)
	if !strings.Contains(frame, "data: selector #room-card-content") {
		t.Fatalf("patch does not target #room-card-content:\n%s", frame)
	}
	if strings.Contains(frame, "data: selector #room-card\n") {
		t.Fatalf("patch targets the whole card; the buttons would be replaced and lose their effects:\n%s", frame)
	}
}
