# Go、API、存储与插件

修改 Go 业务、配置、HTTP API、存储接口或 JS 插件前，先读本文件。任务执行/进度另读 [AGENTS_TASKS.md](AGENTS_TASKS.md)，持久化另读 [AGENTS_DATABASE.md](AGENTS_DATABASE.md)，构建与测试命令见 [AGENTS_DEV.md](AGENTS_DEV.md)。

## Key Directories

| Path | Purpose |
|---|---|
| `cmd/` | Root command calls `Run` (main bot); `upload`, `watch` are standalone subcommands that do NOT run initAll; `geni18n` is the i18n key generator |
| `core/` | `Executable` interface, queue worker loop, hooks; `core/tasks/{tfile,batchtfile,directlinks,parsed,telegraph,transfer,ytdlp,aria2dl}` |
| `client/` | `bot/` (gotgproto client + handlers), `middleware/` (shared retry/floodwait), `user/` (userbot) |
| `storage/` | 7 registered backends + `storage.go` (interfaces/registry) + `load.go` (per-user storage resolution) |
| `parsers/` | `parsers.go` (registry), `js/` (Goja plugins, ghttp/playwright injection, build-tagged), `parsers/` (native: twitter, kemono) |
| `config/` | Viper setup, defaults, `storage/` per-type config structs |
| `database/` | GORM models (User/Dir/Rule/WatchChat), AutoMigrate, `syncUsers` |
| `pkg/` | `queue`, `taskevent`, `tcbdata` (callback data), `rule`, `enums/{tasktype,storage,ctxkey,fnamest}`, `storagetypes`, `tfile`, `parser` |
| `common/` | `tdler` (unified downloader), `utils/{tgutil,dlutil,ioutil,fsutil,strutil,tphutil,netutil}`, `i18n` (embedded locales), `cache` (ristretto) |
| `api/` | HTTP API + webhook (task factory with sink injection) |
| `docs/` | Hugo site (hugo-book theme, zh+en mirrored), separate go.mod |
| `plugins/` | JS parser examples + `README.md` (plugin author contract) |

## 编码与配置

- **Imports**: stdlib → third-party → project-internal, blank-line separated. Aliases for clarity (`storconfig`, `storenum`).
- **Naming**: PascalCase exported, camelCase unexported, files `snake_case.go`; **not** ALL_CAPS constants.
- **Errors**: always wrap with `fmt.Errorf("context: %w", err)`; check with `errors.Is/As`; never ignore.
- **Logging**: `log.FromContext(ctx)` with prefixes (`logger.WithPrefix("component")`); never global logger when ctx is available.
- **Config layering**: bound CLI flag > env `SAVEANY_*` (dots → underscores, e.g. `SAVEANY_TELEGRAM_TOKEN`) > TOML file (path or http(s) URL). `config.C()` returns a **shallow copy** — treat it and its referenced slices/pointers as read-only; modifying the copy is not a configuration update mechanism.
- `config/viper.go`、`config/flags.go`、`config.example.toml` 是配置结构、绑定与字段说明的核对入口。

## HTTP API

- 先读 `api/server.go`、`api/auth.go`、`api/factory.go`、`api/progress.go`、`api/webhook.go` 及 [API 文档](docs/content/zh/usage/api.md)。
- HTTP API 正常入口是 `api.Start`，启用时必须配置 token；`NewServer` 是构造函数，不等于完整启动校验。保持现有 Bearer token 与存储/用户边界；本机 HTTP 验证绑定 loopback，使用 `httptest` 或隔离配置。
- API 任务通过 `TaskFactory.registerAndEnqueueTask` 注入 `taskevent.WithSink` 后进入 `core.AddTask`；复用该链路，具体进度语义见 [AGENTS_TASKS.md](AGENTS_TASKS.md)。

## 存储与插件

- **Capability interfaces + type assertion fallback** is the core extensibility pattern: `StorageBatchSaver`, `StorageProgressSaver` (upload progress), `StorageListable`, `StorageReadable` are optional; consumers assert and fall back (e.g. wrap reader with `ioutil.NewProgressReader`). New backends only need to implement the interface and register.
- **Storage registration (3 places)**: `pkg/enums/storage` ENUM comment (go-enum), `config/storage/factory.go::storageFactories` (config struct with `Validate()`), `storage/storage.go::storageConstructors` (implementation).
- 存储修改先读 `storage/storage.go`、`storage/load.go`、目标后端及其测试、`pkg/storagetypes/`，保留按用户解析存储的边界。
- **JS plugins**: `registerParser({metadata, canHandle, parse})`, `version >= 1.0.0`; per-plugin goja VM is single-goroutine (reqCh buffer 10). Changing `pkg/parser.Item/Resource` JSON fields requires updating `plugins/README.md` and example plugins.
- 插件契约见 [plugins/README.md](plugins/README.md)，运行时见 `parsers/js/plugin.go`；可选实现与 stub 的构建验证见开发分片。

## i18n、注册与文档同步

- **i18n**: edit `common/i18n/locale/{zh-Hans,en}.yaml` → `go generate .` → use `i18nk.<Key>` constants. No raw strings in user-facing Telegram messages. zh-Hans and en must stay in sync; API error codes/JSON follow the existing API contract.
- **Registration points** (never forget): new bot command → `client/bot/handlers/register.go::CommandHandlers` (auto-publishes /help menu); new task type → `pkg/enums/tasktype` + `core/tasks/<name>/` + `api/factory.go::CreateTask`; new storage → 3 places above + `docs/content/{en,zh}/deployment/configuration/storages.md`; new enum value → ENUM comment + `go generate`.
- When a permanent feature/API change ships: update `config.example.toml` if config, mirrored `docs/content/{zh,en}/` if user-facing, `plugins/README.md` if plugin contract, and i18n YAML + `go generate .` for new strings. Only update the documents/contracts affected by the change.
- `main.go` 声明 i18n 生成命令，`client/bot/handlers/register.go` 定义分发顺序及 `CommandHandlers`。生成方式见开发分片；进度 HTML 的转义与消息编辑见任务分片。
