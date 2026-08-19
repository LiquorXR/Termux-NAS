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
		// 拒绝逃逸
		{"..", "", true},
		{"../etc", "", true},
		{"a/../../etc", "", true},
		{"../../", "", true},
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
	bad := []string{"", ".", "..", "a/b", `a\b`, "/etc", "a b/../c"}
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
