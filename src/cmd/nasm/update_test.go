package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testRoot 创建临时部署根。
func testRoot(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root = dir
	return dir, binDir
}

// writeFakeBinary 生成可被 probeVersion 执行的假二进制。
// Windows 上直接生成 .cmd 批处理;Unix 生成 shell 脚本。
func writeFakeBinary(t *testing.T, path, ver string) string {
	t.Helper()
	var content string
	if runtime.GOOS == "windows" {
		// .cmd 无法直接 exec(需要 cmd /c);改用复制当前测试二进制?不可行。
		// 使用 go 生成的真实小二进制成本高;此处仅测文件操作逻辑,
		// probeVersion 相关由冒烟测试覆盖。
		content = "dummy"
	} else {
		content = "#!/bin/sh\necho " + ver + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAtomicReplaceAndRollback(t *testing.T) {
	_, binDir := testRoot(t)
	bin := filepath.Join(binDir, "nasd")
	backup := bin + ".bak"

	// 旧版存在
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBin := bin + ".new"
	if err := os.WriteFile(newBin, []byte("new-content"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 原子替换
	if err := atomicReplace(bin, newBin, backup); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(bin)
	if string(got) != "new-content" {
		t.Errorf("替换后 bin 内容 = %q,期望 new-content", got)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Error("旧版本应备份为 .bak")
	}
	if _, err := os.Stat(newBin); !os.IsNotExist(err) {
		t.Error("新临时文件应已移除")
	}

	// 回滚
	if err := rollback(bin, backup); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(bin)
	if string(got) != "old" {
		t.Errorf("回滚后 bin 内容 = %q,期望 old", got)
	}
}

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sha256("abc") = ba7816bf...
	got, err := fileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") {
		t.Errorf("SHA256 错误: %s", got)
	}
}

func TestFetchUpdateLocalFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fetchUpdate(src, dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "payload" {
		t.Errorf("本地复制内容错误: %q", got)
	}
}

func TestFetchUpdateMissingSource(t *testing.T) {
	dir := t.TempDir()
	if err := fetchUpdate("", filepath.Join(dir, "x")); err == nil {
		t.Fatal("缺少来源应报错")
	}
}
