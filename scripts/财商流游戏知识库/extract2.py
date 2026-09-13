#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""阶段 A2：槽位感知解析 —— 旧档案 → work/persons.jsonl（结构化人物记录）

在 extract_legacy.py 的基础上引入「表头指纹 → 槽位」映射，
把 10 种表头变体统一成同一套语义槽位，再拆解备注串。
"""
import argparse
import json
import os
import re
import sys
from collections import Counter, defaultdict

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lib_cells as L  # noqa: E402
from extract_legacy import parse_detail_sections, split_row, ROW_RE, HDR_RE  # noqa: E402


def parse_file(path):
    with open(path, encoding='utf-8', errors='replace') as fh:
        text = fh.read()
    if text.startswith('﻿'):
        text = text[1:]
    fname = os.path.basename(path)

    header_line, slots = '', None
    for line in text.splitlines():
        if HDR_RE.match(line):
            header_line = line.strip()
            slots = L.lookup_slots(header_line)
            break

    details = parse_detail_sections(text)
    recs = []
    seen = set()
    # 只取「紧跟表头的那一个表格块」，避免把文末的对比/去重表当成人物卡
    lines = text.splitlines()
    hdr_idx = None
    for i, line in enumerate(lines):
        if HDR_RE.match(line):
            hdr_idx = i
            break
    if hdr_idx is None:
        block = []
    else:
        block = []
        for line in lines[hdr_idx + 1:]:
            if ROW_RE.match(line):
                block.append(line)
            elif line.strip().startswith('|---') or line.strip().startswith('| ---'):
                continue
            elif not line.strip():
                if block:
                    break
                continue
            else:
                break
    for line in block:
        m = ROW_RE.match(line)
        if not m:
            continue
        cid = m.group(1)
        if cid in seen:
            continue
        seen.add(cid)
        cells = split_row(line)
        if len(cells) < 2:
            continue
        body = cells[1:]
        slotmap = {}
        if slots:
            for i, s in enumerate(slots):
                if i < len(body):
                    slotmap[s] = body[i]
        else:
            slotmap = {'_unmapped%s' % i: c for i, c in enumerate(body)}
        recs.append({
            'id': cid,
            'src_file': fname,
            'src_header': header_line,
            'src_line': line.strip(),
            'slots': slotmap,
            'cells': cells,
            'detail': details.get(cid),
        })
    for cid, d in details.items():
        if cid in seen:
            for r in recs:
                if r['id'] == cid:
                    pass
            continue
        recs.append({
            'id': cid, 'src_file': fname, 'src_header': header_line,
            'src_line': d['raw'], 'slots': {}, 'cells': [], 'detail': d,
        })
    # 把 detail 挂回（前面是按 id 查的，这里保证一致）
    for r in recs:
        if r['detail'] is None and r['id'] in details:
            r['detail'] = details[r['id']]
    return recs, header_line, slots


def build(path):
    recs, header, slots = parse_file(path)
    out = []
    for r in recs:
        s = r['slots']
        p = {'id': r['id'], 'src_file': r['src_file'], 'src_line': r['src_line'],
             'src_header': r['src_header'], 'slots': s}

        nab = s.get(L.S_NAME_AGE_BODY, '')
        if nab:
            p.update(L.parse_name_age_body(nab))
        # 变体：姓名/年龄/性别 分列
        if L.S_PERSON in s:
            pp = L.parse_person(s[L.S_PERSON])
            p.setdefault('name', pp['name'])
            p['name'] = pp['name'] or p.get('name', '')
            if pp['age']:
                p['age'] = pp['age']
            if pp['gender']:
                p['gender'] = pp['gender']
        if L.S_NAME in s and not p.get('name'):
            p['name'] = s[L.S_NAME].strip()
        if L.S_AGE in s and not p.get('age'):
            m = re.search(r'\d{1,3}', s[L.S_AGE])
            p['age'] = int(m.group(0)) if m else None
        p.setdefault('name', '')
        p.setdefault('age', None)
        p.setdefault('gender', None)

        p['city_housing'] = L.parse_city_housing(s.get(L.S_CITY_HOUSING) or s.get(L.S_CITY, ''))
        p['family'] = L.parse_family(s.get(L.S_FAMILY, ''))
        ci = s.get(L.S_CAREER_INCOME, '')
        if L.S_OCCUPATION in s and L.S_CAREER_INCOME not in s:
            ci = s[L.S_OCCUPATION] + (',' + s[L.S_SALARY] if s.get(L.S_SALARY) else '')
        p['career'] = L.parse_career_income(ci)
        if L.S_SALARY in s and p['career']['income_monthly'] is None:
            m = re.search(r'([\d,]{3,9})', s[L.S_SALARY])
            if m:
                p['career']['income_monthly'] = int(m.group(1).replace(',', ''))
        if L.S_INCOME_ANCHOR in s and not p['career']['occupation']:
            p['career']['occupation'] = s.get(L.S_OCCUPATION, '')
        p['finance'] = L.parse_finance(s.get(L.S_FINANCE, ''))
        p['asset'] = L.parse_asset_protection(s.get(L.S_ASSET_PROTECTION, ''))
        p['profile'] = L.parse_profile(s.get(L.S_PROFILE, ''))
        p['goal_event'] = L.parse_goal_event(s.get(L.S_GOAL_EVENT, ''))
        p['tension'] = s.get(L.S_TENSION, '')
        p['family_body_asset'] = s.get(L.S_FAMILY_BODY_ASSET, '')
        p['misc'] = s.get(L.S_MISC, '')

        # ── 打包列回填（变体表头） ──
        mix_raw = s.get(L.S_FAMILY_BODY_ASSET, '')
        if mix_raw:
            mix = L.parse_mixed(mix_raw)
            if not p['family']['marital']:
                p['family']['marital'] = mix['marital']
            if not p['family']['children']:
                p['family']['children'] = mix['children']
            p['family']['raw'] = p['family']['raw'] or mix_raw
            if not p.get('health_grade'):
                p['health_grade'] = mix['health_grade']
            if not p.get('conditions'):
                p['conditions'] = mix['conditions']
            if not p['finance'].get('savings'):
                p['finance']['savings'] = mix['savings']
            if not p['finance'].get('debt'):
                p['finance']['debt'] = mix['debt']
            if not p['finance'].get('expense'):
                p['finance']['expense'] = mix['expense']
            if not p['asset'].get('education'):
                p['asset']['education'] = mix['education']
            for a in mix['assets']:
                if a not in p['asset']['assets']:
                    p['asset']['assets'].append(a)
            for i in mix['insurance']:
                if i not in p['asset']['insurance']:
                    p['asset']['insurance'].append(i)
        # 资产(万元) 列：负数=负债
        aw = s.get(L.S_ASSET_WAN, '')
        if aw:
            av, dv = L.parse_asset_wan(aw)
            if av is not None:
                p['asset']['assets'].append('%d万元资产' % (av // 10000))
            if dv is not None and not p['finance'].get('debt'):
                p['finance']['debt'] = dv
        if s.get(L.S_TENSION, '') and not p['profile']['items']:
            p['profile'] = L.parse_profile(s[L.S_TENSION])
        p['detail'] = r['detail']

        # ── 详档节优先补充（P01–P18 及 129 个详档文件） ──
        d = r['detail']
        if d:
            p['name'] = d['name'] or p.get('name', '')
            p['gender'] = d.get('gender') or p.get('gender')
            p['age'] = d.get('age') or p.get('age')
            if d.get('occupation'):
                p['career']['occupation'] = d['occupation']
            p['detail_subs'] = {k: v['items'] for k, v in d['subs'].items()}
            p['detail_intro'] = d['intro']
        else:
            p['detail_subs'] = {}
            p['detail_intro'] = ''
        out.append(p)
    return out, header, slots


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--src', required=True)
    ap.add_argument('--out', required=True)
    args = ap.parse_args()

    # 非人物档案（框架/索引/数据锚点/交付说明）不产出人物卡
    SKIP = {'1510-研究来源与数据锚点.md'}
    files = sorted(f for f in os.listdir(args.src)
                   if f.endswith('.md') and f not in SKIP
                   and not f.startswith('交付说明'))
    all_recs, unknown = [], Counter()
    for fn in files:
        recs, header, slots = build(os.path.join(args.src, fn))
        if recs and slots is None:
            unknown[header[:90]] += 1
        all_recs.extend(recs)

    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    with open(args.out, 'w', encoding='utf-8') as fh:
        for r in all_recs:
            fh.write(json.dumps(r, ensure_ascii=False) + '\n')

    print('记录数:', len(all_recs))
    print('未识别表头（文件数）:', dict(unknown))
    no_name = [r for r in all_recs if not r.get('name')]
    print('无名记录:', len(no_name))
    for r in no_name[:10]:
        print('   ', r['id'], r['src_file'][:40], repr(r['src_line'][:90]))
    no_age = sum(1 for r in all_recs if not r.get('age'))
    print('无年龄记录:', no_age)
    print('有职业列:', sum(1 for r in all_recs if r['career']['occupation']))
    print('有收入:', sum(1 for r in all_recs if r['career']['income_monthly']))
    print('有城市:', sum(1 for r in all_recs if r['city_housing']['city']))
    print('有性别:', sum(1 for r in all_recs if r.get('gender')))
    print('有详档:', sum(1 for r in all_recs if r['detail']))
    occs = Counter(r['career']['occupation'] for r in all_recs if r['career']['occupation'])
    print('职业字符串去重数:', len(occs))
    with open(os.path.join(os.path.dirname(args.out), 'occupations.txt'), 'w',
              encoding='utf-8') as fh:
        for o, c in occs.most_common():
            fh.write('%s\t%d\n' % (o, c))


if __name__ == '__main__':
    sys.exit(main())
