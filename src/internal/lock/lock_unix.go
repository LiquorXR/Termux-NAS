//go:build unix

// Package lock 提供 nasd 单实例锁(跨平台实现)。
//
// Termux/Linux走 flock(syscall),Windows开发环境走内核互斥量。
// 由 nasd.Run 在启动时获取;第二个 nasd 实例在此直接失败退出,
// 杜绝双实例同时监听同一端口的竞态。
package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// AcquireLock 获取单实例锁(run/nas.lock,flock 非阻塞)。
// 进程退出时内核自动释放;锁文件刻意保留(删除会引入竞态)。
func AcquireLock(lockPath string) (release func() error, err error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开锁文件 %s: %w", lockPath, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.New("nasd 已在运行(单实例锁被占用)")
		}
		return nil, fmt.Errorf("获取单实例锁: %w", err)
	}
	// 写入 pid 便于诊断双实例问题
	_, _ = f.Seek(0, 0)
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return func() error {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return f.Close()
	}, nil
}
