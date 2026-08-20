package daemon

import (
	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/monitor"
)

// monitorSummary GET /api/monitor/summary → 系统状态 JSON(需登录)。
// 看板渲染由前端(Vite SPA)完成,见 src/web/src/pages/monitor.js。
func (d *Daemon) monitorSummary(c *fiber.Ctx) error {
	return c.JSON(monitor.Collect(d.files.Root()))
}
