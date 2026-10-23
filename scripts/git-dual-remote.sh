#!/bin/sh
# =============================================================================
# 双远端（gitee + gitcode）配置脚本 —— 一键为当前仓库配置：
#   1) origin 带两个 pushurl，普通 `git push` 自动推送到 gitee 与 gitcode；
#   2) 新增 gitcode 远端（可单独 fetch/pull）；
#   3) 安装 pre-push hook：推送前自动 fetch --all --prune 并 fast-forward 到最新；
#   4) 配置 git pull / git sync 别名，自动拉取两个远端的最新提交。
#
# 幂等：可重复执行。
# 用法：sh scripts/git-dual-remote.sh
# =============================================================================
set -e

GITEE_URL="git@gitee.com:liushimeng109117198_admin/lsm-agent-game.git"
GITCODE_URL="git@gitcode.com:liusm109117198/LsmAgentGame.git"

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

echo "==> 配置远端"
if git remote get-url gitcode >/dev/null 2>&1; then
  git remote set-url gitcode "$GITCODE_URL"
else
  git remote add gitcode "$GITCODE_URL"
fi

git remote set-url origin "$GITEE_URL"

# origin 同时带两个 pushurl ⇒ `git push` 一次推送到两个平台
# 先清空既有 pushurl，保证脚本幂等（重复执行不产生重复条目）
while git remote get-url --push --all origin >/dev/null 2>&1; do
  git remote set-url --delete --push origin "$(git remote get-url --push origin)" 2>/dev/null || break
done
git remote set-url --add --push origin "$GITEE_URL"
git remote set-url --add --push origin "$GITCODE_URL"

echo "==> 配置抓取行为（prune / ff-only）"
git config fetch.prune true
git config fetch.pruneTags true
git config pull.ff only
git config push.default current

echo "==> 配置别名"
git config --local --replace-all alias.sync \
  '!git fetch --all --prune && git merge --ff-only origin/main && git merge --ff-only gitcode/main'
git config --local --replace-all alias.pushall '!git push origin && git push gitcode'
git config --local --replace-all alias.pull \
  '!f() { git fetch --all --prune && { if [ $# -gt 0 ]; then command git pull "$@"; else git merge --ff-only origin/main && git merge --ff-only gitcode/main; fi; }; }; f'

echo "==> 安装 pre-push hook（推送前自动同步到最新）"
hooks_dir=$(git rev-parse --git-path hooks)
mkdir -p "$hooks_dir"
cat > "$hooks_dir/pre-push" <<'HOOK'
#!/bin/sh
# 自动拉取到最新（推送前同步 origin=gititee 与 gitcode 两个远端）
set -e
branch=$(git symbolic-ref --short HEAD 2>/dev/null || git rev-parse --short HEAD)
echo "[pre-push] 同步远端 (origin/gitee + gitcode)..." >&2
git fetch --all --prune --quiet || {
  echo "[pre-push] 警告：fetch 失败，跳过自动同步，继续推送" >&2
  exit 0
}
for remote in origin gitcode; do
  ref="refs/remotes/$remote/$branch"
  if git rev-parse --verify --quiet "$ref" >/dev/null 2>&1; then
    if ! git merge-base --is-ancestor "$ref" HEAD 2>/dev/null; then
      if [ -n "$(git status --porcelain)" ]; then
        echo "[pre-push] 错误：工作区有未提交改动，无法自动同步 $remote/$branch，请先提交或 stash" >&2
        exit 1
      fi
      echo "[pre-push] 快进合并 $remote/$branch ..." >&2
      git merge --ff-only "$ref" || {
        echo "[pre-push] 错误：无法快进合并 $remote/$branch（可能双方已分叉），请手动处理" >&2
        exit 1
      }
    fi
  fi
done
echo "[pre-push] 同步完成，继续推送" >&2
exit 0
HOOK
chmod +x "$hooks_dir/pre-push"

echo "==> 完成。当前远端："
git remote -v
echo
echo "常用命令："
echo "  git push        # 自动推送到 gitee + gitcode"
echo "  git pull        # 自动拉取两个远端最新（等价 git sync）"
echo "  git sync        # fetch --all + 快进合并两个远端 main"
