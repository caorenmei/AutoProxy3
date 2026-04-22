package rules

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParseHostRules 从纯文本规则源读取并解析主机名规则。
//
// 参数 r 提供本地规则或自动探测规则文件内容；函数支持空行、以 # 开头的注释行、
// 精确主机规则以及包含 * 的主机通配规则。返回的规则集合仅按主机名匹配，不处理路径。
// 当读取输入或扫描文本失败时，函数会返回带上下文的错误。
func ParseHostRules(r io.Reader) (HostRuleSet, error) {
	set := HostRuleSet{exactHosts: make(map[string]struct{})}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		rule := normalizeHost(line)
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "*") {
			set.wildcardPatterns = append(set.wildcardPatterns, rule)
			continue
		}
		set.exactHosts[rule] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return HostRuleSet{}, fmt.Errorf("scan host rules: %w", err)
	}

	return set, nil
}
