// SCOPE:layer=feature,removal=feature — Todo MVC example (reference implementation)
package handlers

import "github.com/calionauta/gogogo/internal/queue"

// todoUpdateJob builds the queue.Job envelope for SSE-hub todo events.
// Record mutations (create/toggle/delete) now propagate through
// PocketBase realtime (the OnModelAfter*Success hooks broadcast to every
// subscriber of the "todos" topic), so this envelope is only used for
// EPHEMERAL signals still carried by the SSE hub — the durable
// workflow's "workflow-completed" / "workflow-error" notifications sent
// from onboarding.go via broadcaster.PublishTodoUpdate.
//
// Tagged dagnats because its only caller (onboarding.go) is dagnats-only;
// without the tag it would be dead code in the default build (unused lint).
func todoUpdateJob(event, title string) []byte {
	// "id" and "done" are part of the streamTodo wire payload and are emitted as
	// their zero values on purpose: these are workflow-level notifications
	// (completed / error / timeout), not mutations of a specific record, so
	// there is no id to carry and no completion to flip. They are constants here
	// rather than parameters because every call site would pass the same thing —
	// which unparam flags, correctly: a parameter every caller fixes is a
	// constant wearing a parameter's clothes.
	ev := mustJSON(map[string]any{
		"event":  event,
		"source": "remote",
		"id":     "",
		"title":  title,
		"done":   false,
	})
	j := mustJSON(queue.Job{Type: "todo", Payload: ev})
	return j
}
