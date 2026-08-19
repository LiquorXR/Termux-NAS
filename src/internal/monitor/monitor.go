// Package monitor 实现系统监控(M3):CPU / 内存 / 磁盘 / 网络 / 电量汇总。
//
// 采集逻辑按平台分离(linux|android 读 /proc,Windows 读系统 API,其余平台不可用),
// Termux 电量与温度经 termux-battery-status 获取。开发环境(Windows)可运行验证。
package monitor

import (
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Battery 电量信息(Termux)。
type Battery struct {
	Percentage  int     `json:"percentage"`
	Status      string  `json:"status"`
	Temperature float64 `json:"temperature"`
}

// NetStats 网络累计流量。
type NetStats struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

// Summary 系统状态汇总。
type Summary struct {
	CPUPercent  float64   `json:"cpu_percent"`
	MemUsed     uint64    `json:"mem_used_bytes"`
	MemTotal    uint64    `json:"mem_total_bytes"`
	MemPercent  float64   `json:"mem_percent"`
	DiskFree    uint64    `json:"disk_free_bytes"`
	DiskTotal   uint64    `json:"disk_total_bytes"`
	DiskPercent float64   `json:"disk_percent"`
	Platform    string    `json:"platform"`
	Hostname    string    `json:"hostname"`
	Uptime      float64   `json:"uptime_seconds"`
	Battery     *Battery  `json:"battery,omitempty"`
	Net         *NetStats `json:"net,omitempty"`
	Timestamp   string    `json:"timestamp"`
}

// Collect 采集系统汇总(root 为磁盘统计目标目录)。
func Collect(root string) Summary {
	s := Summary{
		Platform:  runtime.GOOS,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	s.Hostname, _ = os.Hostname()
	s.Uptime = uptime()

	if v, ok := cpuPercent(); ok {
		s.CPUPercent = v
	}
	if used, total, ok := memoryStats(); ok {
		s.MemUsed, s.MemTotal = used, total
		if total > 0 {
			s.MemPercent = float64(used) / float64(total) * 100
		}
	}
	if free, total, ok := diskStats(root); ok {
		s.DiskFree, s.DiskTotal = free, total
		if total > 0 {
			s.DiskPercent = float64(total-free) / float64(total) * 100
		}
	}
	if b, ok := batteryStats(); ok {
		s.Battery = &b
	}
	if rx, tx, ok := netStats(); ok {
		s.Net = &NetStats{RxBytes: rx, TxBytes: tx}
	}
	return s
}

// batteryStats 电量与温度(仅 Termux,经 termux-battery-status)。
func batteryStats() (Battery, bool) {
	if os.Getenv("PREFIX") == "" {
		return Battery{}, false // 非 Termux 环境
	}
	out, err := exec.Command("termux-battery-status").Output()
	if err != nil {
		return Battery{}, false
	}
	var b struct {
		Percentage  int     `json:"percentage"`
		Status      string  `json:"status"`
		Temperature float64 `json:"temperature"`
	}
	if json.Unmarshal(out, &b) != nil {
		return Battery{}, false
	}
	return Battery{Percentage: b.Percentage, Status: b.Status, Temperature: b.Temperature}, true
}

// cpuDelta 基于前后两次采样计算 CPU 使用率(百分比)。首次采样返回 false。
var (
	cpuMu     sync.Mutex
	prevIdle  uint64
	prevTotal uint64
	havePrev  bool
)

func cpuDelta(idle, total uint64) (float64, bool) {
	cpuMu.Lock()
	defer cpuMu.Unlock()
	if !havePrev || total < prevTotal || idle < prevIdle {
		prevIdle, prevTotal, havePrev = idle, total, true
		return 0, false
	}
	dTotal := total - prevTotal
	dIdle := idle - prevIdle
	prevIdle, prevTotal = idle, total
	if dTotal == 0 {
		return 0, false
	}
	return (1 - float64(dIdle)/float64(dTotal)) * 100, true
}
