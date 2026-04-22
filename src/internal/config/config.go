package config

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
)

// Config 表示 AutoProxy3 的完整运行配置。
//
// 该结构聚合代理监听地址、上游代理、规则源、自动探测、
// 管理接口与日志等全部顶层配置项，并通过 JSON 标签与配置文件字段对应。
// 调用方应使用 Load 完成解析、默认值填充与相对路径归一化，而不是直接手动构造。
type Config struct {
	// ListenAddr 指定代理服务监听地址。
	ListenAddr string `json:"listen_addr"`
	// UpstreamProxy 指定可选的上游代理地址。
	UpstreamProxy string `json:"upstream_proxy"`
	// WebRules 定义在线规则列表相关配置。
	WebRules WebRulesConfig `json:"web_rules"`
	// CustomRules 定义本地自定义规则文件配置。
	CustomRules FileRulesConfig `json:"custom_rules"`
	// AutoDetect 定义自动探测与结果持久化配置。
	AutoDetect AutoDetectConfig `json:"auto_detect"`
	// Management 定义本地管理接口配置。
	Management ManagementConfig `json:"management"`
	// Logging 定义日志输出与轮转配置。
	Logging LoggingConfig `json:"logging"`
}

// WebRulesConfig 表示在线规则列表配置。
//
// 该配置用于控制在线规则下载、缓存文件路径与刷新行为。
// 启用状态与启动时下载开关支持默认值，并在加载阶段自动补全。
type WebRulesConfig struct {
	// Enabled 表示是否启用在线规则功能。
	Enabled bool `json:"enabled"`
	// URL 表示在线规则下载地址。
	URL string `json:"url"`
	// CachePath 表示在线规则缓存文件路径。
	CachePath string `json:"cache_path"`
	// RefreshInterval 表示自动刷新间隔，单位为秒。
	RefreshInterval int `json:"refresh_interval"`
	// DownloadOnStart 表示启动时是否立即下载规则。
	DownloadOnStart bool `json:"download_on_start"`
}

// FileRulesConfig 表示基于文件的规则配置。
//
// 该配置用于描述本地规则文件的启用状态与文件位置，
// 可被自定义规则等场景复用。
type FileRulesConfig struct {
	// Enabled 表示是否启用该规则文件。
	Enabled bool `json:"enabled"`
	// Path 表示规则文件路径。
	Path string `json:"path"`
}

// AutoDetectConfig 表示自动探测功能配置。
//
// 该配置控制自动探测的启用状态、最大尝试次数与规则文件落盘位置。
// 当未显式设置时，最大尝试次数会采用默认值。
type AutoDetectConfig struct {
	// Enabled 表示是否启用自动探测。
	Enabled bool `json:"enabled"`
	// MaxAttempts 表示直接连接失败后的最大重试次数。
	MaxAttempts int `json:"max_attempts"`
	// RulesPath 表示自动探测结果规则文件路径。
	RulesPath string `json:"rules_path"`
}

// ManagementConfig 表示管理接口配置。
//
// 该配置控制本地管理 HTTP 接口是否启用以及监听端口。
// 当端口未显式设置时，加载阶段会应用默认端口。
type ManagementConfig struct {
	// Enabled 表示是否启用管理接口。
	Enabled bool `json:"enabled"`
	// ListenPort 表示管理接口监听端口。
	ListenPort int `json:"listen_port"`
}

// LoggingConfig 表示日志输出配置。
//
// 该配置控制日志级别、格式、文件输出路径以及轮转参数。
// FilePath 允许为空，表示仅输出到标准输出。
type LoggingConfig struct {
	// Level 表示日志级别。
	Level string `json:"level"`
	// Format 表示日志格式。
	Format string `json:"format"`
	// FilePath 表示日志文件路径；为空时不写入文件。
	FilePath string `json:"file_path"`
	// MaxSize 表示单个日志文件的最大大小，单位为 MB。
	MaxSize int `json:"max_size"`
	// MaxBackups 表示日志文件最大保留数量。
	MaxBackups int `json:"max_backups"`
}

type rawConfig struct {
	ListenAddr    string              `json:"listen_addr"`
	UpstreamProxy string              `json:"upstream_proxy"`
	WebRules      rawWebRulesConfig   `json:"web_rules"`
	CustomRules   rawFileRulesConfig  `json:"custom_rules"`
	AutoDetect    rawAutoDetectConfig `json:"auto_detect"`
	Management    rawManagementConfig `json:"management"`
	Logging       rawLoggingConfig    `json:"logging"`
}

type rawWebRulesConfig struct {
	Enabled         *bool  `json:"enabled"`
	URL             string `json:"url"`
	CachePath       string `json:"cache_path"`
	RefreshInterval int    `json:"refresh_interval"`
	DownloadOnStart *bool  `json:"download_on_start"`
}

type rawFileRulesConfig struct {
	Enabled *bool  `json:"enabled"`
	Path    string `json:"path"`
}

