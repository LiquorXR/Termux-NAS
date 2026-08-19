// Command helper 是插件管理器单元测试用的模拟插件。
//
// 行为:
//   - 解析 --name / --port 参数(与注册协议一致)
//   - 监听 127.0.0.1:<port>,提供 /health 与 /hello 两个端点
//   - 向 stdout 输出注册 JSON(含实际监听端口)
//   - NAS_HELPER_CRASH=1 时:不启动服务,直接以状态码 1 退出(模拟崩溃)
//   - NAS_HELPER_NO_REG=1 时:启动服务但不输出注册 JSON(模拟注册超时)
//
// testdata 目录下的代码不参与 go build ./... 编译,
// 仅在测试中由测试代码显式 go build 生成插件二进制。
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	name := flag.String("name", "", "插件 ID")
	port := flag.Int("port", 0, "监听端口(0 = 随机)")
	flag.Parse()

	if os.Getenv("NAS_HELPER_CRASH") == "1" {
		os.Exit(1)
	}

	// 监听 127.0.0.1:port,端口由内核分配
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(2)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"plugin":%q,"path":%q}`, *name, r.URL.Path)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	// 注册协议:向 stdout 输出一行注册 JSON
	if os.Getenv("NAS_HELPER_NO_REG") != "1" {
		fmt.Printf(`{"id":%q,"name":"测试插件","version":"0.0.1","port":%d,"nav":"测试","icon":"box"}`+"\n",
			*name, actualPort)
	}

	// 阻塞运行,等待被 CommandContext 取消终止
	for {
		time.Sleep(time.Hour)
	}
}
