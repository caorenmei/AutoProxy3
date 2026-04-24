package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/caorenmei/autoproxy3/src/internal/management"
)

// Client 表示管理接口命令客户端。
//
// 该客户端用于向本地管理 HTTP 接口发送规则重载请求，
// 并将失败响应转换为调用方可消费的错误。
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient 创建管理接口客户端。
//
// 参数 baseURL 表示管理接口根地址；httpClient 允许调用方注入自定义 HTTP 客户端。
// 当 httpClient 为 nil 时，函数会回退到 http.DefaultClient。
func NewClient(baseURL string, httpClient *http.Client) *Client {
	return newClient(baseURL, httpClient)
}

func newClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// ReloadWebRules 调用管理接口重载在线规则。
//
// 参数 ctx 用于控制请求生命周期。
// 当管理接口返回失败状态时，函数会返回包含服务端错误详情的错误。
func (c *Client) ReloadWebRules(ctx context.Context) error {
	return c.doReload(ctx, "/reload_web_rules")
}

// ReloadCustomRules 调用管理接口重载本地规则。
//
// 参数 ctx 用于控制请求生命周期。
// 当管理接口返回失败状态时，函数会返回包含服务端错误详情的错误。
func (c *Client) ReloadCustomRules(ctx context.Context) error {
	return c.doReload(ctx, "/reload_custom_rules")
}

// ReloadRules 调用管理接口重载全部规则。
//
// 参数 ctx 用于控制请求生命周期。
// 当管理接口返回失败状态时，函数会返回包含服务端错误详情的错误。
func (c *Client) ReloadRules(ctx context.Context) error {
	return c.doReload(ctx, "/reload_rules")
}

func (c *Client) doReload(ctx context.Context, path string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	var payload management.ActionResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && payload.Success {
		return nil
	}
	if payload.Error != "" {
		return errorsf(payload.Error)
	}
	for _, step := range payload.Steps {
		if step.Error != "" {
			return errorsf(step.Error)
		}
	}
	return fmt.Errorf("request failed with status %d", response.StatusCode)
}

func errorsf(message string) error {
	return fmt.Errorf("%s", message)
}
