// Package webui 嵌入前端构建产物,保证 nasd 单二进制自带全部 Web 界面。
// dist/ 由 Vite 构建生成(src/web → npm run build),不得手工编辑。
package webui

import "embed"

// Static 前端构建产物目录(经 embed 编译进二进制)。
//
//go:embed dist
var Static embed.FS
