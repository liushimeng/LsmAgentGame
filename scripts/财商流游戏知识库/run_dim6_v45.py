#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""run_dim6_v45.py —— v4.5 维度⑥ driver"""
import os, sys
THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)
from lib_dim_v45 import DimGenerator

def main():
    repo_root = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
    out_root = os.path.join(repo_root, 'docs', '财商流游戏', '玩家职业设计')
    g = DimGenerator(dim='DIM6', start_id=9040000, batch_tag='v4.5-dim6', seed=20260916, out_root=out_root)
    report = g.gen_from_pool(__import__('dim_pools_v45_dim6', fromlist=['POOLS']).POOLS, count=1000,
                             report_path=os.path.join(THIS_DIR, 'work', 'v45', 'report_dim6_v4.5-dim6.json'))
    print('DIM6 done:', report['written'])

if __name__ == '__main__':
    main()
