#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""v2.0 数字编号 ID 双轨制迁移脚本

按 v2.0 规范 tmpPlan/财商流游戏-玩家职业设计-v2.0-数字编号规范-20260913-01.md §2-4
为 lag_docs/财商流游戏/玩家职业设计/ 下所有人物卡 (N<流水>-<姓名>.md) 写入:

    occ_id           OCC-<L1 数字>-<5 位流水>  (按 L3 分桶)
    occ_industry_num <L1 数字>               (e.g. "02")
    occ_l2_num       <L4 位>                (e.g. "0207")
    occ_l3_num       <L6 位>                (e.g. "020707")
    _migration_v2    {ts, src_l1, src_l3, merge_note}

旧 id (N<流水>) 保留不动；旧 id 进 _legacy_ids 数组（已有则追加）。

文件名前缀 N<流水>-<姓名>.md 不动（用户决策 D1）。

用法:
    # dry-run（不写文件，只生成报告）
    python3 migrate_v2_ids.py --dry-run

    # 仅迁移 E 行业（试跑 90 张）
    python3 migrate_v2_ids.py --only-l1 E

    # 全量真跑
    python3 migrate_v2_ids.py

    # 跳过子模块（默认就跳过）
    python3 migrate_v2_ids.py --include-submodule
"""
import argparse
import collections
import datetime
import json
import os
import re
import sys
from pathlib import Path
from typing import Dict, List, Tuple

import yaml

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from v2_mapping import (  # noqa: E402
    L1_TO_NUM,
    NUM_TO_L1_NAME,
    SPLIT_RULES,
    resolve_l1,
    l2_num_for,
    l3_num_for,
)

ROOT = Path('/usr/local/LsmAgentGame/LsmAgentGame')
CARD_ROOT = ROOT / 'docs' / '财商流游戏' / '玩家职业设计'
WORK_DIR = ROOT / 'scripts' / '财商流游戏知识库' / 'work'
SUBMODULE_MARKER = 'go-web-debug-tool'

MIGRATION_TS = datetime.datetime.now().isoformat(timespec='seconds')


# === Frontmatter 解析与回写（PyYAML safe_load/dump，不破坏注释）===

def split_frontmatter(text: str) -> Tuple[str, str, str]:
    """拆分 frontmatter / 正文。

    Returns:
        (yaml_block, body, raw_front) 其中 raw_front 包含 --- 标记
    """
    if not text.startswith('---\n'):
        return '', text, ''
    end = text.find('\n---\n', 4)
    if end < 0:
        return '', text, ''
    yaml_text = text[4:end]
    body = text[end + 5:]
    raw_front = text[:end + 5]
    return yaml_text, body, raw_front


def parse_card(path: Path) -> Tuple[dict, str, str]:
    """解析单张卡，返回 (frontmatter_dict, body, raw_frontmatter)。"""
    text = path.read_text(encoding='utf-8')
    yaml_text, body, raw_front = split_frontmatter(text)
    if not yaml_text:
        return {}, body, raw_front
    try:
        fm = yaml.safe_load(yaml_text) or {}
    except yaml.YAMLError:
        return {}, body, raw_front
    return fm, body, raw_front


def write_card(path: Path, fm: dict, body: str) -> None:
    """原子写：tmp → os.replace。"""
    yaml_text = yaml.safe_dump(fm, allow_unicode=True, sort_keys=False, default_flow_style=False)
    new_content = f'---\n{yaml_text}---\n{body}'
    tmp = path.with_suffix(path.suffix + '.tmp')
    tmp.write_text(new_content, encoding='utf-8')
    os.replace(tmp, path)


def is_submodule(path: Path) -> bool:
    """路径是否在子模块 go-web-debug-tool 下。"""
    return SUBMODULE_MARKER in str(path)


# === 扫描与分桶 ===

def scan_cards(root: Path, include_submodule: bool = False) -> List[Path]:
    """扫描所有人物卡文件 N<流水>-<姓名>.md。"""
    paths = []
    for p in root.rglob('N*.md'):
        if not p.is_file():
            continue
        if not include_submodule and is_submodule(p):
            continue
        paths.append(p)
    return paths


def bucket_cards(paths: List[Path]) -> Dict[Tuple[str, str, str], List[Tuple[str, Path, dict]]]:
    """按 (v2.0 l1_num, l2_num, l3_num) 分桶，**不是** v4.0 字母 L1。

    关键：多个 v4.0 字母 L1 (B/G/H/K) 都映射到 v2.0 02；如果按 v4.0 字母
    分桶，跨字母 L3 会各自从 00001 起算产生 occ_id 冲突。

    修复：先用 resolve_l1() 算出每张卡的 v2.0 l1_num，再按
    (l1_num, l2_num, l3_num) 分桶。同 v2.0 L3 内的所有卡共享 5 位流水。

    每桶内按 N<流水> 数字排序，保证幂等。
    """
    buckets: Dict[Tuple[str, str, str], List[Tuple[str, Path, dict]]] = collections.defaultdict(list)
    skipped: List[Tuple[Path, str]] = []
    for p in paths:
        fm, _, _ = parse_card(p)
        if not fm:
            skipped.append((p, 'no_frontmatter'))
            continue
        old_id = fm.get('id', '')
        l1 = fm.get('industry_l1', '')
        l2 = fm.get('industry_l2', '')
        l3 = fm.get('industry_l3', '')
        if not (old_id and l1 and l2 and l3):
            skipped.append((p, f'missing_fields id={old_id} l1={l1}'))
            continue
        # === 关键：先用 resolve_l1 算 v2.0 l1_num ===
        occupation = fm.get('occupation', '')
        l3_dir_name = p.parent.name  # 末段目录名（也是 L3 中文名）
        l1_num = resolve_l1(l1, occupation, l3_dir_name)
        l2_num = l2_num_for(l1, l1_num, l2)
        l3_num = l3_num_for(l2_num, l3)
        # 按 v2.0 数字码分桶（不是 v4.0 字母码）
        buckets[(l1_num, l2_num, l3_num)].append((old_id, p, fm))
    # 桶内按 N<流水> 数字排序
    def n_key(t):
        m = re.match(r'^N(\d+)$', t[0])
        return int(m.group(1)) if m else 0
    for k in buckets:
        buckets[k].sort(key=n_key)
    return buckets, skipped


# === 写回 frontmatter ===

def build_migrated_fm(fm: dict, l1_num: str, occ_id: str, l2_num: str, l3_num: str,
                      src_l1: str, src_l3: str, merge_note: str) -> dict:
    """构造迁移后的 frontmatter dict（不修改原 id）。"""
    new_fm = dict(fm)  # 浅拷贝
    new_fm['occ_id'] = occ_id
    new_fm['occ_industry_num'] = l1_num
    new_fm['occ_l2_num'] = l2_num
    new_fm['occ_l3_num'] = l3_num
    # _legacy_ids 追加（已有则不重复追加）
    legacy = new_fm.get('_legacy_ids')
    old_id = fm.get('id', '')
    if old_id:
        if not legacy:
            new_fm['_legacy_ids'] = [old_id]
        elif isinstance(legacy, list):
            if old_id not in legacy:
                legacy.append(old_id)
                new_fm['_legacy_ids'] = legacy
        elif isinstance(legacy, str):
            # 旧版可能是字符串，统一成列表
            if old_id != legacy:
                new_fm['_legacy_ids'] = [legacy, old_id]
            else:
                new_fm['_legacy_ids'] = [legacy]
    # _migration_v2 审计
    new_fm['_migration_v2'] = {
        'ts': MIGRATION_TS,
        'src_l1': src_l1,
        'src_l3': src_l3,
        'l1_num': l1_num,
        'merge_note': merge_note,
    }
    return new_fm


# === 主流程 ===

def run(dry_run: bool = False, only_l1: str = None, include_submodule: bool = False) -> dict:
    """主迁移流程。

    Args:
        dry_run: True 只生成报告，不写文件
        only_l1: 只迁移指定 v4.0 L1 字母 (e.g. 'E')
        include_submodule: 是否包含子模块（默认 False）

    Returns:
        报告 dict
    """
    WORK_DIR.mkdir(parents=True, exist_ok=True)

    print(f'[*] 扫描人物卡: {CARD_ROOT}')
    paths = scan_cards(CARD_ROOT, include_submodule=include_submodule)
    print(f'[*] 扫描到 {len(paths)} 张卡（排除子模块）')

    buckets, skipped = bucket_cards(paths)
    print(f'[*] 分桶数: {len(buckets)}（按 l1+l2+l3）')

    if only_l1:
        only_l1 = only_l1.upper()
        # v4.0 字母 L1 输入：过滤所有属于该字母 L1 的卡（v4.0 字母 → 桶里 l1_num）
        # 简化：读所有卡（已经分桶），重新过滤
        only_l1_num = L1_TO_NUM.get(only_l1, only_l1)
        # 桶的 key 现在是 (l1_num, l2_num, l3_num)，保留 l1_num == only_l1_num 的桶
        # 但这会丢失被强行合并到其它 num 的卡。改为重新扫描单字母路径。
        print(f'[*] 仅迁移 L1={only_l1}（v4.0 字母）→ v2.0 {only_l1_num}')
        # 重新扫描 + 过滤 v4.0 l1 == only_l1
        from collections import defaultdict
        new_buckets = defaultdict(list)
        for (l1n, l2n, l3n), cards in buckets.items():
            for old_id, p, fm in cards:
                if fm.get('industry_l1', '') == only_l1:
                    new_buckets[(l1n, l2n, l3n)].append((old_id, p, fm))
        buckets = new_buckets
        print(f'[*] 过滤后剩余 {len(buckets)} 桶')

    # 启动校验：所有 v2.0 L1 数字码必须是合法值
    legal_l1_nums = set(NUM_TO_L1_NAME.keys())
    bad_l1_nums = set()
    for (l1_num, _, _) in buckets.keys():
        if l1_num not in legal_l1_nums:
            bad_l1_nums.add(l1_num)
    assert not bad_l1_nums, f'非法的 v2.0 L1 数字码: {bad_l1_nums}'

    # 逐桶分配 occ_id（**逐卡**判定 l1_num，不再按桶共享样本）
    diff_rows: List[dict] = []
    write_count = 0
    error_rows: List[dict] = []

    for (l1_num, l2_num, l3_num), cards in buckets.items():
        # 桶的 key 已经是 v2.0 数字码，直接用
        l3_dir_name = cards[0][1].parent.name  # L3 目录名
        # 第一个卡的 src_l1 用于审计
        sample_src_l1 = cards[0][2].get('industry_l1', '')
        merge_note_base = (
            f'v4.0 {sample_src_l1}-行业 → v2.0 {l1_num}-{NUM_TO_L1_NAME.get(l1_num, "?")}；'
            f'L3 {l3_num}={l3_dir_name}'
        )

        for seq, (old_id, path, fm) in enumerate(cards, start=1):
            # 直接用桶的数字码；不再调用 resolve_l1（避免重复归类冲突）
            occupation = fm.get('occupation', '')
            src_l1 = fm.get('industry_l1', '')
            src_l3 = fm.get('industry_l3', '')
            merge_note = f'{merge_note_base} | occ={occupation[:30]}'

            occ_id = f'OCC-{l1_num}-{seq:05d}'

            new_fm = build_migrated_fm(fm, l1_num, occ_id, l2_num, l3_num,
                                       src_l1=src_l1, src_l3=src_l3, merge_note=merge_note)

            diff_rows.append({
                'old_id': old_id,
                'occ_id': occ_id,
                'l1_num': l1_num,
                'l2_num': l2_num,
                'l3_num': l3_num,
                'src_l1': src_l1,
                'occupation': occupation[:40],
                'file': str(path.relative_to(ROOT)),
                'name': fm.get('name', ''),
            })

            if not dry_run:
                try:
                    _, body, _ = parse_card(path)
                    write_card(path, new_fm, body)
                    write_count += 1
                except Exception as e:
                    error_rows.append({
                        'file': str(path.relative_to(ROOT)),
                        'old_id': old_id,
                        'error': str(e),
                    })
                    print(f'[!] 写失败: {path}: {e}')

    # === 输出报告 ===
    report = {
        'migration_ts': MIGRATION_TS,
        'dry_run': dry_run,
        'only_l1': only_l1,
        'include_submodule': include_submodule,
        'total_cards_scanned': len(paths),
        'total_buckets': len(buckets),
        'total_cards_migrated': len(diff_rows),
        'write_count': write_count,
        'skip_count': len(skipped),
        'error_count': len(error_rows),
        'l1_distribution': dict(collections.Counter(r['l1_num'] for r in diff_rows)),
    }

    report_path = WORK_DIR / 'migration_v2_report.json'
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
    print(f'[*] 报告: {report_path}')

    # diff CSV
    diff_csv_path = WORK_DIR / 'migration_v2_diff.csv'
    with diff_csv_path.open('w', encoding='utf-8', newline='') as f:
        import csv
        w = csv.DictWriter(f, fieldnames=[
            'old_id', 'occ_id', 'l1_num', 'l2_num', 'l3_num',
            'src_l1', 'occupation', 'name', 'file'
        ])
        w.writeheader()
        for row in diff_rows:
            w.writerow(row)
    print(f'[*] diff CSV: {diff_csv_path}')

    if error_rows:
        err_path = WORK_DIR / 'migration_v2_errors.log'
        with err_path.open('w', encoding='utf-8') as f:
            for row in error_rows:
                f.write(json.dumps(row, ensure_ascii=False) + '\n')
        print(f'[!] 错误日志: {err_path}（{len(error_rows)} 条）')

    if skipped:
        skip_path = WORK_DIR / 'migration_v2_skipped.log'
        with skip_path.open('w', encoding='utf-8') as f:
            for path, reason in skipped:
                f.write(f'{path}\t{reason}\n')
        print(f'[!] 跳过: {skip_path}（{len(skipped)} 条）')

    print()
    print('=== 迁移摘要 ===')
    print(f'  模式: {"DRY-RUN" if dry_run else "WRITE"}')
    print(f'  扫描: {len(paths)} 张卡')
    print(f'  桶数: {len(buckets)}')
    print(f'  迁移: {len(diff_rows)} 张卡（写文件 {write_count}）')
    print(f'  跳过: {len(skipped)}')
    print(f'  错误: {len(error_rows)}')
    print(f'  L1 分布: {dict(collections.Counter(r["l1_num"] for r in diff_rows))}')

    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='v2.0 ID 双轨制迁移')
    parser.add_argument('--dry-run', action='store_true', help='只生成报告，不写文件')
    parser.add_argument('--only-l1', type=str, default=None, help='仅迁移指定 v4.0 L1 (e.g. E)')
    parser.add_argument('--include-submodule', action='store_true', help='包含子模块（默认排除）')
    args = parser.parse_args()
    run(dry_run=args.dry_run, only_l1=args.only_l1, include_submodule=args.include_submodule)
