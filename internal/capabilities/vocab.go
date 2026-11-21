// The verb and parameter names a declaration repeats.
//
// Every recipe's Actions list and ActionParams map spell the same handful of
// strings (a verb, a param, a path placeholder). Naming them once here keeps a
// rename to one edit and stops the registry from drifting between the routes it
// mounts and the params it documents — the two are read together.
package capabilities

const (
	verbCreate  = "create"
	verbDelete  = "delete"
	verbEdit    = "edit"
	verbRead    = "read"
	verbReadAll = "read_all"
	verbUpdate  = "update"
	verbRemind  = "remind"
	verbUnread  = "unread"
	verbToggle  = "toggle"
	verbClear   = "clear_completed"
	verbSnap    = "snapshot"
	verbAcquire = "acquire"
	verbAIReq   = "ai_request"
	verbCheck   = "checkout"

	paramBody     = "body"
	paramDocID    = "docID"
	paramClientID = "clientID"
	paramTab      = "tab"
	paramContent  = "content"
	paramText     = "text"
	paramScope    = "scope"
)
