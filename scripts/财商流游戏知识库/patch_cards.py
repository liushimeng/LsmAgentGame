#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""阶段 F：就地修补已落盘人物卡（保留富化成果）

用法:
    python3 patch_cards.py --persons work/persons.jsonl \
                           --classified work/classified \
                           --dest <玩家职业设计目录> [--only-paths <file>]

修补范围（**只动 frontmatter + 正文 §1–§5、§9**，§6/§7/§8 与 `_rich` 原样保留）：
  · 储蓄/支出/负债的「万元」单位换算修正
  · 子女解析修正（`1子5岁` 形态）
  · `mortgage_left` / `children_ages` 的「不适用」语义修正
  · 随之重算 `_sources` / `_completeness` / `_grounded`
"""
import argparse
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import yaml  # noqa: E402
import build_cards as B  # noqa: E402
import lib_derive as D  # noqa: E402
from icg_l2 import l2_index, L2  # noqa: E402
from l1_rules import L1_NAMES  # noqa: E402

TAIL_HEAD = '## 6. 情感与人格'


def split_card(text):
    """→ (frontmatter_str, body_str)"""
    i = text.index('\n---\n', 4)
    return text[4:i], text[i + 5:]


def preserve_tail(body):
    """取正文 §6 起的全部内容（富化区），供原样回填。"""
    j = body.find(TAIL_HEAD)
    return body[j:] if j >= 0 else None


def limit_fields(card, rec):
    """「不适用」语义修正 + 修正后的来源标注。"""
    ch = rec['city_housing']
    tenure = card.get('housing_tenure')
    if card.get('mortgage_left') is None and tenure in (
            '租赁', '合租', '单位/保障住房', '父母产权', '自建'):
        card['mortgage_left'] = 0
        card['_src_mortgage'] = 'derived(housing_tenure)'
    elif card.get('mortgage_left') is None:
        card['_src_mortgage'] = 'missing'
    else:
        card['_src_mortgage'] = 'explicit'

    if card.get('children_count'):
        card['_src_children_ages'] = 'explicit' if card.get('children_ages') else 'missing'
    else:
        card['_src_children_ages'] = 'derived(no_children)'
    return card


NOT_APPLICABLE = {'mortgage_left': ('租赁', '合租', '单位/保障住房', '父母产权', '自建')}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--persons', required=True)
    ap.add_argument('--classified', required=True)
    ap.add_argument('--dest', required=True)
    ap.add_argument('--only-paths', default=None,
                    help='只修补该清单内的文件（用于分批/试跑）')
    ap.add_argument('--dry', action='store_true')
    args = ap.parse_args()

    recs = [json.loads(l) for l in open(args.persons, encoding='utf-8')]
    cls = B.load_classification(args.classified)
    l3idx = B.build_l3_index(cls)

    for r in recs:
        r['_gender'] = B.derive_gender_safe(r)[0]
    B.resolve_ids(recs)
    B.resolve_names(recs, B.build_occ_substrings(recs))

    # 现有卡片按卡号索引（文件名可能因姓名解析修正而变化，故按 id 定位）
    import glob
    index = {}
    for p_ in glob.glob(os.path.join(args.dest, '*', '*', '*', '*', '*', '*.md')):
        base = os.path.basename(p_)
        cid = base.split('-', 1)[0]
        index[cid] = p_
    print('现有卡片索引:', len(index))

    only = None
    if args.only_paths:
        only = set(l.strip() for l in open(args.only_paths, encoding='utf-8') if l.strip())

    stats = {'patched': 0, 'skipped': 0, 'missing': 0, 'rich_kept': 0, 'renamed': 0}
    for r in recs:
        card, pi = B.build(r, cls, l3idx)
        limit_fields(card, r)
        # 重算来源统计（把「不适用」字段计入 derived）
        src = card['_sources']
        remap = {'mortgage_left': card.pop('_src_mortgage'),
                 'children_ages': card.pop('_src_children_ages')}
        for f, s in remap.items():
            for bucket in ('explicit', 'derived', 'assigned', 'missing'):
                if f in src[bucket]:
                    src[bucket].remove(f)
            if s == 'missing':
                src['missing'].append(f)
            elif s == 'explicit':
                src['explicit'].append(f)
            else:
                src['derived'].append(f)
        card['_source_counts'] = {k: len(v) for k, v in src.items()}
        card['_completeness'] = round(
            (len(D.COUNTED_FIELDS) - len(src['missing'])) / len(D.COUNTED_FIELDS), 3)
        card['_grounded'] = round(
            (len(src['explicit']) + 0) / len(D.COUNTED_FIELDS), 3)

        fn = '%s-%s.md' % (card['id'], re.sub(r'[/\\]', '_', r['_new_name'] or card['id']))
        d = os.path.join(args.dest,
                         '%s-%s' % (pi['l1'], B._seg(L1_NAMES.get(pi['l1'], ''))),
                         '%s-%s' % (pi['l2'], B._seg(l2_index().get(pi['l2'], (None, ''))[1])),
                         '%s-%s' % (pi['l3'], B._seg(pi['l3name'])),
                         B._seg(pi['region']), B._seg(pi['age_band']))
        path = os.path.join(d, fn)
        cur = index.get(card['id'])
        if cur is None:
            stats['missing'] += 1
            continue
        if only is not None and cur not in only:
            continue
        old = open(cur, encoding='utf-8').read()
        old_fm, old_body = split_card(old)
        tail = preserve_tail(old_body)
        is_rich = 'richness: rich' in old_fm
        if is_rich:
            card['richness'] = 'rich'
            stats['rich_kept'] += 1
        new_fm = B.yaml_dump({k: v for k, v in card.items() if not k.startswith('_')})
        prov = {
            '_legacy_ids': card['_legacy_ids'],
            '_completeness': card['_completeness'],
            '_grounded': card['_grounded'],
            '_sources': card['_source_counts'],
            '_missing': src['missing'],
            '_raw': card['_raw'],
        }
        if is_rich:
            m = re.search(r'^_rich:\n(?:[ \t].*\n?)*', old_fm, re.M)
            if m:
                prov['_rich'] = yaml.safe_load(m.group(0))['_rich']
        new_prov = B.yaml_dump(prov)
        head_body = B.render_body(card, pi)
        if tail:
            head_body = head_body[:head_body.find(TAIL_HEAD)] + tail
        text = '---\n' + new_fm + '\n' + new_prov + '\n---\n\n' + head_body
        if not args.dry:
            os.makedirs(d, exist_ok=True)
            with open(path, 'w', encoding='utf-8') as fh:
                fh.write(text)
            if os.path.abspath(cur) != os.path.abspath(path):
                os.remove(cur)
                stats['renamed'] += 1
        stats['patched'] += 1

    print('修补:', stats)


if __name__ == '__main__':
    sys.exit(main())
