//go:build windows

package mgmt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// addrFile 在 Windows 上替代 unix socket:nasd 监听 127.0.0.1 随机端口,
// 并把实际地址写入 run/nas.addr 供 nasm 读取。仅用于开发调试,
// 生产环境(Termux)走 unix socket。
const addrFile = "nas.addr"

// Listen 在 Windows 上监听本机回环 TCP,并将地址写入 run/nas.addr。
func Listen(sockPath string) (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addrPath := filepath.Join(filepath.Dir(sockPath), addrFile)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		_ = ln.Close()
		return nil, err
	}
	if err := os.WriteFile(addrPath, []byte(ln.Addr().String()+"\n"), 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("写入管理地址文件: %w", err)
	}
	return ln, nil
}

// Dial 读取 run/nas.addr 后连接。
func Dial(sockPath string) (net.Conn, error) {
	addrPath := filepath.Join(filepath.Dir(sockPath), addrFile)
	f, err := os.Open(addrPath)
	if err != nil {
		return nil, fmt.Errorf("nasd 未在运行(无管理地址文件 %s)", addrPath)
	}
	defer f.Close()
	addr, err := bufio.NewReader(f).ReadString('\n')
	// 无换行符时 ReadString 返回 io.EOF,但地址内容仍有效
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("读取管理地址: %w", err)
	}
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return nil, fmt.Errorf("解析管理地址: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		return nil, fmt.Errorf("非法管理端口: %s", portStr)
	}
	return net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
}

// SocketFile 返回 Windows 平台的管理地址文件路径(调试用)。
func SocketFile(sockPath string) string {
	return filepath.Join(filepath.Dir(sockPath), addrFile)
}

// Cleanup 删除残留的管理地址文件(进程退出后调用)。
func Cleanup(sockPath string) error {
	addrPath := filepath.Join(filepath.Dir(sockPath), addrFile)
	if err := os.Remove(addrPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
