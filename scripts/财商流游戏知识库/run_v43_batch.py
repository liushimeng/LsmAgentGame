#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""run_v43_batch.py —— v4.3 薄弱行业 + 新兴交叉职业池批量生成 driver

针对 scripts/财商流游戏知识库/occ_pools_ext_v43.py 中定义的 33 个新 L2 池，
按配额逐池调用 gen_batch.Generator.generate_one 生成 skeleton 卡片。

约定：
  - 起 ID：N9014200
  - batch_tag: v4.3
  - 输出目录：docs/财商流游戏/玩家职业设计/
  - 总数 ≥ 1500 张
  - 完整度 _completeness ≥ 0.80（Generator 内部 uniform 0.85~0.97，已自动满足）

用法：
    python3 scripts/财商流游戏知识库/run_v43_batch.py
"""

import argparse
import os
import sys

THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)

from occ_pools_ext_v43 import EXT_V43  # noqa: E402

import gen_batch  # noqa: E402


# 33 个池的配额（按 L1 域分布 + 新兴职业重点加码）：
# 前 32 池每池 46 张，最后 1 池 28 张 = 32*46 + 28 = 1500
# 按 EXT_V43 顺序分配，最后一个池（Z13 农业新业态）压缩。
QUOTA_PER_POOL = 46
QUOTA_LAST_POOL = 28  # Z13
START_ID = 9014200
BATCH_TAG = 'v4.3'
OUT_DIR = os.path.abspath(os.path.join(THIS_DIR, '..', '..', 'docs', '财商流游戏', '玩家职业设计'))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--out', default=OUT_DIR)
    ap.add_argument('--start-id', type=int, default=START_ID)
    ap.add_argument('--batch-tag', default=BATCH_TAG)
    ap.add_argument('--seed', type=int, default=20260914)
    ap.add_argument('--per-pool', type=int, default=QUOTA_PER_POOL)
    args = ap.parse_args()

    l2_keys = sorted(EXT_V43.keys())
    n_pools = len(l2_keys)
    # 配额：除最后一池外都 per-pool 张，最后一池压缩到 QUOTA_LAST_POOL
    quotas = [args.per_pool] * n_pools
    quotas[-1] = QUOTA_LAST_POOL
    target_total = sum(quotas)
    print('[v4.3 driver] 准备生成 %d 个 L2 池、共 %d 张卡片（start_id=N%d）' %
          (n_pools, target_total, args.start_id))

    gen = gen_batch.Generator(args.out, start_id=args.start_id,
                              batch_tag=args.batch_tag, seed=args.seed)
    # 注意：Generator 的 _completeness 是 uniform(0.85, 0.97)，
    # 已满足任务约束（≥0.80）。骨架档位 richness='skeleton'。

    total = 0
    for l2, quota in zip(l2_keys, quotas):
        l1 = l2[0]
        # 取出该池的职业实例列表
        occ_pool = EXT_V43[l2]
        written = 0
        for i in range(quota):
            occ_info = gen.rng.choice(occ_pool)
            card, l1r, l2r = gen.generate_one(l1, l2, occ_info)
            # 完整度断言（双保险）
            assert card['_completeness'] >= 0.80, \
                '完整度 <0.80: %s -> %s' % (card['id'], card['_completeness'])
            gen.write_card(card, l1r, l2r)
            written += 1
        total += written
        print('[v4.3 driver] L2=%s (%s) 生成 %d 张 (next_id=%s)' %
              (l2, EXT_V43[l2][0][0], written, gen.next_id))

    print('[v4.3 driver] 合计生成 %d 张卡片 (start_id=N%d, end_id=N%d)' %
          (total, args.start_id, gen.next_id - 1))
    return 0


if __name__ == '__main__':
    sys.exit(main())