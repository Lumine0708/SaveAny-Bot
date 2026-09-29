# Repository Guidelines

## Project Overview

SaveAny-Bot is a Telegram bot written in Go that saves files and messages from Telegram and websites to multiple storage backends (Local, S3, MinIO, WebDAV, AList, Rclone, Telegram). It supports single-file saves, batch/album saves, streaming, multi-user access, storage rules, cross-storage transfers, yt-dlp and Aria2 downloads, and a Goja-based JavaScript parser plugin system with optional Playwright browser automation.

**Tech stack**: Go 1.25, gotgproto + gotd/td v0.149.0 (MTProto), Cobra (CLI), Viper (config), GORM + SQLite, Goja (JS runtime), Playwright, charmbracelet/log. License: AGPL-3.0.

**Note on gotd versions**: `gotd/td` must stay on `v0.149.0` — v0.150+ breaks `gotgproto` (v1.0.0-beta22) compilation (`AsInputDocumentFileLocation` signature, `gotd/log` Logger interface). Do not bump beyond v0.149.

## Architecture & Data Flow

```
Telegram update → client/bot/handlers → core.AddTask(Executable) → pkg/queue (serial workers)
    → core/tasks/* (download via common/tdler) → storage.Storage → progress feedback
```

- **Startup sequence** (`cmd/run.go::initAll`, keep this order): Config → Cache → i18n → Database → Storage → Parser plugins → Userbot → API → Bot. `bot.Init` returns the exit channel; a `SAVEANTBOT-RESTART` error restarts the process (external supervisor).
- **Task pipeline**: handlers build a task (via `core.AddTask`), the queue executes `Executable{Type, Title, TaskID, Execute(ctx)}` with `config.C().Workers` workers. Lifecycle hooks (`TaskBeforeStart/Success/Fail/Cancel`) run around `Execute`. Cancellation = canceling the task's context; tasks must check `ctx.Err()`.
- **Dual progress channels**: (1) `pkg/taskevent` context bus (consumed by `api/` for HTTP/Webhook consumers — `taskevent.WithSink` must be injected for API tasks), (2) Telegram message edits via `ProgressTracker` + `tgutil.ExtFromContext(ctx)` (bot tasks). Upload progress currently reaches the Telegram channel only.
- **Capability interfaces + type assertion fallback** is the core extensibility pattern: `StorageBatchSaver`, `StorageProgressSaver` (upload progress), `StorageListable`, `StorageReadable` are optional; consumers assert and fall back (e.g. wrap reader with `ioutil.NewProgressReader`). New backends only need to implement the interface and register.
- **Config layering**: CLI flag > env `SAVEANY_*` (dots → underscores, e.g. `SAVEANY_TELEGRAM_TOKEN`) > TOML file (path or http(s) URL). `config.C()` returns a **copy** — never mutate it.
- **Storage registration (3 places)**: `pkg/enums/storage` ENUM comment (go-enum), `config/storage/factory.go::storageFactories` (config struct with `Validate()`), `storage/storage.go::storageConstructors` (implementation).

## Key Directories

| Path | Purpose |
|---|---|
| `cmd/` | CLI: `run` (main bot), `upload`, `watch` (standalone subcommands that do NOT run initAll), `geni18n` (i18n key generator) |
| `core/` | `Executable` interface, queue worker loop, hooks; `core/tasks/{tfile,batchtfile,directlinks,parsed,telegraph,transfer,ytdlp,aria2dl}` |
| `client/bot/` | gotgproto client, `handlers/` (all commands + message/callback handlers), `middleware/`, `client/user/` (userbot) |
| `storage/` | 8 backends + `storage.go` (interfaces/registry) + `load.go` (per-user storage resolution) |
| `parsers/` | `parsers.go` (registry), `js/` (Goja plugins, ghttp/playwright injection, build-tagged), `parsers/` (native: twitter, kemono) |
| `config/` | Viper setup, defaults, `storage/` per-type config structs |
| `database/` | GORM models (User/Dir/Rule/WatchChat), AutoMigrate, `syncUsers` |
| `pkg/` | `queue`, `taskevent`, `tcbdata` (callback data), `rule`, `enums/{tasktype,storage,ctxkey,fnamest}`, `storagetypes`, `tfile`, `parser` |
| `common/` | `tdler` (unified downloader), `utils/{tgutil,dlutil,ioutil,fsutil,strutil,tphutil,netutil}`, `i18n` (embedded locales), `cache` (ristretto) |
| `api/` | HTTP API + webhook (task factory with sink injection) |
| `docs/` | Hugo site (hugo-book theme, zh+en mirrored), separate go.mod |
| `plugins/` | JS parser examples + `README.md` (plugin author contract) |

