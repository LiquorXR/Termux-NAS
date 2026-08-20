# Termux NAS

A **pluggable mobile NAS** that runs inside Termux (the Linux environment on Android).

- **Architecture**: a single main-frame daemon `nasd` (one binary) + plugins (independent binaries); the repo-root script `nas.sh` manages the full lifecycle of `nasd` (install / update / start / stop / status / log / uninstall)
- **Tech stack**: Go + Fiber + SQLite (WAL) + HTMX + vanilla JS/CSS (built with Vite)
- **Deployment environment**: Termux — no root, high ports, `nohup` background daemon; the one-liner `nas.sh` installs/updates/starts/stops it, **no Go toolchain needed on the phone**
- **Current stage**: M3 file management + system monitoring onward is complete; all milestones **M1–M6** are done (see [Milestones](#milestones))

## Documentation

| Document | Language | Content |
|---|---|---|
| [Project Guide (English)](docs/PROJECT_GUIDE.en-US.md) | EN | Complete reference: architecture, modules, API, security, deployment |
| [Project Guide (中文)](docs/PROJECT_GUIDE.zh-CN.md) | ZH | 完整参考:架构、模块、API、安全、部署 |
| [NAS Framework Dev Doc (中文)](docs/NAS框架开发文档.md) | ZH | Canonical developer document (architecture / API / security / milestones) |

## Directory Layout

```
~/nas/                          # single deployment root (backup = copy the directory)
├── nas.sh                      # ★ one-shot deploy/update/manage script (Termux-first, no Go toolchain)
├── src/                        # Go source (this repository)
│   ├── cmd/nasd/               # main-frame daemon (single binary)
│   ├── internal/
│   │   ├── config/             # deployment-root resolution + data/config.json
│   │   ├── daemon/             # nasd core: HTTP / DB / plugin lifecycle
│   │   ├── lock/               # single-instance lock (flock / Windows mutex)
│   │   ├── version/            # version info (injected at build time)
│   │   └── webui/              # embedded front-end static assets (single binary)
│   ├── scripts/build.sh        # build script (host / android cross-compile)
│   ├── scripts/smoke-test.sh   # nas.sh smoke tests (mechanism layer cross-platform / runtime layer needs Linux)
│   └── Makefile
├── .github/workflows/          # CI (ci.yml) + release pipeline (release.yml)
├── bin/                        # build artifacts, or the binary downloaded by nas.sh (nasd)
├── plugins/                    # plugin binaries (M4)
├── data/                       # nas.db / config.json / logs/ (generated at runtime)
└── run/                        # single-instance lock run/nas.lock (generated at runtime)
```

## Quick Start

### Local development (Windows / Linux / macOS)

```bash
cd src
make build          # build nasd into ../bin/
# Terminal A: start the daemon (Ctrl+C = graceful stop)
NAS_ROOT=/tmp/nas ./bin/nasd -root /tmp/nas
# Terminal B: manage lifecycle with nas.sh (Linux/macOS; use Ctrl+C on Windows)
bash ../nas.sh status   # or: ../bin/nasd -version
bash ../nas.sh log -n 20
bash ../nas.sh stop
```

> Lifecycle is managed entirely by `nas.sh` (SIGTERM graceful stop / HTTP health probe / direct log file reads).
> No Go management CLI or local admin socket is needed.

### Termux deployment (recommended: one-click script, no Go needed)

No Go toolchain and no manual file copying is required on the phone. `nas.sh` automatically:
creates the `~/nas` layout → pulls the prebuilt `android/arm64` binary from GitHub Releases →
SHA256 verification → sets the executable bit → installs.

```bash
pkg install curl                # first time: install the dependency
# For networks in mainland China the ghfast.top mirror is recommended (nas.sh uses it by default too)
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/LiquorXR/Termux-NAS/main/nas.sh -o nas.sh
bash nas.sh install             # install
bash nas.sh start               # start nasd in the background (detached supervisor keeps the parent alive, prevents orphan reclamation)
```

> `nas.sh` downloads/updates the main program through the **ghfast.top mirror** by default
> (`NAS_MIRROR`, default `https://ghfast.top/`); to hit GitHub directly: `export NAS_MIRROR=`,
> or point `NAS_MIRROR` at another mirror.

Open `http://<phone LAN IP>:7531` in a browser.

> On first start nasd generates `data/config.json` automatically (default port 7531); then follow the
> Web UI wizard to create the administrator account.

**Common commands**

| Command | Purpose |
|---|---|
| `bash nas.sh install` | install / repair |
| `bash nas.sh update [-f] [version]` | update to latest (or a specific `v<version>`), with verify / backup / rollback |
| `bash nas.sh start` / `stop` / `restart` | start / graceful stop / restart |
| `bash nas.sh status` / `log [-n N]` | status / view log tail |
| `bash nas.sh doctor` | environment health check (binary / dirs / health port / disk) |
| `bash nas.sh uninstall [-y]` | uninstall (prints a plan by default; `-y` really deletes data) |
| `bash nas.sh self-update` | update the nas.sh script itself |

**Releases & updates**: pushing a `v*` tag triggers CI to cross-compile and publish the
`nasd-android-arm64` binary together with `sha256sums.txt`.
`bash nas.sh update` defaults to the latest Release; pin a version with `bash nas.sh update 0.2.0`.
Full update flow: download → SHA256 verify → graceful stop (SIGTERM) → atomic replace (old version kept as `.bak`) →
restart + health check → automatic rollback on failure.

### Termux source build (contributors / offline fallback)

If you need to build it yourself (no network or custom build), first install the toolchain:

```bash
pkg install golang
cd ~/nas/src && make android        # cross-compile the android/arm64 static binary (front end included)
# artifact lands in ../bin/nasd; manage it with nas.sh from there on
bash ../nas.sh start                # start nasd in the background (supervised)
```

## Communication & Lifecycle Management

| Channel / method | Purpose | Exposure |
|---|---|---|
| User channel `:7531` HTTP | Web UI / API (login required) | LAN / Tailscale |
| `nas.sh` (SIGTERM graceful stop) | lifecycle control: start / stop / restart / update | local only (Termux shell) |
| `nas.sh` (`/health` + direct log reads) | probe / status / logs | local only |

The main program is a single binary `nasd`; no local admin socket or admin CLI is kept.
All plugin operations go over the user-channel HTTP (the Web UI's "Plugins" page, login required).

## Milestones

| Milestone | Content | Status |
|---|---|---|
| **M1** | Project skeleton: nasd daemon, lifecycle management (nas.sh), start/stop/status | ✅ |
| **M2** | Auth center + front-end shell (login page / layout / HTMX) + SQLite sessions | ✅ |
| **M3** | Built-in modules: file management + system monitoring (HTMX polling dashboard) | ✅ |
| **M4** | Plugin system: manager API + registration protocol + reverse proxy + lazy loading + download plugin | ✅ |
| **M5** | Backup center + security hardening | ✅ |
| **M6** | Atomic update flow + plugin market + PWA + Tailscale integration | ✅ |

## M6: Atomic Updates + Plugin Market + PWA (implemented)

### Atomic updates (`nas.sh update`)
- Single binary `nasd`; `bash nas.sh update`: download → SHA256 verify → SIGTERM graceful stop
  → atomic replace (old `.bak` backup) → restart + health check → automatic rollback on failure
- `update -f`: force update, skipping the version check; same version auto-skips
- Lifecycle is managed end-to-end by `nas.sh` (SIGTERM / health / direct logs), no admin CLI

### Plugin market
- `internal/market`: embedded official market index (`go:embed`): download / alist / media / photos
- API: `GET /api/market` (browse + installed state), `POST /api/market/install` (one-click install)
- Web UI: "Market" page (card browse / install state / one-click install)

### PWA
- `manifest.json` (standalone / theme color / icons) + `icon.svg` + service worker (offline shell cache)
- Navigation requests: network-first, fall back to cache on failure; static assets cache-first; API never cached

### Tailscale integration
- Documentation-guided: after installing Tailscale, reach it via the LAN IP, or via the remote IP Tailscale
  assigns for remote access (see §8 Remote Access in the dev doc)

## M4: Plugin System (implemented)

- **Plugin manager**: state machine (stopped/starting/running/stopping/crashed/crash-loop), process lifecycle,
  cross-platform executable detection (extension + MZ/ELF/shebang magic-byte probing)
- **Registration protocol**: after startup the plugin prints one registration JSON line to stdout
  (id/name/version/port/nav/icon); a 5 s timeout marks failure
- **Crash recovery**: automatic restart with backoff; 3 consecutive crashes enter `crash-loop`; Stop resets it manually
- **Lazy loading**: first access to `/p/<id>/*` auto-starts the plugin; idle reap with a timeout (default 10 min)
- **Reverse proxy**: `/p/<id>/*` → `127.0.0.1:<plugin port>/*`, unified auth, path & query parameters preserved
- **Management API**: `/api/plugins/*` list / install (upload or URL) / start / stop / restart / uninstall / log
- **Web UI**: the "Plugins" page supports install, start/stop, restart, uninstall; 3 s status polling

### A quick look at plugin development

A plugin is a standalone executable that obeys the registration protocol:

```go
// After startup, printing one registration JSON line to stdout lets nasd take it over
fmt.Printf(`{"id":"download","name":"Download Center","version":"1.0.0",
  "port":%d,"nav":"Downloads","icon":"download"}`+"\n", actualPort)
```

- Listen on `127.0.0.1:<port>` (settable via `--port`; `0` = random)
- Provide `GET /health` returning 200 (for probing)
- Package as `.tar.gz` (one executable inside), upload it from the Web UI "Plugins" page

## M5: Backup Center + Security Hardening (implemented)

### Backup center
- `internal/backup`: task CRUD (SQLite persistence) + cron scheduling + executor + completion notifications
- Scheduling: 5-field cron expression (supports `* / , -`), checks due tasks every minute
- Execution: rsync-first (incremental / remote targets), falls back to local copy; restore supported (reversed direction)
- Completion notification: termux-notification (injectable/replaceable)
- API: `/api/backup/jobs|run|restore`; Web UI "Backup" page

### Security hardening
- Login failure rate limiting: 5 consecutive failures per IP lock for 15 minutes (429 + Retry-After)
- Security response headers: CSP / X-Frame-Options DENY / nosniff / no-referrer
- Login/settings pages `Cache-Control: no-store`
- Single-file upload cap: 256 MiB
- Shared-link downloads force `attachment` (html/svg/xml/js inside the file root cannot be inlined, preventing stored XSS)
- Plugin/update package downloads go through a safe HTTP client (timeout + size cap + private/loopback blocking, SSRF protection)
- Session cookie: HttpOnly + SameSite=Lax + Max-Age (aligned with the 7-day TTL)

### Secure deployment options (`data/config.json`)
| Config | Description | Default |
|---|---|---|
| `trust_proxy` | enable when deployed behind a trusted reverse proxy; login rate limiting keys on `X-Forwarded-For`; on direct connections a forged header could bypass the limiter | `false` |
| `force_https` | enable when accessed through an HTTPS reverse proxy/tunnel; adds the `Secure` flag to the session cookie | `false` |

## Key Design Decisions

- **SQLite via `modernc.org/sqlite`** (pure-Go driver) — the key to `CGO_ENABLED=0` static builds, so Termux can build without a C toolchain
- **Lifecycle managed end-to-end by `nas.sh`**: SIGTERM graceful stop + HTTP `/health` probe + direct log reads; no local admin socket, no admin CLI
- **Front-end assets `go:embed`-ed** into nasd — the single binary ships with the entire UI
- **Single responsibility**: nas.sh only manages the nasd lifecycle; plugins are fully controlled by nasd (Web UI)

## License

No license file is present in this repository at the time of writing — contact the author (LiquorXR) before
redistribution or reuse. See the GitHub repository for the latest state.
