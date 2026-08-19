# Termux NAS

在 Termux(Android 上的 Linux 环境)中运行的**可插拔移动端 NAS**。

- **架构**:管理模块 `nasm`(CLI)+ 主框架 `nasd`(常驻守护)+ 插件(独立二进制)
- **技术栈**:Go + Fiber + SQLite(WAL)+ HTMX + Tailwind(daisyUI)
- **部署环境**:Termux,无 root、高位端口、termux-services 守护
- **当前阶段**:M3 文件管理 + 系统监控(见 [里程碑](#里程碑))

## 目录结构

```
~/nas/                          # 单一部署根(备份 = 拷贝目录)
├── src/                        # Go 源码(本仓库)
│   ├── cmd/
│   │   ├── nasm/               # 管理 CLI(只管理 nasd 生命周期)
│   │   └── nasd/               # 主框架守护进程
│   ├── internal/
│   │   ├── config/             # 部署根解析 + data/config.json
│   │   ├── daemon/             # nasd 核心:HTTP/DB/插件扫描/生命周期
│   │   ├── mgmt/               # 管理通道 JSON-RPC(Unix socket,跨平台适配)
│   │   ├── version/            # 版本信息(构建时注入)
│   │   └── webui/              # 嵌入前端静态资源(单二进制)
│   ├── scripts/build.sh        # 构建脚本(host / android 交叉编译)
│   ├── termux-service/         # runit 服务脚本模板
│   └── Makefile
├── bin/                        # 构建产物(nasm / nasd)
├── plugins/                    # 插件二进制(M4)
├── data/                       # nas.db / config.json / logs/(运行时生成)
└── run/                        # nas.sock 管理 socket(运行时生成)
```

## 快速开始

### 本机开发(Windows / Linux / macOS)

```bash
cd src
make build          # 构建到 ../bin/
NAS_ROOT=/tmp/nas ./bin/nasd -root /tmp/nas   # 启动守护进程(终端 A)
./bin/nasm status   # 查询状态(终端 B)
./bin/nasm log -n 20
./bin/nasm stop
```

> 开发环境(Windows)下管理通道自动退化为回环 TCP(地址写入 `run/nas.addr`);
> 生产环境(Termux)使用 Unix socket。无需额外配置。

### Termux 部署

```bash
pkg install golang termux-services termux-api
cd ~/nas/src && make android        # 交叉编译 android/arm64 静态二进制

# 注册 runit 服务(详见 termux-service/nasd-run.sh 头注释)
mkdir -p $PREFIX/var/service/nasd/log
cp termux-service/nasd-run.sh $PREFIX/var/service/nasd/run
chmod +x $PREFIX/var/service/nasd/run
sv-enable nasd                      # 开机自启(Termux:Boot)

nasm status                         # 或直接: sv start nasd
```

浏览器访问 `http://<手机局域网IP>:7531`。

## 两条通信通道

| 通道 | 路径 | 用途 | 暴露面 |
|------|------|------|--------|
| 用户通道 | `:7531` HTTP | Web UI / API | 局域网 / Tailscale |
| 管理通道 | `run/nas.sock`(Unix socket) | nasm ↔ nasd 生命周期指令 | 仅本机 |

管理通道为 JSON-RPC,当前方法:`daemon.status` / `daemon.stop` / `daemon.enterUpdate` / `log.tail`。
**插件操作不在此通道**——统一走用户通道 HTTP(Web UI「插件管理」页,需登录)。

## 里程碑

| 里程碑 | 内容 | 状态 |
|--------|------|------|
| **M1** | 项目骨架:go.mod、nasm CLI、nasd 守护、Unix socket 管理通道、start/stop/status | ✅ |
| **M2** | 认证中心 + 前端壳(登录页/布局/HTMX)+ SQLite 会话 | ✅ |
| **M3** | 内建模块:文件管理 + 系统监控(HTMX 轮询看板) | ✅ 当前 |
| M4 | 插件系统:管理器 API + 注册协议 + 反代 + 懒加载 + download 插件 | ⏳ |
| M5 | 服务控制 + 备份中心 + 安全加固 | ⏳ |
| M6 | nasm update 更新流程 + 插件市场 + PWA + Tailscale 集成 | ⏳ |

## 关键设计决策

- **SQLite 用 `modernc.org/sqlite`**(纯 Go 驱动)——`CGO_ENABLED=0` 静态编译的关键,Termux 无 C 编译链也能构建
- **管理通道跨平台**:Unix socket(Linux/Termux)+ 回环 TCP(Windows 开发调试),build tag 自动切换
- **前端静态资源 `go:embed`** 进 nasd,单二进制自带全部界面
- **职责单点**:nasm 只管理 nasd 生命周期;插件全权由 nasd 控制(Web UI)

## 文档

- `场景调研.md` — 26 个产品方向 × 六维评分(决策依据)
- `技术栈建议.md` — Go + SQLite + HTMX 选型论证
- `NAS框架开发文档.md` — 本文档来源(架构/API/安全/里程碑)
