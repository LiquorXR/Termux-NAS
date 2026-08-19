// nasm · Termux NAS 管理模块(CLI)
//
// 职责边界(开发文档 §4):仅管理主框架 nasd 的生命周期
// (start/stop/restart/status/log/update)。不管理插件——
// 插件的一切操作由 nasd 在 Web UI「插件管理」页中控制。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/termux-nas/nas/internal/config"
	"github.com/termux-nas/nas/internal/mgmt"
	"github.com/termux-nas/nas/internal/version"
)

var root string // 全局部署根(所有子命令共享)

func main() {
	// 预处理全局 -root:允许出现在任意位置(如 nasm -root X status / nasm status -root X)
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-root" && i+1 < len(args) {
			root = args[i+1]
			args = append(args[:i], args[i+2:]...)
			i--
		}
	}
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}

	cmd := args[0]
	rest := args[1:]

	// 子命令自身参数在 runXxx 中二次解析
	if root == "" {
		r, err := config.DefaultRoot()
		if err != nil {
			fatal(err)
		}
		root = r
	}

	var err error
	switch cmd {
	case "start":
		err = cmdStart(rest)
	case "stop":
		err = cmdStop(rest)
	case "restart":
		err = cmdRestart(rest)
	case "status":
		err = cmdStatus(rest)
	case "log":
		err = cmdLog(rest)
	case "update":
		err = cmdUpdate(rest)
	case "self-update":
		err = cmdSelfUpdate(rest)
	case "version", "-v", "--version":
		fmt.Println("nasm", version.String())
		return
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
}

// --- 子命令实现 ---

// cmdStart 启动 nasd。
// Termux 环境优先走 runit(sv);否则直接后台拉起进程。
func cmdStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	_ = fs.Parse(args)

	paths := config.Resolve(root)

	// 已在运行?
	client, err := mgmt.NewClient(paths.SockPath, mustToken(paths))
	if err == nil {
		var st mgmt.StatusResult
		if err := client.Call(mgmt.MethodStatus, nil, &st); err != nil {
			client.Close()
			return fmt.Errorf("管理通道已连通但状态查询失败(管理 token 不匹配?): %w", err)
		}
		client.Close()
		return fmt.Errorf("nasd 已在运行(pid=%d, version=%s)", st.PID, st.Version)
	}
	// 连接失败:清理上次残留的管理通道文件(死进程遗留的 addr/socket)
	_ = mgmt.Cleanup(paths.SockPath)

	nasdBin := findBinary(paths.BinDir, "nasd")
	if _, err := os.Stat(nasdBin); err != nil {
		return fmt.Errorf("未找到 nasd 二进制: %s(请先构建: make build)", nasdBin)
	}

	if err := config.EnsureDirs(paths); err != nil {
		return err
	}

	// Termux(runit)优先
	if inTermux() {
		svcDir := filepath.Join(os.Getenv("PREFIX"), "var", "service", "nasd")
		if _, err := os.Stat(filepath.Join(svcDir, "run")); err == nil {
			if out, err := exec.Command("sv", "start", "nasd").CombinedOutput(); err != nil {
				return fmt.Errorf("sv start 失败: %v\n%s", err, out)
			}
			fmt.Println("nasd 已通过 runit(sv)启动")
			return waitReady(paths)
		}
		fmt.Println("提示:未检测到 runit 服务,改用后台进程方式启动(建议安装 termux-services 并 sv-enable)")
	}

	// 后台拉起。nasd 内部已把日志写入 data/logs/nasd.log,
	// 此处不重定向子进程输出(避免与内部文件日志重复),异常时提示查看日志。
	cmd := exec.Command(nasdBin, "-root", root)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 nasd: %w", err)
	}
	fmt.Printf("nasd 已后台启动(pid=%d)\n", cmd.Process.Pid)
	return waitReady(paths)
}

// cmdStop 优雅停止 nasd。
func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	_ = fs.Parse(args)

	paths := config.Resolve(root)
	client, err := mgmt.NewClient(paths.SockPath, mustToken(paths))
	if err != nil {
		_ = mgmt.Cleanup(paths.SockPath)
		return fmt.Errorf("nasd 未在运行: %w", err)
	}
	defer client.Close()

	var res map[string]bool
	if err := client.Call(mgmt.MethodStop, nil, &res); err != nil {
		return fmt.Errorf("停止指令失败: %w", err)
	}
	fmt.Println("停止指令已发送,等待 nasd 退出...")

	// 轮询等待 socket 关闭,退出后清理残留文件
	for i := 0; i < 50; i++ {
		c, err := mgmt.NewClient(paths.SockPath, mustToken(paths))
		if err != nil {
			_ = mgmt.Cleanup(paths.SockPath)
			fmt.Println("nasd 已停止")
			return nil
		}
		c.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("等待超时,请检查 data/logs/nasd.log")
}

