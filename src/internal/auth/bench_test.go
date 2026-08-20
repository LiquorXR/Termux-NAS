package auth

import (
	"testing"
)

func BenchmarkHashPassword(b *testing.B) {
	for i := 0; i < b.N; i++ {
		h, err := hashPassword("benchmark-password")
		if err != nil {
			b.Fatal(err)
		}
		_ = h
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	h, err := hashPassword("benchmark-password")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ok, err := verifyPassword("benchmark-password", h)
		if err != nil || !ok {
			b.Fatal("验证失败")
		}
	}
}

func BenchmarkRateLimit(b *testing.B) {
	l := newLoginLimiter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.allow(testCtx(b))
	}
}
