#!/usr/bin/env bash
# Termux NAS 构建脚本(仅 nasd 单一二进制)
# 用法:
#   ./scripts/build.sh              本机构建(nasd → ../bin/)
#   ./scripts/build.sh android     交叉编译 Termux(android/arm64)
#   ./scripts/build.sh android amd64 指定架构
set -euo pipefail

cd "$(dirname "$0")/.."          # src/
ROOT="$(pwd)"
OUT="$ROOT/../bin"               # ~/nas/bin
mkdir -p "$OUT"

VERSION="${VERSION:-0.1.0}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo dev)"
BUILD_TIME="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
LDFLAGS="-s -w -X github.com/termux-nas/nas/internal/version.Version=${VERSION} \
         -X github.com/termux-nas/nas/internal/version.Commit=${COMMIT} \
         -X github.com/termux-nas/nas/internal/version.BuildTime=${BUILD_TIME}"

TARGET="${1:-host}"
GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"

# Windows 需要 .exe 后缀才能被 exec 启动
BINEXT=""
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) BINEXT=".exe" ;;
esac

# Termux = Android 上的 Linux(arm64);CGO_ENABLED=0 静态编译,体积最小
case "$TARGET" in
  android)
    GOOS="android"; GOARCH="${2:-arm64}"; BINEXT="" ;;
  host)
    : ;;
  *)
    echo "未知目标: $TARGET (可用: host | android)" >&2; exit 1 ;;
esac

echo "==> 构建前端(Vite → internal/webui/dist,go:embed 打包)"
( cd "$ROOT/web" && npm ci && npm run build )

echo "==> 构建 target=${TARGET} GOOS=${GOOS} GOARCH=${GOARCH}"
rm -f "$OUT"/nasd "$OUT"/nasd.exe
echo "    - nasd"
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
  go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/nasd${BINEXT}" "./cmd/nasd"

echo "==> 完成:"
ls -lh "$OUT"/nasd*
