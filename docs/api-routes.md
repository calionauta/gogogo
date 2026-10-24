# API routes

> Generated from source — do not edit by hand.
> Regenerate: `go test ./internal/capabilities/ -run TestApiRoutesDocFresh -update`
> Coverage is enforced by TestTemplAPIURLsHaveRoutes (every ref below
> must match a registered route); this file is the inventory it proves.

## Registered routes (60)

Every `/api/*` literal in non-test, non-generated Go sources.

- `/api/ai/request`
- `/api/auth/magic`
- `/api/auth/magic/request`
- `/api/auth/magic?`
- `/api/auth/oauth`
- `/api/auth/oauth/{provider}`
- `/api/auth/oauth/{provider}/callback`
- `/api/auth/password`
- `/api/auth/password/reset`
- `/api/auth/password/reset/request`
- `/api/auth/password/reset?token=`
- `/api/auth/sessions`
- `/api/auth/sessions/revoke-all`
- `/api/auth/sessions/{id}`
- `/api/auth/signin/credential`
- `/api/auth/signout`
- `/api/auth/signup/credential`
- `/api/auth/totp/begin`
- `/api/auth/totp/disable`
- `/api/auth/totp/login`
- `/api/auth/totp/recovery`
- `/api/auth/totp/verify`
- `/api/auth/verify`
- `/api/auth/verify/request`
- `/api/auth/verify?token=`
- `/api/byok/*`
- `/api/chat/ask`
- `/api/chat/stream`
- `/api/credits`
- `/api/credits/checkout`
- `/api/credits/stripe-webhook`
- `/api/notes/fragment`
- `/api/notes/{docID}/caret`
- `/api/notes/{docID}/delete`
- `/api/notes/{docID}/leave`
- `/api/notes/{docID}/op`
- `/api/notes/{docID}/stream`
- `/api/notes/{docID}/typing`
- `/api/onboarding/start`
- `/api/review/run`
- `/api/security/csp-report`
- `/api/session/stream`
- `/api/todos`
- `/api/todos/completed/delete`
- `/api/todos/events`
- `/api/todos/fragment`
- `/api/todos/retry-demo`
- `/api/todos/search`
- `/api/todos/stream`
- `/api/todos/suggest`
- `/api/todos/suggest-simulated`
- `/api/todos/{id}/confirm-delete`
- `/api/todos/{id}/delete`
- `/api/todos/{id}/toggle`
- `/api/whiteboard/{docID}/delete`
- `/api/whiteboard/{docID}/presence`
- `/api/whiteboard/{docID}/snapshot`
- `/api/whiteboard/{docID}/stream`
- `/api/whiteboard/{docID}/update`
- `/api/whiteboards/fragment`

## Template and script refs (21)

Every `/api/*` URL fired from `.templ` and `.js` sources.

- `/api/`
- `/api/auth/magic`
- `/api/auth/password/reset`
- `/api/chat/ask`
- `/api/chat/stream`
- `/api/notes/`
- `/api/notes/fragment`
- `/api/onboarding/start`
- `/api/review/run`
- `/api/session/stream`
- `/api/todos`
- `/api/todos/`
- `/api/todos/completed/delete`
- `/api/todos/completed/delete/`
- `/api/todos/fragment`
- `/api/todos/retry-demo`
- `/api/todos/search`
- `/api/todos/stream`
- `/api/todos/suggest`
- `/api/whiteboard/`
- `/api/whiteboards/fragment`
