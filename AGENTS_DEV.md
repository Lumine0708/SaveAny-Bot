# 本机开发与验证

构建、测试、代码生成、安装工具或本机起服务前，先读本文件。修改前另读 [AGENTS_BRANCHING.md](AGENTS_BRANCHING.md)；正式 NAS 操作见 [AGENTS_DEPLOY.md](AGENTS_DEPLOY.md)。

## Development Commands

以下是按需使用的命令，不是每次都执行的清单。纯文档改动检查内容、链接和 diff；Go 改动先验证受影响包，再按下方 Testing & QA 扩大范围。

```bash
# Build (standard; CGO_ENABLED=0 for static; does not start the Bot)
CGO_ENABLED=0 go build -trimpath -o saveany-bot .

# CLI help; the executable entry point is the repository root
go run . --help

# Targeted test examples; select packages matching the actual change
go test ./pkg/queue
go test -run '^TestCancelAndActiveLength$' ./pkg/queue
go test -race ./core/tasks/batchtfile ./common/utils/ioutil ./pkg/queue

# Full offline regression when the three media fixtures/tools are unavailable
go test ./... -skip 'Test(CreateSplitZip|ExtractThumbFrame|GetVideoMetadata)$'
# With those fixtures and ffmpeg/ffprobe available, use go test ./...

# Generate only the affected outputs
go generate .                   # i18n keys; no go-enum required
go generate ./pkg/enums/storage # example: after changing the storage ENUM comment
# go generate ./... regenerates all; go-enum must already be on PATH

# Whole-repository static check when required by the change
go vet ./...
```

只对本次修改的 Go 文件运行 `gofmt -w`。不要为局部改动执行全仓格式化、`go get -u` 或无关的 `go mod tidy`。枚举和 i18n 生成文件随源文件一起核对，只保留对应生成差异；缺少 go-enum 时说明工具缺口，不手改生成文件或顺手升级依赖。

**Build variants**: `Dockerfile` is the default build (no feature tags); `Dockerfile.micro` uses `no_jsparser,no_minio,no_bubbletea`; `Dockerfile.pico` adds `sqlite_glebarez`. `no_playwright` is a separate supported tag for retaining JS without Playwright. When touching optional code or shared interfaces, check the affected `*_stub.go` / `*_glebarez.go` path and compile that variant.

Docker: `docker build -t saveany-bot .` only builds an image. `docker-compose.yml` starts the published `ghcr.io/krau/saveany-bot:latest`; use `docker compose -f docker-compose.local.yml up -d --build` only for an authorized local run with isolated data. Both compose files use host networking and mount `./data ./config.toml ./downloads ./cache`; on macOS verify host-network support before using them. NAS production uses systemd, not these compose files.

CI (`.github/workflows/`) runs **no tests/lint**. `v*` tag pushes trigger release/image publishing; docs changes pushed to `main` can trigger Pages deployment. Treat those pushes as publishing operations, not ordinary verification. The image workflow currently targets `krau/saveany-bot`; verify the fork's publication target before any release.

## 本机运行与隔离

- 编译和离线测试无需启动真实 Bot。确需联调时使用独立测试 Bot/session 和配置，入口是 `go run . --config /absolute/path/to/test-config.toml`；`cmd/` 是普通 Go 包，不是可运行的 main 包。
- 在测试配置中分别隔离 `db.path`、`db.session`、`telegram.userbot.session`、`temp.base_path` 和本地存储 `base_path`，同时核对日志、hook、watch 与外部存储设置。不要复制或复用正式 session 启动第二个实例；单改一个缓存路径不构成隔离。
- 启动会执行 `database.Init` 的 AutoMigrate 和 `syncUsers`（删除不在配置中的用户），退出可能执行 `cleanCache`。不要拿正式数据库、session、下载目录或已有缓存做启动测试；迁移失败日志中的“删除数据库重试”不是删除数据的授权。
- 不提交真实配置、session、SQLite 文件及 WAL/SHM、下载资源、缓存、日志、编译产物或一次性运维脚本；检查 `.gitignore`，不要假定所有敏感路径都已忽略。必要回归测试仍放现有包的 `*_test.go` 并使用 `t.TempDir()`。
- 本机工具链、代理、缓存路径和端口适配只作用于本次进程或私有配置，不硬编码进业务代码，不改系统级工具链。结束时停止本次启动的测试进程，不按进程名批量终止其他实例。
- 本机 HTTP 验证绑定 loopback，使用 `httptest` 或隔离配置；API 入口契约见 [AGENTS_BACKEND.md](AGENTS_BACKEND.md)。数据库与 session 语义见 [AGENTS_DATABASE.md](AGENTS_DATABASE.md)。

