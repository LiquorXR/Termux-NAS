package auth

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// SetupParams 首次设置参数。
type SetupParams struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginParams 登录参数。
type LoginParams struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// HandleSetup 首次设置:创建管理员并自动登录。系统已初始化后禁用。
func (s *Store) HandleSetup(c *fiber.Ctx) error {
	has, err := s.HasUsers()
	if err != nil {
		s.log.Error("检查用户数失败", "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "系统内部错误"})
	}
	if has {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "系统已初始化,不可重复设置"})
	}
	var p SetupParams
	if err := c.BodyParser(&p); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "请求格式无效"})
	}
	p.Username = strings.TrimSpace(p.Username)
	if !validUsername(p.Username) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "用户名需 2-32 位,仅含字母、数字、- _ ."})
	}
	if len(p.Password) < 8 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "密码至少 8 位"})
	}
	if err := s.CreateUser(p.Username, p.Password); err != nil {
		s.log.Error("创建用户失败", "username", p.Username, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建用户失败"})
	}
	user, err := s.Authenticate(p.Username, p.Password)
	if err != nil {
		s.log.Error("设置后自动登录失败", "username", p.Username, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建用户失败"})
	}
	sess, err := s.CreateSession(user.ID)
	if err != nil {
		s.log.Error("创建会话失败", "user_id", user.ID, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建用户失败"})
	}
	SetSessionCookie(c, sess.Token, s.secure)
	c.Set("HX-Redirect", "/")
	return c.JSON(fiber.Map{"ok": true})
}

// HandleLogin 登录:校验凭据并下发会话 cookie。含失败速率限制。
func (s *Store) HandleLogin(c *fiber.Ctx) error {
	// 限流:锁定期间直接拒绝(附 Retry-After)
	if !s.limiter.allow(c) {
		ra := s.limiter.retryAfter(c)
		if ra > 0 {
			c.Set("Retry-After", strconv.Itoa(ra))
		}
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error":       "尝试过于频繁,请稍后再试",
			"retry_after": ra,
		})
	}
	var p LoginParams
	if err := c.BodyParser(&p); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "请求格式无效"})
	}
	user, err := s.Authenticate(p.Username, p.Password)
	if err != nil {
		if errors.Is(err, ErrBadCreds) {
			s.limiter.fail(c) // 记录失败
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "用户名或密码错误"})
		}
		s.log.Error("登录校验失败", "username", p.Username, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "登录失败"})
	}
	s.limiter.success(c) // 登录成功清除计数
	sess, err := s.CreateSession(user.ID)
	if err != nil {
		s.log.Error("创建会话失败", "user_id", user.ID, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "登录失败"})
	}
	SetSessionCookie(c, sess.Token, s.secure)
	c.Set("HX-Redirect", "/")
	return c.JSON(fiber.Map{"ok": true, "username": user.Username})
}

// HandleLogout 登出:删除会话并清除 cookie。
func (s *Store) HandleLogout(c *fiber.Ctx) error {
	s.Logout(c)
	c.Set("HX-Redirect", "/login")
	return c.JSON(fiber.Map{"ok": true})
}

// HandleMe 当前登录用户信息(需 RequireAuth)。
func (s *Store) HandleMe(c *fiber.Ctx) error {
	user := CurrentUser(c)
	return c.JSON(fiber.Map{
		"username":   user.Username,
		"created_at": user.CreatedAt.Format(time.RFC3339),
	})
}

// HandleStatus 初始化与会话状态(免鉴权;SPA 首屏三态判断:未初始化/未登录/已登录)。
func (s *Store) HandleStatus(c *fiber.Ctx) error {
	has, err := s.HasUsers()
	if err != nil {
		s.log.Error("检查用户数失败", "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "系统内部错误"})
	}
	authed := false
	username := ""
	if u := s.SessionUser(c); u != nil {
		authed = true
		username = u.Username
	}
	return c.JSON(fiber.Map{"initialized": has, "authed": authed, "username": username})
}

// validUsername 校验用户名格式(字母/数字/-/_/.,2-32 位)。
func validUsername(u string) bool {
	if len(u) < 2 || len(u) > 32 {
		return false
	}
	for _, r := range u {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
