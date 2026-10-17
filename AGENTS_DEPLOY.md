# NAS 部署、恢复与排障

构建 NAS 发布产物、操作远端目录、检查正式服务、重启、回滚或线上验证前，先读本文件。代码生命周期与进度机制见 [AGENTS_TASKS.md](AGENTS_TASKS.md)；本机验证见 [AGENTS_DEV.md](AGENTS_DEV.md)。

本节用于 Lumine 的正式 NAS 使用场景。以下线上版本、配置、服务与节点信息是 **2026-09-21 的历史快照**，不是当前健康状态或部署承诺；执行运维前需实时核对。代码修复优先局部修改、复用现有接口和依赖，不为这几个故障建设通用监控平台。

## 仓库与工作边界

- 仓库、旧克隆与分支边界见 [AGENTS_BRANCHING.md](AGENTS_BRANCHING.md)。
- NAS 运维背景来源为同级 `nas` 项目；本目录专注 Bot 代码问题，不处理 account、Cloudflare、SQL Server、WebDAV 等无关服务。
- 三类故障、脱敏日志、采样及推理入口：[reviews/nas-incidents/2026-09-20_21/README.md](reviews/nas-incidents/2026-09-20_21/README.md)。事实、源码机制、待验证假设应分别表述。
- 2026-09-20—21 的记录仅整理证据和上下文，未实施代码修复。后续改代码不自动意味着可以部署、升级、重启正式服务或发送 Telegram 测试消息；按当次用户授权执行，已有授权不重复询问。

## 用户场景与正式运行环境

- 用户把 Telegram 单个媒体、多个文件/相册或消息链接发给 Bot，保存到 NAS 本地；经常连续提交多批任务，关注总体吞吐、排队、指令响应与长时间无人干预后的恢复。
- UI 的“上传/保存”在这个存储配置下是从缓存复制到本地 downloads，不是向外部网盘上传。要单独分析下载、复制、进度消息三个环节。
- NAS 使用 SSH 别名 `nas`。连接信息唯一可信来源为本机 `~/.ssh/config`，连接前读取；不在脚本硬编码主机、端口、密钥，不复制凭据。
- 主目录 `/data/500g_0/resources/tg`，二进制 `saveany-bot`，配置 `config.toml`，缓存 `cache/`，目标 `downloads/`，业务日志 `logs/YYYY/YYYY-MM/YYYY-MM-DD.log`。缓存与目标同在 sdc 的 ext4/LVM 上。
- J1900 四核，约 3.7 GiB 内存；机械盘可有其他读取竞争。剩余空间是动态值，操作前用 df 查询。
- 当时正式版本 v0.61.0，提交 `ae0ec2fba82bc7d7177db7fd1b3724973686b956`；本地克隆 HEAD 较新。诊断这次历史事故对比该 tag；诊断新事故先确认实际部署版本，不能拿旧工作区修改推断线上行为。
- 当时 `workers=4`、`threads=6`、`stream=false`、日志级别 info。workers 同时影响外层任务和批量内部文件并发；多个批量会放大总分块并发，小文件又会减少线程。TCP 连接数不等于任务或线程数。
- Mihomo 提供代理/TUN；mixed port 7890，控制 API 9090，MetaCubeXD 18080。09-21 主选择仍是日本移动 1 号，节点需实时核对。控制密钥只从 NAS 活动配置读取，禁止输出或提交配置全文。
- HTTP 网页可达不保证 Telegram 文件传输可用。`/health` 固定 ok，不足以判断业务健康；任务 API 为空也不能单独证明空闲。
- `saveany-bot-tun-watchdog` 已于 09-20 按用户要求删除，禁止根据旧脚本或文档恢复它。每日 03:00 Bot 重启计划保留；流量记录服务此前已停用，不要假定存在完整历史吞吐数据。
- 09-21 21:45 按用户指令重启 Bot，21:45:50 初始化与任务处理就绪；Mihomo 未重启。PID 属于瞬时状态，不写死到命令。

## 用户可用的常见 Telegram 指令

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

## 常用运维与源码核对命令

```sh
# 先在本机读取 ~/.ssh/config，连接参数沿用 nas 别名
ssh nas 'systemctl --user status saveany-bot.service mihomo.service --no-pager'
ssh nas 'systemctl --user show saveany-bot.service -p MainPID -p ActiveState -p ExecMainStartTimestamp'
ssh nas 'df -h /data/500g_0/resources/tg'
# 业务日志在 logs/YYYY/YYYY-MM/YYYY-MM-DD.log；读取时尽量筛选故障时间并脱敏
# 受控重启：仅在用户授权恢复服务或维护范围内执行
ssh nas /data/500g_0/resources/tg/bin/saveany-bot-control.sh restart
# 核对 2026-09-20—21 事故对应源码；新事故换成实时确认的部署 tag/commit
git show v0.61.0:client/bot/bot.go
git diff v0.61.0 -- client/bot cmd/run.go client/middleware core/tasks/batchtfile storage/local
```

不要单凭 health/tasks 接口决定重启；结合最新业务日志、打开的缓存/目标文件增长和进程写入计数。用户已明确要求立即重启时，不重复索要确认。默认复用现有控制脚本，不调用可能更新版本或重建旧守护的历史部署脚本。

已授权部署时，先核对目标架构、磁盘空间、活动任务和实际服务配置；本地构建对应产物，保留版本/提交信息、校验值及旧二进制回滚路径。仅替换本次授权产物，不覆盖配置、session、数据库或下载文件。涉及迁移时单独说明数据兼容与回滚限制，不能假定换回旧二进制就能撤销迁移。恢复验证区分进程启动、客户端就绪与实际文件保存成功；真实任务测试仍按消息发送授权执行。

日志分享只保留必要时间、错误链、源码位置和匿名任务关联，不提交 bot token、API token、订阅 URL、session、数据库、私钥或原始资源文件名。只读排查默认不做写盘压测、不启动长期采集服务。
