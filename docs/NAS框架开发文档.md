# Termux NAS 框架开发文档

> 版本:0.2(架构更新:nasm 已移除,生命周期全由 nas.sh 管理)
> 日期:2026-08-19
> 架构:**主框架(nasd,单一二进制,内建 NAS 必要功能)+ 一键管理脚本(nas.sh)+ 插件扩展(独立二进制)**
> 技术栈:Go + Fiber + SQLite + 前端 Vite 工程化(HTMX + 原生 JS + 手写设计系统)
> 部署环境:Termux(Android 上的 Linux 环境),单进程 + 可选插件进程
>
> 📚 配套文档:
> - 用户向快速开始 → 仓库根 `README.md` / `README.en.md`
> - 完整项目文档(中文)[docs/PROJECT_GUIDE.zh-CN.md](PROJECT_GUIDE.zh-CN.md)
> - Complete Project Documentation (English) [docs/PROJECT_GUIDE.en-US.md](PROJECT_GUIDE.en-US.md)

---

## 1. 项目概述

### 1.1 目标

在 Termux 中构建一个**可插拔的移动端 NAS 系统**:

- **高性能、低资源占用**:主框架为 Go 单二进制,常驻内存约 15-30MB
- **完全兼容移动端 Termux**:无 root 可用,高位端口,termux-services 守护
- **可扩展**:核心功能内建,扩展功能以独立二进制插件形式动态加载
- **易管理**:一键脚本(nas.sh)负责主框架 nasd 的生命周期(安装/更新/启停/状态/日志/卸载);插件的一切操作由主框架(nasd)统一控制

### 1.2 核心设计原则

1. **混合架构**:核心功能(认证/文件/监控/服务控制/备份)内建在 nasd;扩展功能(下载/云盘/媒体/第三方)做成插件
2. **单二进制、职责单点**:nas.sh 只管理 nasd 本体(安装/更新/启停/状态/日志/卸载);nasd 全权控制插件(安装/卸载/启停/更新)
3. **插件进程级隔离**:插件独立崩溃不影响主框架,可独立更新
4. **懒加载**:插件按需启动(点开页面才启动进程),常驻内存不增加
5. **单部署根**:`~/nas/` 一个目录包含一切,备份 = 拷贝目录

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
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│  │ 认证中心  │ │ 文件管理  │ │ 系统监控  │ │ 服务控制  │    │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘    │
│  ┌──────────┐ ┌────────────────────────────────────┐     │
│  │ 备份中心  │ │ 插件管理器(安装/卸载/启停/更新/懒加载) │     │
│  └──────────┘ └────────────────────────────────────┘     │
│  SQLite · 配置 · 日志 · 单实例锁(run/nas.lock)             │
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
|------|------|------|------|
| **nas.sh** | bash 脚本(仓库根) | 主框架全生命周期:安装/更新/启停/状态/日志/卸载 | 否(用完即走) |
| **nasd** | 守护进程二进制 | 运行时全部能力:HTTP 服务、内建 NAS 功能、**插件全权管理** | 是(runit 托管) |
| **插件** | 独立二进制 | 扩展功能:下载/云盘/媒体等;生命周期完全由 nasd 控制 | 按需(懒加载) |

> **职责单点原则**:插件的安装、卸载、启停、更新**只能**通过 nasd(Web UI 的「插件管理」页或用户通道 API)操作,nas.sh 不感知插件存在。nas.sh 与插件之间不存在任何直接交互。

### 2.2 通信与生命周期

| 通道/方式 | 路径/机制 | 用途 | 暴露面 |
|------|------|--------|--------|
| 用户通道 | `:7531` HTTP | 浏览器访问 Web UI / API | 局域网 / Tailscale |
| 生命周期控制 | SIGTERM 优雅停止 + `/health` 探活 + 日志文件直读 | nas.sh 管理主程序 | 仅本机(Termux 命令行) |

> 不保留任何本地管理 socket 或管理 CLI;主程序只有一个二进制 `nasd`。

---

## 3. 目录结构

