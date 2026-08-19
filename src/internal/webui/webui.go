// Package webui 嵌入前端静态资源,保证 nasd 单二进制自带全部 Web 界面。
package webui

import "embed"

// Static 前端静态资源目录(经 embed 编译进二进制)。
//
//go:embed static
var Static embed.FS
