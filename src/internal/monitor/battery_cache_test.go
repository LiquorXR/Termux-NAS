package monitor

import (
	"testing"
	"time"
)

// TestBatteryCache 缓存合并:10s 窗口内多次调用只执行一次底层采集。
func TestBatteryCache(t *testing.T) {
	// 电池采集走 Termux 分支(PREFIX 环境判断),测试中模拟 Termux 环境。
	t.Setenv("PREFIX", "/data/data/com.termux/files/usr")
	calls := 0
	orig := batteryStatsUncached
	batteryStatsUncached = func() (Battery, bool) {
		calls++
		return Battery{Percentage: 66, Status: "Charging", Temperature: 30.5}, true
	}
	defer func() { batteryStatsUncached = orig }()

	// 重置缓存
	batteryMu.Lock()
	batteryCachedAt = time.Time{}
	batteryMu.Unlock()

	// 首次调用:执行底层采集
	if _, ok := batteryStats(); !ok {
		t.Fatal("首次采集应成功")
	}
	if calls != 1 {
		t.Fatalf("首次调用应执行 1 次采集,得到 %d", calls)
	}
	// 缓存命中:不再执行
	for i := 0; i < 5; i++ {
		b, ok := batteryStats()
		if !ok || b.Percentage != 66 {
			t.Fatalf("缓存应返回一致结果: %+v (%v)", b, ok)
		}
	}
	if calls != 1 {
		t.Fatalf("缓存窗口内应只采集 1 次,得到 %d", calls)
	}
	// 强制过期后重新采集
	batteryMu.Lock()
	batteryCachedAt = time.Now().Add(-batteryCacheTTL - time.Second)
	batteryMu.Unlock()
	batteryStats()
	if calls != 2 {
		t.Fatalf("缓存过期应重新采集,得到 %d", calls)
	}
}
