# Issue #248：异步消息编辑实施与实跑交接

状态：实现已完成并通过离线回归；真实并发运行已验证文件处理可以持续推进，消息按钮参数错误尚未修复，另有连接失效问题待定位。本次为阶段性提交，不代表全部线上验收通过。

## 当前代码与约束

- 分支 `codex/fix-248-progress-rpc`，实现基线 `2957438ce8978c144c65356717fe9440cb53f99b`。沿用此分支，不覆盖其他未提交材料。
- 全部原 Bot 编辑调用迁移到统一异步入口：2 个 worker，1024 个待发消息键，同消息串行、待发快照合并、终态封口及新任务接管。
- 新任务优先：队满先淘汰旧中间状态，若全是终态则允许淘汰最旧待发终态并记日志；不承诺终态必达。淘汰的旧生命周期不能重新抢占。
- 共享编辑限速为每秒 1 次、burst 1，位于已有重试/等待链内。验收目标是多任务并发下减少编辑请求、避免触发限流，并保持下载/保存推进；不以等待真实 FLOOD_WAIT 为前提。
- 初次 Reply/SendMessage 仍同步取得消息 ID。保留业务锁边界和文件任务队列；无新依赖或配置项。gotd/td 保持 v0.149.0。
- 关键文件：`common/msgedit/{pool,request}.go`、`common/utils/tgutil/edit.go`、`client/middleware/edit_limit.go`、`client/bot/bot.go`，以及各任务进度与 handler 调用迁移。
- 开发与 Tester 必须为不同 worker，模型不超过 gpt-6-astra，思考级别不超过 high；主会话负责最终验收。CLI 流超时须等连接 fallback 最终结果，不能在中途重连时判失败。

## 待修复：REPLY_MARKUP_INVALID

2026-09-29 13:05 的累计日志有 9 次 `rpc error code 400: REPLY_MARKUP_INVALID`，由 `common/msgedit/pool.go` 的发送 worker 记录。它是 Telegram 拒绝 reply markup（消息按钮结构），不是限流或并发被拦。

优先检查这些实际存在的空键盘路径：

- `common/utils/tgutil/edit.go`：Final 阶段强制 `SetReplyMarkup(&tg.ReplyInlineMarkup{})`，取消通知也使用空 inline markup。
- `common/msgedit/request.go`：存在 reply_markup 标志但值为 nil 时转换为空 inline markup；合并时继承未显式覆盖的字段。
- `client/bot/handlers/utils/shortcut/tftask.go`：批量排队提示显式传空 inline markup。

这是优先复现假设，尚未用最小用例证明具体哪种编码触发拒绝，不能把“空键盘就是根因”写成已确认结论。应核对编辑消息的清除按钮语义、字段标志与合并行为，做最小修复；不得借机重写编辑池或升级 SDK。

已知时间：首个成功任务 12:12:47，紧接 12:12:48 编辑失败；12:34:23–25 新增三次相同错误。后续下载仍进行。错误会导致该次状态/按钮编辑未应用；当前 worker 记录后释放 active 项，不会因这个 400 错误永久占用编辑槽。

离线 fake sender 测试没有验证 Telegram 服务端是否接受该按钮参数，因此此前离线 PASS 不能覆盖这一实跑问题。真实验证不要自行向用户发送消息；优先离线复现和协议核对，再沿用用户提交的测试流程。

## 待定位：connection dead

12:44:09 和 12:44:19 两个顶层任务失败，错误链为：

```text
failed to download file: get file: get next chunk: recovery: waitSession: connection dead
```

实际部署所用 SDK 的 `telegram/internal/manager/conn.go::waitSession` 在连接 dead 信号触发时返回 `pool.ErrConnDead`。能确认下载下一块文件时连接已失效，恢复没有成功，不能确认首次失效原因。代理链路、Telegram 端断连和 SDK 连接恢复都是待排查方向；没有证据认定与新编辑并发有关。

该时间段 mihomo user journal 没有日志，不等于代理没有异常；当前 info 级别证据未保留底层首个断连原因。后续其他连接继续下载。之前两份缓存长期不更新是事实，但仅凭 inode/文件大小不能断言它们与最终失败任务一一对应（inode 会复用）。不要改写为整个 Bot 卡死。

## 真实运行结果（最后观察时间 2026-09-29 13:05，Asia/Shanghai）

