package chi

import (
	"net/http"
	"testing"
)

// BenchmarkChiTodoCreate measures the authed create round-trip (db write +
// SSE render) — the hot path of the slice.
func BenchmarkChiTodoCreate(b *testing.B) {
	f := newChiFixture(b)
	cookie := f.signup("bench@example.com")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := f.do(http.MethodPost, "/api/todos", "title=Bench&filter=all", cookie)
		if rec.Code != http.StatusOK {
			b.Fatalf("create: status = %d", rec.Code)
		}
	}
}
