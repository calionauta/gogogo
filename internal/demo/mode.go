// SCOPE:core — Demo mode: the one place that decides what the cookie means.
//
// Two surfaces ask the same question ("is the machinery docked?"): the todo
// handler, to render the page, and the /_ui/demo route, to flip it. They must
// agree on the cookie name and on the value that means "on", so both read this.
//
// A session cookie, not localStorage and not a query param: the mode has to
// follow the session across modules (open /chat after /todo and the panel is
// still docked) and survive a reload, without a script deciding the layout
// before the server has rendered it. `POST /login`-style session cookies already
// set the precedent; this one simply expires with the browser.
package demo

import "net/http"

// Cookie is the session cookie that carries the mode.
const Cookie = "gogogo_demo"

// On reports whether demo mode is active for this request.
func On(r *http.Request) bool {
	c, err := r.Cookie(Cookie)
	return err == nil && c.Value == "1"
}
