package daemon

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"github.com/termux-nas/nas/internal/version"
	"github.com/termux-nas/nas/internal/webui"
)

// buildHTTP 组装用户通道路由(:7531)。
// M2 提供:认证中心(登录/会话/首次设置)+ 前端壳(登录页/设置页/应用壳)。
// M3 提供:文件管理 + 系统监控(HTMX 轮询看板)。
// M4+ 在此追加插件、服务、备份等模块路由。
func (d *Daemon) buildHTTP() (*fiber.App, error) {
	app := fiber.New(fiber.Config{
		AppName:               "Termux NAS",
		DisableStartupMessage: true,
		// 上传大小上限 512 MiB(fasthttp 会整体缓冲,手机内存有限,暂取保守值)
		BodyLimit: 512 << 20,
	})

	// 健康检查(供 nasm status 探活与 runit 监控)
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"version": version.String(),
			"uptime":  int64(time.Since(d.start).Seconds()),
			"pid":     os.Getpid(),
			"port":    d.cfg.Port,
		})
	})

	// 版本信息
	app.Get("/api/version", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"version":   version.Version,
			"commit":    version.Commit,
			"buildTime": version.BuildTime,
		})
	})

	// --- M2 认证中心 ---
	// 预认证接口(无需会话):首次设置 / 登录 / 登出
	app.Post("/api/auth/setup", checkSameOrigin, d.auth.HandleSetup)
	app.Post("/api/auth/login", checkSameOrigin, d.auth.HandleLogin)
	app.Post("/api/auth/logout", checkSameOrigin, d.auth.OptionalAuth, d.auth.HandleLogout)
	// 需会话接口
	app.Get("/api/auth/me", d.auth.RequireAuth, d.auth.HandleMe)

	// --- 页面 ---
	// 登录页:已登录则直接进入应用壳
	app.Get("/login", func(c *fiber.Ctx) error {
		if d.auth.SessionUser(c) != nil {
			return c.Redirect("/")
		}
		return servePage(c, "login.html")
	})
	// 首次设置页:系统已初始化后不可访问
	app.Get("/setup", func(c *fiber.Ctx) error {
		has, err := d.auth.HasUsers()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("系统错误")
		}
		if has {
			return c.Redirect("/login")
		}
		if d.auth.SessionUser(c) != nil {
			return c.Redirect("/")
		}
		return servePage(c, "setup.html")
	})
	// 应用壳(需登录)
	app.Get("/", d.auth.PageAuth, func(c *fiber.Ctx) error {
		return servePage(c, "app.html")
	})

	// --- M3 文件管理 ---
	app.Get("/api/files/list", d.auth.RequireAuth, d.files.HandleList)
	app.Get("/api/files/download", d.auth.RequireAuth, d.files.HandleDownload)
	app.Get("/api/files/search", d.auth.RequireAuth, d.files.HandleSearch)
	app.Post("/api/files/mkdir", checkSameOrigin, d.auth.RequireAuth, d.files.HandleMkdir)
	app.Post("/api/files/upload", checkSameOrigin, d.auth.RequireAuth, d.files.HandleUpload)
	app.Post("/api/files/rename", checkSameOrigin, d.auth.RequireAuth, d.files.HandleRename)
	app.Post("/api/files/delete", checkSameOrigin, d.auth.RequireAuth, d.files.HandleDelete)
	app.Post("/api/files/share", checkSameOrigin, d.auth.RequireAuth, d.files.HandleShare)
	// 分享链接(公开,凭 token 访问)
	app.Get("/s/:token", d.files.HandleShareDownload)

	// --- M3 系统监控 ---
	app.Get("/api/monitor/summary", d.auth.RequireAuth, d.monitorSummary)

	// --- 前端片段(HTMX,需登录) ---
	app.Get("/partials/files", d.auth.RequireAuth, d.files.HandlePartial)
	app.Get("/partials/monitor", d.auth.RequireAuth, d.monitorPartial)

	// 前端壳占位片段(HTMX 局部加载,需登录)
	app.Get("/partials/plugins", d.auth.RequireAuth, func(c *fiber.Ctx) error {
		return servePage(c, "partials/plugins.html")
	})
	app.Get("/partials/services", d.auth.RequireAuth, func(c *fiber.Ctx) error {
		return servePage(c, "partials/services.html")
	})
	app.Get("/partials/settings", d.auth.RequireAuth, func(c *fiber.Ctx) error {
		return servePage(c, "partials/settings.html")
	})

	// 内建模块路由占位(M4+ 实现):
	// /api/svc/*   服务控制
	// /api/backup/* 备份中心
	// /api/plugins/* 插件管理器
	// /p/<id>/*   插件反代(M4)

	// --- M5 服务控制 ---
	app.Get("/api/svc/list", d.auth.RequireAuth, d.svcList)
	app.Post("/api/svc/start", checkSameOrigin, d.auth.RequireAuth, d.svcStart)
	app.Post("/api/svc/stop", checkSameOrigin, d.auth.RequireAuth, d.svcStop)
	app.Post("/api/svc/restart", checkSameOrigin, d.auth.RequireAuth, d.svcRestart)
	app.Post("/api/svc/autostart", checkSameOrigin, d.auth.RequireAuth, d.svcAutostart)

	// --- M4 插件管理 API(用户通道,需登录;nasd 全权控制) ---
	app.Get("/api/plugins", d.auth.RequireAuth, d.pluginsList)
	app.Post("/api/plugins/install", checkSameOrigin, d.auth.RequireAuth, d.pluginInstall)
	app.Post("/api/plugins/:id/start", checkSameOrigin, d.auth.RequireAuth, d.pluginStart)
	app.Post("/api/plugins/:id/stop", checkSameOrigin, d.auth.RequireAuth, d.pluginStop)
	app.Post("/api/plugins/:id/restart", checkSameOrigin, d.auth.RequireAuth, d.pluginRestart)
	app.Delete("/api/plugins/:id", checkSameOrigin, d.auth.RequireAuth, d.pluginUninstall)
	app.Get("/api/plugins/:id/log", d.auth.RequireAuth, d.pluginLog)

	// --- M4 插件反代(懒加载入口,统一鉴权) ---
	app.All("/p/:id/*", d.auth.RequireAuth, d.pluginProxy)
	app.All("/p/:id", d.auth.RequireAuth, d.pluginProxy)

	// 前端静态资源(嵌入二进制)
	sub, err := fs.Sub(webui.Static, "static")
	if err != nil {
		return nil, err
	}
	// fiber v2 的 Static 只接受磁盘路径;嵌入资源通过 fasthttpadaptor 挂载。
	// 放在路由末尾:先匹配 /health、/api/*、页面路由,其余交给静态文件服务。
	fileServer := fasthttpadaptor.NewFastHTTPHandler(http.FileServer(http.FS(sub)))
	app.Use("/", func(c *fiber.Ctx) error {
		fileServer(c.Context())
		return nil
	})

	return app, nil
}

// servePage 从嵌入资源输出页面(设置 text/html)。
func servePage(c *fiber.Ctx, name string) error {
	b, err := webui.Static.ReadFile("static/" + name)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("页面不存在")
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(b)
}

// checkSameOrigin 非 GET 请求校验 Origin 与 Host 一致(CSRF 第二道防线;
// 主防线为 SameSite=Lax 会话 cookie)。
func checkSameOrigin(c *fiber.Ctx) error {
	if c.Method() == fiber.MethodGet || c.Method() == fiber.MethodHead || c.Method() == fiber.MethodOptions {
		return c.Next()
	}
	origin := c.Get("Origin")
	if origin == "" || origin == "null" {
		return c.Next() // 非浏览器客户端(CLI/curl)
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host != c.Hostname() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "跨站请求被拒绝"})
	}
	return c.Next()
}

// versionString 供 db.go 记录版本。
func versionString() string { return version.String() }
