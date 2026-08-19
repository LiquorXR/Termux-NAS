// Command helper 是插件管理器单元测试用的模拟插件。
//
// 行为由环境变量控制:
//   - NAS_HELPER_CRASH=1:启动后立即以状态码 1 退出(模拟崩溃)
//   - 默认:输出注册 JSON 后阻塞运行(模拟常驻插件,等待被终止)
//
// testdata 目录下的代码不参与 go build ./... 编译,
// 仅在测试中由测试代码显式 go build 生成插件二进制。
package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	// 模拟注册协议输出(与开发文档 §5.3 格式一致)
	fmt.Println(`{"id":"helper","name":"测试插件","version":"0.0.1","port":18099,"nav":"测试","icon":"box"}`)

	if os.Getenv("NAS_HELPER_CRASH") == "1" {
		os.Exit(1)
	}

	// 阻塞运行,等待被 CommandContext 取消终止
	for {
		time.Sleep(time.Hour)
	}
}
