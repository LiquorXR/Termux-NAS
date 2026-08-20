package backup

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestStore 内存 SQLite 测试存储。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := NewStore(db, logger)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreCRUD(t *testing.T) {
	s := newTestStore(t)
	// 创建
	j, err := s.Create(Job{Name: "照片", Source: "/a", Target: "/b", Schedule: "0 2 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	if j.ID == 0 {
		t.Fatal("ID 应为正数")
	}
	// 列表
	jobs, err := s.List()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("列表应有 1 条,得到 %d (%v)", len(jobs), err)
	}
	// 获取
	got, err := s.Get(j.ID)
	if err != nil || got.Name != "照片" {
		t.Fatalf("获取失败: %v %+v", err, got)
	}
	// 更新
	got.Schedule = "0 3 * * *"
	got.Enabled = false
	if err := s.Update(got); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.Get(j.ID)
	if got2.Schedule != "0 3 * * *" || got2.Enabled {
		t.Errorf("更新未生效: %+v", got2)
	}
	// 结果记录
	if err := s.SetResult(j.ID, StatusOK, "", 1024); err != nil {
		t.Fatal(err)
	}
	got3, _ := s.Get(j.ID)
	if got3.LastStatus != StatusOK || got3.LastSize != 1024 {
		t.Errorf("结果未记录: %+v", got3)
	}
	// 删除
	if err := s.Delete(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(j.ID); err != ErrJobNotFound {
		t.Errorf("删除后应返回 ErrJobNotFound,得到 %v", err)
	}
}

func TestStoreValidation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Job{Name: "", Source: "/a", Target: "/b"}); err == nil {
		t.Fatal("缺 name 应报错")
	}
	if _, err := s.Create(Job{Name: "x", Source: "", Target: "/b"}); err == nil {
		t.Fatal("缺 source 应报错")
	}
}

func TestCronMatch(t *testing.T) {
	base := time.Date(2026, 8, 19, 14, 30, 0, 0, time.Local)
	cases := []struct {
		expr string
		at   time.Time
		want bool
	}{
		{"* * * * *", base, true},     // 每分钟
		{"30 14 * * *", base, true},   // 每天 14:30
		{"0 14 * * *", base, false},   // 14:00 不是 14:30
		{"*/5 * * * *", base, true},   // 每 5 分钟(30%5=0)
		{"*/7 * * * *", base, false},  // 30%7!=0
		{"0,30 * * * *", base, true},  // 0 分或 30 分
		{"0-10 * * * *", base, false}, // 0-10 分
		{"30 14 19 8 *", base, true},  // 8 月 19 日 14:30
		{"30 14 * * 3", time.Date(2026, 8, 19, 14, 30, 0, 0, time.Local), true},  // 周三
		{"30 14 * * 2", time.Date(2026, 8, 19, 14, 30, 0, 0, time.Local), false}, // 周二不匹配
		{"bad expr", base, false},          // 非法
		{"30 14 * * * extra", base, false}, // 字段数不对
	}
	for _, c := range cases {
		if got := CronMatch(c.expr, c.at); got != c.want {
			t.Errorf("CronMatch(%q, %v) = %v,期望 %v", c.expr, c.at.Format("2006-01-02 15:04"), got, c.want)
		}
	}
}

