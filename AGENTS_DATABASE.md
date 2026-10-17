# SQLite、用户与会话数据

修改数据库模型、查询、用户同步、迁移或数据修复前，先读本文件；本机运行隔离见 [AGENTS_DEV.md](AGENTS_DEV.md)，正式数据操作见 [AGENTS_DEPLOY.md](AGENTS_DEPLOY.md)。

## 实现入口

- `database/db.go`：GORM 初始化、AutoMigrate、`syncUsers`。
- `database/model.go`、`database/{user,dir,rule,chat}.go`：User、Dir、Rule、WatchChat 及相关操作。
- `database/driver.go`：默认 `ncruces/go-sqlite3/gormlite`；`database/driver_glebarez.go`：`sqlite_glebarez` 构建标签对应驱动。
- `config/db.go`、`config/tg.go`、`config/viper.go`：业务库、Bot session、userbot session 配置。

## 数据约束

- 数据库变更沿用 GORM、现有模型和 `GetDialect`；有 ctx 的操作沿用 `WithContext(ctx)`。涉及持久化结构时核对已有数据与升级影响，并覆盖默认和 `sqlite_glebarez` 驱动受影响路径；不为本地跑通引入新迁移框架或清空数据库。
- `db.path` 是业务数据库，`db.session` 是 Bot session，`telegram.userbot.session` 是 userbot session；不要混用或从正式环境复制到测试实例。
- `database.Init` 会 AutoMigrate 并执行 `syncUsers`：按 `config.C().Users` 创建用户、删除不在配置中的用户。不要拿正式数据库验证临时用户配置，也不要用手工增删用户绕过配置同步。
- 迁移失败日志中的“删除数据库重试”不是删除数据的授权。结构变更需说明已有数据影响；正式迁移的备份、兼容与回滚按部署分片执行。
- 测试使用临时数据库及 `t.TempDir()`；真实 SQLite 文件、WAL/SHM、session 均不提交。
