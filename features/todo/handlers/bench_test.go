package handlers

import (
	"net/http"
	"testing"
)

// BenchmarkHandlersTodoCreate measures the authed create round-trip (db write +
// SSE render) — the hot path of the slice.
func BenchmarkHandlersTodoCreate(b *testing.B) {
	f := newHandlersFixture(b)
	cookie := f.signup("bench@example.com")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := f.do(http.MethodPost, "/api/todos", "title=Bench&filter=all", cookie)
		if rec.Code != http.StatusOK {
			b.Fatalf("create: status = %d", rec.Code)
		}
	}
}
