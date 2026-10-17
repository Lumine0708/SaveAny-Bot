# 分支、工作区与提交

修改文件、提交、推送、发 PR、切换任务方向或清理 worktree 前，先读本文件。项目通用约束见 [AGENTS.md](AGENTS.md)。

## 仓库与基线

- origin：`Lumine0708/SaveAny-Bot`；upstream：`krau/SaveAny-Bot`。
- 本仓库位于 `/Volumes/Lumine_2T/Projects/SaveAny-Bot-fork`。同级旧 `SaveAny-Bot` 有未提交代码和历史运维文档，禁止覆盖、重置或把旧改动当作线上实现。
- 修改前检查 `git branch --show-current`、`git status --short`、`git diff`，确认任务基线；必要时查看 `git worktree list`。当前检出分支不等于线上版本，旧克隆也不等于本仓库。
- 复用适合当前任务的分支/工作区；有改动冲突、并行任务或需从不同版本修复时再隔离。新分支默认用 `codex/<topic>`；生产修复以实时确认的部署提交为基线，不默认把最新上游一起带上线。

## 改动、提交与清理

- 保留其他任务的已暂存、未暂存和未跟踪文件；不为清理工作区执行 `reset --hard`、`clean` 或覆盖性 checkout。提交时逐项选择本次路径，不用 `git add .` 吸收既有证据和本机数据。
- commit/push/PR 按用户本次或已有授权执行；参考 `git log --oneline -n 20` 的 `fix:`、`feat:`、`docs:` 等风格。推送/PR 前核对目标 remote 和 base，区分 fork 的 `origin` 与官方 `upstream`，不顺手同步或强推。
- 推送完成或 PR 合并不代表工作区可删除。清理前确认无任务依赖、未提交/未推送工作已保存；托管 worktree 使用工具归档，不直接删除目录。
- 发布触发器和推送前验证见 [AGENTS_DEV.md](AGENTS_DEV.md)；NAS 发布授权与回滚见 [AGENTS_DEPLOY.md](AGENTS_DEPLOY.md)。
