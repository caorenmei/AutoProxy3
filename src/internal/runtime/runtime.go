package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/caorenmei/autoproxy3/src/internal/buildinfo"
	"github.com/caorenmei/autoproxy3/src/internal/config"
	"github.com/caorenmei/autoproxy3/src/internal/management"
	"github.com/caorenmei/autoproxy3/src/internal/proxy"
	"github.com/caorenmei/autoproxy3/src/internal/rules"
	"github.com/caorenmei/autoproxy3/src/internal/rulesources"
)

var errWebRulesNotConfigured = errors.New("web rules source is not configured")

// Runner 定义 AutoProxy3 运行时的最小执行入口。
//
// 调用方应在完成配置加载后通过 New 构造 Runner，
// 再调用 Run 启动运行时主流程。Run 接收调用方提供的上下文，
// 并在启动失败或运行过程中出现错误时返回对应错误。
type Runner interface {
	Run(context.Context) error
}

// Runtime 表示 AutoProxy3 的最小运行时编排器。
//
// 该类型持有规则引擎、规则源、管理服务与代理服务，并负责把规则重载、
// 状态摘要与 auto-detect 持久化能力接到真实协作链路中。
type Runtime struct {
	config config.Config

	engine          *rules.Engine
	webSource       *rulesources.WebSource
	fileSource      rulesources.FileSource
	autoDetectStore rulesources.AutoDetectStore

	managementServer *management.Server
	proxyServer      *proxy.Server

	loadWebSource        func(*rulesources.WebSource) (rules.WebRuleSet, bool, error)
	loadFileSource       func(rulesources.FileSource, string, string) (rules.HostRuleSet, rules.HostRuleSet, error)
	appendAutoDetectHost func(rulesources.AutoDetectStore, string) error

	statusMu          sync.RWMutex
	webRulesLoaded    bool
	customRulesLoaded bool
	autoDetectLoaded  bool
}

// New 基于给定配置创建最小可运行的运行时实例。
//
// 参数 cfg 为已经完成解析与标准化的运行配置；
// 返回值实现 Runner 接口，并预先接好管理服务与代理服务的运行时依赖。
func New(cfg config.Config) (Runner, error) {
	rt := &Runtime{
		config:          cfg,
		engine:          rules.NewEngine(),
		fileSource:      rulesources.FileSource{},
		autoDetectStore: rulesources.AutoDetectStore{Path: cfg.AutoDetect.RulesPath},
	}
	if cfg.WebRules.Enabled {
		rt.webSource = &rulesources.WebSource{
			URL:       cfg.WebRules.URL,
			CachePath: cfg.WebRules.CachePath,
		}
	}

	rt.managementServer = management.NewServer(management.Options{
		ListenPort:        cfg.Management.ListenPort,
		Version:           buildinfo.Version,
		Features:          enabledFeatures(cfg),
		StatusProvider:    rt.StatusSummary,
		ReloadWebRules:    rt.ReloadWebRules,
		ReloadCustomRules: rt.ReloadCustomRules,
	})
	rt.proxyServer = proxy.NewServer(proxy.Options{
		Engine:                rt.engine,
		UpstreamProxy:         cfg.UpstreamProxy,
		AutoDetectEnabled:     cfg.AutoDetect.Enabled,
		AutoDetectMaxAttempts: cfg.AutoDetect.MaxAttempts,
		AutoDetectRecorder:    rt.autoDetectRecorder(),
	})

	return rt, nil
}

// Run 启动运行时主流程。
//
// 当前阶段仅验证上下文可用性，并保留已装配的运行时依赖，
// 为后续 serve 主流程实现提供稳定入口。
func (r *Runtime) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// ReloadWebRules 重新加载在线规则并刷新引擎快照。
//
// 函数会调用当前配置的在线规则源；仅当加载成功时才替换 engine 中的在线规则快照。
// 若在线规则未配置或加载失败，则返回错误并保持旧快照不变。
func (r *Runtime) ReloadWebRules(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.webSource == nil {
		return errWebRulesNotConfigured
	}

	set, _, err := r.loadWebRules()
	if err != nil {
		return err
	}

	r.engine.ReplaceWebRules(set)
	r.setRuleLoaded(&r.webRulesLoaded, r.config.WebRules.Enabled)
	return nil
}

