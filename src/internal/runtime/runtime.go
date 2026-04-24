package runtime

import (
	"context"

	"github.com/caorenmei/autoproxy3/src/internal/config"
	"github.com/caorenmei/autoproxy3/src/internal/rules"
	"github.com/caorenmei/autoproxy3/src/internal/rulesources"
)

// Runner 定义 AutoProxy3 运行时的最小执行入口。
//
// 调用方应在完成配置加载后通过 New 构造 Runner，
// 再调用 Run 启动运行时主流程。Run 接收调用方提供的上下文，
// 并在启动失败或运行过程中出现错误时返回对应错误。
type Runner interface {
	Run(context.Context) error
}

type customRuleLoader interface {
	LoadCustomAndAutoDetect(customPath, autoDetectPath string) (rules.HostRuleSet, rules.HostRuleSet, error)
}

type autoDetectStore interface {
	AppendHost(host string) error
}

type runner struct {
	config           config.Config
	customRuleLoader customRuleLoader
	autoDetectStore  autoDetectStore
	webSource        *rulesources.WebSource
}

// New 基于给定配置创建最小可运行的运行时实例。
//
// 参数 cfg 为已经完成解析与标准化的运行配置；
// 返回值实现 Runner 接口，可被命令行入口直接调用。
// 当前骨架阶段仅保留配置与规则源依赖，以承接后续 serve 流程，因此通常不会返回错误。
func New(cfg config.Config) (Runner, error) {
	var webSource *rulesources.WebSource
	if cfg.WebRules.Enabled {
		webSource = &rulesources.WebSource{
			URL:       cfg.WebRules.URL,
			CachePath: cfg.WebRules.CachePath,
		}
	}

	return runner{
		config:           cfg,
		customRuleLoader: rulesources.FileSource{},
		autoDetectStore:  rulesources.AutoDetectStore{Path: cfg.AutoDetect.RulesPath},
		webSource:        webSource,
	}, nil
}

func (r runner) Run(ctx context.Context) error {
	_ = r.config
	_ = r.customRuleLoader
	_ = r.autoDetectStore
	_ = r.webSource
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
