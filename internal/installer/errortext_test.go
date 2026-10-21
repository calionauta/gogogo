package installer

import (
	"errors"
	"strings"
	"testing"
)

// TestErrorTextPrefixOnce pins the CLI's one-line error shape: ExitError
// messages written for humans already carry "gogogo:", so the entrypoint must
// not add a second one (it printed "gogogo: gogogo: refusing — read-only
// mode" before).
func TestErrorTextPrefixOnce(t *testing.T) {
	prefixed := &ExitError{code: 1, msg: "gogogo: refusing — read-only mode"}
	if got := ErrorText(prefixed); got != prefixed.msg {
		t.Errorf("prefixed message was rewritten: %q", got)
	}
	bare := errors.New("directory app not found")
	if got := ErrorText(bare); got != "gogogo: "+bare.Error() {
		t.Errorf("bare message lost its prefix: %q", got)
	}
	if strings.HasPrefix(ErrorText(prefixed), "gogogo: gogogo:") {
		t.Error("prefix doubled")
	}
}
