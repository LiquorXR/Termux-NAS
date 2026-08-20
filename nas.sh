#!/usr/bin/env bash
# =============================================================================
# nas.sh — Termux NAS 一键部署与管理脚本(仓库根目录)
#
# 主程序为单一二进制 nasd;本脚本提供交互式菜单与命令面:
#   菜单:启动(前台) / 安装更新 / 卸载 / 退出。
#
# 管理方式(无需任何 Go 管理工具):
#   - 启动:前台运行(nasd 作为本脚本子进程,父进程链 交互 shell→nas.sh→nasd 全存活,
#     天然避免 Android/ROM 对孤儿进程(PPID=1)的秒级回收;日志直打窗口,
#     Ctrl+C 优雅停止;脱离终端用 nohup bash nas.sh start &)
#   - 停止:SIGTERM 优雅停止(等进程退出,超时再 SIGKILL)
#   - 探活:HTTP /health(endpoint)+ 单实例锁 pid(run/nas.lock)
#   - 日志:直读 data/logs/nasd.log 尾部(前台运行时直接窗口打印)
#   - 更新:下载→SHA256 校验→优雅停止→原子替换(.bak)→重启→失败回滚
#
# 用法:
#   bash nas.sh [menu]               # 交互式菜单(默认)
#   bash nas.sh install              # 安装
#   bash nas.sh update [-f] [版本]    # 更新到最新(或指定 v<版本>)
#   bash nas.sh start | stop | restart | status | log [-n N]
#   bash nas.sh uninstall [-y]        # 卸载(默认只打印计划,需 -y 才删除数据)
#   bash nas.sh self-update           # 更新本脚本自身
#   bash nas.sh help | version        # 帮助 / 版本
#
# 环境变量(可覆盖):
#   NAS_ROOT      部署根,默认 $HOME/nas
#   NAS_REPO      GitHub 仓库,默认 LiquorXR/Termux-NAS
#   NAS_MIRROR    GitHub 加速镜像前缀,默认 https://ghfast.top/
#                 (置空则直连 github.com;国内网络建议保留默认加速)
#   NAS_DIST_URL  资产下载基地址(默认按 GitHub Releases 构造;
#                 镜像/本地测试时可指向自定义 URL/file folder,优先级最高)
#   NAS_ARCH      架构覆盖(默认按 uname -m 检测;开发机测试可设 arm64)
# =============================================================================
set -euo pipefail

NAS_SCRIPT_VERSION="3.0.0"
NAS_ROOT="${NAS_ROOT:-$HOME/nas}"
NAS_REPO="${NAS_REPO:-LiquorXR/Termux-NAS}"
NAS_MIRROR="${NAS_MIRROR:-https://ghfast.top/}"
PORT_DEFAULT=7531

# 资产命名(与 .github/workflows/release.yml 输出一致,勿改)
ASSET_NASD="nasd-android-arm64"
ASSET_SUMS="sha256sums.txt"

# 全局清理(EXIT 陷阱兜底:任何 die/中断都保证清理临时目录与操作锁)
NAS_TMP=""
NAS_LOCKDIR=""
NAS_START_PID=""   # 前台启动的 nasd 子进程 pid(供 block_until_exit 阻塞等待)
_nas_exit() {
  [ -n "$NAS_TMP" ]     && rm -rf "$NAS_TMP"
  [ -n "$NAS_LOCKDIR" ] && rmdir "$NAS_LOCKDIR" 2>/dev/null || true
}
trap _nas_exit EXIT

# ---------------- 输出辅助 ----------------
info() { printf '\033[32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33m警告:\033[0m %s\n' "$*" >&2; }
err()  { printf '\033[31m错误:\033[0m %s\n' "$*" >&2; exit 1; }
die()  { err "$*"; }

# ---------------- 基础检查 ----------------
require_cmds() {
  local missing=() c
  for c in curl sha256sum uname mktemp awk grep sed tail head tr; do
    command -v "$c" >/dev/null 2>&1 || missing+=("$c")
  done
  if [ "${#missing[@]}" -gt 0 ]; then
    die "缺少命令:${missing[*]}(Termux 下执行: pkg install curl)"
  fi
}

