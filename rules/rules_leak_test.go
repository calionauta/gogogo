package gorules

// Leak fixtures: 500 bodies built with err.Error() must fire; the same
// shape on a 400 (client recovery reason) must stay quiet. Split from
// rules_test.go at the 500-line file budget; same package.

// leakFixtures are the LeakedErrText500 cases: a 500 body built with
// err.Error() must fire; the same shape on a 400 (client recovery
// reason) must stay quiet. Raw sources (not goFile): the fixture needs
// an errors import goFile does not provide.
func leakFixtures() []ruleFixture {
	bad := `package fixture

import "errors"

type reqEvent struct{}
func (reqEvent) String(code int, msg string) error { return nil }
var statusInternal = 500
func bad(c reqEvent, err error) error {
	if err == nil {
		err = errors.New("boom")
	}
	return c.String(statusInternal, "failed: "+err.Error())
}
`
	quiet400 := `package fixture

type reqEvent struct{}
func (reqEvent) String(code int, msg string) error { return nil }
var statusBadRequest = 400
func quiet(c reqEvent, err error) error {
	return c.String(statusBadRequest, "decode op: "+err.Error())
}
`
	badHTTP := `package fixture

import "net/http"

type reqEvent struct{}
func (reqEvent) String(code int, msg string) error { return nil }
func badHTTP(c reqEvent, err error) error {
	return c.String(http.StatusInternalServerError, "failed: "+err.Error())
}
`
	return []ruleFixture{
		{name: "500 body with err.Error() is flagged", source: bad, wantAtLeast: 1},
		{name: "400 body with err.Error() stays quiet", source: quiet400, wantAtLeast: 0},
		{name: "500 http spelling with err.Error() is flagged", source: badHTTP, wantAtLeast: 1},
	}
}
