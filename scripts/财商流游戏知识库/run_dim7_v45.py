#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""run_dim7_v45.py —— v4.5 维度⑦「行业维度扩容」driver"""
import os, sys
THIS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, THIS_DIR)

from gen_batch import Generator
import icg_l2

names_file = os.path.join(THIS_DIR, 'work', 'v45', 'names_dim7.txt')
with open(names_file, encoding='utf-8') as f:
    NAME_POOL = []
    for ln in f:
        ln = ln.rstrip('\n')
        if not ln.strip(): continue
        parts = ln.split('\t')
        NAME_POOL.append((parts[0].strip(), parts[1].strip() if len(parts) > 1 else '?'))
print(f'[dim7] 化名块 {len(NAME_POOL)} 名')

POOLS = __import__('dim_pools_v45_dim7', fromlist=['POOLS']).POOLS

def main():
    repo_root = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
    out_root = os.path.join(repo_root, 'docs', '财商流游戏', '玩家职业设计')
    gen = Generator(out_root, start_id=9045000, batch_tag='v4.5-dim7', seed=20260916)
    orig_fresh = gen._fresh_name
    name_idx = [0]
    used = set()
    def fresh_name(gender):
        nonlocal name_idx
        # 按性别匹配
        attempts = 0
        while name_idx[0] < len(NAME_POOL) and attempts < len(NAME_POOL):
            nm, gd = NAME_POOL[name_idx[0]]
            attempts += 1
            if (gender == '女' and gd == '女') or (gender == '男' and gd == '男'):
                name_idx[0] += 1
                if nm not in used:
                    used.add(nm)
                    return nm
            name_idx[0] += 1
        # fallback: 取下一个未使用
        while name_idx[0] < len(NAME_POOL):
            nm, _ = NAME_POOL[name_idx[0]]
            name_idx[0] += 1
            if nm not in used:
                used.add(nm)
                return nm
        return orig_fresh(gender)
    gen._fresh_name = fresh_name

    total = 0
    for l2, occs in POOLS.items():
        l1 = l2[0]
        quota = max(50, 1000 // len(POOLS))
        for i in range(quota):
            o = occs[i % len(occs)]
            card, l1r, l2r = gen.generate_one(l1, l2, o)
            gen.write_card(card, l1r, l2r)
            total += 1
    print(f'[dim7] 完成 {total} 张')

if __name__ == '__main__':
    main()
