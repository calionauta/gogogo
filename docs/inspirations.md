# Inspirations

What gogogo learned from other projects. Credit, not compatibility:
ideas below were re-implemented in gogogo's own architecture, not vendored.

## northstar by Nicholas Zanghi (Zangster)

Repo: https://github.com/zangster300/northstar (MIT)

The original scaffold gogogo started from: Go + NATS + Datastar +
Templ + DaisyUI as one boilerplate for realtime hypermedia apps.

What carried over:

- Stack choice: server-rendered hypermedia with Datastar instead of
  a JSON API + SPA.
- Embedded NATS for realtime (northstar embeds NATS in `cmd/web`;
  gogogo embeds NATS JetStream + PocketBase in one binary).
- `features/` + `router/` + `config/` layout and Air/esbuild live reload.

What diverged: gogogo added PocketBase/SQLite persistence, goqite
background jobs, DagNats durable workflows, offline sync with Service
Worker + IndexedDB outbox, installer/MCP/CLI, and a trim-by-deleting
template model. Northstar stays a minimal starter.

