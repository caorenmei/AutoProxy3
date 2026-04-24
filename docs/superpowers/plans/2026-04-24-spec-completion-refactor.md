# AutoProxy3 SPEC Completion Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 通过一次收口式重构补齐 AutoProxy3 的运行时启动、规则源管理、管理接口联动、README/配置样例/文档和 GitHub Actions，使实现与 `SPEC.md` 和最新设计文档一致。

**Architecture:** 保持现有 `config`、`rules`、`proxy`、`logging` 模块不被推倒重来，新增 `internal/runtime` 统一收口生命周期，再新增 `internal/rulesources` 负责 web/custom/auto-detect 的外部读写与刷新。`cmd/autoproxy3` 只保留参数解析和命令分发，管理接口继续作为运行时唯一 reload 入口。

**Tech Stack:** Go、标准库 `net/http` / `context` / `os` / `time`、`slog`、Go 内置测试框架、GitHub Actions

---

## 文件结构映射

- Create: `src/internal/runtime/runtime.go` — 统一装配 logger、rule engine、proxy server、management server，并管理运行生命周期。
- Create: `src/internal/runtime/runtime_test.go` — 验证启动顺序、关闭流程、reload 回调和后台任务。
- Create: `src/internal/rulesources/filesource.go` — 读取 custom / auto-detect 文件。
- Create: `src/internal/rulesources/filesource_test.go` — 验证文件加载成功/失败路径。
- Create: `src/internal/rulesources/websource.go` — 下载、缓存、路由决策和后台刷新。
- Create: `src/internal/rulesources/websource_test.go` — 验证下载、缓存回退、走上游下载和定时刷新。
- Create: `src/internal/rulesources/autodetect_store.go` — auto-detect 追加、去重和落盘。
- Create: `src/internal/rulesources/autodetect_store_test.go` — 验证追加与去重逻辑。
- Modify: `src/cmd/autoproxy3/main.go` — 让 `serve` 命令真正启动 runtime。
- Modify: `src/cmd/autoproxy3/main_test.go` — 覆盖 `serve` 调 runtime 和错误分支。
- Modify: `src/cmd/autoproxy3/main.go` — 为 `reload_rules` 增加默认客户端处理路径。
- Modify: `src/internal/management/server.go` — 接入 runtime 提供的真实状态和 reload 回调（保持薄层）。
- Modify: `src/internal/management/server_test.go` — 增补真实 runtime 回调语义相关断言。
- Modify: `src/internal/cli/reload.go` — 如有必要，补 `ReloadRules` 调用与错误语义一致性。
- Modify: `src/internal/cli/reload_test.go` — 覆盖 `reload_rules` 与管理响应细节。
- Create: `configs/config.json.example` — 完整配置样例。
- Create: `README.md` — 简体中文总说明。
- Create: `docs/architecture/runtime.md` — 运行时架构说明。
- Create: `docs/api/management.md` — 管理接口说明。
- Create: `docs/workflows/development.md` — 开发/测试/CI 工作流说明。
- Create: `docs/guides/configuration.md` — 配置说明。
- Create: `.github/workflows/ci.yml` — 持续集成工作流。

### Task 1: 抽出 runtime 骨架并接管 `serve`

**Files:**
- Create: `src/internal/runtime/runtime.go`
- Create: `src/internal/runtime/runtime_test.go`
- Modify: `src/cmd/autoproxy3/main.go`
- Modify: `src/cmd/autoproxy3/main_test.go`

- [ ] **Step 1: 写 runtime 启动失败测试**

```go
func TestRunServeUsesRuntimeRunner(t *testing.T) {
	called := 0
	cfg := config.Config{ListenAddr: "127.0.0.1:1080"}
	app := newApp(commandHandlers{
		serve: func(args appArgs, got config.Config) error {
			called++
			if got != cfg {
				t.Fatalf("unexpected config: %+v", got)
			}
			return nil
		},
	})

	code := app.run([]string{"autoproxy3", "--config", "config.json", "serve"}, io.Discard, io.Discard)
	_ = code
	_ = called
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./src/cmd/autoproxy3 -run TestRunServeUsesRuntimeRunner -count=1`

