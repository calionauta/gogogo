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

// TestErrorTextEmptyIsSilent covers the interaction between the prefix rule and
// a verdict-only exit. --check-update's code 3 prints its answer on stdout and
// carries no message, so the entrypoint must print nothing at all: a bare
// "gogogo: " reads like a second failure the command never reported.
func TestErrorTextEmptyIsSilent(t *testing.T) {
	if got := ErrorText(&ExitError{code: 3}); got != "" {
		t.Errorf("a verdict-only exit must produce no stderr line, got %q", got)
	}
}
