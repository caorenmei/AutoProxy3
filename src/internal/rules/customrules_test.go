package rules

import (
	"strings"
	"testing"
)

func TestParseCustomRulesIgnoresComments(t *testing.T) {
	set, err := ParseHostRules(strings.NewReader("# comment\n*.google.com\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}

	if !set.Match("mail.google.com") {
		t.Fatalf("expected wildcard host match")
	}
}

func TestParseCustomRulesReturnsEmptySet(t *testing.T) {
	set, err := ParseHostRules(strings.NewReader("\n# comment\n\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}

	if set.Match("example.com") {
		t.Fatal("expected empty rule set")
	}
}

func TestParseCustomRulesMatchesExactHost(t *testing.T) {
	set, err := ParseHostRules(strings.NewReader("example.com\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}

	if !set.Match("example.com") {
		t.Fatal("expected exact host match")
	}
	if set.Match("www.example.com") {
		t.Fatal("expected exact host rule not to match subdomain")
	}
}

func TestParseCustomRulesNormalizesHostCase(t *testing.T) {
	set, err := ParseHostRules(strings.NewReader("*.GOOGLE.COM\nEXAMPLE.COM\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}

	if !set.Match("Mail.Google.Com") {
		t.Fatal("expected wildcard match to ignore case")
	}
	if !set.Match("EXAMPLE.COM") {
		t.Fatal("expected exact match to ignore case")
	}
}
