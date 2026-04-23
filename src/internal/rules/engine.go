package rules

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
)

// Decision 表示规则引擎对单个目标的统一判路结果。
//
// UseProxy 表示是否应走上游代理；Source 表示命中的规则来源，
// 可能值为 `web`、`custom`、`auto-detect` 或 `default`；
// Reason 表示更细粒度的命中原因，便于后续日志与代理层复用。
// 当 UseProxy 为 false 时，调用方应执行直连或后续默认直连流程。
type Decision struct {
	UseProxy bool
	Source   string
	Reason   string
}

// Engine 表示统一规则引擎。
//
// 该类型维护在线规则、本地规则与自动探测规则的只读内存快照。
// 请求路径通过只读方法执行匹配；重载路径通过替换方法原子更新快照。
// Engine 自身是并发安全的，可被多个 goroutine 同时读取与更新。
type Engine struct {
	mu         sync.RWMutex
	web        WebRuleSet
	custom     HostRuleSet
	autoDetect HostRuleSet
}

type engineSnapshot struct {
	web        WebRuleSet
	custom     HostRuleSet
	autoDetect HostRuleSet
}

// NewEngine 创建并返回一个新的规则引擎实例。
//
// 返回值为可立即使用的空引擎。空引擎在未加载任何规则时会对所有目标返回默认直连决策。
func NewEngine() *Engine {
	return &Engine{}
}

// Decide 根据输入目标执行统一判路。
//
// 参数 target 可以是主机名，也可以是绝对 URL。若输入能解析为绝对 URL，
// 函数会按 URL 语义同时检查 URL 前缀规则与主机规则；否则按主机名语义判定。
// 返回值包含是否走代理、命中来源与命中原因。
func (e *Engine) Decide(target string) Decision {
	trimmed := strings.TrimSpace(target)
	if looksLikeAbsoluteURL(trimmed) {
		return e.DecideURL(trimmed)
	}
	return e.DecideHost(trimmed)
}

// DecideHost 根据主机名执行判路。
//
// 参数 host 为待判定的目标主机名，可包含大小写差异或端口信息；函数会进行规范化。
// 返回值中若 UseProxy 为 true，表示在线正向规则、本地规则或自动探测规则命中；
// 若命中在线 `@@` 直连规则或未命中任何规则，则返回直连决策。
func (e *Engine) DecideHost(host string) Decision {
	return e.snapshot().decideHost(normalizeDecisionHost(host))
}

// DecideURL 根据完整 URL 执行判路。
//
// 参数 rawURL 为绝对 URL。函数会优先检查在线规则中的 `@@` 直连主机规则，
// 再检查在线 URL 前缀规则、在线域名规则、本地规则与自动探测规则。
// 返回值包含是否走代理、命中来源与命中原因；非法或不完整 URL 会返回默认直连。
func (e *Engine) DecideURL(rawURL string) Decision {
	return e.snapshot().decideURL(rawURL)
}

// ReplaceWebRules 原子替换在线规则快照。
//
// 参数 set 为新的在线规则集合。函数会复制输入快照，避免调用方后续修改影响引擎内部状态。
func (e *Engine) ReplaceWebRules(set WebRuleSet) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.web = set.clone()
}

// ReplaceCustomRules 原子替换本地规则快照。
//
// 参数 set 为新的本地规则集合。函数会复制输入快照，避免调用方后续修改影响引擎内部状态。
func (e *Engine) ReplaceCustomRules(set HostRuleSet) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.custom = set.clone()
}

// ReplaceAutoDetectRules 原子替换自动探测规则快照。
//
// 参数 set 为新的自动探测规则集合。函数会复制输入快照，避免调用方后续修改影响引擎内部状态。
func (e *Engine) ReplaceAutoDetectRules(set HostRuleSet) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoDetect = set.clone()
}

// ReloadCustomSources 同时重载本地规则源与自动探测规则源。
//
// 参数 customReader 提供本地规则内容，参数 autoReader 提供自动探测规则内容。
// 仅当两个输入都成功解析后，函数才会原子替换当前内存快照。
// 任一解析失败都会返回带上下文的错误，并保持当前快照不变。
func (e *Engine) ReloadCustomSources(customReader io.Reader, autoReader io.Reader) error {
	customSet, err := ParseHostRules(customReader)
	if err != nil {
		return fmt.Errorf("load custom rules: %w", err)
	}

	autoDetectSet, err := ParseHostRules(autoReader)
	if err != nil {
		return fmt.Errorf("load auto-detect rules: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.custom = customSet.clone()
	e.autoDetect = autoDetectSet.clone()
	return nil
}

func (e *Engine) snapshot() engineSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return engineSnapshot{
		web:        e.web,
		custom:     e.custom,
		autoDetect: e.autoDetect,
	}
}

func (s engineSnapshot) decideHost(host string) Decision {
	if s.web.DirectHost(host) {
		return Decision{Source: "web", Reason: "web-direct"}
	}
	if s.web.ProxyHost(host) {
		return Decision{UseProxy: true, Source: "web", Reason: "web-proxy-host"}
	}
	if s.custom.Match(host) {
		return Decision{UseProxy: true, Source: "custom", Reason: "custom-proxy-host"}
	}
	if s.autoDetect.Match(host) {
		return Decision{UseProxy: true, Source: "auto-detect", Reason: "auto-detect-proxy-host"}
	}
	return Decision{Source: "default", Reason: "direct-default"}
}

func (s engineSnapshot) decideURL(rawURL string) Decision {
	host := hostFromURL(rawURL)
	if s.web.DirectHost(host) {
		return Decision{Source: "web", Reason: "web-direct"}
	}
	if s.web.ProxyURL(rawURL) {
		return Decision{UseProxy: true, Source: "web", Reason: "web-proxy-url"}
	}
	if s.web.ProxyHost(host) {
		return Decision{UseProxy: true, Source: "web", Reason: "web-proxy-host"}
	}
	if s.custom.Match(host) {
		return Decision{UseProxy: true, Source: "custom", Reason: "custom-proxy-host"}
	}
	if s.autoDetect.Match(host) {
		return Decision{UseProxy: true, Source: "auto-detect", Reason: "auto-detect-proxy-host"}
	}
	return Decision{Source: "default", Reason: "direct-default"}
}

func normalizeDecisionHost(host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return normalizeHost(strings.Trim(trimmed, "[]"))
	}

	if parsedHost, _, err := net.SplitHostPort(trimmed); err == nil {
		return normalizeHost(parsedHost)
	}

	return normalizeHost(trimmed)
}

func hostFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return ""
	}
	return normalizeHost(parsed.Hostname())
}

func looksLikeAbsoluteURL(target string) bool {
	parsed, err := url.Parse(target)
	return err == nil && parsed.IsAbs() && parsed.Host != ""
}
