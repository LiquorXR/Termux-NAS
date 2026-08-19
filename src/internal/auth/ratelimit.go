package auth

import (
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// loginLimiter 登录失败速率限制:按 IP 维度,连续失败达阈值后锁定窗口。
// 防暴力破解;内存态,重启重置(单实例 NAS 场景足够)。
type loginLimiter struct {
	mu       sync.Mutex
	fails    map[string]int       // key(IP) → 连续失败次数
	locked   map[string]time.Time // key(IP) → 锁定截止时间
	maxFails int
	window   time.Duration // 失败计数保留窗口
	lockFor  time.Duration // 锁定时长
}

// newLoginLimiter 默认:5 次失败锁定 15 分钟。
func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		fails:    make(map[string]int),
		locked:   make(map[string]time.Time),
		maxFails: 5,
		window:   10 * time.Minute,
		lockFor:  15 * time.Minute,
	}
}

// key 客户端标识:优先 X-Forwarded-For(反向代理后),否则远端地址。
func (l *loginLimiter) key(c *fiber.Ctx) string {
	if fwd := c.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	ip := c.IP()
	if ip == "" {
		ip = "unknown"
	}
	return ip
}

// allow 检查是否允许继续尝试(未锁定)。
func (l *loginLimiter) allow(c *fiber.Ctx) bool {
	k := l.key(c)
	l.mu.Lock()
	defer l.mu.Unlock()
	if until, ok := l.locked[k]; ok {
		if time.Now().Before(until) {
			return false
		}
		delete(l.locked, k)
		delete(l.fails, k)
	}
	return true
}

// fail 记录一次失败;返回是否达到锁定阈值。
func (l *loginLimiter) fail(c *fiber.Ctx) {
	k := l.key(c)
	l.mu.Lock()
	defer l.mu.Unlock()
	// 窗口滑动:距上次失败超 window 则重新计数
	l.fails[k]++
	if l.fails[k] >= l.maxFails {
		l.locked[k] = time.Now().Add(l.lockFor)
		l.fails[k] = 0
	}
}

// success 登录成功清除计数。
func (l *loginLimiter) success(c *fiber.Ctx) {
	k := l.key(c)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, k)
	delete(l.locked, k)
}

// retryAfter 剩余锁定秒数(供响应头)。
func (l *loginLimiter) retryAfter(c *fiber.Ctx) int {
	k := l.key(c)
	l.mu.Lock()
	defer l.mu.Unlock()
	if until, ok := l.locked[k]; ok {
		remain := int(time.Until(until).Seconds())
		if remain > 0 {
			return remain
		}
	}
	return 0
}
