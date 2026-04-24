package rules

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestParseWebRulesSupportsExceptionAndDomain(t *testing.T) {
	body := encodeAutoProxy("[AutoProxy 0.2.9]\n||twitter.com\n@@||apple.com\n")
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

func TestParseWebRulesDecodesBase64WithWhitespace(t *testing.T) {
	encoded := encodeAutoProxy("[AutoProxy 0.2.9]\n||twitter.com\n")
	body := encoded[:8] + "\n \t" + encoded[8:16] + "\r\n" + encoded[16:]

	set, err := ParseWebRules(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse web rules: %v", err)
	}

	if !set.ProxyHost("api.twitter.com") {
		t.Fatal("expected whitespace inside base64 payload to be ignored")
	}
}

func TestParseWebRulesReturnsEmptySet(t *testing.T) {
	body := encodeAutoProxy("[AutoProxy 0.2.9]\n\n")
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
	if set.ProxyHost("") {
		t.Fatal("expected empty host to miss proxy host rules")
	}
	if set.DirectHost("") {
		t.Fatal("expected empty host to miss direct host rules")
	}
}

func TestParseWebRulesIgnoresCommentAndBlankLines(t *testing.T) {
	body := encodeAutoProxy("[AutoProxy 0.2.9]\n# comment\n\n||twitter.com\n\n# another comment\n@@||apple.com\n")
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

func TestParseWebRulesIgnoresInvalidURLPrefixLine(t *testing.T) {
	body := encodeAutoProxy("[AutoProxy 0.2.9]\n|foo\n|http://\n|https://specific-site.com/path\n")
	set, err := ParseWebRules(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse web rules: %v", err)
	}

	if set.ProxyURL("http://example.com") {
		t.Fatal("expected invalid URL prefix rules to be ignored")
	}
	if !set.ProxyURL("https://specific-site.com/path/child") {
		t.Fatal("expected valid URL prefix rule to remain effective")
	}
}

func TestParseWebRulesNormalizesURLPrefixMatch(t *testing.T) {
	body := encodeAutoProxy("[AutoProxy 0.2.9]\n|HTTP://Example.COM/path?query=1#Frag\n")
	set, err := ParseWebRules(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse web rules: %v", err)
	}

	if !set.ProxyURL("http://example.com/path?query=1#Frag") {
		t.Fatal("expected scheme and host matching to ignore case and ignore fragment")
	}
	if !set.ProxyURL("http://example.com/path?query=1") {
		t.Fatal("expected fragment to be ignored during normalization")
	}
	if set.ProxyURL("http://example.com/path?query=2#Frag") {
		t.Fatal("expected different query not to match")
	}
}

func TestParseWebRulesNormalizesHostCase(t *testing.T) {
	body := encodeAutoProxy("[AutoProxy 0.2.9]\n||TWITTER.COM\n@@||APPLE.COM\n")
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

func TestParseWebRulesReturnsReadError(t *testing.T) {
	_, err := ParseWebRules(&webRuleErrorReader{err: errors.New("boom")})
	if err == nil {
		t.Fatal("expected read error")
	}
	if !strings.Contains(err.Error(), "read web rules") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseWebRuleTextReturnsScanError(t *testing.T) {
	_, err := parseWebRuleText(&hostRuleErrorReader{
		data: "||twitter.com\n",
		err:  errors.New("boom"),
	})
	if err == nil {
		t.Fatal("expected scan error")
	}
	if !strings.Contains(err.Error(), "scan web rules") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func encodeAutoProxy(content string) string {
	return base64.StdEncoding.EncodeToString([]byte(content))
}

type webRuleErrorReader struct {
	err error
}

func (r *webRuleErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}
