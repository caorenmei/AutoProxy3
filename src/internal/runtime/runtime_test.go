package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caorenmei/autoproxy3/src/internal/config"
	"github.com/caorenmei/autoproxy3/src/internal/management"
	"github.com/caorenmei/autoproxy3/src/internal/rules"
	"github.com/caorenmei/autoproxy3/src/internal/rulesources"
)

func TestNewReturnsRunnerThatRuns(t *testing.T) {
	cfg := config.Config{ListenAddr: "127.0.0.1:8080", AutoDetect: config.AutoDetectConfig{RulesPath: "/var/lib/autoproxy/auto.txt"}}
	runner, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if runner == nil {
		t.Fatal("expected runner")
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestNewPreparesRuleSourceDependencies(t *testing.T) {
	cfg := config.Config{
		ListenAddr: "127.0.0.1:8080",
		WebRules: config.WebRulesConfig{
			Enabled:   true,
			URL:       "https://rules.example.com/list.txt",
			CachePath: "/var/lib/autoproxy/web_rules.txt",
		},
		AutoDetect: config.AutoDetectConfig{RulesPath: "/var/lib/autoproxy/auto.txt"},
	}
	runnerValue, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	concrete, ok := runnerValue.(*Runtime)
	if !ok {
		t.Fatalf("expected concrete runner, got %T", runnerValue)
	}
	if concrete.fileSource == nil {
		t.Fatal("expected custom rule loader")
	}
	store, ok := concrete.autoDetectStore.(rulesources.AutoDetectStore)
	if !ok {
		t.Fatalf("expected auto-detect store type, got %T", concrete.autoDetectStore)
	}
	if store.Path != cfg.AutoDetect.RulesPath {
		t.Fatalf("unexpected auto-detect rules path: got %q want %q", store.Path, cfg.AutoDetect.RulesPath)
	}
	if concrete.webSource == nil {
		t.Fatal("expected web source")
	}
	webSource, ok := concrete.webSource.(*rulesources.WebSource)
	if !ok {
		t.Fatalf("expected web source type, got %T", concrete.webSource)
	}
	if webSource.URL != cfg.WebRules.URL {
		t.Fatalf("unexpected web source url: got %q want %q", webSource.URL, cfg.WebRules.URL)
	}
	if webSource.CachePath != cfg.WebRules.CachePath {
		t.Fatalf("unexpected web source cache path: got %q want %q", webSource.CachePath, cfg.WebRules.CachePath)
	}
	if concrete.engine == nil {
		t.Fatal("expected rules engine")
	}
	if concrete.managementServer == nil {
		t.Fatal("expected management server")
	}
	if concrete.proxyServer == nil {
		t.Fatal("expected proxy server")
	}
}

func TestNewSkipsWebSourceWhenWebRulesDisabled(t *testing.T) {
	runnerValue, err := New(config.Config{
		ListenAddr: "127.0.0.1:8080",
		WebRules:   config.WebRulesConfig{Enabled: false},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	concrete, ok := runnerValue.(*Runtime)
	if !ok {
		t.Fatalf("expected concrete runner, got %T", runnerValue)
	}
	if concrete.webSource != nil {
		t.Fatalf("expected no web source, got %#v", concrete.webSource)
	}
}

func TestRunReturnsContextErrorWhenCancelled(t *testing.T) {
	runner, err := New(config.Config{ListenAddr: "127.0.0.1:8080"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = runner.Run(ctx)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if err != context.Canceled {
		t.Fatalf("expected %v, got %v", context.Canceled, err)
	}
}

func TestRuntimeReloadCustomRulesReplacesSnapshots(t *testing.T) {
	rt := &Runtime{
		config: config.Config{
			CustomRules: config.FileRulesConfig{Enabled: true, Path: "custom.txt"},
			AutoDetect:  config.AutoDetectConfig{Enabled: true, RulesPath: "auto.txt"},
		},
		engine:     rules.NewEngine(),
		fileSource: stubFileSource{custom: mustHostRuleSet(t, "new-custom.example"), autoDetect: mustHostRuleSet(t, "new-auto.example")},
	}
	rt.engine.ReplaceCustomRules(mustHostRuleSet(t, "old-custom.example"))
	rt.engine.ReplaceAutoDetectRules(mustHostRuleSet(t, "old-auto.example"))

	if err := rt.ReloadCustomRules(context.Background()); err != nil {
		t.Fatalf("ReloadCustomRules returned error: %v", err)
	}

	if got := rt.engine.Decide("old-custom.example"); got.Source != rules.DecisionSourceDefault {
		t.Fatalf("expected old custom rule to be replaced, got %+v", got)
	}
	if got := rt.engine.Decide("new-custom.example"); got.Source != rules.DecisionSourceCustom {
		t.Fatalf("expected new custom rule to load, got %+v", got)
	}
	if got := rt.engine.Decide("old-auto.example"); got.Source != rules.DecisionSourceDefault {
		t.Fatalf("expected old auto-detect rule to be replaced, got %+v", got)
	}
	if got := rt.engine.Decide("new-auto.example"); got.Source != rules.DecisionSourceAutoDetect {
		t.Fatalf("expected new auto-detect rule to load, got %+v", got)
	}

	status := rt.StatusSummary()
	if !status.Custom.Enabled || !status.Custom.Loaded {
		t.Fatalf("unexpected custom status: %+v", status.Custom)
	}
	if !status.AutoDetect.Enabled || !status.AutoDetect.Loaded {
		t.Fatalf("unexpected auto-detect status: %+v", status.AutoDetect)
	}
}

func TestRuntimeReloadCustomRulesKeepsOldSnapshotsOnFailure(t *testing.T) {
	rt := &Runtime{
		config: config.Config{
			CustomRules: config.FileRulesConfig{Enabled: true, Path: "custom.txt"},
			AutoDetect:  config.AutoDetectConfig{Enabled: true, RulesPath: "auto.txt"},
		},
		engine:            rules.NewEngine(),
		fileSource:        stubFileSource{err: errors.New("load failed")},
		customRulesLoaded: true,
		autoDetectLoaded:  true,
	}
	rt.engine.ReplaceCustomRules(mustHostRuleSet(t, "old-custom.example"))
	rt.engine.ReplaceAutoDetectRules(mustHostRuleSet(t, "old-auto.example"))

	err := rt.ReloadCustomRules(context.Background())
	if err == nil || err.Error() != "load failed" {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := rt.engine.Decide("old-custom.example"); got.Source != rules.DecisionSourceCustom {
		t.Fatalf("expected old custom rule to stay active, got %+v", got)
	}
	if got := rt.engine.Decide("old-auto.example"); got.Source != rules.DecisionSourceAutoDetect {
		t.Fatalf("expected old auto-detect rule to stay active, got %+v", got)
	}

	status := rt.StatusSummary()
	if !status.Custom.Loaded || !status.AutoDetect.Loaded {
		t.Fatalf("expected loaded flags to remain true, got %+v", status)
	}
}

func TestRuntimeReloadWebRulesReplacesSnapshot(t *testing.T) {
	rt := &Runtime{
		config:    config.Config{WebRules: config.WebRulesConfig{Enabled: true}},
		engine:    rules.NewEngine(),
		webSource: stubWebSource{set: mustWebRuleSet(t, "[AutoProxy 0.2.9]\n||new-web.example\n")},
	}
	rt.engine.ReplaceWebRules(mustWebRuleSet(t, "[AutoProxy 0.2.9]\n||old-web.example\n"))

	if err := rt.ReloadWebRules(context.Background()); err != nil {
		t.Fatalf("ReloadWebRules returned error: %v", err)
	}

	if got := rt.engine.Decide("old-web.example"); got.Source != rules.DecisionSourceDefault {
		t.Fatalf("expected old web rule to be replaced, got %+v", got)
	}
	if got := rt.engine.Decide("new-web.example"); got.Source != rules.DecisionSourceWeb {
		t.Fatalf("expected new web rule to load, got %+v", got)
	}

	status := rt.StatusSummary()
	if !status.Web.Enabled || !status.Web.Loaded {
		t.Fatalf("unexpected web status: %+v", status.Web)
	}
}

func TestRuntimeReloadWebRulesReturnsConfigurationErrorWhenSourceMissing(t *testing.T) {
	rt := &Runtime{
		config: config.Config{WebRules: config.WebRulesConfig{Enabled: true}},
		engine: rules.NewEngine(),
	}

	err := rt.ReloadWebRules(context.Background())
	if !errors.Is(err, errWebRulesNotConfigured) {
		t.Fatalf("expected %v, got %v", errWebRulesNotConfigured, err)
	}
}

func TestRuntimeReloadWebRulesReturnsContextError(t *testing.T) {
	rt := &Runtime{
		config:    config.Config{WebRules: config.WebRulesConfig{Enabled: true}},
		engine:    rules.NewEngine(),
		webSource: stubWebSource{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rt.ReloadWebRules(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected %v, got %v", context.Canceled, err)
	}
}

func TestRuntimeReloadWebRulesKeepsOldSnapshotOnLoadError(t *testing.T) {
	rt := &Runtime{
		config:         config.Config{WebRules: config.WebRulesConfig{Enabled: true}},
		engine:         rules.NewEngine(),
		webSource:      stubWebSource{err: errors.New("download failed")},
		webRulesLoaded: true,
	}
	rt.engine.ReplaceWebRules(mustWebRuleSet(t, "[AutoProxy 0.2.9]\n||old-web.example\n"))

	err := rt.ReloadWebRules(context.Background())
	if err == nil || err.Error() != "download failed" {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := rt.engine.Decide("old-web.example"); got.Source != rules.DecisionSourceWeb {
		t.Fatalf("expected old web rule to stay active, got %+v", got)
	}
	if !rt.StatusSummary().Web.Loaded {
		t.Fatal("expected loaded state to remain true")
	}
}

func TestRuntimeReloadCustomRulesReturnsContextError(t *testing.T) {
	rt := &Runtime{
		config:     config.Config{},
		engine:     rules.NewEngine(),
		fileSource: stubFileSource{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rt.ReloadCustomRules(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected %v, got %v", context.Canceled, err)
	}
}

func TestNewWiresManagementServerToRuntimeCallbacks(t *testing.T) {
	cfg := config.Config{
		ListenAddr: "127.0.0.1:8080",
		WebRules: config.WebRulesConfig{
			Enabled:   true,
			URL:       "https://rules.example.com/list.txt",
			CachePath: "web.txt",
		},
		CustomRules: config.FileRulesConfig{Enabled: true, Path: "custom.txt"},
		AutoDetect:  config.AutoDetectConfig{Enabled: true, RulesPath: "auto.txt"},
		Management:  config.ManagementConfig{Enabled: true, ListenPort: 9091},
	}
	runnerValue, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	rt := runnerValue.(*Runtime)
	rt.webSource = stubWebSource{set: mustWebRuleSet(t, "[AutoProxy 0.2.9]\n||web.example\n")}
	rt.fileSource = stubFileSource{
		custom:     mustHostRuleSet(t, "custom.example"),
		autoDetect: mustHostRuleSet(t, "auto.example"),
	}

	reloadRecorder := httptest.NewRecorder()
	reloadRequest := httptest.NewRequest(http.MethodPost, "/reload_rules", nil)
	rt.managementServer.ServeHTTP(reloadRecorder, reloadRequest)
	if reloadRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", reloadRecorder.Code, http.StatusOK)
	}

	if got := rt.engine.Decide("web.example"); got.Source != rules.DecisionSourceWeb {
		t.Fatalf("expected management reload to refresh web rules, got %+v", got)
	}
	if got := rt.engine.Decide("custom.example"); got.Source != rules.DecisionSourceCustom {
		t.Fatalf("expected management reload to refresh custom rules, got %+v", got)
	}
	if got := rt.engine.Decide("auto.example"); got.Source != rules.DecisionSourceAutoDetect {
		t.Fatalf("expected management reload to refresh auto-detect rules, got %+v", got)
	}

	indexRecorder := httptest.NewRecorder()
	indexRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	rt.managementServer.ServeHTTP(indexRecorder, indexRequest)
	if indexRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", indexRecorder.Code, http.StatusOK)
	}

	var response management.IndexResponse
	if err := json.Unmarshal(indexRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.Rules.Web.Loaded || !response.Rules.Custom.Loaded || !response.Rules.AutoDetect.Loaded {
		t.Fatalf("expected runtime-backed status to report loaded rules, got %+v", response.Rules)
	}
}

func TestRuntimeAutoDetectRecorderDelegatesToStore(t *testing.T) {
	store := &stubAutoDetectStore{}
	recorder := runtimeAutoDetectRecorder{store: store}

	if err := recorder.Record(context.Background(), "example.com"); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if store.host != "example.com" {
		t.Fatalf("unexpected stored host: got %q want %q", store.host, "example.com")
	}
}

type stubFileSource struct {
	custom     rules.HostRuleSet
	autoDetect rules.HostRuleSet
	err        error
}

func (s stubFileSource) LoadCustomAndAutoDetect(string, string) (rules.HostRuleSet, rules.HostRuleSet, error) {
	if s.err != nil {
		return rules.HostRuleSet{}, rules.HostRuleSet{}, s.err
	}
	return s.custom, s.autoDetect, nil
}

type stubWebSource struct {
	set rules.WebRuleSet
	err error
}

func (s stubWebSource) Load() (rules.WebRuleSet, bool, error) {
	if s.err != nil {
		return rules.WebRuleSet{}, false, s.err
	}
	return s.set, true, nil
}

type stubAutoDetectStore struct {
	host string
	err  error
}

func (s *stubAutoDetectStore) AppendHost(host string) error {
	s.host = host
	return s.err
}

func mustHostRuleSet(t *testing.T, body string) rules.HostRuleSet {
	t.Helper()
	set, err := rules.ParseHostRules(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}
	return set
}

func mustWebRuleSet(t *testing.T, body string) rules.WebRuleSet {
	t.Helper()
	set, err := rules.ParseWebRules(strings.NewReader(base64.StdEncoding.EncodeToString([]byte(body))))
	if err != nil {
		t.Fatalf("parse web rules: %v", err)
	}
	return set
}
