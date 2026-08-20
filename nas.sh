#!/usr/bin/env bash
# =============================================================================
# nas.sh — Termux NAS 一键部署与管理脚本(仓库根目录)
#
# 功能:自动创建目录结构、从 GitHub Releases 拉取预编译二进制(android/arm64)、
#       校验 SHA256、赋予可执行权限,并管理主程序 nasm/nasd 的
#       安装 / 更新 / 启动 / 停止 / 重启 / 状态 / 日志 / 卸载。
#
# 用法:
#   bash nas.sh install [--service]  # 安装(可选注册 runit 开机自启)
#   bash nas.sh update [-f] [版本]    # 更新到最新(或指定 v<版本>):校验→优雅停机→替换→重启→回滚
#   bash nas.sh start | stop | restart | status [-json] | log [-n N]
#   bash nas.sh doctor                # 环境体检
#   bash nas.sh uninstall [-y]        # 卸载(默认只打印计划,需 -y 才删除数据)
#   bash nas.sh self-update           # 更新本脚本自身
#   bash nas.sh help | version        # 帮助 / 版本
#
# 环境变量(可覆盖):
#   NAS_ROOT      部署根,默认 $HOME/nas
#   NAS_REPO      GitHub 仓库,默认 LiquorXR/Termux-NAS
#   NAS_DIST_URL  资产下载基地址(默认按 GitHub Releases 构造;
#                 镜像/本地测试时可指向自定义 URL/file folder)
# =============================================================================
set -euo pipefail

NAS_SCRIPT_VERSION="1.0.0"
NAS_ROOT="${NAS_ROOT:-$HOME/nas}"
NAS_REPO="${NAS_REPO:-LiquorXR/Termux-NAS}"
PORT_DEFAULT=7531

# 资产命名(与 .github/workflows/release.yml 输出一致,勿改)
ASSET_NASM="nasm-android-arm64"
ASSET_NASD="nasd-android-arm64"
ASSET_SUMS="sha256sums.txt"

# 全局清理(EXIT 陷阱兜底:任何 die/中断都保证清理临时目录与操作锁)
NAS_TMP=""
NAS_LOCKDIR=""
_nas_exit() {
  [ -n "$NAS_TMP" ]      && rm -rf "$NAS_TMP"
  [ -n "$NAS_LOCKDIR" ]  && rmdir "$NAS_LOCKDIR" 2>/dev/null || true
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
  for c in curl sha256sum uname mktemp awk grep sed; do
    command -v "$c" >/dev/null 2>&1 || missing+=("$c")
  done
  if [ "${#missing[@]}" -gt 0 ]; then
    die "缺少命令:${missing[*]}(Termux 下执行: pkg install curl)"
  fi
}

# 是否真实 Linux 系(含 Termux: uname -s 返回 Linux)。
# 用于区分“必须能执行 Linux 二进制”(Lin/unux/Android)与
# 仅在开发机上做文件机制测试(如 Windows Git Bash,无法运行 linux 产物)。
is_linux() { [ "$(uname -s)" = "Linux" ]; }

detect_arch() {
  # NAS_ARCH 可显式覆盖(开发机/分叉仓库测试用): 如 NAS_ARCH=arm64 bash nas.sh ...
  local m="${NAS_ARCH:-$(uname -m)}"
  case "$m" in
    aarch64|arm64) NAS_ARCH="arm64" ;;
    *)
      die "不支持的架构: $m。当前仅发布 android/arm64 预编译二进制;\
请改用源码构建(见 README「源码构建」: make android)。"
      ;;
  esac
}

