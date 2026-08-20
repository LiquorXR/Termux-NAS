package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	p := Resolve("/nas")
	want := map[string]string{
		"Root":     "/nas",
		"BinDir":   "/nas/bin",
		"Plugins":  "/nas/plugins",
		"DataDir":  "/nas/data",
		"LogDir":   "/nas/data/logs",
		"RunDir":   "/nas/run",
		"FilesDir": "/nas/files",
		"DBFile":   "/nas/data/nas.db",
		"ConfFile": "/nas/data/config.json",
	}
	got := map[string]string{
		"Root":     p.Root, // Root 为传入原值,不经过 Join,不做分隔符转换
		"BinDir":   p.BinDir,
		"Plugins":  p.Plugins,
		"DataDir":  p.DataDir,
		"LogDir":   p.LogDir,
		"RunDir":   p.RunDir,
		"FilesDir": p.FilesDir,
		"DBFile":   p.DBFile,
		"ConfFile": p.ConfFile,
	}
	for field, w := range want {
		exp := w
		if field != "Root" {
			exp = filepath.FromSlash(w)
		}
		if got[field] != exp {
			t.Errorf("%s = %q,期望 %q", field, got[field], exp)
		}
	}
}

func TestLoadDefaultsAndRoundtrip(t *testing.T) {
	root := t.TempDir()
	p := Resolve(root)
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	// 首次加载:生成默认配置 + 随机管理 token
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 7531 || c.Host != "0.0.0.0" {
		t.Errorf("默认值不符: %+v", c)
	}
	// 再次加载:保持一致
	c2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c2.CreatedAt != c.CreatedAt {
		t.Error("重复加载不应改写已有配置")
	}
	// 修改字段并保存
	c.Port = 9000
	c.ForceHTTPS = true
	c.TrustProxy = true
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	c3, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c3.Port != 9000 || !c3.ForceHTTPS || !c3.TrustProxy {
		t.Errorf("保存后字段不符: %+v", c3)
	}
}

func TestLoadDefaultsMissingField(t *testing.T) {
	root := t.TempDir()
	p := Resolve(root)
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	// 写一份缺字段的配置(模拟旧版本)
	if err := os.WriteFile(p.ConfFile, []byte(`{"port":9000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "0.0.0.0" {
		t.Errorf("缺字段应回填默认值: %+v", c)
	}
	if c.Port != 9000 {
		t.Errorf("已有字段应保留: %d", c.Port)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	root := t.TempDir()
	p := Resolve(root)
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfFile, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("损坏的配置应报错")
	}
}

func TestDefaultRootEnv(t *testing.T) {
	t.Setenv("NAS_ROOT", "/tmp/nas-env")
	root, err := DefaultRoot()
	if err != nil || root != "/tmp/nas-env" {
		t.Errorf("应优先取 NAS_ROOT,得到 %q (%v)", root, err)
	}
}

func TestEnsureDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nas")
	p := Resolve(root)
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{p.BinDir, p.Plugins, p.DataDir, p.LogDir, p.RunDir, p.FilesDir} {
		fi, err := os.Stat(d)
		if err != nil || !fi.IsDir() {
			t.Errorf("目录 %s 应存在: %v", d, err)
		}
	}
}
