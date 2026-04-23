package rules

import (
	"errors"
	"strings"
	"testing"
)

func TestParseHostRulesIgnoresComments(t *testing.T) {
	set, err := ParseHostRules(strings.NewReader("# comment\n*.google.com\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}

	if !set.Match("mail.google.com") {
		t.Fatal("expected wildcard host match")
	}
}

func TestParseHostRulesReturnsEmptySet(t *testing.T) {
	set, err := ParseHostRules(strings.NewReader("\n# comment\n\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}

	if set.Match("example.com") {
		t.Fatal("expected empty rule set")
	}
	if set.Match("") {
		t.Fatal("expected empty host to miss")
	}
}

func TestParseHostRulesMatchesExactHost(t *testing.T) {
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

func TestParseHostRulesNormalizesHostCase(t *testing.T) {
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

func TestParseHostRulesWildcardMatchesSubdomainsOnly(t *testing.T) {
	set, err := ParseHostRules(strings.NewReader("*.google.com\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}

	if !set.Match("mail.google.com") {
		t.Fatal("expected wildcard to match subdomain")
	}
	if !set.Match("deep.mail.google.com") {
		t.Fatal("expected wildcard to match nested subdomain")
	}
	if set.Match("google.com") {
		t.Fatal("expected wildcard not to match bare domain")
	}
}

func TestParseHostRulesSkipsBlankNormalizedRule(t *testing.T) {
	set, err := ParseHostRules(strings.NewReader(".\nexample.com.\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}

	if !set.Match("example.com") {
		t.Fatal("expected normalized trailing-dot host to match")
	}
}

func TestParseHostRulesReturnsScanError(t *testing.T) {
	_, err := ParseHostRules(&hostRuleErrorReader{
		data: "example.com\n",
		err:  errors.New("boom"),
	})
	if err == nil {
		t.Fatal("expected scan error")
	}
	if !strings.Contains(err.Error(), "scan host rules") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostRuleSetMatchIgnoresInvalidWildcardPattern(t *testing.T) {
	set := HostRuleSet{wildcardPatterns: []string{"["}}

	if set.Match("example.com") {
		t.Fatal("expected invalid wildcard pattern to miss")
	}
}

type hostRuleErrorReader struct {
	data string
	err  error
	read bool
}

func (r *hostRuleErrorReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, r.err
	}

	r.read = true
	n := copy(p, r.data)
	return n, r.err
}
