package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/license"
)

func TestEntitlementCheck(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "license.token")

	if nerr := entitlementCheck(&config.Config{}); nerr != nil {
		t.Fatalf("none mode refused: %v", nerr)
	}

	gated := &config.Config{LicenseMode: config.LicenseRequired, LicenseGate: true, LicenseFile: path}
	gated.LicensePublicKey = "00"
	if gerr := entitlementCheck(gated); gerr == nil {
		t.Fatal("gated build without a token opened the window")
	}

	gated.LicensePublicKey = hex.EncodeToString(pub)
	st, err := license.Open(path, pub)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	token, _ := license.Sign(priv, license.Claims{Plan: "pro", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if _, aerr := st.Activate(token, time.Now()); aerr != nil {
		t.Fatalf("activate: %v", aerr)
	}
	if aerr := entitlementCheck(gated); aerr != nil {
		t.Fatalf("activated build refused: %v", aerr)
	}
}
