package rules

import (
	"errors"
	"fmt"
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
	if decision.Source != DecisionSourceWeb {
		t.Fatalf("expected web source, got %q", decision.Source)
	}
	if decision.Reason != DecisionReasonWebDirect {
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
	if decision.Source != DecisionSourceWeb {
		t.Fatalf("expected web source, got %q", decision.Source)
	}
	if decision.Reason != DecisionReasonWebProxyURL {
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
	if decision.Reason != DecisionReasonWebProxyURL {
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
		{name: "web host", host: "api.twitter.com", source: DecisionSourceWeb, reason: DecisionReasonWebProxyHost},
		{name: "custom host", host: "example.com", source: DecisionSourceCustom, reason: DecisionReasonCustomProxyHost},
		{name: "auto-detect host", host: "autodetect.example", source: DecisionSourceAutoDetect, reason: DecisionReasonAutoDetectProxyHost},
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
	if decision.Source != DecisionSourceDefault {
		t.Fatalf("expected default source, got %q", decision.Source)
	}
	if decision.Reason != DecisionReasonDirectDefault {
		t.Fatalf("expected direct-default reason, got %q", decision.Reason)
	}
}

func TestEngineDecideURLDefaultsToDirectForInvalidURL(t *testing.T) {
	engine := NewEngine()

	decision := engine.DecideURL("://bad-url")
	if decision.UseProxy {
		t.Fatal("expected invalid URL to default to direct")
	}
	if decision.Source != DecisionSourceDefault {
		t.Fatalf("expected default source, got %q", decision.Source)
	}
}

func TestEngineDecideURLCoversAllRemainingBranches(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceWebRules(WebRuleSet{
		directDomains: []string{"direct.example"},
		proxyDomains:  []string{"proxy.example"},
	})
	engine.ReplaceCustomRules(HostRuleSet{
		exactHosts: map[string]struct{}{"custom.example": {}},
	})
	engine.ReplaceAutoDetectRules(HostRuleSet{
		exactHosts: map[string]struct{}{"auto.example": {}},
	})

	testCases := []struct {
		name     string
		rawURL   string
		useProxy bool
		source   string
		reason   string
	}{
		{
			name:     "web direct host rule",
			rawURL:   "https://direct.example/resource",
			useProxy: false,
			source:   DecisionSourceWeb,
			reason:   DecisionReasonWebDirect,
		},
		{
			name:     "web proxy host rule",
			rawURL:   "https://proxy.example/resource",
			useProxy: true,
			source:   DecisionSourceWeb,
			reason:   DecisionReasonWebProxyHost,
		},
		{
			name:     "custom host rule",
			rawURL:   "https://custom.example/resource",
			useProxy: true,
			source:   DecisionSourceCustom,
			reason:   DecisionReasonCustomProxyHost,
		},
		{
			name:     "auto detect host rule",
			rawURL:   "https://auto.example/resource",
			useProxy: true,
			source:   DecisionSourceAutoDetect,
			reason:   DecisionReasonAutoDetectProxyHost,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			decision := engine.DecideURL(tc.rawURL)
			if decision.UseProxy != tc.useProxy {
				t.Fatalf("expected useProxy=%t, got %+v", tc.useProxy, decision)
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

func TestReloadCustomRulesKeepsSnapshotOnCustomLoadFailure(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceCustomRules(HostRuleSet{exactHosts: map[string]struct{}{"stable-custom.example": {}}})
	engine.ReplaceAutoDetectRules(HostRuleSet{exactHosts: map[string]struct{}{"stable-auto.example": {}}})

	err := engine.ReloadCustomSources(errReader{err: errors.New("boom")}, strings.NewReader("new-auto.example\n"))
	if err == nil {
		t.Fatal("expected reload error")
	}
	if !strings.Contains(err.Error(), "load custom rules") {
		t.Fatalf("unexpected error: %v", err)
	}

	if !engine.Decide("stable-custom.example").UseProxy {
		t.Fatal("expected previous custom snapshot to remain active")
	}
	if !engine.Decide("stable-auto.example").UseProxy {
		t.Fatal("expected previous auto-detect snapshot to remain active")
	}
	if engine.Decide("new-auto.example").UseProxy {
		t.Fatal("expected new auto-detect rules to stay inactive when custom reload fails")
	}
}

func TestReplaceWebRulesCopiesInputSnapshot(t *testing.T) {
	engine := NewEngine()
	rules := WebRuleSet{
		proxyDomains:     []string{"proxy.example"},
		directDomains:    []string{"direct.example"},
		proxyURLPrefixes: []string{"https://url.example/path"},
	}

	engine.ReplaceWebRules(rules)

	rules.proxyDomains[0] = "mutated-proxy.example"
	rules.directDomains[0] = "mutated-direct.example"
	rules.proxyURLPrefixes[0] = "https://mutated.example/path"

	if decision := engine.Decide("direct.example"); decision.Reason != DecisionReasonWebDirect {
		t.Fatalf("expected direct snapshot to remain unchanged, got %+v", decision)
	}
	if decision := engine.Decide("proxy.example"); decision.Reason != DecisionReasonWebProxyHost {
		t.Fatalf("expected proxy snapshot to remain unchanged, got %+v", decision)
	}
	if decision := engine.DecideURL("https://url.example/path/child"); decision.Reason != DecisionReasonWebProxyURL {
		t.Fatalf("expected URL snapshot to remain unchanged, got %+v", decision)
	}
	if decision := engine.Decide("mutated-proxy.example"); decision.UseProxy {
		t.Fatalf("expected mutated proxy input to stay outside engine snapshot, got %+v", decision)
	}
}

func TestReplaceHostRulesCopyInputSnapshot(t *testing.T) {
	engine := NewEngine()
	customRules := HostRuleSet{
		exactHosts:       map[string]struct{}{"custom.example": {}},
		wildcardPatterns: []string{"*.custom.example"},
	}
	autoRules := HostRuleSet{
		exactHosts:       map[string]struct{}{"auto.example": {}},
		wildcardPatterns: []string{"*.auto.example"},
	}

	engine.ReplaceCustomRules(customRules)
	engine.ReplaceAutoDetectRules(autoRules)

	delete(customRules.exactHosts, "custom.example")
	customRules.exactHosts["mutated-custom.example"] = struct{}{}
	customRules.wildcardPatterns[0] = "*.mutated-custom.example"
	delete(autoRules.exactHosts, "auto.example")
	autoRules.exactHosts["mutated-auto.example"] = struct{}{}
	autoRules.wildcardPatterns[0] = "*.mutated-auto.example"

	if decision := engine.Decide("custom.example"); decision.Source != DecisionSourceCustom {
		t.Fatalf("expected custom snapshot to remain unchanged, got %+v", decision)
	}
	if decision := engine.Decide("api.custom.example"); decision.Source != DecisionSourceCustom {
		t.Fatalf("expected custom wildcard snapshot to remain unchanged, got %+v", decision)
	}
	if decision := engine.Decide("auto.example"); decision.Source != DecisionSourceAutoDetect {
		t.Fatalf("expected auto-detect snapshot to remain unchanged, got %+v", decision)
	}
	if decision := engine.Decide("api.auto.example"); decision.Source != DecisionSourceAutoDetect {
		t.Fatalf("expected auto-detect wildcard snapshot to remain unchanged, got %+v", decision)
	}
	if decision := engine.Decide("mutated-custom.example"); decision.UseProxy {
		t.Fatalf("expected mutated custom input to stay outside engine snapshot, got %+v", decision)
	}
	if decision := engine.Decide("mutated-auto.example"); decision.UseProxy {
		t.Fatalf("expected mutated auto input to stay outside engine snapshot, got %+v", decision)
	}
}

func TestNormalizeDecisionHostHandlesEmptyPortAndIPv6(t *testing.T) {
	testCases := []struct {
		name string
		host string
		want string
	}{
		{name: "empty", host: "   ", want: ""},
		{name: "host port", host: "Example.COM:8443", want: "example.com"},
		{name: "ipv6", host: "[2001:DB8::1]", want: "2001:db8::1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeDecisionHost(tc.host); got != tc.want {
				t.Fatalf("normalizeDecisionHost(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
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

func TestEngineConcurrentReloadAndDecideSeesWholeSnapshot(t *testing.T) {
	engine := NewEngine()
	engine.ReplaceCustomRules(HostRuleSet{exactHosts: map[string]struct{}{"old.example": {}}})

	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		errOnce sync.Once
		errText string
	)

	recordFailure := func(format string, args ...any) {
		errOnce.Do(func() {
			errText = fmt.Sprintf(format, args...)
		})
	}

	checkSnapshot := func() {
		snapshot := engine.snapshot()
		oldDecision := snapshot.decideHost("old.example")
		newDecision := snapshot.decideURL("https://new.example/path")

		oldIsCustom := oldDecision.Reason == DecisionReasonCustomProxyHost
		newIsAuto := newDecision.Reason == DecisionReasonAutoDetectProxyHost

		switch {
		case oldIsCustom && !newIsAuto:
			return
		case !oldIsCustom && newIsAuto:
			return
		default:
			recordFailure("unexpected mixed snapshot state: old=%+v new=%+v", oldDecision, newDecision)
		}
	}

	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 200; j++ {
				checkSnapshot()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for j := 0; j < 200; j++ {
			if err := engine.ReloadCustomSources(strings.NewReader(""), strings.NewReader("new.example\n")); err != nil {
				recordFailure("reload to new snapshot: %v", err)
				return
			}
			if err := engine.ReloadCustomSources(strings.NewReader("old.example\n"), strings.NewReader("")); err != nil {
				recordFailure("reload to old snapshot: %v", err)
				return
			}
		}
	}()

	close(start)
	wg.Wait()

	if errText != "" {
		t.Fatal(errText)
	}
}

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}
