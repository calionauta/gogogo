// Datastar action strings the inbox buttons emit.
package components

import "testing"

// TestReadActionsAreOneStringPlusClientID pins the exact expressions the two
// mark-read buttons put in data-on:click. The single-row action shipped with a
// missing closing quote, so the browser parsed `'/api/…/read + '` as one
// string and then a bare `?clientID=` — a syntax error Datastar surfaced as
// "Blocked by CSP", which is why this is a string-for-string pin and not a
// substring check.
func TestReadActionsAreOneURLPlusClientID(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "one row",
			got:  readAction("01a17f3b-38b9-7646-85e5-149aee2fc5c5"),
			want: "@post('/api/notifications/01a17f3b-38b9-7646-85e5-149aee2fc5c5/read'" +
				" + '?clientID=' + encodeURIComponent(window.__gogogoClientID || ''))",
		},
		{
			name: "every row",
			got:  readAllAction(),
			want: "@post('/api/notifications/read-all'" +
				" + '?clientID=' + encodeURIComponent(window.__gogogoClientID || ''))",
		},
		{
			name: "one row unread",
			got:  unreadAction("01a17f3b-38b9-7646-85e5-149aee2fc5c5"),
			want: "@post('/api/notifications/01a17f3b-38b9-7646-85e5-149aee2fc5c5/unread'" +
				" + '?clientID=' + encodeURIComponent(window.__gogogoClientID || ''))",
		},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s action =\n  %s\nwant\n  %s", tc.name, tc.got, tc.want)
		}
	}
}

// TestRowShowBindsTheFilterToEachRow pins the client-side filter expression:
// an unread row stays visible under both tabs, a read row hides under
// "unread". The expression travels in the row's markup, so a stream or action
// patch re-binds the current tab without a server round-trip.
func TestRowShowBindsTheFilterToEachRow(t *testing.T) {
	if got, want := rowShow(true), "$notificationFilter !== 'unread' || true"; got != want {
		t.Errorf("rowShow(unread) = %q, want %q", got, want)
	}
	if got, want := rowShow(false), "$notificationFilter !== 'unread' || false"; got != want {
		t.Errorf("rowShow(read) = %q, want %q", got, want)
	}
}
