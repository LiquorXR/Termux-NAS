package backup

import (
	"testing"
	"time"
)

func BenchmarkCronMatch(b *testing.B) {
	exprs := []string{
		"* * * * *",
		"0 2 * * *",
		"*/15 * * * *",
		"0-30/10 5-23/3 * * 1-5",
		"30 14 19 8 *",
	}
	now := time.Date(2026, 8, 19, 14, 30, 0, 0, time.Local)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, e := range exprs {
			_ = CronMatch(e, now)
		}
	}
}