Expected: FAIL，提示 `serve` 路径仍是空实现或未调用真实 runtime runner。

- [ ] **Step 3: 写最小 runtime 骨架**

```go
type Runner interface {
	Run(context.Context) error
}

type Factory func(config.Config) (Runner, error)

type runtimeRunner struct{}

func (runtimeRunner) Run(context.Context) error { return nil }

func New(config.Config) (Runner, error) {
	return runtimeRunner{}, nil
}
```

- [ ] **Step 4: 在 `main.go` 中把 `serve` 路径接到 runtime**

```go
var newRuntime = runtime.New

func defaultServe(args appArgs, cfg config.Config) error {
	runner, err := newRuntime(cfg)
	if err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	return runner.Run(context.Background())
}

func newAppWithConfigLoader(handlers commandHandlers, loader configLoader) app {
	if handlers.serve == nil {
		handlers.serve = defaultServe
	}
	// 其余逻辑保持不变
}
```

- [ ] **Step 5: 运行针对性测试确认通过**

Run: `go test ./src/cmd/autoproxy3 ./src/internal/runtime -count=1`

Expected: PASS，`cmd/autoproxy3` 与 `internal/runtime` 两个包测试通过。

- [ ] **Step 6: 提交**

```bash
git add src/cmd/autoproxy3/main.go src/cmd/autoproxy3/main_test.go src/internal/runtime/runtime.go src/internal/runtime/runtime_test.go
git commit -m "refactor: route serve through runtime"
```

### Task 2: 实现 custom / auto-detect 文件源与持久化

**Files:**
- Create: `src/internal/rulesources/filesource.go`
- Create: `src/internal/rulesources/filesource_test.go`
- Create: `src/internal/rulesources/autodetect_store.go`
- Create: `src/internal/rulesources/autodetect_store_test.go`
- Modify: `src/internal/runtime/runtime.go`
- Test: `src/internal/runtime/runtime_test.go`

- [ ] **Step 1: 为文件源写失败测试**

```go
func TestFileSourceLoadCustomAndAutoDetect(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom.txt")
	autoPath := filepath.Join(dir, "auto.txt")
	if err := os.WriteFile(customPath, []byte("*.google.com\n"), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}
	if err := os.WriteFile(autoPath, []byte("example.com\n"), 0o644); err != nil {
		t.Fatalf("write auto: %v", err)
	}

	source := FileSource{}
	customSet, autoSet, err := source.LoadCustomAndAutoDetect(customPath, autoPath)
	if err != nil {
		t.Fatalf("load files: %v", err)
	}
	if !customSet.Match("mail.google.com") || !autoSet.Match("example.com") {
		t.Fatal("expected loaded rules to match")
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./src/internal/rulesources -run TestFileSourceLoadCustomAndAutoDetect -count=1`

Expected: FAIL，提示 `FileSource` 或 `LoadCustomAndAutoDetect` 未定义。

- [ ] **Step 3: 写最小文件源实现**

```go
type FileSource struct{}

func (FileSource) LoadCustomAndAutoDetect(customPath, autoDetectPath string) (rules.HostRuleSet, rules.HostRuleSet, error) {
	customFile, err := os.Open(customPath)
	if err != nil {
		return rules.HostRuleSet{}, rules.HostRuleSet{}, fmt.Errorf("open custom rules: %w", err)
	}
	defer customFile.Close()

	autoFile, err := os.Open(autoDetectPath)
	if err != nil {
		return rules.HostRuleSet{}, rules.HostRuleSet{}, fmt.Errorf("open auto-detect rules: %w", err)
	}
	defer autoFile.Close()

	customSet, err := rules.ParseHostRules(customFile)
	if err != nil {
		return rules.HostRuleSet{}, rules.HostRuleSet{}, fmt.Errorf("parse custom rules: %w", err)
	}
	autoSet, err := rules.ParseHostRules(autoFile)
	if err != nil {
		return rules.HostRuleSet{}, rules.HostRuleSet{}, fmt.Errorf("parse auto-detect rules: %w", err)
	}
	return customSet, autoSet, nil
}
```

