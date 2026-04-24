package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

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
	if reflect.TypeOf(concrete.fileSource) != reflect.TypeOf(rulesources.FileSource{}) {
		t.Fatalf("expected file source type %T, got %T", rulesources.FileSource{}, concrete.fileSource)
	}
	store := concrete.autoDetectStore
	if store.Path != cfg.AutoDetect.RulesPath {
		t.Fatalf("unexpected auto-detect rules path: got %q want %q", store.Path, cfg.AutoDetect.RulesPath)
	}
	if concrete.webSource.URL != cfg.WebRules.URL {
		t.Fatalf("unexpected web source url: got %q want %q", concrete.webSource.URL, cfg.WebRules.URL)
	}
	if concrete.webSource.CachePath != cfg.WebRules.CachePath {
		t.Fatalf("unexpected web source cache path: got %q want %q", concrete.webSource.CachePath, cfg.WebRules.CachePath)
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

func TestRuntimeFieldTypesMatchPlannedSources(t *testing.T) {
	runtimeType := reflect.TypeOf(Runtime{})

	cases := []struct {
		field string
		want  reflect.Type
	}{
		{field: "webSource", want: reflect.TypeOf(rulesources.WebSource{})},
		{field: "fileSource", want: reflect.TypeOf(rulesources.FileSource{})},
		{field: "autoDetectStore", want: reflect.TypeOf(rulesources.AutoDetectStore{})},
	}

	for _, tc := range cases {
		field, ok := runtimeType.FieldByName(tc.field)
		if !ok {
			t.Fatalf("expected field %q to exist", tc.field)
		}
		if field.Type != tc.want {
			t.Fatalf("unexpected type for %s: got %v want %v", tc.field, field.Type, tc.want)
		}
	}
}

func TestRuntimeDoesNotExposeIndirectLoaderFields(t *testing.T) {
	runtimeType := reflect.TypeOf(Runtime{})

	for _, fieldName := range []string{"loadWebSource", "loadFileSource", "appendAutoDetectHost"} {
		if _, ok := runtimeType.FieldByName(fieldName); ok {
			t.Fatalf("expected runtime to stop exposing %q indirection field", fieldName)
		}
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
	if concrete.webSource.URL != "" {
		t.Fatalf("expected zero-value web source url, got %q", concrete.webSource.URL)
	}
	if concrete.webSource.CachePath != "" {
		t.Fatalf("expected zero-value web source cache path, got %q", concrete.webSource.CachePath)
	}
	if concrete.webSource.HTTPClient != nil {
		t.Fatalf("expected zero-value web source client, got %#v", concrete.webSource.HTTPClient)
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
	customPath, autoPath := writeRuntimeRuleFiles(t, "new-custom.example\n", "new-auto.example\n")
	rt := &Runtime{
		config: config.Config{
			CustomRules: config.FileRulesConfig{Enabled: true, Path: customPath},
			AutoDetect:  config.AutoDetectConfig{Enabled: true, RulesPath: autoPath},
		},
		engine:     rules.NewEngine(),
		fileSource: rulesources.FileSource{},
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
	workspace := newRuntimeTestWorkspace(t)
	rt := &Runtime{
		config: config.Config{
			CustomRules: config.FileRulesConfig{Enabled: true, Path: filepath.Join(workspace, "missing-custom.txt")},
			AutoDetect:  config.AutoDetectConfig{Enabled: true, RulesPath: filepath.Join(workspace, "auto.txt")},
		},
		engine:            rules.NewEngine(),
		fileSource:        rulesources.FileSource{},
		customRulesLoaded: true,
		autoDetectLoaded:  true,
	}
	rt.engine.ReplaceCustomRules(mustHostRuleSet(t, "old-custom.example"))
	rt.engine.ReplaceAutoDetectRules(mustHostRuleSet(t, "old-auto.example"))

	err := rt.ReloadCustomRules(context.Background())
	if err == nil || !strings.Contains(err.Error(), "open custom rules") {
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
	webSource := newRuntimeWebSource(t, runtimeWebSourceFixture{
		body: mustEncodeWebRulesBody(t, "[AutoProxy 0.2.9]\n||new-web.example\n"),
	})
	rt := &Runtime{
		config:    config.Config{WebRules: config.WebRulesConfig{Enabled: true}},
		engine:    rules.NewEngine(),
		webSource: webSource,
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

func TestRuntimeReloadWebRulesReturnsConfigurationErrorWhenWebRulesDisabled(t *testing.T) {
	rt := &Runtime{
		config:    config.Config{WebRules: config.WebRulesConfig{Enabled: false}},
		engine:    rules.NewEngine(),
		webSource: rulesources.WebSource{},
	}

	err := rt.ReloadWebRules(context.Background())
	if !errors.Is(err, errWebRulesNotConfigured) {
		t.Fatalf("expected %v, got %v", errWebRulesNotConfigured, err)
	}
}

func TestRuntimeReloadWebRulesReturnsConfigurationErrorWhenSourceZeroValue(t *testing.T) {
	rt := &Runtime{
		config:    config.Config{WebRules: config.WebRulesConfig{Enabled: true}},
		engine:    rules.NewEngine(),
		webSource: rulesources.WebSource{},
	}

	err := rt.ReloadWebRules(context.Background())
	if !errors.Is(err, errWebRulesNotConfigured) {
		t.Fatalf("expected %v, got %v", errWebRulesNotConfigured, err)
	}
}

func TestRuntimeReloadWebRulesReturnsContextError(t *testing.T) {
	rt := &Runtime{
		config: config.Config{WebRules: config.WebRulesConfig{Enabled: true}},
		engine: rules.NewEngine(),
		webSource: newRuntimeWebSource(t, runtimeWebSourceFixture{
			body: mustEncodeWebRulesBody(t, "[AutoProxy 0.2.9]\n||unused.example\n"),
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rt.ReloadWebRules(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected %v, got %v", context.Canceled, err)
	}
}

func TestRuntimeReloadWebRulesKeepsOldSnapshotOnLoadError(t *testing.T) {
	webSource := newRuntimeWebSource(t, runtimeWebSourceFixture{
		err: errors.New("download failed"),
	})
	rt := &Runtime{
		config:         config.Config{WebRules: config.WebRulesConfig{Enabled: true}},
		engine:         rules.NewEngine(),
		webSource:      webSource,
		webRulesLoaded: true,
	}
	rt.engine.ReplaceWebRules(mustWebRuleSet(t, "[AutoProxy 0.2.9]\n||old-web.example\n"))

	err := rt.ReloadWebRules(context.Background())
	if err == nil || !strings.Contains(err.Error(), "download failed") {
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
		fileSource: rulesources.FileSource{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rt.ReloadCustomRules(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected %v, got %v", context.Canceled, err)
	}
}

func TestNewWiresManagementServerToRuntimeCallbacks(t *testing.T) {
	workspace := newRuntimeTestWorkspace(t)
	cfg := config.Config{
		ListenAddr: "127.0.0.1:8080",
		WebRules: config.WebRulesConfig{
			Enabled:   true,
			URL:       "https://rules.example.com/list.txt",
			CachePath: filepath.Join(workspace, "web-cache.txt"),
		},
		CustomRules: config.FileRulesConfig{Enabled: true, Path: filepath.Join(workspace, "custom.txt")},
		AutoDetect:  config.AutoDetectConfig{Enabled: true, RulesPath: filepath.Join(workspace, "auto.txt")},
		Management:  config.ManagementConfig{Enabled: true, ListenPort: 9091},
	}
	writeRuntimeRuleFile(t, cfg.CustomRules.Path, "custom.example\n")
	writeRuntimeRuleFile(t, cfg.AutoDetect.RulesPath, "auto.example\n")
	runnerValue, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	rt := runnerValue.(*Runtime)
	rt.webSource = newRuntimeWebSource(t, runtimeWebSourceFixture{
		cachePath: cfg.WebRules.CachePath,
		body:      mustEncodeWebRulesBody(t, "[AutoProxy 0.2.9]\n||web.example\n"),
	})

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
	autoPath := filepath.Join(newRuntimeTestWorkspace(t), "auto.txt")
	rt := &Runtime{
		config:          config.Config{AutoDetect: config.AutoDetectConfig{Enabled: true, RulesPath: autoPath}},
		engine:          rules.NewEngine(),
		autoDetectStore: rulesources.AutoDetectStore{Path: autoPath},
	}
	recorder := runtimeAutoDetectRecorder{runtime: rt}

	if err := recorder.Record(context.Background(), "example.com"); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	content, err := os.ReadFile(autoPath)
	if err != nil {
		t.Fatalf("read auto-detect store: %v", err)
	}
	if strings.TrimSpace(string(content)) != "example.com" {
		t.Fatalf("unexpected persisted host: %q", string(content))
	}
}

func TestRuntimeAutoDetectRecorderRefreshesEngineAfterStoreAppend(t *testing.T) {
	workspace := newRuntimeTestWorkspace(t)
	customPath := filepath.Join(workspace, "custom.txt")
	autoPath := filepath.Join(workspace, "auto.txt")
	if err := os.WriteFile(customPath, nil, 0o644); err != nil {
		t.Fatalf("write custom rules: %v", err)
	}

	rt := &Runtime{
		config: config.Config{
			CustomRules: config.FileRulesConfig{Enabled: true, Path: customPath},
			AutoDetect:  config.AutoDetectConfig{Enabled: true, RulesPath: autoPath},
		},
		engine:          rules.NewEngine(),
		fileSource:      rulesources.FileSource{},
		autoDetectStore: rulesources.AutoDetectStore{Path: autoPath},
	}

	recorder := rt.autoDetectRecorder()
	if recorder == nil {
		t.Fatal("expected auto-detect recorder")
	}

	if err := recorder.Record(context.Background(), " Recorder-Refresh.EXAMPLE:443 "); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}

	if got := rt.engine.Decide("recorder-refresh.example"); got.Source != rules.DecisionSourceAutoDetect {
		t.Fatalf("expected recorder to refresh engine snapshot, got %+v", got)
	}
	if !rt.StatusSummary().AutoDetect.Loaded {
		t.Fatal("expected auto-detect loaded state to become true")
	}
}

func newRuntimeTestWorkspace(t *testing.T) string {
	t.Helper()

	baseDir := filepath.Join("test-artifacts", sanitizeTestName(t.Name())+"-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("create runtime test workspace: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(baseDir)
	})
	return baseDir
}

func sanitizeTestName(name string) string {
	replacer := strings.NewReplacer("/", "-", " ", "-", ":", "-", "\\", "-")
	return replacer.Replace(name)
}

func writeRuntimeRuleFiles(t *testing.T, customBody, autoBody string) (string, string) {
	t.Helper()

	workspace := newRuntimeTestWorkspace(t)
	customPath := filepath.Join(workspace, "custom.txt")
	autoPath := filepath.Join(workspace, "auto.txt")
	writeRuntimeRuleFile(t, customPath, customBody)
	writeRuntimeRuleFile(t, autoPath, autoBody)
	return customPath, autoPath
}

func writeRuntimeRuleFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write runtime rule file %s: %v", path, err)
	}
}

type runtimeWebSourceFixture struct {
	cachePath string
	body      string
	status    int
	err       error
}

func newRuntimeWebSource(t *testing.T, fixture runtimeWebSourceFixture) rulesources.WebSource {
	t.Helper()

	cachePath := fixture.cachePath
	if cachePath == "" {
		cachePath = filepath.Join(newRuntimeTestWorkspace(t), "web-cache.txt")
	}

	status := fixture.status
	if status == 0 {
		status = http.StatusOK
	}

	client := &http.Client{
		Transport: runtimeRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			if fixture.err != nil {
				return nil, fixture.err
			}
			return &http.Response{
				StatusCode: status,
				Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(fixture.body)),
			}, nil
		}),
	}

	return rulesources.WebSource{
		URL:        "https://rules.example.com/list.txt",
		CachePath:  cachePath,
		HTTPClient: client,
	}
}

type runtimeRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f runtimeRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func mustEncodeWebRulesBody(t *testing.T, body string) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString([]byte(body))
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