# 是否真实 Linux 系(含 Termux: uname -s 返回 Linux)。
# 用于区分“必须能执行 Linux 二进制”与仅做文件机制测试(如 Windows Git Bash)。
is_linux() { [ "$(uname -s)" = "Linux" ]; }

detect_arch() {
  local m="${NAS_ARCH:-$(uname -m)}"
  case "$m" in
    aarch64|arm64) NAS_ARCH="arm64" ;;
    *)
      die "不支持的架构: $m。当前仅发布 android/arm64 预编译二进制;\
请改用源码构建(见 README「源码构建」: make android)。"
      ;;
  esac
}

# ---------------- 路径与端口 ----------------
bin_path()    { printf '%s/bin/nasd' "$NAS_ROOT"; }
nasd_bin()    { bin_path; }
log_path()    { printf '%s/data/logs/nasd.log' "$NAS_ROOT"; }
lock_path()   { printf '%s/run/nas.lock' "$NAS_ROOT"; }

# 从 data/config.json 读取端口;未生成时用默认 7531
config_port() {
  local p
  p="$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' \
       "$NAS_ROOT/data/config.json" 2>/dev/null | head -n1)"
  printf '%s' "${p:-$PORT_DEFAULT}"
}

# ---------------- 目录结构 ----------------
# 与 internal/config 的部署根布局一致:bin plugins data/logs run files
ensure_dirs() {
  mkdir -p "$NAS_ROOT"/bin \
           "$NAS_ROOT"/plugins \
           "$NAS_ROOT"/data/logs \
           "$NAS_ROOT"/run \
           "$NAS_ROOT"/files
}

# ---------------- 下载与校验 ----------------
# 给 GitHub 直链加镜像前缀加速(NAS_MIRROR 置空则直连)。
# 保证前缀以 "/" 结尾再拼接,避免出现 "ghfast.tophttps://..." 的 URL 错误。
github_url() {
  local url="$1" m="${NAS_MIRROR:-}"
  if [ -n "$m" ]; then
    case "$m" in
      */) : ;;
      *) m="$m/" ;;
    esac
    printf '%s%s' "$m" "$url"
  else
    printf '%s' "$url"
  fi
}

# 构造资产下载基地址;channel 为 latest 或语义化版本号(不含 v)。
# NAS_DIST_URL 优先(镜像/本地测试用);否则默认走 ghfast.top 加速的 GitHub Releases。
release_base_url() {
  local channel="$1"
  if [ -n "${NAS_DIST_URL:-}" ]; then
    printf '%s' "$NAS_DIST_URL"
    return
  fi
  if [ "$channel" = "latest" ]; then
    github_url "https://github.com/$NAS_REPO/releases/latest/download"
  else
    github_url "https://github.com/$NAS_REPO/releases/download/v$channel"
  fi
}

download_assets() {
  local base="$1" tmp="$2" url a
  for a in "$ASSET_NASD" "$ASSET_SUMS"; do
    url="$base/$a"
    info "下载 $a"
    curl -fL --retry 3 --connect-timeout 20 --max-time 180 -o "$tmp/$a" "$url" \
      || die "下载失败: $url"
  done
}

verify_assets() {
  local tmp="$1"
  ( cd "$tmp" && sha256sum -c sha256sums.txt ) \
    || die "SHA256 校验失败,已中止(不安装任何未经验证的二进制)。\
请重试或检查网络;如为镜像问题请设置 NAS_DIST_URL。"
  info "SHA256 校验通过"
}

hash_for() {
  local tmp="$1" name="$2"
  awk -v n="$name" '$2 == n { print $1; exit }' "$tmp/sha256sums.txt"
}

# 探测 nasd 版本号(-version,语义化版本第一个字段)。
# 容错:无法执行时返回空串且不报错(非 Linux 开发环境无法运行 linux 产物);
# “必须能执行”的硬校验由调用方按 is_linux 显式执行。
probe_version() {
  local bin="$1" line
  line="$("$bin" -version 2>/dev/null || true)"
  printf '%s' "$line" | awk '{ print $1 }'
}

# ---------------- 运行状态:健康探活 + pid ----------------
health_up() {
  curl -sf -o /dev/null --max-time 3 "http://127.0.0.1:$(config_port)/health"
}

