// Package config 负责部署根目录解析与 data/config.json 的读写。
//
// 单部署根约定(见开发文档 §3):~/nas/ 一个目录包含一切,备份 = 拷贝目录。
// 根目录可通过环境变量 NAS_ROOT 覆盖(便于开发调试与测试)。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// 部署根内的固定子目录名。
const (
	DirBin     = "bin"     // nasd 二进制
	DirPlugins = "plugins" // 插件二进制(可执行文件)
	DirData    = "data"    // 运行时数据:nas.db / config.json / logs/
	DirLogs    = "logs"    // data/logs
	DirRun     = "run"     // 管理 socket(运行时创建)
	DirFiles   = "files"   // 文件管理根目录(默认,可用 file_root 覆盖)
)

// Config 主框架配置,持久化于 data/config.json。
type Config struct {
	// Port 用户通道 HTTP 监听端口(高位端口,默认 7531)。
	Port int `json:"port"`
	// Host 用户通道监听地址(默认 0.0.0.0,局域网可访问)。
	Host string `json:"host,omitempty"`
	// FileRoot 文件管理根目录。为空时使用 <root>/files;
	// Termux 可设为 ~/storage/shared(Android 共享存储)。
	FileRoot string `json:"file_root,omitempty"`
	// PluginIdleTimeout 插件懒加载空闲回收秒数(M4 使用,预留)。
	PluginIdleTimeout int `json:"plugin_idle_timeout,omitempty"`
	// TrustProxy 信任 X-Forwarded-For 头(仅当 nasd 部署在可信反向代理之后;
	// 直连部署开启会被伪造头绕过登录限流)。
	TrustProxy bool `json:"trust_proxy,omitempty"`
	// ForceHTTPS 强制 HTTPS 语义:会话 cookie 加 Secure 标记。
	// 仅当通过 HTTPS 反向代理/隧道访问时开启,否则登录后将无法下发 cookie。
	ForceHTTPS bool `json:"force_https,omitempty"`
	// CreatedAt 配置首次生成时间。
	CreatedAt string `json:"created_at,omitempty"`
}

// Paths 派生的部署根路径集合(不入库)。
type Paths struct {
	Root     string // 部署根 ~/nas
	BinDir   string
	Plugins  string
	DataDir  string
	LogDir   string
	RunDir   string // run/ 存放单实例锁 run/nas.lock
	FilesDir string // 文件管理根目录(默认 <root>/files)
	DBFile   string // data/nas.db
	ConfFile string // data/config.json
}

// DefaultRoot 返回默认部署根:$NAS_ROOT 或 $HOME/nas。
func DefaultRoot() (string, error) {
	if r := os.Getenv("NAS_ROOT"); r != "" {
		return r, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定用户主目录: %w", err)
	}
	return filepath.Join(home, "nas"), nil
}

// Resolve 计算全部派生路径。
func Resolve(root string) Paths {
	return Paths{
		Root:     root,
		BinDir:   filepath.Join(root, DirBin),
		Plugins:  filepath.Join(root, DirPlugins),
		DataDir:  filepath.Join(root, DirData),
		LogDir:   filepath.Join(root, DirData, DirLogs),
		RunDir:   filepath.Join(root, DirRun),
		FilesDir: filepath.Join(root, DirFiles),
		DBFile:   filepath.Join(root, DirData, "nas.db"),
		ConfFile: filepath.Join(root, DirData, "config.json"),
	}
}

// EnsureDirs 创建部署根下全部目录。
func EnsureDirs(p Paths) error {
	for _, d := range []string{p.BinDir, p.Plugins, p.DataDir, p.LogDir, p.RunDir, p.FilesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s: %w", d, err)
		}
	}
	return nil
}

// Load 读取配置;文件不存在时生成默认配置(含随机管理 token)。
func Load(p Paths) (*Config, error) {
	data, err := os.ReadFile(p.ConfFile)
	if err == nil {
		var c Config
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("解析 %s: %w", p.ConfFile, err)
		}
		if c.Port == 0 {
			c.Port = 7531
		}
		if c.Host == "" {
			c.Host = "0.0.0.0"
		}
		return &c, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取 %s: %w", p.ConfFile, err)
	}
	// 首次初始化:生成默认配置
	c := &Config{Port: 7531, Host: "0.0.0.0", PluginIdleTimeout: 600}
	if err := Save(p, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Save 原子写配置(先写临时文件再 rename)。
func Save(p Paths, c *Config) error {
	c.CreatedAt = nowISO()
	buf, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.ConfFile + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("写配置临时文件: %w", err)
	}
	if err := os.Rename(tmp, p.ConfFile); err != nil {
		return fmt.Errorf("落盘配置: %w", err)
	}
	return nil
}
