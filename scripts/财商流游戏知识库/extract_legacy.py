#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""财商流游戏 · 玩家职业设计知识库 —— 旧档案解析器（阶段 A）

把 `lag_docs/财商流游戏/玩家职业设计/*.md`（6,503 个扁平文件）解析为统一的
人物记录 JSONL，供后续 分类(ICG) → 列解析 → 推导 → 渲染 使用。

用法:
    python3 extract_legacy.py --src <旧目录> --out work/records.jsonl
"""
import argparse
import json
import os
import re
import sys
from collections import Counter

# ── 表头指纹 → 列语义映射 ─────────────────────────────────────────
# 标准 8/9 列表头
STD_HEADER = ['编号', '姓名 · 年龄/身体', '城市与住房', '家庭/抚养',
              '职业 · 月收入', '支出/储蓄/负债', '资产与保障', '财商画像',
              '目标与开局事件']

ROW_RE = re.compile(r'^\|\s*([PN]\d{1,8})\s*\|(.*)\|\s*$')
HDR_RE = re.compile(r'^\|\s*编号\s*\|')
DETAIL_RE = re.compile(
    r'^(#{2,3})\s*([PN]\d{1,8})\s*·\s*([^·|]+?)\s*·\s*([^·（(]+?)\s*·\s*(\d{1,3})\s*岁\s*(?:[（(]([^）)]*)[）)])?\s*$',
    re.M)

SUB_RE = re.compile(r'^####\s*(.+?)\s*$', re.M)
BULLET_RE = re.compile(r'^-\s*\*\*(.+?)\*\*\s*[：:]\s*(.*)$', re.M)

# 各详档小节名 → 语义键
SUB_KEYS = {
    '基础档案': 'base',
    '家族背景': 'family',
    '收入结构': 'income',
    '收入与财务': 'income',
    '支出与储蓄': 'expense',
    '资产与负债': 'asset',
    '健康与保障': 'health',
    '健康与职业风险': 'health',
    '工作强度与风险': 'work',
    '职业特征': 'work',
    '个人特质': 'trait',
    '心理张力': 'tension',
    '行为金融偏差': 'bias',
    '决策倾向': 'decision',
    '财商画像与决策倾向': 'decision',
    '人生目标': 'goal',
    '人生目标与开局事件': 'goal',
    '开局钩子': 'hook',
    '开局事件': 'hook',
    '负债与资金链': 'debt',
}


def split_row(line):
    """把一行表格拆成单元格列表（去掉首尾的 | ）。"""
    body = line.strip()
    if body.startswith('|'):
        body = body[1:]
    if body.endswith('|'):
        body = body[:-1]
    return [c.strip() for c in body.split('|')]


def parse_detail_sections(text):
    """解析全部 `### ID · 姓名 · 性别 · 年龄 岁（职业）` 详档节。

    返回 {id: {'name','gender','age','occupation','subs': {key: [(标签, 值)]}, 'raw': str}}
    """
    out = {}
    marks = []
    for m in SUB_RE.finditer(text):
        pass
    # 先定位所有详档标题
    heads = list(DETAIL_RE.finditer(text))
    if not heads:
        return out
    # 所有 #### 小节的位置
    subs = list(SUB_RE.finditer(text))
    for h in heads:
        cid = h.group(2)
        start = h.start()
        # 该详档节的结束位置 = 下一个同类或更高级标题
        nxt = len(text)
        for h2 in heads:
            if h2.start() > start:
                nxt = min(nxt, h2.start())
                break
        for s in subs:
            if s.start() > start:
                nxt = min(nxt, s.start())
                break
        # 实际上 #### 属于本节，需要取到下一个 ### 为止
        section_end = len(text)
        for h2 in heads:
            if h2.start() > start:
                section_end = h2.start()
                break
        raw = text[start:section_end].strip()
        # 解析本节的 #### 小节
        sec_subs = [s for s in subs if start < s.start() < section_end]
        submap = {}
        for i, s in enumerate(sec_subs):
            e = sec_subs[i + 1].start() if i + 1 < len(sec_subs) else section_end
            body = text[s.end():e].strip()
            key = SUB_KEYS.get(s.group(1).strip())
            if not key:
                key = 'other:' + s.group(1).strip()
            items = [(b.group(1).strip(), b.group(2).strip()) for b in BULLET_RE.finditer(body)]
            submap[key] = {'title': s.group(1).strip(), 'items': items, 'text': body}
        # 标题前的引言（> **职业卡** / > **开局画像**）
        intro_end = sec_subs[0].start() if sec_subs else section_end
        intro = text[h.end():intro_end].strip()
        out[cid] = {
            'id': cid,
            'name': h.group(3).strip(),
            'gender': h.group(4).strip(),
            'age': int(h.group(5)),
            'occupation': (h.group(6) or '').strip(),
            'subs': submap,
            'intro': intro,
            'raw': raw,
            'head_level': len(h.group(1)),
        }
    return out


def parse_file(path):
    """解析单个旧档案文件 → (records, meta)"""
    with open(path, encoding='utf-8', errors='replace') as fh:
        text = fh.read()
    if text.startswith('﻿'):
        text = text[1:]
    fname = os.path.basename(path)

    header = ''
    for line in text.splitlines():
        if HDR_RE.match(line):
            header = line.strip()
            break

    rows = []
    for line in text.splitlines():
        m = ROW_RE.match(line)
        if m:
            rows.append(split_row(line))

    recs = []
    details = parse_detail_sections(text)

    # 1) 表格行
    seen_ids = set()
    for cells in rows:
        if len(cells) < 2:
            continue
        cid = cells[0]
        if cid in seen_ids:
            continue
        seen_ids.add(cid)
        recs.append({
            'id': cid,
            'src_file': fname,
            'src_kind': 'table',
            'src_line': '| ' + ' | '.join(cells) + ' |',
            'header': header,
            'cells': cells,
            'detail': None,
        })

    # 2) 详档节：并入对应记录（以详档为准），并补充无表格行的记录
    by_id = {r['id']: r for r in recs}
    for cid, d in details.items():
        if cid in by_id:
            by_id[cid]['detail'] = d
            by_id[cid]['src_kind'] = 'table+detail'
        else:
            recs.append({
                'id': cid,
                'src_file': fname,
                'src_kind': 'detail',
                'src_line': d['raw'],
                'header': header,
                'cells': [],
                'detail': d,
            })

    return recs, {'file': fname, 'rows': len(rows), 'details': len(details),
                  'header': header, 'has_detail': bool(details)}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--src', required=True)
    ap.add_argument('--out', required=True)
    args = ap.parse_args()

    files = sorted(f for f in os.listdir(args.src) if f.endswith('.md'))
    all_recs = []
    metas = []
    for fn in files:
        recs, meta = parse_file(os.path.join(args.src, fn))
        all_recs.extend(recs)
        metas.append(meta)

    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    with open(args.out, 'w', encoding='utf-8') as fh:
        for r in all_recs:
            fh.write(json.dumps(r, ensure_ascii=False) + '\n')

    # ── 统计 ──
    ids = Counter(r['id'] for r in all_recs)
    names = Counter(r['detail']['name'] if r['detail'] else
                    (re.split(r'[·・]', r['cells'][1])[0].strip() if len(r['cells']) > 1 else '')
                    for r in all_recs)
    kind = Counter(r['src_kind'] for r in all_recs)
    with_detail = sum(1 for r in all_recs if r['detail'])
    hdr = Counter(m['header'] for m in metas)

    print('文件数           :', len(files))
    print('人物记录数       :', len(all_recs))
    print('唯一编号数       :', len(ids))
    print('重号编号数       :', sum(1 for v in ids.values() if v > 1))
    print('重名姓名数       :', sum(1 for v in names.values() if v > 1))
    print('记录来源分布     :', dict(kind))
    print('含详档记录数     :', with_detail)
    print('表头种类数       :', len(hdr))
    for h, c in hdr.most_common(8):
        print('   %5d  %s' % (c, h[:100]))
    bad = [m for m in metas if not m['rows'] and not m['details']]
    print('无人物文件数     :', len(bad))
    for m in bad:
        print('   -', m['file'])


if __name__ == '__main__':
    sys.exit(main())
