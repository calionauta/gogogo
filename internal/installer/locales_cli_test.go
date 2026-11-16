// `gogogo locales`: the operator's language answer, recorded once.
package installer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLocalesReportsTheDefaultWhenNothingIsRecorded(t *testing.T) {
	var out bytes.Buffer
	if err := runLocales([]string{"--dir", t.TempDir()}, &out); err != nil {
		t.Fatalf("locales: %v", err)
	}
	if !strings.Contains(out.String(), "default") || !strings.Contains(out.String(), "en") {
		t.Errorf("output does not name the default and its source:\n%s", out.String())
	}
}

func TestLocalesSetWritesAndReadsBack(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	if err := runLocales([]string{"--dir", root, "--set", "en,pt-BR,es", "--format", "json"}, &out); err != nil {
		t.Fatalf("locales --set: %v", err)
	}
	var rec struct {
		Locales []string `json:"locales"`
		Source  string   `json:"source"`
		Written bool     `json:"written"`
	}
	if err := json.Unmarshal(out.Bytes(), &rec); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if !rec.Written || rec.Source != "locales.yml" || len(rec.Locales) != 3 {
		t.Fatalf("recorded %+v", rec)
	}
	out.Reset()
	if err := runLocales([]string{"--dir", root}, &out); err != nil {
		t.Fatalf("locales: %v", err)
	}
	if !strings.Contains(out.String(), "es") {
		t.Errorf("read-back lost a language:\n%s", out.String())
	}
}

// TestLocalesRefusesNoLanguages: an empty set would make the boot fall back to
// English silently, so the CLI refuses instead.
func TestLocalesRefusesNoLanguages(t *testing.T) {
	if err := runLocales([]string{"--dir", t.TempDir(), "--set", " , "}, &bytes.Buffer{}); err == nil {
		t.Fatal("an empty language set was recorded")
	}
}
