#!/usr/bin/env bash
# ConduitVPN 专属：校验双远程，抓取作者上游，并安全地重放当前分支提交。
set -euo pipefail

readonly EXPECTED_ORIGIN_URL="https://github.com/lazzman/conduitvpn.git"
readonly EXPECTED_UPSTREAM_URL="https://github.com/sarices/conduitvpn"
readonly DEFAULT_UPSTREAM_REF="upstream/main"

usage() {
  cat <<'USAGE'
用法: rebase_upstream.sh [upstream/<branch>]

默认将当前 ConduitVPN 分支安全 rebase 到 upstream/main。
脚本会校验 origin=lazzman/conduitvpn、upstream=sarices/conduitvpn，保护未提交
与未跟踪改动，在抓取远程后启动 rebase；脚本绝不会推送。

冲突时保留现场：
  RESULT=CONFLICT       退出码 20
  RESULT=STASH_CONFLICT 退出码 21
USAGE
}

result() {
  printf 'RESULT=%s\n' "$1"
}

fail() {
  printf 'ERROR=%s\n' "$1" >&2
  result "FAILED"
  exit 1
}

in_git_state() {
  test -e "$(git rev-parse --git-path "$1")"
}

normalize_github_url() {
  local raw="$1" normalized
  normalized="${raw%.git}"
  normalized="${normalized%/}"

  if [[ "$normalized" =~ ^git@github\.com:(.+)$ ]]; then
    printf 'https://github.com/%s.git\n' "${BASH_REMATCH[1]}"
    return
  fi
  if [[ "$normalized" =~ ^ssh://git@github\.com/(.+)$ ]]; then
    printf 'https://github.com/%s.git\n' "${BASH_REMATCH[1]}"
    return
  fi
  if [[ "$normalized" =~ ^https://github\.com/(.+)$ ]]; then
    printf 'https://github.com/%s.git\n' "${BASH_REMATCH[1]}"
    return
  fi

  printf '%s\n' "$raw"
}

require_remote_url() {
  local remote="$1" expected="$2" actual expected_normalized
  git remote get-url "$remote" >/dev/null 2>&1 || fail "缺少远程仓库: $remote"

  actual="$(normalize_github_url "$(git remote get-url "$remote")")"
  expected_normalized="$(normalize_github_url "$expected")"
  [[ "$actual" == "$expected_normalized" ]] \
    || fail "远程 $remote 地址不符合项目约定：实际为 $actual，期望为 $expected_normalized"

  printf 'REMOTE_OK=%s url=%s\n' "$remote" "$actual"
}

configure_routes() {
  # main 的拉取基线固定为作者上游，当前分支的发布目标固定为个人 origin。
  if [[ "$branch" == "main" ]]; then
    git config branch.main.remote upstream
    git config branch.main.merge refs/heads/main
  fi
  git config "branch.${branch}.pushRemote" origin

  # 显式禁用作者仓库推送，避免操作时误将本地改动发布到上游。
  git config --unset-all remote.upstream.pushurl >/dev/null 2>&1 || true
  git config --add remote.upstream.pushurl DISABLED
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -gt 1 ]]; then
  usage >&2
  exit 64
fi

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || fail "当前目录不是 Git 工作树"
repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

# 通过 Go 模块、入口目录和构建文件阻止在其他仓库误执行。
if [[ ! -f go.mod || ! -f Makefile || ! -f Dockerfile || ! -d cmd/conduitvpn || ! -d internal/webui ]] \
  || ! grep -qx 'module conduitvpn' go.mod; then
  fail "当前仓库不像 ConduitVPN（缺少核心项目文件或模块标识）"
fi

branch="$(git branch --show-current)"
[[ -n "$branch" ]] || fail "当前处于 detached HEAD，无法安全重放本地分支提交"

if in_git_state rebase-merge || in_git_state rebase-apply || in_git_state MERGE_HEAD || in_git_state CHERRY_PICK_HEAD; then
  fail "仓库已有未完成的 rebase、merge 或 cherry-pick；请先解决现有操作"
fi

upstream_ref="${1:-$DEFAULT_UPSTREAM_REF}"
[[ "$upstream_ref" == upstream/* ]] \
  || fail "本项目仅允许以 upstream/<branch> 作为 rebase 基线"

require_remote_url origin "$EXPECTED_ORIGIN_URL"
require_remote_url upstream "$EXPECTED_UPSTREAM_URL"
configure_routes

# 先刷新作者基线，再刷新发布端引用供后续 --force-with-lease 校验使用。
printf 'FETCH_REMOTE=upstream\n'
git fetch --prune upstream
printf 'FETCH_REMOTE=origin\n'
git fetch --prune origin

git rev-parse --verify --quiet "${upstream_ref}^{commit}" >/dev/null \
  || fail "无法解析目标上游: $upstream_ref（请确认已抓取）"

original_head="$(git rev-parse HEAD)"
stash_ref=""
stash_commit=""
if [[ -n "$(git status --porcelain)" ]]; then
  stash_label="conduitvpn-rebase-upstream-${branch}-$(date +%Y%m%d%H%M%S)"
  git stash push --include-untracked --message "$stash_label" >/dev/null \
    || fail "无法保护未提交工作区"
  stash_ref="stash@{0}"
  stash_commit="$(git rev-parse --verify refs/stash)"
fi

git config rerere.enabled true

printf 'BRANCH=%s\n' "$branch"
printf 'UPSTREAM=%s\n' "$upstream_ref"
printf 'PUSH_REMOTE=origin\n'
printf 'ORIGIN_URL=%s\n' "$(git remote get-url origin)"
printf 'UPSTREAM_URL=%s\n' "$(git remote get-url upstream)"
printf 'ORIGINAL_HEAD=%s\n' "$original_head"
printf 'STASH_REF=%s\n' "${stash_ref:-none}"
printf 'STASH_COMMIT=%s\n' "${stash_commit:-none}"
printf 'PUSH_HINT=git push --force-with-lease origin HEAD:%s\n' "$branch"

if ! git rebase "$upstream_ref"; then
  if in_git_state rebase-merge || in_git_state rebase-apply; then
    printf 'CONFLICT_FILES=\n'
    git diff --name-only --diff-filter=U
    result "CONFLICT"
    exit 20
  fi
  fail "git rebase 执行失败，但未进入可解决的冲突状态"
fi

if [[ -n "$stash_ref" ]]; then
  if ! git stash pop "$stash_ref"; then
    printf 'CONFLICT_FILES=\n'
    git diff --name-only --diff-filter=U
    result "STASH_CONFLICT"
    exit 21
  fi
fi

printf 'REBASED_HEAD=%s\n' "$(git rev-parse HEAD)"
printf 'COMMITS_AHEAD=\n'
git log --oneline "${upstream_ref}..HEAD" || true
result "SUCCESS"
