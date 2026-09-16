#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""run_dim4_v45.py —— v4.5 维度④「家庭生活与消费财商」driver

生成 ≥1,400 张家庭生活与消费财商维度人物卡。
卡号区间 N9030000–N9034999（DIM4 独占），化名取自 work/v45/names_dim4.txt（预分配 2000 名）。

权威规范：docs/财商流游戏/玩家职业设计/_框架/12-多维度人群档案体系_v4.5.md §8。
池数据：scripts/财商流游戏知识库/dim_pools_v45_dim4/（7 L2 × 24 L3）。

用法：
    python3 scripts/财商流游戏知识库/run_dim4_v45.py            # 生成 + 全库校验
    python3 scripts/财商流游戏知识库/run_dim4_v45.py --dry      # 只打印配额，不写盘
    python3 scripts/财商流游戏知识库/run_dim4_v45.py --count=1400
"""
import argparse
import os
import sys

THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)

from lib_dim_v45 import DimGenerator  # noqa: E402
from dim_pools_v45_dim4 import POOLS  # noqa: E402
import verify_dim_v45 as V  # noqa: E402

# ── 维度参数（DIM4 硬编码；改动前请先核对 lib_dim_v45.DIMENSIONS['DIM4']）──
DIM = 'DIM4'
START_ID = 9030000
BATCH_TAG = 'v4.5-dim4'
SEED = 20260916
DEFAULT_COUNT = 1400


def main():
    ap = argparse.ArgumentParser(description='v4.5 维度④「家庭生活与消费财商」driver')
    ap.add_argument('--count', type=int, default=DEFAULT_COUNT, help='计划生成张数（默认 1400）')
    ap.add_argument('--dry', action='store_true', help='只打印配额计划，不写盘')
    ap.add_argument('--skip-verify', action='store_true', help='跳过全库校验（仅开发用）')
    args = ap.parse_args()

    repo_root = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
    out_root = os.path.join(repo_root, 'docs', '财商流游戏', '玩家职业设计')

    # ── 配额预览（dry-run）──
    if args.dry:
        total = 0
        for p in POOLS:
            l2_name = p['l2_name']
            for g in p['l3']:
                w = g.get('weight', 1)
                total += w
        print('[dry-run] 池权重合计=%d；目标 %d 张 → 每权重 ≈ %.2f 张'
              % (total, args.count, args.count / max(total, 1)))
        for p in POOLS:
            for g in p['l3']:
                q = int(round(args.count * g.get('weight', 1) / max(total, 1)))
                print('  %s %s → 约 %d 张（weight=%d, age=%s, gender=%s, marital=%s)' % (
                    g['code'], g['name'], q,
                    g.get('weight', 1), g.get('age_range'), g.get('gender'), g.get('marital_in')))
        return 0

    # ── 生成 ──
    g = DimGenerator(dim=DIM, start_id=START_ID, batch_tag=BATCH_TAG, seed=SEED,
                     out_root=out_root)
    report = g.gen_from_pool(POOLS, count=args.count,
                             report_path=os.path.join(
                                 THIS_DIR, 'work', 'v45',
                                 'report_%s_%s.json' % (DIM.lower(), BATCH_TAG)))

    # ── 全库校验 ──
    if not args.skip_verify:
        print('[verify] 开始 v4.5 全库校验...')
        issues = V.run_full(repo_root=repo_root)
        if issues:
            print('[verify] ⚠ 警告数=%d：' % len(issues))
            for x in issues[:30]:
                print('  -', x)
            if len(issues) > 30:
                print('  ... 共 %d 条（仅显示前 30）' % len(issues))
        else:
            print('[verify] ✅ 全库 v4.5 校验通过（id/name 唯一、字段齐备、YAML 可解析）')

    # ── 报告 ──
    print('\n[report]')
    print('  维度：%s %s' % (report['dimension'], report['dimension_name']))
    print('  计划/实际：%d / %d' % (report['planned'], report['written']))
    print('  卡号区间：%s → %s（next=%s）' % (
        report['id_range'][0], report['id_range'][1], report['id_next']))
    print('  用时：%.1fs' % report['elapsed_sec'])
    print('  每 L3 分布：')
    for code, n in sorted(report['per_l3'].items()):
        print('    %s: %d' % (code, n))
    print('  报告文件：work/v45/report_%s_%s.json' % (DIM.lower(), BATCH_TAG))
    return 0


if __name__ == '__main__':
    sys.exit(main())
