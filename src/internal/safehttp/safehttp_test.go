package safehttp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.8.8.8", true},
		{"::1", true},
		{"0.0.0.0", true},
		{"169.254.1.1", true},
		{"255.255.255.255", true},
		{"10.0.0.1", true},
		{"172.16.5.5", true},
		{"192.168.1.1", true},
		{"100.64.1.1", true},
		{"100.127.255.255", true},
		{"fc00::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"114.114.114.114", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("非法测试 IP: %s", c.ip)
		}
		if got := isBlockedIP(ip); got != c.want {
			t.Errorf("isBlockedIP(%s) = %v,期望 %v", c.ip, got, c.want)
		}
	}
}

func TestDownloadSchemeRejected(t *testing.T) {
	c := New()
	if _, err := c.Download(context.Background(), "file:///etc/passwd", 0); err == nil {
		t.Fatal("file:// 应被拒绝")
	}
	if _, err := c.Download(context.Background(), "ftp://example.com/x", 0); err == nil {
		t.Fatal("ftp:// 应被拒绝")
	}
}

func TestDownloadLoopbackBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret"))
	}))
	defer srv.Close()

	c := New()
	_, err := c.Download(context.Background(), srv.URL, 0)
	if err == nil {
		t.Fatal("回环地址应被 SSRF 防护拦截")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("错误信息应说明受限地址,得到: %v", err)
	}
}

func TestDownloadHostnameBlocked(t *testing.T) {
	c := New()
	_, err := c.Download(context.Background(), "http://localhost:7531/x", 0)
	if err == nil {
		t.Fatal("localhost 应被拦截")
	}
}

func TestDownloadSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	c := New(WithAllowPrivate(), WithMaxBody(5))
	_, err := c.Download(context.Background(), srv.URL, 0)
	if err == nil {
		t.Fatal("超过大小上限应报错")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("错误信息应含上限说明,得到: %v", err)
	}

	body, err := c.Download(context.Background(), srv.URL, 0)
	if err == nil && len(body) != 5 {
		t.Fatalf("上限内应返回完整内容,得到 %d 字节", len(body))
	}
}

func TestDownloadSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != "Termux-NAS" {
			t.Errorf("UA 应为 Termux-NAS,得到 %s", r.UserAgent())
		}
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	c := New(WithAllowPrivate())
	body, err := c.Download(context.Background(), srv.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Errorf("内容不符: %s", body)
	}
}

func TestDownloadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(WithAllowPrivate())
	if _, err := c.Download(context.Background(), srv.URL, 0); err == nil {
		t.Fatal("404 应报错")
	}
}

func TestRedirectLimit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL, http.StatusFound)
	}))
	defer srv.Close()

	c := New(WithAllowPrivate())
	if _, err := c.Download(context.Background(), srv.URL, 0); err == nil {
		t.Fatal("无限重定向应被拒绝")
	}
}
