package daemon

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// --- 服务控制 API(用户通道,需登录) ---

// svcList GET /api/svc/list → 服务列表及状态。
func (d *Daemon) svcList(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"services": d.svc.List(c.Context())})
}

// svcStart POST /api/svc/start → 启动服务 (body: {name})。
func (d *Daemon) svcStart(c *fiber.Ctx) error {
	name, err := svcName(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if _, err := d.svc.Get(c.Context(), name); err != nil {
		return svcErr(c, err)
	}
	if err := d.svc.Start(c.Context(), name); err != nil {
		return svcErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "name": name})
}

// svcStop POST /api/svc/stop → 停止服务 (body: {name})。
func (d *Daemon) svcStop(c *fiber.Ctx) error {
	name, err := svcName(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if _, err := d.svc.Get(c.Context(), name); err != nil {
		return svcErr(c, err)
	}
	if err := d.svc.Stop(c.Context(), name); err != nil {
		return svcErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "name": name})
}

// svcRestart POST /api/svc/restart → 重启服务 (body: {name})。
func (d *Daemon) svcRestart(c *fiber.Ctx) error {
	name, err := svcName(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if _, err := d.svc.Get(c.Context(), name); err != nil {
		return svcErr(c, err)
	}
	if err := d.svc.Restart(c.Context(), name); err != nil {
		return svcErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "name": name})
}

// svcAutostart POST /api/svc/autostart → 设置开机自启 (body: {name, enabled})。
func (d *Daemon) svcAutostart(c *fiber.Ctx) error {
	var body struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := c.BodyParser(&body); err != nil || body.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "参数无效"})
	}
	if _, err := d.svc.Get(c.Context(), body.Name); err != nil {
		return svcErr(c, err)
	}
	if err := d.svc.SetAutostart(c.Context(), body.Name, body.Enabled); err != nil {
		return svcErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "name": body.Name, "enabled": body.Enabled})
}

// svcName 从请求体解析服务名。
func svcName(c *fiber.Ctx) (string, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil || body.Name == "" {
		return "", errors.New("参数无效: 缺少 name")
	}
	return body.Name, nil
}

// svcErr 服务操作错误响应。
func svcErr(c *fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	switch {
	case strings.HasPrefix(err.Error(), "未知服务"):
		status = fiber.StatusNotFound
	case strings.HasPrefix(err.Error(), "参数无效"):
		status = fiber.StatusBadRequest
	}
	return c.Status(status).JSON(fiber.Map{"error": err.Error()})
}
