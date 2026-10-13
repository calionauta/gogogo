package gorules

// Leak fixtures: 500 bodies built with err.Error() must fire; the same
// shape on a 400 (client recovery reason) must stay quiet. Split from
// rules_test.go at the 500-line file budget; same package.

// leakFixtures are the LeakedErrText500 cases: a 500 body built with
// err.Error() must fire; the same shape on a 400 (client recovery
// reason) must stay quiet, and a static 500 without err text must stay
// quiet. Raw sources (not goFile): the fixtures stub the slice-local
// writeJSON/writeText helpers the real handlers use.
func leakFixtures() []ruleFixture {
	badJSON := `package fixture

import "net/http"

func writeJSON(w http.ResponseWriter, code int, v any) {}

func bad(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

var _ = bad
`
	badText := `package fixture

import "net/http"

func writeText(w http.ResponseWriter, code int, msg string) {}

func bad(w http.ResponseWriter, err error) {
	writeText(w, http.StatusInternalServerError, "failed: "+err.Error())
}

var _ = bad
`
	quiet400 := `package fixture

import "net/http"

func writeJSON(w http.ResponseWriter, code int, v any) {}

func quiet(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
}

var _ = quiet
`
	badHTTP := `package fixture

import "net/http"

func badHTTP(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, "failed: "+err.Error(), http.StatusInternalServerError)
}

var _ = badHTTP
`
	quietStatic500 := `package fixture

import "net/http"

func writeJSON(w http.ResponseWriter, code int, v any) {}

func quiet(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "boom"})
}

var _ = quiet
`
	quietStatic500Text := `package fixture

import "net/http"

func writeText(w http.ResponseWriter, code int, msg string) {}

func quiet(w http.ResponseWriter) {
	writeText(w, http.StatusInternalServerError, "boom")
}

var _ = quiet
`
	quietOtherStatus := `package fixture

import "net/http"

func writeText(w http.ResponseWriter, code int, msg string) {}

func quiet(w http.ResponseWriter) {
	writeText(w, http.StatusBadRequest, "bad")
}

var _ = quiet
`
	return []ruleFixture{
		{name: "500 JSON body with err.Error() is flagged", source: badJSON, wantAtLeast: 1},
		{name: "500 text body with err.Error() is flagged", source: badText, wantAtLeast: 1},
		{name: "400 body with err.Error() stays quiet", source: quiet400, wantAtLeast: 0},
		{name: "500 http.Error with err.Error() is flagged", source: badHTTP, wantAtLeast: 1},
		{name: "static 500 without err text stays quiet", source: quietStatic500, wantAtLeast: 0},
		{name: "static 500 text without err stays quiet", source: quietStatic500Text, wantAtLeast: 0},
		{name: "other status constants stay quiet", source: quietOtherStatus, wantAtLeast: 0},
	}
}
