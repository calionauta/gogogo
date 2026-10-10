// SCOPE:core
package secrets

import (
	"testing"
)

func TestParseEnvLines_empty(t *testing.T) {
	lines := parseEnvLines("")
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestParseEnvLines_commentsAndBlanks(t *testing.T) {
	input := "# comment\n\n  # indented comment\nKEY=val\n"
	lines := parseEnvLines(input)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0].Key != "KEY" || lines[0].Value != "val" {
		t.Errorf("expected KEY=val, got %s=%s", lines[0].Key, lines[0].Value)
	}
}

func TestParseEnvLines_multiple(t *testing.T) {
	input := "A=1\nB=two\nC=three=four\n"
	lines := parseEnvLines(input)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0].Value != "1" {
		t.Errorf("expected A=1, got A=%s", lines[0].Value)
	}
	if lines[1].Value != "two" {
		t.Errorf("expected B=two, got B=%s", lines[1].Value)
	}
	if lines[2].Value != "three=four" {
		t.Errorf("expected C=three=four, got C=%s", lines[2].Value)
	}
}

func TestParseEnvLines_trimWhitespace(t *testing.T) {
	input := "  KEY =  val  \n"
	lines := parseEnvLines(input)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0].Key != "KEY" || lines[0].Value != "val" {
		t.Errorf("expected KEY=val, got %s=%s", lines[0].Key, lines[0].Value)
	}
}

func TestParseEnvLines_noValue(t *testing.T) {
	input := "KEY=\n"
	lines := parseEnvLines(input)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0].Key != "KEY" || lines[0].Value != "" {
		t.Errorf("expected KEY='', got %s='%s'", lines[0].Key, lines[0].Value)
	}
}

func TestParseLine_invalid(t *testing.T) {
	if l := parseLine(""); l != nil {
		t.Error("expected nil for empty line")
	}
	if l := parseLine("noequal"); l != nil {
		t.Error("expected nil for line without =")
	}
	if l := parseLine("=val"); l != nil {
		t.Error("expected nil when key is empty")
	}
}

func TestParseLine_comment(t *testing.T) {
	if l := parseLine("# comment"); l != nil {
		t.Error("expected nil for comment")
	}
	if l := parseLine("  # indented"); l != nil {
		t.Error("expected nil for indented comment")
	}
}
