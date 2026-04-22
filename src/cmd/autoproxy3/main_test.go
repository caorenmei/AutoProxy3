package main

import (
	"bytes"
	"testing"
)

func TestNormalizeArgsServe(t *testing.T) {
	got := normalizeArgs([]string{"autoproxy3", "serve"})
	if got.mode != "serve" {
		t.Fatalf("expected serve mode, got %q", got.mode)
	}
}

func TestNormalizeArgsVersion(t *testing.T) {
	got := normalizeArgs([]string{"autoproxy3", "version"})
	if got.mode != "version" {
		t.Fatalf("expected version mode, got %q", got.mode)
	}
}

func TestPrintHelpIncludesServe(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf)

	if got := buf.String(); got != "Usage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\n" {
		t.Fatalf("unexpected help text: %q", got)
	}
}