# 返回运行中 nasd 的 pid(每行一个)
nasd_pids() {
  local lp=""
  # 优先:单实例锁文件中的 pid(unix flock 由 nasd 写入)
  if [ -s "$(lock_path)" ]; then
    lp="$(head -n1 "$(lock_path)" 2>/dev/null || true)"
    case "$lp" in ''|*[!0-9]*) lp="" ;; esac
    if [ -n "$lp" ] && kill -0 "$lp" 2>/dev/null; then
      printf '%s\n' "$lp"
      return
    fi
  fi
  # 兜底:按命令行匹配(锚定二进制路径开头,排除外壳自身)
  pgrep -f "^$(nasd_bin)" 2>/dev/null || true
}

is_running() {
  health_up || [ -n "$(nasd_pids)" ]
}

# ---------------- 安装 ----------------
cmd_install() {
  ensure_dirs
  local tmp; tmp="$(mktemp -d)"; NAS_TMP="$tmp"

  local base; base="$(release_base_url latest)"
  download_assets "$base" "$tmp"
  verify_assets "$tmp"
  chmod +x "$tmp/$ASSET_NASD"

  local need_install=1
  if [ -s "$(nasd_bin)" ] && is_linux; then
    local old new
    old="$(probe_version "$(nasd_bin)")"
    new="$(probe_version "$tmp/$ASSET_NASD")"
    if [ -n "$old" ] && [ "$old" = "$new" ]; then
      info "已安装相同版本 $old,跳过替换(如需重装可先 uninstall)"
      need_install=0
    fi
  fi

  if [ "$need_install" = "1" ]; then
    if [ -f "$(nasd_bin)" ]; then
      info "保留旧版备份: nasd.bak"
      mv -f "$(nasd_bin)" "$(nasd_bin).bak"
    fi
    mv -f "$tmp/$ASSET_NASD" "$(nasd_bin)"
    chmod +x "$(nasd_bin)"
    if is_linux && [ -z "$(probe_version "$(nasd_bin)")" ]; then
      die "安装的 nasd 无法执行,请检查产物"
    fi
    local vd; vd="$(probe_version "$(nasd_bin)")"
    [ -n "$vd" ] && info "二进制已验证(nasd v${vd})"
  fi

  info "安装完成。部署根: $NAS_ROOT"
  info "下一步: bash nas.sh start;浏览器访问 http://<本机局域网IP>:$(config_port)"
  info "提示: 首次启动 nasd 自动生成 data/config.json(默认端口 $PORT_DEFAULT)。"
}

# ---------------- 生命周期 ----------------
# 优雅停止:SIGTERM → 等待进程退出 → 超时 SIGKILL
stop_nasd() {
  local pids remaining i
  pids="$(nasd_pids)"
  if [ -z "$pids" ]; then
    return 0
  fi
  info "优雅停止 nasd($(printf '%s' "$pids" | tr '\n' ' '))..."
  for p in $pids; do
    kill -TERM "$p" 2>/dev/null || true
  done
  for i in $(seq 1 120); do # 最长约 12s
    if [ -z "$(nasd_pids)" ]; then
      info "nasd 已停止"
      return 0
    fi
    sleep 0.1
  done
  remaining="$(nasd_pids)"
  warn "优雅停止超时,强制结束: $(printf '%s' "$remaining" | tr '\n' ' ')"
  for p in $remaining; do
    kill -KILL "$p" 2>/dev/null || true
  done
  sleep 1
}

# 等待健康就绪;超时返回非零
wait_ready() {
  local t="${1:-30}" i
  for i in $(seq 1 $((t * 2))); do
    if health_up; then return 0; fi
    sleep 0.5
  done
  return 1
}

# 前台启动 nasd:nasd 作为本脚本的子进程运行(父进程链 交互 shell→nas.sh→nasd
# 全部存活,天然避免 Android/ROM 对孤儿进程(PPID=1)的秒级回收)。
# 日志由 nasd 直接写 stderr → 窗口直打,同时落盘 data/logs/nasd.log。
cmd_start() {
  NAS_START_PID=""
  [ -s "$(nasd_bin)" ] || die "未安装 nasd,请先 bash nas.sh install"
  ensure_dirs
  if is_running; then
    info "nasd 已在运行(pid $(nasd_pids | head -n1)),无需重复启动"
    return 0
  fi
  info "启动 nasd..."
  "$(nasd_bin)" -root "$NAS_ROOT" &
  NAS_START_PID=$!
  info "nasd 已启动(pid $NAS_START_PID),日志打印本窗口"
}

