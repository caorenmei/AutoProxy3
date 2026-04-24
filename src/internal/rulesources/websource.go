package rulesources

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/caorenmei/autoproxy3/src/internal/rules"
)

const useUpstreamHeader = "X-AutoProxy3-Use-Upstream"

// WebSource 表示基于 HTTP 下载的在线规则源。
//
// 该类型负责下载在线规则、在下载失败时回退到本地缓存，并提供后台定时刷新的最小入口。
// ShouldUseProxy 用于复用现有判路语义，以决定下载请求本身是否应标记为“走上游”。
type WebSource struct {
	URL            string
	CachePath      string
	HTTPClient     *http.Client
	ShouldUseProxy func(string) bool
}

// Load 下载并解析在线规则，必要时回退到缓存。
//
// 函数会向 URL 发起 GET 请求，并在 ShouldUseProxy 判定为 true 时附带可测试的上游标记头。
// 下载成功后会解析响应体并写入 CachePath；若下载、读取或解析远端响应失败，则尝试加载缓存。
// 返回值中的 fromRemote 为 true 表示结果来自远端下载；为 false 表示结果来自缓存回退。
func (s WebSource) Load() (set rules.WebRuleSet, fromRemote bool, err error) {
	return s.load(context.Background())
}

func (s WebSource) load(ctx context.Context) (set rules.WebRuleSet, fromRemote bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return s.loadFromCache(fmt.Errorf("build web rules request: %w", err))
	}
	if s.ShouldUseProxy != nil && s.ShouldUseProxy(s.URL) {
		req.Header.Set(useUpstreamHeader, "true")
	}

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return s.loadFromCache(fmt.Errorf("download web rules: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return s.loadFromCache(fmt.Errorf("download web rules: unexpected status %s", resp.Status))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return s.loadFromCache(fmt.Errorf("read web rules response: %w", err))
	}

	set, err = rules.ParseWebRules(bytes.NewReader(body))
	if err != nil {
		return s.loadFromCache(fmt.Errorf("parse web rules response: %w", err))
	}

	if err := os.WriteFile(s.CachePath, body, 0o644); err != nil {
		return rules.WebRuleSet{}, false, fmt.Errorf("write web rules cache: %w", err)
	}

	return set, true, nil
}

// StartRefreshLoop 启动在线规则后台刷新循环。
//
// 当 interval 小于等于 0 时函数会直接返回且不启动 goroutine。
// 否则函数会按给定间隔调用 Load；仅在加载成功时才调用 apply 更新规则快照。
// 当 ctx 结束后，后台循环会停止并释放 ticker。
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
				if !s.refreshOnce(ctx, apply) {
					return
				}
			}
		}
	}()
}

func (s WebSource) refreshOnce(ctx context.Context, apply func(rules.WebRuleSet)) bool {
	if ctx.Err() != nil {
		return false
	}
	set, _, err := s.load(ctx)
	if ctx.Err() != nil {
		return false
	}
	if err == nil && ctx.Err() == nil {
		apply(set)
	}
	return true
}

func (s WebSource) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return http.DefaultClient
}

func (s WebSource) loadFromCache(remoteErr error) (rules.WebRuleSet, bool, error) {
	body, err := os.ReadFile(s.CachePath)
	if err != nil {
		return rules.WebRuleSet{}, false, remoteErr
	}
	set, err := rules.ParseWebRules(bytes.NewReader(body))
	return set, false, err
}