## Development Commands

```bash
# Build (standard; CGO_ENABLED=0 for static)
CGO_ENABLED=0 go build -trimpath -o saveany-bot .
go run ./cmd

# Test — known failures: storage/telegram TestCreateSplitZip/TestExtractThumbFrame/TestGetVideoMetadata
# (need gitignored fixtures tests/testfile.dat, tests/testvideo; ffmpeg/ffprobe)
go test ./...
go test -race ./core/tasks/... ./storage/... ./pkg/queue/... ./common/...
go test -run TestQueueBasic ./pkg/queue

# Codegen — run after editing locale YAML or enum comments
go generate ./...            # geni18n (i18nk keys) + go-enum (pkg/enums/*)
# go-enum is NOT in go.mod; install externally. geni18n runs via go run.

# Verify
go vet ./...
go fmt ./...
```

**Build variants** (Dockerfile.default/micro/pico): `-tags=no_jsparser,no_playwright,no_minio,no_bubbletea,sqlite_glebarez` — each has a `*_stub.go`/`*_glebarez.go` pairing; keep stubs in sync.

Docker: `docker build -t saveany-bot .`, `docker compose up -d` (host network, mounts `./data ./config.toml ./downloads ./cache`). CI (`.github/workflows/`) runs **no tests/lint** — only tag-triggered release/docker builds and docs deployment; run `go test ./...` manually before pushing.

## Code Conventions & Common Patterns

