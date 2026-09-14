# -*- coding: utf-8 -*-
"""occ_pools_ext.py —— 职业池扩展聚合入口

按 §4 单文件 ≤ 1800 行约束，把 v4.2 已有的 172 个 L2 池拆为
  occ_pools_ext_v42_ahkl.py / occ_pools_ext_v42_mnopr.py / occ_pools_ext_v42_tuvwxyz.py
三个子文件（各约 1200 行）。
v4.3 新增 33 个 L2 池（薄弱行业 + 新兴交叉职业）放于
  occ_pools_ext_v43.py。

本文件仅做 import + 合并，仍以单一 `EXT` 字典暴露给 gen_batch.py，
下游 importlib 加载逻辑无须改动。

历史：原 occ_pools_ext.py 3618 行（v4.2 单一文件），
      2026-09-14 §v4.3 任务拆分为聚合入口。
"""

from occ_pools_ext_v42_ahkl import EXT_A
from occ_pools_ext_v42_mnopr import EXT_B
from occ_pools_ext_v42_tuvwxyz import EXT_C
from occ_pools_ext_v43 import EXT_V43

EXT = {}
EXT.update(EXT_A)   # A/H/K/L 域（40 个 L2）
EXT.update(EXT_B)   # M/N/O/P/Q/R 域（62 个 L2）
EXT.update(EXT_C)   # T/U/V/W/X/Y/Z 域（70 个 L2）
EXT.update(EXT_V43)  # v4.3 新增（33 个 L2，含薄弱 E/I/S/B/F/D/G/C/J/H/W 补强 + Z10-Z13）

__all__ = ['EXT']

if __name__ == '__main__':
    print('EXT 字典合计 %d 个 L2 池' % len(EXT))
    by_l1 = {}
    for k in EXT:
        by_l1[k[0]] = by_l1.get(k[0], 0) + 1
    for l1 in sorted(by_l1):
        print('  L1 %s: %2d 个 L2 池' % (l1, by_l1[l1]))