# Termux NAS

在 Termux(Android 上的 Linux 环境)中运行的**可插拔移动端 NAS**。

- **架构**:单一主框架守护进程 `nasd` + 插件(独立二进制);仓库根脚本 `nas.sh`
  全周期管理 nasd 的生命周期(安装/更新/启停/状态/日志/卸载)
- **技术栈**:Go + Fiber + SQLite(WAL)+ HTMX + Tailwind(daisyUI)
- **部署环境**:Termux,无 root、高位端口、termux-services 守护;**一键脚本 `nas.sh`
  安装/更新/启停,无需手机安装 Go**
- **当前阶段**:M3 文件管理 + 系统监控(见 [里程碑](#里程碑))

## 目录结构

```
~/nas/                          # 单一部署根(备份 = 拷贝目录)
├── nas.sh                      # ★ 一键部署/更新/管理脚本(Termux 首选,无需 Go 工具链)
├── src/                        # Go 源码(本仓库)
│   ├── cmd/nasd/               # 主框架守护进程(单一二进制)
│   ├── internal/
│   │   ├── config/             # 部署根解析 + data/config.json
│   │   ├── daemon/             # nasd 核心:HTTP/DB/插件扫描/生命周期
│   │   ├── lock/               # 单实例锁(flock / Windows 互斥量)
│   │   ├── version/            # 版本信息(构建时注入)
│   │   └── webui/              # 嵌入前端静态资源(单二进制)
│   ├── scripts/build.sh        # 构建脚本(host / android 交叉编译)
│   ├── scripts/smoke-test.sh   # nas.sh 冒烟测试(机制层全平台/运行时层需 Linux)
│   ├── termux-service/         # runit 服务脚本模板
│   └── Makefile
├── .github/workflows/          # CI(ci.yml)+ 发布流水线(release.yml)
├── bin/                        # 构建产物或 nas.sh 下载的二进制(nasd)
├── plugins/                    # 插件二进制(M4)
├── data/                       # nas.db / config.json / logs/(运行时生成)
└── run/                        # 单实例锁 run/nas.lock(运行时生成)
```

## 快速开始

### 本机开发(Windows / Linux / macOS)

```bash
cd src
make build          # 构建 nasd 到 ../bin/
# 终端 A:启动守护进程(Ctrl+C 即优雅停止)
NAS_ROOT=/tmp/nas ./bin/nasd -root /tmp/nas
# 终端 B:用 nas.sh 管理生命周期(Linux/macOS;Windows 上直接 Ctrl+C)
bash ../nas.sh status   # 或: ../bin/nasd -version
bash ../nas.sh log -n 20
bash ../nas.sh stop
```

> 生命周期由 `nas.sh` 统一管理(SIGTERM 优雅停止 / HTTP 健康探活 / 日志文件直读),
> 不再需要任何 Go 管理 CLI 或本地管理 socket。

### Termux 部署(推荐:一键脚本,无需装 Go)

在手机 Termux 里**不需要安装 Go 工具链、不需要手动拷贝文件**。`nas.sh` 会自动:
创建 `~/nas` 目录结构 → 从 GitHub Releases 拉取 android/arm64 预编译二进制 →
SHA256 校验 → 赋予可执行权限 → 安装(可选注册开机自启)。

```bash
pkg install curl                # 首次:补齐依赖
curl -LO https://raw.githubusercontent.com/LiquorXR/Termux-NAS/main/nas.sh
bash nas.sh install --service   # 安装 + 注册 runit 开机自启(termux-services)
bash nas.sh start               # 启动 nasd
```

浏览器访问 `http://<手机局域网IP>:7531`。

> 首次启动 nasd 自动生成 `data/config.json`(默认端口 7531),随后按 Web UI
> 引导创建管理员账号即可。

**常用命令**

| 命令 | 作用 |
|------|------|
| `bash nas.sh install [--service]` | 安装/修复(可选注册开机自启) |
| `bash nas.sh update [-f] [版本]` | 更新到最新(或指定 `v<版本>`),自动校验/备份/回滚 |
| `bash nas.sh start` / `stop` / `restart` | 启动 / 优雅停止 / 重启 |
| `bash nas.sh status` / `log [-n N]` | 状态 / 查看日志尾部 |
| `bash nas.sh doctor` | 环境体检(二进制/目录/健康端口/磁盘) |
| `bash nas.sh uninstall [-y]` | 卸载(默认只打印计划,需 `-y` 才删除数据) |
| `bash nas.sh self-update` | 更新 nas.sh 脚本自身 |

**版本发布与更新**:推送 `v*` 标签即触发 CI 自动交叉编译并发布
`nasd-android-arm64` 与 `sha256sums.txt` 两个资产。
`bash nas.sh update` 默认更新到最新 Release;指定版本如 `bash nas.sh update 0.2.0`。
更新全程:下载 → SHA256 校验 → 优雅停止(SIGTERM)→ 原子替换(旧版保留 `.bak`)→
重启并健康检查 → 失败自动回滚。

### Termux 源码构建(贡献者/离线回退)

若需自行构建(如无网络或定制),须先 `pkg install golang`:

```bash
cd ~/nas/src && make android        # 交叉编译 android/arm64 静态二进制(含前端)
# 产物在 ../bin/nasd,再用 nas.sh 后续流程管理即可
mkdir -p $PREFIX/var/service/nasd/log
cp termux-service/nasd-run.sh $PREFIX/var/service/nasd/run
chmod +x $PREFIX/var/service/nasd/run
sv-enable nasd                      # 开机自启(Termux:Boot)
sv start nasd                       # 或: bash nas.sh start
```

## 通信与生命周期管理

| 通道/方式 | 用途 | 暴露面 |
|------|------|--------|
| 用户通道 `:7531` HTTP | Web UI / API(需登录) | 局域网 / Tailscale |
| `nas.sh`(SIGTERM 优雅停止) | 生命周期控制:启停/重启/更新 | 仅本机(Termux 命令行) |
| `nas.sh`(`/health` + 日志文件直读) | 探活 / 状态 / 日志 | 仅本机 |

主程序只有一个二进制 `nasd`;不保留任何本地管理 socket 或管理 CLI。
插件操作统一走用户通道 HTTP(Web UI「插件管理」页,需登录)。

## 里程碑

| 里程碑 | 内容 | 状态 |
|--------|------|------|
| **M1** | 项目骨架:nasd 守护、生命周期管理(nas.sh)、start/stop/status | ✅ |
| **M2** | 认证中心 + 前端壳(登录页/布局/HTMX)+ SQLite 会话 | ✅ |
| **M3** | 内建模块:文件管理 + 系统监控(HTMX 轮询看板) | ✅ |
| **M4** | 插件系统:管理器 API + 注册协议 + 反代 + 懒加载 + download 插件 | ✅ |
| **M5** | 服务控制 + 备份中心 + 安全加固 | ✅ |
| **M6** | 原子更新流程 + 插件市场 + PWA + Tailscale 集成 | ✅ |

## M6 原子更新 + 插件市场 + PWA(已实现)

### 原子更新(nas.sh update)
- 单二进制 `nasd`;`bash nas.sh update`:下载 → SHA256 校验 → SIGTERM 优雅停止
  → 原子替换(旧版 `.bak` 备份)→ 重启 + 健康检查 → 失败自动回滚
- `update -f`:跳过版本检查强制更新;版本相同自动跳过
- 生命周期由 `nas.sh` 全周期管理(SIGTERM/health/日志直读),不再依赖管理 CLI

### 插件市场
- `internal/market`:内嵌官方市场索引(go:embed),download/alist/media/photos
- API:`GET /api/market`(浏览+已装状态)、`POST /api/market/install`(一键安装)
- Web UI:「市场」页(卡片浏览/安装状态/一键安装)

### PWA
- manifest.json(standalone/主题色/图标)+ icon.svg + service worker(离线壳缓存)
- 导航请求网络优先失败回退缓存;静态资源缓存优先;API 不缓存

### Tailscale 集成
- 文档引导:安装 Tailscale 后直接以局域网 IP 访问,或通过 Tailscale 分配的内网 IP 远程访问(见开发文档 §8 远程访问)

## M4 插件系统(已实现)

- **插件管理器**:状态机(stopped/starting/running/stopping/crashed/crash-loop)、进程生命周期、跨平台可执行判定(扩展名 + MZ/ELF/shebang 文件头探测)
- **注册协议**:插件启动后向 stdout 输出注册 JSON(id/name/version/port/nav/icon),5s 超时判失败
- **崩溃恢复**:自动重启(带退避),连续 3 次进入 crash-loop,Stop 人工复位
- **懒加载**:首次访问 `/p/<id>/*` 自动启动,空闲超时回收(默认 10 分钟)
- **反向代理**:`/p/<id>/*` → `127.0.0.1:<插件端口>/*`,统一鉴权,保留路径与查询参数
- **管理 API**:`/api/plugins/*` 列表/安装(上传或 URL)/启停/重启/卸载/日志
- **Web UI**:「插件」页支持安装、启停、重启、卸载,状态 3s 轮询

### 插件开发速览

插件是独立可执行文件,遵循注册协议:

```go
// 插件启动后向 stdout 输出一行注册 JSON 即可被 nasd 接管
fmt.Printf(`{"id":"download","name":"下载中心","version":"1.0.0",
  "port":%d,"nav":"下载","icon":"download"}`+"\n", actualPort)
```

- 监听 `127.0.0.1:<port>`(可由 `--port` 指定,0 为随机)
- 提供 `GET /health` 返回 200(供探活)
- 打包为 `.tar.gz`(内含单个可执行文件),在 Web UI「插件」页上传安装

## M5 服务控制 + 备份中心 + 安全加固(已实现)

### 服务控制
- `internal/svc`:基于 termux-services(runit)的服务启停/重启/自启,解析 `sv status` 输出
- 内置服务目录:sshd / samba / nginx / aria2 / cron / mysql
- 平台适配:Termux/Linux 真实执行;Windows 开发环境自动模拟(MockRunner)
- API:`/api/svc/list|start|stop|restart|autostart`;Web UI「服务」页(5s 轮询)

### 备份中心
- `internal/backup`:任务 CRUD(SQLite 持久化)+ cron 调度 + 执行器 + 完成通知
- 调度:5 字段 cron 表达式(支持 `* / , -`),每分钟检查到期任务
- 执行:rsync 优先(增量/远程地址),降级本地复制;支持恢复(方向反转)
- 完成通知:termux-notification(可注入替换)
- API:`/api/backup/jobs|run|restore`;Web UI「备份」页

### 安全加固
- 登录失败限流:按 IP 连续 5 次失败锁定 15 分钟(429 + Retry-After)
- 安全响应头:CSP / X-Frame-Options DENY / nosniff / no-referrer
- 登录页/设置页 `Cache-Control: no-store`
- 文件上传单文件上限 256 MiB
- 分享链接下载强制 attachment(文件根内 html/svg/xml/js 不可同源内联渲染,防存储型 XSS)
- 插件/更新包下载经安全 HTTP 客户端(超时 + 大小上限 + 私网/回环拦截,防 SSRF)
- 会话 cookie:HttpOnly + SameSite=Lax + Max-Age(与 7 天 TTL 对齐)

### 安全部署选项(`data/config.json`)
| 配置项 | 说明 | 默认 |
|--------|------|------|
| `trust_proxy` | 部署在可信反向代理之后时开启,登录限流按 X-Forwarded-For 计数;直连开启可被伪造头绕过限流 | `false` |
| `force_https` | 通过 HTTPS 反代/隧道访问时开启,会话 cookie 加 `Secure` 标记 | `false` |

## 关键设计决策

- **SQLite 用 `modernc.org/sqlite`**(纯 Go 驱动)——`CGO_ENABLED=0` 静态编译的关键,Termux 无 C 编译链也能构建
- **生命周期由 `nas.sh` 全周期管理**:SIGTERM 优雅停止 + HTTP `/health` 探活 + 日志文件直读,无本地管理 socket、无管理 CLI
- **前端静态资源 `go:embed`** 进 nasd,单二进制自带全部界面
- **职责单点**:nas.sh 只管理 nasd 生命周期;插件全权由 nasd 控制(Web UI)

## 文档

- `场景调研.md` — 26 个产品方向 × 六维评分(决策依据)
- `技术栈建议.md` — Go + SQLite + HTMX 选型论证
- `NAS框架开发文档.md` — 本文档来源(架构/API/安全/里程碑)
