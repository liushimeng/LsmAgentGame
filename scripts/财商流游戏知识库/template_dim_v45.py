#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""template_dim_v45.py —— v4.5 维度卡 driver 模板（直接拷贝 → 修改）

新增维度 driver **必须** 按本模板结构编写：
  1. 定义 POOLS 列表（维度池数据，按 §6 规范）
  2. 调 DimGenerator.gen_from_pool 生成
  3. 调 verify_dim_v45.run 跑全库校验
  4. 写一份交付说明到 _交付说明/

详细字段含义见 `_框架/12-多维度人群档案体系_v4.5.md` §6。
"""
import os, sys
THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)
from lib_dim_v45 import DimGenerator
import verify_dim_v45 as V


# ── 1. 维度池（按 §6 规范填充；每 L3 组至少 1 个 occupations 实例）────
POOLS = [
    {
        'l2': 'DIMX-01', 'l2_name': '示例细分（请改成你的细分名）',
        'l3': [
            {
                'code': 'DIMX-0101', 'name': '示例人群族',
                'industry': None,                  # None=无行业归属；否则('T','T01')
                'age_range': (20, 35),             # 人群年龄约束（必填）
                'gender': None,                     # '男'/'女'/None
                'marital_in': ['未婚', '已婚'],
                'weight': 2,                        # 配额权重（默认1）
                'tag': '示例人群族',
                'note': '示例人群族',
                'income_composition': [             # 覆盖默认（按就业形态推导）
                    ('工资性收入', 0.80),
                    ('财产性收入', 0.20),
                ],
                'finance': {                        # 维度③必备，其余可选
                    'savings_range': (10000, 50000),
                    'debt_range': (0, 0),
                    'expense_ratio': (0.50, 0.75),
                    'housing_tenure': ['自有', '租赁'],
                    'assets_extra': [
                        {'type': '股票/基金', 'desc': '小额试水', 'range': (1000, 30000), 'prob': 0.3},
                    ],
                    'debt_mix': [('房贷', 1.0)],
                },
                'goals': [
                    ('1 年内', '把储蓄率提到 20%'),
                    ('1 年内', '完成保障型保险配齐'),
                    ('5 年内', '攒下第一个 10 万元投资本金'),
                ],
                # 同一 L3 下可挂若干职业实例（姓名/就业形态/收入中值/波动/证书/健康风险/压力源）
                'occupations': [
                    ('示例职业 A', '全职', 12000, 2000,
                     ['初级资格证'], ['久坐'], ['晋升压力']),
                    ('示例职业 B', '全职', 15000, 2500,
                     ['中级资格证'], ['久坐', '出差'],
                     ['项目交付压力', '行业波动']),
                ],
            },
        ],
    },
    # …继续添加 L2/L3…
]


def main():
    dim = 'DIM2'                      # ← 改成你的维度代码（DIM2/DIM3/…/DIM7）
    start_id = 9020000                # ← 改成对应维度的 start_id（见 DIMENSIONS）
    batch_tag = 'v4.5-<dim>'          # ← 例：v4.5-dim2
    seed = 20260916                   # 团队统一 seed，便于跨批次复现
    total = 1500                      # ← 本批次计划张数

    repo_root = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
    out_root = os.path.join(repo_root, 'docs', '财商流游戏', '玩家职业设计')

    g = DimGenerator(dim=dim, start_id=start_id, batch_tag=batch_tag, seed=seed,
                     out_root=out_root)
    report = g.gen_from_pool(POOLS, count=total,
                             report_path=os.path.join(
                                 THIS_DIR, 'work', 'v45',
                                 'report_%s_%s.json' % (dim.lower(), batch_tag)))
    # 全库校验（v4.5 全部维度一起跑，~2 分钟）
    issues = V.run_full(repo_root=repo_root)
    if issues:
        print('[verify] 警告数=%d：' % len(issues))
        for x in issues[:20]:
            print('  -', x)
    else:
        print('[verify] 全库 v4.5 校验通过（id/name 唯一、字段齐备、YAML 可解析）')


if __name__ == '__main__':
    main()
