# 客户端生命周期、任务与进度

修改启动退出、任务队列、下载/保存、取消、并发或进度反馈前，先读本文件。NAS 故障现场与恢复操作见 [AGENTS_DEPLOY.md](AGENTS_DEPLOY.md)。

## 调用链与入口

```text
Telegram update → client/bot/handlers → core.AddTask(Executable) → pkg/queue (FIFO, multiple workers)
    → core/tasks/* (download via common/tdler) → storage.Storage → progress feedback
```

- `main.go`、`cmd/run.go`、`client/bot/bot.go`、`client/middleware/`：启动、退出、重试与 floodwait。
- `core/core.go`、`pkg/queue/`：worker、生命周期 hook、AddTask/CancelTask、FIFO 队列及重复 TaskID 检查。
- `common/tdler/dler.go`：统一下载入口；`core/tasks/batchtfile/`：批量执行与进度。
- `core/tasks/batchtfile/item_progress.go`：Downloading/Transferring/Uploading/Retrying/Confirming、FailureStage。
- `common/utils/ioutil/progress_reader.go`、`storage/local/local.go`：读进度回调与本地保存。

## 启动与执行契约

- **Startup sequence**: `cmd.Run` loads Config, then `initAll` runs Cache → i18n → Database → Storage → Parser plugins → Userbot → API → Bot; `core.Run` starts workers afterward. Preserve this order. `bot.Init` returns `shouldRestart`, currently signalled only by the special dispatcher error `SAVEANTBOT-RESTART`; `cmd.Run` then cancels the application context. Restart depends on the external supervisor. Do not assume every client background exit reaches this channel.
- **Task pipeline**: handlers build a task (via `core.AddTask`), the queue executes `Executable{Type, Title, TaskID, Execute(ctx)}` with `config.C().Workers` workers. Lifecycle hooks (`TaskBeforeStart/Success/Fail/Cancel`) run around `Execute`. Cancellation = canceling the task's context; tasks must check `ctx.Err()`.
- **Dual progress channels**: (1) `pkg/taskevent` context bus (consumed by `api/` for HTTP/Webhook consumers — `taskevent.WithSink` must be injected for API tasks), (2) Telegram message edits via `ProgressTracker` + `tgutil.ExtFromContext(ctx)` (bot tasks). Upload progress currently reaches the Telegram channel only.
- **Cancellation**: queue tasks carry a `WithCancel`-derived ctx; check `ctx.Err()` in loops; classify with `errors.Is(err, context.Canceled)`.

## Context、进度与消息

- **Context values** (read from the passed ctx, never globals): `log.FromContext`, `tgutil.ExtFromContext` (Telegram ext — **required for message edits; if nil, edits are silently dropped**), `storage.FromContext`, `storagetypes.WithSourceCaption`, `ctxkey.ContentLength` / `ctxkey.OverwriteExisting`.
- **Progress rendering** (#228 convention): i18n templates declare styles with Telegram HTML (`<b>/<code>/<blockquote>/<i>`); dynamic data MUST go through `i18n.T(key, tgutil.EscapeHTMLTemplateData(data))` before `tgutil.RenderHTML`. Never interpolate user data raw, never render-then-substring-search.
- **Progress tracking**: each task package defines its own small `ProgressTracker` interface; optional `UploadProgressTracker` is probed via type assertion (skip if absent). Protect shared state and preserve notification order; throttle edits (≥1s); aggregate per-item progress monotonically. Inspect the blocking path below before changing locks or message delivery.
- **Concurrency**: reuse `errgroup.WithContext` + `SetLimit(config.C().Workers)`, `atomic.Int64` counters and `sync.Once` where appropriate. Inspect actual lock and callback ordering: `ProgressReadSeeker.Read` invokes progress synchronously, and batch upload callbacks currently run under `uploadMu`; message edits can block that path. Do not assume callbacks are asynchronous or already outside locks. Any fix must preserve notification order, throttling, cancellation and terminal updates, with a focused blocking-callback regression test.
- **Message edits**: `ext.EditMessage(chatID, &tg.MessagesEditMessageRequest{...})`; cancel buttons via `tgutil.BuildCancelButton(taskID)`; callback payloads via `pkg/tcbdata` + `common/cache`.

## 验证与诊断

- 按 [AGENTS_DEV.md](AGENTS_DEV.md) 运行相关包测试；并发、共享状态与取消改动加 `-race`，用可控假客户端/阻塞回调覆盖问题，不连接正式 Telegram session。
- NAS 本地存储的“上传/保存”是缓存复制到 downloads。分别核对下载、复制、进度编辑；任务进入保存阶段仍占用 worker。
- 外层任务和批量内部都使用 workers，多个批量会放大实际文件并发；threads 控制文件分块，小文件可能减少线程。TCP 连接数不等于任务或线程数。
- 历史现场只用于确定复现与排查入口；具体版本、采样限制和操作边界见部署分片，不把推理写成已确认根因。