# 前台阻塞:本进程保持为 nasd 的活父进程并等待其退出。
# Ctrl+C(SIGINT)/SIGTERM 转发给 nasd → nasd 优雅停止 → 本函数返回(回到菜单/命令结束)。
# 脱离终端常驻请用: nohup bash nas.sh start &(父链 交互 shell→nas.sh→nasd 需保持存活)。
block_until_exit() {
  local pid="${NAS_START_PID:-}"
  [ -n "$pid" ] || return 0
  trap 'kill -INT "$pid" 2>/dev/null || true' INT TERM
  printf '%s\n' "前台运行中… 日志直打本窗口;Ctrl+C 优雅停止;脱离终端用 nohup bash nas.sh start &"
  wait "$pid" || true
  trap - INT TERM
  info "nasd 已退出(pid $pid)"
}

cmd_stop()    { [ -s "$(nasd_bin)" ] || { info "nasd 未安装,无需停止"; return; }; stop_nasd; }
cmd_restart() { cmd_stop; cmd_start; }

cmd_status() {
  local port h v up pid
  port="$(config_port)"
  if health_up; then
    h="$(curl -sf --max-time 3 "http://127.0.0.1:$port/health")"
    v="$(printf '%s' "$h" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    up="$(printf '%s' "$h" | sed -n 's/.*"uptime"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p')"
    pid="$(printf '%s' "$h" | sed -n 's/.*"pid"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p')"
    echo "nasd:      运行中"
    echo "版本:      $v"
    echo "PID:       $pid"
    echo "Uptime:    ${up}s"
    echo "端口:      $port"
  else
    echo "nasd: 未运行"
    if [ -s "$(nasd_bin)" ] && is_linux; then
      echo "已安装: v$(probe_version "$(nasd_bin)")(bash nas.sh start 启动)"
    fi
  fi
}

cmd_log() {
  [ -s "$(nasd_bin)" ] || die "未安装 nasd,请先 bash nas.sh install"
  local n=50
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -n) n="${2:-50}"; shift 2 ;;
      *) shift ;;
    esac
  done
  tail -n "$n" "$(log_path)" 2>/dev/null || echo "(日志文件尚未生成)"
}

# ---------------- 更新 ----------------
cmd_update() {
  local channel="latest" force=0 a
  for a in "$@"; do
    case "$a" in
      -f|--force) force=1 ;;
      -h|--help)  die "用法: nas.sh update [-f] [版本号]" ;;
      *)          channel="$a" ;;
    esac
  done
  channel="${channel#v}"

  [ -s "$(nasd_bin)" ] || die "未安装 nasd,请先 bash nas.sh install"
  ensure_dirs

  local tmp; tmp="$(mktemp -d)"; NAS_TMP="$tmp"

  local base; base="$(release_base_url "$channel")"
  download_assets "$base" "$tmp"
  verify_assets "$tmp"
  chmod +x "$tmp/$ASSET_NASD"

  local new_ver old_ver=""
  new_ver="$(probe_version "$tmp/$ASSET_NASD")"
  # 真实 Linux(含 Termux)下,下载产物必须能执行(防损坏/架构不符)
  if [ -z "$new_ver" ] && is_linux; then
    die "下载的 nasd 无法执行(产物损坏或架构不符),已中止"
  fi
  if [ -s "$(nasd_bin)" ]; then old_ver="$(probe_version "$(nasd_bin)")"; fi

  if [ "$force" != "1" ] && [ -n "$new_ver" ] && [ -n "$old_ver" ] && [ "$new_ver" = "$old_ver" ]; then
    info "已是最新版本 $new_ver,跳过(如需强制覆盖: nas.sh update -f)"
    NAS_TMP=""; rm -rf "$tmp"
    return
  fi

  info "版本变更: ${old_ver:-未安装} → $new_ver"

  local was_running=0
  if is_running; then
    was_running=1
    # 运行中:先优雅停止,确保旧进程退出、单实例锁释放后再替换二进制
    stop_nasd
  else
    info "nasd 未运行,直接替换二进制..."
  fi

  if [ -f "$(nasd_bin)" ]; then
    mv -f "$(nasd_bin)" "$(nasd_bin).bak"
  fi
  mv -f "$tmp/$ASSET_NASD" "$(nasd_bin)"
  chmod +x "$(nasd_bin)"
  if is_linux && [ -z "$(probe_version "$(nasd_bin)")" ]; then
    die "替换后的 nasd 无法执行"
  fi
  info "二进制已替换(v$(probe_version "$(nasd_bin)"))"

  if [ "$was_running" = "1" ]; then
    # 原运行中 → 重启并健康检查;失败自动回滚。
    # 先输出完成信息与旧版保留提示,再前台阻塞启动,避免消息被阻塞在后台。
    if [ -f "$(nasd_bin).bak" ]; then
      info "旧版已保留: nasd.bak(如需回滚: 替换回 bin/nasd 后重启)"
    fi
    info "nasd 更新完成: v$(probe_version "$(nasd_bin)"),正在前台启动…"
    cmd_start
    if ! wait_ready 30; then
      warn "新版启动失败,回滚到 .bak 版本"
      stop_nasd
      [ -f "$(nasd_bin).bak" ] && mv -f "$(nasd_bin).bak" "$(nasd_bin)"
      chmod +x "$(nasd_bin)"
      cmd_start
      wait_ready 30 || die "回滚后仍无法启动,请查看日志: $(log_path)"
      die "已回滚到旧版本 v$(probe_version "$(nasd_bin)")"
    fi
  else
    info "更新完成(未启动),可执行 bash nas.sh start"
  fi
  NAS_TMP=""; rm -rf "$tmp"
}

