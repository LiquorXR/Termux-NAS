// Package daemon 实现主框架 nasd 守护进程。
//
// 职责边界(开发文档 §5):内建 NAS 必要功能 + 全权管理插件;
// 生命周期仅由 nasm 通过管理通道(Unix socket)控制。
package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	"github.com/termux-nas/nas/internal/mgmt"
	"github.com/termux-nas/nas/internal/svc"
	"github.com/termux-nas/nas/internal/version"
)

// Daemon 主框架守护进程。
type Daemon struct {
	cfg   *config.Config
	paths config.Paths
	log   *slog.Logger

	start time.Time
	db    *sql.DB
	auth  *auth.Store
	files *files.Store
	app   *fiber.App
	pm    *Manager          // 插件管理器(M4)
	svc   *svc.Controller   // 服务控制(M5)
	backups *backup.Manager // 备份中心(M5)

	mgmtLn  net.Listener
	mgmtSrv *mgmt.Server
	logFile io.WriteCloser

	stopSignalOnce sync.Once // 仅用于关闭 stopCh
	stopOnce       sync.Once // 仅用于执行 Stop 清理
	stopCh         chan struct{}
}

// New 创建守护进程实例(不启动)。
func New(cfg *config.Config, paths config.Paths, log *slog.Logger) *Daemon {
	return &Daemon{
		cfg:    cfg,
		paths:  paths,
		log:    log,
		start:  time.Now(),
		stopCh: make(chan struct{}),
	}
}

// Run 启动全部子系统并阻塞,直到收到停止信号或 ctx 取消。
func (d *Daemon) Run(ctx context.Context) error {
	// 0) 单实例锁:第二个实例在此失败退出,防双实例竞态
	releaseLock, err := mgmt.AcquireLock(filepath.Join(d.paths.RunDir, "nas.lock"))
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

	// 4) 管理通道 Unix socket(仅本机)
	if err := d.startMgmt(); err != nil {
		_ = app.Shutdown()
		return err
	}
	defer d.stopMgmt()

	// 5) 插件空闲回收 ticker(懒加载释放资源)
	reapCtx, reapCancel := context.WithCancel(ctx)
	defer reapCancel()
	go d.reapLoop(reapCtx)

	// 6) 备份调度 ticker(每分钟检查 cron 到期任务)
	backupCtx, backupCancel := context.WithCancel(ctx)
	defer backupCancel()
	go d.backupLoop(backupCtx)

	d.log.Info("nasd 已就绪", "version", version.String(), "pid", os.Getpid(),
		"root", d.paths.Root)

	select {
	case <-ctx.Done():
		d.log.Info("收到退出信号,开始优雅停止")
	case <-d.stopCh:
		d.log.Info("收到管理通道停止指令,开始优雅停止")
	case err := <-httpErr:
		if err != nil {
			d.log.Error("HTTP 服务异常退出", "err", err)
		}
	}

	stopErr := d.Stop()
	d.log.Info("Stop 完成,Run 即将返回")
	return stopErr
}

// Stop 优雅停止:先停插件进程,再关 HTTP,最后清理管理通道退出。
func (d *Daemon) Stop() error {
	var firstErr error
	d.stopOnce.Do(func() {
		if d.pm != nil {
			d.pm.ShutdownAll()
		}
		if d.app != nil {
			// 带超时关闭:避免 keep-alive 连接长期阻塞进程退出
			// (阻塞会导致单实例锁不释放,nasm update 无法替换二进制)
			if err := d.app.ShutdownWithTimeout(3 * time.Second); err != nil {
				d.log.Warn("HTTP 关闭异常", "err", err)
				firstErr = err
			}
		}
		// 清理管理通道残留文件(unix socket / windows addr 文件)
		_ = mgmt.Cleanup(d.paths.SockPath)
		d.log.Info("nasd 已退出", "uptime", time.Since(d.start).Round(time.Second))
	})
	return firstErr
}

// WaitStop 返回停止信号通道(由管理通道 daemon.stop 触发)。
func (d *Daemon) WaitStop() <-chan struct{} { return d.stopCh }

func (d *Daemon) requestStop() {
	d.stopSignalOnce.Do(func() { close(d.stopCh) })
}

// --- mgmt.Handler 实现(仅生命周期方法,见开发文档 §4.2) ---

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

// setUpdateFlag 写入更新标记(run/update.flag),供 nasm 确认进入更新模式。
func (d *Daemon) setUpdateFlag() error {
	return os.WriteFile(filepath.Join(d.paths.RunDir, "update.flag"),
		[]byte("nasd update in progress"), 0o644)
}

// Handle 分发管理通道请求。
func (d *Daemon) Handle(method string, params json.RawMessage) (json.RawMessage, *mgmt.RPCError) {
	switch method {
	case mgmt.MethodStatus:
		return marshal(mgmt.StatusResult{
			Running: true,
			Version: version.String(),
			Uptime:  int64(time.Since(d.start).Seconds()),
			PID:     os.Getpid(),
			Healthy: true,
			Port:    d.cfg.Port,
		})
	case mgmt.MethodStop:
		// 异步停止,先应答再退出,保证 nasm 能收到成功响应。
		go d.requestStop()
		return marshal(map[string]bool{"stopping": true})
	case mgmt.MethodEnterUpdate:
		// 进入更新模式:优雅停止(先停插件,再关 HTTP 退出),由 nasm 完成二进制替换。
		// 与 daemon.stop 的区别:记录更新标记,重启由 nasm 负责。
		d.log.Info("进入更新模式,准备停止")
		go func() {
			if err := d.setUpdateFlag(); err != nil {
				d.log.Warn("写入更新标记失败", "err", err)
			}
			d.requestStop()
		}()
		return marshal(map[string]bool{"accepted": true})
	case mgmt.MethodLogTail:
		var p mgmt.LogTailParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &mgmt.RPCError{Code: mgmt.ErrCodeInvalidParams, Message: "参数无效"}
			}
		}
		if p.Lines <= 0 {
			p.Lines = 100
		}
		lines, err := tailFile(filepath.Join(d.paths.LogDir, "nasd.log"), p.Lines)
		if err != nil {
			return nil, &mgmt.RPCError{Code: mgmt.ErrCodeInternal, Message: err.Error()}
		}
		return marshal(mgmt.LogTailResult{Lines: lines})
	default:
		return nil, &mgmt.RPCError{Code: mgmt.ErrCodeMethodNotFound, Message: "未知管理方法: " + method}
	}
}

// --- 小工具 ---

func marshal(v any) (json.RawMessage, *mgmt.RPCError) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, &mgmt.RPCError{Code: mgmt.ErrCodeInternal, Message: "序列化失败"}
	}
	return b, nil
}

// maxTailBytes 日志尾部读取上限:防止日志膨胀时整文件读入内存。
const maxTailBytes = 1 << 20 // 1 MiB

// tailFile 返回文件最后 n 行;只读取文件尾部最多 maxTailBytes 字节。
func tailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{"(日志文件尚未创建)"}, nil
		}
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := st.Size() - maxTailBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	lines := splitLines(string(buf))
	if start > 0 && len(lines) > 0 {
		// 从文件中间截断,首行可能不完整,丢弃
		lines = lines[1:]
	}
	if len(lines) <= n {
		return lines, nil
	}
	return lines[len(lines)-n:], nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
