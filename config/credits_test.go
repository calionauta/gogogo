// SCOPE:core — BYOK provider URL guardrail.
package config

import "testing"

func TestParseProvidersDropsNonHTTP(t *testing.T) {
	got := parseProviders(
		"openai=https://api.openai.com/v1,local=http://127.0.0.1:11434/v1," +
			"file=file:///etc/passwd,junk=not a url,user=http://user:pass@example.com",
	)
	if got["openai"] != "https://api.openai.com/v1" {
		t.Fatalf("openai = %q", got["openai"])
	}
	if got["local"] != "http://127.0.0.1:11434/v1" {
		t.Fatalf("loopback provider must stay allowed, got %q", got["local"])
	}
	for _, k := range []string{"file", "junk", "user"} {
		if _, ok := got[k]; ok {
			t.Errorf("malformed provider %q survived", k)
		}
	}
}
