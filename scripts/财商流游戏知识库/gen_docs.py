#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""阶段 D：生成 `_框架/` 规范文档（分类体系、字段字典、档案清单映射表）"""
import argparse
import collections
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lib_derive as D  # noqa: E402
from icg_l2 import L2  # noqa: E402
from l1_rules import L1_NAMES  # noqa: E402


def load_classification(d):
    out = {}
    for fn in sorted(os.listdir(d)):
        if not fn.endswith('.jsonl'):
            continue
        for line in open(os.path.join(d, fn), encoding='utf-8'):
            line = line.strip()
            if line:
                o = json.loads(line)
                out[o['file']] = o
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--persons', required=True)
    ap.add_argument('--classified', required=True)
    ap.add_argument('--out', required=True)
    args = ap.parse_args()

    recs = [json.loads(l) for l in open(args.persons, encoding='utf-8')]
    cls = load_classification(args.classified)

    file_count = collections.Counter(r['src_file'] for r in recs)
    # (l1,l2,l3名) → 文件数 / 卡数
    trio = collections.defaultdict(lambda: [set(), 0])
    for f, o in cls.items():
        trio[(o['l1'], o['l2'], o.get('l3') or '其他未归类')][0].add(f)
    for r in recs:
        o = cls.get(r['src_file'])
        if o:
            trio[(o['l1'], o['l2'], o.get('l3') or '其他未归类')][1] += 1

    os.makedirs(args.out, exist_ok=True)
    tax_dir = os.path.join(args.out, '03-行业分类体系')
    os.makedirs(tax_dir, exist_ok=True)

    # ── 03-行业分类体系总览 ──
    lines = ['# 财商流游戏 · 行业分类体系 ICG（Industry Classification for Game）',
             '',
             '> 本文件由 `scripts/财商流游戏知识库/gen_docs.py` 依据 `icg_l2.py` 与全量分类结果自动生成。',
             '> **禁止手工编辑本目录**；修改分类请改 `icg_l2.py` / `work/classified/*.jsonl` 后重跑生成器。',
             '',
             '## 1. 编码规则',
             '',
             '| 层 | 名称 | 格式 | 示例 | 基数 |',
             '|---|---|---|---|---|',
             '| L1 | 行业域 | 单个大写字母 `A`–`Z` | `R` | 26 |',
             '| L2 | 行业细分 | `<L1字母><2位序号>` | `R01` | 239（已用） |',
             '| L3 | 职业族 | `<L2码><2位序号>` | `R0102` | %d |' % len(trio),
             '| L4 | 地区档 | 固定枚举 | `CN-E-华东` | 8 + 兜底 |',
             '| L5 | 年龄段 | 固定枚举 | `45-54` | 6 + 兜底 |',
             '',
             '## 2. 26 个行业域（L1）',
             '',
             '| 码 | 行业域 | L2 数 | L3 族数 | 档案文件数 | 人物卡数 | 细分目录 |',
             '|---|---|---|---|---|---|---|']
    for code in sorted(L1_NAMES):
        l2s = [x for x in trio if x[0] == code]
        n_file = sum(len(trio[x][0]) for x in l2s)
        n_card = sum(trio[x][1] for x in l2s)
        n_l2 = len({x[1] for x in l2s})
        lines.append('| `%s` | %s | %d | %d | %d | %d | [%s-%s](03-行业分类体系/%s-%s.md) |'
                     % (code, L1_NAMES[code], n_l2, len(l2s), n_file, n_card,
                        code, L1_NAMES[code], code, L1_NAMES[code]))
    lines += ['', '## 3. L4 地区档', '',
              '| 码 | 覆盖省份 |', '|---|---|',
              '| `CN-N-华北` | 北京、天津、河北、山西、内蒙古 |',
              '| `CN-NE-东北` | 辽宁、吉林、黑龙江 |',
              '| `CN-E-华东` | 上海、江苏、浙江、安徽、福建、江西、山东 |',
              '| `CN-C-华中` | 河南、湖北、湖南 |',
              '| `CN-S-华南` | 广东、广西、海南 |',
              '| `CN-SW-西南` | 重庆、四川、贵州、云南、西藏 |',
              '| `CN-NW-西北` | 陕西、甘肃、青海、宁夏、新疆 |',
              '| `OV-海外` | 境外（含港澳台） |',
              '| `XX-未知` | 原档未载明城市时的兜底 |',
              '', '## 4. L5 年龄段', '',
              '`16-24` · `25-34` · `35-44` · `45-54` · `55-64` · `65+` · `XX-未知`', '',
              '## 5. 目录路径形如', '',
              '```',
              '<L1码>-<L1名>/<L2码>-<L2名>/<L3码>-<L3名>/<L4地区档>/<L5年龄段>/<编号>-<姓名>.md',
              'O-住宿与餐饮/O01-餐饮门店经营/O0101-餐厅店长/CN-S-华南/35-44/N1034000-陈砚青.md',
              '```', '',
              '## 5.1 v2.0 数字编号（2026-09-13 落地，兼容期与字母并存）', '',
              '人物卡 frontmatter 新增 `occ_id / occ_industry_num / occ_l2_num / occ_l3_num`，',
              '旧 `N<流水>` 进 `_legacy_ids`；目录树 L1/L2/L3 重命名为数字（详见 tmpPlan/...-20260913-01.md）。',
              '字母版与数字版**字段共存**，文件名仍 `N<流水>-<姓名>.md` 不变。',
              '', '']
    with open(os.path.join(args.out, '03-行业分类体系-ICG.md'), 'w', encoding='utf-8') as fh:
        fh.write('\n'.join(lines))

    # ── 每个 L1 一个细分文件 ──
    for code in sorted(L1_NAMES):
        rows = [x for x in trio if x[0] == code]
        if not rows:
            continue
        body = ['# ICG %s · %s' % (code, L1_NAMES[code]), '',
                '> 由 `gen_docs.py` 自动生成，请勿手工编辑。返回 [总览](../03-行业分类体系-ICG.md)。', '']
        by_l2 = collections.defaultdict(list)
        for (l1, l2, l3) in rows:
            by_l2[l2].append(l3)
        for l2 in sorted(by_l2):
            name = L2[code]
            nm = dict(name).get(l2, '')
            n_card = sum(trio[(code, l2, x)][1] for x in by_l2[l2])
            body += ['## `%s` %s' % (l2, nm), '',
                     '共 %d 个职业族、%d 位人物卡。' % (len(by_l2[l2]), n_card), '',
                     '| L3 码 | 职业族 | 档案文件数 | 人物卡数 |', '|---|---|---|---|']
            for l3 in sorted(by_l2[l2]):
                fs, cs = trio[(code, l2, l3)]
                body.append('| `%s%02d` | %s | %d | %d |'
                            % (l2, sorted(by_l2[l2]).index(l3) + 1, l3, len(fs), cs))
            body.append('')
        with open(os.path.join(tax_dir, '%s-%s.md' % (code, L1_NAMES[code])),
                  'w', encoding='utf-8') as fh:
            fh.write('\n'.join(body))

    # ── 07-档案清单映射表（旧文件 → 新路径） ──
    map_dir = os.path.join(args.out, '06-档案清单')
    os.makedirs(map_dir, exist_ok=True)
    files = sorted(cls.keys(), key=lambda f: (int(f.split('-')[0]) if f.split('-')[0].isdigit() else 0, f))
    CHUNK = 700
    idx_lines = ['# 档案清单 · 旧文件 → 新目录 映射表索引', '',
                 '> 由 `gen_docs.py` 自动生成。每行：旧档案文件 → 分类（L1/L2/L3）+ 人物卡数。', '']
    for i in range(0, len(files), CHUNK):
        part = files[i:i + CHUNK]
        n = i // CHUNK + 1
        fn = '映射表-第%02d批.md' % n
        idx_lines.append('- [第%02d批](%s)：`%s` … `%s`（%d 个文件）'
                         % (n, fn, part[0], part[-1], len(part)))
        out = ['# 档案清单映射表 · 第 %02d 批' % n, '',
               '> 由 `gen_docs.py` 自动生成。返回 [索引](../06-档案清单/README.md)。', '',
               '| 旧档案文件 | L1 | L2 | L3 职业族 | 人物卡 | 新目录 |', '|---|---|---|---|---|---|']
        for f in part:
            o = cls[f]
            l2 = o['l2']
            l3 = o.get('l3') or '其他未归类'
            out.append('| `%s` | `%s` | `%s` | %s | %d | `%s-%s/%s-%s/%s-%s/` |'
                       % (f, o['l1'], l2, l3, file_count.get(f, 0),
                          o['l1'], L1_NAMES.get(o['l1'], ''),
                          l2, dict(L2[o['l1']]).get(l2, ''), l3, l3))
        with open(os.path.join(map_dir, fn), 'w', encoding='utf-8') as fh:
            fh.write('\n'.join(out) + '\n')
    with open(os.path.join(map_dir, 'README.md'), 'w', encoding='utf-8') as fh:
        fh.write('\n'.join(idx_lines) + '\n')

    print('生成完成：')
    print('  03-行业分类体系-ICG.md + %d 个域细分文件' % len([c for c in L1_NAMES]))
    print('  06-档案清单/ %d 个映射批次 + README' % ((len(files) + CHUNK - 1) // CHUNK))


if __name__ == '__main__':
    sys.exit(main())
