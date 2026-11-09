package cloudflare

// Cloudflare API v4 客户端（Bearer Token 认证）
// 仅依赖标准库 net/http，不引入第三方 SDK。
//
// 认证方式：API Token（Bearer）。
//   在 Cloudflare 面板「My Profile → API Tokens」创建，读取 Zone 列表需要
//   Zone → Zone → Read 权限（按账户需要可加 Account → Account → Read）；
//   DNS 记录读写需要 Zone → DNS → Edit 权限。
//
// 客户端方法导出（Get/Post/Put/Delete），供同包及 cloudflare/dns 子包复用。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const apiBase = "https://api.cloudflare.com/client/v4"

// Client Cloudflare API 客户端
type Client struct {
	token     string
	accountID string // Cloudflare Account ID（用于按账户过滤 zones）
	http      *http.Client
}

// NewClient 创建客户端。accountID 可为空（不过滤，返回 Token 有权限的所有 zones）
func NewClient(token, accountID string) *Client {
	return &Client{
		token:     token,
		accountID: accountID,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// ResultInfo 分页信息（list 接口的 result_info）
type ResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

// cfResponse Cloudflare API 统一响应信封
type cfResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Messages []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"messages"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *ResultInfo     `json:"result_info"`
}

// Get 发起 GET 请求并解析响应信封，result 反序列化到 out（可为 nil 表示丢弃）
func (c *Client) Get(path string, query url.Values, out interface{}) (*ResultInfo, error) {
	u := apiBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, out)
}

// Post 发起 POST 请求（JSON body），result 反序列化到 out（可为 nil）
func (c *Client) Post(path string, body interface{}, out interface{}) (*ResultInfo, error) {
	return c.sendJSON(http.MethodPost, path, body, out)
}

// Put 发起 PUT 请求（JSON body，Cloudflare 的更新操作为全量替换）
func (c *Client) Put(path string, body interface{}, out interface{}) (*ResultInfo, error) {
	return c.sendJSON(http.MethodPut, path, body, out)
}

// Delete 发起 DELETE 请求（无 body）
func (c *Client) Delete(path string, out interface{}) (*ResultInfo, error) {
	req, err := http.NewRequest(http.MethodDelete, apiBase+path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, out)
}

// sendJSON 序列化 body 并以 JSON 发送请求
func (c *Client) sendJSON(method, path string, body interface{}, out interface{}) (*ResultInfo, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, apiBase+path, reader)
	if err != nil {
		return nil, err
	}
	return c.do(req, out)
}

// do 执行请求并解析统一响应信封（Get/Post/Put/Delete 共用）
func (c *Client) do(req *http.Request, out interface{}) (*ResultInfo, error) {
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求Cloudflare API失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16MB 上限保护
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("Cloudflare API 认证失败(HTTP %d)，请检查 API Token 及权限", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Cloudflare API 返回 HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var envelope cfResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if !envelope.Success {
		if len(envelope.Errors) > 0 {
			return nil, fmt.Errorf("Cloudflare API 错误[%d]: %s", envelope.Errors[0].Code, envelope.Errors[0].Message)
		}
		return nil, fmt.Errorf("Cloudflare API 返回失败: %s", truncate(string(body), 200))
	}
	if out != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, out); err != nil {
			return nil, fmt.Errorf("解析result失败: %w", err)
		}
	}
	return envelope.ResultInfo, nil
}

// setHeaders 设置认证与内容类型头
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
}

// truncate 截断字符串，避免把超长错误信息刷入日志
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
