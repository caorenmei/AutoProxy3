package main

import "testing"

func TestNormalizeArgsVersion(t *testing.T) {
	got := normalizeArgs([]string{"autoproxy3", "version"})
	if got.mode != "version" {
		t.Fatalf("expected version mode, got %q", got.mode)
	}
}
