package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// --- 插件管理 API(用户通道,需登录;由 nasd 全权控制) ---

// pluginsList GET /api/plugins → 插件列表及状态。
func (d *Daemon) pluginsList(c *fiber.Ctx) error {
	list, err := d.pm.Scan()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"plugins": list})
}

// pluginStart POST /api/plugins/:id/start → 启动插件。
func (d *Daemon) pluginStart(c *fiber.Ctx) error {
	id := c.Params("id")
	if err := d.pm.Start(id); err != nil {
		return pluginErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "id": id})
}

// pluginStop POST /api/plugins/:id/stop → 停止插件。
func (d *Daemon) pluginStop(c *fiber.Ctx) error {
	id := c.Params("id")
	if err := d.pm.Stop(id); err != nil {
		return pluginErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "id": id})
}

// pluginRestart POST /api/plugins/:id/restart → 重启插件。
func (d *Daemon) pluginRestart(c *fiber.Ctx) error {
	id := c.Params("id")
	if err := d.pm.Restart(id); err != nil {
		return pluginErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "id": id})
}

// pluginUninstall DELETE /api/plugins/:id → 卸载插件(停止进程 + 删除文件)。
func (d *Daemon) pluginUninstall(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := d.pm.Get(id); err != nil {
		return pluginErr(c, err)
	}
	// 先停止(运行中或崩溃态均可停止;stopped 返回 ErrNotRunning,忽略)
	_ = d.pm.Stop(id)
	// 删除文件(插件目录:plugins/<name>,保留目录本身)
	full := filepath.Join(d.paths.Plugins, id)
	if fi, err := os.Lstat(full); err == nil {
		if fi.IsDir() {
			if err := os.RemoveAll(full); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "删除插件目录失败: " + err.Error()})
			}
		} else {
			if err := os.Remove(full); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "删除插件文件失败: " + err.Error()})
			}
		}
		d.log.Info("插件已卸载", "id", id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	// 重新扫描,注销登记
	if _, err := d.pm.Scan(); err != nil {
		d.log.Warn("卸载后重扫失败", "err", err)
	}
	return c.JSON(fiber.Map{"ok": true, "id": id})
}

// pluginLog GET /api/plugins/:id/log → 插件日志尾部(透传插件 stdout/stderr 由
// 系统日志承接;此处返回进程最近错误信息 + 运行状态)。
func (d *Daemon) pluginLog(c *fiber.Ctx) error {
	id := c.Params("id")
	info, err := d.pm.Get(id)
	if err != nil {
		return pluginErr(c, err)
	}
	return c.JSON(fiber.Map{
		"id":       id,
		"state":    info.State,
		"restarts": info.Restarts,
		"last_err": info.LastErr,
	})
}

// pluginInstall POST /api/plugins/install → 安装插件。
// body: {name: "插件名", source: "https://.../plugin.tar.gz"} 或 multipart 上传。
func (d *Daemon) pluginInstall(c *fiber.Ctx) error {
	// 支持两种来源:URL(body JSON)与上传(multipart file)
	var name, source string
	ct := c.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		fh, err := c.FormFile("file")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "缺少上传文件"})
		}
		name = strings.TrimSuffix(fh.Filename, ".tar.gz")
		name = strings.TrimSuffix(name, ".tgz")
		if !validPluginID(name) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "插件名非法"})
		}
		src, err := fh.Open()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		defer src.Close()
		binName, err := d.installPlugin(name, src)
		if err != nil {
			return pluginInstallErr(c, err)
		}
		return c.JSON(fiber.Map{"ok": true, "id": binName})
	}

	var body struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "参数无效"})
	}
	name = body.Name
	source = body.Source
	if !validPluginID(name) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "插件名非法"})
	}
	if source == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "缺少 source URL"})
	}
	resp, err := http.Get(source)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "下载失败: " + err.Error()})
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "下载失败: HTTP " + resp.Status})
	}
	downloaded, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "读取下载内容失败: " + err.Error()})
	}
	if len(downloaded) == 0 {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "下载内容为空"})
	}
	binName, err := d.installPlugin(name, bytes.NewReader(downloaded))
	if err != nil {
		return pluginInstallErr(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "id": binName})
}

