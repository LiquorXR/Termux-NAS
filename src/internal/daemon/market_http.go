package daemon

import (
	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/market"
)

// --- 插件市场 API(用户通道,需登录) ---

// marketIndex GET /api/market → 市场索引(内嵌官方市场)。
func (d *Daemon) marketIndex(c *fiber.Ctx) error {
	idx, err := market.Load()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	// 标注已安装状态
	installed := map[string]bool{}
	for _, p := range d.pm.List() {
		installed[p.ID] = true
	}
	type entry struct {
		market.Plugin
		Installed bool `json:"installed"`
	}
	out := make([]entry, 0, len(idx.Plugins))
	for _, p := range idx.Plugins {
		out = append(out, entry{Plugin: p, Installed: installed[p.ID] || installed[p.ID+".exe"]})
	}
	return c.JSON(fiber.Map{"market": fiber.Map{
		"name":    idx.Name,
		"version": idx.Version,
		"plugins": out,
	}})
}

// marketInstall POST /api/market/install → 从市场一键安装 (body: {id})。
func (d *Daemon) marketInstall(c *fiber.Ctx) error {
	var body struct {
		ID string `json:"id"`
	}
	if err := c.BodyParser(&body); err != nil || body.ID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "缺少插件 ID"})
	}
	idx, err := market.Load()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	p, ok := idx.Find(body.ID)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "市场中不存在插件: " + body.ID})
	}
	// 复用插件安装器:URL 下载 → 校验 → 落盘 → 扫描
	if err := d.installPluginFromURL(p.ID, p.DownloadURL); err != nil {
		return pluginInstallErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "id": p.ID})
}
