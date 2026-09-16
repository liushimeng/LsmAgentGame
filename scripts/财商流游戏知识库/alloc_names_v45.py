#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""alloc_names_v45.py —— v4.5 多维度批次「全局唯一化名」预分配器

背景（v4.5，2026-09-16）：
  知识库扩容改为「多 SubAgent 并行生成」，若各 Agent 自行随机取名，跨 Agent 之间
  必然产生化名碰撞（v4.4 实测已存在 355 个重名 / 725 张卡）。本脚本一次性扫描
  全库既有化名，再按 block 预生成互不重叠的化名清单落盘，各生成 Agent 只能消费
  自己 block 内的名字 → 从源头消除碰撞。

用法：
    python3 scripts/财商流游戏知识库/alloc_names_v45.py            # 按内置 BLOCKS 生成
    python3 scripts/财商流游戏知识库/alloc_names_v45.py --dry      # 只统计不写盘

输出：
    scripts/财商流游戏知识库/work/v45/names_<block>.txt   每行一个化名
    scripts/财商流游戏知识库/work/v45/alloc_report.json   分配报告（既有名数/各 block 名数/校验）
"""
import argparse
import json
import os
import random
import re
import sys

THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)

from gen_batch import SURNAMES_M, SURNAMES_F, GIVEN_M, GIVEN_F  # noqa: E402

REPO_ROOT = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
CARD_ROOT = os.path.join(REPO_ROOT, 'docs', '财商流游戏', '玩家职业设计')
OUT_DIR = os.path.join(THIS_DIR, 'work', 'v45')

# block 名 → 需要预分配的化名数量（含 25% 冗余，供生成器重名重试/富化补卡）
BLOCKS = {
    'dim2': 2500,   # 维度②身份与就业状态人群
    'dim3': 2000,   # 维度③财富阶层与资产负债人群
    'dim4': 2000,   # 维度④家庭生活与消费财商人群
    'dim5': 2000,   # 维度⑤财商观念与行为偏差人群
    'dim6': 1800,   # 维度⑥联网调研驱动的新兴/真实人物人群
    'dim7': 1800,   # 维度⑦行业维度扩容（新职业族补强）
    'repair': 1200, # 既有 355 重名 / 725 卡改名修复用
}

SEED = 20260916
NAME_RE = re.compile(r'^[NP]\d*-(.+)\.md$')


def scan_existing_names(root):
    """扫描全库既有化名（跳过 `_` 前缀目录：框架/交付说明非卡池）。"""
    names = set()
    n_cards = 0
    for dirpath, dirnames, filenames in os.walk(root):
        if os.path.relpath(dirpath, root).startswith('_'):
            dirnames[:] = []
            continue
        for fn in filenames:
            if not fn.endswith('.md'):
                continue
            m = NAME_RE.match(fn)
            if not m:
                continue
            n_cards += 1
            names.add(m.group(1))
            # 卡内 frontmatter 的 name 字段一并纳入（历史 legacy_name 亦防撞）
    return names, n_cards


def scan_frontmatter_names(root, limit=None):
    """补扫 frontmatter `name:` / `_raw.legacy_name`，进一步降低撞名概率。"""
    extra = set()
    pat_name = re.compile(r'^name:\s*(\S+)\s*$', re.M)
    pat_legacy = re.compile(r'^\s*legacy_name:\s*(\S+)\s*$', re.M)
    n = 0
    for dirpath, dirnames, filenames in os.walk(root):
        if os.path.relpath(dirpath, root).startswith('_'):
            dirnames[:] = []
            continue
        for fn in filenames:
            if not fn.endswith('.md'):
                continue
            n += 1
            if limit and n > limit:
                return extra
            try:
                with open(os.path.join(dirpath, fn), encoding='utf-8',
                          errors='replace') as f:
                    head = f.read(6000)
            except OSError:
                continue
            for pat in (pat_name, pat_legacy):
                for m in pat.finditer(head):
                    v = m.group(1).strip().strip('\'"')
                    if v:
                        extra.add(v)
    return extra


def gen_block(rng, used, count):
    """生成 count 个未使用化名（2–3 字，姓氏库 × 名字库，男女各半）。

    返回 [(name, gender), ...] —— **必须带性别**：v4.5 维度卡按人群性别结构取名
    （如「全职照料者」女性占比高、「矿井支护工」男性占比高），若名字与卡片性别
    错配（女性卡配「某兵志」）会直接破坏档案真实感。
    """
    out = []
    guard = 0
    while len(out) < count and guard < count * 200:
        guard += 1
        gender = '女' if rng.random() < 0.5 else '男'
        s = rng.choice(SURNAMES_F if gender == '女' else SURNAMES_M)
        g = ''.join(rng.choices(GIVEN_F if gender == '女' else GIVEN_M,
                                k=rng.choice([1, 1, 2, 2])))
        name = s + g
        if name in used:
            continue
        used.add(name)
        out.append((name, gender))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--dry', action='store_true', help='只统计不写盘')
    ap.add_argument('--card-root', default=CARD_ROOT)
    ap.add_argument('--out-dir', default=OUT_DIR)
    ap.add_argument('--seed', type=int, default=SEED)
    args = ap.parse_args()

    fn_names, n_cards = scan_existing_names(args.card_root)
    fm_names = scan_frontmatter_names(args.card_root)
    used = set(fn_names) | set(fm_names)
    print('[alloc] 卡文件数=%d 文件名化名=%d frontmatter 追加化名=%d 合计去重=%d'
          % (n_cards, len(fn_names), len(fm_names), len(used)))

    rng = random.Random(args.seed)
    report = {'seed': args.seed, 'existing_cards': n_cards,
              'existing_names': len(used), 'blocks': {}}
    all_new = []
    for block, count in BLOCKS.items():
        names = gen_block(rng, used, count)
        n_male = sum(1 for _, g in names if g == '男')
        report['blocks'][block] = {'requested': count, 'allocated': len(names),
                                   'male': n_male, 'female': len(names) - n_male}
        all_new.append((block, names))
        if len(names) < count:
            print('[alloc][WARN] block %s 只生成 %d/%d（词库容量接近上限）'
                  % (block, len(names), count))

    # 交叉校验：block 内 + block 间 + 与既有库 三重零碰撞
    flat = [n for _, names in all_new for n, _g in names]
    assert len(flat) == len(set(flat)), '新名内部存在碰撞'
    assert not (set(flat) & set(fn_names)), '新名与既有文件名化名碰撞'

    if args.dry:
        print('[alloc] dry-run 校验通过，新名合计 %d' % len(flat))
        return

    os.makedirs(args.out_dir, exist_ok=True)
    for block, names in all_new:
        fp = os.path.join(args.out_dir, 'names_%s.txt' % block)
        with open(fp, 'w', encoding='utf-8') as f:
            # 格式：`<化名>\t<性别>`（lib_dim_v45.load_names 按此解析；兼容纯名行）
            f.write('\n'.join('%s\t%s' % (n, g) for n, g in names) + '\n')
        print('[alloc] 写出 %s（%d 名）' % (fp, len(names)))
    report['total_new'] = len(flat)
    with open(os.path.join(args.out_dir, 'alloc_report.json'), 'w',
              encoding='utf-8') as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    print('[alloc] 报告：%s' % os.path.join(args.out_dir, 'alloc_report.json'))


if __name__ == '__main__':
    main()
