# Termux NAS — 完整项目文档(中文)

> **状态**:里程碑 M1–M6 全部完成 · 当前版本线 v0.1.0 / v0.2.0
> **架构**:主框架守护进程(`nasd`,单二进制内建 NAS 必要功能)+ 一键管理脚本(`nas.sh`)+ 插件(独立二进制,nasd 全权控制)
> **技术栈**:Go + Fiber + SQLite(WAL)+ 前端 Vite 工程化(HTMX + 原生 JS + 手写设计系统)
> **部署环境**:Termux(Android 上的 Linux 环境),单进程 + 可选插件进程

---

## 目录

1. [项目概述](#1-项目概述)
2. [整体架构](#2-整体架构)
3. [仓库与部署目录结构](#3-仓库与部署目录结构)
4. [技术栈](#4-技术栈)
5. [快速开始](#5-快速开始)
6. [nas.sh 生命周期管理](#6-nassh-生命周期管理)
7. [主框架 nasd 内部](#7-主框架-nasd-内部)
8. [内建模块](#8-内建模块)
9. [插件系统](#9-插件系统)
10. [插件市场](#10-插件市场)
11. [PWA(渐进式 Web 应用)](#11-pwa渐进式-web-应用)
12. [前端(Vite Web 应用)](#12-前端vite-web-应用)
13. [API 参考](#13-api-参考)
14. [配置参考](#14-配置参考)
15. [安全设计](#15-安全设计)
16. [CI/CD 与发布](#16-cicd-与发布)
17. [测试与质量](#17-测试与质量)
18. [关键设计决策](#18-关键设计决策)
19. [历史与路线](#19-历史与路线)
20. [常见问题与排障](#20-常见问题与排障)

---

## 1. 项目概述

### 1.1 目标

在 Termux 中构建一个**可插拔的移动端 NAS 系统**:

- **高性能、低资源占用**:主框架为 Go 单二进制,常驻内存约 15–30 MB
- **完全兼容移动端 Termux**:无 root 可用,高位端口,nohup 后台守护
- **可扩展**:核心功能内建,扩展功能以独立二进制插件形式动态加载
- **易管理**:一键脚本(nas.sh)负责主框架 nasd 的生命周期(安装/更新/启停/状态/日志/卸载);插件的一切操作由主框架(nasd)通过 Web UI 统一控制

### 1.2 核心设计原则

1. **混合架构** — 核心功能(认证/文件/监控/备份)内建在 nasd;扩展功能(下载/云盘/媒体/第三方)做成插件。
2. **单二进制、职责单点** — nas.sh 只管理 nasd 本体(安装/更新/启停/状态/日志/卸载);nasd 全权控制插件(安装/卸载/启停/更新)。
3. **插件进程级隔离** — 插件独立崩溃不影响主框架,可独立更新。
4. **懒加载** — 插件按需启动(点开页面才启动进程),常驻内存不随插件数量增长。
5. **单部署根** — `~/nas/` 一个目录包含一切,备份 = 拷贝目录。

### 1.3 代码规模

| 领域 | 规模 |
|---|---|
| Go 生产代码 | 38 个文件,约 4,900 行 |
| Go 测试 | 20 个文件,约 2,300 行 |
| 前端 | Vite + HTMX + 原生 JS;手写设计系统 `app.css`(约 760 行) |
| 运维脚本 | `nas.sh`(595 行,生命周期)、`scripts/smoke-test.sh`(冒烟测试) |
| CI/CD | `ci.yml`(全量质量门禁)+ `release.yml`(标签驱动 android/arm64 发布) |

---

## 2. 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                  用户/浏览器 (统一入口 :7531)              │
│        Web UI 同时提供「插件管理」页面(由 nasd 控制)       │
└──────────────────────────┬──────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────┐
│             nasd · 主框架(常驻守护进程,单一二进制)          │
│                                                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│  │ 认证中心  │ │ 文件管理  │ │ 系统监控  │    │
│  └──────────┘ └──────────┘ └──────────┘    │
│  ┌──────────┐ ┌────────────────────────────────────┐     │
│  │ 备份中心  │ │ 插件管理器(安装/卸载/启停/更新/懒加载) │     │
│  └──────────┘ └────────────────────────────────────┘     │
│  SQLite · 配置 · 日志 · 单实例锁(run/nas.lock/互斥量)        │
└──────────────┬───────────────────────────────▲───────────┘
               │ SIGTERM 优雅停止 / /health 探活  │ 用户请求 /p/<id>/*
┌──────────────▼───────────────┐   ┌──────────┴───────────┐
│  nas.sh · 一键管理脚本        │   │  plugins/ 插件二进制   │
│  install/update/start/      │   │  download :18002     │
│  stop/restart/status/log    │   │  alist :18003        │
│  不管理插件 · 不常驻           │   │  media  :18004       │
└──────────────────────────────┘   │  (nasd 全权控制)      │
                                   └──────────────────────┘
```

### 2.1 三个组成部分

| 组件 | 形态 | 职责 | 常驻 |
|---|---|---|---|
| **nas.sh** | bash 脚本(仓库根) | 主框架全生命周期:安装/更新/启停/状态/日志/卸载 | 否(用完即走) |
| **nasd** | 守护进程二进制 | 运行时全部能力:HTTP 服务、内建 NAS 功能、**插件全权管理** | 是(nas.sh nohup 后台) |
| **插件** | 独立二进制 | 扩展功能:下载/云盘/媒体等;生命周期完全由 nasd 控制 | 按需(懒加载) |

> **职责单点原则**:插件的安装、卸载、启停、更新**只能**通过 nasd(Web UI「插件管理」页或用户通道 API)操作,nas.sh 不感知插件存在。nas.sh 与插件之间不存在任何直接交互。

### 2.2 通信与生命周期

| 通道/方式 | 路径/机制 | 用途 | 暴露面 |
|---|---|---|---|
| 用户通道 | `:7531` HTTP | 浏览器访问 Web UI / API | 局域网 / Tailscale |
| 生命周期控制 | SIGTERM 优雅停止 + `/health` 探活 + 日志文件直读 | nas.sh 管理主程序 | 仅本机(Termux 命令行) |

> 不保留任何本地管理 socket 或管理 CLI;主程序只有一个二进制 `nasd`。

---

## 3. 仓库与部署目录结构

### 3.1 部署根(`~/nas/`)

```
~/nas/                          # 单一部署根
├── bin/nasd                    # 主框架守护进程(单一二进制)
├── plugins/                    # 插件可执行文件
│   ├── download                # 下载中心插件
│   ├── alist                   # 云盘聚合插件
│   └── media                   # 媒体服务插件
├── data/
│   ├── nas.db                  # SQLite(会话 / 分享 / 备份任务 / meta)
│   ├── config.json             # 主框架配置
│   └── logs/nasd.log           # 主框架日志
├── files/                      # 文件管理根目录(默认;可用 file_root 覆盖)
└── run/nas.lock                # 单实例锁(flock,运行时创建)
```

**约定**:
- 备份 `data/` 目录 = 备份全部配置与状态;`plugins/` 可重新下载。
- 更新只动 `bin/nasd` 一个二进制,`.bak` 保留旧版以便回滚。

### 3.2 仓库根

```
.
├── README.md / README.en.md     # 快速开始(中文 / English)
├── nas.sh                       # ★ 一键部署/更新/管理脚本
├── scripts/smoke-test.sh        # nas.sh 机制 + 运行时冒烟测试
├── .github/workflows/
│   ├── ci.yml                   # 全量质量门禁(gofmt/vet/test/race/android/smoke)
│   └── release.yml              # 推送 v* 标签 → 构建并发布 android/arm64
├── docs/
│   ├── NAS框架开发文档.md        # 正式开发文档(中文)
│   ├── PROJECT_GUIDE.zh-CN.md   # 本文档(中文)
│   └── PROJECT_GUIDE.en-US.md   # 本文档(English)
└── src/
    ├── cmd/nasd/main.go         # 守护进程入口
    ├── internal/                # 全部 Go 包(见下表)
    ├── web/                     # Vite 前端
    ├── scripts/build.sh         # 构建辅助(host / android)
    └── Makefile
```

### 3.3 Go 包一览(`src/internal/`)

| 包 | 职责 |
|---|---|
| `config` | 部署根解析、`data/config.json` 读写(原子写) |
| `daemon` | 核心:HTTP 路由、DB 打开/迁移、插件管理器、备份/市场 HTTP 处理器 |
| `auth` | Argon2id 密码哈希、SQLite 会话、Cookie 中间件、登录限流 |
| `files` | 文件增删改查/搜索/分享链接(严格路径围栏) |
| `monitor` | 系统状态:CPU/内存/磁盘/网络/电量(linux/android + windows 双平台) |
| `backup` | 备份任务:SQLite 存储、5 字段 cron 调度、rsync/复制执行器、通知 |
| `market` | 内嵌插件市场索引(`go:embed`) |
| 插件子系统 | 位于 `daemon` 包(`plugins.go`、`plugins_http.go`、`proxy.go`) |
| `safehttp` | SSRF 防护的安全 HTTP 客户端(插件/更新包下载) |
| `lock` | 单实例锁(Unix flock / Windows 互斥量) |
| `version` | 构建注入的版本信息 |
| `webui` | `go:embed` 打包前端构建产物 |

---

## 4. 技术栈

### 后端

| 层 | 选型 | 理由 |
|---|---|---|
| 语言 | Go(`go 1.25.5`,模块 `github.com/termux-nas/nas`) | 单静态二进制、低内存、可交叉编译 android/arm64 |
| Web 框架 | Fiber v2.52.15(fasthttp) | 高性能、体积小,适配低端手机 |
| SQLite 驱动 | `modernc.org/sqlite` v1.56.0(纯 Go) | 支撑 `CGO_ENABLED=0` 静态编译 —— Termux 无 C 编译链 |
| HTTP 客户端 | `net/http` + `fasthttp` | safehttp 下载客户端;fasthttpadaptor 托管内嵌静态资源 |
| 密码哈希 | `golang.org/x/crypto/argon2` | Argon2id(OWASP 推荐档位,按手机端调优) |
| 系统 API | `golang.org/x/sys` | Windows 互斥量实现单实例(开发环境) |
| 日志 | `log/slog`(标准库) | text handler → stderr + `data/logs/nasd.log` |

### 前端(`src/web/`)

| 项 | 选型 |
|---|---|
| 构建工具 | Vite 5.4.11(`npm run build` → `../internal/webui/dist`,`go:embed` 打包) |
| 渐进增强 | htmx.org 2.0.4 |
| 应用形态 | 原生 JS 模块,无框架;手写设计系统 |
| 样式 | 单一 `src/styles/app.css`(约 760 行),CSS 变量,深色/浅色主题 |
| PWA | `manifest.json` + `icon.svg` + service worker(`sw.js`) |

---

## 5. 快速开始

### 5.1 本机开发(Windows / Linux / macOS)

```bash
cd src
make build          # 构建 nasd 到 ../bin/(自动先构建前端)
# 终端 A:启动守护进程(Ctrl+C 即优雅停止)
NAS_ROOT=/tmp/nas ./bin/nasd -root /tmp/nas
# 终端 B:用 nas.sh 管理生命周期(Linux/macOS)
bash ../nas.sh status
bash ../nas.sh log -n 20
bash ../nas.sh stop
```

说明:
- `nasd` 支持 `-root`(部署根;默认 `$NAS_ROOT` 或 `$HOME/nas`)、`-debug`、`-version`。
- Windows 下产物带 `.exe` 后缀,生命周期一般直接 Ctrl+C 结束(Windows 无 flock/SIGTERM 语义,单实例锁走内核互斥量)。
- `bin/` 被 gitignore,构建产物不入库。

### 5.2 Termux 部署(推荐:一键脚本,无需装 Go)

```bash
pkg install curl                # 首次:补齐依赖
# 国内网络建议经 ghfast.top 镜像下载脚本(nas.sh 内置下载同样默认走此镜像)
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/LiquorXR/Termux-NAS/main/nas.sh -o nas.sh
bash nas.sh install   # 安装
bash nas.sh start
```

安装器自动完成:创建 `~/nas` 目录结构 → 从最新 GitHub Release(经镜像)拉取 `nasd-android-arm64` →
SHA256 校验 → chmod +x → 落盘。

浏览器访问 `http://<手机局域网IP>:7531`,按向导创建管理员账号即可。

### 5.3 Termux 源码构建(贡献者/离线回退)

```bash
pkg install golang
cd ~/nas/src && make android    # CGO_ENABLED=0 GOOS=android GOARCH=arm64,内嵌前端
bash ../nas.sh start            # 后台启动 nasd(nohup)
```

---

## 6. nas.sh 生命周期管理

`nas.sh`(脚本版本 2.1.0)是主守护进程的**唯一**管理接口。常用命令速查见 README,本节讲内部机制。

### 6.1 命令面

```
bash nas.sh install             # 建目录 → 拉取 Release 二进制 → SHA256 校验 → 落盘
bash nas.sh update [-f] [版本]     # 更新到最新(或指定 v<版本>)
bash nas.sh start | stop | restart
bash nas.sh status [-json] | log [-n N]
bash nas.sh doctor                 # 体检(目录/二进制/健康端口/磁盘)
bash nas.sh uninstall [-y]
bash nas.sh self-update
bash nas.sh help | version
```

### 6.2 与 nasd 的交互(无需管理 socket)

| 操作 | 机制 |
|---|---|
| 停止 | 向守护进程发 SIGTERM;nasd 走优雅退出(`ctx.Done()` → 先停插件 → HTTP `ShutdownWithTimeout(3s)` → 释放锁)。`nas.sh` 最多等约 12s,超时升级 SIGKILL |
| 探活/状态 | 请求用户通道 `GET /health`,返回 status/version/uptime/pid/port;PID 优先读 `run/nas.lock`,`pgrep` 兜底 |
| 日志 | 直读 `data/logs/nasd.log` 尾部 |
| 单实例 | nasd 启动时对 `run/nas.lock` 做 flock;第二个实例直接退出 |
| 防并发 | 变更类命令(install/update/start/stop/restart/uninstall/self-update)先取锁目录 `$NAS_ROOT/.nas.lock.d`,避免互相竞争 |

### 6.3 原子更新(`nas.sh update`)

```
① 下载新版 nasd → 临时文件(对照 sha256sums.txt 校验;篡改立即中止且无副作用)
② 若运行中:SIGTERM 优雅停止,等待进程退出、单实例锁释放
③ 原子替换:旧版 → bin/nasd.bak;新版 → bin/nasd;chmod +x
④ 重启新 nasd,轮询 /health 直到就绪
⑤ 就绪即完成(保留最近一份 .bak 便于手动回滚);启动失败 → 回滚 .bak 旧版并重启
```

- `update -f` 强制替换(即使版本相同);同版本默认跳过。
- 版本比较取 `nasd -version` 输出的语义化版本首字段。
- Linux 下下载产物必须可执行且能报出版本(防损坏/架构不符)。

### 6.4 环境变量

| 变量 | 含义 | 默认 |
|---|---|---|
| `NAS_ROOT` | 部署根 | `$HOME/nas` |
| `NAS_REPO` | GitHub 仓库 | `LiquorXR/Termux-NAS` |
| `NAS_MIRROR` | GitHub 加速镜像前缀 | `https://ghfast.top/`(置空直连) |
| `NAS_DIST_URL` | 资产下载基地址(优先级最高,镜像/本地测试用) | 按 GitHub Releases 构造 |
| `NAS_ARCH` | 架构覆盖(开发机测试可设 arm64) | `uname -m` |

---

## 7. 主框架 nasd 内部

### 7.1 启动序列(`cmd/nasd/main.go` + `daemon.Run`)

```
signal ctx(SIGINT/SIGTERM)
  → 解析根与路径(config.Resolve + EnsureDirs)
  → 加载/生成配置(config.Load)
  → 打开日志(stderr + data/logs/nasd.log),可选 -debug
  → daemon.Run(ctx):
      0.  获取单实例锁(run/nas.lock)
      1.  WAL 模式打开 SQLite,执行迁移
      1.5 认证存储(应用 trust-proxy / secure-cookie 选项)
      1.6 文件存储(默认 <root>/files,或 cfg.FileRoot)
      2.  插件管理器:扫描登记元信息(不启动进程——懒加载)
      2.5 备份管理器(存储 + 调度 + 执行 + 通知)
      3.  组装 HTTP 应用并以 goroutine 监听 :7531
      4.  插件空闲回收 ticker
      5.  备份调度 ticker(每分钟)
      6.  等待 ctx 取消或 HTTP 错误 → Stop()
```

### 7.2 优雅退出(`daemon.Stop`)

依次停止全部运行中插件,然后以 3 秒超时关闭 HTTP——避免 keep-alive 连接长期阻塞进程退出
(否则单实例锁不释放,`nas.sh` 无法安全替换二进制)。

### 7.3 数据库结构与会话迁移

- 纯 Go SQLite,`PRAGMA journal_mode=WAL`、`synchronous=NORMAL`、`busy_timeout=5000`、`foreign_keys=ON`。
- `db.SetMaxOpenConns(1)`(SQLite 单写者)。
- 版本化迁移记录于 `meta.schema_version`:
  - **v1**:`meta` 表(幂等基座)
  - **v2**:`users`、`sessions`(认证中心)
  - **v3**:`shares`(文件分享链接)
- `backup_jobs` 由备份存储自建。
- `meta` 还会记录每次启动的 `nasd_version` 与 `last_start`。

### 7.4 HTTP 路由策略(`daemon/buildHTTP`)

- Fiber 应用,`BodyLimit: 512 MiB`。
- 全局 `securityHeaders` 中间件。
- `GET /health`(免鉴权,供 nas.sh 探活)、`GET /api/version`(免鉴权)。
- 页面路由:`/login`、`/setup`、`/`(按登录态分发)。
- 全部 `/api/...` 业务路由需登录;写操作额外过 `checkSameOrigin`(Origin == Host)。
  例外:公开分享下载 `/s/:token`。
- 插件反代:`/p/:id` 与 `/p/:id/*`(需登录)。
- 内嵌静态资源最后经 `http.FileServer` + `fasthttpadaptor` 托管。

---

## 8. 内建模块

### 8.1 认证中心(`internal/auth`)

- **首次设置**:仅首次运行可用,`/api/auth/setup` 创建管理员并自动登录;系统有用户后禁用。
- **凭据**:密码经 Argon2id(`t=3, m=64 MiB, p=4, key=32 B`,编码
  `argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>`)哈希;校验用恒定时间比较。
- **会话**:32 字节随机 token 存 SQLite,TTL 7 天;cookie `nas_session`
  为 `HttpOnly + SameSite=Lax`,`Max-Age` 与 TTL 对齐;`force_https` 时加 `Secure`。
- **中间件**:`RequireAuth`(401 JSON)、`PageAuth`(重定向登录页)、`OptionalAuth`、`SessionUser`。
- **限流**:按 IP 滑动窗口——10 分钟内连续 5 次失败锁定 15 分钟(`429` + `Retry-After`)。
  `trust_proxy` 切换为按 `X-Forwarded-For` 计(`仅在可信反代之后`);内存上限 4096 键、惰性清扫。
- **CSRF**:SameSite=Lax cookie 为主防线;非 GET 请求的 `Origin==Host` 校验为第二道防线。

### 8.2 文件管理(`internal/files`)

- 作用于文件根(默认 `<root>/files`;Termux 可将 `file_root` 设为 `~/storage/shared` 指向 Android 共享存储)。
- 操作:列表(目录优先、不区分大小写排序)、建目录、多文件上传、流式下载、重命名、
  递归删除、名称搜索、分享链接。
- **路径安全**(`safe.go`):所有用户路径经 `Normalize`——拒绝绝对路径(平台绝对路径 + 前导 `/`
  + UNC `\\` + Windows 盘符变体含 `C:foo`)、拒绝 `..` 逃逸、强制限定在根内;不跟随符号链接。
- **上传限制**:单文件 ≤ 256 MiB;文件名必须过 `SafeName`(仅基础名,禁分隔符)。
- **下载**为手动打开 + `SetBodyStream` 流式输出(不走 Fiber 文件缓存),Windows 下不会锁住文件。
- **存储型 XSS 防护**:可执行内容类型(html/xhtml/svg/xml/js/json)禁止内联渲染,一律强制 `attachment`;
  分享下载恒为 `attachment`。
- **搜索**:递归、不区分大小写子串,上限 200 条、深度 8。
- **分享链接**:随机 16 字节 hex token 存 `shares`,带有效期(默认 24h,上限 365 天);
  公开端点 `GET /s/:token` 校验后流式下载;过期链接访问时自动清理。

### 8.3 系统监控(`internal/monitor`)

- `GET /api/monitor/summary` → CPU%/内存/磁盘/网络累计/电量(Termux)/平台/主机名/运行时长。
- 平台采集:Linux/Android 读 `/proc`(`/proc/stat`、`/proc/meminfo`、`/proc/net/dev`、
  `/proc/uptime`、`statfs`);Windows 读系统 API(`monitor/windows.go`);其余平台返回空。
- CPU 使用率为相邻两次采样的差值(首次返回 0)。
- 电量经 `termux-battery-status`(仅当设置了 `$PREFIX`),带 10s 结果缓存,轮询时不会每请求拉起子进程。
- 前端看板(`pages/monitor.js`)每 3s 轮询,环形进度按阈值变色(≥80% 预警,≥90% 危险)。

### 8.4 备份中心(`internal/backup`)

- **任务**(`backup_jobs` 表):名称/源/目标/定时/启用/保留份数/最近运行统计。
- **调度**:进程内 ticker 每分钟检查到期任务,匹配 5 字段 cron(支持 `* / , -`,分–周);
  定时为空 = 仅手动。
- **执行**(`runBackup`):rsync 优先(`-a --delete` 增量;远程 `user@host:`/`rsync://` 必须 rsync);
  否则降级本地递归复制。恢复 = 源/目标反转执行。
- **通知**:完成时经 `termux-notification`(可注入函数,默认 `defaultNotify`)。
- **防重入**:执行中的任务不可重复触发(按任务 ID 的 `running` 集合)。
- **API**:`/api/backup/jobs`(GET/POST/PUT/DELETE)、`/api/backup/run`、`/api/backup/restore`;UI「备份」页。
- 说明:`keep_copies` 字段已持久化并在 API 暴露,但执行器目前尚未强制做多份轮转。

---

## 9. 插件系统

### 9.1 插件管理器(`internal/daemon/plugins.go`)

- **状态机**:`stopped → starting → running → stopping → stopped`,另有 `crashed`、`crash-loop`。
- **扫描**(`Scan`):遍历 `plugins/`,登记可执行文件(仅元信息,不启动进程);
  磁盘上已删除的插件会被注销(运行中的不删,避免悬空);登记不会重置运行中插件的状态。
- **可执行判定**跨平台:Unix 看执行位;Windows 看扩展名(`exe/bat/cmd/com`)或文件头
  (`MZ`=PE、`\x7fELF`、`#!` shebang)。
- **启动**(`startLocked`):以 `--name=<id> --port=0` 拉起进程,5 秒内在 stdout 中解析一行注册 JSON
  (非 JSON 行按日志跳过);失败杀进程并视为崩溃处理。
- **崩溃恢复**(`watchExit`):区分主动停止(`stopping`)与崩溃。带线性退避(`n * 2s`)自动重启;
  连续 3 次崩溃进入 `crash-loop` 停止自动重启;稳定运行 ≥10s 清零计数;人工 `Stop` 清零并解除 crash-loop。
- **懒加载**(`EnsureRunning`):首次访问 `/p/<id>/*` 才启动并等待注册;
  空闲回收(`Reap`)回收超过 `plugin_idle_timeout`(默认 600s=10min)未访问的插件,以半周期扫描。
- **反向代理**(`proxy.go`):`/p/<id>/*` → `http://127.0.0.1:<port>/*`,保留子路径与查询串。
  边缘统一鉴权在前;转发前**剥离** `Cookie`、`Authorization`、`Proxy-Authorization`,插件无法冒用登录用户。
  插件 ID 校验为 `[A-Za-z0-9_.-]`、长度 ≤64,明确拒绝 `.`/`..`(路径穿越)。
- **停机**(`ShutdownAll`):守护进程优雅退出时依次停止全部插件。

### 9.2 插件管理 API(`plugins_http.go`)

```
GET    /api/plugins                 # 列表 + 状态
POST   /api/plugins/install         # 安装:multipart 上传 或 {name, source: url}
POST   /api/plugins/<id>/start|stop|restart
DELETE /api/plugins/<id>            # 卸载(停进程 + 删文件 + 重扫)
GET    /api/plugins/<id>/log        # 插件日志/信息
```

- 包格式:`.tar.gz` 内含**恰好一个**可执行文件(单层,容忍 `./` 前缀)。
  落盘文件名是插件 **ID**(不信任包内文件名);Windows 下无扩展名 PE 自动改名 `<id>.exe`。
- URL 安装经 `safehttp`(30s 超时、64 MiB 上限、私网/回环拦截)——SSRF 防护。

### 9.3 注册协议(插件 ↔ nasd)

```
① nasd 以 --name=<id> --port=0 启动插件进程
② 插件绑定(随机)端口,并向 stdout 输出一行注册 JSON:
     {"id":"download","name":"下载中心","version":"1.0.0","port":18002,"nav":"下载","icon":"download"}
③ nasd 解析 stdout,登记条目,标记 running
④ 5 秒内未注册成功 → 判定启动失败 → 记日志并自动重启(最多 3 次)
```

### 9.4 插件必备能力

| 要求 | 说明 |
|---|---|
| 遵守注册协议 | 启动后向 stdout 输出注册 JSON |
| 监听随机端口 | 经 `--port 0`(或传入的 `--port`) |
| 响应健康检查 | `GET /health` 返回 200(供探活) |
| 独立打包 | Go 静态编译自带全部依赖 |

---

## 10. 插件市场

- 市场索引为内嵌 JSON(`internal/market/static/market.json`,`go:embed`),
  收录 `download`、`alist`、`media`、`photos`(名称/版本/描述/作者/图标/下载地址/体积提示)。
- `GET /api/market` 将索引与已安装状态合并(也识别 `.exe` 变体)。
- `POST /api/market/install {id}` 一键安装,复用插件安装器(安全下载 → 解包 → 落盘 → 重扫)。
- Web UI:「市场」页卡片浏览、已装徽标、一键安装。
- 索引文档带版本字段,预留远程索引覆盖/刷新的能力(刷新端点留待后续)。

---

## 11. PWA(渐进式 Web 应用)

- `public/manifest.json`:standalone 展示、主题/背景色、SVG 图标(`any` purpose)。
- `public/icon.svg`:矢量应用图标。
- `public/sw.js`:带版本化缓存(`nas-v2`)的 service worker:
  - 导航请求:网络优先,离线回退缓存 `/`;
  - 同源静态资源:缓存优先,再回退网络;
  - API(`/api/`)、跨域与非 GET 请求一概不缓存。
- 生产环境加载时注册(dev 5173 跳过,避免离线壳缓存陈旧开发资源)。

---

## 12. 前端(Vite Web 应用)

`src/web/` 结构:

| 路径 | 内容 |
|---|---|
| `index.html` | 单一 HTML 壳:图标 SVG 符号库、认证视图、应用壳、底部抽屉、对话框;防闪烁主题脚本 |
| `src/main.js` | 启动:认证三态路由(未初始化/未登录/已登录)、导航、主题切换、会话失效处理、PWA 注册 |
| `src/api.js` | fetch 封装(JSON、错误归一、401 → 登录)+ 格式化工具 |
| `src/ui.js` | toast / 确认对话框 / 输入对话框 / 按钮 loading 原语 |
| `src/pages/files.js` | 文件管理页(JS 渲染;桌面表格、移动卡片) |
| `src/pages/monitor.js` | 系统监控看板(3s 轮询、环形进度) |
| `public/partials/*.html` | HTMX 页面:插件、市场、服务、备份、设置(各带内联行为脚本) |
| `public/manifest.json`、`public/icon.svg`、`public/sw.js` | PWA 资产 |
| `src/styles/app.css` | 整个设计系统:CSS 变量、主题、布局、组件(约 760 行) |
| `vite.config.mjs` | dev 服务 :5173,`/api`、`/p`、`/s`、`/health` 代理到 nasd;产物输出 `../internal/webui/dist` |

设计要点:
- 原生 JS 模块 + HTMX 2(`window.htmx` 已暴露给 partial 内联脚本)。
- 深色/浅色/跟随系统主题(`data-theme` 挂 `<html>`),主题存 `localStorage`。
- 响应式:桌面侧边栏;移动端顶栏 + 底部标签栏 +「更多」抽屉。
- partial 内联脚本轻量,用 `fetch` + 重渲染并轮询。

---

## 13. API 参考

基址:`http://<host>:7531`。除特别说明外,所有端点需要会话 cookie(`nas_session`,登录/设置时下发)。

### 健康与版本(免鉴权)

```
GET /health                → {status, version, uptime, pid, port}
GET /api/version           → {version, commit, buildTime}
```

### 认证

```
GET  /api/auth/status      → {initialized, authed, username}          (免鉴权)
POST /api/auth/setup       → 创建管理员并自动登录(仅首次)
POST /api/auth/login       → {username:...} + Set-Cookie
POST /api/auth/logout      → 清理会话与 cookie
GET  /api/auth/me          → {username, created_at}
```

### 文件

```
GET  /api/files/list?path=         → {path, entries[]}
GET  /api/files/download?path=     → 流式下载(attachment)
GET  /api/files/search?q=          → {results[]}
POST /api/files/mkdir              → {path, name} | HX-Prompt
POST /api/files/upload             → multipart(path + files[])
POST /api/files/rename             → {path, new_name} | HX-Prompt
POST /api/files/delete             → {path}
POST /api/files/share              → {path, expires_hours?} → {url, expires_at}
GET  /s/:token                     → 公开分享下载(attachment)
```

### 监控

```
GET /api/monitor/summary           → {cpu_percent, mem_*, disk_*, battery?, net?, platform, ...}
```

### 备份

```
GET    /api/backup/jobs
POST   /api/backup/jobs            → Job(name/source/target/schedule)
PUT    /api/backup/jobs/:id
DELETE /api/backup/jobs/:id
POST   /api/backup/run             → {id}   (异步)
POST   /api/backup/restore         → {id}   (异步,方向反转)
```

### 插件市场

```
GET  /api/market                   → {market:{name, version, plugins[]}}
POST /api/market/install           → {id}
```

### 插件

```
GET    /api/plugins                → {plugins[]}(id/path/size/state/pid/restarts/reg/last_err)
POST   /api/plugins/install        → multipart(file) 或 {name, source}
POST   /api/plugins/<id>/start|stop|restart
DELETE /api/plugins/<id>
GET    /api/plugins/<id>/log       → {id, state, restarts, last_err}
GET|POST|PUT|DELETE /p/<id>[/...]  → 插件反向代理(统一鉴权,剥离敏感头)
```

---

## 14. 配置参考(`data/config.json`)

首次启动自动生成;改配置建议先停 nasd(启动时重新读取):

| 键 | 含义 | 默认 |
|---|---|---|
| `port` | 用户通道 HTTP 端口(高位端口,无 root 无法绑 80/443) | `7531` |
| `host` | 监听地址 | `0.0.0.0` |
| `file_root` | 文件管理根目录(Termux 可设 `~/storage/shared`) | `<root>/files` |
| `plugin_idle_timeout` | 插件懒加载空闲回收秒数 | `600` |
| `trust_proxy` | 信任 `X-Forwarded-For` 用于登录限流(仅在可信反向代理之后!) | `false` |
| `force_https` | 会话 cookie 加 `Secure` 标记(仅在 HTTPS 反向代理/隧道之后) | `false` |
| `created_at` | 首次生成时间(只读) | — |

写入为原子操作(临时文件 + rename),权限 `0600`。

---

## 15. 安全设计

| 关注点 | 对策 |
|---|---|
| 认证 | Argon2id 密码哈希;32 字节随机会话 token;7 天 TTL 会话 cookie `HttpOnly + SameSite=Lax` |
| 暴力破解 | 按 IP 登录限流:5 次失败 → 锁 15 分钟(`429` + `Retry-After`);滑动窗口;内存有界 |
| CSRF | SameSite=Lax cookie + 非 GET 请求 `Origin == Host` 校验 |
| XSS | CSP(`default-src 'self'`,为 HTMX 谨慎放行内联);JS 渲染页输出转义;文件下载的存储型 XSS 防护(html/svg/xml/js 强制 attachment) |
| 点击劫持 | `X-Frame-Options: DENY` |
| MIME 嗅探 | `X-Content-Type-Options: nosniff`;`Referrer-Policy: no-referrer` |
| 页面缓存 | `/login`、`/setup` 使用 `Cache-Control: no-store` |
| 路径穿越/LFI | 严格 `Normalize`(拒绝绝对/UNC/盘符/`..`)+ 根内包含校验;不跟随符号链接 |
| SSRF | `safehttp` 在拨号阶段拦截回环/私网/链路本地/CGNAT/ULA 地址(DNS 重绑定安全),30s 超时、64 MiB 上限、≤5 次重定向 |
| 插件隔离/最小权限 | 插件只监听 `127.0.0.1`;转发前剥离 `Cookie`/`Authorization`/`Proxy-Authorization`;边缘统一鉴权 |
| 上传 | 单文件 256 MiB;`SafeName`;插件包只能含单个可执行文件且按插件 ID 落盘 |
| 暴露面 | 对外仅用户通道单端口;生命周期控制仅本机命令行(SIGTERM/health/日志直读);无管理 socket/CLI |
| 部署加固 | 置于 Tailscale/Cloudflare Tunnel 之后;仅在可信 TLS 反代后才开启 `trust_proxy`/`force_https` |

---

## 16. CI/CD 与发布

### CI(`ci.yml`,push 到 main/master 或 PR)

1. 装 Go(按 `src/go.mod`)+ Node 20(npm 缓存);
2. **先构建前端**(`npm ci && npm run build` → `dist`),因为 `go:embed` 要求 `src/internal/webui/dist`
   存在才能编译(该目录被 gitignore);任何 Go 步骤之前必须先出 dist;
3. `gofmt` 检查、`go vet`、`go build`、`go test`、`go test -race`;
4. android/arm64 交叉编译;
5. `nas.sh` 冒烟测试(机制层 + 运行时层;自产 v9.9.9 测试二进制,覆盖
   install/重装幂等、update、校验和篡改拒绝、卸载保护;Linux 下另跑 start/status/log/health/restart/doctor)。

### Release(`release.yml`,推送 `v*` 标签)

1. 先构建前端,再 `CGO_ENABLED=0 GOOS=android GOARCH=arm64`,
   经 `-ldflags` 注入 `VERSION`(去 v)、短 commit、UTC 构建时间;
2. 生成 `sha256sums.txt`;
3. 经 `softprops/action-gh-release@v2` 发布 `nasd-android-arm64` 与 `sha256sums.txt`
   到 GitHub Release;`nas.sh` 从 `releases/latest/download`(或 `download/v<版本>`)经镜像拉取。

手动发布:`git tag v0.1.0 && git push origin v0.1.0`。

---

## 17. 测试与质量

- **Go 单元/集成测试**(约 2,300 行、20 个文件)覆盖:认证(存储/限流/密码/基准)、
  文件路径安全、分享、插件管理器 + HTTP + 反代端到端、备份、
  市场、守护安全头、DB、代理、电池缓存、safehttp。
- **nas.sh 冒烟测试**(`scripts/smoke-test.sh`)双层:
  - *机制层*(任意 bash):目录创建、二进制落盘、重装幂等、SHA256 门禁(篡改校验和拒绝且无副作用)、
    强制替换生成 `.bak`、同版本跳过、卸载需 `-y`;
  - *运行时层*(Linux/Termux/WSL2):start / status / log / health / restart / 运行中
    `update -f`(优雅停止 → 替换 → 重启 → `.bak`)/ doctor。
- **CI** 跑格式 + 静态检查 + 构建 + 测试 + race + android 交叉编译 + 冒烟。
- 常用命令:`make build`(本机)、`make android`、`make test`、`make check`(全量门禁)、`make tidy`、`make clean`。

---

## 18. 关键设计决策

| 决策点 | 结论 | 理由 |
|---|---|---|
| 插件加载方式 | 独立 Go 二进制(非 WASM/Lua/编译期) | 开发零学习成本、原生性能、进程隔离、可热更新 |
| 核心功能归属 | 内建在 nasd | 低内存、单进程、Termux 友好、部署简单 |
| 管理方式 | 单一脚本 nas.sh(SIGTERM/health/日志直读) | 无需额外 Go 管理二进制;零编程能力即可运维 |
| 职责单点 | nas.sh 只管理主框架生命周期;插件全权由 nasd 控制(Web UI) | 单一管理入口,避免两套命令/两处状态 |
| 进程守护 | flock 单实例锁 + nas.sh nohup 后台(nasd 日志自带落盘) | 防双实例竞态;崩溃后由 nas.sh start 重新拉起 |
| 插件内存策略 | 懒加载 + 空闲回收 | 常驻内存不随插件数量增长 |
| 通信协议 | 用户通道 HTTP :7531(唯一对外) | 无本地管理 socket,精简暴露面 |
| SQLite 驱动 | modernc.org/sqlite(纯 Go) | `CGO_ENABLED=0` 静态编译,Termux 无 C 链也能构建 |

---

## 19. 历史与路线

六个里程碑全部完成(✅):

- **M1** 骨架:`go.mod`、nasd 守护骨架、nas.sh 生命周期(start/stop/status)
- **M2** 认证中心 + 前端壳(登录页/布局/HTMX)+ SQLite 会话
- **M3** 内建模块:文件管理 + 系统监控(轮询看板)
- **M4** 插件系统:管理器 API + 注册协议 + 反向代理 + 懒加载(+ download 插件)
- **M5** 备份中心 + 安全加固
- **M6** 原子更新流程 + 插件市场 + PWA(+ Tailscale 文档引导)

历史要点(据 git log):M1–M6 分里程碑提交;移除早期 `nasm` 管理模块改为 nas.sh 全周期管理;
存储型 XSS/SSRF 加固;登录限流;CI 前端先行排序;UNC/路径处理修复;nas.sh 镜像前缀修复;CI 冒烟覆盖。

后续自然演进方向:落地 `keep_copies` 多份轮转;远程市场索引刷新;多用户;修改密码页;
插件 WebSocket 代理支持。

---

## 20. 常见问题与排障

**Q: `nas.sh` 提示「下载失败」/ SHA256 校验失败。**
A: 受限网络下可设置 `NAS_MIRROR` 为可用加速镜像或置空直连 GitHub;或设 `NAS_DIST_URL` 指向镜像。
校验失败是有意中止——说明下载损坏或被篡改。

**Q: 浏览器打不开 Web UI。**
A: 先 `bash nas.sh status` 确认在运行及端口;确保手机与电脑同一局域网;Termux 需授予网络权限,
建议将应用加入电池优化白名单并 `termux-wake-lock` 防休眠;远程访问建议走 Tailscale/隧道而非直暴露 7531。

**Q: 登录提示「尝试过于频繁,请稍后再试」。**
A: 触发了每 IP 5 次/15 分钟限流,等待解锁即可。若在反向代理之后,应开启 `trust_proxy`
避免所有用户归到代理单一 IP;直连部署切勿开启(可被伪造头绕过)。

**Q: 插件显示 `crash-loop`。**
A: 到「插件」页查看 last_err/日志,修复后先 Stop(复位)再 Start。

**Q: 想更新又保留旧版本。**
A: `nas.sh update` 会保留最近一份 `bin/nasd.bak`,需要时手动换回并重启。

**Q: 我的数据在哪里?**
A: 一切都在 `~/nas/`:`data/nas.db`(会话/分享/备份任务)、`data/config.json`、`files/`。
备份 = 拷贝 `~/nas/`(插件二进制可重新从市场下载)。

**Q: 我能在普通桌面 Linux 上跑吗?**
A: 可以——`make build` 后 `NAS_ROOT=/tmp/nas ./bin/nasd -root /tmp/nas`,用 `bash nas.sh ...`
管理。Windows 也可用于开发(单实例用互斥量),
但完整测试覆盖需 Linux/WSL2/Termux。

---
*本文档根据撰写时仓库全量分析生成(README v2.x、nas.sh 2.1.0、go.mod 依赖)。*