# ---------------- 下载与校验 ----------------
# 构造资产下载基地址;channel 为 latest 或语义化版本号(不含 v)
release_base_url() {
  local channel="$1"
  if [ -n "${NAS_DIST_URL:-}" ]; then
    printf '%s' "$NAS_DIST_URL"
    return
  fi
  if [ "$channel" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download' "$NAS_REPO"
  else
    printf 'https://github.com/%s/releases/download/v%s' "$NAS_REPO" "$channel"
  fi
}

download_assets() {
  local base="$1" tmp="$2" url a
  for a in "$ASSET_NASM" "$ASSET_NASD" "$ASSET_SUMS"; do
    url="$base/$a"
    info "下载 $a"
    curl -fL --retry 3 --connect-timeout 20 --max-time 180 -o "$tmp/$a" "$url" \
      || die "下载失败: $url"
  done
}

# 严格校验:sha256sum -c 要求校验和文件列出的每个资产都存在且匹配
verify_assets() {
  local tmp="$1"
  ( cd "$tmp" && sha256sum -c sha256sums.txt ) \
    || die "SHA256 校验失败,已中止(不安装任何未经验证的二进制)。\
请重试或检查网络;如为镜像问题请设置 NAS_DIST_URL。"
  info "SHA256 校验通过"
}

# 从校验和文件取出单个资产的哈希
hash_for() {
  local tmp="$1" name="$2"
  awk -v n="$name" '$2 == n { print $1; exit }' "$tmp/sha256sums.txt"
}

# 探测二进制版本号(语义化版本第一个字段)。
# nasd 用 -version;nasm 用 version 子命令(输出首字段为 "nasm",取第二字段)。
probe_version() {
  local bin="$1" line v
  line="$("$bin" -version 2>/dev/null || "$bin" version 2>/dev/null || true)"
  v="$(printf '%s' "$line" | awk '{ print $1 }')"
  if [ "$v" = "nasm" ]; then v="$(printf '%s' "$line" | awk '{ print $2 }')"; fi
  printf '%s' "$v"
}

bin_path() { printf '%s/bin/%s' "$NAS_ROOT" "$1"; }
nasm_bin() { bin_path nasm; }
nasd_bin() { bin_path nasd; }

# ---------------- 目录结构 ----------------
# 与 internal/config 的部署根布局一致:bin plugins data/logs run files
ensure_dirs() {
  mkdir -p "$NAS_ROOT"/bin \
           "$NAS_ROOT"/plugins \
           "$NAS_ROOT"/data/logs \
           "$NAS_ROOT"/run \
           "$NAS_ROOT"/files
}

# ---------------- 运行状态 ----------------
is_running() {
  local out json
  out="$("$NAS_ROOT/bin/nasm" status -json 2>/dev/null)" || return 1
  json="$(printf '%s' "$out" | sed -n 's/.*"running"[[:space:]]*:[[:space:]]*\(true\|false\).*/\1/p')"
  [ "$json" = "true" ]
}

# ---------------- 安装 ----------------
cmd_install() {
  local service=0 a
  for a in "$@"; do
    case "$a" in
      --service) service=1 ;;
      *) err "未知参数: $a(用法: nas.sh install [--service])" ;;
    esac
  done

  ensure_dirs
  local tmp; tmp="$(mktemp -d)"; NAS_TMP="$tmp"

  local base; base="$(release_base_url latest)"
  download_assets "$base" "$tmp"
  verify_assets "$tmp"
  chmod +x "$tmp/$ASSET_NASM" "$tmp/$ASSET_NASD"

  local need_install=1
  if [ -x "$(nasd_bin)" ]; then
    local old new
    old="$(probe_version "$(nasd_bin)")"
    new="$(probe_version "$tmp/$ASSET_NASD")"
    if [ -n "$old" ] && [ "$old" = "$new" ]; then
      info "已安装相同版本 $old,跳过替换(如需重装可先 uninstall)"
      need_install=0
    fi
  fi

  if [ "$need_install" = "1" ]; then
    local pair name asset
    for pair in "nasd:$ASSET_NASD" "nasm:$ASSET_NASM"; do
      name="${pair%%:*}"; asset="${pair##*:}"
      if [ -f "$(bin_path "$name")" ]; then
        info "保留旧版备份: ${name}.bak"
        mv -f "$(bin_path "$name")" "$(bin_path "$name").bak"
      fi
      mv -f "$tmp/$asset" "$(bin_path "$name")"
      chmod +x "$(bin_path "$name")"
    done

    # 冒烟验证:新二进制必须可执行并输出版本
    probe_version "$(nasd_bin)" >/dev/null || die "安装的 nasd 无法执行,请检查产物"
    probe_version "$(nasm_bin)" >/dev/null || die "安装的 nasm 无法执行,请检查产物"
    local vd vn
    vd="$(probe_version "$(nasd_bin)")"; vn="$(probe_version "$(nasm_bin)")"
    [ -n "$vd$vn" ] && info "二进制已验证(nasd v${vd} / nasm v${vn})"
  fi

  if [ "$service" = "1" ]; then
    register_service
  fi

  info "安装完成。部署根: $NAS_ROOT"
  info "下一步: bash nas.sh start;浏览器访问 http://<本机局域网IP>:$PORT_DEFAULT"
  info "提示: 首次启动 nasd 会自动生成 data/config.json(含管理 token)。"
}

