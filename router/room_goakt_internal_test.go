// SCOPE:layer=infra,removal=plugin — Room wiring guard test.
//
// Internal (package router) on purpose: it calls the unexported
// registerRoomStack directly, so no export surface — and no coupling
// to another unit's export_test.go — is involved.
package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"

	"github.com/calionauta/gogogo/config"
)

// TestRegisterRoomStackDisabledRegistersNothing pins the off-guard without
// booting an app: no engine (Default() is nil in this package's tests) and
// the flag off means /room must not exist on the mux. Registration only
// adds routes, so a bare ServeEvent suffices.
func TestRegisterRoomStackDisabledRegistersNothing(t *testing.T) {
	r := pbrouter.NewRouter[*core.RequestEvent](
		func(_ http.ResponseWriter, req *http.Request) (*core.RequestEvent, pbrouter.EventCleanupFunc) {
			return &core.RequestEvent{Request: req}, nil
		},
	)
	se := &core.ServeEvent{Router: r}
	registerRoomStack(se, &config.Config{})
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/room", nil)
	if err != nil {
		t.Fatalf("GET /room req: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /room: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("disabled room unit still serves /room with 200")
	}
}
