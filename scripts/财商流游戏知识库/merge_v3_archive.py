#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""merge_v3_archive.py —— 阶段 A：v3.x 归档合并入字母目录树

执行方案 `tmpPlan/财商流游戏-玩家职业设计-v4.4-归档合并与全息画像补全-20260914-01.md` §3。

把 `docs/财商流游戏/99_归档与待整理/v3.x旧版扁平目录归档/` 下 69,437 张 N 卡 + 18 张 P 卡
合并进 `docs/财商流游戏/玩家职业设计/` 的字母五层目录树，并统一文件命名规格。

用法：
    python3 merge_v3_archive.py --dry                # 干跑：闸门 + 报告
    python3 merge_v3_archive.py --dry --limit 100    # 抽样 100 张做干跑
    python3 merge_v3_archive.py                     # 真跑（需闸门全过）

干跑/真跑均落盘：
    work/merge_v44_report.json   总体报告（总量/冲突/闸门/抽样）
    work/merge_v44_name_map.json id→新化名全量映射
"""
import argparse
import json
import os
import random
import re
import shutil
import sys
import time
from collections import Counter, defaultdict
from datetime import datetime, timezone

import yaml

# ── 路径常量 ───────────────────────────────────────────────────────
ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..'))
ARCHIVE_ROOT = os.path.join(ROOT, 'docs', '财商流游戏', '99_归档与待整理', 'v3.x旧版扁平目录归档')
LETTER_TREE_ROOT = os.path.join(ROOT, 'docs', '财商流游戏', '玩家职业设计')
WORK_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'work')
REPORT_PATH = os.path.join(WORK_DIR, 'merge_v44_report.json')
NAME_MAP_PATH = os.path.join(WORK_DIR, 'merge_v44_name_map.json')

L4_VALID = {'CN-N-华北', 'CN-NE-东北', 'CN-E-华东', 'CN-C-华中', 'CN-S-华南',
            'CN-SW-西南', 'CN-NW-西北', 'OV-海外', 'XX-未知'}
L5_VALID = {'16-24', '25-34', '35-44', '45-54', '55-64', '65+', '65', 'XX-未知'}
# 注：spec 定义 7 档（'65+'），但字母树目录实际使用 '65'，
#     归档也用 '65' 和 '65+' 两套写法（22 个 '65' dir 涉及 22 张卡）。
#     按任务要求"L4/L5 沿用归档原路径段"——原样保留 '65' 和 '65+' 两种。

# ── 种子化名生成器（与 gen_batch._fresh_name 行为一致） ──────────────
SURNAMES_M = ['王', '李', '张', '刘', '陈', '杨', '赵', '黄', '周', '吴', '徐', '孙', '胡', '朱', '高',
              '林', '何', '郭', '马', '罗', '梁', '宋', '郑', '谢', '韩', '唐', '冯', '于', '董', '萧',
              '程', '曹', '袁', '邓', '许', '傅', '沈', '曾', '彭', '吕', '苏', '卢', '蒋', '蔡', '贾',
              '丁', '魏', '薛', '叶', '阎', '余', '潘', '杜', '戴', '夏', '钟', '汪', '田', '任', '姜',
              '范', '方', '石', '姚', '谭', '廖', '邹', '熊', '金', '陆', '郝', '孔', '白', '崔', '康']
SURNAMES_F = SURNAMES_M
GIVEN_M = list('强军伟勇刚磊涛峰波辉鹏飞龙虎彪斌杰亮明华建国志民永康健铁钢山河海川兵武雄豪铭鑫栋梁坚毅承旭阳锋航帆骏骐腾源洲洋浩宇轩宸昊煜烨燊垚焱淼猛超越凯旋仁信义礼智忠孝勤俭谦恭')
GIVEN_F = list('兰婷娟秀慧芳燕玲莉娜静丽敏雪梅萍红霞珍琴婉妍媛瑶瑾琪珊琳琦莹蕊薇蕾蓉菲萱芸茜茵荷莲怡悦欣晴岚心月昕晞晗曦暖妤姝娴婵婧婕姣娅娴嫣彤茹蓓菁菡菱菊桃樱棠柔妙')


class NameGenerator:
    """种子化、唯一化名生成器（每 50 次重试回退 '某'）。"""

    def __init__(self, seed, used_names):
        self.rng = random.Random(seed)
        self.used = set(used_names)

    def fresh(self, gender):
        for _ in range(50):
            s = self.rng.choice(SURNAMES_F if gender == '女' else SURNAMES_M)
            n = ''.join(self.rng.choices(GIVEN_F if gender == '女' else GIVEN_M,
                                          k=self.rng.choice([1, 1, 2])))
            name = s + n
            if name not in self.used:
                self.used.add(name)
                return name
        # 超 50 次重试回退
        idx = 0
        while True:
            name = '某' + str(idx)
            if name not in self.used:
                self.used.add(name)
                return name
            idx += 1


def scan_letter_tree():
    """扫描字母树，建三张索引表：
       - l1_dirs: {L1 letter -> set(L1 dir full name 如 'A-农林牧渔')}
       - l2_dirs: {L2 code -> set(dir full name 如 'A02-林业与林下经济')}
       - l3_dirs: {L3 code -> set(dir full name 如 'A0202-xxx')}
       - existing_names: set(姓名)
       - existing_ids: set(id 字符串)
       - l2_codes_by_l1: {L1 letter -> set(L2 codes)}
       - l3_codes_by_l2: {L2 code -> set(L3 codes)}
    """
    l1_dirs = defaultdict(set)
    l2_dirs = defaultdict(set)
    l3_dirs = defaultdict(set)
    existing_names = set()
    existing_ids = set()
    l2_codes_by_l1 = defaultdict(set)
    l3_codes_by_l2 = defaultdict(set)
    if not os.path.isdir(LETTER_TREE_ROOT):
        return l1_dirs, l2_dirs, l3_dirs, existing_names, existing_ids, l2_codes_by_l1, l3_codes_by_l2

    for entry in sorted(os.listdir(LETTER_TREE_ROOT)):
        if entry.startswith('_') or '-' not in entry:
            continue
        l1_letter = entry.split('-', 1)[0]
        if len(l1_letter) != 1 or not l1_letter.isalpha():
            continue
        l1_dirs[l1_letter].add(entry)
        l1_path = os.path.join(LETTER_TREE_ROOT, entry)
        for l2_entry in sorted(os.listdir(l1_path)):
            if l2_entry.startswith('_') or '-' not in l2_entry:
                continue
            l2_code = l2_entry.split('-', 1)[0]
            l2_dirs[l2_code].add(l2_entry)
            l2_codes_by_l1[l1_letter].add(l2_code)
            l2_path = os.path.join(l1_path, l2_entry)
            for l3_entry in sorted(os.listdir(l2_path)):
                if l3_entry.startswith('_') or '-' not in l3_entry:
                    continue
                l3_code = l3_entry.split('-', 1)[0]
                l3_dirs[l3_code].add(l3_entry)
                l3_codes_by_l2[l2_code].add(l3_code)
                l3_path = os.path.join(l2_path, l3_entry)
                # 叶子（5 层：L1/L2/L3/L4/L5/file.md）—— 但也可能有 S000+ 分片
                for root, _, files in os.walk(l3_path):
                    for f in files:
                        if f.endswith('.md'):
                            # 提取姓名（前缀是 id-）
                            m = re.match(r'^([NP]\d+)-(.+)\.md$', f)
                            if m:
                                fid = m.group(1)
                                fname = m.group(2)
                                existing_ids.add(fid)
                                existing_names.add(fname)
    return (l1_dirs, l2_dirs, l3_dirs, existing_names, existing_ids,
            l2_codes_by_l1, l3_codes_by_l2)


def split_frontmatter(content):
    """拆 frontmatter，返回 (frontmatter_str, fm_dict, body_str)."""
    if not content.startswith('---'):
        return None, None, content
    parts = content.split('---', 2)
    if len(parts) < 3:
        return None, None, content
    fm_str = parts[1].lstrip('\n')
    body = parts[2]
    try:
        data = yaml.safe_load(fm_str)
        if not isinstance(data, dict):
            return fm_str, None, body
    except Exception:
        return fm_str, None, body
    return fm_str, data, body


def parse_archive_path(rel_path):
    """从归档相对路径解析 L4 / L5 / 数字 L3 dir 名。
    rel 形如 '01-农林牧渔/01-02-林业与林下经济/010202-林业与林下经济/CN-NE-东北/45-54/N10220-林下经.md'
    返回 (L4, L5, numeric_l3_dir_name, numeric_l3_name_segment)
    """
    parts = rel_path.split('/')
    if len(parts) < 6:
        return None, None, None, None
    l4 = parts[3]
    l5 = parts[4]
    l3_dir = parts[2]
    l3_name = '-'.join(l3_dir.split('-')[1:])
    return l4, l5, l3_dir, l3_name


def build_plan(dry=True, limit=None):
    """扫描归档，建：
       - cards: 列表 of dicts {id, src_path, rel_path, fm, gender, industry_*, l4, l5, l3_dir_name}
       - target_path_for_id: {id -> target_rel_path}
       - filename_for_id: {id -> target_filename (含 .md)}
       - new_name_for_id: {id -> 新化名}（仅 N 卡）
    返回 (cards, stats, conflicts, name_gen)
    """
    t0 = time.time()
    l1_dirs, l2_dirs, l3_dirs, existing_names, existing_ids, l2_codes_by_l1, l3_codes_by_l2 = scan_letter_tree()

    # 加载 l1_rules.L1_NAMES（行业域中文名）
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    try:
        from l1_rules import L1_NAMES
    except Exception:
        L1_NAMES = {}

    # 收集所有归档卡
    cards = []
    if not os.path.isdir(ARCHIVE_ROOT):
        return [], {'error': f'archive root missing: {ARCHIVE_ROOT}'}, [], None

    file_list = []
    for root, _, files in os.walk(ARCHIVE_ROOT):
        for f in files:
            if (f.startswith('N') or f.startswith('P')) and f.endswith('.md'):
                file_list.append(os.path.join(root, f))
    file_list.sort()  # 确定序，姓名种子化分配稳定

    if limit is not None:
        file_list = file_list[:limit]

    name_gen = NameGenerator(seed=20260914, used_names=set(existing_names))
    name_gen.used.update({'林若曦', '张建国', '吴祖德', '陈志远', '孙明哲', '沈文涛', '周小慧', '赵明轩',
                          '陈美凤', '钱昊', '刘芳', '苏晴', '郑卫东', '马晓军', '何秀英', '王小军',
                          '李秀兰', '罗志强'})  # 18 P 卡实名保险（不依赖现有 letter tree 是否已有这些）

    target_path_for_id = {}
    filename_for_id = {}
    new_name_for_id = {}

    target_filenames = set(os.listdir(LETTER_TREE_ROOT)) if False else set()
    target_paths_seen = set()

    # 用于唯一性 + 冲突检测
    target_id_to_path = {}
    target_path_to_id = {}
    id_conflicts = []     # 同 id 目标路径不同
    fn_conflicts = []     # 同 target filename 已存在
    l4_l5_invalid = []    # L4/L5 段非法
    missing_industry = [] # 缺 industry_l1/l2/l3
    multi_name_per_l3 = defaultdict(set)  # industry_l3 -> set(path_l3_name) 检测冲突
    multi_name_anomalies = []

    for full_path in file_list:
        rel = os.path.relpath(full_path, ARCHIVE_ROOT)
        try:
            with open(full_path, 'r', encoding='utf-8') as fh:
                content = fh.read()
        except Exception as e:
            continue
        fm_str, fm, body = split_frontmatter(content)
        if fm is None:
            continue
        cid = fm.get('id')
        if cid is None:
            continue
        # 跳过 P 卡：原文件名已是真名 (per 02-规约) 且独立路径处理
        is_p = isinstance(cid, str) and cid.startswith('P')

        l4, l5, l3_dir_name, l3_name = parse_archive_path(rel)
        if l4 not in L4_VALID:
            l4_l5_invalid.append({'id': cid, 'rel': rel, 'L4': l4})
            continue
        if l5 not in L5_VALID:
            l4_l5_invalid.append({'id': cid, 'rel': rel, 'L5': l5})
            continue

        industry_l1 = fm.get('industry_l1')
        industry_l2 = fm.get('industry_l2')
        industry_l3 = fm.get('industry_l3')
        if not (industry_l1 and industry_l2 and industry_l3):
            missing_industry.append({'id': cid, 'rel': rel,
                                     'l1': industry_l1, 'l2': industry_l2, 'l3': industry_l3})
            continue

        # industry_l1 必须是 A-Z 单字母
        if not (isinstance(industry_l1, str) and len(industry_l1) == 1 and industry_l1.isalpha()):
            missing_industry.append({'id': cid, 'rel': rel, 'l1': industry_l1})
            continue
        if industry_l2 not in l2_dirs:
            missing_industry.append({'id': cid, 'rel': rel, 'l2': industry_l2,
                                     'reason': 'industry_l2 not in letter tree'})
            continue

        # L1 dir（优先级① 字母树既有）
        if l1_dirs.get(industry_l1):
            l1_dir = sorted(l1_dirs[industry_l1])[0]  # 取唯一（按排序确定性）
        else:
            # 兜底：l1_rules.L1_NAMES
            l1_name = L1_NAMES.get(industry_l1, '未知域')
            l1_dir = f'{industry_l1}-{l1_name}'

        # L2 dir
        if l2_dirs.get(industry_l2):
            l2_dir = sorted(l2_dirs[industry_l2])[0]
        else:
            # 兜底：从归档 path L2 dir 名（parts[1]）
            l2_archive = rel.split('/')[1]
            l2_name_seg = '-'.join(l2_archive.split('-')[1:])
            l2_dir = f'{industry_l2}-{l2_name_seg}'

        # L3 dir（优先级① 字母树；③ 归档 path L3 dir 名）
        if l3_dirs.get(industry_l3):
            l3_dir = sorted(l3_dirs[industry_l3])[0]
        else:
            # 兜底：用 industry_l3 letter code 作为 prefix，name 取归档 path L3 dir 名
            # 例如 industry_l3=A0102, l3_dir_name='010102-中药材种植与加工' → 'A0102-中药材种植与加工'
            # 这样保证目标路径符合 02-规约「<L3码>-<L3名>」形态
            l3_name_only = l3_name  # 归档 path L3 dir 名去掉数字前缀部分
            l3_dir = f'{industry_l3}-{l3_name_only}'

        # 一致性检查：同一 industry_l3 在归档内若有多种 path 名，记入异常
        multi_name_per_l3[industry_l3].add(l3_name)

        # 目标路径
        target_rel = os.path.join(l1_dir, l2_dir, l3_dir, l4, l5)
        target_rel_unix = target_rel.replace(os.sep, '/')

        # 新文件名 / 新化名
        if is_p:
            # P 卡：保留原文件名（含真名）
            target_filename = os.path.basename(rel)
            new_name = None
        else:
            # N 卡：生成新化名
            gender = fm.get('gender') or '男'
            new_name = name_gen.fresh(gender)
            target_filename = f'{cid}-{new_name}.md'

        # 检查冲突
        if cid in target_id_to_path and target_id_to_path[cid] != target_rel_unix:
            id_conflicts.append({'id': cid, 'paths': [target_id_to_path[cid], target_rel_unix]})
        target_id_to_path[cid] = target_rel_unix

        # 文件名冲突：检查同一目标目录下是否已有同 id 文件名（来自字母树）
        target_full = os.path.join(LETTER_TREE_ROOT, target_rel_unix, target_filename)
        if os.path.exists(target_full):
            fn_conflicts.append({'id': cid, 'target': os.path.relpath(target_full, ROOT)})

        if target_filename in target_paths_seen:
            fn_conflicts.append({'id': cid, 'duplicate_filename': target_filename})
        target_paths_seen.add(target_filename)

        target_path_for_id[cid] = target_rel_unix
        filename_for_id[cid] = target_filename
        if new_name is not None:
            new_name_for_id[cid] = new_name

        cards.append({
            'id': cid,
            'src_path': rel,
            'src_full': full_path,
            'gender': fm.get('gender') or '男',
            'industry_l1': industry_l1,
            'industry_l2': industry_l2,
            'industry_l3': industry_l3,
            'l4': l4,
            'l5': l5,
            'content': content,
            'fm_str': fm_str,
            'body': body,
            'is_p': is_p,
            'target_rel': target_rel_unix,
            'target_filename': target_filename,
            'new_name': new_name,
        })

    # 处理 multi-name anomalies
    for code, names in multi_name_per_l3.items():
        if len(names) > 1:
            multi_name_anomalies.append({'industry_l3': code, 'names': sorted(names)})

    elapsed = time.time() - t0

    stats = {
        'scanned_files': len(file_list),
        'planned_cards': len(cards),
        'planned_n_cards': sum(1 for c in cards if not c['is_p']),
        'planned_p_cards': sum(1 for c in cards if c['is_p']),
        'elapsed_sec': round(elapsed, 2),
        'existing_letter_tree_ids': len(existing_ids),
        'existing_letter_tree_names': len(existing_names),
        'unique_target_filenames': len(target_paths_seen),
    }
    conflicts = {
        'id_conflicts': id_conflicts,
        'filename_conflicts': fn_conflicts,
        'l4_l5_invalid': l4_l5_invalid,
        'missing_industry': missing_industry,
        'multi_name_anomalies': multi_name_anomalies,
    }
    return cards, stats, conflicts, new_name_for_id, name_gen


def modify_content(card, ts_iso, src_rel_path, name_map_for_card, stats_anomalies):
    """原地修改 frontmatter：name 行替换；_raw 块追加 legacy_name；frontmatter 末尾追加 _merge_v44 章。
    正文 H1 姓名段替换；§9 末尾追加 v4.4 行。
    返回 (new_content, ok_bool, anomaly_msg_or_None)
    """
    content = card['content']
    cid = card['id']
    new_name = card['new_name']

    # 1. frontmatter: line-level 替换 `name: <旧>` 行
    fm_str = card['fm_str']
    lines = fm_str.split('\n')
    new_lines = []
    name_replaced = False
    legacy_name = None
    if not card['is_p']:
        # 找 `name: <something>` 行
        name_pat = re.compile(r'^(name\s*:\s*)(.+?)\s*$')
        for ln in lines:
            m = name_pat.match(ln)
            if m and not name_replaced:
                legacy_name = m.group(2).strip()
                # 引号保护
                if (legacy_name.startswith('"') and legacy_name.endswith('"')) \
                        or (legacy_name.startswith("'") and legacy_name.endswith("'")):
                    quoted = legacy_name
                else:
                    quoted = legacy_name
                new_lines.append(f'{m.group(1)}{new_name}')
                name_replaced = True
            else:
                new_lines.append(ln)
        if not name_replaced:
            stats_anomalies.append({'id': cid, 'rel': card['src_path'], 'reason': 'name line not found'})
            return content, False, 'name line not found'
        new_fm_str = '\n'.join(new_lines)
        # 在 _raw 块下追加 legacy_name（在 _raw: 之后的第一个非缩进行/缩进 key 之前）——
        # 简单方案：找到 `_raw:` 行末尾追加一个 legacy_name 缩进行
        # 实际 _raw 是 mapping，找最后缩进行后插入
        legacy_line = f'  legacy_name: {legacy_name}' if not legacy_name.startswith('"') and not legacy_name.startswith("'") \
                       else f'  legacy_name: {legacy_name}'
        # 找到 _raw: 行索引，向下找到第一个非缩进行（含 '---' 或顶层 key 行）
        raw_lines = new_fm_str.split('\n')
        raw_idx = None
        for i, ln in enumerate(raw_lines):
            if ln.strip().startswith('_raw:'):
                raw_idx = i
                break
        if raw_idx is not None:
            # 找 _raw 块结束：下一个非缩进行（不以空格/制表符开头），或文件末
            j = raw_idx + 1
            while j < len(raw_lines):
                if raw_lines[j] and not (raw_lines[j].startswith(' ') or raw_lines[j].startswith('\t')):
                    break
                j += 1
            # 在 j-1 后（即 _raw 块内最后一行后）插入 legacy_line
            # 先找到 _raw 块最后非空行
            insert_at = raw_idx + 1
            while insert_at < j and raw_lines[insert_at].strip():
                insert_at += 1
            # 现在 insert_at 指向 _raw 块末尾下一行；插入位置应是最后有内容那一行后
            # 找到 _raw 块最后一行（j-1 是第一个非缩进行；块内最后有内容行是 j-1 之前最后一个非空）
            last_content_in_raw = raw_idx + 1
            for k in range(raw_idx + 1, j):
                if raw_lines[k].strip():
                    last_content_in_raw = k
            raw_lines.insert(last_content_in_raw + 1, legacy_line)
            new_fm_str = '\n'.join(raw_lines)

        # 在末尾（最后一个 '---' 之前）插入 _merge_v44 章
        merge_yaml = (
            '_merge_v44:\n'
            f"  ts: '{ts_iso}'\n"
            f"  src_path: '{src_rel_path}'\n"
            f"  legacy_file: '{os.path.basename(card['src_path'])}'"
        )
        # 在 fm_str 末尾插入（在 '---' 前）
        # fm_str 不含包裹的 '---'，只是 frontmatter 内部文本
        if new_fm_str.endswith('\n'):
            new_fm_str = new_fm_str + merge_yaml + '\n'
        else:
            new_fm_str = new_fm_str + '\n' + merge_yaml + '\n'

        # 重组 content
        # 找到原 content 中第一个 '---' 后的 body 开始
        body = card['body']
        # H1 行处理： "# <旧职业名> · 男 · 52 岁 · <原始职业>"
        h1_pat = re.compile(r'^(#\s+)(.+?)(\s*·\s*)', re.MULTILINE)
        body_lines = body.split('\n')
        h1_replaced = False
        for i, ln in enumerate(body_lines):
            mm = re.match(r'^(#\s+)(.+?)(\s*·.*)?$', ln)
            if mm and not h1_replaced:
                # 替换首段（姓名）
                old_h1 = mm.group(2)
                new_body_line = f'{mm.group(1)}{new_name}{mm.group(3) or ""}'
                body_lines[i] = new_body_line
                h1_replaced = True
                break
        # §9 数据溯源末尾追加
        # 找 §9 节末尾：在最后一个非空行后追加
        new_body = '\n'.join(body_lines)
        v44_line = f"- **v4.4 归档合并**：自 `{card['src_path']}` 并入，原文件名 `{os.path.basename(card['src_path'])}`"
        # 在末尾追加（保留原结尾换行）
        if new_body.endswith('\n'):
            new_body = new_body + v44_line + '\n'
        else:
            new_body = new_body + '\n' + v44_line + '\n'

        new_content = '---\n' + new_fm_str + '---\n' + new_body
        return new_content, True, None

    # P 卡：仅添加 _merge_v44 章（不动 name）
    else:
        merge_yaml = (
            '_merge_v44:\n'
            f"  ts: '{ts_iso}'\n"
            f"  src_path: '{src_rel_path}'\n"
            f"  legacy_file: '{os.path.basename(card['src_path'])}'"
        )
        if fm_str.endswith('\n'):
            new_fm_str = fm_str + merge_yaml + '\n'
        else:
            new_fm_str = fm_str + '\n' + merge_yaml + '\n'
        # 重组
        new_content = '---\n' + new_fm_str + '---\n' + card['body']
        return new_content, True, None


def apply_real(cards, stats_anomalies, name_map_for_id, ts_iso, dry=True):
    """真跑：写文件到 LETTER_TREE_ROOT + 移动（先复制后删）。dry 模式仅做路径试算。
    name_map_for_id: {id: new_name}（仅 N 卡）
    返回 stats 增量。
    """
    applied = 0
    skipped = 0
    sample_log = []
    for i, card in enumerate(cards):
        src_full = card['src_full']
        target_rel = card['target_rel']
        target_filename = card['target_filename']
        target_full = os.path.join(LETTER_TREE_ROOT, target_rel, target_filename)
        if dry:
            # 仅记录首 20 条作为抽样
            if i < 20:
                sample_log.append({
                    'id': card['id'],
                    'src_path': card['src_path'],
                    'target_path': os.path.relpath(target_full, ROOT),
                    'old_name': (re.search(r'^name:\s*(.+)$', card['fm_str'], re.MULTILINE) or [None, None]).group(1) if False else None,
                    'new_name': card['new_name'],
                })
            continue

        # 修改内容
        new_content, ok, anomaly = modify_content(card, ts_iso, card['src_path'],
                                                  None, stats_anomalies)
        if not ok:
            skipped += 1
            continue
        # 写目标目录
        os.makedirs(os.path.dirname(target_full), exist_ok=True)
        if os.path.exists(target_full):
            skipped += 1
            stats_anomalies.append({'id': card['id'], 'reason': 'target file exists before write',
                                    'target': os.path.relpath(target_full, ROOT)})
            continue
        with open(target_full, 'w', encoding='utf-8') as fh:
            fh.write(new_content)
        # 删除原文件
        try:
            os.remove(src_full)
        except FileNotFoundError:
            pass
        applied += 1
    return {'applied': applied, 'skipped': skipped, 'sample_log': sample_log}


def gate_check(conflicts, stats, name_map):
    """闸门：dry 阶段全部通过才准真跑。
    Returns (passed: bool, summary: list[str])
    """
    summaries = []
    ok = True
    if conflicts['id_conflicts']:
        ok = False
        summaries.append(f'FAIL id_conflicts={len(conflicts["id_conflicts"])}')
    else:
        summaries.append('PASS id_conflicts=0')
    if conflicts['filename_conflicts']:
        ok = False
        summaries.append(f'FAIL filename_conflicts={len(conflicts["filename_conflicts"])}')
    else:
        summaries.append('PASS filename_conflicts=0')
    if conflicts['l4_l5_invalid']:
        ok = False
        summaries.append(f'FAIL l4_l5_invalid={len(conflicts["l4_l5_invalid"])}')
    else:
        summaries.append('PASS l4_l5_invalid=0')
    if conflicts['missing_industry']:
        ok = False
        summaries.append(f'FAIL missing_industry={len(conflicts["missing_industry"])}')
    else:
        summaries.append('PASS missing_industry=0')
    # 化名全局唯一
    n_cards = stats['planned_n_cards']
    if len(name_map) != n_cards:
        ok = False
        summaries.append(f'FAIL name_map_size={len(name_map)} != n_cards={n_cards}')
    elif len(set(name_map.values())) != n_cards:
        dups = [k for k, v in Counter(name_map.values()).items() if v > 1]
        ok = False
        summaries.append(f'FAIL name_dup_count={len(dups)} sample={dups[:3]}')
    else:
        summaries.append(f'PASS name_unique={n_cards}')
    # multi_name anomalies 允许存在但需记录
    if conflicts['multi_name_anomalies']:
        summaries.append(f'WARN multi_name_anomalies={len(conflicts["multi_name_anomalies"])}')
    return ok, summaries


def main():
    ap = argparse.ArgumentParser(description='merge v3.x archive into letter tree')
    ap.add_argument('--dry', action='store_true', help='dry run: gates + report only')
    ap.add_argument('--limit', type=int, default=None, help='limit number of archive files to process')
    args = ap.parse_args()

    dry = args.dry
    os.makedirs(WORK_DIR, exist_ok=True)
    t0 = time.time()
    ts_iso = datetime.now(timezone.utc).isoformat(timespec='seconds')

    print(f'=== merge_v3_archive.py  dry={dry}  limit={args.limit} ===')
    cards, stats, conflicts, name_map, _name_gen = build_plan(dry=True, limit=args.limit)
    print(f'  scanned_files: {stats["scanned_files"]}')
    print(f'  planned_cards: {stats["planned_cards"]}')
    print(f'    N cards: {stats["planned_n_cards"]}')
    print(f'    P cards: {stats["planned_p_cards"]}')
    print(f'  existing letter tree ids/names: {stats["existing_letter_tree_ids"]}/{stats["existing_letter_tree_names"]}')

    gate_ok, gate_summary = gate_check(conflicts, stats, name_map)
    for s in gate_summary:
        print(f'  gate: {s}')

    # 真跑（仅在闸门通过且非 dry 时）
    applied_stats = {}
    anomalies = []
    if gate_ok and not dry:
        applied_stats = apply_real(cards, anomalies, name_map, ts_iso, dry=False)
        print(f'  applied: {applied_stats["applied"]}, skipped: {applied_stats["skipped"]}')

    # 抽样（dry 模式给出 20 条对照，真跑后取实际应用前 20 条）
    sample_log = []
    if dry:
        sample_log = apply_real(cards, [], name_map, ts_iso, dry=True)['sample_log']
    else:
        sample_log = applied_stats.get('sample_log', [])

    elapsed = time.time() - t0

    # 落盘
    report = {
        'ts': ts_iso,
        'dry': dry,
        'limit': args.limit,
        'stats': stats,
        'gates': gate_summary,
        'gate_passed': gate_ok,
        'conflicts_summary': {
            'id_conflicts': len(conflicts['id_conflicts']),
            'filename_conflicts': len(conflicts['filename_conflicts']),
            'l4_l5_invalid': len(conflicts['l4_l5_invalid']),
            'missing_industry': len(conflicts['missing_industry']),
            'multi_name_anomalies': len(conflicts['multi_name_anomalies']),
        },
        'conflicts_detail': {
            'id_conflicts': conflicts['id_conflicts'][:30],
            'filename_conflicts': conflicts['filename_conflicts'][:30],
            'l4_l5_invalid': conflicts['l4_l5_invalid'][:30],
            'missing_industry': conflicts['missing_industry'][:30],
            'multi_name_anomalies': conflicts['multi_name_anomalies'][:50],
        },
        'sample_log': sample_log,
        'anomalies_during_apply': anomalies[:50],
        'applied_stats': applied_stats,
        'elapsed_sec': round(elapsed, 2),
    }
    with open(REPORT_PATH, 'w', encoding='utf-8') as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2)
    with open(NAME_MAP_PATH, 'w', encoding='utf-8') as fh:
        json.dump(name_map, fh, ensure_ascii=False, indent=2, sort_keys=True)

    print(f'\n=== DONE in {elapsed:.2f}s ===')
    print(f'  report: {REPORT_PATH}')
    print(f'  name_map: {NAME_MAP_PATH}')
    if not gate_ok:
        print('  ! gates FAILED — fix and rerun')
        sys.exit(2)
    if dry:
        print('  (dry run; rerun without --dry to apply)')
    else:
        print(f'  applied {applied_stats.get("applied", 0)} cards')


if __name__ == '__main__':
    main()