// TestCronMatchStep 步进语义边界:范围+步进、"N/step" 延伸到字段上限。
func TestCronMatchStep(t *testing.T) {
	base := time.Date(2026, 8, 19, 14, 30, 0, 0, time.Local)
	cases := []struct {
		expr string
		at   time.Time
		want bool
	}{
		{"1-5/2 * * * *", base, false},                                            // 30 不在 1-5
		{"1-5/2 * * * *", time.Date(2026, 8, 19, 14, 3, 0, 0, time.Local), true},  // 3 ∈ {1,3,5}
		{"1-5/2 * * * *", time.Date(2026, 8, 19, 14, 4, 0, 0, time.Local), false}, // 4 ∉ {1,3,5}
		{"10/5 * * * *", time.Date(2026, 8, 19, 14, 30, 0, 0, time.Local), true},  // 30 = 10+4*5
		{"10/5 * * * *", time.Date(2026, 8, 19, 14, 55, 0, 0, time.Local), true},  // 55 = 10+9*5(延伸到 59)
		{"10/5 * * * *", time.Date(2026, 8, 19, 14, 56, 0, 0, time.Local), false}, // 56 超出步进序列
		{"0-30/10 * * * *", time.Date(2026, 8, 19, 14, 30, 0, 0, time.Local), true},
		{"0-30/10 * * * *", time.Date(2026, 8, 19, 14, 40, 0, 0, time.Local), false}, // 40 超出范围上界
		{"*/15 * * * *", time.Date(2026, 8, 19, 14, 45, 0, 0, time.Local), true},
		{"*/15 * * * *", time.Date(2026, 8, 19, 14, 50, 0, 0, time.Local), false},
		{"* * * * */2", time.Date(2026, 8, 20, 14, 30, 0, 0, time.Local), true},  // 周四=4 ∈ {0,2,4,6}
		{"* * * * */2", time.Date(2026, 8, 19, 14, 30, 0, 0, time.Local), false}, // 周三=3 ∉ {0,2,4,6}
		{"0 14 * * 7", time.Date(2026, 8, 23, 14, 0, 0, 0, time.Local), true},    // 周日=7
		{"bad/2 * * * *", base, false},
		{"5/x * * * *", base, false},
		{"1x * * * *", base, false},
		{"-1 * * * *", base, false},
	}
	for _, c := range cases {
		if got := CronMatch(c.expr, c.at); got != c.want {
			t.Errorf("CronMatch(%q, %v) = %v,期望 %v", c.expr, c.at.Format("2006-01-02 15:04"), got, c.want)
		}
	}
}

// TestCronMatchHourField 小时字段边界("N/step" 延伸至 23)。
func TestCronMatchHourField(t *testing.T) {
	cases := []struct {
		expr string
		at   time.Time
		want bool
	}{
		{"0 5/6 * * *", time.Date(2026, 8, 19, 5, 0, 0, 0, time.Local), true}, // 5,11,17,23
		{"0 5/6 * * *", time.Date(2026, 8, 19, 23, 0, 0, 0, time.Local), true},
		{"0 5/6 * * *", time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local), false},
		{"0 5/6 * * *", time.Date(2026, 8, 20, 5, 0, 0, 0, time.Local), true},
	}
	for _, c := range cases {
		if got := CronMatch(c.expr, c.at); got != c.want {
			t.Errorf("CronMatch(%q, %v) = %v,期望 %v", c.expr, c.at.Format("2006-01-02 15:04"), got, c.want)
		}
	}
}

func TestRunCopyBackup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runCopy(Job{Source: src, Target: dst})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.txt", "sub/b.txt"} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Errorf("复制后 %s 不存在: %v", f, err)
		}
	}
}

func TestRunBackupSourceMissing(t *testing.T) {
	dir := t.TempDir()
	err := runBackup(Job{Source: filepath.Join(dir, "nope"), Target: filepath.Join(dir, "dst")})
	if err == nil || !strings.Contains(err.Error(), "源不存在") {
		t.Fatalf("源不存在应报错,得到 %v", err)
	}
}

func TestManagerRun(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "x.txt"), []byte("data"), 0o644)

	notified := make(chan string, 1)
	m := NewManager(s, slog.New(slog.NewTextHandler(os.Stderr, nil)),
		func(title, body string) { notified <- title + "|" + body })
	j, _ := s.Create(Job{Name: "测试", Source: src, Target: dst})
	msg := m.Run(j.ID, "manual")
	if msg != "" {
		t.Fatalf("Run 应成功,得到: %s", msg)
	}
	select {
	case n := <-notified:
		if !strings.Contains(n, "成功") {
			t.Errorf("通知应含成功,得到 %s", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未收到完成通知")
	}
	// 结果已记录
	got, _ := s.Get(j.ID)
	if got.LastStatus != StatusOK {
		t.Errorf("状态应为 ok,得到 %s", got.LastStatus)
	}
}

func TestManagerRunConcurrent(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(src, 0o755)
	j, _ := s.Create(Job{Name: "并发", Source: src, Target: dst})
	m := NewManager(s, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)
	// 同一任务并发两次:第二次应提示执行中
	m.mu.Lock()
	m.running[j.ID] = true
	m.mu.Unlock()
	msg := m.Run(j.ID, "manual")
	if !strings.Contains(msg, "执行中") {
		t.Errorf("并发执行应提示执行中,得到 %q", msg)
	}
	m.mu.Lock()
	delete(m.running, j.ID)
	m.mu.Unlock()
}
