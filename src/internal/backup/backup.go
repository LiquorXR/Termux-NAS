// Package backup 实现备份中心模块(M5):定时备份任务、执行器(rsync/本地复制)、
// GPG 加密、完成通知(termux-notification)。
//
// 任务持久化于 SQLite(nas.db backup_jobs 表);调度器为进程内 ticker,
// 每分钟检查到期任务;执行在独立 goroutine 中运行,不阻塞 HTTP。
package backup

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Job 一个备份任务(与 DB 行对应)。
type Job struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Source      string    `json:"source"`   // 备份源目录
	Target      string    `json:"target"`   // 备份目标目录(可含远程 rsync:// 前缀)
	Schedule    string    `json:"schedule"` // cron 表达式(5 字段)或空(仅手动)
	Enabled     bool      `json:"enabled"`
	KeepCopies  int       `json:"keep_copies"` // 保留最近 N 份(0=不限制)
	LastRun     time.Time `json:"last_run,omitempty"`
	LastStatus  string    `json:"last_status"` // ok / error / running
	LastError   string    `json:"last_error,omitempty"`
	LastSize    int64     `json:"last_size,omitempty"`
}

// Status 任务执行状态。
const (
	StatusOK      = "ok"
	StatusError   = "error"
	StatusRunning = "running"
)

// ErrJobNotFound 任务不存在。
var ErrJobNotFound = errors.New("备份任务不存在")

