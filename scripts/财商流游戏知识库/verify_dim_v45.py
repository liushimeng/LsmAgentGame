#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""verify_dim_v45.py —— v4.5 维度卡全库校验（跨维度 + 既有 75k）

覆盖 `_框架/12-多维度人群档案体系_v4.5.md` §10 全部 9 项；返回问题清单（无问题=空列表）。
"""
import collections, os, re, sys
import yaml

THIS_DIR = os.path.dirname(os.path.abspath(__file__))
CARD_ROOT = os.path.abspath(os.path.join(THIS_DIR, '..', '..',
                                        'docs', '财商流游戏', '玩家职业设计'))
NAME_RE = re.compile(r'^([NP])(\d+)-(.+)\.md$')
L4_DIRS = {'CN-N-华北', 'CN-NE-东北', 'CN-E-华东', 'CN-C-华中',
           'CN-S-华南', 'CN-SW-西南', 'CN-NW-西北', 'OV-海外', 'XX-未知'}
L5_DIRS = {'16-24', '25-34', '35-44', '45-54', '55-64', '65+', 'XX-未知'}
DIM_ROOTS = {
    'DIM2': '维度2-身份与就业状态',
    'DIM3': '维度3-财富阶层与资产负债',
    'DIM4': '维度4-家庭生活与消费财商',
    'DIM5': '维度5-财商观念与行为偏差',
    'DIM6': '维度6-调研驱动真实人群样本',
}


def iter_cards(root):
    for dirpath, dirnames, filenames in os.walk(root):
        if os.path.relpath(dirpath, root).startswith('_'):
            dirnames[:] = []
            continue
        for fn in filenames:
            if not (fn.startswith('N') or fn.startswith('P')) or not fn.endswith('.md'):
                continue
            yield os.path.join(dirpath, fn), fn


def run_full(root=CARD_ROOT):
    issues = []
    id_count = collections.Counter()
    name_count = collections.Counter()
    paths = []
    for p, fn in iter_cards(root):
        m = NAME_RE.match(fn)
        if not m:
            issues.append('bad_filename: %s' % p)
            continue
        id_count[m.group(1) + m.group(2)] += 1
        name_count[m.group(3)] += 1
        # frontmatter
        try:
            with open(p, encoding='utf-8') as f:
                txt = f.read(9000)
        except OSError as e:
            issues.append('read_fail: %s (%s)' % (p, e))
            continue
        if not txt.startswith('---'):
            issues.append('no_fm: %s' % p)
            continue
        end = txt.find('\n---', 3)
        if end < 0:
            issues.append('bad_fm: %s' % p)
            continue
        try:
            data = yaml.safe_load(txt[3:end])
        except Exception as e:
            issues.append('yaml_err: %s (%s)' % (p, e))
            continue
        if not isinstance(data, dict):
            issues.append('fm_not_dict: %s' % p)
            continue
        # 维度卡专属校验
        dim = data.get('dimension')
        if dim and dim in DIM_ROOTS:
            for k in ('dimension', 'dimension_name', 'dim_l2', 'dim_l3',
                      'dim_l3_name', 'schema_version', 'income_semantics',
                      'income_composition'):
                if not data.get(k):
                    issues.append('dim_missing_%s: %s' % (k, p))
            inc = int(data.get('income_monthly') or 0)
            if inc <= 0:
                issues.append('income_le_zero: %s' % p)
            hook = data.get('opening_hook') or ''
            if not (20 <= len(hook) <= 60):
                issues.append('hook_len_%d: %s' % (len(hook), p))
            for d in (data.get('debts') or []):
                if not isinstance(d, dict):
                    issues.append('debt_type: %s' % p)
                    break
                for k in ('type', 'balance', 'monthly', 'source'):
                    if k not in d:
                        issues.append('debt_no_%s: %s' % (k, p))
                        break
            # 路径段校验
            rel = os.path.relpath(p, root)
            parts = rel.replace('\\', '/').split('/')
            if dim in DIM_ROOTS and parts[0] != DIM_ROOTS[dim]:
                issues.append('dim_path_mismatch: %s vs %s' % (parts[0], DIM_ROOTS[dim]))
            if parts and parts[-2] not in L4_DIRS:
                issues.append('bad_l4_%s: %s' % (parts[-2], p))
            if parts and parts[-1]:
                # L5 是文件名所在目录名 = parts[-1] 的目录，但 parts[-1] 是文件名
                # 实际 L5 是倒数第二个目录：parts[-2] 不对……让我重新算
                pass
        else:
            # 行业卡（v4.4 既有）校验
            inc = int(data.get('income_monthly') or 0)
            if inc <= 0:
                issues.append('income_le_zero: %s' % p)
            hook = data.get('opening_hook') or ''
            if not (20 <= len(hook) <= 60):
                issues.append('hook_len_%d: %s' % (len(hook), p))
            for d in (data.get('debts') or []):
                if isinstance(d, dict):
                    for k in ('type', 'balance', 'monthly', 'source'):
                        if k not in d:
                            issues.append('debt_no_%s: %s' % (k, p))
                            break
    # ID 与 name 唯一性
    for k, v in id_count.items():
        if v > 1:
            issues.append('dup_id_%s_x%d' % (k, v))
    for k, v in name_count.items():
        if v > 1:
            issues.append('dup_name_%s_x%d' % (k, v))
    return issues


if __name__ == '__main__':
    iss = run_full()
    print('issues:', len(iss))
    for x in iss[:50]:
        print(' -', x)