# ---------------- 卸载 ----------------
cmd_uninstall() {
  local yes=0 service=0 a
  for a in "$@"; do
    case "$a" in
      -y|--yes) yes=1 ;;
      *) err "未知参数: $a(用法: nas.sh uninstall [-y])" ;;
    esac
  done

  echo "卸载计划:"
  echo "  1. 停止 nasd(如运行中)"
  echo "  2. 删除部署根 $NAS_ROOT(含 data 数据库、files、plugins、bin——不可恢复)"
  if [ "$yes" != "1" ]; then
    die "未确认卸载:请加 -y 确认执行(-y 将真正删除全部数据)"
  fi

  cmd_stop
  rm -rf "$NAS_ROOT"
  info "已删除 $NAS_ROOT"
}

# ---------------- 脚本自身更新 ----------------
cmd_self_update() {
  local url
  url="$(github_url "https://raw.githubusercontent.com/$NAS_REPO/main/nas.sh")"
  local tmp; tmp="$(mktemp -d)"; NAS_TMP="$tmp"
  info "从 $url 拉取最新脚本"
  curl -fL --retry 3 --connect-timeout 20 --max-time 60 -o "$tmp/nas.sh" "$url" \
    || die "下载脚本失败"
  bash -n "$tmp/nas.sh" || die "下载的脚本语法检查未通过,已中止"
  mv -f "$0" "$0.bak" 2>/dev/null || true
  install -m 0755 "$tmp/nas.sh" "$0"
  NAS_TMP=""; rm -rf "$tmp"
  info "脚本已更新为最新版(旧版备份: $0.bak),请重新执行 nas.sh"
}

# ---------------- 交互式菜单 ----------------
menu() {
  local choice
  while :; do
    echo
    echo "Termux NAS 管理"
    echo "--------------------------------"
    if is_running; then
      echo "状态: 运行中   PID $(nasd_pids | head -n1)   端口 $(config_port)"
    else
      echo "状态: 未运行"
      if [ -s "$(nasd_bin)" ] && is_linux; then
        echo "版本: v$(probe_version "$(nasd_bin)")"
      fi
    fi
    echo "--------------------------------"
    echo " 1) 启动(前台运行,日志直打,Ctrl+C 优雅停止)"
    echo " 2) 安装 / 更新"
    echo " 3) 卸载"
    echo " 0) 退出"
    printf '选择: '
    read -r choice || break
    case "$choice" in
      1)
        if [ ! -s "$(nasd_bin)" ]; then
          warn "未安装 nasd,请先选择 2 安装"
        else
          mutex cmd_start
          block_until_exit
        fi
        ;;
      2)
        if [ -s "$(nasd_bin)" ]; then
          mutex cmd_update
          block_until_exit
        else
          ( mutex cmd_install ) || warn "安装失败,请检查网络"
        fi
        ;;
      3) menu_uninstall ;;
      0|q|Q) break ;;
      *) warn "无效选择: $choice" ;;
    esac
  done
  echo "再见"
}

