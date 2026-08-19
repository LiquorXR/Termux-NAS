package files

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
)

// defaultShareHours 分享链接默认有效期。
const defaultShareHours = 24

// HandleShare POST /api/files/share {path, expires_hours} → {url}。
// 生成随机 token 入库,公开端点 GET /s/<token> 供下载。
func (s *Store) HandleShare(c *fiber.Ctx) error {
	var p struct {
		Path         string `json:"path"`
		ExpiresHours int    `json:"expires_hours"`
	}
	if err := c.BodyParser(&p); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "请求格式无效"})
	}
	full, err := s.norm(p.Path)
	if err != nil {
		return s.respondErr(c, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return s.respondErr(c, err)
	}
	if info.IsDir() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "暂不支持分享目录"})
	}
	if p.ExpiresHours <= 0 {
		p.ExpiresHours = defaultShareHours
	}
	if p.ExpiresHours > 24*365 {
		p.ExpiresHours = 24 * 365
	}
	token, err := shareToken(16)
	if err != nil {
		s.log.Error("生成分享 token 失败", "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "生成分享链接失败"})
	}
	now := time.Now()
	_, err = s.db.Exec(
		`INSERT INTO shares(token, path, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, s.rel(full), now.Format(time.RFC3339), now.Add(time.Duration(p.ExpiresHours)*time.Hour).Format(time.RFC3339))
	if err != nil {
		s.log.Error("保存分享失败", "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建分享失败"})
	}
	return c.JSON(fiber.Map{"ok": true, "url": "/s/" + token, "expires_at": now.Add(time.Duration(p.ExpiresHours) * time.Hour).Format(time.RFC3339)})
}

// HandleShareDownload GET /s/:token → 校验有效期并流式下载(内联)。
func (s *Store) HandleShareDownload(c *fiber.Ctx) error {
	token := c.Params("token")
	var relPath, expiresAt string
	err := s.db.QueryRow(`SELECT path, expires_at FROM shares WHERE token = ?`, token).Scan(&relPath, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "分享链接不存在"})
		}
		s.log.Error("查询分享失败", "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "系统错误"})
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(t) {
		_ = s.DeleteShare(token)
		return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": "分享链接已过期"})
	}
	full, err := s.norm(relPath)
	if err != nil {
		return s.respondErr(c, err)
	}
	if _, err := os.Stat(full); err != nil {
		return s.respondErr(c, err)
	}
	return s.serveFile(c, full, "", true)
}

// DeleteShare 删除分享(token 过期清理)。
func (s *Store) DeleteShare(token string) error {
	_, err := s.db.Exec(`DELETE FROM shares WHERE token = ?`, token)
	return err
}

// rel 返回相对路径(供入库)。
func (s *Store) rel(full string) string { return Rel(s.root, full) }

// shareToken 生成 n 字节随机 hex 分享 token。
func shareToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
