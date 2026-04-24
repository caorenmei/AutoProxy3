package rulesources

import (
	"fmt"
	"os"

	"github.com/caorenmei/autoproxy3/src/internal/rules"
)

// FileSource 表示基于本地文件的 custom 与 auto-detect 规则源。
//
// 该类型负责打开规则文件并调用 rules.ParseHostRules 解析内容，
// 供运行时在后续任务中统一加载本地规则快照。
type FileSource struct{}

// LoadCustomAndAutoDetect 读取并解析 custom 与 auto-detect 规则文件。
//
// 参数 customPath 与 autoDetectPath 分别指定两个规则文件路径；
// 返回值依次为 custom 规则集合、auto-detect 规则集合以及错误。
// 任一文件无法打开或解析时，函数会返回带上下文的错误。
func (FileSource) LoadCustomAndAutoDetect(customPath, autoDetectPath string) (rules.HostRuleSet, rules.HostRuleSet, error) {
	customFile, err := os.Open(customPath)
	if err != nil {
		return rules.HostRuleSet{}, rules.HostRuleSet{}, fmt.Errorf("open custom rules: %w", err)
	}
	defer customFile.Close()

	customSet, err := rules.ParseHostRules(customFile)
	if err != nil {
		return rules.HostRuleSet{}, rules.HostRuleSet{}, fmt.Errorf("parse custom rules: %w", err)
	}

	autoDetectFile, err := os.Open(autoDetectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return customSet, rules.HostRuleSet{}, nil
		}
		return rules.HostRuleSet{}, rules.HostRuleSet{}, fmt.Errorf("open auto-detect rules: %w", err)
	}
	defer autoDetectFile.Close()

	autoDetectSet, err := rules.ParseHostRules(autoDetectFile)
	if err != nil {
		return rules.HostRuleSet{}, rules.HostRuleSet{}, fmt.Errorf("parse auto-detect rules: %w", err)
	}

	return customSet, autoDetectSet, nil
}
