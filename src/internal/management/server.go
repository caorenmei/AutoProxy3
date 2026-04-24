package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// RuleState 表示单类规则源的当前状态。
//
// Enabled 表示是否启用该规则源；Loaded 表示规则内容是否已成功加载到内存。
type RuleState struct {
	Enabled bool `json:"enabled"`
	Loaded  bool `json:"loaded"`
}

// RuleStatusSummary 汇总各类规则源状态。
//
// Web 表示在线规则状态；Custom 表示本地自定义规则状态；
// AutoDetect 表示自动探测规则状态。
type RuleStatusSummary struct {
	Web        RuleState `json:"web"`
	Custom     RuleState `json:"custom"`
	AutoDetect RuleState `json:"auto_detect"`
}

// IndexResponse 表示管理首页响应体。
//
// 该响应包含版本号、启用特性以及当前规则状态概览。
type IndexResponse struct {
	Success  bool              `json:"success"`
	Version  string            `json:"version"`
	Features []string          `json:"features"`
	Rules    RuleStatusSummary `json:"rules"`
}

// ActionStep 表示单个重载步骤结果。
//
// Name 表示步骤名称；Success 表示该步骤是否成功；
// Error 在失败时给出具体错误信息。
type ActionStep struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ActionResponse 表示管理动作响应体。
//
// Success 表示整体是否成功；Partial 表示是否为部分成功；
// Steps 详细列出每个重载步骤的执行结果。
type ActionResponse struct {
	Success bool         `json:"success"`
	Partial bool         `json:"partial"`
	Error   string       `json:"error,omitempty"`
	Steps   []ActionStep `json:"steps,omitempty"`
}

// Options 表示管理服务初始化参数。
//
// Version 与 Features 用于首页展示；StatusProvider 用于提供规则状态快照；
// ReloadCustomRules 负责统一处理 custom 与 auto-detect 本地规则重载。
type Options struct {
	ListenPort        int
	Version           string
	Features          []string
	StatusProvider    func() RuleStatusSummary
	ReloadWebRules    func(context.Context) error
	ReloadCustomRules func(context.Context) error
}

// Server 表示管理 HTTP 服务。
//
// Server 实现 http.Handler，并负责首页查询与规则重载相关端点。
type Server struct {
	options Options
	mux     *http.ServeMux
}

// NewServer 创建管理 HTTP 服务。
//
// 返回值会注册首页、在线规则重载、本地规则重载与全部规则重载端点。
// 当可选回调未提供时，对应端点会返回显式错误而不是静默跳过。
// 其中本地规则重载端点会复用统一的 custom 回调以同步刷新 auto-detect 快照。
func NewServer(opts Options) *Server {
	server := &Server{
		options: opts,
		mux:     http.NewServeMux(),
	}
	server.mux.HandleFunc("/", server.handleIndex)
	server.mux.HandleFunc("/reload_web_rules", server.handleReloadWebRules)
	server.mux.HandleFunc("/reload_custom_rules", server.handleReloadCustomRules)
	server.mux.HandleFunc("/reload_rules", server.handleReloadRules)
	return server
}

// ServeHTTP 分发管理接口请求。
//
// 参数 w 用于写出 HTTP 响应；参数 r 描述当前请求。
// 当请求路径未注册时，函数会沿用 http.ServeMux 的默认 404 行为。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeActionResponse(w, http.StatusMethodNotAllowed, ActionResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	status := RuleStatusSummary{}
	if s.options.StatusProvider != nil {
		status = s.options.StatusProvider()
	}
	writeJSON(w, http.StatusOK, IndexResponse{
		Success:  true,
		Version:  s.options.Version,
		Features: append([]string(nil), s.options.Features...),
		Rules:    status,
	})
}

func (s *Server) handleReloadWebRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeActionResponse(w, http.StatusMethodNotAllowed, ActionResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}
	writeReloadResponse(w, runSteps(
		r.Context(),
		reloadStep{name: "reload_web_rules", fn: s.options.ReloadWebRules, missingError: "reload web rules handler is not configured"},
	))
}

func (s *Server) handleReloadCustomRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeActionResponse(w, http.StatusMethodNotAllowed, ActionResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}
	writeReloadResponse(w, runSteps(
		r.Context(),
		reloadStep{name: "reload_custom_rules", fn: s.options.ReloadCustomRules, missingError: "reload custom rules handler is not configured"},
	))
}

func (s *Server) handleReloadRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeActionResponse(w, http.StatusMethodNotAllowed, ActionResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}
	writeReloadResponse(w, runSteps(
		r.Context(),
		reloadStep{name: "reload_web_rules", fn: s.options.ReloadWebRules, missingError: "reload web rules handler is not configured"},
		reloadStep{name: "reload_custom_rules", fn: s.options.ReloadCustomRules, missingError: "reload custom rules handler is not configured"},
	))
}

type reloadStep struct {
	name         string
	fn           func(context.Context) error
	missingError string
}

func runSteps(ctx context.Context, steps ...reloadStep) ActionResponse {
	response := ActionResponse{
		Steps: make([]ActionStep, 0, len(steps)),
	}
	successCount := 0
	for _, step := range steps {
		actionStep := ActionStep{Name: step.name}
		if step.fn == nil {
			actionStep.Error = step.missingError
			response.Steps = append(response.Steps, actionStep)
			continue
		}
		if err := step.fn(ctx); err != nil {
			actionStep.Error = err.Error()
			response.Steps = append(response.Steps, actionStep)
			continue
		}
		actionStep.Success = true
		successCount++
		response.Steps = append(response.Steps, actionStep)
	}
	response.Success = successCount == len(steps)
	response.Partial = successCount > 0 && successCount < len(steps)
	if !response.Success {
		response.Error = firstStepError(response.Steps)
	}
	return response
}

func firstStepError(steps []ActionStep) string {
	for _, step := range steps {
		if step.Error != "" {
			return step.Error
		}
	}
	return "request failed"
}

func writeReloadResponse(w http.ResponseWriter, response ActionResponse) {
	status := http.StatusOK
	if !response.Success {
		status = http.StatusInternalServerError
	}
	writeActionResponse(w, status, response)
}

func writeActionResponse(w http.ResponseWriter, status int, response ActionResponse) {
	writeJSON(w, status, response)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

var errHandlerNotConfigured = errors.New("handler is not configured")
