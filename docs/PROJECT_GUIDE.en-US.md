# Termux NAS — Complete Project Documentation (English)

> **Status**: All milestones M1–M6 complete · Current version lineage v0.1.0 / v0.2.0
> **Architecture**: main-frame daemon (`nasd`, single binary with all built-in NAS features) + one-shot
> management script (`nas.sh`) + plugins (independent binaries, fully controlled by nasd)
> **Tech stack**: Go + Fiber + SQLite (WAL) + Vite-built front end (HTMX + vanilla JS + hand-written design system)
> **Deployment environment**: Termux (Linux on Android), single process + optional plugin processes

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [Architecture](#2-architecture)
3. [Repository & Deployment Layout](#3-repository--deployment-layout)
4. [Technology Stack](#4-technology-stack)
5. [Getting Started](#5-getting-started)
6. [Lifecycle Management with nas.sh](#6-lifecycle-management-with-nassh)
7. [Main Daemon (nasd) Internals](#7-main-daemon-nasd-internals)
8. [Built-in Modules](#8-built-in-modules)
9. [Plugin System](#9-plugin-system)
10. [Plugin Market](#10-plugin-market)
11. [PWA (Progressive Web App)](#11-pwa-progressive-web-app)
12. [Front End (Vite Web App)](#12-front-end-vite-web-app)
13. [API Reference](#13-api-reference)
14. [Configuration Reference](#14-configuration-reference)
15. [Security Design](#15-security-design)
16. [CI/CD & Release](#16-cicd--release)
17. [Testing & Quality](#17-testing--quality)
18. [Key Design Decisions](#18-key-design-decisions)
19. [History & Roadmap](#19-history--roadmap)
20. [Troubleshooting & FAQ](#20-troubleshooting--faq)

---

## 1. Project Overview

### 1.1 Goal

Build a **pluggable mobile NAS** inside Termux:

- **High performance, low footprint**: the main frame is a single Go binary; resident memory is roughly 15–30 MB
- **Fully Termux-compatible**: runs without root, uses high ports, foreground-run daemon
- **Extensible**: core functions are built in; extended functions are loaded dynamically as independent binary plugins
- **Easy to manage**: a one-shot script (`nas.sh`) owns the whole `nasd` lifecycle (install/update/start/stop/status/log/uninstall); everything about plugins is controlled by the main frame (`nasd`) through the Web UI

### 1.2 Core design principles

1. **Hybrid architecture** — core functions (auth / files / monitor / backup) are built into `nasd`; extended functions (download / cloud drive / media / third-party) become plugins.
2. **Single binary, single responsibility** — `nas.sh` manages only the `nasd` binary (install/update/start/stop/status/log/uninstall); `nasd` has full authority over plugins (install/uninstall/start-stop/update).
3. **Process-level plugin isolation** — a crashing plugin never takes down the main frame, and plugins can be updated independently.
4. **Lazy loading** — plugins start on demand (only when their page is opened); resident memory does not grow with the number of plugins.
5. **Single deployment root** — `~/nas/` holds everything; a backup is just a directory copy.

### 1.3 Scale of the codebase

| Area | Scope |
|---|---|
| Go production code | ~4,900 lines across 38 files |
| Go tests | ~2,300 lines across 20 files |
| Front end | Vite + HTMX + vanilla JS; hand-written design system in `app.css` (~760 lines) |
| Shell infrastructure | `nas.sh` (595 lines, lifecycle), `scripts/smoke-test.sh` (smoke tests), build scripts |
| CI/CD | `ci.yml` (full quality gate) + `release.yml` (tag-driven android/arm64 publishing) |

---

## 2. Architecture

```
┌─────────────────────────────────────────────────────────┐
│                User / Browser (single entry :7531)      │
│      The Web UI also provides the "Plugins" page        │
│      (plugins are controlled by nasd)                   │
└──────────────────────────┬──────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────┐
│             nasd · Main frame (resident daemon, one bin) │
│                                                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│  │ Auth     │ │ Files    │ │ Monitor  │    │
│  └──────────┘ └──────────┘ └──────────┘    │
│  ┌──────────┐ ┌────────────────────────────────────┐     │
│  │ Backup   │ │ Plugin manager(install/uninstall/  │     │
│  └──────────┘ │ start-stop/update/lazy-load)        │     │
│  SQLite · config · logs · single-instance lock       │     │
│  (run/nas.lock / Windows mutex)                      │     │
└──────────────┬───────────────────────────────▲───────────┘
               │ SIGTERM graceful stop / /health │ user requests /p/<id>/*
┌──────────────▼───────────────┐   ┌──────────┴───────────┐
│  nas.sh · one-shot script    │   │  plugins/ binaries    │
│  install/update/start/       │   │  download :18002     │
│  stop/restart/status/log     │   │  alist :18003        │
│  never touches plugins       │   │  media  :18004       │
│  never resident              │   │  (fully owned by nasd)│
└──────────────────────────────┘   └──────────────────────┘
```

### 2.1 Three components

| Component | Form | Responsibility | Resident |
|---|---|---|---|
| **nas.sh** | bash script (repo root) | full lifecycle of the main frame: install/update/start-stop/status/log/uninstall | no (run-and-exit) |
| **nasd** | daemon binary | all runtime capabilities: HTTP service, built-in NAS features, **full plugin management** | yes (foreground run) |
| **Plugins** | independent binaries | extended features: download / cloud drive / media ...; lifecycle fully controlled by nasd | on demand (lazy load) |

> **Single responsibility**: installing, uninstalling, starting/stopping and updating plugins can **only** be done
> through nasd (the Web UI "Plugins" page or user-channel API). nas.sh is not even aware of plugins; there is no
> direct interaction between nas.sh and plugins.

### 2.2 Communication & lifecycle

| Channel / method | Path / mechanism | Purpose | Exposure |
|---|---|---|---|
| User channel | `:7531` HTTP | browser Web UI / API | LAN / Tailscale |
| Lifecycle control | SIGTERM graceful stop + `/health` probe + direct log reads | nas.sh manages the main program | local only (Termux shell) |

> No local admin socket or admin CLI is kept; the main program is a single binary `nasd`.

---

## 3. Repository & Deployment Layout

### 3.1 Deployment root (`~/nas/`)

```
~/nas/                          # single deployment root
├── bin/nasd                    # main-frame daemon (single binary)
├── plugins/                    # plugin executables
│   ├── download                # download-center plugin
│   ├── alist                   # cloud-drive aggregator plugin
│   └── media                   # media-service plugin
├── data/
│   ├── nas.db                  # SQLite (sessions / shares / backup jobs / meta)
│   ├── config.json             # main-frame configuration
│   └── logs/nasd.log           # main-frame log
├── files/                      # file-management root (default; overridable via file_root)
└── run/nas.lock                # single-instance lock (flock, created at runtime)
```

Conventions:
- Backing up `data/` backs up all configuration and state; `plugins/` can be re-downloaded.
- An update only touches the single binary `bin/nasd`; the old version is kept as `.bak` for rollback.

### 3.2 Repository root

```
.
├── README.md / README.en.md     # quick start (中文 / English)
├── nas.sh                       # ★ one-shot deploy/update/manage script
├── scripts/smoke-test.sh        # nas.sh mechanism + runtime smoke tests
├── .github/workflows/
│   ├── ci.yml                   # full quality gate (format/vet/test/race/android/smoke)
│   └── release.yml              # push v* tag → build & publish android/arm64
├── docs/
│   ├── NAS框架开发文档.md        # canonical developer document (中文)
│   ├── PROJECT_GUIDE.zh-CN.md   # this guide (中文)
│   └── PROJECT_GUIDE.en-US.md   # this guide (English)
└── src/
    ├── cmd/nasd/main.go         # daemon entry point
    ├── internal/                # all packages (see below)
    ├── web/                     # Vite front end
    ├── scripts/build.sh         # build helper (host / android)
    └── Makefile
```

### 3.3 Go packages (`src/internal/`)

| Package | Responsibility |
|---|---|
| `config` | deployment-root resolution, `data/config.json` load/save (atomic write) |
| `daemon` | core: HTTP routing, DB open/migrate, plugin manager, backup/market HTTP handlers |
| `auth` | Argon2id password hashing, SQLite sessions, cookie middleware, login rate limiting |
| `files` | file CRUD / search / share links with strict path confinement |
| `monitor` | system stats: CPU / memory / disk / network / battery (linux/android + windows builds) |
| `backup` | backup jobs: SQLite store, 5-field cron scheduler, rsync/copy executor, notifications |
| `market` | embedded plugin-market index (`go:embed`) |
| `plugin-subsystem` | lives under `daemon` (`plugins.go`, `plugins_http.go`, `proxy.go`) |
| `safehttp` | SSRF-safe HTTP client for plugin/update downloads |
| `lock` | single-instance lock (flock on Unix, mutual exclusion on Windows) |
| `version` | build-injected version info |
| `webui` | `go:embed` of the built front end |

---

## 4. Technology Stack

### Back end

| Layer | Choice | Why |
|---|---|---|
| Language | Go (`go 1.25.5`, module `github.com/termux-nas/nas`) | single static binary, low memory, cross-compiles to android/arm64 |
| Web framework | Fiber v2.52.15 (fasthttp) | high performance, small footprint, fits low-end phones |
| SQLite driver | `modernc.org/sqlite` v1.56.0 (pure Go) | enables `CGO_ENABLED=0` static builds — Termux has no C toolchain |
| HTTP client | `net/http` + `fasthttp` | safehttp client for downloads; fasthttpadaptor serves embedded assets |
| Password hashing | `golang.org/x/crypto/argon2` | Argon2id (OWASP-recommended work factors tuned for phones) |
| System APIs | `golang.org/x/sys` | Windows mutex for single-instance lock (dev) |
| Logging | `log/slog` (Go stdlib) | text handler → stderr + `data/logs/nasd.log` |

### Front end (`src/web/`)

| Item | Choice |
|---|---|
| Build tool | Vite 5.4.11 (`npm run build` → `../internal/webui/dist`, `go:embed`-ed) |
| Progressive enhancement | htmx.org 2.0.4 |
| Application style | vanilla JS modules, no framework; hand-written design system |
| Styling | single `src/styles/app.css` (~760 lines), CSS custom properties, dark/light themes |
| PWA | `manifest.json` + `icon.svg` + service worker (`sw.js`) |

---

## 5. Getting Started

### 5.1 Local development (Windows / Linux / macOS)

```bash
cd src
make build          # builds nasd into ../bin/ (front end is built first, auto)
# Terminal A: run the daemon (Ctrl+C = graceful stop)
NAS_ROOT=/tmp/nas ./bin/nasd -root /tmp/nas
# Terminal B: manage lifecycle with nas.sh (Linux/macOS)
bash ../nas.sh status
bash ../nas.sh log -n 20
bash ../nas.sh stop
```

Notes:
- `nasd` accepts `-root` (deployment root; default `$NAS_ROOT` or `$HOME/nas`), `-debug`, `-version`.
- On Windows the binary gets a `.exe` suffix and lifecycle control is typically just Ctrl+C in the terminal
  (Windows has no `flock`/`SIGTERM` semantics; the single-instance lock uses a kernel mutex there).
- `bin/` is git-ignored — build artifacts are not committed.

### 5.2 Termux deployment (recommended: one-click, no Go on the phone)

```bash
pkg install curl                # first-time dependency
# Mainland-China users: fetch via ghfast.top mirror (nas.sh internally defaults to it too)
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/LiquorXR/Termux-NAS/main/nas.sh -o nas.sh
bash nas.sh install          # install
bash nas.sh start
```

The installer automatically: creates the `~/nas` layout → downloads `nasd-android-arm64` from the
latest GitHub Release (through mirror) → verifies SHA256 → chmod +x → places it.

Browse to `http://<phone-lan-ip>:7531`, follow the wizard to create the admin account.

### 5.3 Termux source build (contributors / offline)

```bash
pkg install golang
cd ~/nas/src && make android    # CGO_ENABLED=0 GOOS=android GOARCH=arm64, front end embedded
bash ../nas.sh start            # start nasd in the background (supervised)
```

---

## 6. Lifecycle Management with nas.sh

`nas.sh` (script version 3.0.0) is the **only** management interface for the main daemon.
All commands are listed in the quick table in README; this section explains internals.

### 6.1 Command surface

```
bash nas.sh install              # create layout → pull Release binary → SHA256 verify → place
bash nas.sh update [-f] [版本]     # update to latest (or pinned v<version>)
bash nas.sh start | stop | restart
bash nas.sh status [-json] | log [-n N]
bash nas.sh uninstall [-y]
bash nas.sh self-update
bash nas.sh help | version
```

### 6.2 How it interacts with nasd (no admin socket)

| Operation | Mechanism |
|---|---|
| Stop | sends SIGTERM to the daemon; nasd runs its graceful shutdown (`ctx.Done()` → stop plugins → HTTP `ShutdownWithTimeout(3s)` → release lock). `nas.sh` waits up to ~12 s, then escalates to SIGKILL |
| Probe / status | `GET /health` on the user channel returns `status/version/uptime/pid/port`; PID detection also reads `run/nas.lock` first, with a `pgrep` fallback |
| Log | directly reads the tail of `data/logs/nasd.log` |
| Single instance | nasd `flock`s `run/nas.lock` at startup; a second instance exits immediately |
| Concurrency | mutating commands (`install/update/start/stop/restart/uninstall/self-update`) take a lock dir `$NAS_ROOT/.nas.lock.d` to avoid racing each other |

### 6.3 Atomic update (`nas.sh update`)

```
① nas.sh downloads the new nasd → temp file (SHA256 verified against sha256sums.txt; tampering aborts, no side effects)
② if running: SIGTERM graceful stop, wait for process exit and lock release
③ atomic replace: old → bin/nasd.bak; new → bin/nasd; chmod +x
④ restart the new nasd, poll /health until ready
⑤ ready ⇒ done (keep the most recent .bak for manual rollback); start failure ⇒ roll .bak back and restart
```

- `update -f` forces the replace even if versions match; same-version updates are skipped by default.
- Version comparisons use the semantic-version first field reported by `nasd -version`.
- On Linux the downloaded artifact must be executable and report a version (guards against corrupt/wrong-arch binaries).

### 6.4 Environment variables

| Variable | Meaning | Default |
|---|---|---|
| `NAS_ROOT` | deployment root | `$HOME/nas` |
| `NAS_REPO` | GitHub repo | `LiquorXR/Termux-NAS` |
| `NAS_MIRROR` | GitHub accelerator prefix | `https://ghfast.top/` (empty = direct) |
| `NAS_DIST_URL` | asset download base (overrides, for mirrors/local tests) | from GitHub Releases |
| `NAS_ARCH` | arch override (dev machines can fake `arm64`) | `uname -m` |

---

## 7. Main Daemon (nasd) Internals

### 7.1 Startup sequence (`cmd/nasd/main.go` + `daemon.Run`)

```
signal ctx (SIGINT/SIGTERM)
  → resolve root & paths (config.Resolve + EnsureDirs)
  → load/generate config (config.Load)
  → open log (stderr + data/logs/nasd.log), optional -debug
  → daemon.Run(ctx):
      0.  Acquire single-instance lock (run/nas.lock)
      1.  Open SQLite in WAL mode, run migrations
      1.5 Auth store (trust-proxy / secure-cookie flags applied)
      1.6 Files store (root default <root>/files, or cfg.FileRoot)
      2.  Plugin manager: scan & register metadata (no process start — lazy loading)
      2.5 Backup manager (store + scheduler + executor + notifier)
      3.  Build HTTP app and listen on :7531 (in a goroutine)
      4.  Plugin idle-reap ticker
      5.  Backup schedule ticker (every minute)
      6.  Wait for ctx cancel or HTTP error → Stop()
```

### 7.2 Graceful shutdown (`daemon.Stop`)

Stops every running plugin (in sequence), then closes HTTP with a 3 s timeout so keep-alive connections cannot
block process exit (which would leave the single-instance lock held and block `nas.sh` from swapping the binary).

### 7.3 Database schema & migrations

- Pure-Go SQLite with `PRAGMA journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON`.
- `db.SetMaxOpenConns(1)` (SQLite is single-writer).
- Versioned migrations tracked in `meta.schema_version`:
  - **v1**: `meta` table (idempotent base)
  - **v2**: `users`, `sessions` (auth center)
  - **v3**: `shares` (file share links)
- `backup_jobs` is created by the backup store's own migration.
- `meta` also records `nasd_version` and `last_start` on each startup.

### 7.4 HTTP routing strategy (`daemon/buildHTTP`)

- Fiber app with `BodyLimit: 512 MiB`.
- Global `securityHeaders` middleware.
- `GET /health` (no auth, for nas.sh probing) and `GET /api/version` (no auth).
- Page routes: `/login`, `/setup`, `/` (auth-aware handlers).
- All `/api/...` business routes are auth-protected; state-changing ones also pass `checkSameOrigin`
  (Origin == Host). Exceptions: public share downloads under `/s/:token`.
- Plugin reverse proxy: `/p/:id` and `/p/:id/*` (auth required).
- Embedded static assets served last via `http.FileServer` wrapped with `fasthttpadaptor`.

---

## 8. Built-in Modules

### 8.1 Auth center (`internal/auth`)

- **Setup**: first run only — `/api/auth/setup` creates the admin user and logs it in. Disabled once any user exists.
- **Credentials**: password hashed with Argon2id (`t=3, m=64 MiB, p=4, key=32 B`, encoded as
  `argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>`); verification uses constant-time compare.
- **Sessions**: 32-byte random token, stored in SQLite, TTL 7 days; cookie
  `nas_session` is `HttpOnly + SameSite=Lax`, `Max-Age` aligned to TTL; `Secure` added when `force_https`.
- **Middleware**: `RequireAuth` (401 JSON), `PageAuth` (redirect to `/login`), `OptionalAuth`, `SessionUser`.
- **Rate limiting**: per-IP sliding-window limiter — 5 consecutive failures within 10 minutes lock the IP for
  15 minutes (`429` + `Retry-After`). `trust_proxy` switches the key to `X-Forwarded-For` (only behind a trusted
  reverse proxy). Caps memory at 4096 keys with lazy pruning.
- **CSRF**: SameSite=Lax cookie is the primary defense; `checkSameOrigin` on non-GET requests is the second line.

### 8.2 File management (`internal/files`)

- Operates on the file root (default `<root>/files`; set `file_root` to `~/storage/shared` on Termux for the
  shared Android storage).
- Operations: list (dirs first, case-insensitive sort), mkdir, multi-file upload, streamed download,
  rename, recursive delete, name search, share links.
- **Path safety** (`safe.go`): every user path goes through `Normalize` — rejects absolute paths (platform +
  leading `/` + UNC `\\` + Windows drive-letter variants including `C:foo`), rejects `..` escapes, and enforces
  strict containment inside the root. Symbolic links are not followed.
- **Upload limits**: single file ≤ 256 MiB; filenames must pass `SafeName` (base name only, no separators).
- **Downloads** are streamed (manual open + `SetBodyStream`) rather than served through Fiber's file cache, so
  files are never held open/locked on Windows.
- **Stored-XSS protection**: inline rendering is disabled for executable content types
  (html/xhtml/svg/xml/js/json) — downloads are forced to `attachment`, and share-downloads are always `attachment`.
- **Search**: recursive, case-insensitive substring, capped at 200 results and depth 8.
- **Share links**: random 16-byte hex token stored in `shares` with an expiry (default 24 h, max 365 days);
  public endpoint `GET /s/:token` validates + streams; expired links are cleaned on access.

### 8.3 System monitoring (`internal/monitor`)

- `GET /api/monitor/summary` → CPU %, memory, disk, network totals, battery (Termux), platform/hostname/uptime.
- Platform collection: Linux/Android read `/proc` (`/proc/stat`, `/proc/meminfo`, `/proc/net/dev`,
  `/proc/uptime`, `statfs`); Windows reads system APIs (`monitor/windows.go`); other platforms return nothing.
- CPU usage is computed as a delta between consecutive samples (first call returns 0).
- Battery via `termux-battery-status` (only when `$PREFIX` is set), cached for 10 s so dashboard polling does
  not spawn a subprocess per request.
- The front-end dashboard (`pages/monitor.js`) polls every 3 s using ring gauges with threshold colors
  (≥80% warn, ≥90% danger).

### 8.4 Backup center (`internal/backup`)

- **Jobs** (`backup_jobs` table): name / source / target / schedule / enabled / keep_copies / last-run stats.
- **Scheduling**: in-process ticker checks due jobs every minute against a 5-field cron expression
  (supports `* / , -`, minute–dow); empty schedule = manual only.
- **Execution** (`runBackup`): rsync-first (`-a --delete`, incremental, remote `user@host:`/`rsync://` targets
  require rsync); falls back to a local recursive copy. Restore runs the job with source/target reversed.
- **Notification**: `termux-notification` on completion (injectable function, default `defaultNotify`).
- **Concurrency guard**: an in-flight job cannot be re-run (per-job `running` map).
- **API**: `/api/backup/jobs` (GET/POST/PUT/DELETE), `/api/backup/run`, `/api/backup/restore`; UI "Backup" page.
- Note: `keep_copies` is persisted and exposed in the API, but copy rotation is not yet enforced by the executor.

---

## 9. Plugin System

### 9.1 Plugin manager (`internal/daemon/plugins.go`)

- **State machine**: `stopped → starting → running → stopping → stopped`, plus `crashed` and `crash-loop`.
- **Scanning** (`Scan`): walks `plugins/`, registers executable files (metadata only, never starts processes);
  removes plugins that vanished from disk (unless running). Registration never resets a running plugin's state.
- **Executable detection** is cross-platform: on Unix use the execute bit; on Windows use extension
  (`exe/bat/cmd/com`) or magic bytes (`MZ` = PE, `\x7fELF`, `#!` shebang).
- **Start** (`startLocked`): spawns the plugin with `--name=<id> --port=0`, reads its stdout and parses the
  one-line registration JSON within 5 s (non-JSON lines are skipped as logs). Failure kills the process and is
  treated as a crash.
- **Crash recovery** (`watchExit`): distinguishes intentional stop (`stopping`) from crashes. Automatic restart
  with linear backoff (`n * 2 s`); after 3 consecutive crashes the plugin enters `crash-loop` and stops being
  restarted. A stable run ≥ 10 s resets the counter. Manual `Stop` clears the counter and is the way out of
  `crash-loop`.
- **Lazy loading** (`EnsureRunning`): first request to `/p/<id>/*` starts the plugin and waits for registration;
  the idle-reap loop (`Reap`) stops plugins untouched for `plugin_idle_timeout` (default 600 s = 10 min),
  scanning at half the timeout interval.
- **Reverse proxy** (`proxy.go`): `/p/<id>/*` → `http://127.0.0.1:<port>/*`, preserving sub-path and query
  string. The daemon's unified auth runs first; the request headers `Cookie`, `Authorization`,
  `Proxy-Authorization` are **stripped** before forwarding so a plugin can never impersonate the logged-in user.
  Plugin IDs are validated against `[A-Za-z0-9_.-]`, max 64, rejecting `.`/`..` (path traversal).
- **Shutdown** (`ShutdownAll`): stops every plugin during daemon graceful stop.

### 9.2 Plugin management API (`plugins_http.go`)

```
GET    /api/plugins                 # list + state
POST   /api/plugins/install         # install: multipart upload OR {name, source: url}
POST   /api/plugins/<id>/start|stop|restart
DELETE /api/plugins/<id>            # uninstall (stop process + delete file + rescan)
GET    /api/plugins/<id>/log        # plugin log/info
```

- Package format: a `.tar.gz` containing **exactly one** executable (single-level, `./`-prefixed paths
  tolerated). The filename used on disk is the plugin **ID** (the tarball's internal name is not trusted).
  On Windows a PE payload without an extension is automatically rewritten to `<id>.exe`.
- URL installs go through `safehttp` (30 s timeout, 64 MiB cap, private/loopback blocking) — SSRF protection.

### 9.3 Registration protocol (plugin ↔ nasd)

```
① nasd starts the plugin process with -–name=<id> --port=0
② the plugin binds a (random) port and prints ONE JSON registration line to stdout:
     {"id":"download","name":"Download Center","version":"1.0.0","port":18002,"nav":"下载","icon":"download"}
③ nasd parses stdout, registers the entry, marks it running
④ if no valid registration within 5 s → startup failure → log + auto-restart (max 3)
```

### 9.4 Plugin requirements

| Requirement | Notes |
|---|---|
| Speak the registration protocol | print the registration JSON to stdout on startup |
| Listen on a random port | via `--port 0` (or the passed `--port`) |
| Answer `GET /health` with 200 | used for probing |
| Self-contained package | Go static builds carry all dependencies |

---

## 10. Plugin Market

- The market index is an embedded JSON (`internal/market/static/market.json`, `go:embed`) listing
  `download`, `alist`, `media`, `photos` (name / version / description / author / icon / download_url / size).
- `GET /api/market` merges the index with the installed state (it knows `.exe` variants too).
- `POST /api/market/install {id}` one-click installs by reusing the plugin installer (safe download →
  tarball parse → write → rescan).
- Web UI: "Market" page with cards, install-state badges, and a one-click install button.
- The index document supports a version field and is designed to be refreshable/overridable from a remote index
  (the refresh endpoint is reserved for future work).

---

## 11. PWA (Progressive Web App)

- `public/manifest.json`: standalone display, theme/background colors, SVG icon (`any` purpose).
- `public/icon.svg`: vector app icon.
- `public/sw.js`: service worker with a versioned cache (`nas-v2`):
  - navigations: network-first, fall back to cached `/` when offline;
  - same-origin static assets: cache-first then network;
  - API requests (`/api/`), cross-origin and non-GET are never cached.
- Registered on load in production (skipped on the `5173` dev server so the offline shell never caches stale dev assets).

---

## 12. Front End (Vite Web App)

Structure under `src/web/`:

| Path | Content |
|---|---|
| `index.html` | single HTML shell: icon SVG symbol library, auth view, app shell, bottom sheet, dialogs; anti-FOUC theme script |
| `src/main.js` | boot: auth tri-state routing (uninitialized/not-logged-in/logged-in), navigation, theme switching, session-expiry handling, PWA registration |
| `src/api.js` | fetch wrapper (JSON, error normalization, 401 → login) + formatting helpers |
| `src/ui.js` | toast / confirm dialog / prompt dialog / button loading primitives |
| `src/pages/files.js` | file manager page (JS-rendered; table on desktop, cards on mobile) |
| `src/pages/monitor.js` | monitoring dashboard (3 s polling, ring gauges) |
| `public/partials/*.html` | HTMX pages: plugins, market, services, backup, settings (each with inline behavior script) |
| `public/manifest.json`, `public/icon.svg`, `public/sw.js` | PWA assets |
| `src/styles/app.css` | the whole design system: CSS variables, themes, layout, components (~760 lines) |
| `vite.config.mjs` | dev server on :5173 with `/api`, `/p`, `/s`, `/health` proxied to nasd; build output to `../internal/webui/dist` |

Design notes:
- Vanilla JS modules + HTMX 2 (`window.htmx` is exposed for partial inline scripts).
- Dark/light/system themes via `data-theme` on `<html>`; theme stored in `localStorage`.
- Responsive: sidebar (desktop), top bar + bottom tab bar + "More" sheet (mobile).
- All partial inline scripts are small, dependency-light, and use `fetch` + re-render with polling.

---

## 13. API Reference

Base: `http://<host>:7531`. Unless noted, every endpoint requires a session cookie
(`nas_session`, set by login/setup).

### Health & version (no auth)

```
GET /health                → {status, version, uptime, pid, port}
GET /api/version           → {version, commit, buildTime}
```

### Auth

```
GET  /api/auth/status      → {initialized, authed, username}          (no auth)
POST /api/auth/setup       → create admin + auto-login (first run only)
POST /api/auth/login       → {username:...} + Set-Cookie
POST /api/auth/logout      → clears session + cookie
GET  /api/auth/me          → {username, created_at}
```

### Files

```
GET  /api/files/list?path=         → {path, entries[]}
GET  /api/files/download?path=     → streamed file (attachment)
GET  /api/files/search?q=          → {results[]}
POST /api/files/mkdir              → {path, name} | HX-Prompt
POST /api/files/upload             → multipart (path + files[])
POST /api/files/rename             → {path, new_name} | HX-Prompt
POST /api/files/delete             → {path}
POST /api/files/share              → {path, expires_hours?} → {url, expires_at}
GET  /s/:token                     → public share download (attachment)
```

### Monitor

```
GET /api/monitor/summary           → {cpu_percent, mem_*, disk_*, battery?, net?, platform, ...}
```

### Backup

```
GET    /api/backup/jobs
POST   /api/backup/jobs            → Job (name/source/target/schedule)
PUT    /api/backup/jobs/:id
DELETE /api/backup/jobs/:id
POST   /api/backup/run             → {id}   (async)
POST   /api/backup/restore         → {id}   (async, reversed direction)
```

### Plugin market

```
GET  /api/market                   → {market:{name, version, plugins[]}}
POST /api/market/install           → {id}
```

### Plugins

```
GET    /api/plugins                → {plugins[]}  (id/path/size/state/pid/restarts/reg/last_err)
POST   /api/plugins/install        → multipart (file) or {name, source}
POST   /api/plugins/<id>/start|stop|restart
DELETE /api/plugins/<id>
GET    /api/plugins/<id>/log       → {id, state, restarts, last_err}
GET|POST|PUT|DELETE /p/<id>[/...]  → plugin reverse proxy (unified auth, headers stripped)
```

---

## 14. Configuration Reference (`data/config.json`)

Generated on first start with defaults; editable with the daemon stopped (it is re-read at startup):

| Key | Meaning | Default |
|---|---|---|
| `port` | user-channel HTTP port (high port; no root can't bind 80/443) | `7531` |
| `host` | listen address | `0.0.0.0` |
| `file_root` | file-management root (Termux: `~/storage/shared`) | `<root>/files` |
| `plugin_idle_timeout` | plugin lazy-load idle-reap seconds | `600` |
| `trust_proxy` | trust `X-Forwarded-For` for rate limiting (only behind a trusted reverse proxy!) | `false` |
| `force_https` | add `Secure` to the session cookie (only behind HTTPS reverse proxy/tunnel) | `false` |
| `created_at` | first-generation timestamp (read-only) | — |

Write is atomic (temp file + rename), permissions `0600`.

---

## 15. Security Design

| Concern | Mitigation |
|---|---|
| Authentication | Argon2id password hashing; 32-byte random session tokens; 7-day TTL session cookie `HttpOnly + SameSite=Lax` |
| Brute force | per-IP login rate limit: 5 fails → 15 min lock (`429` + `Retry-After`); sliding window; bounded memory |
| CSRF | SameSite=Lax cookie + `Origin == Host` check on non-GET requests |
| XSS | CSP (`default-src 'self'`, careful inline allowances for HTMX); output escaping in JS-rendered pages; stored-XSS guard on file downloads (attachment for html/svg/xml/js) |
| Clickjacking | `X-Frame-Options: DENY` |
| MIME sniffing | `X-Content-Type-Options: nosniff`; `Referrer-Policy: no-referrer` |
| Page caching | `/login` and `/setup` use `Cache-Control: no-store` |
| Path traversal / LFI | strict `Normalize` (abs/UNC/drive/`..` rejected) + containment; no symlink following |
| SSRF | `safehttp` blocks loopback/private/link-local/CGNAT/ULA addresses at dial time (DNS-rebinding safe), 30 s timeout, 64 MiB body cap, ≤5 redirects |
| Plugin isolation / privilege | plugins only hear `127.0.0.1`; daemon strips `Cookie`/`Authorization`/`Proxy-Authorization` before proxying; unified auth at the edge |
| Uploads | 256 MiB per file; `SafeName`; plugin packages must contain exactly one executable and are written under the plugin ID |
| Exposed surface | single high port for the user channel; lifecycle control is local-shell-only (SIGTERM/health/log read); no admin socket/CLI |
| Deployment hardening | run behind Tailscale/Cloudflare Tunnel; enable `trust_proxy`/`force_https` behind a trusted TLS reverse proxy only |

---

## 16. CI/CD & Release

### CI (`ci.yml`, on push to main/master or PR)

1. setup Go (from `src/go.mod`) + Node 20 (npm cache);
2. **front-end build first** (`npm ci && npm run build` → `dist`), because `go:embed` requires
   `src/internal/webui/dist` to exist for any Go compile (the directory is git-ignored);
3. `gofmt` check, `go vet`, `go build`, `go test`, `go test -race`;
4. android/arm64 cross-compile;
5. `nas.sh` smoke test (mechanism + runtime layers; builds its own `v9.9.9` test binary and exercises
   install/reinstall idempotency, update, checksum-tamper rejection, uninstall protection, and on Linux
   plus start/status/log/health/restart).

### Release (`release.yml`, on `v*` tag)

1. builds the front end, then `CGO_ENABLED=0 GOOS=android GOARCH=arm64` with
   `VERSION=<tag without v>`, short commit, UTC build time injected via `-ldflags`;
2. generates `sha256sums.txt`;
3. publishes `nasd-android-arm64` + `sha256sums.txt` to a GitHub Release
   (`softprops/action-gh-release@v2`), and `nas.sh` fetches them from
   `releases/latest/download` (or `download/v<version>`) through the mirror.

Manual release: `git tag v0.1.0 && git push origin v0.1.0`.

---

## 17. Testing & Quality

- **Go unit/integration tests** (~2,300 lines, 20 files) covering auth (store/rate-limit/password/bench),
  files path-safety, shares, plugins manager + HTTP + reverse-proxy e2e, svc (with mock runner), backup,
  market, daemon security, db, proxy, monitor battery cache, safehttp.
- **nas.sh smoke tests** (`scripts/smoke-test.sh`): a two-layer suite —
  - *Mechanism layer* (any bash): directory creation, binary placement, reinstall idempotency,
    SHA256 gate (tampered sums rejected with no side effects), `.bak` creation on forced replace,
    same-version skip, uninstall requires `-y`;
  - *Runtime layer* (Linux/ Termux/WSL2): start / status / log / health / restart / in-service
    `update -f` (graceful stop → replace → restart → `.bak`).
- **CI** runs format + vet + build + test + race + android cross-compile + smoke.
- Development commands: `make build` (host), `make android`, `make test`, `make check` (full gate), `make tidy`, `make clean`.

---

## 18. Key Design Decisions

| Decision | Conclusion | Rationale |
|---|---|---|
| Plugin loading | independent Go binaries (not WASM/Lua/compile-in) | zero learning cost, native performance, process isolation, hot-updatable |
| Core features | built into nasd | low memory, single process, Termux-friendly, simple deployment |
| Management | single script nas.sh (SIGTERM / health / direct logs) | no extra Go admin binary; zero-programming ops |
| Responsibility ownership | nas.sh manages only the main frame; nasd fully owns plugins (Web UI) | one management entry point, consistent operations, no dual state |
| Process supervision | flock single-instance lock + foreground run (nasd is this script's child; interactive shell→nas.sh→nasd all alive) | prevents dual-instance races; the foreground model inherently avoids the system SIGKILLing orphaned (PPID=1) processes (setsid / tmux / self-detached supervisors all observed killed, see FAQ) |
| Plugin memory | lazy load + idle reap | resident memory doesn't grow with plugin count |
| Communication | user-channel HTTP :7531 (only exposure) | no local admin socket, minimal attack surface |
| SQLite driver | modernc.org/sqlite (pure Go) | `CGO_ENABLED=0` static builds work without a C toolchain on Termux |

---

## 19. History & Roadmap

All six milestones are complete (✅):

- **M1** skeleton: `go.mod`, nasd daemon, nas.sh lifecycle (start/stop/status)
- **M2** auth center + front-end shell (login/layout/HTMX) + SQLite sessions
- **M3** built-ins: file management + system monitoring (HTMX polling dashboard)
- **M4** plugin system: manager API + registration protocol + reverse proxy + lazy loading (+ download plugin)
- **M5** backup center + security hardening
- **M6** atomic update flow + plugin market + PWA (+ Tailscale documentation)

Notable history (from git log): incremental M1–M6 feature commits; removal of the earlier `nasm`
management module in favour of nas.sh-only lifecycle; stored-XSS/SSRF hardening; login rate limiting;
CI front-end-first ordering; UNC/path-handling fixes; `nas.sh` mirror-prefix fix; CI smoke coverage.

Future/next (natural follow-ups): enforce `keep_copies` rotation, plugin-download; remote-market refresh;
multi-user; change-password page; WebSocket proxy support for plugins.

---

## 20. Troubleshooting & FAQ

**Q: `nas.sh` fails with "下载失败" / SHA256 error.**
A: Behind restricted networks set `NAS_MIRROR` to a working accelerator or empty it for direct GitHub; or point
`NAS_DIST_URL` at a mirror. Checksum failure intentionally aborts — it means the download is corrupt/tampered.

**Q: Browser can't reach the Web UI.**
A: Confirm `bash nas.sh status` shows running and the port from `status`; make sure the phone and computer are on
the same LAN; Termux needs the network permission and the app should be whitelisted from battery optimization
(wake lock via `termux-wake-lock`). For remote access use Tailscale / a tunnel rather than exposing port 7531.

**Q: Login says "尝试过于频繁,请稍后再试".**
A: You hit the 5-fail/15-min per-IP limiter. Wait for the lock to expire. If you're behind a reverse proxy,
enable `trust_proxy` so all users are not lumped onto one proxy IP; never enable it on direct connections.

**Q: A plugin shows `crash-loop`.**
A: Check its log/`last_err` in the Plugins page, fix the plugin, then press Stop (reset) and Start again.

**Q: I want to update and keep the old version.**
A: `nas.sh update` keeps the most recent `bin/nasd.bak`; roll it back manually (replace `bin/nasd`, restart).

**Q: `nas.sh start` reports success but nasd disappears within seconds (no log), while a manual `nohup` keeps running.**
A: This is the system reclaiming **orphan processes**: Android / many CN ROMs SIGKILL any process whose
parent has exited (PPID=1). A same-UID process can signal it without root; nasd is SIGKILLed before it can
write a log, so it looks like a silent exit. Empirically every "backgrounding" approach was killed on device:
`setsid`, tmux daemonization (its server is also PPID=1), and a self-detached supervisor — the only thing that
works is an **always-alive parent chain**. So nas.sh uses the **foreground model**: `bash nas.sh start` blocks,
nasd runs as this script's child (interactive shell → nas.sh → nasd all alive), logs stream to the window, and
Ctrl+C stops it gracefully; for a detached session use `nohup bash nas.sh start &` (the parent chain must stay
alive). To survive Android-level reclamation of the whole app, you still need `termux-wake-lock`, battery
optimization whitelisting, and not swiping Termux away.

**Q: Where is my data?**
A: Everything is under `~/nas/`: `data/nas.db` (sessions/shares/backup jobs), `data/config.json`, `files/`.
Back up = copy `~/nas/` (plug-in binaries can be re-downloaded from the market).

**Q: Can I run this on a normal desktop Linux?**
A: Yes — `make build`, then `NAS_ROOT=/tmp/nas ./bin/nasd -root /tmp/nas`; manage via `bash nas.sh ...`
(the script handles non-Termux Linux transparently). On Windows it also runs for development
(single-instance uses a mutex) — full test coverage needs Linux/WSL2/Termux.

---
*Generated from a full analysis of the repository at the time of writing (README v2.x, nas.sh 3.0.0,
Go modules per go.mod).*