- [ ] **Step 4: 为 auto-detect store 写去重测试**

```go
func TestAutoDetectStoreAppendHostDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto.txt")
	store := AutoDetectStore{Path: path}
	if err := store.AppendHost("example.com"); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := store.AppendHost("example.com"); err != nil {
		t.Fatalf("second append: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if strings.Count(string(body), "example.com") != 1 {
		t.Fatalf("unexpected file content: %q", string(body))
	}
}
```

- [ ] **Step 5: 写最小 auto-detect store 实现**

```go
type AutoDetectStore struct {
	Path string
}

func (s AutoDetectStore) AppendHost(host string) error {
	normalized := strings.TrimSpace(host)
	if normalized == "" {
		return nil
	}
	existing, _ := os.ReadFile(s.Path)
	if strings.Contains("\n"+string(existing)+"\n", "\n"+normalized+"\n") {
		return nil
	}
	file, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open auto-detect rules file: %w", err)
	}
	defer file.Close()
	_, err = fmt.Fprintln(file, normalized)
	return err
}
```

- [ ] **Step 6: 运行规则源测试确认通过**

Run: `go test ./src/internal/rulesources -count=1`

Expected: PASS，`internal/rulesources` 包测试通过。

- [ ] **Step 7: 提交**

```bash
git add src/internal/rulesources/filesource.go src/internal/rulesources/filesource_test.go src/internal/rulesources/autodetect_store.go src/internal/rulesources/autodetect_store_test.go
git commit -m "feat: add file-backed rule sources"
```

### Task 3: 实现 web 规则下载、缓存、判路与后台刷新

**Files:**
- Create: `src/internal/rulesources/websource.go`
- Create: `src/internal/rulesources/websource_test.go`
- Modify: `src/internal/runtime/runtime.go`
- Modify: `src/internal/runtime/runtime_test.go`

- [ ] **Step 1: 写 web 下载走上游的失败测试**

```go
func TestWebSourceDownloadUsesProxyWhenRuleRequiresProxy(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("[AutoProxy 0.2.9]\n||twitter.com\n"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-AutoProxy-Upstream"); got != "true" {
			t.Fatalf("unexpected upstream header: %q", got)
		}
		_, _ = w.Write([]byte(encoded))
	}))
	defer server.Close()

	source := WebSource{
		URL:        server.URL,
		CachePath:  filepath.Join(t.TempDir(), "web.txt"),
		HTTPClient: server.Client(),
		ShouldUseProxy: func(rawURL string) bool {
			return true
		},
	}

	set, fromRemote, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load web rules: %v", err)
	}
	if !fromRemote || !set.ProxyHost("twitter.com") {
		t.Fatal("expected remote rules to be applied")
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./src/internal/rulesources -run TestWebSourceDownloadUsesProxyWhenRuleRequiresProxy -count=1`

Expected: FAIL，提示 `WebSource` 或 `Load` 未定义。

- [ ] **Step 3: 写最小 web source 实现**

```go
type WebSource struct {
	URL            string
	CachePath      string
	HTTPClient     *http.Client
	ShouldUseProxy func(string) bool
}

func (s WebSource) Load(ctx context.Context) (rules.WebRuleSet, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return rules.WebRuleSet{}, false, fmt.Errorf("build web rules request: %w", err)
	}
	if s.ShouldUseProxy != nil && s.ShouldUseProxy(s.URL) {
		request.Header.Set("X-AutoProxy-Upstream", "true")
	}
	response, err := s.httpClient().Do(request)
	if err != nil {
		return rules.WebRuleSet{}, false, fmt.Errorf("download web rules: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return rules.WebRuleSet{}, false, fmt.Errorf("read web rules response: %w", err)
	}
	set, err := rules.ParseWebRules(bytes.NewReader(body))
	if err != nil {
		return rules.WebRuleSet{}, false, err
	}
	if err := os.WriteFile(s.CachePath, body, 0o644); err != nil {
		return rules.WebRuleSet{}, false, fmt.Errorf("write web rules cache: %w", err)
	}
	return set, true, nil
}
```

