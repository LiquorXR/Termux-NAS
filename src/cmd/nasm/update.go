package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/termux-nas/nas/internal/config"
	"github.com/termux-nas/nas/internal/mgmt"
)

// cmdUpdate 更新主框架 nasd(开发文档 §4.3):
//
//	nasm update [url|file] [期望版本]
//
// 流程:下载新版 → SHA256 校验 → daemon.enterUpdate(旧进程优雅退出)
// → 原子替换 → 重启 → 健康检查(失败回滚)。
func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.String("sha256", "", "期望 SHA256 校验和(可选,防篡改)")
	force := fs.Bool("f", false, "跳过版本号检查,强制更新")
	_ = fs.Parse(args)

	source := ""
	if fs.NArg() > 0 {
		source = fs.Arg(0)
	}
	wantVersion := ""
	if fs.NArg() > 1 {
		wantVersion = fs.Arg(1)
	}

	paths := config.Resolve(root)

	// ① 获取新版二进制到临时文件
	nasdBin := findBinary(paths.BinDir, "nasd")
	tmpNew := nasdBin + ".new"
	if err := fetchUpdate(source, tmpNew); err != nil {
		return err
	}
	defer os.Remove(tmpNew)

	// ② 校验:SHA256 + 版本
	if *check != "" {
		got, err := fileSHA256(tmpNew)
		if err != nil {
			return err
		}
		if !strings.EqualFold(got, strings.TrimSpace(*check)) {
			return fmt.Errorf("SHA256 校验失败:期望 %s,实际 %s", *check, got)
		}
		fmt.Println("SHA256 校验通过")
	}
	newVer, err := probeVersion(tmpNew)
	if err != nil {
		return fmt.Errorf("新版二进制无法执行(可能损坏): %w", err)
	}
	fmt.Printf("新版版本: %s\n", newVer)
	if wantVersion != "" && newVer != wantVersion {
		return fmt.Errorf("版本不匹配:期望 %s,实际 %s", wantVersion, newVer)
	}
	if !*force {
		cur, _ := probeVersion(nasdBin)
		if cur == newVer {
			fmt.Printf("当前已是最新版本 %s,跳过更新(用 -f 强制)\n", cur)
			return nil
		}
	}

	// ③ 通过管理通道进入更新模式(旧进程优雅退出)
	if err := enterUpdateMode(paths); err != nil {
		return fmt.Errorf("通知 nasd 进入更新模式失败: %w", err)
	}

	// ④ 原子替换:备份旧版 → 替换 → 失败回滚
	backup := nasdBin + ".bak"
	if err := atomicReplace(nasdBin, tmpNew, backup); err != nil {
		_ = cmdStart(nil) // 尽力恢复服务
		return err
	}

	// ⑤ 重启并健康检查
	fmt.Println("替换完成,重启 nasd...")
	if err := cmdStart(nil); err != nil {
		// 重启失败:回滚旧版本
		if rbErr := rollback(nasdBin, backup); rbErr != nil {
			return fmt.Errorf("重启失败且回滚失败: %v;回滚错误: %v", err, rbErr)
		}
		fmt.Printf("重启失败(%v),已回滚旧版本,尝试再次启动...\n", err)
		if err2 := cmdStart(nil); err2 != nil {
			return fmt.Errorf("回滚后重启仍失败: %v", err2)
		}
	}
	os.Remove(backup) // 更新成功,清理备份
	fmt.Println("nasd 更新完成:", newVer)
	return nil
}

// fetchUpdate 获取新版二进制:URL 下载或本地文件复制。
func fetchUpdate(source, dst string) error {
	if source == "" {
		return fmt.Errorf("缺少更新来源(URL 或本地文件路径)")
	}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		fmt.Println("下载更新包:", source)
		resp, err := http.Get(source)
		if err != nil {
			return fmt.Errorf("下载失败: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	// 本地文件
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("读取本地文件失败: %w", err)
	}
	return os.WriteFile(dst, data, 0o755)
}

// probeVersion 执行 `nasd -version` 探测版本(验证二进制可执行且版本可用)。
func probeVersion(bin string) (string, error) {
	cmd := exec.Command(bin, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("执行 %s -version 失败: %w", filepath.Base(bin), err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("版本输出为空")
	}
	return v, nil
}

// enterUpdateMode 调用管理通道 daemon.enterUpdate,等待旧进程退出。
func enterUpdateMode(paths config.Paths) error {
	client, err := mgmt.NewClient(paths.SockPath, mustToken(paths))
	if err != nil {
		return err
	}
	defer client.Close()
	var res map[string]bool
	if err := client.Call(mgmt.MethodEnterUpdate, nil, &res); err != nil {
		return err
	}
	fmt.Println("nasd 已确认进入更新模式,等待退出...")
	// 轮询等待进程退出
	for i := 0; i < 100; i++ {
		if _, err := mgmt.NewClient(paths.SockPath, mustToken(paths)); err != nil {
			_ = mgmt.Cleanup(paths.SockPath)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("等待 nasd 退出超时")
}

// atomicReplace 原子替换:旧版 → .bak,新版 → bin。
// Windows 上 os.Rename 不能覆盖已存在文件,需先移动旧文件。
func atomicReplace(bin, newBin, backup string) error {
	// 旧版备份
	if _, err := os.Stat(bin); err == nil {
		if err := os.Rename(bin, backup); err != nil {
			return fmt.Errorf("备份旧版本失败: %w", err)
		}
	}
	// 新版替换
	if err := os.Rename(newBin, bin); err != nil {
		// 回滚备份
		_ = os.Rename(backup, bin)
		return fmt.Errorf("替换新版本失败: %w", err)
	}
	return nil
}

// rollback 回滚:用 .bak 恢复原二进制。
func rollback(bin, backup string) error {
	_ = os.Remove(bin)
	if err := os.Rename(backup, bin); err != nil {
		return err
	}
	return nil
}

// fileSHA256 计算文件 SHA256。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// cmdSelfUpdate 更新 nasm 自身(与 update 流程一致,但二进制为 nasm)。
func cmdSelfUpdate(args []string) error {
	fs := flag.NewFlagSet("self-update", flag.ExitOnError)
	check := fs.String("sha256", "", "期望 SHA256 校验和")
	_ = fs.Parse(args)
	source := ""
	if fs.NArg() > 0 {
		source = fs.Arg(0)
	}
	paths := config.Resolve(root)
	nasmBin := findBinary(paths.BinDir, "nasm")
	tmpNew := nasmBin + ".new"
	if err := fetchUpdate(source, tmpNew); err != nil {
		return err
	}
	defer os.Remove(tmpNew)
	if *check != "" {
		got, err := fileSHA256(tmpNew)
		if err != nil {
			return err
		}
		if !strings.EqualFold(got, strings.TrimSpace(*check)) {
			return fmt.Errorf("SHA256 校验失败:期望 %s,实际 %s", *check, got)
		}
	}
	backup := nasmBin + ".bak"
	if err := atomicReplace(nasmBin, tmpNew, backup); err != nil {
		return err
	}
	os.Remove(backup)
	fmt.Println("nasm 自身更新完成(下次执行生效)")
	return nil
}
