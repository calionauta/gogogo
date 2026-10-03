// Command agehelper is a tiny stand-in for the `age` / `age-keygen` CLIs,
// used by bin/init-secrets when neither binary is installed. It exists so the
// secrets bootstrap works on a machine that has Go but not age, without adding
// a dependency: filippo.io/age is already a direct requirement of this module
// (internal/secrets uses it at boot).
//
// Usage:
//
//	go run ./scripts/agehelper keygen  <key-file>
//	go run ./scripts/agehelper encrypt <recipient> <in-file> <out-file>
//
// The key file format matches age-keygen's so the two are interchangeable:
//
//	# created: ...
//	# public key: age1...
//	AGE-SECRET-KEY-1...
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

// Argument counts for each subcommand, as named constants so the intent is
// readable and there are no bare numbers in the conditions.
const (
	argsKeygen  = 3 // keygen <key-file>
	argsEncrypt = 5 // encrypt <recipient> <in-file> <out-file>

	// modeSecret is owner read/write only. Key material and the encrypted
	// secret file must never be world-readable, so every file this helper
	// writes uses this mode.
	modeSecret = 0o600
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: agehelper <keygen|encrypt> [args]")
	}
	switch os.Args[1] {
	case "keygen":
		if len(os.Args) != argsKeygen {
			fail("usage: agehelper keygen <key-file>")
		}
		keygen(cleanPath(os.Args[2]))
	case "encrypt":
		if len(os.Args) != argsEncrypt {
			fail("usage: agehelper encrypt <recipient> <in-file> <out-file>")
		}
		encrypt(os.Args[2], cleanPath(os.Args[3]), cleanPath(os.Args[4]))
	default:
		fail("unknown subcommand: " + os.Args[1])
	}
}

// cleanPath normalises a user-supplied path. This is a local CLI operating on
// paths the invoking user already owns (its only caller is bin/init-secrets,
// which passes fixed paths under $SECRETS_DIR), so filepath.Clean is the
// appropriate handling: it removes `.`/`..` segments and duplicate separators
// rather than leaving them for the OS to interpret. It exists to make that
// reasoning explicit for the taint analysis in gosec G703.
func cleanPath(p string) string {
	return filepath.Clean(p)
}

func keygen(path string) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		fail("generate identity: " + err.Error())
	}
	// Same shape as `age-keygen -o`: a comment header, then the secret key.
	body := fmt.Sprintf(
		"# created: %s\n# public key: %s\n%s\n",
		time.Now().UTC().Format(time.RFC3339),
		id.Recipient().String(),
		id.String(),
	)
	//nolint:gosec // G703: path comes from this CLI's own argv; the only caller
	// (bin/init-secrets) passes fixed paths under $SECRETS_DIR, and cleanPath()
	// has already normalised it. There is no privilege boundary being crossed.
	if err := os.WriteFile(path, []byte(body), modeSecret); err != nil {
		fail("write key file: " + err.Error())
	}
	fmt.Printf("Public key: %s\n", id.Recipient())
}

func encrypt(recipient, in, out string) {
	rcpt, err := age.ParseX25519Recipient(strings.TrimSpace(recipient))
	if err != nil {
		fail("parse recipient: " + err.Error())
	}

	//nolint:gosec // G703: see the note in keygen — argv path, already cleaned,
	// no privilege boundary. Same for the output file below.
	inFile, err := os.Open(in)
	if err != nil {
		fail("open input: " + err.Error())
	}
	defer inFile.Close()

	//nolint:gosec // G703: argv path, already cleaned via cleanPath.
	outFile, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, modeSecret)
	if err != nil {
		fail("create output: " + err.Error())
	}
	defer outFile.Close()

	w, err := age.Encrypt(outFile, rcpt)
	if err != nil {
		fail("init encrypt: " + err.Error())
	}
	if _, err := io.Copy(w, inFile); err != nil {
		fail("encrypt: " + err.Error())
	}
	if err := w.Close(); err != nil {
		fail("finalize: " + err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "agehelper: "+msg)
	os.Exit(1)
}
