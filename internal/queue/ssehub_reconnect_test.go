package queue

import (
	"strings"
	"testing"
	"time"
)

// TestSSEHub_SendAndRegisterAreSerialized pins the invariant that makes
// reconnects lossless: "decide where this event goes" and "put it there" are ONE
// lock hold.
//
// The failure it guards is neither a data race (-race stays silent — channels
// are safe) nor a deadlock (nothing hangs). It is a lost update between two
// correct-looking halves of the hub's contract:
//
//	Register: replaces h.clients[id], then drains AND deletes the replay buffer
//	Send:     looks up h.clients[id], then delivers (or buffers)
//
// Release the lock between Send's lookup and its delivery and a Register landing
// in that gap receives the event on the channel it just REPLACED — which no
// handler reads any more — while the replay buffer, the only other destination,
// is drained and deleted by that same Register. The frame is gone, and from the
// browser it is a chat answer or a notes patch that never arrived because the
// tab happened to reconnect.
//
// HOW THIS OBSERVES IT WITHOUT A TIMING RACE. The drop handler is the point
// where a sender reports "I did not deliver", and it runs at a known place in
// the sequence: inside the critical section when delivery and decision are one
// step, outside it when they are split. Parking the handler lets a second
// goroutine ask whether the hub lock is held at that instant — a deterministic
// read of the invariant, not a lucky interleaving. TryLock (never Lock) so the
// probe can never wedge the hub.
func TestSSEHub_SendAndRegisterAreSerialized(t *testing.T) {
	t.Parallel()

	inDrop := make(chan struct{})
	release := make(chan struct{})
	h := NewSSEHub(WithDropHandler(func(string, []byte, string) {
		close(inDrop)
		<-release
	}))

	// A full channel makes Send take the drop path deterministically.
	ch := make(chan []byte, 1)
	ch <- []byte("occupying")
	h.Register("tab-1", "", ch)

	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		h.Send("tab-1", []byte("payload"))
	}()

	select {
	case <-inDrop:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Send never reached the drop handler")
	}

	// The drop report is in flight: is the hub lock held right now?
	// TryLock==true means it was FREE, so the decision was made outside it —
	// and we must release what we just took.
	free := h.mu.TryLock()
	if free {
		h.mu.Unlock()
	}
	serialized := !free

	close(release)
	<-sendDone

	if !serialized {
		t.Fatal("Send makes its delivery decision OUTSIDE the hub's critical section: a " +
			"concurrent Register can replace the client's channel between Send's lookup and " +
			"its delivery, so the event lands on a channel no handler reads while the replay " +
			"buffer that was its only other destination is already drained — the frame is " +
			"silently lost on reconnect. Hold the read lock across lookup AND delivery, as " +
			"Broadcast/BroadcastExcept already do via deliverOrDrop")
	}
}

// TestSSEHub_RegisterRejectsUnbufferedChannel pins that a setup mistake fails
// loudly instead of degrading silently.
//
// The hub delivers with `select { case ch <- data: default: drop }`. On an
// UNBUFFERED channel that select takes `default` whenever no reader is already
// parked, so the client receives events only by luck — and every miss is
// reported as "slow client" while the client is perfectly healthy. That is the
// worst kind of bug: intermittent, mislabelled, and it looks like a slow
// browser rather than a wrong argument. Register now refuses it at setup.
//
// RED-PROOFED: drop the `cap(ch) == 0` check from Register and this panics with
// "did not panic", i.e. the hub accepts a channel that cannot work.
func TestSSEHub_RegisterRejectsUnbufferedChannel(t *testing.T) {
	t.Parallel()

	h := NewSSEHub()
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("Register accepted an unbuffered channel: every non-blocking send to it " +
				"takes `default`, so the client would miss events at random and each miss would " +
				"be mislabelled as a slow client. Register must refuse cap==0")
		}
		msg, ok := rec.(string)
		if !ok || !strings.Contains(msg, "BUFFERED") {
			t.Fatalf("panic message %q should name the BUFFERED requirement", rec)
		}
	}()
	h.Register("tab-1", "", make(chan []byte)) // unbuffered: the bug
}

// TestSSEHub_RegisterAcceptsABufferedChannel is the other half: the guard must
// not reject the shape production actually uses, AND a buffered channel must
// actually deliver — otherwise the rejection above would be "safe" by breaking
// the working path.
func TestSSEHub_RegisterAcceptsABufferedChannel(t *testing.T) {
	t.Parallel()

	h := NewSSEHub()
	ch := make(chan []byte, 64)
	h.Register("tab-1", "u1", ch) // must not panic
	defer h.Unregister("tab-1")

	h.Send("tab-1", []byte("payload"))

	select {
	case got := <-ch:
		if string(got) != "payload" {
			t.Fatalf("delivered %q, want \"payload\"", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no event on a registered buffered channel: the guard is rejecting the " +
			"shape production uses, or Send no longer reaches it")
	}
}
