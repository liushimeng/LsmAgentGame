#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""repair_dup_yaml_keys_v45.py —— v4.5 _enrich_v44 子映射 YAML 重复键修复

背景（v4.5，2026-09-16）：
  13,323 张人物卡的 `_enrich_v44` 子映射下存在 `legacy_name` 重复键
  （v4.4 enrich 阶段多次写入遗留）。Go yaml.v3 严格模式下报错，
  且违反「单一事实来源」原则。本脚本在字符串层面精确修复：
    - 保留最后一个出现位置的值（最新 enrich 结果即为最终值）
    - 删除前面的重复行
    - 不动其它格式（字段顺序、缩进、注释）
  重点：**不得**用 yaml.safe_load → dump 重排（会破坏既有格式）。

优化：先用 grep 快速定位含重复 legacy_name 的行，再逐文件精确修复。

用法：
    python3 scripts/财商流游戏知识库/repair_dup_yaml_keys_v45.py            # 执行修复
    python3 scripts/财商流游戏知识库/repair_dup_yaml_keys_v45.py --dry      # 只扫描报告
"""
import argparse
import collections
import os
import re
import subprocess
import sys

import yaml

THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)

from lib_dim_v45 import CARD_ROOT  # noqa: E402

NAME_RE = re.compile(r'^([NP]\d+)-(.+)\.md$')


def find_files_with_dup_legacy(root):
    """用 grep 快速定位含多个 '  legacy_name:' 的文件。"""
    # grep -c 计算每个文件的匹配行数
    try:
        result = subprocess.run(
            ['grep', '-rc', r'^  legacy_name:', root],
            capture_output=True, text=True)
    except Exception:
        return []
    files = []
    for line in result.stdout.splitlines():
        if ':' not in line:
            continue
        fp, count = line.rsplit(':', 1)
        try:
            if int(count) >= 2:
                files.append(fp)
        except ValueError:
            continue
    return files


def find_enrich_block_in_fm(lines):
    """在 frontmatter 行列表中找 _enrich_v44 块。

    lines 应仅为 frontmatter（从 '---' 到 '---' 之间的内容）。
    返回 (start_idx, end_idx) 在 lines 中的索引。
    """
    enrich_start = None
    for i, line in enumerate(lines):
        stripped = line.rstrip('\n')
        if stripped == '_enrich_v44:':
            enrich_start = i
            continue
        if enrich_start is not None:
            # 退出条件：遇到 0 缩进的顶层键
            if stripped and not line.startswith(' '):
                return (enrich_start, i - 1)
    if enrich_start is not None:
        return (enrich_start, len(lines) - 1)
    return None


def fix_duplicate_keys_in_block(lines, start, end):
    """在 lines[start:end+1] 内修复重复键。

    只处理 _enrich_v44 块（子键 2 空格缩进）。
    保留最后一个出现位置的值。
    """
    block = lines[start:end + 1]
    key_pattern = re.compile(r'^  (\w+):\s*(.*)$')
    # 收集 key 行
    key_entries = []  # [(block_idx, key, line)]
    for i, line in enumerate(block):
        m = key_pattern.match(line)
        if m:
            key_entries.append((i, m.group(1), line))

    key_count = collections.Counter(k for _, k, _ in key_entries)
    dup_keys = {k for k, v in key_count.items() if v > 1}

    if not dup_keys:
        return lines, 0, {}

    # 确定要删除的行
    to_remove = set()
    dup_detail = collections.defaultdict(list)
    for idx, key, line in key_entries:
        if key in dup_keys:
            dup_detail[key].append((idx, line))

    for key, entries in dup_detail.items():
        # 保留最后一个
        for idx, _ in entries[:-1]:
            to_remove.add(idx)

    # 重建 block
    new_block = [line for i, line in enumerate(block) if i not in to_remove]
    # 重建 lines
    new_lines = lines[:start] + new_block + lines[end + 1:]
    # 构建修改摘要
    summary = {k: len(v) - 1 for k, v in dup_detail.items()}
    return new_lines, len(to_remove), summary


def process_file(fp):
    """处理单个文件。返回 (fix_count, summary_dict)。"""
    with open(fp, encoding='utf-8') as f:
        content = f.read()

    # 快速检查：不含 2 个及以上 '  legacy_name:' 则跳过
    if content.count('  legacy_name:') < 2:
        return 0, {}

    lines = content.split('\n')

    # 找 frontmatter
    if not lines or lines[0].rstrip() != '---':
        return 0, {}
    fm_end = None
    for i in range(1, min(len(lines), 800)):
        if lines[i].rstrip() == '---':
            fm_end = i
            break
    if fm_end is None:
        return 0, {}

    # 找 _enrich_v44 块
    result = find_enrich_block_in_fm(lines[1:fm_end])
    if result is None:
        return 0, {}

    enrich_start, enrich_end = result
    # 偏移 +1 因为 find_enrich_block_in_fm 使用的是 lines[1:]
    enrich_start += 1
    enrich_end += 1

    new_lines, fix_count, summary = fix_duplicate_keys_in_block(
        lines, enrich_start, enrich_end)
    if fix_count == 0:
        return 0, {}

    new_content = '\n'.join(new_lines)

    # 校验 YAML
    try:
        end_marker = new_content.find('\n---', 3)
        if end_marker < 0:
            raise ValueError('frontmatter 结束符缺失')
        yaml.safe_load(new_content[3:end_marker])
    except Exception as e:
        raise ValueError('修复后 YAML 解析失败: %s' % e)

    # 校验 legacy_name 只剩一个
    remaining = new_content.count('  legacy_name:')
    if remaining != 1:
        raise ValueError('修复后 legacy_name 数量=%d（应为1）' % remaining)

    with open(fp, 'w', encoding='utf-8') as f:
        f.write(new_content)

    return fix_count, summary


def main():
    ap = argparse.ArgumentParser(description='v4.5 _enrich_v44 重复键修复')
    ap.add_argument('--dry', action='store_true', help='只扫描报告不修改')
    ap.add_argument('--card-root', default=CARD_ROOT)
    args = ap.parse_args()

    print('[phase1] grep 定位含重复 legacy_name 的文件...')
    targets = find_files_with_dup_legacy(args.card_root)
    print('[phase1] 目标文件: %d' % len(targets))

    if args.dry:
        print('[dry] 不修改。前 5 个目标:')
        for fp in targets[:5]:
            print('  %s' % fp)
        return

    total = 0
    fixed = 0
    errors = []
    summary_all = collections.Counter()

    for fp in targets:
        try:
            fc, summary = process_file(fp)
            if fc > 0:
                fixed += 1
                total += fc
                for k, v in summary.items():
                    summary_all[k] += v
                print('[fix] %s 删除 %d 个重复键 %s' % (
                    os.path.basename(fp), fc, dict(summary)))
        except Exception as e:
            errors.append('%s: %s' % (fp, e))

    print('\n[done] 修复文件: %d, 删除重复键行: %d, 错误: %d' % (
        fixed, total, len(errors)))
    print('[summary] 按 key: %s' % dict(summary_all))
    if errors:
        for e in errors[:10]:
            print('[err]  %s' % e)


if __name__ == '__main__':
    main()