# runit 服务注册(Termux:termux-services);依赖 $PREFIX
register_service() {
  if [ -z "${PREFIX:-}" ]; then
    warn "未检测到 Termux 环境(\$PREFIX 为空),跳过服务注册(开发机可忽略)"
    return
  fi
  if ! command -v sv-enable >/dev/null 2>&1; then
    warn "未安装 termux-services(sv-enable 不可用),跳过服务注册(pkg install termux-services)"
    return
  fi
  local sd="$PREFIX/var/service/nasd"
  if ! { mkdir -p "$sd" \
      && printf '#!%s/bin/sh\nexport NAS_ROOT="%s"\nexec "%s/bin/nasd" -root "%s"\n' \
           "$PREFIX" "$NAS_ROOT" "$NAS_ROOT" "$NAS_ROOT" > "$sd/run" \
      && chmod +x "$sd/run"; }; then
    warn "写入 $PREFIX/var/service/nasd 失败(权限?),跳过服务注册"
    return
  fi
  if sv-enable nasd 2>/dev/null || sv enable nasd 2>/dev/null; then
    info "runit 服务已注册并启用: nasd(开机自启)"
  else
    warn "sv-enable 失败,请稍后手动执行: sv-enable nasd"
  fi
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
  channel="${channel#v}" # 容忍用户传 v1.2.3

  [ -s "$(nasm_bin)" ] || die "未安装 nasm,请先 bash nas.sh install"
  ensure_dirs

  local tmp; tmp="$(mktemp -d)"; NAS_TMP="$tmp"

  local base; base="$(release_base_url "$channel")"
  download_assets "$base" "$tmp"
  verify_assets "$tmp"
  chmod +x "$tmp/$ASSET_NASM" "$tmp/$ASSET_NASD"

  local new_ver
  new_ver="$(probe_version "$tmp/$ASSET_NASD")"
  # 真实 Linux(含 Termux)下,下载产物必须能执行(防损坏/架构不符);
  # 非 Linux 开发环境无法运行 linux 产物,跳过该检查仅做文件替换。
  if [ -z "$new_ver" ] && is_linux; then
    die "下载的 nasd 无法执行(产物损坏或架构不符),已中止"
  fi

  local old_ver=""
  if [ -s "$(nasd_bin)" ]; then old_ver="$(probe_version "$(nasd_bin)")"; fi

  if [ "$force" != "1" ] && [ -n "$new_ver" ] && [ -n "$old_ver" ] && [ "$new_ver" = "$old_ver" ]; then
    info "已是最新版本 $new_ver,跳过(如需强制覆盖: nas.sh update -f)"
    NAS_TMP=""; rm -rf "$tmp"
    return
  fi

  info "版本变更: ${old_ver:-未安装} → $new_ver"
  local hash_nasd hash_nasm
  hash_nasd="$(hash_for "$tmp" "$ASSET_NASD")"
  hash_nasm="$(hash_for "$tmp" "$ASSET_NASM")"

  if is_running; then
    # 运行中:复用 nasm 内建更新(nasm update 负责版本比对/enterUpdate 优雅停机/
    # 原子替换(.bak)/重启与失败自动回滚;先更 nasd 后更 nasm,保留旧 nasm 作回滚工具)
    info "nasd 运行中,走优雅更新通道(nasm update)..."
    if [ "$channel" = "latest" ]; then
      "$(nasm_bin)" update -sha256 "$hash_nasd" "$tmp/$ASSET_NASD" \
        || die "nasd 更新失败(nasm 已自动回滚,请用 bash nas.sh log 查看详情)"
    else
      # 指定版本:传入期望版本号,nasm 会校验下载产物版本一致
      "$(nasm_bin)" update -sha256 "$hash_nasd" "$tmp/$ASSET_NASD" "$channel" \
        || die "nasd 更新失败(nasm 已自动回滚,请用 bash nas.sh log 查看详情)"
    fi

    "$(nasm_bin)" self-update -sha256 "$hash_nasm" "$tmp/$ASSET_NASM" || true
    if probe_version "$(nasm_bin)" >/dev/null 2>&1; then
      info "nasm 更新完成: v$(probe_version "$(nasm_bin)")"
    else
      warn "新版 nasm 无法执行,从 .bak 回滚"
      if [ -f "$(nasm_bin).bak" ]; then mv -f "$(nasm_bin).bak" "$(nasm_bin)"; fi
      chmod +x "$(nasm_bin)"
    fi
    info "nasd 更新完成: v$(probe_version "$(nasd_bin)")"
  else
    # 未运行:管理通道不可用,直接替换(无进程占用二进制,安全)
    info "nasd 未运行,直接替换二进制..."
    local pair name asset
    for pair in "nasd:$ASSET_NASD" "nasm:$ASSET_NASM"; do
      name="${pair%%:*}"; asset="${pair##*:}"
      if [ -f "$(bin_path "$name")" ]; then
        mv -f "$(bin_path "$name")" "$(bin_path "$name").bak"
      fi
      mv -f "$tmp/$asset" "$(bin_path "$name")"
      chmod +x "$(bin_path "$name")"
    done
    probe_version "$(nasd_bin)" >/dev/null || die "替换后的 nasd 无法执行"
    probe_version "$(nasm_bin)" >/dev/null || die "替换后的 nasm 无法执行"
    info "二进制已替换(v$(probe_version "$(nasd_bin)"));未自动启动,可执行 bash nas.sh start"
  fi
  NAS_TMP=""; rm -rf "$tmp"
}

