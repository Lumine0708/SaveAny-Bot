# ubuntu209 测试环境与真实联调

本项目独立测试环境的复用约定。需要测试环境时先读本文件，不要求用户重复提供已记录的主机、Bot 和目录背景。本机测试命令见 [AGENTS_DEV.md](AGENTS_DEV.md)，正式 NAS 见 [AGENTS_DEPLOY.md](AGENTS_DEPLOY.md)。

## 当前状态与边界

截至 **2026-09-29 15:25（Asia/Shanghai）**，ubuntu209 上本项目测试服务及专用目录已按用户要求全部清理：`saveany-bot-test.service` 已停止、禁用、删除；`/home/lumine/Projects/Services/saveanybot` 已删除，包括配置、session、数据库、下载文件、备份、源码、工具链和缓存。**下表是重建时沿用的约定，不代表文件或服务仍存在。**

- 不因读取本文、提交代码或合并分支而重新部署。后续用户要求真实联调/测试部署时，按当次及已有授权推进；已获授权的例行步骤不重复确认。
- ubuntu209 是测试主机，`nas` 是正式环境。不得把测试部署指向 NAS，也不得复制正式 session、数据库或用户资源来充当隔离环境。
- `/home/lumine/Projects/Services/saveany` 是另一个独立的 Python/Web 项目，不是本 Bot 的旧目录；测试清理不得删除它。
- 主机 `mihomo.service` 保留，清理时仍 active；不是 Bot 专用文件。不得随 Bot 清理卸载或停用它，不清空主机共享 systemd journal。
- Codex 自动化 `ubuntu209-bot` 已暂停，不因重建环境或读取旧交接记录自行恢复。

## 连接、Bot 与目录约定

| 项目 | 约定或最后验证值 |
| --- | --- |
| SSH | 使用别名 `ubuntu209`；地址、端口和密钥以本机 `~/.ssh/config` 为准，不硬编码 |
| 远端用户 / 构建目标 | `lumine`；上次验证为 Linux amd64，重建前核对 `uname -m` |
| 测试 Bot | `@test_saveanybot_bot`，Telegram 聊天显示名 `test-saveanybot` |
| 专用根目录 | `/home/lumine/Projects/Services/saveanybot`，与无 `bot` 后缀的项目严格区分 |
| 源码 / 工具链 / 构建缓存 | 根目录下 `src/`、`tools/`、`cache/` |
| 运行目录 | 根目录下 `run/`；二进制 `run/saveany-bot`，私有配置 `run/config.toml`（0600） |
| 运行数据 | `run/data/` 放测试数据库/session；`run/downloads/` 放目标文件；`run/cache/` 放任务临时文件；`run/backups/` 放本次测试所需备份 |
| systemd | 用户级 `saveany-bot-test.service`；unit 在 `~/.config/systemd/user/`，使用 `systemctl --user` |
| 日志 | `journalctl --user -u saveany-bot-test.service`；按时间窗口读取，先脱敏再输出 |
| 本地存储 | 显示名 `ubuntu209测试盘`，根目录 `run/downloads/`；“保存/上传”在该配置下是复制到测试机本地 |
| 上次联调参数 | `workers=4`、`threads=6`、`stream=false`；用户默认存储为上述测试盘、根目录，`silent=1`（新资源直接保存） |
| HTTP API | 上次未开启；不要仅为查询任务状态额外开启 |

上次使用 Go 1.25.10，位置为 `tools/go1.25.10/bin/go`；`GOPATH` 为根目录下 `cache/gopath`，`GOCACHE` 为 `cache/go-build`。这些均已删除，重建时先检查主机现有工具，不假定旧路径可执行。沿用项目 Go 1.25+ 和锁定的 gotd/td v0.149.0。

Bot token、Telegram API 凭据和代理密钥只从当次授权的私有来源获取，不写入本文、命令输出、提交或报告。旧测试配置已删除，不把“记得 Bot 用户名”当成凭据仍可用；只有确实缺少重建必需的信息时才向用户补问。

## 代理背景

2026-09-29 的测试主机使用 Mihomo v1.19.28、MetaCubeXD v1.268.4，面板运行时为 Node v22.23.0，启用 TUN；mixed port 7890、controller 9090、面板 18090。Clash Verge 已按用户要求卸载，不恢复。以上为历史值，联调前核对当前服务和必要的连通性，不为 Bot 测试擅自改节点、端口或主机网络设置。HTTP 网页可达不能替代 Telegram 文件传输验证。

## 真实联调约定

1. 先核对运行版本、进程、磁盘和在途任务。首次重建使用独立测试配置、数据库和全新 session；隔离字段见开发分片。A/B 只改变待验证因素，记录代码提交、二进制 SHA256 和时间；替换版本前检查 `/task` 与 `/task queued`，避免中断未完成任务。
2. 用户授权 Computer Use 时，只操控其当前打开的 `test-saveanybot` 聊天窗口；每次发送前核对目标，不切换其他聊天或正式 Bot。测试使用专门生成的无敏感内容样本，避免重复使用用户原有资源。更新测试部署或重新绑定 Bot token 按当次授权范围执行。
3. Computer Use 依赖桌面可捕获。锁屏或合盖造成 macOS 捕捉失败时，记录阻塞，不无限重试，也不把未发出的消息算作复现；用户要求停止时停止 UI 联调，不要求其长期开屏。
4. 完成标准是 **稳定真实复现 → 局部修复 → 同类场景重跑得到预期结果**。记录 Telegram 可见状态、脱敏服务日志和实际保存文件；需要证明文件完整时核对大小及 SHA256。离线 fake sender、编译成功、任务后台成功均不能替代 UI 终态验证。
5. 报告明确复现次数、成功/失败、观察窗口及未验证路径。按钮消失、取消或批量行为没有直接覆盖，就不扩展结论。日志仅保留必要时间、错误链和匿名任务关联，不输出原始资源名、链接、配置、session 或数据库内容。
6. 结束时记录环境是保留还是清理。清理已获授权时，停用并移除本测试 unit，再删除已核对的专用目录，检查残留进程和启动链接；本机保留代码及脱敏验收证据，不误删邻近项目或共享代理。

## 历史验收入口

[Issue #248 交接](docs/issue-248-handoff.md) 记录异步消息编辑及 reply markup 修复。旧版普通小文件 3/3 终态编辑报错，修复版 6/6 显示完成；后者包括三个小文件、两个 64 MiB 和一个 256 MiB 文件。取消与批量专用入口未单独实跑，`connection dead` 仍是另一待定位问题。

本机 `reviews/issue-248-reply/` 下保留脱敏 journal、样本校验和验收报告；它未纳入 Git，其他机器缺失时不得声称已读取。以后部署版本和环境状态以更晚的实际核验为准。