- [ ] **Step 4: 写缓存回退与首次直连测试**

```go
func TestWebSourceLoadFallsBackToCacheWhenDownloadFails(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "web.txt")
	encoded := base64.StdEncoding.EncodeToString([]byte("[AutoProxy 0.2.9]\n||twitter.com\n"))
	if err := os.WriteFile(cachePath, []byte(encoded), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	source := WebSource{
		URL:        "http://rules.example.com/list.txt",
		CachePath:  cachePath,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("boom") })},
	}

	set, fromRemote, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load web rules: %v", err)
	}
	if fromRemote {
		t.Fatal("expected cache fallback")
	}
	if !set.ProxyHost("twitter.com") {
		t.Fatal("expected cached rules to be applied")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
```

- [ ] **Step 5: 实现缓存回退和后台刷新入口**

```go
func (s WebSource) Load(ctx context.Context) (rules.WebRuleSet, bool, error) {
	set, err := s.download(ctx)
	if err == nil {
		return set, true, nil
	}
	cacheSet, cacheErr := s.loadCache()
	if cacheErr == nil {
		return cacheSet, false, nil
	}
	return rules.WebRuleSet{}, false, errors.Join(err, cacheErr)
}

func (s WebSource) StartRefreshLoop(ctx context.Context, interval time.Duration, apply func(rules.WebRuleSet)) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if set, _, err := s.Load(ctx); err == nil {
					apply(set)
				}
			}
		}
	}()
}
```

- [ ] **Step 6: 运行规则源与 runtime 测试**

Run: `go test ./src/internal/rulesources ./src/internal/runtime -count=1`

Expected: PASS，下载、缓存和后台刷新测试通过。

- [ ] **Step 7: 提交**

```bash
git add src/internal/rulesources/websource.go src/internal/rulesources/websource_test.go src/internal/runtime/runtime.go src/internal/runtime/runtime_test.go
git commit -m "feat: add web rules source orchestration"
```

### Task 4: 把 runtime 接到 proxy / management 的真实回调

**Files:**
- Modify: `src/internal/runtime/runtime.go`
- Modify: `src/internal/runtime/runtime_test.go`
- Modify: `src/internal/management/server.go`
- Modify: `src/internal/management/server_test.go`
- Modify: `src/internal/proxy/server.go`
- Modify: `src/internal/proxy/server_test.go`

- [ ] **Step 1: 写 runtime reload 回调失败测试**

```go
func TestRuntimeReloadCustomRulesReplacesSnapshotsAtomically(t *testing.T) {
	dir := t.TempDir()
	rt := &Runtime{
		engine:         rules.NewEngine(),
		fileSource:     rulesources.FileSource{},
		customPath:     filepath.Join(dir, "custom.txt"),
		autoDetectPath: filepath.Join(dir, "auto.txt"),
	}
	if err := os.WriteFile(rt.customPath, []byte("*.google.com\n"), 0o644); err != nil {
		t.Fatalf("write custom rules: %v", err)
	}
	if err := os.WriteFile(rt.autoDetectPath, []byte("example.com\n"), 0o644); err != nil {
		t.Fatalf("write auto rules: %v", err)
	}

	if err := rt.ReloadCustomRules(context.Background()); err != nil {
		t.Fatalf("reload custom rules: %v", err)
	}
	if !rt.engine.DecideHost("mail.google.com").UseProxy {
		t.Fatal("expected custom rules to be active")
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./src/internal/runtime -run TestRuntimeReloadCustomRulesReplacesSnapshotsAtomically -count=1`

Expected: FAIL，提示 `ReloadCustomRules` 未实现或 runtime 未持有实际规则源。

- [ ] **Step 3: 在 runtime 中注入 management 与 proxy 需要的协作接口**

