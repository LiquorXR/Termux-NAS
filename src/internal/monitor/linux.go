//go:build linux || android

package monitor

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// cpuPercent 从 /proc/stat 计算 CPU 使用率。
func cpuPercent() (float64, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, false
	}
	var user, nice, system, idle, iowait, irq, softirq, steal uint64
	parse := func(i int) uint64 {
		if i < len(fields) {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			return v
		}
		return 0
	}
	user, nice, system = parse(1), parse(2), parse(3)
	idle, iowait = parse(4), parse(5)
	irq, softirq, steal = parse(6), parse(7), parse(8)
	idle += iowait
	total := user + nice + system + idle + irq + softirq + steal
	return cpuDelta(idle, total)
}

// memoryStats 从 /proc/meminfo 读取内存。
func memoryStats() (used, total uint64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	var memTotal, memAvail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			memTotal, _ = parseKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			memAvail, _ = parseKB(line)
		}
	}
	if memTotal == 0 {
		return 0, 0, false
	}
	return memTotal - memAvail, memTotal, true
}

// parseKB 解析 "Key:  N kB" 形式的行,返回 N(字节)。
func parseKB(line string) (uint64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}
	v, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v * 1024, true
}

// diskStats 统计 root 所在文件系统使用情况。
func diskStats(root string) (free, total uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, 0, false
	}
	bsize := uint64(st.Bsize)
	free = st.Bavail * bsize
	total = st.Blocks * bsize
	return free, total, true
}

// netStats 从 /proc/net/dev 汇总非回环接口流量。
func netStats() (rx, tx uint64, ok bool) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // 表头
	sc.Scan()
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "lo:") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx, true
}

// uptime 从 /proc/uptime 读取系统运行秒数。
func uptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}
