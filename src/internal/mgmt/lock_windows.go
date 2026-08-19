//go:build windows

package mgmt

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// AcquireLock Windows 开发环境使用内核互斥量实现单实例。
// 生产环境(Termux)走 flock 实现(lock_unix.go)。
func AcquireLock(_ string) (release func() error, err error) {
	name, err := windows.UTF16PtrFromString(`Local\TermuxNAS.nasd`)
	if err != nil {
		return nil, fmt.Errorf("编码互斥量名称: %w", err)
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if err == windows.ERROR_ALREADY_EXISTS {
			_ = windows.CloseHandle(h)
			return nil, errors.New("nasd 已在运行(单实例互斥量被占用)")
		}
		return nil, fmt.Errorf("创建单实例互斥量: %w", err)
	}
	return func() error {
		return windows.CloseHandle(h)
	}, nil
}
