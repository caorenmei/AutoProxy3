package rules

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// ParseWebRules 从在线规则源读取并解析 Base64 编码的 AutoProxy 规则文本。
//
// 参数 r 提供 HTTP 响应体中的 Base64 文本内容；函数会先完成 Base64 解码，
// 再解析支持的 AutoProxy 规则行，包括头行、域名规则、直连排除规则与 URL 前缀规则。
// 当读取输入、解码 Base64 或扫描文本失败时，函数会返回带上下文的错误。
func ParseWebRules(r io.Reader) (WebRuleSet, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return WebRuleSet{}, fmt.Errorf("read web rules: %w", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(stripWhitespace(string(body)))
	if err != nil {
		return WebRuleSet{}, fmt.Errorf("decode base64: %w", err)
	}

	var set WebRuleSet
	scanner := bufio.NewScanner(strings.NewReader(string(decoded)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "[AutoProxy") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "@@||"):
			rule := normalizeHost(strings.TrimPrefix(line, "@@||"))
			if rule != "" {
				set.directDomains = append(set.directDomains, rule)
			}
		case strings.HasPrefix(line, "||"):
			rule := normalizeHost(strings.TrimPrefix(line, "||"))
			if rule != "" {
				set.proxyDomains = append(set.proxyDomains, rule)
			}
		case strings.HasPrefix(line, "|"):
			rule := normalizeURLForMatch(strings.TrimPrefix(line, "|"))
			if rule != "" {
				set.proxyURLPrefixes = append(set.proxyURLPrefixes, rule)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return WebRuleSet{}, fmt.Errorf("scan web rules: %w", err)
	}

	return set, nil
}

func stripWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}