# ---------------- 生命周期(委托 nasm) ----------------
cmd_start() {
  [ -s "$(nasm_bin)" ] || die "未安装 nasm,请先 bash nas.sh install"
  ensure_dirs
  "$(nasm_bin)" start
}
cmd_stop() {
  if [ ! -s "$(nasm_bin)" ]; then info "nasm 未安装,无需停止"; return; fi
  "$(nasm_bin)" stop || true
}
cmd_restart() { cmd_stop; cmd_start; }
cmd_status() {
  if [ ! -s "$(nasm_bin)" ]; then info "nasm 未安装(见: bash nas.sh install)"; return; fi
  "$(nasm_bin)" status "$@"
}
cmd_log() {
  [ -s "$(nasm_bin)" ] || die "未安装 nasm,请先 bash nas.sh install"
  "$(nasm_bin)" log "$@"
}

# ---------------- 体检 ----------------
cmd_doctor() {
  info "体检报告(部署根: $NAS_ROOT)"
  local ok=0 bad=0
  check() {
    if eval "$2" >/dev/null 2>&1; then info "  [✓] $1"; ok=$((ok+1)); else warn "  [✗] $1"; bad=$((bad+1)); fi
  }
  require_cmds
  check "目录结构"                        "ensure_dirs"
  check "nasd 二进制存在"                 "[ -x \"\$(nasd_bin)\" ]"
  check "nasm 二进制存在"                 "[ -x \"\$(nasm_bin)\" ]"
  check "nasd 可执行(版本探测)"           "( [ -x \"\$(nasd_bin)\" ] && probe_version \"\$(nasd_bin)\" >/dev/null )"
  check "HTTP 健康(:$PORT_DEFAULT)"       "curl -sf -o /dev/null http://127.0.0.1:$PORT_DEFAULT/health"
  df -h "$NAS_ROOT" 2>/dev/null | tail -1 | awk '{ print "  [i] 磁盘: " $4 " 可用 / " $2 " 总量(" $5 " 已用)" }' || true
  info "检查完成: $ok 项通过, $bad 项异常"
  [ "$bad" = "0" ] || err "存在异常项,请按上方提示处理"
}