- **Imports**: stdlib → third-party → project-internal, blank-line separated. Aliases for clarity (`storconfig`, `storenum`).
- **Naming**: PascalCase exported, camelCase unexported, files `snake_case.go`; **not** ALL_CAPS constants.
- **Errors**: always wrap with `fmt.Errorf("context: %w", err)`; check with `errors.Is/As`; never ignore.
- **Logging**: `log.FromContext(ctx)` with prefixes (`logger.WithPrefix("component")`); never global logger when ctx is available.
- **Context values** (read from the passed ctx, never globals): `log.FromContext`, `tgutil.ExtFromContext` (Telegram ext — **required for message edits; if nil, edits are silently dropped**), `storage.FromContext`, `storagetypes.WithSourceCaption`, `ctxkey.ContentLength` / `ctxkey.OverwriteExisting`.
- **Progress rendering** (#228 convention): i18n templates declare styles with Telegram HTML (`<b>/<code>/<blockquote>/<i>`); dynamic data MUST go through `i18n.T(key, tgutil.EscapeHTMLTemplateData(data))` before `tgutil.RenderHTML`. Never interpolate user data raw, never render-then-substring-search.
- **Progress tracking**: each task package defines its own small `ProgressTracker` interface; optional `UploadProgressTracker` is probed via type assertion (skip if absent). Serialize state + message edits with a mutex; throttle edits (≥1s); aggregate per-item progress monotonically.
- **i18n**: only edit `common/i18n/locale/{zh-Hans,en}.yaml` → `go generate ./...` → use `i18nk.<Key>` constants. No raw strings in user-facing messages. zh-Hans and en must stay in sync.
- **Registration points** (never forget): new bot command → `client/bot/handlers/register.go::CommandHandlers` (auto-publishes /help menu); new task type → `pkg/enums/tasktype` + `core/tasks/<name>/` + `api/factory.go::CreateTask`; new storage → 3 places above + `docs/content/{en,zh}/deployment/configuration/storages.md`; new enum value → ENUM comment + `go generate`.
- **Concurrency**: `errgroup.WithContext` + `SetLimit(config.C().Workers)`, `atomic.Int64` counters, `sync.Once` for single-shot events, mutex around render state. No lock-in-callback (callbacks fire after unlock).
- **Cancellation**: queue tasks carry a `WithCancel`-derived ctx; check `ctx.Err()` in loops; classify with `errors.Is(err, context.Canceled)`.
- **JS plugins**: `registerParser({metadata, canHandle, parse})`, `version >= 1.0.0`; per-plugin goja VM is single-goroutine (reqCh buffer 10). Changing `pkg/parser.Item/Resource` JSON fields requires updating `plugins/README.md` and example plugins.
- **Message edits**: `ext.EditMessage(chatID, &tg.MessagesEditMessageRequest{...})`; cancel buttons via `tgutil.BuildCancelButton(taskID)`; callback payloads via `pkg/tcbdata` + `common/cache`.

## Important Files

- `main.go` — `//go:generate` for i18n keys
- `cmd/run.go` — startup sequence `Run/initAll/cleanCache` (cache cleanup on exit, `NoCleanCache` opt-out)
- `core/core.go` — worker loop, hooks, AddTask/CancelTask
- `pkg/queue/queue.go` — generic serial queue (cond/list; duplicate TaskID rejected)
- `storage/storage.go` — interfaces + registry + compile-time capability assertions
- `config/viper.go`, `config.example.toml` — config schema (authoritative field docs)
- `database/db.go` — GORM init, `GetDialect` (build-tag selectable SQLite driver)
- `client/bot/handlers/register.go` — handler dispatch order and CommandHandlers
- `common/tdler/dler.go` — unified download entry
- `core/tasks/batchtfile/item_progress.go` — per-item phase state machine (Downloading/Transferring/Uploading/Retrying/Confirming, FailureStage)
- `parsers/js/plugin.go` — Goja plugin runtime
- `.github/workflows/` — release/docker/docs (no test gate)

## Runtime/Tooling Preferences

- **Go 1.25+**: `t.Context()`, `sync.WaitGroup.Go`, `for range n` are available.
- **Runtime binaries**: ffmpeg/ffprobe (media processing/video split), yt-dlp (ytdlp tasks), aria2 optional; Playwright browsers install on demand to `./playwright` (`playwright.Install(chromium, ...)` at first `pw.get()`); Docker images: default has ffmpeg+yt-dlp, micro only curl, pico is scratch static.
- **No Makefile, no golangci.yml, no test/lint CI** — verification is manual (`go vet`, `go test`).
- **go-enum** required externally for enum generation; **geni18n** is in-repo.
- **Docs**: Hugo site in `docs/` (separate go.mod, hugo-book); edit `docs/content/{zh,en}/` — keep both languages mirrored. `docs/public/` is gitignored build output.
- **gitignored fixtures**: `storage/telegram/tests/` (missing — 3 tests fail locally), `data/`, `config.toml`, `playwright/`, `testplugins/`.

## Testing & QA

- Pure stdlib `testing` (no testify); table-driven (`[]struct{name...}` + `t.Run`) with `t.Fatalf` got/want assertions. Mock via hand-written interface impls or package-variable replacement (`runMediaTool` in `video_split_test.go`, restored with `t.Cleanup`); in-process services for HTTP (`httptest`), S3 (`gofakes3+s3mem`), WebDAV (`x/net/webdav`).
- **Locale-dependent tests**: pin with `i18n.Init("zh-Hans")` + `t.Cleanup(...)`.
- **Progress/HTML tests**: assert rendered text with `strings.Contains` AND entity counts (`tg.MessageEntityBold/Code/Blockquote/Italic`) — verify style injection stays escaped (`<b>A&B</b>` input must render as literal text).
- **Known failures**: `storage/telegram` `TestCreateSplitZip`, `TestExtractThumbFrame`, `TestGetVideoMetadata` need gitignored fixtures + real ffmpeg — skip with `-skip 'Test(CreateSplitZip|ExtractThumbFrame|GetVideoMetadata)$'`; `api/handlers_test.go` has one `t.Skip` (needs initialized core).
- **Coverage expectations**: pure logic gets table tests (parsers, URL/path utils, progress throttling, grouping); regressions get bug-scenario-named tests (`progress_regression_test.go`). Network/Telegram/Playwright must never be touched by tests.
- When a permanent feature/API change ships: update `config.example.toml` if config, `docs/` if user-facing, `plugins/README.md` if plugin contract, and i18n YAML + `go generate` for new strings.


## 本 fork 的 NAS 排障上下文（2026-09-21 核对）

本节用于 Lumine 的正式 NAS 使用场景；状态会变化，执行操作前需实时核对。代码修复优先局部修改、复用现有接口和依赖，不为这几个故障建设通用监控平台。原有架构说明是概括，涉及客户端退出或锁内回调时必须以实际源码和下面的现场证据为准。

### 仓库与工作边界

- origin：`Lumine0708/SaveAny-Bot`；upstream：`krau/SaveAny-Bot`，已核实 fork 关系。
- 本次独立克隆位于 `/Volumes/Lumine_2T/Projects/SaveAny-Bot-fork`。同级旧 `SaveAny-Bot` 有未提交代码和历史运维文档，禁止覆盖、重置或把旧改动当作线上实现。
- NAS 运维背景来源为同级 `nas` 项目；本目录专注 Bot 代码问题，不处理 account、Cloudflare、SQL Server、WebDAV 等无关服务。
- 三类故障、脱敏日志、采样及推理入口：[reviews/nas-incidents/2026-09-20_21/README.md](reviews/nas-incidents/2026-09-20_21/README.md)。事实、源码机制、待验证假设应分别表述。
- 此次仅整理证据和上下文，尚未实施修复。后续改代码不自动意味着可以部署、升级、重启正式服务或发送 Telegram 测试消息；按当次用户授权执行，已有授权不重复询问。

### 用户场景与正式运行环境

- 用户把 Telegram 单个媒体、多个文件/相册或消息链接发给 Bot，保存到 NAS 本地；经常连续提交多批任务，关注总体吞吐、排队、指令响应与长时间无人干预后的恢复。
- UI 的“上传/保存”在这个存储配置下是从缓存复制到本地 downloads，不是向外部网盘上传。要单独分析下载、复制、进度消息三个环节。
- NAS 使用 SSH 别名 `nas`。连接信息唯一可信来源为本机 `~/.ssh/config`，连接前读取；不在脚本硬编码主机、端口、密钥，不复制凭据。
- 主目录 `/data/500g_0/resources/tg`，二进制 `saveany-bot`，配置 `config.toml`，缓存 `cache/`，目标 `downloads/`，业务日志 `logs/YYYY/YYYY-MM/YYYY-MM-DD.log`。缓存与目标同在 sdc 的 ext4/LVM 上。
- J1900 四核，约 3.7 GiB 内存；机械盘可有其他读取竞争。剩余空间是动态值，操作前用 df 查询。
- 正式版本 v0.61.0，提交 `ae0ec2fba82bc7d7177db7fd1b3724973686b956`；本地克隆 HEAD 较新，诊断线上先对比该 tag，不能拿旧工作区修改推断线上行为。
- 当前 `workers=4`、`threads=6`、`stream=false`、日志级别 info。workers 同时影响外层任务和批量内部文件并发；多个批量会放大总分块并发，小文件又会减少线程。TCP 连接数不等于任务或线程数。
- Mihomo 提供代理/TUN；mixed port 7890，控制 API 9090，MetaCubeXD 18080。09-21 主选择仍是日本移动 1 号，节点需实时核对。控制密钥只从 NAS 活动配置读取，禁止输出或提交配置全文。
- HTTP 网页可达不保证 Telegram 文件传输可用。`/health` 固定 ok，不足以判断业务健康；任务 API 为空也不能单独证明空闲。
- `saveany-bot-tun-watchdog` 已于 09-20 按用户要求删除，禁止根据旧脚本或文档恢复它。每日 03:00 Bot 重启计划保留；流量记录服务此前已停用，不要假定存在完整历史吞吐数据。
- 09-21 21:45 按用户指令重启 Bot，21:45:50 初始化与任务处理就绪；Mihomo 未重启。PID 属于瞬时状态，不写死到命令。

### 用户可用的常见 Telegram 指令

以下语法已核对当前 handlers；不是声称用户实际使用过每一条。让用户自己发送，自动发送测试消息须有对应授权。

| 操作 | 指令 |
| --- | --- |
| 帮助 | `/help` 或 `/start` |
| 运行中的任务 | `/task` 或 `/task running` |
| 等待队列 | `/task queued` |
| 取消指定任务 | `/cancel <任务ID>` 或 `/task cancel <任务ID>`，也可点任务消息上的取消按钮 |
| 选择存储与目录 | `/storage`、`/dir` |
| 保存、静默模式 | `/save`、`/silent`，具体参数按 Bot 帮助 |

任务进入保存阶段仍会占用 worker。取消提示“任务不存在”可能是旧消息按钮/旧任务 ID，不应自动归为会话崩溃。

### 常用运维与源码核对命令

```sh
# 先在本机读取 ~/.ssh/config，连接参数沿用 nas 别名
ssh nas 'systemctl --user status saveany-bot.service mihomo.service --no-pager'
ssh nas 'systemctl --user show saveany-bot.service -p MainPID -p ActiveState -p ExecMainStartTimestamp'
ssh nas 'df -h /data/500g_0/resources/tg'
# 业务日志在 logs/YYYY/YYYY-MM/YYYY-MM-DD.log；读取时尽量筛选故障时间并脱敏
# 受控重启：仅在用户授权恢复服务或维护范围内执行
ssh nas /data/500g_0/resources/tg/bin/saveany-bot-control.sh restart
# 核对线上对应源码与本地差异
git show v0.61.0:client/bot/bot.go
git diff v0.61.0 -- client/bot cmd/run.go client/middleware core/tasks/batchtfile storage/local
```

不要单凭 health/tasks 接口决定重启；结合最新业务日志、打开的缓存/目标文件增长和进程写入计数。用户已明确要求立即重启时，不重复索要确认。默认复用现有控制脚本，不调用可能更新版本或重建旧守护的历史部署脚本。

日志分享只保留必要时间、错误链、源码位置和匿名任务关联，不提交 bot token、API token、订阅 URL、session、数据库、私钥或原始资源文件名。只读排查默认不做写盘压测、不启动长期采集服务。