// installPlugin 解析 .tar.gz 插件包并安装,返回实际落地文件名
// (Windows 上 PE 文件自动补 .exe 后缀)。
func (d *Daemon) installPlugin(name string, r io.Reader) (string, error) {
	binName, err := d.installPluginFromReader(name, r)
	return binName, err
}

// pluginInstallErr 安装失败错误映射。
func pluginInstallErr(c *fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	var badReq *badRequestError
	if errors.As(err, &badReq) {
		status = fiber.StatusBadRequest
	}
	return c.Status(status).JSON(fiber.Map{"error": err.Error()})
}

// badRequestError 客户端输入错误(安装包格式/内容问题)。
type badRequestError struct{ msg string }

func (e *badRequestError) Error() string { return e.msg }

// pluginBinaryName 决定插件落地文件名。
// Windows 上无扩展名的 PE 文件无法被 exec 直接启动(Go 的 LookPath 会追加
// .exe 等扩展名查找),因此探测到 PE 头时自动补 .exe。
// Termux/Linux 无此问题,保持原文件名(无后缀 ELF)。
func pluginBinaryName(name string, payload []byte) string {
	if runtime.GOOS != "windows" || filepath.Ext(name) != "" {
		return name
	}
	if len(payload) >= 2 && payload[0] == 'M' && payload[1] == 'Z' { // PE 头
		return name + ".exe"
	}
	return name
}

// installPluginFromReader 解析 .tar.gz 插件包并安装到 plugins/<name>,
// 返回实际落地文件名(Windows 上 PE 自动补 .exe)。
// 只接受单个可执行文件(或含可执行文件的单层目录),设置可执行位。
func (d *Daemon) installPluginFromReader(name string, r io.Reader) (string, error) {
	// 底层兜底校验(API 层已校验,防御直接调用)
	if !validPluginID(name) {
		return "", &badRequestError{"插件名非法"}
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", &badRequestError{"不是有效的 gzip 包"}
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var payload []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", &badRequestError{"解析 tar 失败"}
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// 只接受顶层可执行文件(忽略 ./ 前缀)
		base := filepath.Base(filepath.Clean(hdr.Name))
		if base == "." || base == ".." {
			continue
		}
		if payload != nil {
			return "", &badRequestError{"插件包应只包含一个可执行文件"}
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return "", fmt.Errorf("读取插件内容失败: %w", err)
		}
		payload = b
	}
	if payload == nil {
		return "", &badRequestError{"插件包中无可执行文件"}
	}

	// 写入 plugins/<name>(文件名保持插件 ID,不信任包内文件名)
	binName := pluginBinaryName(name, payload)
	target := filepath.Join(d.paths.Plugins, binName)
	if err := os.WriteFile(target, payload, 0o755); err != nil {
		return "", fmt.Errorf("写入插件失败: %w", err)
	}
	d.log.Info("插件已安装", "id", binName, "size", len(payload))
	if _, err := d.pm.Scan(); err != nil {
		d.log.Warn("安装后重扫失败", "err", err)
	}
	return binName, nil
}

// pluginErr 统一插件 API 错误响应。
func pluginErr(c *fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	switch {
	case errors.Is(err, ErrPluginNotFound):
		status = fiber.StatusNotFound
	case errors.Is(err, ErrAlreadyRunning), errors.Is(err, ErrNotRunning):
		status = fiber.StatusConflict
	case errors.Is(err, ErrCrashLoop):
		status = fiber.StatusConflict
	case errors.Is(err, ErrStartFailed), errors.Is(err, ErrRegFailed):
		status = fiber.StatusBadGateway
	}
	return c.Status(status).JSON(fiber.Map{"error": err.Error()})
}
