//go:build !linux && !android && !windows

package monitor

// 其他平台(开发中不使用的目标)统一返回"不可用"。

func cpuPercent() (float64, bool)                         { return 0, false }
func memoryStats() (used, total uint64, ok bool)          { return 0, 0, false }
func diskStats(root string) (free, total uint64, ok bool) { return 0, 0, false }
func netStats() (rx, tx uint64, ok bool)                  { return 0, 0, false }
func uptime() float64                                     { return 0 }
