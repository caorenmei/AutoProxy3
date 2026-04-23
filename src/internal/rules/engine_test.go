package rules

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestEngineDirectRuleOverridesProxyRules(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceWebRules(WebRuleSet{directDomains: []string{"apple.com"}})
	engine.ReplaceCustomRules(HostRuleSet{
		exactHosts:       map[string]struct{}{"apple.com": {}},
		wildcardPatterns: []string{"*.apple.com"},
	})
	engine.ReplaceAutoDetectRules(HostRuleSet{
		exactHosts: map[string]struct{}{"apple.com": {}},
	})

	decision := engine.Decide("apple.com")
	if decision.UseProxy {
		t.Fatalf("expected apple.com to bypass proxy")
	}
	if decision.Source != "web" {
		t.Fatalf("expected web source, got %q", decision.Source)
	}
	if decision.Reason != "web-direct" {
		t.Fatalf("expected web-direct reason, got %q", decision.Reason)
	}
}

func TestEngineURLPrefixRuleUsesProxy(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceWebRules(WebRuleSet{
		proxyURLPrefixes: []string{"https://secure.example.com/path"},
	})

	decision := engine.DecideURL("https://secure.example.com/path/child")
	if !decision.UseProxy {
		t.Fatal("expected URL prefix rule to use proxy")
	}
	if decision.Source != "web" {
		t.Fatalf("expected web source, got %q", decision.Source)
	}
	if decision.Reason != "web-proxy-url" {
		t.Fatalf("expected web-proxy-url reason, got %q", decision.Reason)
	}
}

func TestEngineDecideTreatsAbsoluteURLAsURLDecision(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceWebRules(WebRuleSet{
		proxyURLPrefixes: []string{"https://secure.example.com/path"},
	})

	decision := engine.Decide("https://secure.example.com/path/child")
	if !decision.UseProxy {
		t.Fatal("expected Decide to route absolute URL through URL matcher")
	}
	if decision.Reason != "web-proxy-url" {
		t.Fatalf("expected web-proxy-url reason, got %q", decision.Reason)
	}
}

func TestEngineHostRulesUseProxy(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceWebRules(WebRuleSet{proxyDomains: []string{"twitter.com"}})
	engine.ReplaceCustomRules(HostRuleSet{
		exactHosts: map[string]struct{}{"example.com": {}},
	})
	engine.ReplaceAutoDetectRules(HostRuleSet{
		exactHosts: map[string]struct{}{"autodetect.example": {}},
	})

	testCases := []struct {
		name   string
		host   string
		source string
		reason string
	}{
		{name: "web host", host: "api.twitter.com", source: "web", reason: "web-proxy-host"},
		{name: "custom host", host: "example.com", source: "custom", reason: "custom-proxy-host"},
		{name: "auto-detect host", host: "autodetect.example", source: "auto-detect", reason: "auto-detect-proxy-host"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			decision := engine.Decide(tc.host)
			if !decision.UseProxy {
				t.Fatalf("expected %s to use proxy", tc.host)
			}
			if decision.Source != tc.source {
				t.Fatalf("expected source %q, got %q", tc.source, decision.Source)
			}
			if decision.Reason != tc.reason {
				t.Fatalf("expected reason %q, got %q", tc.reason, decision.Reason)
			}
		})
	}
}

func TestEngineDefaultsToDirect(t *testing.T) {
	engine := NewEngine()

	decision := engine.Decide("unknown.example")
	if decision.UseProxy {
		t.Fatal("expected unmatched host to connect directly")
	}
	if decision.Source != "default" {
		t.Fatalf("expected default source, got %q", decision.Source)
	}
	if decision.Reason != "direct-default" {
		t.Fatalf("expected direct-default reason, got %q", decision.Reason)
	}
}

func TestEngineDecideURLDefaultsToDirectForInvalidURL(t *testing.T) {
	engine := NewEngine()

	decision := engine.DecideURL("://bad-url")
	if decision.UseProxy {
		t.Fatal("expected invalid URL to default to direct")
	}
	if decision.Source != "default" {
		t.Fatalf("expected default source, got %q", decision.Source)
	}
}

func TestReloadCustomRulesReplacesBothSourcesOnSuccess(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceCustomRules(HostRuleSet{exactHosts: map[string]struct{}{"old-custom.example": {}}})
	engine.ReplaceAutoDetectRules(HostRuleSet{exactHosts: map[string]struct{}{"old-auto.example": {}}})

	err := engine.ReloadCustomSources(
		strings.NewReader("*.facebook.com\n"),
		strings.NewReader("twitter.com\n"),
	)
	if err != nil {
		t.Fatalf("reload custom sources: %v", err)
	}

	if !engine.Decide("api.facebook.com").UseProxy {
		t.Fatal("expected new custom rules to become active")
	}
	if !engine.Decide("twitter.com").UseProxy {
		t.Fatal("expected new auto-detect rules to become active")
	}
	if engine.Decide("old-custom.example").UseProxy {
		t.Fatal("expected old custom snapshot to be replaced")
	}
	if engine.Decide("old-auto.example").UseProxy {
		t.Fatal("expected old auto-detect snapshot to be replaced")
	}
}

func TestReloadCustomRulesKeepsSnapshotOnAutoDetectLoadFailure(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceCustomRules(HostRuleSet{wildcardPatterns: []string{"*.google.com"}})
	engine.ReplaceAutoDetectRules(HostRuleSet{exactHosts: map[string]struct{}{"twitter.com": {}}})

	err := engine.ReloadCustomSources(strings.NewReader("*.facebook.com\n"), errReader{err: errors.New("boom")})
	if err == nil {
		t.Fatal("expected reload error")
	}

	if !engine.Decide("twitter.com").UseProxy {
		t.Fatalf("expected previous auto-detect snapshot to remain active")
	}
	if !engine.Decide("mail.google.com").UseProxy {
		t.Fatalf("expected previous custom snapshot to remain active")
	}
	if strings.Contains(err.Error(), "load auto-detect rules") == false {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEngineConcurrentReadAndReplace(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceCustomRules(HostRuleSet{exactHosts: map[string]struct{}{"alpha.example": {}}})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = engine.Decide("alpha.example")
				_ = engine.DecideURL("https://service.example/path")
			}
		}()
	}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				engine.ReplaceWebRules(WebRuleSet{proxyURLPrefixes: []string{"https://service.example/path"}})
				engine.ReplaceCustomRules(HostRuleSet{exactHosts: map[string]struct{}{"alpha.example": {}}})
				engine.ReplaceAutoDetectRules(HostRuleSet{exactHosts: map[string]struct{}{"beta.example": {}}})
			}
		}()
	}

	wg.Wait()

	if !engine.Decide("beta.example").UseProxy {
		t.Fatal("expected final auto-detect snapshot to remain readable")
	}
	if !engine.DecideURL("https://service.example/path").UseProxy {
		t.Fatal("expected final web snapshot to remain readable")
	}
}

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}
