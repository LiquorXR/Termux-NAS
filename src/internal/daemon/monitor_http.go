package daemon

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/monitor"
)

// monitorSummary GET /api/monitor/summary → 系统状态 JSON(需登录)。
// 看板渲染由前端(Vite SPA)完成,见 src/web/src/pages/monitor.js。
func (d *Daemon) monitorSummary(c *fiber.Ctx) error {
	return c.JSON(monitor.Collect(d.files.Root()))
}

// humanSize 人类可读字节数。
func humanSize(n uint64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatUint(n, 10) + " B"
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + []string{"KB", "MB", "GB", "TB"}[exp]
}

// formatUptime 秒数转人类可读时长。
func formatUptime(sec float64) string {
	d := time.Duration(sec * float64(time.Second))
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(m) + "m" + strconv.Itoa(s) + "s"
}
