//go:build windows

package monitor

import (
	"syscall"
	"unsafe"
)

// Windows 采集(开发调试环境)。x/sys/windows 未覆盖的 API 经 kernel32 直调。

type filetime struct {
	low, high uint32
}

func (f *filetime) val() uint64 { return uint64(f.high)<<32 | uint64(f.low) }

// memoryStatusEx 对应 MEMORYSTATUSEX(32/64 位均为 64 字节)。
type memoryStatusEx struct {
	length       uint32
	memoryLoad   uint32
	totalPhys    uint64
	availPhys    uint64
	totalPage    uint64
	availPage    uint64
	totalVirtual uint64
	availVirtual uint64
	availExt     uint64
}

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procSysTimes    = kernel32.NewProc("GetSystemTimes")
	procMemStatus   = kernel32.NewProc("GlobalMemoryStatusEx")
	procTickCount64 = kernel32.NewProc("GetTickCount64")
	procDiskFree    = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// cpuPercent 经 GetSystemTimes 采样计算 CPU 使用率。
func cpuPercent() (float64, bool) {
	var idle, kernel, user filetime
	r, _, _ := procSysTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0, false
	}
	return cpuDelta(idle.val(), kernel.val()+user.val())
}

// memoryStats 经 GlobalMemoryStatusEx 读取内存。
func memoryStats() (used, total uint64, ok bool) {
	var st memoryStatusEx
	st.length = uint32(unsafe.Sizeof(st))
	r, _, _ := procMemStatus.Call(uintptr(unsafe.Pointer(&st)))
	if r == 0 {
		return 0, 0, false
	}
	total = st.totalPhys
	used = total - st.availPhys
	return used, total, true
}

// diskStats 经 GetDiskFreeSpaceExW 统计磁盘。
func diskStats(root string) (free, total uint64, ok bool) {
	path, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, 0, false
	}
	var avail, tot, freeBytes uint64
	r, _, _ := procDiskFree.Call(
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&tot)),
		uintptr(unsafe.Pointer(&freeBytes)))
	if r == 0 {
		return 0, 0, false
	}
	return avail, tot, true
}

// netStats Windows 开发环境不支持累计流量采集。
func netStats() (rx, tx uint64, ok bool) { return 0, 0, false }

// uptime 经 GetTickCount64 读取系统运行毫秒数。
func uptime() float64 {
	r, _, _ := procTickCount64.Call()
	return float64(r) / 1000.0
}
