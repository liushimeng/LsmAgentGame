#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""v2.0 目录树重命名脚本（自下而上）

按 v2.0 规范 §1 + §4 把字母目录树改为数字目录树:

    L3 目录 (5,872 个):  A0101-水稻种植    -> 010101-水稻种植
    L2 目录 (254 个):    A01-种植业        -> 01-01-种植业
    L1 目录 (25 个):     A-农林牧渔        -> 01-农林牧渔

L4 (CN-E-华东 等地区档) / L5 (年龄段) 不动。
文件名前缀 N<流水>-<姓名>.md 不动（用户决策 D1）。
子模块 go-web-debug-tool 跳过。

每 500 个 git mv 后自动 git add + git commit（避免 git index lock）。
全部走 Python shutil + subprocess，避免 shell sed 注入。

用法:
    # dry-run 模式（只列计划，不实际 mv）
    python3 rename_to_v2.py --dry-run

    # 仅重命名 L3
    python3 rename_to_v2.py --only-level l3

    # 全量真跑（每批 500 commit）
    python3 rename_to_v2.py --batch-size 500
"""
import argparse
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import List, Tuple

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from v2_mapping import L1_TO_NUM  # noqa: E402

ROOT = Path('/usr/local/LsmAgentGame/LsmAgentGame')
CARD_ROOT = ROOT / 'docs' / '财商流游戏' / '玩家职业设计'
WORK_DIR = ROOT / 'scripts' / '财商流游戏知识库' / 'work'
SUBMODULE_MARKER = 'go-web-debug-tool'


def is_submodule(path: Path) -> bool:
    return SUBMODULE_MARKER in str(path)


def safe_git_mv(src: Path, dst: Path) -> bool:
    """git mv src → dst；不存在则 mkdir + git add。"""
    if not src.exists():
        return False
    if dst.exists():
        # 目标已存在，跳过（防止覆盖）
        return False
    dst.parent.mkdir(parents=True, exist_ok=True)
    rel_src = src.relative_to(ROOT)
    rel_dst = dst.relative_to(ROOT)
    try:
        subprocess.run(
            ['git', 'mv', str(rel_src), str(rel_dst)],
            cwd=str(ROOT),
            check=True,
            capture_output=True,
        )
        return True
    except subprocess.CalledProcessError as e:
        print(f'[!] git mv 失败: {rel_src} -> {rel_dst}: {e.stderr.decode()}')
        return False


def git_commit_batch(msg: str, paths: List[Path]) -> bool:
    """git add 指定路径 → commit。"""
    if not paths:
        return True
    rels = [str(p.relative_to(ROOT)) for p in paths]
    try:
        subprocess.run(['git', 'add', '--'] + rels, cwd=str(ROOT), check=True, capture_output=True)
        subprocess.run(['git', 'commit', '-m', msg], cwd=str(ROOT), check=True, capture_output=True)
        return True
    except subprocess.CalledProcessError as e:
        print(f'[!] git commit 失败: {e.stderr.decode()}')
        return False


def l3_rename_plan() -> List[Tuple[Path, Path]]:
    """L3 目录重命名计划：A0101-xxx → 010101-xxx

    L3 目录名格式: <L1字母><L2序号 2 位><L3序号 2 位>-<名称>
    例: A0101-水稻种植 → 010101-水稻种植
    """
    plan = []
    for l1_dir in sorted(CARD_ROOT.iterdir()):
        if not l1_dir.is_dir():
            continue
        if l1_dir.name.startswith('_') or is_submodule(l1_dir):
            continue
        m_l1 = re.match(r'^([A-Z])-', l1_dir.name)
        if not m_l1:
            continue
        l1_letter = m_l1.group(1)
        l1_num = L1_TO_NUM.get(l1_letter)
        if not l1_num:
            continue
        # 遍历 L2 目录
        for l2_dir in sorted(l1_dir.iterdir()):
            if not l2_dir.is_dir() or l2_dir.name.startswith('_'):
                continue
            m_l2 = re.match(r'^([A-Z])(\d{2})-', l2_dir.name)
            if not m_l2:
                continue
            l2_seq = m_l2.group(2)
            l2_num = f'{l1_num}{l2_seq}'
            # 遍历 L3 目录
            for l3_dir in sorted(l2_dir.iterdir()):
                if not l3_dir.is_dir() or l3_dir.name.startswith('_'):
                    continue
                m_l3 = re.match(r'^([A-Z])(\d{2})(\d{2})-', l3_dir.name)
                if not m_l3:
                    continue
                l3_seq = m_l3.group(3)
                l3_num = f'{l2_num}{l3_seq}'
                # 新名 = <l3_num>-<原名后缀>
                # 原: A0101-水稻种植 → 新: 010101-水稻种植
                new_name = re.sub(r'^[A-Z]\d{4}-', f'{l3_num}-', l3_dir.name)
                if new_name != l3_dir.name:
                    plan.append((l3_dir, l3_dir.parent / new_name))
    return plan


def l2_rename_plan() -> List[Tuple[Path, Path]]:
    """L2 目录重命名计划：A01-种植业 → 01-01-种植业"""
    plan = []
    for l1_dir in sorted(CARD_ROOT.iterdir()):
        if not l1_dir.is_dir():
            continue
        if l1_dir.name.startswith('_') or is_submodule(l1_dir):
            continue
        m_l1 = re.match(r'^([A-Z])-', l1_dir.name)
        if not m_l1:
            continue
        l1_letter = m_l1.group(1)
        l1_num = L1_TO_NUM.get(l1_letter)
        if not l1_num:
            continue
        for l2_dir in sorted(l1_dir.iterdir()):
            if not l2_dir.is_dir() or l2_dir.name.startswith('_'):
                continue
            m_l2 = re.match(r'^([A-Z])(\d{2})-', l2_dir.name)
            if not m_l2:
                continue
            l2_seq = m_l2.group(2)
            new_name = re.sub(r'^[A-Z]\d{2}-', f'{l1_num}-{l2_seq}-', l2_dir.name)
            if new_name != l2_dir.name:
                plan.append((l2_dir, l2_dir.parent / new_name))
    return plan


def l1_rename_plan() -> List[Tuple[Path, Path]]:
    """L1 目录重命名计划：A-农林牧渔 → 01-农林牧渔"""
    plan = []
    for l1_dir in sorted(CARD_ROOT.iterdir()):
        if not l1_dir.is_dir():
            continue
        if l1_dir.name.startswith('_') or is_submodule(l1_dir):
            continue
        m_l1 = re.match(r'^([A-Z])-', l1_dir.name)
        if not m_l1:
            continue
        l1_letter = m_l1.group(1)
        l1_num = L1_TO_NUM.get(l1_letter)
        if not l1_num:
            continue
        new_name = re.sub(r'^[A-Z]-', f'{l1_num}-', l1_dir.name)
        if new_name != l1_dir.name:
            plan.append((l1_dir, l1_dir.parent / new_name))
    return plan


def run(dry_run: bool = False, only_level: str = None, batch_size: int = 500) -> None:
    WORK_DIR.mkdir(parents=True, exist_ok=True)
    log_path = WORK_DIR / 'rename_v2_log.txt'

    plans = {}
    if only_level is None or only_level == 'l3':
        plans['l3'] = l3_rename_plan()
    if only_level is None or only_level == 'l2':
        plans['l2'] = l2_rename_plan()
    if only_level is None or only_level == 'l1':
        plans['l1'] = l1_rename_plan()

    print('=== 目录重命名计划 ===')
    for level, plan in plans.items():
        print(f'  L{level[-1]}: {len(plan)} 个待改名')
        for src, dst in plan[:3]:
            print(f'    {src.name}  →  {dst.name}')
        if len(plan) > 3:
            print(f'    ... 还有 {len(plan) - 3} 个')

    if dry_run:
        print('\n[DRY-RUN] 不实际执行 mv')
        return

    # 真跑：按 level 顺序处理（l3 → l2 → l1），每批 commit
    log_lines = []
    total_moved = 0
    for level in ['l3', 'l2', 'l1']:
        if level not in plans:
            continue
        plan = plans[level]
        print(f'\n[*] 开始 L{level[-1]} 重命名（{len(plan)} 个）...')

        moved_in_batch: List[Path] = []
        for i, (src, dst) in enumerate(plan, start=1):
            if safe_git_mv(src, dst):
                moved_in_batch.append(dst)
                total_moved += 1
            if len(moved_in_batch) >= batch_size:
                msg = f'refactor(财商流游戏): v2.0 目录重命名 L{level[-1]} 第 {i // batch_size} 批 ({len(moved_in_batch)} 个)'
                ok = git_commit_batch(msg, moved_in_batch)
                log_lines.append(f'L{level[-1]} batch {i // batch_size}: commit {len(moved_in_batch)} ok={ok}')
                moved_in_batch = []
        # 收尾
        if moved_in_batch:
            msg = f'refactor(财商流游戏): v2.0 目录重命名 L{level[-1]} 收尾 ({len(moved_in_batch)} 个)'
            ok = git_commit_batch(msg, moved_in_batch)
            log_lines.append(f'L{level[-1]} tail: commit {len(moved_in_batch)} ok={ok}')

    log_path.write_text('\n'.join(log_lines), encoding='utf-8')
    print(f'\n=== 完成 ===')
    print(f'  移动总数: {total_moved}')
    print(f'  日志: {log_path}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='v2.0 目录树重命名')
    parser.add_argument('--dry-run', action='store_true', help='只列计划，不实际 mv')
    parser.add_argument('--only-level', type=str, choices=['l1', 'l2', 'l3'], default=None)
    parser.add_argument('--batch-size', type=int, default=500, help='每批 commit 数量')
    args = parser.parse_args()
    run(dry_run=args.dry_run, only_level=args.only_level, batch_size=args.batch_size)