menu_uninstall() {
  echo "卸载计划:"
  echo "  1. 停止 nasd(如运行中)"
  echo "  2. 删除部署根 $NAS_ROOT(含 data 数据库、files、plugins、bin——不可恢复)"
  printf '确认卸载?输入 y 确认: '
  local a
  read -r a || return 1
  if [ "$a" != "y" ]; then
    info "已取消卸载"
    return 0
  fi
  mutex cmd_uninstall -y
}

# ---------------- 帮助 ----------------
usage() {
  cat <<'EOF'
Termux NAS 一键管理脚本(nas.sh)

主程序为单一二进制 nasd;本脚本提供交互式菜单与命令面。

用法:
  bash nas.sh [menu]                交互式菜单(默认):
                                      1) 启动(前台运行,日志直打,Ctrl+C 优雅停止)
                                      2) 安装 / 更新
                                      3) 卸载
                                      0) 退出
  bash nas.sh install               安装:建目录→拉取 Release 二进制→SHA256 校验→落盘
  bash nas.sh update [-f] [版本]     更新:下载→校验→优雅停止→原子替换(.bak)→重启→失败回滚
  bash nas.sh start                 前台启动 nasd(阻塞;Ctrl+C 优雅停止;
                                      脱离终端用 nohup bash nas.sh start &)
  bash nas.sh stop                  优雅停止 nasd(SIGTERM,超时强制结束)
  bash nas.sh restart               重启 nasd(停止后前台启动)
  bash nas.sh status                查看运行状态(版本/PID/Uptime/端口)
  bash nas.sh log [-n 行数]          查看主框架日志尾部(默认 50 行)
  bash nas.sh uninstall [-y]        卸载(需 -y 才真正删除数据)
  bash nas.sh self-update           更新脚本自身
  bash nas.sh help | version        帮助 / 脚本版本

环境变量:
  NAS_ROOT      部署根(默认 $HOME/nas)
  NAS_REPO      GitHub 仓库(默认 LiquorXR/Termux-NAS)
  NAS_MIRROR    GitHub 加速镜像前缀(默认 https://ghfast.top/,置空则直连)
  NAS_DIST_URL  资产下载基地址(镜像/本地测试用,优先级最高)
  NAS_ARCH      架构覆盖(默认按 uname -m 检测;开发机测试可设 arm64)

一键体验:
  curl -LO https://raw.githubusercontent.com/LiquorXR/Termux-NAS/main/nas.sh
  bash nas.sh install && bash nas.sh start
EOF
}

# ---------------- 互斥锁(变更操作防并发) ----------------
mutex() {
  ensure_dirs
  local lockdir="$NAS_ROOT/.nas.lock.d"
  if ! mkdir "$lockdir" 2>/dev/null; then
    die "另一个 nas.sh 操作正在进行(锁目录 $lockdir 存在)。确认无其他进程后请手动删除。"
  fi
  NAS_LOCKDIR="$lockdir"
  "$@"
  NAS_LOCKDIR=""
  rmdir "$lockdir" 2>/dev/null || true
}

# ---------------- 入口 ----------------
main() {
  require_cmds
  detect_arch
  local cmd="${1:-menu}"
  shift || true

  case "$cmd" in
    menu|"")        menu ;;
    install|setup)  mutex cmd_install "$@" ;;
    update)         mutex cmd_update "$@"; block_until_exit ;;
    start)          mutex cmd_start; block_until_exit ;;
    stop)           mutex cmd_stop ;;
    restart)        mutex cmd_restart; block_until_exit ;;
    status)         cmd_status "$@" ;;
    log)            cmd_log "$@" ;;
    uninstall)      mutex cmd_uninstall "$@" ;;
    self-update)    mutex cmd_self_update ;;
    version|-v)     echo "nas.sh $NAS_SCRIPT_VERSION" ;;
    help|-h|--help) usage ;;
    *) err "未知命令: $cmd(见: bash nas.sh help)" ;;
  esac
}

main "$@"