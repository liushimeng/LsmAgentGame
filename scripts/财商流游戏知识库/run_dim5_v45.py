#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""run_dim5_v45.py —— v4.5 维度⑤ driver"""
import os, sys
THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)
from lib_dim_v45 import DimGenerator
import verify_dim_v45 as V

POOLS = __import__('dim_pools_v45_dim5', fromlist=['POOLS']).POOLS

def main():
    repo_root = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
    out_root = os.path.join(repo_root, 'docs', '财商流游戏', '玩家职业设计')
    g = DimGenerator(dim='DIM5', start_id=9035000, batch_tag='v4.5-dim5', seed=20260916, out_root=out_root)
    report = g.gen_from_pool(POOLS, count=1200,
                             report_path=os.path.join(THIS_DIR, 'work', 'v45', 'report_dim5_v4.5-dim5.json'))
    print('[verify] 开始...')
    issues = V.run_full()
    print('issues:', len(issues))
    for x in issues[:20]: print(' -', x)

if __name__ == '__main__':
    main()
