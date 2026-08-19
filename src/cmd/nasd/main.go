// nasd · Termux NAS 主框架守护进程
//
// 职责:用户通道 HTTP(内建 NAS 功能)+ 管理通道(仅生命周期)。
// 生命周期由 nasm 通过 Unix socket 管理;插件由 nasd 全权控制(Web UI)。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/termux-nas/nas/internal/config"
	"github.com/termux-nas/nas/internal/daemon"
	"github.com/termux-nas/nas/internal/version"
)

func main() {
	flagRoot := flag.String("root", "", "部署根目录(默认 $NAS_ROOT 或 $HOME/nas)")
	flagDebug := flag.Bool("debug", false, "开启调试日志")
	flagVersion := flag.Bool("version", false, "输出版本信息后退出")
	flag.Parse()

	if *flagVersion {
		fmt.Println(version.String())
		return
	}

	root := *flagRoot
	if root == "" {
		r, err := config.DefaultRoot()
		if err != nil {
			fatal(err)
		}
		root = r
	}

	paths := config.Resolve(root)
	if err := config.EnsureDirs(paths); err != nil {
		fatal(err)
	}

	cfg, err := config.Load(paths)
	if err != nil {
		fatal(err)
	}

	logger, logFile, err := newLogger(paths, *flagDebug)
	if err != nil {
		fatal(err)
	}
	defer logFile.Close()

	d := daemon.New(cfg, paths, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("nasd 启动", "root", root, "debug", *flagDebug)
	if err := d.Run(ctx); err != nil {
		logger.Error("nasd 异常退出", "err", err)
		os.Exit(1)
	}
	logger.Info("nasd Run 已返回,进程即将退出")
}

// newLogger 日志同时输出到 stderr 与 data/logs/nasd.log。
func newLogger(paths config.Paths, debug bool) (*slog.Logger, io.WriteCloser, error) {
	logPath := filepath.Join(paths.LogDir, "nasd.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("打开日志文件 %s: %w", logPath, err)
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	w := io.MultiWriter(os.Stderr, f)
	logger := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
	return logger, f, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "nasd:", err)
	os.Exit(1)
}
