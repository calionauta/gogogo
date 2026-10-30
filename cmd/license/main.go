// Command license mints and inspects the offline entitlement tokens verified
// by internal/license.
//
// The product owner keeps the private key offline and ships only the public
// key in the binary (LICENSE_PUBLIC_KEY). Buyers paste the token at /activate;
// the app verifies it locally and never phones home.
//
//	license keygen -out license.key          # one-time; prints the public hex
//	license mint -key license.key -email a@b -plan pro
//	license mint -key license.key -sub <user-id> -email a@b -sync nats://host:4222
//	license verify -pub <hex> < token.txt
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/calionauta/gogogo/internal/license"
)

const keyFileMode = 0o600 // private key: owner-only

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "keygen":
		keygen(os.Args[2:])
	case "mint":
		mint(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  license keygen [-out license.key]
  license mint -key license.key [-sub ID] [-email EMAIL] [-plan NAME]
               [-days N] [-sync URL] [-sync-token TOKEN] [-out FILE]
  license verify -pub <hex|base64> [token]   (reads stdin when omitted)
`)
	os.Exit(2)
}

func keygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "license.key", "private key file (hex, 0600)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail("generate key", err)
	}
	// #nosec G703 -- the path is an operator-supplied CLI flag.
	if err := os.WriteFile(*out, []byte(hex.EncodeToString(priv)), keyFileMode); err != nil {
		fail("write private key", err)
	}
	fmt.Printf("private key: %s (keep it offline)\n", *out)
	fmt.Printf("LICENSE_PUBLIC_KEY=%s\n", hex.EncodeToString(pub))
}

func mint(args []string) {
	fs := flag.NewFlagSet("mint", flag.ContinueOnError)
	keyPath := fs.String("key", "license.key", "private key file")
	sub := fs.String("sub", "", "user id the token is bound to (account mode)")
	email := fs.String("email", "", "buyer email (display only)")
	plan := fs.String("plan", "pro", "plan label")
	days := fs.Int("days", 0, "expiry in days (0 = perpetual)")
	syncURL := fs.String("sync", "", "NATS leaf-node endpoint embedded in the token")
	syncToken := fs.String("sync-token", "", "NATS credential embedded in the token")
	out := fs.String("out", "", "output file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	rawKey, err := os.ReadFile(*keyPath)
	if err != nil {
		fail("read private key", err)
	}
	priv, err := license.ParsePrivateKey(string(rawKey))
	if err != nil {
		fail("parse private key", err)
	}
	c := license.Claims{Sub: *sub, Email: *email, Plan: *plan, SyncURL: *syncURL, SyncToken: *syncToken}
	if *days > 0 {
		c.ExpiresAt = time.Now().UTC().Add(time.Duration(*days) * 24 * time.Hour).Unix()
	}
	token, err := license.Sign(priv, c)
	if err != nil {
		fail("sign token", err)
	}
	if *out != "" {
		// #nosec G703 -- the path is an operator-supplied CLI flag.
		if err := os.WriteFile(*out, []byte(token+"\n"), keyFileMode); err != nil {
			fail("write token", err)
		}
		fmt.Printf("token written to %s\n", *out)
		return
	}
	fmt.Println(token)
}

func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	pubHex := fs.String("pub", "", "public key (hex or base64)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	pub, err := license.ParsePublicKey(*pubHex)
	if err != nil {
		fail("parse public key", err)
	}
	var raw []byte
	if fs.NArg() > 0 {
		raw = []byte(fs.Arg(0))
	} else {
		raw, err = io.ReadAll(os.Stdin)
		if err != nil {
			fail("read token", err)
		}
	}
	c, err := license.Verify(pub, string(raw), time.Now())
	if err != nil {
		fail("verify", err)
	}
	fmt.Printf("valid: sub=%q email=%q plan=%q sync=%q", c.Sub, c.Email, c.Plan, c.SyncURL)
	if c.ExpiresAt > 0 {
		fmt.Printf(" expires=%s", time.Unix(c.ExpiresAt, 0).UTC().Format(time.RFC3339))
	} else {
		fmt.Print(" expires=never")
	}
	fmt.Println()
}

func fail(what string, err error) {
	fmt.Fprintf(os.Stderr, "license: %s: %v\n", what, err)
	os.Exit(1)
}
