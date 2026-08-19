// Package market 实现插件市场(M6):内嵌官方市场索引,支持浏览与一键安装。
//
// 市场索引为静态 JSON(go:embed),可被远程索引覆盖(预留 refresh);
// 安装复用 nasd 插件安装器(下载 .tar.gz → 校验 → 落盘 → 扫描登记)。
package market

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed static/market.json
var staticFS embed.FS

// Plugin 市场插件条目。
type Plugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Icon        string `json:"icon"`
	DownloadURL string `json:"download_url"`
	SizeHint    string `json:"size_hint,omitempty"`
}

// Index 市场索引。
type Index struct {
	Name      string   `json:"name"`
	Version   int      `json:"version"`
	UpdatedAt string   `json:"updated_at"`
	Plugins   []Plugin `json:"plugins"`
}

// Load 加载内嵌市场索引。
func Load() (*Index, error) {
	data, err := staticFS.ReadFile("static/market.json")
	if err != nil {
		return nil, fmt.Errorf("读取内嵌市场索引: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("解析市场索引: %w", err)
	}
	sort.Slice(idx.Plugins, func(i, j int) bool { return idx.Plugins[i].ID < idx.Plugins[j].ID })
	return &idx, nil
}

// Find 按 ID 查找插件。
func (idx *Index) Find(id string) (Plugin, bool) {
	for _, p := range idx.Plugins {
		if p.ID == id {
			return p, true
		}
	}
	return Plugin{}, false
}
