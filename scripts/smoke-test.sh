#!/usr/bin/env bash
# nas.sh 冒烟测试(开发机/CI/Termux 均可)
#
# 主程序为单一二进制 nasd(nasm 已移除)。分两层:
#   A. 机制断言(所有 bash 环境可跑):目录结构 / 下载 / SHA256 校验门禁 /
#      .bak 备份 / 同版本跳过 / 强制替换 / 篡改拒绝 / 卸载保护与清理
#   B. 运行时断言(仅 Linux/WSL/Termux):start(后台化)/status/log/restart/stop、
#      运行中 update(优雅停止/重启/回滚)、健康端口
# 在非 Linux(如 Windows Git Bash)运行会跳过 B 并提示,不报错。
#
# 依赖: Go 工具链(构建测试产物)、bash、curl、sha256sum、cygpath(Linux 可无)
# 用法: bash scripts/smoke-test.sh
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"          # 仓库根
SMOKE="$ROOT/.smoke"
FAIL=0
OS="$(uname -s)"
IS_LINUX=0; case "$OS" in Linux) IS_LINUX=1 ;; esac

step() { printf '\n\033[36m==== %s ====\033[0m\n' "$*"; }
ok()   { printf '\033[32m  [✓] %s\033[0m\n' "$*"; }
bad()  { printf '\033[31m  [✗] %s\033[0m\n' "$*"; FAIL=1; }

if [ "$IS_LINUX" = "0" ]; then
  step "运行环境"
  printf '\033[33m [i]检测到非 Linux(%s):运行时断言(B 层)将跳过,仅验证机制(A 层)。\n\033[0m' "$OS"
  printf '\033[33m [i]完整测试请在 Termux / WSL2 / CI 的 Linux 下运行。\n\033[0m'
  pkill -f 'bin/nasd' >/dev/null 2>&1 || true
else
  "$ROOT/nas.sh" stop >/dev/null 2>&1 || true
  pkill -f 'bin/nasd' >/dev/null 2>&1 || true
fi
sleep 1
rm -rf "$SMOKE"
mkdir -p "$SMOKE/dist" "$SMOKE/nas" "$SMOKE/nas2"
DIST_URL_FILE="file://$(cygpath -m "$SMOKE/dist" 2>/dev/null || echo "$SMOKE/dist")"
TAMPER_URL_FILE=""

# ---------- 1. 构建测试二进制(linux/amd64,版本 9.9.9) ----------
step "构建测试产物(nasd v9.9.9)"
cd "$ROOT/src" || { bad "无法进入 src"; exit 1; }
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64
LDFLAGS="-s -w \
  -X github.com/termux-nas/nas/internal/version.Version=9.9.9 \
  -X github.com/termux-nas/nas/internal/version.Commit=smoketest \
  -X github.com/termux-nas/nas/internal/version.BuildTime=2025-01-01T00:00:00Z"
go build -trimpath -ldflags "$LDFLAGS" -o "$SMOKE/dist/nasd-android-arm64" ./cmd/nasd || { bad "构建 nasd 失败"; exit 1; }
( cd "$SMOKE/dist" && sha256sum * > sha256sums.txt ) || { bad "生成校验和失败"; exit 1; }
ok "构建完成"

# ---------- 2. 机制: install(建目录/下载/校验/放置) ----------
step "机制: install 建目录/下载/校验/放置"
export NAS_ARCH=arm64 NAS_DIST_URL="$DIST_URL_FILE" NAS_ROOT="$SMOKE/nas"
bash "$ROOT/nas.sh" install || { bad "install 失败"; exit 1; }
for d in bin plugins data/logs run files; do
  [ -d "$NAS_ROOT/$d" ] || { bad "缺少目录 $d"; exit 1; }
done
ok "目录结构就绪(bin plugins data/logs run files)"
[ -s "$NAS_ROOT/bin/nasd" ] || { bad "缺少非空二进制 nasd"; exit 1; }
ok "nasd 已放置到 bin/(非空)"
if [ "$IS_LINUX" = "1" ]; then
  [ -x "$NAS_ROOT/bin/nasd" ] || { bad "缺少可执行位"; exit 1; }
  [ -n "$( "$NAS_ROOT/bin/nasd" -version 2>/dev/null )" ] || { bad "版本探测失败"; exit 1; }
  ok "nasd 可执行且版本可探测"
else
  echo "  [i] 非 Linux:跳过可执行位/运行探测(将在 Termux/WSL 校验)"
fi

# ---------- 3. 机制: 重装幂等(严格断言依赖 Linux) ----------
step "机制: 重装幂等(安装可重复执行)"
before_hash="$(sha256sum "$NAS_ROOT/bin/nasd" | awk '{print $1}')"
bash "$ROOT/nas.sh" install >/dev/null 2>&1 || { bad "重装 install 失败"; exit 1; }
after_hash="$(sha256sum "$NAS_ROOT/bin/nasd" | awk '{print $1}')"
[ "$before_hash" = "$after_hash" ] || { bad "重装后二进制哈希变化"; exit 1; }
ok "重装幂等(exit 0 且产物仍在)"
if [ "$IS_LINUX" = "1" ]; then
  [ -f "$NAS_ROOT/bin/nasd.bak" ] && { bad "同版本跳过不应产生 .bak"; exit 1; } || ok "同版本跳过,未产生 .bak"
else
  echo "  [i] 非 Linux:跳过‘同版本跳过’严格断言(exec 位模拟限制)"
fi

