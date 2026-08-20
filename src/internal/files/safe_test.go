package files

import (
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	root := filepath.FromSlash("/nas/files")

	tests := []struct {
		rel     string
		want    string
		wantErr bool
	}{
		{"", "/nas/files", false},
		{"/", "/nas/files", false},
		{".", "/nas/files", false},
		{"a", "/nas/files/a", false},
		{"a/b", "/nas/files/a/b", false},
		{"./a/../b", "/nas/files/b", false},
		{"a/b/..", "/nas/files/a", false},
		{"a b/文件.txt", "/nas/files/a b/文件.txt", false},
		// 拒绝绝对路径
		{"/etc/passwd", "", true},
		{`C:\Windows`, "", true},
		{`C:`, "", true},
		{`\\server\share`, "", true},
		// 拒绝逃逸
		{"..", "", true},
		{"../etc", "", true},
		{"a/../../etc", "", true},
		{"../../", "", true},
		{"a/../../../..", "", true},
		// 前缀欺骗:root=/nas/files,兄弟目录 /nas/files2 必须被拒
		{"../files2/evil", "", true},
		{"../files2", "", true},
	}
	for _, tt := range tests {
		got, err := Normalize(root, tt.rel)
		if tt.wantErr {
			if err == nil {
				t.Errorf("Normalize(%q): 期望错误,得到 %q", tt.rel, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Normalize(%q): 意外错误 %v", tt.rel, err)
			continue
		}
		want := filepath.FromSlash(tt.want)
		if got != want {
			t.Errorf("Normalize(%q) = %q, 期望 %q", tt.rel, got, want)
		}
	}
}

func TestSafeName(t *testing.T) {
	ok := []string{"a.txt", "我的文件", "a-b_c.d", "1", "..a"}
	bad := []string{"", ".", "..", "a/b", `a\b`, "/etc", "a b/../c", `..\..`}
	for _, n := range ok {
		if !SafeName(n) {
			t.Errorf("SafeName(%q): 应为合法", n)
		}
	}
	for _, n := range bad {
		if SafeName(n) {
			t.Errorf("SafeName(%q): 应判非法", n)
		}
	}
}

// TestNormalizeRootPrefix 前缀欺骗回归:root 的子串前缀目录不得被当作 root。
func TestNormalizeRootPrefix(t *testing.T) {
	root := filepath.FromSlash("/nas/files")
	for _, rel := range []string{"../files2", "../files2/x", "../../files"} {
		if got, err := Normalize(root, rel); err == nil {
			t.Errorf("Normalize(root=%q, %q): 期望越界错误,得到 %q", root, rel, got)
		}
	}
}

// TestRel 相对路径换算。
func TestRel(t *testing.T) {
	root := filepath.FromSlash("/nas/files")
	full := filepath.Join(root, "a", "b.txt")
	if got := Rel(root, full); got != "a/b.txt" {
		t.Errorf("Rel = %q, 期望 %q", got, "a/b.txt")
	}
	if got := Rel(root, root); got != "" {
		t.Errorf("Rel(root) = %q, 期望空串", got)
	}
}

// TestIsInlineUnsafe 存储型 XSS 回归:可执行内容类型禁止内联。
func TestIsInlineUnsafe(t *testing.T) {
	unsafe := []string{"text/html", "text/html; charset=utf-8", "image/svg+xml",
		"text/xml", "application/xml", "application/javascript", "text/javascript", "application/json"}
	safe := []string{"text/plain", "application/octet-stream", "image/png",
		"application/pdf", "text/markdown", "application/zip", "video/mp4"}
	for _, ct := range unsafe {
		if !isInlineUnsafe(ct) {
			t.Errorf("isInlineUnsafe(%q): 应判为不可内联", ct)
		}
	}
	for _, ct := range safe {
		if isInlineUnsafe(ct) {
			t.Errorf("isInlineUnsafe(%q): 不应判为不可内联", ct)
		}
	}
}
