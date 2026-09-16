#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""dim_pools_v45_dim3.py —— v4.5 维度③「财富阶层与资产负债」池数据聚合入口

聚合 7 个 L2 分层（L1_POOLS ~ L7_POOLS），共 20 个 L3 组：
  - DIM3-01 顶层财富阶层（4 L3）：大型企业主/上市公司高管/顶级投资人/顶级专业服务者
  - DIM3-02 上层财富阶层（3 L3）：资深高管/资深专业人士/FIRE 实践者
  - DIM3-03 中产阶层（3 L3）：一线新中产/城市白领中产/二线省会中产
  - DIM3-04 底层低收入（3 L3）：城乡普通工薪/县域个体户/进城务工
  - DIM3-05 破败与高负债（3 L3）：高房贷挤压/失信被执行人/赌博套路贷
  - DIM3-06 食利与被动收入（2 L3）：城中村收租/土地分红信托
  - DIM3-07 拆迁继承暴富（2 L3）：城中村村改/遗产彩票中奖

权重合计 = 14.0（顶层 4.2 + 上层 2.8 + 中产 3.4 + 底层 2.8 + 破败 2.2 + 食利 1.4 + 拆迁 1.2 = 18.0）
目标 1400 张 → 每权重 ≈ 78 张。

注：由 run_dim3_v45.py 统一入口；禁止直接 import 单个 _l1~_l7 模块（仅本聚合器 import）。
"""
from dim_pools_v45_dim3_l1 import L1_POOLS
from dim_pools_v45_dim3_l2 import L2_POOLS
from dim_pools_v45_dim3_l3 import L3_POOLS
from dim_pools_v45_dim3_l4 import L4_POOLS
from dim_pools_v45_dim3_l5 import L5_POOLS
from dim_pools_v45_dim3_l6 import L6_POOLS
from dim_pools_v45_dim3_l7 import L7_POOLS

POOLS = L1_POOLS + L2_POOLS + L3_POOLS + L4_POOLS + L5_POOLS + L6_POOLS + L7_POOLS