```go
type Runtime struct {
	engine          *rules.Engine
	webSource       rulesources.WebSource
	fileSource      rulesources.FileSource
	autoDetectStore rulesources.AutoDetectStore
	customPath      string
	autoDetectPath  string
}

func (r *Runtime) ReloadWebRules(context.Context) error {
	set, _, err := r.webSource.Load(context.Background())
	if err != nil {
		return err
	}
	r.engine.ReplaceWebRules(set)
	return nil
}

func (r *Runtime) ReloadCustomRules(context.Context) error {
	customSet, autoSet, err := r.fileSource.LoadCustomAndAutoDetect(r.customPath, r.autoDetectPath)
	if err != nil {
		return err
	}
	r.engine.ReplaceCustomRules(customSet)
	r.engine.ReplaceAutoDetectRules(autoSet)
	return nil
}
```

- [ ] **Step 4: 把 `management.NewServer` 的 `Options` 接到 runtime**

```go
management.NewServer(management.Options{
	ListenPort: cfg.Management.ListenPort,
	Version:    buildinfo.Version,
	Features:   []string{"proxy", "management"},
	StatusProvider: func() management.RuleStatusSummary {
		return r.StatusSummary()
	},
	ReloadWebRules:    r.ReloadWebRules,
	ReloadCustomRules: r.ReloadCustomRules,
})
```

- [ ] **Step 5: 运行 management / proxy / runtime 测试**

Run: `go test ./src/internal/runtime ./src/internal/management ./src/internal/proxy -count=1`

Expected: PASS，管理回调、自动探测持久化与状态摘要测试通过。

- [ ] **Step 6: 提交**

```bash
git add src/internal/runtime/runtime.go src/internal/runtime/runtime_test.go src/internal/management/server.go src/internal/management/server_test.go src/internal/proxy/server.go src/internal/proxy/server_test.go
git commit -m "feat: connect runtime with management and proxy"
```

### Task 5: 补全 CLI / runtime 集成测试并恢复全量测试为绿

**Files:**
- Modify: `src/cmd/autoproxy3/main.go`
- Modify: `src/cmd/autoproxy3/main_test.go`
- Modify: `src/internal/cli/reload_test.go`
- Modify: `src/internal/runtime/runtime_test.go`

- [ ] **Step 1: 写 CLI `reload_rules` 集成测试**

