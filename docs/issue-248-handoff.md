# Issue #248：异步消息编辑实施与实跑交接

状态：异步编辑实现已通过离线回归，真实并发运行已验证文件处理可以持续推进。reply markup 最小修复已通过 ubuntu209 真实 Telegram A/B：旧版 3/3 报错，修复版 6/6 完成且无新增该错误。2026-09-29 15:25 测试服务和专用目录已清理，不再部署于 ubuntu209。连接失效问题仍待定位，不代表全部线上问题已解决。

## 当前代码与约束

- 分支 `codex/fix-248-progress-rpc`；异步编辑提交 `db2f436`，reply markup 修复提交 `a384c0d`，已推送 origin。沿用此分支，不覆盖其他未提交材料。
- 全部原 Bot 编辑调用迁移到统一异步入口：2 个 worker，1024 个待发消息键，同消息串行、待发快照合并、终态封口及新任务接管。
- 新任务优先：队满先淘汰旧中间状态，若全是终态则允许淘汰最旧待发终态并记日志；不承诺终态必达。淘汰的旧生命周期不能重新抢占。
- 共享编辑限速为每秒 1 次、burst 1，位于已有重试/等待链内。验收目标是多任务并发下减少编辑请求、避免触发限流，并保持下载/保存推进；不以等待真实 FLOOD_WAIT 为前提。
- 初次 Reply/SendMessage 仍同步取得消息 ID。保留业务锁边界和文件任务队列；无新依赖或配置项。gotd/td 保持 v0.149.0。
- 关键文件：`common/msgedit/{pool,request}.go`、`common/utils/tgutil/edit.go`、`client/middleware/edit_limit.go`、`client/bot/bot.go`，以及各任务进度与 handler 调用迁移。
- 开发与 Tester 必须为不同 worker，模型不超过 gpt-6-astra，思考级别不超过 high；主会话负责最终验收。CLI 流超时须等连接 fallback 最终结果，不能在中途重连时判失败。

## REPLY_MARKUP_INVALID：已真实复现并修复

2026-09-29 14:02（Asia/Shanghai）只读排查时，journal 累计 14 次错误：11 个顶层任务结束后各一次、3 个批量任务开始前各一次。对应终态及批量排队的空 inline markup 路径，之后用专用合成文件完成真实 A/B。

在用户授权的 Telegram `test-saveanybot` 窗口通过 Computer Use 提交测试，bot、账号、配置、session、存储和 SDK 保持一致，仅替换测试服务二进制：

| 版本与时间 | 实跑 | 结果 |
|---|---|---|
| 旧版，14:56–14:59 | 3 个 1138 字节普通文件，逐个提交 | 文件保存全部成功；3/3 终态编辑报 `REPLY_MARKUP_INVALID`，界面停在“已添加到任务队列” |
| 修复版，15:03–15:16 | 同类小文件 3 次，另加 64 MiB 两次、256 MiB 一次 | 6/6 文件保存成功；全部在 Telegram 显示“✅ 处理完成”，无新增编辑失败或 `REPLY_MARKUP_INVALID` |

全部 9 个文件的远端大小及 SHA256 与本机合成样本一致。B5 界面实际观察到“下载中 95%”，随后完成且截图无按钮；未截图捕捉完成前的取消按钮，不能将按钮出现/消失的全程写成直接目视证据。15:17 `/task` 返回没有正在运行的任务。实跑未覆盖用户主动取消、批量专用入口或下载连接恢复。

根因是旧发送路径把“清除按钮”编码成 flag 2 + 零行 `ReplyInlineMarkup`，被 Telegram 拒绝。队内仍保留显式清除标记，避免合并时继承旧按钮；仅在 worker 发送前将零行 inline markup 转为 nil 并清除 flag 2。非空按钮、SDK、调度、限速和配置不变。该表达与官方 TDLib 的[清除转换](https://github.com/tdlib/td/blob/42e6a5259551178d1dab54a22ad96d14bd906e20/td/telegram/ReplyMarkup.cpp#L295)、[请求构造](https://github.com/tdlib/td/blob/42e6a5259551178d1dab54a22ad96d14bd906e20/td/telegram/MessagesManager.cpp#L1887)一致。

最小稳定复现：旧版向测试 Bot 发送一个很小的普通文件，等待保存结束，核对 journal 终态编辑 400 和界面停留；换修复版按同流程重新提交即可看到完成。不需要大文件、并发或限流。旧消息不会被新版本自动重放修复。

新增回归 `TestReplyMarkupClearWireEncoding` 使用 SDK 真实 TL Encode/Decode：旧实现 6 个清除子例失败、4 个对照通过；修复后 10 个子例全通过。独立 Tester 复核，六包定向测试与 race 全部通过。完整证据在本机 `reviews/issue-248-reply/investigation.md`、`acceptance.md`、`live/`；实跑操作与独立证据复核分别记录。

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

## ubuntu209 测试环境：已清理

2026-09-29 15:25（Asia/Shanghai）完成清理：

- `saveany-bot-test.service` 已停止、禁用并删除 unit 和启动链接；复核为 `MainPID=0`、`LoadState=not-found`、`ActiveState=inactive`。
- `/home/lumine/Projects/Services/saveanybot` 整个专用目录已删除，清理前约 27 GiB，包含源码快照、二进制、Go 工具链及构建缓存、配置、session、数据库、下载文件、临时缓存与备份。
- 独立 Python/Web 项目 `/home/lumine/Projects/Services/saveany` 和主机 `mihomo.service` 保留；NAS 正式环境未改动。未清空主机共享 systemd journal。
- 本机代码、脱敏日志、校验及验收记录保留。当前没有可直接重启或回滚的远端 SaveAny-Bot 实例；不要再按旧部署记录重启测试服务。
- 自动化 `ubuntu209-bot` 保持暂停，不恢复。

历史 A/B 二进制身份：旧版 SHA256 `19214baea5017a755785b15979c5e43296c5d488a2550d46997b4c5e486749aa`；修复版 SHA256 `ff9b626376c3526f772437c922704d4d6cf2aa23d95c0e45cc4baf0711b86902`。相关远端二进制及备份现已删除，实跑证据仍在本机 `reviews/issue-248-reply/live/`。

## 离线验证与本地证据

开发 Astra/high，独立 Tester 和评审 Sol/high。Ubuntu Go 1.25.10 的定向测试、race、全量离线回归、vet、默认及精简静态构建通过。全量只跳过缺少既有媒体 fixture 的 `TestCreateSplitZip`、`TestExtractThumbFrame`、`TestGetVideoMetadata`。同消息新任务接管、旧生命周期过期、真实直链入口排队即取消的回归均通过。

本机证据目录 `reviews/issue-248-async/orchestration-1/` 未纳入提交，保留在工作区：

- `tester/acceptance-3.md`、`tester/deployment-ubuntu209.md`：独立离线与部署验收。
- `tester/implementation-3.manifest`：358 文件清单，SHA256 `90b7e6b13e9747e0264d7b0e66032d582d3b71fb0f08b9f7fa8692907cd32221`；本次提交前逐项复核一致。
- `monitor/manual-status-20260929-1305.md` 及对应 JSON：最新手动实跑证据。
- `monitor/known-findings.json`：历史问题记录；以后以更晚实测为准。
- `review-package.md`、`summary.md`：部署前离线验收记录，里面“未部署/未实跑”是当时状态，不代表当前状态。

`docs/issue-248-async-message-edit-draft.md` 是本机保留的原草案，仍标有“尚未实现”，不作为当前状态依据。`reviews/nas-incidents/` 是先前材料，未修改或纳入本次提交。
