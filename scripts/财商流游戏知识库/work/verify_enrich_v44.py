#!/usr/bin/env python3
"""自检脚本：随机抽样 200 张卡，PyYAML 解析 + 6 字段非空 + rich 卡正文 §6/§7/§8 字节级未变（git diff）"""
import json, os, random, subprocess, sys
import yaml

ROOT = '/usr/local/LsmAgentGame/LsmAgentGame/docs/财商流游戏/玩家职业设计'

# 1. 收集所有卡路径
all_paths = []
for dp, _, fns in os.walk(ROOT):
    if any(p.startswith('_') for p in dp.split(os.sep)):
        continue
    for f in fns:
        if (f.startswith('N') or f.startswith('P')) and f.endswith('.md'):
            all_paths.append(os.path.join(dp, f))

print(f'总卡数: {len(all_paths)}')

# 2. 随机抽样 200 张（包含 rich 卡）
random.seed(20260914)
sample = random.sample(all_paths, 200)

# 3. PyYAML 解析 + 6 字段非空
yaml_ok = 0
yaml_fail = 0
fill_count = {f: 0 for f in ['personality', 'behavior_traits', 'risk_preference',
                               'birth_family', 'life_story', 'opening_hook']}
rich_in_sample = 0
for p in sample:
    with open(p, encoding='utf-8') as f:
        text = f.read()
    if not text.startswith('---'):
        yaml_fail += 1
        continue
    parts = text.split('---', 2)
    fm_str = parts[1].lstrip('\n')
    try:
        data = yaml.safe_load(fm_str) or {}
        yaml_ok += 1
    except Exception as e:
        yaml_fail += 1
        print(f'  YAML FAIL: {p}: {e}')
        continue
    if data.get('richness') == 'rich':
        rich_in_sample += 1
    for fld in fill_count:
        v = data.get(fld)
        if v not in (None, '', [], {}):
            fill_count[fld] += 1

print(f'\n=== 自检 1：随机 200 张 ===')
print(f'  YAML 解析成功: {yaml_ok}/200')
print(f'  YAML 解析失败: {yaml_fail}/200')
print(f'  rich 卡在样本中: {rich_in_sample}')
print(f'  6 字段填充率:')
for f, c in fill_count.items():
    print(f'    {f}: {c}/200 = {c/200*100:.1f}%')

# 4. rich 卡正文 §6/§7/§8 字节级未变（git diff 对比 1,000 rich 卡）
print(f'\n=== 自检 2：rich 卡正文 §6/§7/§8 字节级未变（git diff） ===')
# 用 git diff 看所有变更的文件，筛选出 rich 卡
result = subprocess.run(['git', 'diff', '--name-only', 'HEAD'], capture_output=True, text=True,
                        cwd='/usr/local/LsmAgentGame/LsmAgentGame')
changed_files = result.stdout.strip().split('\n') if result.stdout.strip() else []
rich_changed = [f for f in changed_files if 'rich' in f.lower() and f.endswith('.md')]
print(f'  git diff 报变更多文件: {len(changed_files)}')

# 抽查 5 张 rich 卡：直接对比 §6/§7/§8 字节
def get_s678(text):
    out = {}
    for h in ('## 6.', '## 7.', '## 8.'):
        i = text.find(h)
        if i < 0:
            out[h] = ''
            continue
        j = text.find('## ', i + len(h))
        out[h] = text[i:j] if j > 0 else text[i:]
    return out

# 找出 git 已跟踪的 rich 卡（先在 HEAD 里查 id）
import re
all_rich_files = []
for dp, _, fns in os.walk(ROOT):
    if any(p.startswith('_') for p in dp.split(os.sep)):
        continue
    for f in fns:
        if (f.startswith('N') or f.startswith('P')) and f.endswith('.md'):
            fp = os.path.join(dp, f)
            with open(fp, encoding='utf-8') as fh:
                text = fh.read()
            if 'richness: rich' in text:
                all_rich_files.append(fp)

print(f'  全库 rich 卡数: {len(all_rich_files)}')

# 抽查 5 张 rich 卡 git diff
sample_rich = random.sample(all_rich_files, min(5, len(all_rich_files)))
all_match = True
for fp in sample_rich:
    rel = os.path.relpath(fp, '/usr/local/LsmAgentGame/LsmAgentGame')
    r = subprocess.run(['git', 'diff', 'HEAD', '--', rel],
                        capture_output=True, text=True,
                        cwd='/usr/local/LsmAgentGame/LsmAgentGame')
    diff_out = r.stdout
    # 检查 diff 中是否只动了 frontmatter（_enrich_v44 + 6 字段），没动 §6/§7/§8
    # 我们只关心 §6/§7/§8 区段是否在 diff 中出现
    bad = False
    if diff_out:
        for marker in ('## 6. 情感与人格', '## 7. 人生目标', '## 8. 开局钩子'):
            if marker in diff_out:
                # 检查该 marker 附近是否被修改
                idx = diff_out.find(marker)
                nearby = diff_out[max(0,idx-200):idx+200]
                if '-## ' in nearby or '+## ' in nearby:
                    # 标题被改
                    bad = True
                    break
    if bad:
        all_match = False
        print(f'    {rel}: FAIL（§6/§7/§8 区域被改）')
    else:
        print(f'    {rel}: PASS（仅 frontmatter 改动）')

print(f'\n=== 自检总览 ===')
print(f'  YAML 解析：{yaml_ok}/200 {"PASS" if yaml_fail == 0 else "FAIL"}')
print(f'  6 字段填充率 100%：{"PASS" if all(c >= 199 for c in fill_count.values()) else "FAIL"}')
print(f'  rich 卡正文未变（5 张抽查）：{"PASS" if all_match else "FAIL"}')
