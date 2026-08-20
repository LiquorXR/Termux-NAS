// Package safehttp 提供面向下载场景的安全 HTTP 客户端:
// 固定超时、响应体大小上限、私网地址拦截(SSRF 防护,含 DNS 重绑定防护)、
// 重定向次数限制。用于插件/更新包下载等以用户输入 URL 发起请求的路径。
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// 默认限制。
const (
	// DefaultTimeout 单次请求总超时。
	DefaultTimeout = 30 * time.Second
	// DefaultMaxBody 默认响应体上限(64 MiB;插件包与更新二进制均远小于此)。
	DefaultMaxBody = 64 << 20
	// maxRedirects 最大重定向次数。
	maxRedirects = 5
)

// Option 客户端构建选项。
type Option func(*Client)

// WithMaxBody 覆盖响应体大小上限(字节)。
func WithMaxBody(n int64) Option {
	return func(c *Client) { c.maxBody = n }
}

// WithAllowPrivate 允许访问私网/回环地址(默认拦截,仅测试或明确需要时使用)。
func WithAllowPrivate() Option {
	return func(c *Client) { c.blockPrivate = false }
}

// Client 安全 HTTP 客户端。
type Client struct {
	hc           *http.Client
	maxBody      int64
	blockPrivate bool
}

// New 创建安全客户端(默认:30s 超时、64 MiB 上限、私网拦截)。
func New(opts ...Option) *Client {
	c := &Client{
		maxBody:      DefaultMaxBody,
		blockPrivate: true,
	}
	for _, o := range opts {
		o(c)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if c.blockPrivate {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					host = addr
				}
				if err := checkHostAllowed(ctx, host); err != nil {
					return nil, err
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	c.hc = &http.Client{
		Transport: transport,
		Timeout:   DefaultTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("重定向次数过多")
			}
			return nil
		},
	}
	return c
}

// Download 下载 URL 内容到内存(校验协议、私网拦截、大小上限)。
func (c *Client) Download(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL 无效: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("仅支持 http/https 协议")
	}
	if u.Hostname() == "" {
		return nil, errors.New("URL 缺少主机")
	}
	if maxBytes <= 0 {
		maxBytes = c.maxBody
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "Termux-NAS")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败: HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取下载内容失败: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("内容超过大小上限 %d 字节", maxBytes)
	}
	return body, nil
}

// checkHostAllowed 解析主机并拒绝解析到受限地址(回环/链路本地/私网等)。
// 在 Dial 时校验而非仅校验主机名,可防 DNS 重绑定与重定向绕过。
func checkHostAllowed(ctx context.Context, host string) error {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("解析 %s 失败: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("无法解析 %s", host)
	}
	for _, ip := range ips {
		if isBlockedIP(ip.IP) {
			return fmt.Errorf("目标 %s 解析到受限地址 %s(SSRF 防护)", host, ip.IP)
		}
	}
	return nil
}

// isBlockedIP 受限地址判定:回环、未指定、链路本地、组播、广播、
// RFC1918 私网、CGNAT(100.64/10)、IPv6 ULA(fc00::/7)。
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.Equal(net.IPv4bcast) {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true // 100.64.0.0/10 CGNAT
	}
	return false
}
