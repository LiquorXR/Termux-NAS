package auth

import (
	"github.com/gofiber/fiber/v2"
)

// CookieName 会话 cookie 名。
const CookieName = "nas_session"

// RequireAuth API 认证中间件:无有效会话返回 401 JSON。
func (s *Store) RequireAuth(c *fiber.Ctx) error {
	user, err := s.authenticate(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "未登录或会话已过期"})
	}
	c.Locals("user", user)
	return c.Next()
}

// PageAuth 页面认证中间件:无有效会话重定向到登录页。
func (s *Store) PageAuth(c *fiber.Ctx) error {
	user, err := s.authenticate(c)
	if err != nil {
		return c.Redirect("/login")
	}
	c.Locals("user", user)
	return c.Next()
}

// OptionalAuth 可选认证:有会话则附加用户,无则继续(供登出等接口)。
func (s *Store) OptionalAuth(c *fiber.Ctx) error {
	if user, err := s.authenticate(c); err == nil {
		c.Locals("user", user)
	}
	return c.Next()
}

// authenticate 从 cookie 解析会话并加载用户。
func (s *Store) authenticate(c *fiber.Ctx) (*User, error) {
	token := c.Cookies(CookieName)
	if token == "" {
		return nil, ErrNoSession
	}
	sess, err := s.GetSession(token)
	if err != nil {
		return nil, err
	}
	return s.UserByID(sess.UserID)
}

// SessionUser 当前请求的已登录用户(无有效会话返回 nil)。
func (s *Store) SessionUser(c *fiber.Ctx) *User {
	user, err := s.authenticate(c)
	if err != nil {
		return nil
	}
	return user
}

// CurrentUser 从上下文读取当前用户(RequireAuth/PageAuth 之后)。
func CurrentUser(c *fiber.Ctx) *User {
	u, _ := c.Locals("user").(*User)
	return u
}

// SetSessionCookie 下发会话 cookie(HttpOnly + SameSite=Lax)。
func SetSessionCookie(c *fiber.Ctx, token string) {
	c.Cookie(&fiber.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

// Logout 清理会话与 cookie。
func (s *Store) Logout(c *fiber.Ctx) {
	if token := c.Cookies(CookieName); token != "" {
		_ = s.DeleteSession(token)
	}
	c.ClearCookie(CookieName)
}
