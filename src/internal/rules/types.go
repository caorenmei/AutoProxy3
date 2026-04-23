package rules

import (
	"net/url"
	"path"
	"strings"
)

// WebRuleSet 表示从在线 AutoProxy 规则源解析得到的规则集合。
//
// 该类型仅负责保存解析结果，并提供基于主机名或完整 URL 的只读匹配能力。
// 调用方可通过 ProxyHost、DirectHost 与 ProxyURL 区分“需要代理”和“显式直连”两类命中结果。
type WebRuleSet struct {
	proxyDomains     []string
	directDomains    []string
	proxyURLPrefixes []string
}

// ProxyHost 判断给定主机名是否命中需要代理的域名规则。
//
// 参数 host 为待匹配的主机名，函数会执行大小写归一化并按域名后缀规则匹配，
// 同时支持规则命中裸域名及其子域名。返回 true 表示命中代理规则；返回 false 表示未命中。
func (s WebRuleSet) ProxyHost(host string) bool {
	return matchDomainRules(host, s.proxyDomains)
}

// DirectHost 判断给定主机名是否命中显式直连的域名规则。
//
// 参数 host 为待匹配的主机名，函数会执行大小写归一化并按域名后缀规则匹配，
// 同时支持规则命中裸域名及其子域名。返回 true 表示命中直连规则；返回 false 表示未命中。
func (s WebRuleSet) DirectHost(host string) bool {
	return matchDomainRules(host, s.directDomains)
}

// ProxyURL 判断给定完整 URL 是否命中需要代理的 URL 前缀规则。
//
// 参数 rawURL 为待匹配的原始 URL 字符串；函数会对协议与主机名执行大小写归一化，
// 忽略 fragment，并按前缀规则匹配。返回 true 表示命中代理 URL 规则；返回 false 表示未命中。
func (s WebRuleSet) ProxyURL(rawURL string) bool {
	normalized := normalizeURLForMatch(rawURL)
	for _, prefix := range s.proxyURLPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

// HostRuleSet 表示从本地或自动探测规则文件解析得到的主机规则集合。
//
// 该类型仅按主机名进行匹配，不处理 URL 路径信息。
// 规则同时支持精确主机名与包含 * 的通配模式，适用于本地规则和自动探测规则复用；
// 其中 `*.example.com` 这类模式只匹配子域名，不匹配裸域 `example.com`。
type HostRuleSet struct {
	exactHosts       map[string]struct{}
	wildcardPatterns []string
}

// Match 判断给定主机名是否命中主机规则集合。
//
// 参数 host 为待匹配的主机名，函数会执行大小写归一化，并依次检查精确主机规则和通配模式规则。
// 该方法同时适用于本地规则与自动探测规则。返回 true 表示命中任一规则；返回 false 表示未命中。
func (s HostRuleSet) Match(host string) bool {
	normalized := normalizeHost(host)
	if normalized == "" {
		return false
	}
	if _, ok := s.exactHosts[normalized]; ok {
		return true
	}
	for _, pattern := range s.wildcardPatterns {
		matched, err := path.Match(pattern, normalized)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func matchDomainRules(host string, rules []string) bool {
	normalized := normalizeHost(host)
	if normalized == "" {
		return false
	}
	for _, rule := range rules {
		if normalized == rule || strings.HasSuffix(normalized, "."+rule) {
			return true
		}
	}
	return false
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func normalizeURLForMatch(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(strings.ToLower(parsed.Scheme))
	builder.WriteString("://")
	builder.WriteString(strings.ToLower(parsed.Host))
	builder.WriteString(parsed.EscapedPath())
	if parsed.RawQuery != "" {
		builder.WriteString("?")
		builder.WriteString(parsed.RawQuery)
	}
	return builder.String()
}

func (s WebRuleSet) clone() WebRuleSet {
	return WebRuleSet{
		proxyDomains:     append([]string(nil), s.proxyDomains...),
		directDomains:    append([]string(nil), s.directDomains...),
		proxyURLPrefixes: append([]string(nil), s.proxyURLPrefixes...),
	}
}

func (s HostRuleSet) clone() HostRuleSet {
	cloned := HostRuleSet{
		exactHosts:       make(map[string]struct{}, len(s.exactHosts)),
		wildcardPatterns: append([]string(nil), s.wildcardPatterns...),
	}
	for host := range s.exactHosts {
		cloned.exactHosts[host] = struct{}{}
	}
	return cloned
}
