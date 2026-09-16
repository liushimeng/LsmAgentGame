#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""repair_dup_names_v45.py —— v4.5 人物卡化名去重修复

背景（v4.5，2026-09-16）：
  既有 75k 人物卡中 355 个化名被多张卡片共用（共 725 张卡），化名全局唯一性破裂。
  `alloc_names_v45.py` 已预生成 1200 唯一化名（`work/v45/names_repair.txt`），
  与既有 75k 零碰撞。本脚本扫描全库找出重名组，按 card_id 升序保留最小 ID 卡不变，
  其余卡片依次从 repair 块按性别取名，完成：
    1. 文件重命名：N<id>-<old>.md → N<id>-<new>.md
    2. frontmatter name: <old> → name: <new>
    3. _raw 下追加 legacy_name: <old>（追溯旧名）

用法：
    python3 scripts/财商流游戏知识库/repair_dup_names_v45.py            # 执行修复
    python3 scripts/财商流游戏知识库/repair_dup_names_v45.py --dry      # 只扫描报告
"""
import argparse
import collections
import os
import re
import sys

THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)

from lib_dim_v45 import CARD_ROOT  # noqa: E402

NAMES_FILE = os.path.join(THIS_DIR, 'work', 'v45', 'names_repair.txt')
NAME_RE = re.compile(r'^([NP]\d+)-(.+)\.md$')
GENDER_RE = re.compile(r'^gender:\s*(\S+)\s*$', re.M)
FM_NAME_RE = re.compile(r'^name:\s*(\S+)\s*$', re.M)
RAW_RE = re.compile(r'^_raw:\s*$', re.M)


def scan_groups(root):
    """按文件名化名段分组，返回 {化名: [(card_id, filepath), ...]}。"""
    groups = collections.defaultdict(list)
    for dirpath, dirnames, filenames in os.walk(root):
        if os.path.relpath(dirpath, root).startswith('_'):
            dirnames[:] = []
            continue
        for fn in filenames:
            if not (fn.endswith('.md') and fn[0] in 'NP'):
                continue
            m = NAME_RE.match(fn)
            if not m:
                continue
            card_id = m.group(1)
            name = m.group(2)
            groups[name].append((card_id, os.path.join(dirpath, fn)))
    return groups


def read_gender(fp):
    """读取人物卡的 gender 字段（返回 '男' / '女' / None）。"""
    try:
        with open(fp, encoding='utf-8', errors='replace') as f:
            head = f.read(2000)
    except OSError:
        return None
    m = GENDER_RE.search(head)
    return m.group(1) if m else None


def read_names_repair():
    """读取 repair 化名块，按性别分成两个 deque。"""
    male_q = collections.deque()
    female_q = collections.deque()
    with open(NAMES_FILE, encoding='utf-8') as f:
        for line in f:
            line = line.rstrip('\n')
            if not line.strip():
                continue
            parts = line.split('\t')
            nm = parts[0].strip()
            gd = parts[1].strip() if len(parts) > 1 else '?'
            if gd == '男':
                male_q.append(nm)
            elif gd == '女':
                female_q.append(nm)
            else:
                male_q.append(nm)
                female_q.append(nm)
    return {'男': male_q, '女': female_q}


def pick_name(pools, gender):
    """按性别从化名池中取一个化名（优先匹配性别，失败则从另一边取）。"""
    q = pools.get(gender)
    if q:
        try:
            return q.popleft()
        except IndexError:
            pass
    other = '女' if gender == '男' else '男'
    q = pools.get(other)
    if q:
        try:
            return q.popleft()
        except IndexError:
            pass
    raise RuntimeError('化名池已耗尽，请重新跑 alloc_names_v45.py --block repair 增大')


def rename_card(fp, old_name, new_name, card_id):
    """修复单张卡：重命名文件 + 更新 name + 追加 legacy_name。"""
    dirpath = os.path.dirname(fp)
    new_fn = '%s-%s.md' % (card_id, new_name)
    new_fp = os.path.join(dirpath, new_fn)

    with open(fp, encoding='utf-8') as f:
        content = f.read()

    # 1. 更新 frontmatter name: <old> → name: <new>
    new_content, n = FM_NAME_RE.subn('name: %s' % new_name, content, count=1)
    if n == 0:
        raise ValueError('frontmatter 中未找到 name: %s' % old_name)

    # 2. 在 _raw: 行后追加 `  legacy_name: <old>`（作为 _raw 子键）
    #    检查是否已存在 legacy_name 子键（避免重复修复）
    has_legacy_sub = re.search(
        r'^_raw:\s*\n\s+legacy_name:', new_content, re.M)
    if RAW_RE.search(new_content) and not has_legacy_sub:
        replacement = '_raw:\n  legacy_name: %s' % old_name
        new_content = RAW_RE.sub(replacement, new_content, count=1)
    elif not RAW_RE.search(new_content):
        # 没有 _raw: 行 → 在 frontmatter 结束符前插入 _raw 块
        end_match = re.search(r'\n---\s*(?:\n|$)', new_content)
        if end_match:
            insert_pos = end_match.start()
            block = '\n_raw:\n  legacy_name: %s' % old_name
            new_content = (new_content[:insert_pos]
                           + block
                           + new_content[insert_pos:])

    # 写回文件
    with open(fp, 'w', encoding='utf-8') as f:
        f.write(new_content)

    # 重命名文件（如果文件名变了）
    if fp != new_fp and os.path.exists(fp):
        os.rename(fp, new_fp)

    print('[fix] %s → %s (name: %s→%s, legacy_name: %s)' % (
        os.path.basename(fp), os.path.basename(new_fp),
        old_name, new_name, old_name))
    return new_fp


def main():
    ap = argparse.ArgumentParser(description='v4.5 人物卡化名去重修复')
    ap.add_argument('--dry', action='store_true', help='只扫描报告不修改')
    ap.add_argument('--card-root', default=CARD_ROOT)
    args = ap.parse_args()

    groups = scan_groups(args.card_root)
    # 找出重名组
    dup_groups = {k: v for k, v in groups.items() if len(v) > 1}
    total_dup = sum(len(v) for v in dup_groups.values())
    to_rename_count = total_dup - len(dup_groups)
    print('[scan] 总人物卡: %d' % sum(len(v) for v in groups.values()))
    print('[scan] 重名组: %d, 涉及卡: %d, 需改名: %d' % (
        len(dup_groups), total_dup, to_rename_count))

    if args.dry:
        print('[dry] 扫描完成，不修改任何文件。')
        for i, (name, items) in enumerate(sorted(dup_groups.items())[:5]):
            items.sort(key=lambda x: x[0])
            print('[dry]  组 %d: %s (%d 张)' % (i + 1, name, len(items)))
            for card_id, fp in items:
                print('[dry]     %s → %s' % (card_id, os.path.basename(fp)))
        return

    # 读取 repair 化名池
    pools = read_names_repair()
    print('[load] repair 化名池: 男 %d, 女 %d' % (
        len(pools['男']), len(pools['女'])))

    renamed = 0
    errors = []
    for name, items in dup_groups.items():
        items.sort(key=lambda x: x[0])  # 按 card_id 升序
        keeper_id, keeper_fp = items[0]  # 保留最小 ID 卡
        for card_id, fp in items[1:]:
            gender = read_gender(fp)
            if not gender:
                errors.append('gender_unknown: %s' % fp)
                continue
            new_name = pick_name(pools, gender)
            try:
                rename_card(fp, name, new_name, card_id)
                renamed += 1
            except Exception as e:
                errors.append('rename_fail: %s (%s)' % (fp, e))

    print('[done] 改名 %d 张，错误 %d 个' % (renamed, len(errors)))
    for e in errors[:20]:
        print('[err]  %s' % e)
    if len(errors) > 20:
        print('[err]  ... 共 %d 个错误' % len(errors))

    # 最终校验
    print('\n[verify] 修复后重名组扫描...')
    groups2 = scan_groups(args.card_root)
    dup2 = {k: v for k, v in groups2.items() if len(v) > 1}
    print('[verify] 剩余重名组: %d' % len(dup2))


if __name__ == '__main__':
    main()
