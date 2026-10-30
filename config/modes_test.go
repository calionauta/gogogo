package config

import "testing"

func TestValidateModes(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{name: "defaults to none/local", mutate: func(_ *Config) {}, wantErr: false},
		{
			name:    "gated license needs a public key",
			mutate:  func(c *Config) { c.LicenseMode, c.LicenseGate = LicenseRequired, true },
			wantErr: true,
		},
		{
			name:    "issuer needs a private key",
			mutate:  func(c *Config) { c.LicenseMode = LicenseAccount },
			wantErr: true,
		},
		{
			name:    "issuer with a private key",
			mutate:  func(c *Config) { c.LicenseMode, c.LicensePrivateKey = LicenseAccount, "00" },
			wantErr: false,
		},
		{
			name: "license with a key",
			mutate: func(c *Config) {
				c.LicenseMode, c.LicensePublicKey = LicenseRequired, "00"
			},
			wantErr: false,
		},
		{name: "unknown license mode", mutate: func(c *Config) { c.LicenseMode = "trial" }, wantErr: true},
		{
			name:    "open auth beyond loopback is refused",
			mutate:  func(c *Config) { c.AuthMode, c.Host = AuthOpen, "0.0.0.0" },
			wantErr: true,
		},
		{
			name: "open auth beyond loopback with opt-in",
			mutate: func(c *Config) {
				c.AuthMode, c.Host, c.AllowOpenRemote = AuthOpen, "0.0.0.0", true
			},
			wantErr: false,
		},
		{
			name:    "open auth on loopback",
			mutate:  func(c *Config) { c.AuthMode, c.Host = AuthOpen, "127.0.0.1" },
			wantErr: false,
		},
		{name: "unknown auth mode", mutate: func(c *Config) { c.AuthMode = "sso" }, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Host: "127.0.0.1"}
			tc.mutate(c)
			err := c.ValidateModes()
			if tc.wantErr != (err != nil) {
				t.Fatalf("ValidateModes() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