type rawAutoDetectConfig struct {
	Enabled     *bool  `json:"enabled"`
	MaxAttempts int    `json:"max_attempts"`
	RulesPath   string `json:"rules_path"`
}

type rawManagementConfig struct {
	Enabled    *bool `json:"enabled"`
	ListenPort int   `json:"listen_port"`
}

type rawLoggingConfig struct {
	Level      string `json:"level"`
	Format     string `json:"format"`
	FilePath   string `json:"file_path"`
	MaxSize    int    `json:"max_size"`
	MaxBackups int    `json:"max_backups"`
}

// Load 从给定读取器解析 JSON 配置并返回标准化后的结果。
//
// 参数 r 提供原始 JSON 内容；sourcePath 用于确定配置文件所在目录，
// 以便将支持的相对路径字段归一化为绝对路径。
// 当 JSON 解析失败时，函数会返回带有上下文信息的错误。
func Load(r io.Reader, sourcePath string) (Config, error) {
	var raw rawConfig
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	cfg := Config{
		ListenAddr:    raw.ListenAddr,
		UpstreamProxy: raw.UpstreamProxy,
		WebRules: WebRulesConfig{
			URL:             raw.WebRules.URL,
			CachePath:       raw.WebRules.CachePath,
			RefreshInterval: raw.WebRules.RefreshInterval,
		},
		CustomRules: FileRulesConfig{
			Path: raw.CustomRules.Path,
		},
		AutoDetect: AutoDetectConfig{
			MaxAttempts: raw.AutoDetect.MaxAttempts,
			RulesPath:   raw.AutoDetect.RulesPath,
		},
		Management: ManagementConfig{
			ListenPort: raw.Management.ListenPort,
		},
		Logging: LoggingConfig{
			Level:      raw.Logging.Level,
			Format:     raw.Logging.Format,
			FilePath:   raw.Logging.FilePath,
			MaxSize:    raw.Logging.MaxSize,
			MaxBackups: raw.Logging.MaxBackups,
		},
	}

	if raw.WebRules.Enabled != nil {
		cfg.WebRules.Enabled = *raw.WebRules.Enabled
	}
	if raw.WebRules.DownloadOnStart != nil {
		cfg.WebRules.DownloadOnStart = *raw.WebRules.DownloadOnStart
	}
	if raw.CustomRules.Enabled != nil {
		cfg.CustomRules.Enabled = *raw.CustomRules.Enabled
	}
	if raw.AutoDetect.Enabled != nil {
		cfg.AutoDetect.Enabled = *raw.AutoDetect.Enabled
	}
	if raw.Management.Enabled != nil {
		cfg.Management.Enabled = *raw.Management.Enabled
	}

	applyDefaults(&cfg, raw)
	canonicalSourcePath, err := canonicalizeSourcePath(sourcePath)
	if err != nil {
		return Config{}, fmt.Errorf("canonicalize source path: %w", err)
	}
	resolveRelativePaths(&cfg, filepath.Dir(canonicalSourcePath))
	return cfg, nil
}

func applyDefaults(cfg *Config, raw rawConfig) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "localhost:1080"
	}
	if raw.WebRules.Enabled == nil {
		cfg.WebRules.Enabled = true
	}
	if raw.WebRules.DownloadOnStart == nil {
		cfg.WebRules.DownloadOnStart = true
	}
	if raw.CustomRules.Enabled == nil {
		cfg.CustomRules.Enabled = true
	}
	if cfg.AutoDetect.MaxAttempts == 0 {
		cfg.AutoDetect.MaxAttempts = 3
	}
	if raw.Management.Enabled == nil {
		cfg.Management.Enabled = true
	}
	if cfg.Management.ListenPort == 0 {
		cfg.Management.ListenPort = 9091
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}
	if cfg.Logging.MaxSize == 0 {
		cfg.Logging.MaxSize = 10
	}
	if cfg.Logging.MaxBackups == 0 {
		cfg.Logging.MaxBackups = 5
	}
}

func resolveRelativePaths(cfg *Config, baseDir string) {
	cfg.WebRules.CachePath = resolvePath(cfg.WebRules.CachePath, baseDir)
	cfg.CustomRules.Path = resolvePath(cfg.CustomRules.Path, baseDir)
	cfg.AutoDetect.RulesPath = resolvePath(cfg.AutoDetect.RulesPath, baseDir)
	cfg.Logging.FilePath = resolvePath(cfg.Logging.FilePath, baseDir)
}

func resolvePath(pathValue, baseDir string) string {
	if pathValue == "" || filepath.IsAbs(pathValue) {
		return pathValue
	}
	if baseDir == "" {
		return filepath.Clean(pathValue)
	}
	return filepath.Clean(filepath.Join(baseDir, pathValue))
}

func canonicalizeSourcePath(sourcePath string) (string, error) {
	if sourcePath == "" {
		return "", nil
	}
	if filepath.IsAbs(sourcePath) {
		return filepath.Clean(sourcePath), nil
	}
	absolutePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolutePath), nil
}
