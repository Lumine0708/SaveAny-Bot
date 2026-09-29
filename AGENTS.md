# SaveAny-Bot fork 协作入口

本文件只保留项目概况、核心约束和按需读取索引。全局“最小改动优先”规则继续适用；`../agent-config/docs/` 是规则模板来源，相邻项目只作参考，不自动继承其技术栈、分支、部署命令或审批流程。

## 项目概况

SaveAny-Bot 是 Go 编写的 Telegram 文件保存机器人，支持单文件、批量/相册、多用户、存储规则、跨存储传输、yt-dlp/Aria2 下载和 Goja/Playwright 解析插件。存储后端包括 Local、S3、MinIO、WebDAV、AList、Rclone、Telegram。

技术栈：Go 1.25、gotgproto、gotd/td、Cobra、Viper、GORM + SQLite、Goja、Playwright、charmbracelet/log；许可证 AGPL-3.0。

本 fork 的主要场景是长期运行的个人 NAS Bot：用户通过 Telegram 提交任务，保存到 NAS 本地。正式环境涉及真实文件和 session，线上事实以实时核对为准。

## 核心约束

- 用户只要求阅读、分析或评审时，保持只读；明确要求完善文档或修复代码时，先说明基于证据的改动范围，再在授权范围内完成，不为常规局部修改重复索要确认。
- 每项改动对应用户要求、已确认需求、可复现缺陷、日志或现有契约。先找最接近的实现；范围外发现只报告证据和影响，不顺手修复。文档任务不修改业务代码、测试、依赖、CI 或运行配置。
- 沿用既有配置、鉴权、队列与存储能力，只实现本次需求和已证实风险所必需的改动，不新增无关的公网运营能力或通用监控平台。
- `gotd/td` 保持 `v0.149.0`；v0.150+ 与当前 `gotgproto v1.0.0-beta22` 存在编译不兼容（`AsInputDocumentFileLocation`、`gotd/log` Logger 接口），不得顺手升级。
- 修改前确认分支与工作区，保护其他任务的已有改动及同级旧 `SaveAny-Bot` 克隆；具体流程见分支分片。
- 不提交真实配置、token、session、数据库、资源文件或未脱敏日志；本机联调隔离运行数据，不启动复用正式 session 的第二个实例。
- 代码修改不自动授权部署、升级、重启正式服务或发送 Telegram 测试消息；按当前及已有用户授权执行，不重复询问已获授权的操作。
- 交付说明实际改动、验证结果及未验证项。区分“编译通过”“离线回归通过”“线上任务完成”，不把其中一个写成另一个。

## 分片索引（按需读取）

下列文件不会因文件名自动全部加载。执行对应任务前主动读取；跨领域任务读取相关分片，日常任务无需遍历全部文档。

| 分片 | 何时读取 |
| --- | --- |
| [AGENTS_BRANCHING.md](AGENTS_BRANCHING.md) | 修改文件、提交、推送、发 PR、切换任务方向或清理 worktree 前 |
| [AGENTS_DEV.md](AGENTS_DEV.md) | 构建、测试、代码生成、安装工具或本机起服务前 |
| [AGENTS_BACKEND.md](AGENTS_BACKEND.md) | 修改 Go 业务、配置、HTTP API、存储接口、插件或 i18n 前 |
| [AGENTS_DATABASE.md](AGENTS_DATABASE.md) | 修改 SQLite 模型、查询、用户同步、迁移或数据修复前 |
| [AGENTS_TASKS.md](AGENTS_TASKS.md) | 修改客户端生命周期、排队、取消、下载/保存、并发或进度反馈前 |
| [AGENTS_DEPLOY.md](AGENTS_DEPLOY.md) | 构建 NAS 发布产物、检查正式服务、部署、重启、回滚或线上验证前 |

## 项目文档与证据

- [config.example.toml](config.example.toml)：配置字段说明；具体绑定与默认值以配置源码核对。
- [中文用户文档](docs/content/zh/_index.md) / [英文用户文档](docs/content/en/_index.md)：受影响内容保持双语同步。
- [插件契约](plugins/README.md)：JS 插件协议及示例约定。
- [NAS 故障证据](reviews/nas-incidents/2026-09-20_21/README.md)：历史日志、采样与推理；该目录可能只存在于本地，缺失时说明证据限制，不从文件名推断结论。

后续细节维护在对应分片；根入口只更新概要和读取路由，避免同一规则在多个文件完整复制。