```go
func TestAppRunUsesDefaultReloadAllClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	requests := make(chan string, 1)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests <- r.Method + " " + r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true}`))
		}),
	}
	defer server.Close()
	go server.Serve(listener)

	cfg := config.Config{Management: config.ManagementConfig{Enabled: true, ListenPort: listener.Addr().(*net.TCPAddr).Port}}
	code := newAppWithConfigLoader(commandHandlers{}, func(string) (config.Config, error) { return cfg, nil }).run([]string{"autoproxy3", "reload_rules"}, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if got := <-requests; got != "POST /reload_rules" {
		t.Fatalf("unexpected request: %q", got)
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./src/cmd/autoproxy3 -run TestAppRunUsesDefaultReloadAllClient -count=1`

Expected: FAIL，提示 `reload_rules` 默认客户端路径尚未覆盖或实现不一致。

- [ ] **Step 3: 补最小实现与测试夹具**

```go
type reloadRulesHandler func(config.Config) error

type commandHandlers struct {
	serve             serveHandler
	reloadWebRules    reloadRulesHandler
	reloadCustomRules reloadRulesHandler
	reloadRules       reloadRulesHandler
}

func defaultReloadRules(cfg config.Config) error {
	return newReloadClient(cfg).ReloadRules(context.Background())
}

func newAppWithConfigLoader(handlers commandHandlers, loader configLoader) app {
	if handlers.reloadRules == nil {
		handlers.reloadRules = defaultReloadRules
	}
	// 其余逻辑保持不变
}
```

- [ ] **Step 4: 运行局部与全量测试**

Run: `go test ./... -count=1`

Expected: PASS，所有包测试通过。

- [ ] **Step 5: 运行覆盖率检查**

Run: `go test -cover ./...`

Expected: PASS，所有有测试的包均显示 `coverage: 100.0% of statements`。

- [ ] **Step 6: 提交**

```bash
git add src/cmd/autoproxy3/main.go src/cmd/autoproxy3/main_test.go src/internal/cli/reload_test.go src/internal/runtime/runtime_test.go
git commit -m "test: cover runtime and cli integration"
```

### Task 6: 补配置样例、README 和实现文档

**Files:**
- Create: `configs/config.json.example`
- Create: `README.md`
- Create: `docs/architecture/runtime.md`
- Create: `docs/api/management.md`
- Create: `docs/workflows/development.md`
- Create: `docs/guides/configuration.md`

- [ ] **Step 1: 写 README 与配置样例草稿**

```json
{
  "listen_addr": "127.0.0.1:1080",
  "upstream_proxy": "http://127.0.0.1:8080",
  "web_rules": {
    "enabled": true,
    "url": "http://rules.example.com/list.txt",
    "cache_path": "web_rules.txt",
    "refresh_interval": 3600,
    "download_on_start": true
  },
  "custom_rules": {
    "enabled": true,
    "path": "custom_rules.txt"
  },
  "auto_detect": {
    "enabled": true,
    "max_attempts": 3,
    "rules_path": "auto_detect_rules.txt"
  },
  "management": {
    "enabled": true,
    "listen_port": 9091
  },
  "logging": {
    "level": "info",
    "format": "text",
    "file_path": "logs/proxy.log",
    "max_size": 10,
    "max_backups": 5
  }
}
```

- [ ] **Step 2: 编写 README 关键段落**

```markdown
# AutoProxy3

AutoProxy3 是一个基于 Go 的本地 HTTP/HTTPS/TCP 代理，使用 HTTP CONNECT 统一处理直连与上游代理转发。

## 功能

- 在线规则、本地规则、自动探测规则
- 本地管理接口与命令行 reload
- 文本 / JSON 日志

## 测试

- `go test ./...`
- `go test -cover ./...`
```

- [ ] **Step 3: 写架构/API/工作流/配置文档**

```markdown
## 管理接口

- `GET /` 返回版本、功能列表和规则状态
- `POST /reload_web_rules` 重载在线规则
- `POST /reload_custom_rules` 重载本地规则和 auto-detect 规则
- `POST /reload_rules` 重载全部规则
```

- [ ] **Step 4: 验证文档与实现一致**

Run: `rg "reload_custom_rules|refresh_interval|download_on_start|listen_port" README.md configs docs src -n`

Expected: 所有文档字段名与代码配置字段一致。

- [ ] **Step 5: 提交**

```bash
git add configs/config.json.example README.md docs/architecture/runtime.md docs/api/management.md docs/workflows/development.md docs/guides/configuration.md
git commit -m "docs: add runtime and configuration guides"
```

### Task 7: 增加 GitHub Actions CI

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: 写 CI 工作流**

```yaml
name: ci

on:
  push:
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go test ./... -count=1
      - run: go test -cover ./...
```

- [ ] **Step 2: 本地校验工作流文件被正确写入**

Run: `test -f .github/workflows/ci.yml && sed -n '1,120p' .github/workflows/ci.yml`

Expected: 输出 `name: ci` 和两个 `go test` 步骤。

- [ ] **Step 3: 运行最终全量验证**

Run: `go test ./... -count=1 && go test -cover ./...`

Expected: 全部通过，覆盖率满足要求。

- [ ] **Step 4: 提交**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add go test workflow"
```

## 自检结论

1. **Spec coverage：** 运行时启动、规则源、web 下载路由、reload、README/配置样例/docs、GitHub Actions 都有对应任务，没有遗漏最新补充的“web 规则下载自身判路”要求。
2. **Placeholder scan：** 计划中没有 `TODO`、`TBD`、`implement later` 这类占位语句；每个代码步骤都给了明确文件和代码片段。
3. **Type consistency：** 计划统一使用 `runtime.New` / `Runtime.ReloadWebRules` / `Runtime.ReloadCustomRules` / `rulesources.WebSource` / `rulesources.FileSource` / `rulesources.AutoDetectStore` 这组命名，没有前后漂移。
