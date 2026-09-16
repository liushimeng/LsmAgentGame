#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""dim_pools_v45_dim4 —— v4.5 维度④「家庭生活与消费财商」池数据

7 个 L2 细分 / 24 个 L3 人群族，总配额 ≥1,400 张。
按 L2 拆为 _l1.py – _l7.py，单文件均 ≤ 1800 行。

用法：
    from dim_pools_v45_dim4 import POOLS
    g = DimGenerator(dim='DIM4', ...)
    report = g.gen_from_pool(POOLS, count=1400)
"""
from . import _l1, _l2, _l3, _l4, _l5, _l6, _l7  # noqa: F401

POOLS = (
    _l1.POOLS_L1
    + _l2.POOLS_L2
  + _l3.POOLS_L3
  + _l4.POOLS_L4
  + _l5.POOLS_L5
  + _l6.POOLS_L6
  + _l7.POOLS_L7
)
