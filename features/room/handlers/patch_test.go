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
	"bytes"
	"context"
	"strings"
	"testing"

	roomcomponents "github.com/calionauta/gogogo/features/room/components"
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

// TestRoomLeaveAnswerCarriesTheLeftFlag pins the ONE per-viewer value a leave
// answer ships. Leaving used to be invisible on the leaver's own screen (the
// roster row disappears, which reads as "nothing happened") while the card kept
// offering Take/Leave for a room the tab was no longer in. The flag is a
// BOOLEAN in the signals frame — a quoted "true" would merge as a truthy
// string and `$roomLeft === true` would never match — and it must never ride a
// broadcast, which cannot know who is reading.
//
// RED-PROOFED: drop the extra argument from handleLeave and the frame assertion
// fails; move the flag into RoomLockSignals and the broadcast assertion fails.
func TestRoomLeaveAnswerCarriesTheLeftFlag(t *testing.T) {
	var out bytes.Buffer
	if err := sendRosterPatch(context.Background(), patchWriter(&out), "lobby",
		appgoakt.Roster{}, "bea@example.com", roomcomponents.RoomLeftSignal(),
	); err != nil {
		t.Fatalf("leave answer: %v", err)
	}
	sigs := signalsLine(out.String())
	if !strings.Contains(sigs, `"roomLeft":true`) {
		t.Fatalf("leave answer does not set $roomLeft:\n%s", sigs)
	}
	if strings.Contains(sigs, `"roomLeft":"`) {
		t.Fatalf("$roomLeft is a string; `$roomLeft === true` would never match:\n%s", sigs)
	}

	broadcast, err := rosterPatch(context.Background(), "lobby", appgoakt.Roster{})
	if err != nil {
		t.Fatalf("broadcast patch: %v", err)
	}
	if b := signalsLine(string(broadcast)); strings.Contains(b, "roomLeft") {
		t.Fatalf("a broadcast writes $roomLeft; it would resurrect the buttons on whoever just left:\n%s", b)
	}
}

// TestRoomPageSeedsTheLeftFlag pins the other writer: the page's hidden signals
// sibling seeds $roomLeft=false, so a fresh (or reloaded) tab starts in the room
// with the roster actions bound. The flag has exactly two writers — this seed
// and the leave answer — and a shared/broadcast signal set carries neither.
func TestRoomPageSeedsTheLeftFlag(t *testing.T) {
	seed := roomcomponents.RoomLockSignalsJSON(appgoakt.Roster{}, "bea@example.com")
	if !strings.Contains(seed, `"roomLeft":false`) {
		t.Fatalf("page seed does not carry roomLeft=false: %s", seed)
	}
	if shared := roomcomponents.RoomLockSignalsJSON(appgoakt.Roster{}, ""); strings.Contains(shared, "roomLeft") {
		t.Fatalf("the shared/broadcast signal set carries roomLeft: %s", shared)
	}
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