```
~/nas/                          # 单一部署根
├── bin/
│   └── nasd                    # 主框架守护进程(单一二进制)
├── plugins/                    # 插件二进制(可执行文件)
│   ├── download                # 下载中心插件
│   ├── alist                   # 云盘聚合插件
│   └── media                   # 媒体服务插件
├── data/                       # 运行时数据
│   ├── nas.db                  # SQLite(配置/插件注册/系统状态)
│   ├── config.json             # 主框架配置
│   └── logs/
│       └── nasd.log            # 主框架日志
└── run/
    └── nas.lock                # 单实例锁(nasd flock,运行时创建)
```

**约定**:
- 备份 `data/` 目录 = 备份全部配置与状态;`plugins/` 可重新下载
- 更新只动 `bin/nasd` 一个二进制,`.bak` 保留旧版以便回滚

---

## 4. 生命周期管理(nas.sh,单一脚本)

> **职责边界**:nas.sh 只负责主框架 nasd 的**生命周期管理**(安装/更新/启停/状态/日志/卸载)。
> **不包含插件管理**——插件的安装、卸载、启停、更新全部由 nasd 在 Web UI(「插件管理」页)中控制。

### 4.1 命令设计

```
bash nas.sh install [--service]  # 安装(可选注册 runit 开机自启)
bash nas.sh update [-f] [版本]    # 更新到最新(或指定 v<版本>)
bash nas.sh start|stop|restart   # 启动/优雅停止/重启
bash nas.sh status|log [-n N]    # 状态/日志尾部
bash nas.sh doctor               # 环境体检
bash nas.sh uninstall [-y]       # 卸载(需 -y 才删数据)
bash nas.sh self-update          # 更新 nas.sh 自身
```

> 插件状态不通过 nas.sh 查看——请在 Web UI 的「插件管理」页查看。

### 4.2 与 nasd 的交互(无需管理 socket)

- **停止**:向 nasd 进程发送 SIGTERM(nasd 收到后走 `ctx.Done()` 做优雅退出:
  先停全部插件进程 → HTTP `ShutdownWithTimeout` → 释放单实例锁)
- **探活/状态**:请求用户通道 `GET /health`(返回 status/version/uptime/pid/port)
- **日志**:直读 `data/logs/nasd.log` 尾部
- **单实例**:nasd 启动时对 `run/nas.lock` 做 flock,第二个实例直接失败退出
- **不暴露任何本地管理 socket/端口**,仅本机 Termux 命令行可控

### 4.3 更新主框架流程(nas.sh update)

```
① nas.sh 下载新版 nasd → 临时文件(SHA256 校验,防损坏/防篡改)
② 若运行中:SIGTERM 优雅停止,等待进程退出、单实例锁释放
③ 原子替换:旧版 → nasd.bak;新版 → bin/nasd;chmod +x
④ 重新启动新 nasd,轮询 /health 直到就绪
⑤ 就绪即完成(清理 .bak);启动失败则回滚 .bak 旧版并重启
```

### 4.4 插件管理(不在 nas.sh 中)

