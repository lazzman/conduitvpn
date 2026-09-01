---
name: conduitvpn-rebase-upstream
description: 安全地将当前 ConduitVPN 分支的本地提交 rebase 到作者仓库 sarices/conduitvpn 的 upstream/main，保护并恢复未提交改动，验证重放结果，并在用户明确要求发布时仅以 --force-with-lease 推送到个人 origin。用于“同步上游”“rebase upstream”“同步 fork”“将本地改动接到最新版 main”“强推到 origin”或“拉取 sarices/conduitvpn 更新”。
---

# ConduitVPN 上游安全 Rebase

将个人仓库的本地提交安全重放到作者上游。默认仅同步和验证；仅在用户明确要求发布时才安全强推。

| 角色 | 远程 | 仓库 | 用途 |
| --- | --- | --- | --- |
| 作者仓库 | `upstream` | `https://github.com/sarices/conduitvpn` | 拉取与 rebase 基线 |
| 个人仓库 | `origin` | `https://github.com/lazzman/conduitvpn.git` | 发布目标 |

默认基线为 `upstream/main`。脚本仅允许以 `upstream/*` 作为 rebase 目标，且会将当前分支的默认拉取配置为 `upstream/main`、默认推送配置为 `origin`；同时显式禁用 `upstream` 的推送地址。

## 不可违反的规则

- 必须先抓取 `upstream`，禁止基于过期的远程跟踪引用 rebase。
- 禁止使用 `git reset --hard`、`git clean -fd`、`git rebase --skip`、普通 `--force`，以及删除本地提交或覆盖未提交工作。
- 工作区不干净时，必须使用 `git stash push --include-untracked`；成功后必须恢复 stash。
- rebase 冲突中，`--ours` 是 `upstream` 基线，`--theirs` 是正在重放的本地提交。先阅读三方版本和 `REBASE_HEAD`，再合并双方有效改动。
- 不得向 `upstream` 推送。发布到 `origin` 时只能使用带租约的强制推送。

## 本地高价值改动

冲突时优先保留并合入这些本地能力，同时并入上游修复：

- Trellis 开发规范（`.trellis/spec/`、任务与工作日志）
- 上游安全同步技能与定时检查脚本
- `AGENTS.md` / `CLAUDE.md` 中的 Trellis 说明

## 执行

在仓库根目录运行：

```bash
bash .agents/skills/conduitvpn-rebase-upstream/scripts/rebase_upstream.sh
# 或显式指定作者分支：
bash .agents/skills/conduitvpn-rebase-upstream/scripts/rebase_upstream.sh upstream/main
```

脚本会校验仓库身份和双远程、抓取 `upstream` 与 `origin`、保护脏工作区、执行 rebase 并输出：

```text
BRANCH=...
UPSTREAM=...
ORIGINAL_HEAD=...
STASH_REF=...
RESULT=SUCCESS|CONFLICT|STASH_CONFLICT|FAILED
REBASED_HEAD=...
```

| 结果 | 退出码 | 后续动作 |
| --- | ---: | --- |
| `SUCCESS` | 0 | 执行验证；仅在用户要求时推送。 |
| `CONFLICT` | 20 | 保留 rebase 现场，进入冲突处理。 |
| `STASH_CONFLICT` | 21 | rebase 已完成；解决恢复未提交工作时的冲突。 |
| `FAILED` | 1 | 阅读 `ERROR`，保留现场并报告阻塞原因。 |

## 冲突处理

重复以下操作，直到 rebase 结束：

```bash
git status --short
git diff --name-only --diff-filter=U
git diff --check

# 对每个冲突文件 path：
git show :1:path  # 共同祖先
git show :2:path  # upstream 基线（ours）
git show :3:path  # 本地正在重放的提交（theirs）
git show --stat --oneline REBASE_HEAD
git show REBASE_HEAD -- path
```

合并两边不冲突的有效逻辑，删除全部冲突标记后继续：

```bash
git diff --check
git add path
GIT_EDITOR=true git rebase --continue
```

如果仍有 `STASH_REF`，执行：

```bash
git stash pop "$STASH_REF"
```

stash 恢复冲突时，沿用相同的三方分析流程。保留恢复后的未提交修改，不要为了获得干净工作区而强制提交。

## 验证与发布

恢复工作区后执行：

```bash
git status --short
git diff --check
git log --oneline "$UPSTREAM"..HEAD
git range-diff "$UPSTREAM"..."$ORIGINAL_HEAD" "$UPSTREAM"...HEAD
git fsck --no-reflogs --no-progress
```

按改动范围运行最小相关检查，例如：

```bash
# Go 代码有实质变更时：
go test ./...

# 仅文档/技能/规范变更时：
git diff --check
```

不要无故跑 Docker 镜像全量 build。

用户明确要求发布后，确认验证通过且 `origin` 指向个人仓库，再执行：

```bash
git push --force-with-lease origin HEAD:"$(git branch --show-current)"
```

最终报告当前分支、上游、`ORIGINAL_HEAD` 与 `REBASED_HEAD`、冲突文件及合并取舍、验证结果和推送结果。
