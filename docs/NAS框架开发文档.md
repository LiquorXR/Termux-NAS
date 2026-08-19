# Termux NAS 框架开发文档

> 版本:0.1(设计稿)
> 日期:2026-08-19
> 架构:**管理模块(nasm)+ 主框架(nasd,内建 NAS 必要功能)+ 插件扩展(独立二进制)**
> 技术栈:Go + Fiber + SQLite + HTMX + Tailwind(daisyUI)
> 部署环境:Termux(Android 上的 Linux 环境),单进程 + 可选插件进程

---

## 1. 项目概述

### 1.1 目标

在 Termux 中构建一个**可插拔的移动端 NAS 系统**:

- **高性能、低资源占用**:主框架为 Go 单二进制,常驻内存约 15-30MB
- **完全兼容移动端 Termux**:无 root 可用,高位端口,termux-services 守护
- **可扩展**:核心功能内建,扩展功能以独立二进制插件形式动态加载
- **易管理**:管理模块(nasm)只负责主框架的生命周期(启动/停止/更新);插件的一切操作由主框架(nasd)统一控制

### 1.2 核心设计原则

1. **混合架构**:核心功能(认证/文件/监控/服务控制/备份)内建在 nasd;扩展功能(下载/云盘/媒体/第三方)做成插件
2. **双二进制、职责单点**:nasm 只管理 nasd 本体(启动/停止/更新);nasd 全权控制插件(安装/卸载/启停/更新)
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
│             nasd · 主框架(常驻守护进程)                     │
│                                                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│  │ 认证中心  │ │ 文件管理  │ │ 系统监控  │ │ 服务控制  │    │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘    │
│  ┌──────────┐ ┌────────────────────────────────────┐     │
│  │ 备份中心  │ │ 插件管理器(安装/卸载/启停/更新/懒加载) │     │
│  └──────────┘ └────────────────────────────────────┘     │
│  SQLite · 配置 · 日志 · 管理 API (Unix socket)             │
└──────────────┬───────────────────────────────▲───────────┘
               │ 管理API(仅生命周期)           │ 用户请求 /p/<id>/*
┌──────────────▼───────────────┐   ┌──────────┴───────────┐
│  nasm · 管理模块(CLI)         │   │  plugins/ 插件二进制   │
│  start/stop/restart/update   │   │  download :18002     │
│  不管理插件 · 不常驻           │   │  alist :18003        │
└──────────────────────────────┘   │  media  :18004       │
                                   │  (nasd 全权控制)      │
                                   └──────────────────────┘
```

### 2.1 三个组成部分

| 组件 | 形态 | 职责 | 常驻 |
|------|------|------|------|
| **nasm** | CLI 二进制 | 仅管理主框架本体:启动/停止/重启/更新/状态 | 否(用完即走) |
| **nasd** | 守护进程二进制 | 运行时全部能力:HTTP 服务、内建 NAS 功能、**插件全权管理** | 是(runit 托管) |
| **插件** | 独立二进制 | 扩展功能:下载/云盘/媒体等;生命周期完全由 nasd 控制 | 按需(懒加载) |

> **职责单点原则**:插件的安装、卸载、启停、更新**只能**通过 nasd(Web UI 的「插件管理」页或用户通道 API)操作,nasm 不感知插件存在。nasm 与插件之间不存在任何直接交互。

### 2.2 两条通信通道(严格分离)

| 通道 | 路径 | 用途 | 暴露面 |
|------|------|------|--------|
| 用户通道 | `:7531` HTTP | 浏览器访问 Web UI / API | 局域网 / Tailscale |
| 管理通道 | `~/nas/run/nas.sock` Unix socket | nasm ↔ nasd 管理指令 | **仅本机** |

---

## 3. 目录结构

```
~/nas/                          # 单一部署根
├── bin/
│   ├── nasm                    # 管理模块(用户 PATH 中的命令)
│   └── nasd                    # 主框架守护进程
├── plugins/                    # 插件二进制(可执行文件)
│   ├── download                # 下载中心插件
│   ├── alist                   # 云盘聚合插件
│   └── media                   # 媒体服务插件
├── data/                       # 运行时数据
│   ├── nas.db                  # SQLite(配置/插件注册/系统状态)
│   ├── config.json             # 主框架配置
│   └── logs/
│       ├── nasd.log            # 主框架日志
│       └── plugin-<id>.log     # 各插件日志(统一收集)
└── run/
    └── nas.sock                # 管理 socket(运行时创建)
```

**约定**:
- `nasm` 应链接到 PATH(如 `ln -s ~/nas/bin/nasm $PREFIX/bin/nasm`)
- 备份 `data/` 目录 = 备份全部配置与状态;`plugins/` 可重新下载
- 更新只动对应二进制文件,互不干扰

---

## 4. 管理模块 nasm(CLI)

> **职责边界**:nasm 只负责主框架 nasd 的**生命周期管理**(启动/停止/重启/更新/状态查询)。
> **不包含插件管理**——插件的安装、卸载、启停、更新全部由 nasd 在 Web UI(「插件管理」页)中控制。

### 4.1 命令设计

```
nasm start                     # 启动 nasd 守护进程(runit 托管)
nasm stop                      # 优雅停止 nasd
nasm restart                   # 重启 nasd
nasm status                    # 查看 nasd 运行状态(版本/uptime/健康)
nasm log [lines]               # 查看主框架日志
nasm update [version]          # 更新主框架 nasd
nasm self-update               # 更新 nasm 自身
nasm version                   # 版本信息
```

> 插件状态不通过 nasm 查看——请在 Web UI 的「插件管理」页查看。

### 4.2 与 nasd 的通信(管理 API · 仅生命周期)

- 通过 `~/nas/run/nas.sock`(Unix socket)发起 JSON-RPC 请求
- 管理 API 仅监听本地 socket,**不暴露公网**
- 请求鉴权:管理 token(首次初始化时生成,存于 `data/config.json`)

**管理 API 端点(JSON-RPC)**:

```jsonc
// 请求
{ "method": "daemon.status", "params": {}, "id": 1 }
// 响应
{ "result": { "running": true, "version": "0.1.0", "uptime": 3600 }, "id": 1 }
```

| 方法 | 参数 | 说明 |
|------|------|------|
| `daemon.status` | - | 返回 nasd 运行状态、版本、uptime、健康检查结果 |
| `daemon.stop` | - | 优雅停止(先停所有插件,再退出) |
| `daemon.enterUpdate` | - | 进入更新模式(暂停服务,等待替换) |
| `log.tail` | `{lines}` | 获取主框架日志尾部 |

> 管理 API **只**包含主框架生命周期方法。插件相关操作不在 socket 通道中暴露,统一走用户通道(Web UI / HTTP API,需登录)。

### 4.3 更新主框架流程(nasm update)

```
① nasm 下载新版 nasd → 临时文件 nasd.new
② 校验:版本号 + SHA256 校验和(防损坏/防篡改)
③ 通过 socket 调用 daemon.enterUpdate → 旧 nasd 优雅退出
④ 原子替换:rename(nasd.new → bin/nasd)
⑤ nasm 重新启动新 nasd
⑥ 验证健康检查 → 完成(失败则回滚旧版本)
全程 nasm 自身在线,不存在"更新自己"的问题
```

### 4.4 插件管理(不在 nasm 中)

> 插件的安装、卸载、启停、更新、日志查看**全部由 nasd 的「插件管理」模块控制**,操作入口为 Web UI 的「插件管理」页面(需登录),详见 [5.3 插件管理器](#53-插件管理器插件全权控制--核心组件)。nasm 不提供任何插件命令。

---

## 5. 主框架 nasd

### 5.1 进程生命周期

```
启动:Termux:Boot → nasm start → runit 托管 nasd
运行:nasm start 拉起 → 加载配置 → 打开 SQLite → 扫描插件(登记,不启动)
     → 启动 HTTP :7531 → 监听管理 socket(仅生命周期方法)
停止:管理 API daemon.stop → 逐个停止插件 → 关闭 HTTP → 持久化 → 退出
```

### 5.2 内建 NAS 必要功能模块

| 模块 | 功能 | 关键实现 |
|------|------|---------|
| **认证中心** | 登录/会话/权限/CSRF | SQLite 会话表 + cookie;首次启动生成管理员账号 |
| **文件管理** | 浏览/上传/下载/删除/重命名/分享链接/搜索 | `os` + `io/fs` 操作 `~/storage` 共享目录;分享链接带短 token |
| **系统监控** | CPU/内存/温度/电量/磁盘/网络流量 | 读 `/proc` + termux-api(`termux-battery-status` 等);HTMX 每 3s 轮询 |
| **服务控制** | Samba/SSH/nginx/aria2 等启停、自启、状态 | 进程管理封装(`Svc()` API);基于 termux-services |
| **备份中心** | 定时备份/rsync 同步/GPG 加密/完成通知 | cron 调度 + `termux-notification` |
| **插件管理** | 插件安装/卸载/启停/更新/状态(Web UI) | 见 5.3;插件的一切操作仅通过本模块 |

**内建模块与插件的关系**:内建模块直接以 Go 包形式编译进 nasd,注册路由与插件同走一套注册表(框架内部接口),对外表现一致——内建模块永不卸载,插件可装卸。

### 5.3 插件管理器(插件全权控制 · 核心组件)

> **插件操作唯一入口**:插件的安装、卸载、启停、更新、状态查看,全部通过 nasd 的「插件管理」模块完成,入口为 Web UI 的「插件管理」页面(用户通道 HTTP API,需登录)。nasm 不提供任何插件命令,管理 socket 也不暴露插件方法。

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
- 统一鉴权:所有 `/p/*` 请求先经过登录校验;透传管理 token 供插件内部调用
- WebSocket/SSE 透传(实时监控、推送)

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

## 7. 内建模块 API 概览(用户通道 :8080)

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
| 管理通道 | 仅 Unix socket 本机访问 + 管理 token |
| 插件鉴权 | 用户请求经 nasd 校验后透传 token;插件不暴露公网端口(监听 127.0.0.1) |
| 分享链接 | 短随机 token,可设过期时间;路径穿越防护(所有路径规范化校验) |
| 远程访问 | Tailscale / Cloudflare Tunnel 加密隧道,不建议直接暴露 8080 |
| 上传安全 | 大小限制、文件名消毒、MIME 校验 |

---

## 9. 开发计划(里程碑)

| 里程碑 | 内容 | 交付物 |
|--------|------|--------|
| **M1** | 项目骨架:go.mod、nasm CLI 框架、nasd 守护骨架、Unix socket 管理通道、start/stop/status | 可运行的双二进制骨架 |
| **M2** | 认证中心 + 前端壳(登录页/布局/导航/HTMX)+ SQLite | 能登录的空壳 NAS |
| **M3** | 内建模块:文件管理 + 系统监控(HTMX 轮询看板) | NAS 核心功能可用 |
| **M4** | 插件系统:管理器(安装/卸载/启停/更新 API)+ 注册协议 + 反代 + 懒加载 + download 插件验证 | 插件全链路跑通,Web UI 插件管理页可用 |
| **M5** | 服务控制 + 备份中心 + 安全加固 | 完整 NAS |
| **M6** | nasm update 更新流程 + 插件市场 + PWA + Tailscale 集成 | 可日常使用 |

---

## 10. Termux 部署要点

```bash
# 依赖
pkg install golang termux-services termux-api

# 构建
cd ~/nas/src && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/nasd ./cmd/nasd
cd ~/nas/src && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/nasm ./cmd/nasm

# 服务注册(runit)
pkg install termux-services
mkdir -p $PREFIX/var/service/nasd/log
# 编写 run 脚本启动 nasd,sv-enable nasd 实现开机自启

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
| 管理模块分离 | nasm 独立二进制 | 更新 nasd 不影响管理工具,权限分离 |
| **职责单点** | **nasm 只管理主框架生命周期;插件全权由 nasd 控制(Web UI)** | 单一管理入口,避免两套命令/两处状态,操作一致性好 |
| 管理通道 | Unix socket(仅本机),仅暴露生命周期方法 | 管理操作不暴露公网;插件操作走用户通道(需登录) |
| 插件内存策略 | 懒加载 + 空闲回收 | 常驻内存不随插件数量增长 |
| 通信协议 | 用户通道 HTTP :8080 + 管理 JSON-RPC socket | 两条通道职责严格分离 |
