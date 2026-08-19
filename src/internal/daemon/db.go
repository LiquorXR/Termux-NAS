package daemon

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动,CGO_ENABLED=0 可编译,适配 Termux
)

// openDB 打开 SQLite(WAL 模式)并执行初始化迁移。
func (d *Daemon) openDB() error {
	db, err := sql.Open("sqlite", d.paths.DBFile)
	if err != nil {
		return fmt.Errorf("打开 SQLite: %w", err)
	}
	// SQLite 单写者,限制连接数并开启 WAL,提升手机端并发读体验。
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return fmt.Errorf("执行 PRAGMA %q: %w", pragma, err)
		}
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return err
	}
	d.db = db
	return nil
}

func (d *Daemon) closeDB() {
	if d.db != nil {
		_ = d.db.Close()
	}
}

// migrate 顺序执行迁移,版本记录于 meta.schema_version。
// 新增 schema 变更时:在 migrations 末尾追加新版本迁移函数。
func migrate(db *sql.DB) error {
	// meta 表是所有迁移的前提
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("创建 meta 表: %w", err)
	}
	if _, err := db.Exec(
		"INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', '0')"); err != nil {
		return fmt.Errorf("写入 schema_version: %w", err)
	}

	var cur int
	if err := db.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&cur); err != nil {
		return fmt.Errorf("读取 schema_version: %w", err)
	}
	if cur < 0 {
		cur = 0
	}

	for _, m := range migrations {
		if m.version <= cur {
			continue
		}
		if err := m.up(db); err != nil {
			return fmt.Errorf("迁移 v%d: %w", m.version, err)
		}
		if _, err := db.Exec(
			"UPDATE meta SET value = ? WHERE key = 'schema_version'", strconv.Itoa(m.version)); err != nil {
			return fmt.Errorf("更新 schema_version: %w", err)
		}
	}

	// 记录本次启动的 nasd 版本
	if _, err := db.Exec(
		"INSERT INTO meta(key, value) VALUES ('nasd_version', ?) "+
			"ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		versionString()); err != nil {
		return fmt.Errorf("写入 nasd_version: %w", err)
	}
	if _, err := db.Exec(
		"INSERT INTO meta(key, value) VALUES ('last_start', ?) "+
			"ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("写入 last_start: %w", err)
	}
	return nil
}

// migration 单次 schema 变更。
type migration struct {
	version int
	up      func(*sql.DB) error
}

// migrations 顺序迁移表(M1+ 历史迁移):
//   - v1: M1 骨架,meta 表(幂等兜底)
//   - v2: M2 认证中心,users / sessions 表
//   - v3: M3 文件管理,shares 分享链接表
var migrations = []migration{
	{1, migrateV1},
	{2, migrateV2},
	{3, migrateV3},
}

// migrateV1 M1:meta 表(迁移框架本身已兜底创建,保留幂等)。
func migrateV1(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
)`)
	return err
}

// migrateV2 M2 认证中心:用户与会话表。
func migrateV2(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);`)
	return err
}

// migrateV3 M3 文件管理:分享链接表。
func migrateV3(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS shares (
    token      TEXT PRIMARY KEY,
    path       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_shares_expires ON shares(expires_at);`)
	return err
}