// cmdRestart 重启 nasd。
func cmdRestart(args []string) error {
	if err := cmdStop(nil); err != nil && !strings.Contains(err.Error(), "未在运行") {
		return err
	}
	// 等待进程完全退出
	paths := config.Resolve(root)
	for i := 0; i < 50; i++ {
		if _, err := mgmt.NewClient(paths.SockPath, mustToken(paths)); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cmdStart(nil)
}

// cmdStatus 查询 nasd 运行状态。
func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "输出 JSON")
	_ = fs.Parse(args)

	paths := config.Resolve(root)
	client, err := mgmt.NewClient(paths.SockPath, mustToken(paths))
	if err != nil {
		fmt.Println("nasd: 未运行")
		return nil
	}
	defer client.Close()

	var st mgmt.StatusResult
	if err := client.Call(mgmt.MethodStatus, nil, &st); err != nil {
		return err
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	state := "运行中"
	if !st.Healthy {
		state = "异常"
	}
	fmt.Printf("nasd:   %s\n", state)
	fmt.Printf("版本:   %s\n", st.Version)
	fmt.Printf("PID:    %d\n", st.PID)
	fmt.Printf("Uptime: %s\n", (time.Duration(st.Uptime) * time.Second).Round(time.Second))
	fmt.Printf("端口:   %d\n", st.Port)
	return nil
}

// cmdLog 查看主框架日志尾部。
func cmdLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	lines := fs.Int("n", 50, "行数")
	_ = fs.Parse(args)

	paths := config.Resolve(root)
	client, err := mgmt.NewClient(paths.SockPath, mustToken(paths))
	if err != nil {
		return fmt.Errorf("nasd 未在运行: %w", err)
	}
	defer client.Close()

	var res mgmt.LogTailResult
	if err := client.Call(mgmt.MethodLogTail, mgmt.LogTailParams{Lines: *lines}, &res); err != nil {
		return err
	}
	for _, l := range res.Lines {
		fmt.Println(l)
	}
	return nil
}

// cmdUpdate 更新主框架 nasd(M1 占位,M6 实现下载+校验+原子替换)。
func cmdUpdate(args []string) error {
	_ = flag.NewFlagSet("update", flag.ExitOnError)
	return fmt.Errorf("nasm update 尚未实现(规划于 M6:下载校验 → daemon.enterUpdate → 原子替换)")
}

// cmdSelfUpdate 更新 nasm 自身(M1 占位)。
func cmdSelfUpdate(args []string) error {
	_ = flag.NewFlagSet("self-update", flag.ExitOnError)
	return fmt.Errorf("nasm self-update 尚未实现(规划于 M6)")
}

// --- 辅助 ---

// waitReady 轮询管理通道,等待 nasd 完成启动。
func waitReady(paths config.Paths) error {
	for i := 0; i < 100; i++ {
		c, err := mgmt.NewClient(paths.SockPath, mustToken(paths))
		if err == nil {
			c.Close()
			fmt.Println("nasd 已就绪")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("nasd 启动超时,请检查 data/logs/nasd.log")
}

// mustToken 读取管理 token;配置不存在时由 Load 生成(与 nasd 共享同一文件)。
func mustToken(paths config.Paths) string {
	cfg, err := config.Load(paths)
	if err != nil {
		return ""
	}
	return cfg.ManageToken
}

// findBinary 定位二进制,兼容 Windows(.exe 后缀)。
func findBinary(dir, name string) string {
	candidates := []string{name, name + ".exe"}
	if runtime.GOOS == "windows" {
		candidates = []string{name + ".exe", name}
	}
	for _, n := range candidates {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(dir, name)
}

// inTermux 判断是否运行在 Termux 环境。
func inTermux() bool {
	return os.Getenv("PREFIX") != ""
}

func usage() {
	fmt.Print(`Termux NAS 管理模块 (nasm)

用法:
  nasm start                启动 nasd 守护进程
  nasm stop                 优雅停止 nasd
  nasm restart              重启 nasd
  nasm status [-json]       查看运行状态(版本/uptime/健康)
  nasm log [-n 行数]         查看主框架日志尾部
  nasm update [version]     更新主框架 nasd(规划 M6)
  nasm self-update          更新 nasm 自身(规划 M6)
  nasm version              版本信息
  nasm help                 帮助

全局参数:
  -root <dir>  指定部署根(默认 $NAS_ROOT 或 $HOME/nas)

注意:插件管理不在 nasm 中——请在 Web UI「插件管理」页操作。
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "nasm:", err)
	os.Exit(1)
}
