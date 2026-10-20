// SCOPE:plugin — Datastar rendering helpers (RenderAndPatch, MergeSignals)
package datastar

import (
	"context"
	"strings"

	"github.com/a-h/templ"
	datastarsdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/internal/observability"
)

// RenderAndPatch renders a Templ component and patches it into the DOM via SSE.
func RenderAndPatch(
	sse *datastarsdk.ServerSentEventGenerator,
	component templ.Component,
	opts ...datastarsdk.PatchElementOption,
) error {
	var buf strings.Builder
	if err := component.Render(context.Background(), &buf); err != nil {
		observability.Error(context.Background(), "datastar render failed",
			observability.WithComponent("datastar", "core")...)
		return err
	}
	return sse.PatchElements(buf.String(), opts...)
}

// MergeSignals merges signals into the frontend state via SSE.
func MergeSignals(sse *datastarsdk.ServerSentEventGenerator, signals any) error {
	return sse.MarshalAndPatchSignals(signals)
}

// StreamOpenedSignal is the signal name a Datastar stream writes as its
// first frame. The value is the tab's clientID.
const StreamOpenedSignal = "streamOpen"

// AckStreamOpened is the FIRST frame of every Datastar SSE stream. The
// client cannot observe a Datastar @get from the outside, and clicking the
// opener before Datastar binds is a silent no-op; this frame is the
// end-to-end proof that the click took, the request reached the handler,
// and the BROWSER applied the response — carried by the same connection
// the page is about to use. sse-open.js waits for this signal instead of
// asking the server whether the hub registered a client (a second request
// per open that proved less: registration says nothing about delivery).
func AckStreamOpened(sse *datastarsdk.ServerSentEventGenerator, clientID string) error {
	return sse.MarshalAndPatchSignals(map[string]string{StreamOpenedSignal: clientID})
}
