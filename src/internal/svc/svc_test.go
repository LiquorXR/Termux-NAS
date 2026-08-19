package svc

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

// fakeRunner 可编程假执行器,捕获调用并返回预设输出。
type fakeRunner struct {
	outputs map[string]string // 命令签名 → 输出
	errs    map[string]error
	calls   []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if err, ok := f.errs[key]; ok {
		return "", err
	}
	return f.outputs[key], nil
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
}

func testCtl(r Runner) *Controller {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	return New([]Service{{Name: "sshd", DisplayName: "SSH"}}, r, logger)
}

func TestListParsesRunning(t *testing.T) {
	r := newFakeRunner()
	r.outputs["sv status sshd"] = "run: sshd: (pid 1234) 45s"
	ctl := testCtl(r)
	infos := ctl.List(context.Background())
	if len(infos) != 1 {
		t.Fatalf("应返回 1 个服务,得到 %d", len(infos))
	}
	i := infos[0]
	if i.State != StatusRunning {
		t.Errorf("状态应为 running,得到 %s", i.State)
	}
	if i.Pid != 1234 {
		t.Errorf("PID 应为 1234,得到 %d", i.Pid)
	}
}

func TestListParsesDown(t *testing.T) {
	r := newFakeRunner()
	r.outputs["sv status sshd"] = "down: sshd: 60s, normally up"
	ctl := testCtl(r)
	infos := ctl.List(context.Background())
	if infos[0].State != StatusDown {
		t.Errorf("状态应为 down,得到 %s", infos[0].State)
	}
	if !infos[0].Autostart {
		t.Errorf("normally up 应视为自启")
	}
}

func TestStartStopRestart(t *testing.T) {
	r := newFakeRunner()
	ctl := testCtl(r)
	ctx := context.Background()
	if err := ctl.Start(ctx, "sshd"); err != nil {
		t.Fatal(err)
	}
	if err := ctl.Stop(ctx, "sshd"); err != nil {
		t.Fatal(err)
	}
	if err := ctl.Restart(ctx, "sshd"); err != nil {
		t.Fatal(err)
	}
	want := []string{"sv start sshd", "sv stop sshd", "sv restart sshd"}
	for i, w := range want {
		if r.calls[i] != w {
			t.Errorf("调用[%d] = %q,期望 %q", i, r.calls[i], w)
		}
	}
}

func TestSetAutostart(t *testing.T) {
	r := newFakeRunner()
	ctl := testCtl(r)
	ctx := context.Background()
	if err := ctl.SetAutostart(ctx, "sshd", true); err != nil {
		t.Fatal(err)
	}
	if r.calls[0] != "sv-enable sshd" {
		t.Errorf("启用自启应调用 sv-enable,得到 %q", r.calls[0])
	}
	if err := ctl.SetAutostart(ctx, "sshd", false); err != nil {
		t.Fatal(err)
	}
	if r.calls[1] != "sv-disable sshd" {
		t.Errorf("关闭自启应调用 sv-disable,得到 %q", r.calls[1])
	}
}

func TestGetUnknownService(t *testing.T) {
	r := newFakeRunner()
	ctl := testCtl(r)
	if _, err := ctl.Get(context.Background(), "ghost"); err == nil {
		t.Fatal("未知服务应报错")
	}
}

func TestParseSvStatus(t *testing.T) {
	cases := []struct {
		out   string
		state Status
		pid   int
	}{
		{"run: sshd: (pid 42) 10s", StatusRunning, 42},
		{"down: nginx: 5s, normally down", StatusDown, 0},
		{"something weird", StatusUnknown, 0},
	}
	for _, c := range cases {
		var info Info
		parseSvStatus(c.out, &info)
		if info.State != c.state {
			t.Errorf("parse(%q) 状态 = %s,期望 %s", c.out, info.State, c.state)
		}
		if info.Pid != c.pid {
			t.Errorf("parse(%q) PID = %d,期望 %d", c.out, info.Pid, c.pid)
		}
	}
}

// TestMockRunnerSmoke Windows 模拟器冒烟(任意平台可跑,验证假执行器自洽)。
func TestMockRunnerSmoke(t *testing.T) {
	r := &MockRunner{state: map[string]bool{}}
	ctx := context.Background()
	if _, err := r.Run(ctx, "sv", "start", "sshd"); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, "sv", "status", "sshd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "run:") {
		t.Errorf("start 后 status 应显示 run,得到 %q", out)
	}
	if _, err := r.Run(ctx, "sv-enable", "sshd"); err != nil {
		t.Fatal(err)
	}
}
