// HTTP notes wire types (request/response envelopes)
//
// These duplicate the framework handler's wire structs field-for-field so
// the client speaks one protocol to either stack. The framework copies go
// away with it at step 7; these stay as the single home.
package handlers

import (
	"github.com/calionauta/gogogo/internal/collab"
)

// wireNoteText is the SSE event type for resolved text.
const wireNoteText = "note-text"

// authedKey is the session flag carried by op answers and the stream
// session event, so tabs learn their auth state without a round trip.
const authedKey = "authed"

// notesSyncSubject is the NATS subject space for cross-instance server
// convergence (unused while the chi stack runs SSE-only; kept so the
// envelope shape stays identical).
const notesSyncSubject = "app.notes."

// NoteTextEvent is the wire envelope broadcast to peers: the resolved text
// after applying an op batch. Rev lets clients advance their base without
// a round-trip. From is the sender's per-tab clientID (echo suppression +
// caret identity); User is the display name for labels. The two split when
// one user holds two tabs — keying by User alone merged their carets.
type NoteTextEvent struct {
	Type string `json:"type"`
	Doc  string `json:"doc"`
	From string `json:"from"`
	User string `json:"user,omitempty"`
	Text string `json:"text"`
	Rev  uint64 `json:"rev"`
	// Caret travels WITH the text it belongs to (same event, zero lag).
	// Omitted when absent or invalid — decoration must never break the
	// op path.
	Caret *CaretReport `json:"caret,omitempty"`
}

// CaretReport is one caret position attached to an op batch or event.
//
// Start is the selection anchor: when present and different from Pos, the
// reporter holds a range and peers paint it as a highlight beside the caret.
// A POINTER because 0 is a real offset (selecting the first character) and an
// absent field must mean "collapsed caret", not "selection from 0" — the
// latter would paint a highlight on every old client's caret.
type CaretReport struct {
	Line  int  `json:"line"`
	Pos   int  `json:"pos"`
	Start *int `json:"start,omitempty"`
}

// validCaret reports whether a client-supplied caret is sane enough to
// relay. Invalid values are dropped, never rejected: caret is decoration.
func validCaret(c *CaretReport) bool {
	return c != nil && c.Line >= 1 && c.Pos >= 0 && (c.Start == nil || *c.Start >= 0)
}

// opRequest is one POST body: an ordered batch of character ops applied
// atomically. Base is the server revision the client computed positions
// against; a mismatch is refused with 409 + authoritative state.
type opRequest struct {
	Base uint64          `json:"base,omitempty"`
	Ops  []collab.TextOp `json:"ops"`
	// Caret travels with the batch so text and dot arrive atomically.
	Caret *CaretReport `json:"caret,omitempty"`
}

// typingRequest is one typing-state report. Ephemeral: broadcast only.
type typingRequest struct {
	Typing bool `json:"typing"`
}

// caretRequest is one caret report: 1-based line plus UTF-16 offset, the
// optional selection anchor, or `gone` when the tab stops owning a caret
// (blur/hidden) so peers retire the dot immediately instead of waiting out
// their expiry. Broadcast only, never persisted.
type caretRequest struct {
	Line  int  `json:"line"`
	Pos   int  `json:"pos"`
	Start *int `json:"start,omitempty"`
	Gone  bool `json:"gone,omitempty"`
}

// caretOrNil passes a client caret through only when sane; anything else
// (including nil) becomes an omitted field, never a 400.
func caretOrNil(c *CaretReport) *CaretReport {
	if !validCaret(c) {
		return nil
	}
	return c
}