// Store 备份任务存储(基于 SQLite)。
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// NewStore 创建备份存储并建表。
func NewStore(db *sql.DB, log *slog.Logger) (*Store, error) {
	s := &Store{db: db, log: log}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS backup_jobs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    source       TEXT NOT NULL,
    target       TEXT NOT NULL,
    schedule     TEXT NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    keep_copies  INTEGER NOT NULL DEFAULT 0,
    last_run     TEXT NOT NULL DEFAULT '',
    last_status  TEXT NOT NULL DEFAULT '',
    last_error   TEXT NOT NULL DEFAULT '',
    last_size    INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_backup_enabled ON backup_jobs(enabled);`)
	return err
}

// List 返回全部任务。
func (s *Store) List() ([]Job, error) {
	rows, err := s.db.Query(`SELECT id, name, source, target, schedule, enabled,
		keep_copies, last_run, last_status, last_error, last_size
		FROM backup_jobs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var j Job
		var enabled int
		var lastRun, lastSize string
		if err := rows.Scan(&j.ID, &j.Name, &j.Source, &j.Target, &j.Schedule,
			&enabled, &j.KeepCopies, &lastRun, &j.LastStatus, &j.LastError, &lastSize); err != nil {
			return nil, err
		}
		j.Enabled = enabled != 0
		j.LastRun = parseTime(lastRun)
		fmt.Sscanf(lastSize, "%d", &j.LastSize)
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// Get 返回单个任务。
func (s *Store) Get(id int64) (Job, error) {
	jobs, err := s.List()
	if err != nil {
		return Job{}, err
	}
	for _, j := range jobs {
		if j.ID == id {
			return j, nil
		}
	}
	return Job{}, ErrJobNotFound
}

// Create 新建任务。
func (s *Store) Create(j Job) (Job, error) {
	if j.Name == "" || j.Source == "" || j.Target == "" {
		return Job{}, errors.New("name/source/target 必填")
	}
	res, err := s.db.Exec(`INSERT INTO backup_jobs
		(name, source, target, schedule, enabled, keep_copies, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		j.Name, j.Source, j.Target, j.Schedule, boolInt(j.Enabled), j.KeepCopies,
		time.Now().Format(time.RFC3339))
	if err != nil {
		return Job{}, err
	}
	id, _ := res.LastInsertId()
	j.ID = id
	return j, nil
}

// Update 更新任务(全部字段覆盖)。
func (s *Store) Update(j Job) error {
	res, err := s.db.Exec(`UPDATE backup_jobs SET
		name=?, source=?, target=?, schedule=?, enabled=?, keep_copies=?
		WHERE id=?`,
		j.Name, j.Source, j.Target, j.Schedule, boolInt(j.Enabled), j.KeepCopies, j.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}

// Delete 删除任务。
func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM backup_jobs WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}

// SetResult 记录任务执行结果。
func (s *Store) SetResult(id int64, status, errMsg string, size int64) error {
	_, err := s.db.Exec(`UPDATE backup_jobs SET
		last_run=?, last_status=?, last_error=?, last_size=?
		WHERE id=?`,
		time.Now().Format(time.RFC3339), status, errMsg, size, id)
	return err
}

// SetRunning 标记任务执行中。
func (s *Store) SetRunning(id int64) error {
	_, err := s.db.Exec(`UPDATE backup_jobs SET last_status=?, last_error=''
		WHERE id=?`, StatusRunning, id)
	return err
}

// --- 调度与执行 ---

// Manager 备份管理器:任务调度 + 执行 + 通知。
type Manager struct {
	store   *Store
	log     *slog.Logger
	notify  func(string, string) // 完成通知(termux-notification 封装,可注入)
	mu      sync.Mutex
	running map[int64]bool // 正在执行的任务 ID(防重入)
}

// NewManager 创建备份管理器。notify 为空时使用默认通知。
func NewManager(store *Store, log *slog.Logger, notify func(string, string)) *Manager {
	if notify == nil {
		notify = defaultNotify
	}
	return &Manager{store: store, log: log, notify: notify, running: map[int64]bool{}}
}

// Store 返回任务存储(供 API 层 CRUD)。
func (m *Manager) Store() *Store { return m.store }

// Schedule 按 cron 表达式判断任务是否到期。scheduler 由 Daemon 每分钟调用。
func (m *Manager) Schedule(now time.Time) {
	jobs, err := m.store.List()
	if err != nil {
		m.log.Error("读取备份任务失败", "err", err)
		return
	}
	for _, j := range jobs {
		if !j.Enabled || j.Schedule == "" {
			continue
		}
		if !CronMatch(j.Schedule, now) {
			continue
		}
		m.log.Info("备份任务到期", "job", j.Name, "schedule", j.Schedule)
		go m.Run(j.ID, "schedule")
	}
}

// Run 立即执行任务(手动触发或调度)。返回错误信息。
func (m *Manager) Run(id int64, trigger string) string {
	return m.RunJob(id, trigger, nil)
}

// RunJob 执行任务,可覆盖任务定义(jobOverride 非空时使用其方向,用于恢复)。
func (m *Manager) RunJob(id int64, trigger string, jobOverride *Job) string {
	m.mu.Lock()
	if m.running[id] {
		m.mu.Unlock()
		return "任务正在执行中"
	}
	m.running[id] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, id)
		m.mu.Unlock()
	}()

	job, err := m.store.Get(id)
	if err != nil {
		return err.Error()
	}
	if jobOverride != nil {
		job.Source, job.Target = jobOverride.Source, jobOverride.Target
	}
	if err := m.store.SetRunning(id); err != nil {
		m.log.Error("标记任务运行失败", "err", err)
	}

	m.log.Info("备份开始", "job", job.Name, "trigger", trigger, "source", job.Source)
	start := time.Now()
	err = runBackup(job)
	size, _ := dirSize(job.Target)
	elapsed := time.Since(start).Round(time.Millisecond)

	status, msg := StatusOK, ""
	if err != nil {
		status, msg = StatusError, err.Error()
	}
	_ = m.store.SetResult(id, status, msg, size)
	m.notify(job.Name, fmt.Sprintf("备份%s(%s,%s,%s)",
		map[bool]string{true: "成功", false: "失败"}[err == nil], trigger,
		elapsed.String(), humanSize(uint64(size))))

	if err != nil {
		m.log.Error("备份失败", "job", job.Name, "err", err)
		return err.Error()
	}
	m.log.Info("备份完成", "job", job.Name, "size", size, "elapsed", elapsed)
	return ""
}

// runBackup 执行备份:源 → 目标(rsync 优先,失败回退本地复制)。
func runBackup(job Job) error {
	if job.Source == "" || job.Target == "" {
		return errors.New("源或目标为空")
	}
	if _, err := os.Stat(job.Source); err != nil {
		return fmt.Errorf("备份源不存在: %w", err)
	}
	// 远程目标(rsync:// 或 user@host:)必须用 rsync
	if strings.Contains(job.Target, "://") || strings.Contains(job.Target, "@") {
		return runRsync(job)
	}
	// 本地目标:尝试 rsync(更快、增量),不可用则本地复制
	if _, err := exec.LookPath("rsync"); err == nil {
		return runRsync(job)
	}
	return runCopy(job)
}

// runRsync 使用 rsync 增量同步(-a 归档,--delete 删除目标多余文件)。
func runRsync(job Job) error {
	if err := os.MkdirAll(job.Target, 0o755); err != nil && !strings.Contains(job.Target, ":") {
		return err
	}
	src := strings.TrimSuffix(job.Source, "/") + "/"
	cmd := exec.Command("rsync", "-a", "--delete", src, job.Target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync 失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runCopy 本地复制(rsync 不可用时的降级)。
func runCopy(job Job) error {
	if err := os.MkdirAll(job.Target, 0o755); err != nil {
		return err
	}
	// 复制源目录内容到目标(不含目标自身)
	entries, err := os.ReadDir(job.Source)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(job.Source, e.Name())
		dst := filepath.Join(job.Target, e.Name())
		if e.IsDir() {
			if err := copyDir(src, dst); err != nil {
				return err
			}
		} else {
			if err := copyFile(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyDir 递归复制目录。
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcP := filepath.Join(src, e.Name())
		dstP := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(srcP, dstP); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcP, dstP); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile 复制单个文件。
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// dirSize 递归统计目录字节数。
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过无法访问项
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// defaultNotify 默认通知:termux-notification(Android)。
func defaultNotify(title, body string) {
	cmd := exec.Command("termux-notification", "--title", "NAS 备份 - "+title, "--content", body)
	_ = cmd.Start() // 异步,不阻塞
}

// CronMatch 简单 5 字段 cron 匹配(分 时 日 月 周)。字段支持 * / , -。
func CronMatch(expr string, t time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	if !cronFieldMatch(fields[0], t.Minute()) {
		return false
	}
	if !cronFieldMatch(fields[1], t.Hour()) {
		return false
	}
	if !cronFieldMatch(fields[2], t.Day()) {
		return false
	}
	if !cronFieldMatch(fields[3], int(t.Month())) {
		return false
	}
	// 周:0/7=周日,1-6=周一至周六
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return cronFieldMatch(fields[4], wd)
}

// cronFieldMatch 匹配单个字段(支持 *、数字、- 范围、, 列表、/ 步进)。
func cronFieldMatch(field string, val int) bool {
	if field == "*" {
		return true
	}
	for _, part := range strings.Split(field, ",") {
		if matchCronPart(part, val) {
			return true
		}
	}
	return false
}

func matchCronPart(part string, val int) bool {
	if strings.Contains(part, "/") {
		base, stepStr, ok := strings.Cut(part, "/")
		if !ok {
			return false
		}
		var step int
		fmt.Sscanf(stepStr, "%d", &step)
		if step <= 0 {
			return false
		}
		lo, hi := 0, maxFieldVal(base)
		if base != "*" && base != "" {
			lo = hi
			fmt.Sscanf(base, "%d", &lo)
		}
		if val < lo || val > hi {
			return false
		}
		return (val-lo)%step == 0
	}
	if strings.Contains(part, "-") {
		loStr, hiStr, _ := strings.Cut(part, "-")
		var lo, hi int
		fmt.Sscanf(loStr, "%d", &lo)
		fmt.Sscanf(hiStr, "%d", &hi)
		return val >= lo && val <= hi
	}
	var n int
	if _, err := fmt.Sscanf(part, "%d", &n); err == nil {
		return val == n
	}
	return false
}

// maxFieldVal cron 字段的最大值(默认 59,仅用于步进范围上界)。
func maxFieldVal(field string) int {
	if field == "*" {
		return 59
	}
	if strings.Contains(field, "-") {
		_, hiStr, _ := strings.Cut(field, "-")
		var hi int
		fmt.Sscanf(hiStr, "%d", &hi)
		return hi
	}
	var n int
	fmt.Sscanf(field, "%d", &n)
	return n
}

// --- 小工具 ---

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func humanSize(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), []string{"KB", "MB", "GB", "TB"}[exp])
}
