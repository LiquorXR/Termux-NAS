package market

import "testing"

func TestLoadIndex(t *testing.T) {
	idx, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if idx.Name == "" {
		t.Error("市场名称不应为空")
	}
	if len(idx.Plugins) < 3 {
		t.Errorf("应有至少 3 个插件,得到 %d", len(idx.Plugins))
	}
	// 每个条目字段完整
	for _, p := range idx.Plugins {
		if p.ID == "" || p.Name == "" || p.DownloadURL == "" {
			t.Errorf("条目字段不完整: %+v", p)
		}
	}
	// 排序
	for i := 1; i < len(idx.Plugins); i++ {
		if idx.Plugins[i-1].ID > idx.Plugins[i].ID {
			t.Error("插件应按 ID 排序")
		}
	}
}

func TestFind(t *testing.T) {
	idx, _ := Load()
	p, ok := idx.Find("download")
	if !ok || p.Name != "下载中心" {
		t.Errorf("Find(download) = %+v, %v", p, ok)
	}
	if _, ok := idx.Find("ghost"); ok {
		t.Error("不存在插件不应命中")
	}
}
