package daemon

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/proxy"
)

// pluginProxy 插件反向代理(懒加载入口)。
//
// 路径映射:/p/<id>/* → http://127.0.0.1:<插件端口>/*
// 懒加载:插件未运行时由 EnsureRunning 启动并等待注册;
// 统一鉴权由调用方(路由注册处 RequireAuth)保证。
func (d *Daemon) pluginProxy(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "缺少插件 ID"})
	}
	if !validPluginID(id) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "插件 ID 非法"})
	}

	info, err := d.pm.EnsureRunning(id)
	if err != nil {
		status := fiber.StatusServiceUnavailable
		msg := err.Error()
		if err == ErrPluginNotFound {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(fiber.Map{"error": msg})
	}
	if info.Reg == nil || info.Reg.Port <= 0 {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "插件未完成注册"})
	}

	// 更新活跃时间(空闲回收依据)
	d.pm.Touch(id)

	// 拼接目标地址:保留子路径与查询参数
	rest := strings.TrimPrefix(c.Params("*"), "/")
	target := fmt.Sprintf("http://127.0.0.1:%d", info.Reg.Port)
	if rest != "" {
		target += "/" + rest
	}
	if qs := string(c.Request().URI().QueryString()); len(qs) > 0 {
		target += "?" + string(qs)
	}
	return proxy.Do(c, target)
}

// validPluginID 校验插件 ID:仅允许字母数字、下划线、点、短横线,
// 显式拒绝 "." 与 ".."(路径穿越),防止特殊字符注入。
func validPluginID(id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// buildPluginProxyURL 供测试验证路径拼接(纯函数)。
func buildPluginProxyURL(regPort int, subPath, query string) string {
	u := fmt.Sprintf("http://127.0.0.1:%d/%s", regPort, strings.TrimPrefix(subPath, "/"))
	if query != "" {
		u += "?" + query
	}
	return u
}

// parsePluginProxyURL 供测试解析(验证拼接结果)。
func parsePluginProxyURL(raw string) (host string, path string, query string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", err
	}
	return u.Host, u.Path, u.RawQuery, nil
}