- 11 个顶层任务开始，6 成功、2 失败，3 个批量任务尚无终态。
- 三批分别于 12:39:23、12:40:52、12:44:09 开始。
- 下载目录有 9 个文件，共约 5.93 GiB；6 个缓存文件在 20 秒窗口内全部增长，已分配空间增长约 120 MiB（约 6 MiB/s）。文件数与顶层任务数不能混用。
- 未观察到 FLOOD_WAIT、engine was closed 或编辑队列淘汰；上述 400 错误不属于限流。未声称未来不会限流。
- Bot 和 mihomo 均未重启，磁盘剩余约 404.5 GiB。新 session 必须重新核对当前状态。

## ubuntu209 测试环境

SSH 使用本机 `~/.ssh/config` 的 `ubuntu209` 别名。NAS 正式服务没有改动，不要操作 NAS 任务或复用其 session。

- Bot：`@test_saveanybot_bot`，用户级服务 `saveany-bot-test.service`。
- 运行根目录：`/home/lumine/Projects/Services/saveanybot/run`。
- 二进制：`run/saveany-bot`，SHA256 `19214baea5017a755785b15979c5e43296c5d488a2550d46997b4c5e486749aa`，对应已测试的 implementation-3 源码快照。
- 配置 `run/config.toml`（0600），数据库与全新 session 在 `run/data`，下载在 `run/downloads`，临时文件在 `run/cache`；均与正式环境隔离。不得提交或输出配置凭据、session、数据库及资源名称。
- 参数 workers=4、threads=6、stream=false。默认存储已设为 `ubuntu209测试盘`、根目录；用户 `silent=1`，新资源直接保存，不再选盘。相关修改前的数据库备份在 `run/backups`。
- Go：`~/Projects/Services/saveanybot/tools/go1.25.10/bin/go`；源码：`~/Projects/Services/saveanybot/src/implementation-3`。
- Bot 日志在 user journal。HTTP API 未开启，不要为了查看任务状态开启 API 或重启实例。
- 原 Clash Verge 已按用户指令卸载；新 `mihomo.service` 采用 NAS 同版本 Mihomo v1.19.28、MetaCubeXD v1.268.4、Node v22.23.0 与 TUN 配置。mixed 7890、controller 9090、面板 18090；凭据只在远端私有配置中。旧代理配置备份在 `run/old-proxy-backup`。
- 用户正在提交真实测试任务。检查/修复代码不等于可以中断任务；部署前先核对在途任务与已有授权。
- **自动化 `ubuntu209-bot` 已按用户明确要求暂停，不要自行恢复。**

只读检查：

```sh
ssh ubuntu209 'systemctl --user show saveany-bot-test.service mihomo.service -p ActiveState -p MainPID -p NRestarts'
ssh ubuntu209 'df -h /home; du -sh ~/Projects/Services/saveanybot/run/cache ~/Projects/Services/saveanybot/run/downloads'
```

读取 journal 后先脱敏再输出，避免资源名、链接和凭据出现在工具输出/报告。

## 离线验证与本地证据

开发 Astra/high，独立 Tester 和评审 Sol/high。Ubuntu Go 1.25.10 的定向测试、race、全量离线回归、vet、默认及精简静态构建通过。全量只跳过缺少既有媒体 fixture 的 `TestCreateSplitZip`、`TestExtractThumbFrame`、`TestGetVideoMetadata`。同消息新任务接管、旧生命周期过期、真实直链入口排队即取消的回归均通过。

本机证据目录 `reviews/issue-248-async/orchestration-1/` 未纳入提交，保留在工作区：

- `tester/acceptance-3.md`、`tester/deployment-ubuntu209.md`：独立离线与部署验收。
- `tester/implementation-3.manifest`：358 文件清单，SHA256 `90b7e6b13e9747e0264d7b0e66032d582d3b71fb0f08b9f7fa8692907cd32221`；本次提交前逐项复核一致。
- `monitor/manual-status-20260929-1305.md` 及对应 JSON：最新手动实跑证据。
- `monitor/known-findings.json`：历史问题记录；以后以更晚实测为准。
- `review-package.md`、`summary.md`：部署前离线验收记录，里面“未部署/未实跑”是当时状态，不代表当前状态。

`docs/issue-248-async-message-edit-draft.md` 是本机保留的原草案，仍标有“尚未实现”，不作为当前状态依据。`reviews/nas-incidents/` 是先前材料，未修改或纳入本次提交。
