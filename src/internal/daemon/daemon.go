// Package daemon 实现主框架 nasd 守护进程。
//
// 职责边界(开发文档 §5):内建 NAS 必要功能 + 全权管理插件。
// 生命周期由仓库根 nas.sh 全周期管理(SIGTERM 优雅停止 / HTTP 健康探活 /
// 日志文件直读),不再依赖任何 Go 管理 CLI 或本地管理 socket。
package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/auth"
	"github.com/termux-nas/nas/internal/backup"
	"github.com/termux-nas/nas/internal/config"
	"github.com/termux-nas/nas/internal/files"
	"github.com/termux-nas/nas/internal/lock"
	"github.com/termux-nas/nas/internal/svc"
	"github.com/termux-nas/nas/internal/version"
)

// Daemon 主框架守护进程。
type Daemon struct {
	cfg   *config.Config
	paths config.Paths
	log   *slog.Logger

	start   time.Time
	db      *sql.DB
	auth    *auth.Store
	files   *files.Store
	app     *fiber.App
	pm      *Manager        // 插件管理器(M4)
	svc     *svc.Controller // 服务控制(M5)
	backups *backup.Manager // 备份中心(M5)

	stopOnce sync.Once // 仅用于执行 Stop 清理
}

// New 创建守护进程实例(不启动)。
func New(cfg *config.Config, paths config.Paths, log *slog.Logger) *Daemon {
	return &Daemon{
		cfg:   cfg,
		paths: paths,
		log:   log,
		start: time.Now(),
	}
}

// Run 启动全部子系统并阻塞,直到 ctx 取消(SIGINT/SIGTERM)。
func (d *Daemon) Run(ctx context.Context) error {
	// 0) 单实例锁:第二个实例在此失败退出,防双实例竞态
	releaseLock, err := lock.AcquireLock(filepath.Join(d.paths.RunDir, "nas.lock"))
	if err != nil {
		return err
	}
	defer func() { _ = releaseLock() }()

	// 1) SQLite(WAL 模式)
	if err := d.openDB(); err != nil {
		return err
	}
	defer d.closeDB()

	// 1.5) 认证存储(M2 认证中心)
	d.auth = auth.NewStore(d.db, d.log)
	// 安全部署选项(仅反向代理/HTTPS 场景开启,见 config.Config 注释)
	d.auth.SetTrustProxy(d.cfg.TrustProxy)
	d.auth.SetCookieSecure(d.cfg.ForceHTTPS)

	// 1.6) 文件管理(M3):根目录可用 config.file_root 覆盖(默认 <root>/files)
	fileRoot := d.paths.FilesDir
	if d.cfg.FileRoot != "" {
		fileRoot = d.cfg.FileRoot
	}
	if err := os.MkdirAll(fileRoot, 0o755); err != nil {
		return fmt.Errorf("创建文件根目录 %s: %w", fileRoot, err)
	}
	d.files = files.NewStore(fileRoot, d.db, d.log)

	// 2) 插件管理器:扫描登记元信息,不启动进程(懒加载)
	d.pm = NewManager(d.paths.Plugins, d.log)
	if _, err := d.pm.Scan(); err != nil {
		d.log.Warn("插件扫描失败", "err", err)
	} else {
		d.log.Info("插件扫描完成", "count", len(d.pm.List()))
	}

	// 2.5) 服务控制(M5):基于 termux-services,Windows 开发环境自动模拟
	d.svc = svc.New(nil, nil, d.log)

	// 2.6) 备份中心(M5):任务存储 + 调度 + 执行 + 通知
	backupStore, err := backup.NewStore(d.db, d.log)
	if err != nil {
		return fmt.Errorf("初始化备份存储: %w", err)
	}
	d.backups = backup.NewManager(backupStore, d.log, nil)

	// 3) 用户通道 HTTP :7531
	app, err := d.buildHTTP()
	if err != nil {
		return err
	}
	d.app = app
	httpErr := make(chan error, 1)
	go func() {
		d.log.Info("用户通道启动", "addr", d.cfg.Host+":"+itoa(d.cfg.Port))
		httpErr <- app.Listen(d.cfg.Host + ":" + itoa(d.cfg.Port))
	}()

	// 4) 插件空闲回收 ticker(懒加载释放资源)
	reapCtx, reapCancel := context.WithCancel(ctx)
	defer reapCancel()
	go d.reapLoop(reapCtx)

	// 5) 备份调度 ticker(每分钟检查 cron 到期任务)
	backupCtx, backupCancel := context.WithCancel(ctx)
	defer backupCancel()
	go d.backupLoop(backupCtx)

	d.log.Info("nasd 已就绪", "version", version.String(), "pid", os.Getpid(),
		"root", d.paths.Root)

	select {
	case <-ctx.Done():
		d.log.Info("收到退出信号(SIGINT/SIGTERM),开始优雅停止")
	case err := <-httpErr:
		if err != nil {
			d.log.Error("HTTP 服务异常退出", "err", err)
		}
	}

	return d.Stop()
}

// Stop 优雅停止:先停插件进程,再关 HTTP。
func (d *Daemon) Stop() error {
	var firstErr error
	d.stopOnce.Do(func() {
		if d.pm != nil {
			d.pm.ShutdownAll()
		}
		if d.app != nil {
			// 带超时关闭:避免 keep-alive 连接长期阻塞进程退出
			// (阻塞会导致单实例锁不释放,nas.sh 无法安全替换二进制)
			if err := d.app.ShutdownWithTimeout(3 * time.Second); err != nil {
				d.log.Warn("HTTP 关闭异常", "err", err)
				firstErr = err
			}
		}
		d.log.Info("nasd 已退出", "uptime", time.Since(d.start).Round(time.Second))
	})
	return firstErr
}

// --- 后台循环 ---

// reapLoop 周期扫描空闲插件并回收(懒加载配套)。
func (d *Daemon) reapLoop(ctx context.Context) {
	if d.cfg.PluginIdleTimeout <= 0 {
		return
	}
	idle := time.Duration(d.cfg.PluginIdleTimeout) * time.Second
	ticker := time.NewTicker(idle / 2) // 半周期扫描一次
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reaped := d.pm.Reap(idle); len(reaped) > 0 {
				d.log.Info("空闲插件已回收", "ids", reaped)
			}
		}
	}
}

// backupLoop 每分钟扫描一次 cron 到期任务并触发备份。
func (d *Daemon) backupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	// 启动后先对齐一次(立即检查,便于测试)
	d.backups.Schedule(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			d.backups.Schedule(now)
		}
	}
}

// --- 小工具 ---

func itoa(n int) string { return strconv.Itoa(n) }
