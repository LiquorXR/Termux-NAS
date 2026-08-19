// Package version 集中管理版本信息,由构建脚本通过 -ldflags 注入。
package version

// 构建时注入变量(见 scripts/build.sh 与 Makefile):
//
//	-ldflags "-X github.com/termux-nas/nas/internal/version.Version=$(VERSION) \
//	          -X github.com/termux-nas/nas/internal/version.Commit=$(COMMIT) \
//	          -X github.com/termux-nas/nas/internal/version.BuildTime=$(BUILD_TIME)"
var (
	// Version 语义化版本号
	Version = "0.1.0"
	// Commit git 提交短哈希(未注入时为 "dev")
	Commit = "dev"
	// BuildTime 构建时间(未注入时为 "unknown")
	BuildTime = "unknown"
)

// String 返回格式化版本串,供 CLI 与 /api/version 输出。
func String() string {
	return Version + " (" + Commit + " " + BuildTime + ")"
}