## Runtime/Tooling Preferences

- **Go 1.25+**: `t.Context()`, `sync.WaitGroup.Go`, `for range n` are available.
- **Runtime binaries**: ffmpeg/ffprobe (media processing/video split), yt-dlp (ytdlp tasks), aria2 optional; Playwright browsers install on demand to `./playwright` (`playwright.Install(chromium, ...)` at first `pw.get()`); Docker images: default has ffmpeg+yt-dlp, micro only curl, pico is scratch static.
- **No Makefile, no golangci.yml, no test/lint CI** — verification is manual (`go vet`, `go test`).
- **go-enum** required externally for enum generation; **geni18n** is in-repo.
- **Docs**: Hugo site in `docs/` (separate go.mod, hugo-book); edit `docs/content/{zh,en}/` — keep both languages mirrored. `docs/public/` is gitignored build output.
- **gitignored fixtures**: `storage/telegram/tests/` (missing — 3 tests fail locally), `data/`, `config.toml`, `playwright/`, `testplugins/`.

## Testing & QA

- **按改动选择验证**：纯文档检查事实、路径、链接和 `git diff --check`；Go 逻辑运行相关包测试并编译；涉及队列、共享进度或取消时对相关包加 `-race`；涉及 build tags 时补对应裁剪构建。跨模块改动及 Go 改动推送前运行全量离线测试和 `go vet ./...`，不要用 CI 发布成功替代回归。
- Pure stdlib `testing` (no testify); table-driven (`[]struct{name...}` + `t.Run`) with `t.Fatalf` got/want assertions. Mock via hand-written interface impls or package-variable replacement (`runMediaTool` in `video_split_test.go`, restored with `t.Cleanup`); in-process services for HTTP (`httptest`), S3 (`gofakes3+s3mem`), WebDAV (`x/net/webdav`).
- **Locale-dependent tests**: pin with `i18n.Init("zh-Hans")` + `t.Cleanup(...)`.
- **Progress/HTML tests**: assert rendered text with `strings.Contains` AND entity counts (`tg.MessageEntityBold/Code/Blockquote/Italic`) — verify style injection stays escaped (`<b>A&B</b>` input must render as literal text).
- **Known failures**: `storage/telegram` `TestCreateSplitZip`, `TestExtractThumbFrame`, `TestGetVideoMetadata` need gitignored fixtures + real ffmpeg — skip with `-skip 'Test(CreateSplitZip|ExtractThumbFrame|GetVideoMetadata)$'`; `api/handlers_test.go` has one `t.Skip` (needs initialized core).
- **Coverage expectations**: cover changed behavior and its main regression risks; pure logic uses table tests (parsers, URL/path utils, progress throttling, grouping), regressions use bug-scenario-named tests (`progress_regression_test.go`). Offline tests must not contact real Telegram, remote storage or websites, or launch real Playwright browsers; loopback `httptest` servers are expected. Restore package globals/config/locale with cleanup and avoid parallel tests that mutate shared state.
- **结果报告**：记录实际命令、通过/失败、跳过原因与未覆盖边界。三个已知媒体测试缺 fixture 不等于所有 `storage/telegram` 失败都可忽略；其他失败要核对是否由本次改动引起。资源不足、工具缺失或没有执行的检查，不写成已通过。

涉及配置、文档、插件契约或 i18n 的同步要求见 [AGENTS_BACKEND.md](AGENTS_BACKEND.md)。
