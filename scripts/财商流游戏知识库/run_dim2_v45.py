#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""run_dim2_v45.py —— v4.5 维度②「身份与就业状态」driver

生成 ≥1,500 张维度②人物卡（在校学生/退休/失业/零工/全职照料/创业者）。
起 ID = N9020000；化名块 work/v45/names_dim2.txt（2,500 名已预分配）。
"""
import os, sys
THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)

from lib_dim_v45 import DimGenerator
from dim_pools_v45_dim2 import POOLS
import verify_dim_v45 as V


def main():
    dim = 'DIM2'
    start_id = 9020000
    batch_tag = 'v4.5-dim2'
    seed = 20260916
    total = 1500

    repo_root = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
    out_root = os.path.join(repo_root, 'docs', '财商流游戏', '玩家职业设计')

    g = DimGenerator(dim=dim, start_id=start_id, batch_tag=batch_tag, seed=seed,
                     out_root=out_root)
    report = g.gen_from_pool(POOLS, count=total,
                             report_path=os.path.join(
                                 THIS_DIR, 'work', 'v45',
                                 'report_%s_%s.json' % (dim.lower(), batch_tag)))
    # 全库校验
    issues = V.run_full(repo_root=repo_root)
    if issues:
        print('[verify] 警告数=%d：' % len(issues))
        for x in issues[:20]:
            print('  -', x)
    else:
        print('[verify] 全库 v4.5 校验通过（id/name 唯一、字段齐备、YAML 可解析）')
    return report


if __name__ == '__main__':
    main()
