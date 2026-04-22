package rules

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseWebRulesSupportsExceptionAndDomain(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("[AutoProxy 0.2.9]\n||twitter.com\n@@||apple.com\n"))
	set, err := ParseWebRules(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse web rules: %v", err)
	}

	if !set.ProxyHost("twitter.com") {
		t.Fatalf("expected twitter.com to require proxy")
	}
	if !set.DirectHost("apple.com") {
		t.Fatalf("expected apple.com to be direct")
	}
}

func TestParseWebRulesRejectsInvalidBase64(t *testing.T) {
	_, err := ParseWebRules(strings.NewReader("%%%"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode base64") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseWebRulesReturnsEmptySet(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("[AutoProxy 0.2.9]\n\n"))
	set, err := ParseWebRules(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse web rules: %v", err)
	}

	if set.ProxyHost("example.com") {
		t.Fatal("expected empty proxy host rules")
	}
	if set.DirectHost("example.com") {
		t.Fatal("expected empty direct host rules")
	}
	if set.ProxyURL("http://example.com/path") {
		t.Fatal("expected empty proxy url rules")
	}
}

func TestParseWebRulesSupportsURLPrefix(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("[AutoProxy 0.2.9]\n|http://specific-site.com/path\n"))
	set, err := ParseWebRules(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse web rules: %v", err)
	}

	if !set.ProxyURL("http://specific-site.com/path/child") {
		t.Fatal("expected URL prefix match")
	}
	if set.ProxyURL("http://specific-site.com/other") {
		t.Fatal("expected non-matching URL prefix to miss")
	}
}

func TestParseWebRulesNormalizesHostCase(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("[AutoProxy 0.2.9]\n||TWITTER.COM\n@@||APPLE.COM\n"))
	set, err := ParseWebRules(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse web rules: %v", err)
	}

	if !set.ProxyHost("Api.Twitter.com") {
		t.Fatal("expected case-insensitive proxy host match")
	}
	if !set.DirectHost("WWW.apple.COM") {
		t.Fatal("expected case-insensitive direct host match")
	}
}
