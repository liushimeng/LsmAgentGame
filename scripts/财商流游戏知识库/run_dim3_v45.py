#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""run_dim3_v45.py —— v4.5 维度③「财富阶层与资产负债」driver

生成入口：
  python3 scripts/财商流游戏知识库/run_dim3_v45.py

维度代码：DIM3
卡号区间：N9025000 – N9029999
化名块：work/v45/names_dim3.txt（2000 名已预分配）
目标张数：≥1,400（按权重自动分配，不足时补到最重组）
batch_tag：v4.5-dim3
seed：20260916（团队统一）
"""
import os
import sys

THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)

from lib_dim_v45 import DimGenerator
from dim_pools_v45_dim3 import POOLS
import verify_dim_v45 as V


def main():
    dim = 'DIM3'
    start_id = 9025000
    batch_tag = 'v4.5-dim3'
    seed = 20260916
    total = 1400  # 维度③目标张数（≥1400）

    repo_root = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
    out_root = os.path.join(repo_root, 'docs', '财商流游戏', '玩家职业设计')

    g = DimGenerator(dim=dim, start_id=start_id, batch_tag=batch_tag, seed=seed,
                     out_root=out_root)
    report = g.gen_from_pool(
        POOLS, count=total,
        report_path=os.path.join(THIS_DIR, 'work', 'v45',
                                 'report_%s_%s.json' % (dim.lower(), batch_tag)))

    # ── 全库校验（v4.5 全部维度一起跑）──────────────────────
    issues = V.run_full(repo_root=repo_root)
    if issues:
        print('[verify] 警告数=%d：' % len(issues))
        for x in issues[:30]:
            print('  -', x)
    else:
        print('[verify] 全库 v4.5 校验通过（id/name 唯一、字段齐备、YAML 可解析）')

    return report


if __name__ == '__main__':
    main()
