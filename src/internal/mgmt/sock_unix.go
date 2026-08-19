//go:build unix

package mgmt

import (
	"net"
	"os"
	"path/filepath"
)

// Listen 在 Unix 平台上监听 unix domain socket(run/nas.sock)。
// 监听前清理可能残留的旧 socket 文件。
func Listen(sockPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return nil, err
	}
	// 残留的 socket 文件会导致 bind 失败,先清理
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}
	// 保证目录权限正确
	_ = os.Chmod(filepath.Dir(sockPath), 0o755)
	return ln, nil
}

// Dial 连接 Unix 平台的管理 socket。
func Dial(sockPath string) (net.Conn, error) {
	return net.Dial("unix", sockPath)
}

// SocketFile 返回 Unix 平台下的 socket 路径。
func SocketFile(sockPath string) string { return sockPath }

// Cleanup 删除残留的管理通道文件(进程退出后调用)。
func Cleanup(sockPath string) error {
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
