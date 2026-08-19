package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// SessionTTL 会话有效期。
const SessionTTL = 7 * 24 * time.Hour

// 认证错误。
var (
	ErrUserExists = errors.New("用户名已存在")
	ErrBadCreds   = errors.New("用户名或密码错误")
	ErrNoSession  = errors.New("会话无效或已过期")
)

// User 认证用户。
type User struct {
	ID        int64
	Username  string
	CreatedAt time.Time
}

// Session 登录会话。
type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

// Store 认证数据访问(users / sessions 表,迁移 v2)。
type Store struct {
	db       *sql.DB
	log      *slog.Logger
	limiter  *loginLimiter // 登录失败限流(M5 安全加固)
}

// NewStore 创建认证存储。
func NewStore(db *sql.DB, log *slog.Logger) *Store {
	return &Store{db: db, log: log, limiter: newLoginLimiter()}
}

// HasUsers 是否存在用户(决定 /setup 是否可用)。
func (s *Store) HasUsers() (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return false, fmt.Errorf("查询用户数: %w", err)
	}
	return n > 0, nil
}

// CreateUser 创建用户(密码经 Argon2id 哈希后入库)。
func (s *Store) CreateUser(username, password string) error {
	username = strings.TrimSpace(username)
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO users(username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, hash, time.Now().Format(time.RFC3339))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrUserExists
		}
		return fmt.Errorf("创建用户: %w", err)
	}
	return nil
}

// Authenticate 校验用户名密码,成功返回用户。
func (s *Store) Authenticate(username, password string) (*User, error) {
	var u User
	var hash, createdAt string
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, created_at FROM users WHERE username = ?`,
		strings.TrimSpace(username)).Scan(&u.ID, &u.Username, &hash, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBadCreds
		}
		return nil, fmt.Errorf("查询用户: %w", err)
	}
	ok, err := verifyPassword(password, hash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrBadCreds
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &u, nil
}

// CreateSession 创建会话(32 字节随机 token)。
func (s *Store) CreateSession(userID int64) (*Session, error) {
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	sess := &Session{Token: token, UserID: userID, ExpiresAt: time.Now().Add(SessionTTL)}
	_, err = s.db.Exec(
		`INSERT INTO sessions(token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, time.Now().Format(time.RFC3339), sess.ExpiresAt.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("创建会话: %w", err)
	}
	return sess, nil
}

// GetSession 校验 token 并返回会话(含过期检查,过期自动清理)。
func (s *Store) GetSession(token string) (*Session, error) {
	var sess Session
	var expiresAt string
	err := s.db.QueryRow(
		`SELECT token, user_id, expires_at FROM sessions WHERE token = ?`, token).
		Scan(&sess.Token, &sess.UserID, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoSession
		}
		return nil, fmt.Errorf("查询会话: %w", err)
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("解析会话过期时间: %w", err)
	}
	if time.Now().After(t) {
		_ = s.DeleteSession(token)
		return nil, ErrNoSession
	}
	sess.ExpiresAt = t
	return &sess, nil
}

// UserByID 按 ID 取用户(供会话 → 用户映射)。
func (s *Store) UserByID(id int64) (*User, error) {
	var u User
	var createdAt string
	err := s.db.QueryRow(`SELECT id, username, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoSession
		}
		return nil, fmt.Errorf("查询用户: %w", err)
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &u, nil
}

// DeleteSession 删除会话(登出)。
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// randomToken 生成 n 字节随机 hex token。
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机 token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
