package auth

import (
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// loginLimiter 登录失败速率限制:按 IP 维度,窗口内连续失败达阈值后锁定。
// 防暴力破解;内存态,重启重置(单实例 NAS 场景足够)。
type loginLimiter struct {
	mu        sync.Mutex
	fails     map[string]failEntry // key(IP) → 失败计数(含滑动窗口时间)
	locked    map[string]time.Time // key(IP) → 锁定截止时间
	maxFails  int                  // 窗口内连续失败阈值
	window    time.Duration        // 失败计数滑动窗口
	lockFor   time.Duration        // 锁定时长
	trustXFF  bool                 // 是否信任 X-Forwarded-For(仅反向代理部署)
	maxKeys   int                  // 内存表容量上限(防无限增长)
	nextPrune time.Time            // 下次全量清扫时间
}

// failEntry 单个 IP 的失败记录。
type failEntry struct {
	count int
	last  time.Time
}

// newLoginLimiter 默认:5 次失败锁定 15 分钟,10 分钟滑动窗口。
func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		fails:    make(map[string]failEntry),
		locked:   make(map[string]time.Time),
		maxFails: 5,
		window:   10 * time.Minute,
		lockFor:  15 * time.Minute,
		maxKeys:  4096,
	}
}

// key 客户端标识:仅当显式配置信任反向代理时使用 X-Forwarded-For,
// 否则一律使用真实远端地址(默认直连部署,信任 XFF 可被伪造绕过限流)。
func (l *loginLimiter) key(c *fiber.Ctx) string {
	if l.trustXFF {
		if fwd := c.Get("X-Forwarded-For"); fwd != "" {
			if i := indexByte(fwd, ','); i >= 0 {
				fwd = fwd[:i] // 取最左侧(原始客户端)
			}
			return trimSpace(fwd)
		}
	}
	ip := c.IP()
	if ip == "" {
		ip = "unknown"
	}
	return ip
}

// allow 检查是否允许继续尝试(未锁定);顺带清扫过期条目。
func (l *loginLimiter) allow(c *fiber.Ctx) bool {
	k := l.key(c)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if until, ok := l.locked[k]; ok {
		if now.Before(until) {
			return false
		}
		delete(l.locked, k)
		delete(l.fails, k)
	}
	l.pruneIfNeeded(now)
	return true
}

// fail 记录一次失败(滑动窗口:窗口外重置计数);达到阈值则锁定。
func (l *loginLimiter) fail(c *fiber.Ctx) {
	k := l.key(c)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.fails[k]
	if !ok {
		e = failEntry{}
	}
	// 滑动窗口:距上次失败超过 window 则重新计数
	if !e.last.IsZero() && now.Sub(e.last) > l.window {
		e.count = 0
	}
	e.count++
	e.last = now
	l.fails[k] = e
	if e.count >= l.maxFails {
		l.locked[k] = now.Add(l.lockFor)
		delete(l.fails, k)
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

// pruneIfNeeded 清扫过期失败记录,防止 map 无限增长:
// 仅在条目数超容量上限时全量清扫(每分钟至多一次),常规操作零开销。
func (l *loginLimiter) pruneIfNeeded(now time.Time) {
	if len(l.fails) <= l.maxKeys && len(l.locked) <= l.maxKeys {
		return
	}
	if now.Before(l.nextPrune) {
		return
	}
	l.nextPrune = now.Add(time.Minute)
	for k, e := range l.fails {
		if now.Sub(e.last) > l.window {
			delete(l.fails, k)
		}
	}
	for k, until := range l.locked {
		if !now.Before(until) {
			delete(l.locked, k)
		}
	}
}

// setTrustXFF 配置是否信任 X-Forwarded-For(由部署模式决定)。
func (l *loginLimiter) setTrustXFF(v bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.trustXFF = v
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
