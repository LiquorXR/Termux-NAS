package daemon

import "testing"

func TestValidPluginID(t *testing.T) {
	ok := []string{"download", "alist-2", "my_plugin", "a.b", "A1"}
	bad := []string{"", "a/b", "..", "../x", "a b", "a?b", "x#y", "a:b", "01234567890123456789012345678901234567890123456789012345678901234567890"}
	for _, id := range ok {
		if !validPluginID(id) {
			t.Errorf("validPluginID(%q): 应为合法", id)
		}
	}
	for _, id := range bad {
		if validPluginID(id) {
			t.Errorf("validPluginID(%q): 应判非法", id)
		}
	}
}

func TestBuildPluginProxyURL(t *testing.T) {
	cases := []struct {
		port  int
		sub   string
		query string
		want  string
	}{
		{18002, "/tasks", "", "http://127.0.0.1:18002/tasks"},
		{18002, "tasks", "", "http://127.0.0.1:18002/tasks"},
		{18002, "", "", "http://127.0.0.1:18002/"},
		{18002, "/a/b", "page=1", "http://127.0.0.1:18002/a/b?page=1"},
	}
	for _, c := range cases {
		got := buildPluginProxyURL(c.port, c.sub, c.query)
		if got != c.want {
			t.Errorf("buildPluginProxyURL(%d,%q,%q) = %q,期望 %q", c.port, c.sub, c.query, got, c.want)
		}
	}
	// 解析验证
	host, path, query, err := parsePluginProxyURL("http://127.0.0.1:18002/a/b?page=1")
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1:18002" || path != "/a/b" || query != "page=1" {
		t.Errorf("解析结果不符: host=%q path=%q query=%q", host, path, query)
	}
}
