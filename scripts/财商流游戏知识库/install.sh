#!/usr/bin/env bash
# 阶段 E：把新目录树安装到 docs/财商流游戏/玩家职业设计/，并归档旧文件
# 用法: bash install.sh <构建产物目录> <目标目录> <本脚本目录>
set -euo pipefail

BUILD="${1:?构建产物目录}"
DEST="${2:?目标目录}"
HERE="${3:?脚本目录}"

echo "── 1. 建立 _框架 / _交付说明 ──"
mkdir -p "$DEST/_框架" "$DEST/_交付说明" "$DEST/_框架/07-历史归档"

echo "── 2. 框架文档搬迁 ──"
mv "$DEST/1510-研究来源与数据锚点.md" "$DEST/_框架/04-数据来源与锚点.md"

# 04/05/06/07 合并为《05-画像与阶层框架.md》，内容整段保留
{
  echo "# 财商流游戏 · 画像与阶层框架"
  echo
  echo "> 本文件由 v4.0 重构合并原 `04-配偶关系与家庭经济动态.md`、`05-行为金融决策画像与AI性格.md`、"
  echo "> `06-家庭阶层目标与画像事件.md`、`07-教育平衡性与差异化边界.md` 四份文档而成，"
  echo "> **正文逐字保留**，仅增加分节标题。原始四份文件已归档至 [_框架/07-历史归档/](07-历史归档/)。"
  echo
  for n in 04 05 06 07; do
    f=$(ls "$DEST/$n-"*.md 2>/dev/null | head -1)
    [ -z "$f" ] && continue
    echo "---"; echo
    sed 's/^# /## /; s/^## /### /; s/^### /#### /' "$f"
    echo
  done
} > "$DEST/_框架/05-画像与阶层框架.md"

for n in 04 05 06 07; do
  f=$(ls "$DEST/$n-"*.md 2>/dev/null | head -1)
  [ -n "$f" ] && mv "$f" "$DEST/_框架/07-历史归档/"
done

# 旧索引（描述旧扁平结构）归档，由 gen_docs 生成的新索引替代
mv "$DEST/00-职业画像框架与索引.md" "$DEST/_框架/07-历史归档/00-职业画像框架与索引-v3.4.md"

for f in "$DEST"/交付说明_*.md; do
  [ -e "$f" ] && mv "$f" "$DEST/_交付说明/"
done

echo "── 3. 规范文档落位 ──"
cp "$HERE/_tpl_01_字段字典.md" "$DEST/_框架/01-人物卡字段字典.md"
cp "$HERE/_tpl_02_目录规约.md" "$DEST/_框架/02-目录结构与分片规约.md"
cp "$HERE/_tpl_08_富化规约.md" "$DEST/_框架/08-人物卡富化规约.md"

echo "── 4. 安装 6.5 万张人物卡 ──"
cp -r "$BUILD"/* "$DEST"/

echo "── 5. 删除旧扁平档案（内容已全量转换，git 历史保留） ──"
find "$DEST" -maxdepth 1 -type f -name '*.md' -print -delete | wc -l

echo "── 6. 结果 ──"
echo "人物卡: $(find "$DEST" -name '*.md' -path '*/CN-*' | wc -l)"
echo "目录数: $(find "$DEST" -type d | wc -l)"
echo "顶层:"
ls "$DEST"
