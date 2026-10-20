# API routes

> Generated from source — do not edit by hand.
> Regenerate: `go test ./internal/capabilities/ -run TestApiRoutesDocFresh -update`
> Coverage is enforced by TestTemplAPIURLsHaveRoutes (every ref below
> must match a registered route); this file is the inventory it proves.

## Registered routes (63)

Every `/api/*` literal in non-test, non-generated Go sources.

- `/api/ai/`
- `/api/ai/request`
- `/api/auth/`
- `/api/auth/magic`
- `/api/auth/magic/request`
- `/api/auth/magic?token=`
- `/api/auth/oauth/`
- `/api/auth/signin/credential`
- `/api/auth/signout`
- `/api/auth/signup/credential`
- `/api/auth/totp/begin`
- `/api/auth/totp/disable`
- `/api/auth/totp/login`
- `/api/auth/totp/recovery`
- `/api/auth/totp/verify`
- `/api/byok/`
- `/api/byok/*`
- `/api/chat/`
- `/api/chat/ask`
- `/api/chat/stream`
- `/api/credits`
- `/api/credits/`
- `/api/credits/checkout`
- `/api/credits/stripe-webhook`
- `/api/genui/`
- `/api/genui/ask`
- `/api/genui/stream`
- `/api/notes`
- `/api/notes/`
- `/api/notes/fragment`
- `/api/notes/{docID}/caret`
- `/api/notes/{docID}/delete`
- `/api/notes/{docID}/leave`
- `/api/notes/{docID}/op`
- `/api/notes/{docID}/stream`
- `/api/notes/{docID}/typing`
- `/api/onboarding`
- `/api/onboarding/`
- `/api/onboarding/start`
- `/api/review/`
- `/api/review/run`
- `/api/session/stream`
- `/api/todos`
- `/api/todos/`
- `/api/todos/completed/delete`
- `/api/todos/fragment`
- `/api/todos/retry-demo`
- `/api/todos/search`
- `/api/todos/stream`
- `/api/todos/suggest`
- `/api/todos/suggest-simulated`
- `/api/todos/{id}/confirm-delete`
- `/api/todos/{id}/delete`
- `/api/todos/{id}/toggle`
- `/api/whiteboard`
- `/api/whiteboard/`
- `/api/whiteboard/{docID}/delete`
- `/api/whiteboard/{docID}/presence`
- `/api/whiteboard/{docID}/snapshot`
- `/api/whiteboard/{docID}/stream`
- `/api/whiteboard/{docID}/update`
- `/api/whiteboards/`
- `/api/whiteboards/fragment`

## Template and script refs (21)

Every `/api/*` URL fired from `.templ` and `.js` sources.

- `/api/`
- `/api/chat/ask`
- `/api/chat/stream`
- `/api/genui/ask`
- `/api/genui/stream`
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
