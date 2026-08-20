package daemon

import "testing"

func BenchmarkBuildPluginProxyURL(b *testing.B) {
	for i := 0; i < b.N; i++ {
		u := buildPluginProxyURL(8080, "files/photo.jpg", "token=abc&q=1")
		_ = u
	}
}

func BenchmarkValidPluginID(b *testing.B) {
	ids := []string{"download", "alist", "my_plugin-v2", "bad/../path"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, id := range ids {
			_ = validPluginID(id)
		}
	}
}