# ---------------- 卸载 ----------------
cmd_uninstall() {
  local yes=0 service=0 a
  for a in "$@"; do
    case "$a" in
      -y|--yes) yes=1 ;;
      --service) service=1 ;;
      *) err "未知参数: $a(用法: nas.sh uninstall [-y] [--service])" ;;
    esac
  done

  echo "卸载计划:"
  echo "  1. 停止 nasd(如运行中)"
  echo "  2. 删除部署根 $NAS_ROOT(含 data 数据库、files、plugins、bin——不可恢复)"
  if [ "$service" = "1" ] && [ -n "${PREFIX:-}" ]; then
    echo "  3. 移除 runit 服务 $PREFIX/var/service/nasd"
  fi
  if [ "$yes" != "1" ]; then
    die "未确认卸载:请加 -y 确认执行(-y 将真正删除全部数据)"
  fi

  cmd_stop
  if [ "$service" = "1" ] && [ -n "${PREFIX:-}" ]; then
    command -v sv-disable >/dev/null 2>&1 && sv-disable nasd 2>/dev/null || true
    rm -rf "$PREFIX/var/service/nasd"
    info "runit 服务已移除"
  fi
  rm -rf "$NAS_ROOT"
  info "已删除 $NAS_ROOT"
}

# ---------------- 脚本自身更新 ----------------
cmd_self_update() {
  local url="https://raw.githubusercontent.com/$NAS_REPO/main/nas.sh"
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

# ---------------- 帮助 ----------------
usage() {
  cat <<'EOF'
Termux NAS 一键管理脚本(nas.sh)

用法:
  bash nas.sh install [--service]  安装:建目录→拉取 Release 二进制→SHA256 校验→
                                   赋予可执行权限(可选:注册 runit 开机自启)
  bash nas.sh update [-f] [版本]   更新到最新(或指定 v<版本>):
                                   下载→校验→优雅停机→原子替换(.bak)→重启→失败回滚
  bash nas.sh start                启动 nasd(runit 优先,否则后台)
  bash nas.sh stop                 优雅停止 nasd
  bash nas.sh restart              重启 nasd
  bash nas.sh status [-json]       查看运行状态
  bash nas.sh log [-n 行数]         查看主框架日志尾部(默认 50 行)
  bash nas.sh doctor               环境体检(二进制/目录/健康端口/磁盘)
  bash nas.sh uninstall [-y] [--service]  卸载(需 -y 才真正删除数据)
  bash nas.sh self-update          更新脚本自身
  bash nas.sh help | version       帮助 / 脚本版本

环境变量:
  NAS_ROOT      部署根(默认 $HOME/nas)
  NAS_REPO      GitHub 仓库(默认 LiquorXR/Termux-NAS)
  NAS_DIST_URL  资产下载基地址(镜像/本地测试用,默认 GitHub Releases)
  NAS_ARCH      架构覆盖(默认按 uname -m 检测;开发机测试可设 arm64)

一键体验:
  curl -LO https://raw.githubusercontent.com/LiquorXR/Termux-NAS/main/nas.sh
  bash nas.sh install --service && bash nas.sh start
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
  local cmd="${1:-help}"
  shift || true

  case "$cmd" in
    install|setup)  mutex cmd_install "$@" ;;
    update)         mutex cmd_update "$@" ;;
    start)          mutex cmd_start ;;
    stop)           mutex cmd_stop ;;
    restart)        mutex cmd_restart ;;
    status)         cmd_status "$@" ;;
    log)            cmd_log "$@" ;;
    doctor|check)   cmd_doctor ;;
    uninstall)      mutex cmd_uninstall "$@" ;;
    self-update)    mutex cmd_self_update ;;
    version|-v)     echo "nas.sh $NAS_SCRIPT_VERSION" ;;
    help|-h|--help|"") usage ;;
    *) err "未知命令: $cmd(见: bash nas.sh help)" ;;
  esac
}

main "$@"