# ---------- 4. 机制: update 幂等 → update -f 强制替换(.bak) ----------
step "机制: update 幂等 → update -f 强制替换并生成 .bak"
bash "$ROOT/nas.sh" update || { bad "update 失败"; exit 1; }
[ -s "$NAS_ROOT/bin/nasd" ] || { bad "update 后丢失 nasd"; exit 1; }
ok "update 可重复执行(exit 0,产物完好)"
if [ "$IS_LINUX" = "1" ]; then
  [ -f "$NAS_ROOT/bin/nasd.bak" ] && { bad "同版本 update 不应产生 .bak"; exit 1; } || ok "同版本 update 跳过,无 .bak(版本比较生效)"
else
  echo "  [i] 非 Linux:跳过‘同版本 update 跳过’严格断言"
fi
rm -f "$NAS_ROOT/bin/nasd.bak"
bash "$ROOT/nas.sh" update -f || { bad "update -f 失败"; exit 1; }
[ -f "$NAS_ROOT/bin/nasd.bak" ] || { bad "-f 后缺少 .bak 备份"; exit 1; }
ok "强制替换后 .bak 备份存在"
[ -s "$NAS_ROOT/bin/nasd" ] || { bad "-f 替换后 nasd 缺失"; exit 1; }
ok "-f 替换后 nasd 完好"

# ---------- 5. 机制: SHA256 篡改必须拒绝且无副作用 ----------
step "机制: 校验和篡改防护"
TAMPER="$SMOKE/tamper"; mkdir -p "$TAMPER"
cp "$SMOKE/dist/nasd-android-arm64" "$TAMPER/"
printf '%064d  nasd-android-arm64\n' 0 > "$TAMPER/sha256sums.txt"
TAMPER_URL_FILE="file://$(cygpath -m "$TAMPER" 2>/dev/null || echo "$TAMPER")"
export NAS_ROOT="$SMOKE/nas2" NAS_DIST_URL="$TAMPER_URL_FILE"
if bash "$ROOT/nas.sh" install >/dev/null 2>&1; then bad "篡改校验和竟被接受"; exit 1; else ok "篡改校验和已被拒绝"; fi
[ -e "$NAS_ROOT/bin" ] && [ -n "$(ls -A "$NAS_ROOT"/bin 2>/dev/null)" ] && { bad "拒绝后仍有二进制落盘"; exit 1; }
ok "拒绝后无副作用(bin/ 为空)"

# ---------- 6. 机制: uninstall 需 -y 且清理 ----------
step "机制: uninstall 保护与清理"
export NAS_ROOT="$SMOKE/nas"
if bash "$ROOT/nas.sh" uninstall >/dev/null 2>&1; then bad "无 -y 竟然执行卸载"; exit 1; else ok "无 -y 被拒绝"; fi
[ -d "$NAS_ROOT" ] || { bad "拒绝卸载后目录不应被删除"; exit 1; }
bash "$ROOT/nas.sh" uninstall -y >/dev/null 2>&1 || { bad "uninstall -y 失败"; exit 1; }
[ -e "$NAS_ROOT" ] && { bad "卸载后残留"; exit 1; }
ok "uninstall -y 清理完成"

# ---------- 7. 运行时(B 层,仅 Linux) ----------
if [ "$IS_LINUX" = "1" ]; then
  wait_nasd_up() {
    local i
    for i in $(seq 1 40); do
      curl -sf -o /dev/null "http://127.0.0.1:7531/health" && return 0
      sleep 0.5
    done
    return 1
  }

  step "运行时: start → status → log → health → restart → stop"
  export NAS_DIST_URL="$DIST_URL_FILE" NAS_ROOT="$SMOKE/nas"
  bash "$ROOT/nas.sh" install >/dev/null 2>&1 || { bad "重新 install 失败"; exit 1; }
  # start 为前台阻塞命令:置于后台运行,轮询健康端口确认就绪
  nohup bash "$ROOT/nas.sh" start >/dev/null 2>&1 &
  wait_nasd_up || { bad "start 后 health 未就绪"; exit 1; }
  bash "$ROOT/nas.sh" status || { bad "status 失败"; exit 1; }
  bash "$ROOT/nas.sh" log -n 5 || { bad "log 失败"; exit 1; }
  curl -sf -o /dev/null "http://127.0.0.1:7531/health" || { bad "health 探活失败"; exit 1; }
  ok "start/status/log/health 正常"
  nohup bash "$ROOT/nas.sh" restart >/dev/null 2>&1 &
  wait_nasd_up || { bad "restart 后 health 未就绪"; exit 1; }
  bash "$ROOT/nas.sh" status >/dev/null || { bad "restart 后 status 失败"; exit 1; }
  ok "restart 正常"

  step "运行时: 运行中 update -f(优雅停止→替换→重启)"
  nohup bash "$ROOT/nas.sh" update -f >/dev/null 2>&1 &
  wait_nasd_up || { bad "更新后 health 未就绪"; exit 1; }
  bash "$ROOT/nas.sh" status >/dev/null || { bad "更新后状态异常"; exit 1; }
  [ -f "$NAS_ROOT/bin/nasd.bak" ] || { bad "运行中更新未留下 .bak"; exit 1; }
  ok "运行中更新正常(停止+替换+重启,保留 .bak)"

  bash "$ROOT/nas.sh" stop || { bad "stop 失败"; exit 1; }
  sleep 1
  bash "$ROOT/nas.sh" status >/dev/null && true
  ok "stop 正常"
else
  step "运行时(B 层)"
  echo "  [i] 非 Linux 环境,跳过 start/status/log/update 运行时断言。"
  echo "      请在 Termux 或 WSL2 运行本脚本以覆盖 B 层。"
fi

# ---------- 汇总 ----------
step "结果"
if [ "$FAIL" = "0" ]; then echo "SMOKE_ALL_OK"; else echo "SMOKE_HAS_FAILURES"; exit 1; fi