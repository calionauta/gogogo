# goqite Patterns

## Basic Setup

```go
db, _ := sql.Open("sqlite3", "queue.db")
q, _ := goqite.New(db)
```

## Send & Receive

```go
q.Send(ctx, goqite.Message{Body: data})
msg, _ := q.Receive(ctx)
q.Delete(ctx, msg.ID)
```

## SSE Hub Integration

```go
// Worker: process → notify client via SSE
hub.Send(clientID, result)
q.Delete(ctx, msg.ID)
```

## Key Rules

- Always set visibility timeout on Receive
- Always Delete after successful processing
- Use separate `queue.db` (not the app's main DB) to avoid lock contention
- goqite is for fire-and-forget; use DagNats (always compiled; opt out via DAGNATS_ENABLED=false) or JetStream for durable orchestration