> 插件的安装、卸载、启停、更新、日志查看**全部由 nasd 的「插件管理」模块控制**,操作入口为 Web UI 的「插件管理」页面(需登录),详见 [5.3 插件管理器](#53-插件管理器插件全权控制--核心组件)。nas.sh 不提供任何插件命令。

---

## 5. 主框架 nasd

### 5.1 进程生命周期

```
启动:Termux:Boot → nas.sh/ sv → runit 托管 nasd
运行:nasd 启动(flock 单实例锁)→ 加载配置 → 打开 SQLite → 扫描插件(登记,不启动)
     → 启动 HTTP :7531(/health 供探活)
停止:SIGTERM → 逐个停止插件 → 关闭 HTTP(超时)→ 释放单实例锁 → 退出
```

### 5.2 内建 NAS 必要功能模块

| 模块 | 功能 | 关键实现 |
|------|------|---------|
| **认证中心** | 登录/会话/权限/CSRF | SQLite 会话表 + cookie;首次启动生成管理员账号 |
| **文件管理** | 浏览/上传/下载/删除/重命名/分享链接/搜索 | `os` + `io/fs` 操作 `~/storage` 共享目录;分享链接带短 token |
| **系统监控** | CPU/内存/温度/电量/磁盘/网络流量 | 读 `/proc` + termux-api(`termux-battery-status` 等);前端轮询看板 |
| **服务控制** | Samba/SSH/nginx/aria2 等启停、自启、状态 | 进程管理封装(`Svc()` API);基于 termux-services |
| **备份中心** | 定时备份/rsync 同步/GPG 加密/完成通知 | cron 调度 + `termux-notification` |
| **插件管理** | 插件安装/卸载/启停/更新/状态(Web UI) | 见 5.3;插件的一切操作仅通过本模块 |

**内建模块与插件的关系**:内建模块直接以 Go 包形式编译进 nasd,注册路由与插件同走一套注册表(框架内部接口),对外表现一致——内建模块永不卸载,插件可装卸。

### 5.3 插件管理器(插件全权控制 · 核心组件)

> **插件操作唯一入口**:插件的安装、卸载、启停、更新、状态查看,全部通过 nasd 的「插件管理」模块完成,入口为 Web UI 的「插件管理」页面(用户通道 HTTP API,需登录)。nas.sh 只管理主框架生命周期,不提供任何插件命令。

**扫描**:启动时扫描 `~/nas/plugins/` 下所有可执行文件 → 登记元信息(不启动进程)。

**插件管理 API(用户通道,需登录)**:

```
GET  /api/plugins                # 插件列表及状态(安装/运行/停止/崩溃)
POST /api/plugins/install        # 安装(body: {name, source: url|upload})
POST /api/plugins/<id>/start     # 启动插件
POST /api/plugins/<id>/stop      # 停止插件
POST /api/plugins/<id>/restart   # 重启插件
POST /api/plugins/<id>/update    # 更新插件(重新下载替换)
DELETE /api/plugins/<id>         # 卸载插件(停止进程 + 删除文件)
GET  /api/plugins/<id>/log       # 插件日志
```

**安装插件流程(Web UI → nasd)**:
```
① 用户在「插件管理」页上传插件包(.tar.gz)或填写插件源 URL
② nasd 下载/解压 → 校验校验和 + 可执行权限 → 放入 ~/nas/plugins/<name>
③ nasd 重新扫描 → 登记到插件注册表 → 导航栏出现入口 → 完成
卸载:Web UI 点击卸载 → nasd 停止进程 → 删除文件 → 重新扫描
更新:Web UI 点击更新 → nasd 下载新版 → 停止旧进程 → 原子替换 → 重启
```

**注册协议**(插件进程 ↔ nasd):

```
① nasd 启动插件进程,传入参数:--name=<id> --port=0(随机)
② 插件启动后监听随机端口,并向 stdout 输出一行 JSON 注册信息:
   {"id":"download","name":"下载中心","version":"1.0.0",
    "port":18002,"nav":"下载","icon":"download"}
③ nasd 读取 stdout 解析 → 登记到插件注册表 → 返回确认
④ 若 5 秒内未注册 → 判定启动失败 → 记录日志并重启(最多 3 次)
```

**懒加载**:
- 插件默认不启动进程(常驻内存 +0)
- 用户首次访问 `/p/<id>/*` 时 → nasd 启动插件进程 → 等待注册 → 代理请求
- 空闲超时(默认 10 分钟)或手动 stop → 停止进程释放资源

**反向代理**:
- 路径映射:`/p/<id>/*` → `http://127.0.0.1:<插件端口>/*`
- 统一鉴权:所有 `/p/*` 请求先经登录校验;转发前**剥离会话 Cookie 与鉴权头**(插件不得冒用 nasd 用户身份);保留原始路径与查询参数
- 当前为 HTTP 反代(`proxy.Do`);不支持 WebSocket 升级,插件实时推送请用轮询

**故障恢复**:
- 插件进程崩溃 → nasd 检测到退出 → 记录日志 → 自动重启(带退避)
- 连续 3 次崩溃 → 标记 `crash-loop` 状态,停止自动重启,等待人工介入

### 5.4 共享能力(框架提供给插件)

| 能力 | 说明 |
|------|------|
| SQLite 访问 | 共享 `data/nas.db`,插件可建自己的表(自动迁移) |
| 配置读写 | `data/config.json`,按插件 ID 分区 |
| 日志 | 统一收集到 `data/logs/plugin-<id>.log` |
| 服务控制 | 插件可调用 `Svc()` 启停系统服务(Samba/aria2 等) |
| 事件总线(可选) | 插件间发布/订阅事件(如"文件已上传"→ 通知插件) |
| 系统信息 | CPU/温度/电量等,插件可查询 |

---

## 6. 插件开发指南

### 6.1 插件 SDK

插件是**独立可执行文件**,通过 `go build` 编译,依赖一个轻量 SDK 包:

```go
// go.mod 中引入(示例,正式版发布后公开)
require github.com/termux-nas/plugin-sdk v0.1.0
```

```go
package main

import (
    "github.com/gofiber/fiber/v2"
    "github.com/termux-nas/plugin-sdk"
)

func main() {
    sdk.Run(pluginSDK.Config{
        ID:      "download",
        Name:    "下载中心",
        Version: "1.0.0",
        Icon:    "download",
        Nav:     "下载",
        RegisterRoutes: func(app *fiber.App, api *sdk.API) {
            app.Get("/", func(c *fiber.Ctx) error {
                return c.SendString("Hello from download plugin")
            })
            app.Get("/tasks", func(c *fiber.Ctx) error { /* ... */ })
        },
    })
}
```

SDK 内部处理:监听随机端口 → 向 stdout 输出注册 JSON → 等待 nasd 确认 → 挂载路由。

### 6.2 插件必须具备的能力

| 要求 | 说明 |
|------|------|
| 遵守注册协议 | 启动后向 stdout 输出注册 JSON |
| 监听随机端口 | 通过 `--port` 参数或 `:0` 自动获取 |
| 响应健康检查 | `GET /health` 返回 200(供 nasd 探活) |
| 独立打包 | 自带所有依赖(Go 静态编译) |

### 6.3 插件清单(规划)

| 插件 | 功能 | 状态 |
|------|------|------|
| download | 下载中心(aria2 集成:添加/暂停/速度/进度) | 规划 |
| alist | 云盘聚合(挂载阿里云盘/百度网盘等) | 规划 |
| media | 媒体服务(音乐/视频流媒体) | 规划 |
| photos | 照片浏览/轻量管理 | 规划 |
| *第三方* | 社区开发者可贡献任意插件 | 开放 |

---

## 7. 内建模块 API 概览(用户通道 :7531)

```
POST /api/auth/login            # 登录
POST /api/auth/logout           # 登出
GET  /api/auth/me               # 当前用户

GET  /api/files/list?path=      # 文件列表
POST /api/files/upload          # 上传
GET  /api/files/download?path=  # 下载
POST /api/files/mkdir           # 建目录
POST /api/files/rename          # 重命名
POST /api/files/delete          # 删除
POST /api/files/share           # 生成分享链接
GET  /api/files/search?q=       # 搜索

GET  /api/monitor/summary       # CPU/内存/温度/电量汇总
GET  /api/monitor/history?r=1h  # 历史曲线数据
GET  /api/monitor/net           # 网络流量

GET  /api/svc/list              # 服务列表及状态
POST /api/svc/start             # 启动服务 (body: {name})
POST /api/svc/stop              # 停止服务
POST /api/svc/autostart         # 设置开机自启

GET  /api/backup/jobs           # 备份任务列表
POST /api/backup/jobs           # 新建任务
POST /api/backup/run            # 立即执行
POST /api/backup/restore        # 恢复

# --- 插件管理(由 nasd 全权控制,需登录) ---
GET    /api/plugins                     # 插件列表及状态
POST   /api/plugins/install             # 安装(body: {name, source})
POST   /api/plugins/<id>/start          # 启动插件
POST   /api/plugins/<id>/stop           # 停止插件
POST   /api/plugins/<id>/restart        # 重启插件
POST   /api/plugins/<id>/update         # 更新插件
DELETE /api/plugins/<id>                # 卸载插件
GET    /api/plugins/<id>/log            # 插件日志

GET  /p/<plugin_id>/*           # 插件路由(反代,统一鉴权)
```

---

## 8. 安全设计

| 项目 | 方案 |
|------|------|
| 认证 | 会话 cookie + CSRF token;密码哈希(Argon2id) |
| 生命周期管理 | 仅本机 Termux 命令行(nas.sh);无本地管理 socket/端口暴露 |
| 插件鉴权 | nasd 统一登录校验;转发前剥离会话 Cookie/鉴权头;插件仅监听 127.0.0.1 |
| 分享链接 | 短随机 token,可设过期时间;路径穿越防护(所有路径规范化校验) |
| 远程访问 | Tailscale / Cloudflare Tunnel 加密隧道,不建议直接暴露 7531 |
| 上传安全 | 大小限制、文件名消毒、MIME 校验 |

---

## 9. 开发计划(里程碑)

| 里程碑 | 内容 | 交付物 |
|--------|------|--------|
| **M1** | 项目骨架:go.mod、nasd 守护骨架、生命周期管理(nas.sh)、start/stop/status | 可运行的单二进制骨架 |
| **M2** | 认证中心 + 前端壳(登录页/布局/导航)+ SQLite | 能登录的空壳 NAS |
| **M3** | 内建模块:文件管理 + 系统监控(轮询看板) | NAS 核心功能可用 |
| **M4** | 插件系统:管理器(安装/卸载/启停/更新 API)+ 注册协议 + 反代 + 懒加载 + download 插件验证 | 插件全链路跑通,Web UI 插件管理页可用 |
| **M5** | 服务控制 + 备份中心 + 安全加固 | 完整 NAS |
| **M6** | 原子更新流程 + 插件市场 + PWA + Tailscale 集成 | 可日常使用 |

---

## 10. Termux 部署要点

### 10.1 一键脚本(推荐,无需手机安装 Go)

仓库根目录 `nas.sh` 自动完成:创建 `~/nas` 目录结构 → 从 GitHub Releases
拉取 android/arm64 预编译二进制 → SHA256 校验 → 赋予可执行权限 → 安装,
并管理 nasd 的启动/停止/重启/状态/日志与原子更新(旧版 `.bak` 备份、失败回滚)。

```bash
# 依赖(仅 base/curl;无需 golang)
pkg install curl

# 安装 + 注册 runit 开机自启
curl -LO https://raw.githubusercontent.com/LiquorXR/Termux-NAS/main/nas.sh
bash nas.sh install --service
bash nas.sh start

# 日常
bash nas.sh status            # 状态
bash nas.sh log -n 50         # 日志
bash nas.sh update            # 更新到最新 Release(优雅停止→替换→重启→回滚)
bash nas.sh update 0.2.0      # 更新到指定版本
bash nas.sh doctor            # 体检
bash nas.sh uninstall -y      # 卸载(需 -y 才删数据)
```

二进制分发:推送 `v*` 标签触发 `.github/workflows/release.yml` 自动交叉编译并发布
`nasd-android-arm64` 与 `sha256sums.txt`。
`nas.sh` 通过 `releases/latest/download`(无需 API/jq)拉取;`NAS_DIST_URL` 可覆盖为镜像。

### 10.2 源码构建(贡献者 / 离线回退)

```bash
# 依赖
pkg install golang termux-services termux-api

# 构建(单一二进制 nasd)
cd ~/nas/src && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/nasd ./cmd/nasd

# 服务注册(runit)
pkg install termux-services
mkdir -p $PREFIX/var/service/nasd/log
# 编写 run 脚本启动 nasd,sv-enable nasd 实现开机自启
# (或直接使用 termux-service/nasd-run.sh 模板)

# 权限与保活
# - 电池设置:Termux 加入"不限制后台"
# - termux-wake-lock 防休眠
# - 建议充电上限 80%(防止长期满电鼓包)
# - 监听 7531 高位端口(无 root 无法用 80/443)
```

---

## 11. 附录:关键决策记录

| 决策点 | 结论 | 理由 |
|--------|------|------|
| 插件加载方式 | 独立 Go 二进制(非 WASM/Lua/编译期) | 开发零学习成本、原生性能、进程隔离、可热更新 |
| 核心功能归属 | 内建在 nasd | 低内存、单进程、Termux 友好、部署简单 |
| 管理方式 | 单一脚本 nas.sh(SIGTERM/health/日志直读) | 无需额外 Go 管理二进制;零编程能力即可运维 |
| **职责单点** | **nas.sh 只管理主框架生命周期;插件全权由 nasd 控制(Web UI)** | 单一管理入口,避免两套命令/两处状态,操作一致性好 |
| 进程守护 | 单实例锁(flock)+ runit(sv)托管 | 防双实例竞态;开机自启与崩溃自动拉起 |
| 插件内存策略 | 懒加载 + 空闲回收 | 常驻内存不随插件数量增长 |
| 通信协议 | 用户通道 HTTP :7531(唯一对外) | 无本地管理 socket,精简暴露面 |