// ReloadCustomRules 重新加载本地 custom 与 auto-detect 规则并刷新引擎快照。
//
// 函数会通过文件规则源一次性读取 custom 与 auto-detect 两类规则；
// 仅当两类规则都成功加载后才替换 engine 中的对应快照。
// 任一加载失败都会返回错误，并保持旧快照与旧状态不变。
func (r *Runtime) ReloadCustomRules(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	customSet, autoDetectSet, err := r.loadCustomAndAutoDetectRules(
		r.config.CustomRules.Path,
		r.config.AutoDetect.RulesPath,
	)
	if err != nil {
		return err
	}

	r.engine.ReplaceCustomRules(customSet)
	r.engine.ReplaceAutoDetectRules(autoDetectSet)
	r.setRuleLoaded(&r.customRulesLoaded, r.config.CustomRules.Enabled)
	r.setRuleLoaded(&r.autoDetectLoaded, r.config.AutoDetect.Enabled)
	return nil
}

// StatusSummary 返回当前规则源状态摘要。
//
// 返回值会反映 web、custom 与 auto-detect 三类规则源的启用状态，
// 以及它们是否已成功加载到当前运行时中。
func (r *Runtime) StatusSummary() management.RuleStatusSummary {
	r.statusMu.RLock()
	defer r.statusMu.RUnlock()

	return management.RuleStatusSummary{
		Web: management.RuleState{
			Enabled: r.config.WebRules.Enabled,
			Loaded:  r.webRulesLoaded,
		},
		Custom: management.RuleState{
			Enabled: r.config.CustomRules.Enabled,
			Loaded:  r.customRulesLoaded,
		},
		AutoDetect: management.RuleState{
			Enabled: r.config.AutoDetect.Enabled,
			Loaded:  r.autoDetectLoaded,
		},
	}
}

func (r *Runtime) autoDetectRecorder() proxy.AutoDetectRecorder {
	if strings.TrimSpace(r.autoDetectStore.Path) == "" {
		return nil
	}
	return runtimeAutoDetectRecorder{
		store:      r.autoDetectStore,
		appendHost: r.appendAutoDetectHost,
	}
}

func (r *Runtime) setRuleLoaded(target *bool, loaded bool) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	*target = loaded
}

func enabledFeatures(cfg config.Config) []string {
	features := []string{"proxy"}
	if cfg.WebRules.Enabled {
		features = append(features, "web-rules")
	}
	if cfg.CustomRules.Enabled {
		features = append(features, "custom-rules")
	}
	if cfg.AutoDetect.Enabled {
		features = append(features, "auto-detect")
	}
	if cfg.Management.Enabled {
		features = append(features, "management")
	}
	return features
}

func (r *Runtime) loadWebRules() (rules.WebRuleSet, bool, error) {
	if r.loadWebSource != nil {
		return r.loadWebSource(r.webSource)
	}
	return r.webSource.Load()
}

func (r *Runtime) loadCustomAndAutoDetectRules(customPath, autoDetectPath string) (rules.HostRuleSet, rules.HostRuleSet, error) {
	if r.loadFileSource != nil {
		return r.loadFileSource(r.fileSource, customPath, autoDetectPath)
	}
	return r.fileSource.LoadCustomAndAutoDetect(customPath, autoDetectPath)
}

type runtimeAutoDetectRecorder struct {
	store      rulesources.AutoDetectStore
	appendHost func(rulesources.AutoDetectStore, string) error
}

func (r runtimeAutoDetectRecorder) Record(_ context.Context, host string) error {
	if r.appendHost != nil {
		return r.appendHost(r.store, host)
	}
	return r.store.AppendHost(host)
}
