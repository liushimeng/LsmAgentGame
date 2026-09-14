#!/usr/bin/env python3
"""硬性自检：随机抽样 200 张 skeleton 卡，确认 frontmatter 行数变化合理 + PyYAML 解析通过"""
import json, os, random, subprocess, sys
import yaml

ROOT = '/usr/local/LsmAgentGame/LsmAgentGame/docs/财商流游戏/玩家职业设计'

# 找出所有 skeleton 卡
all_paths = []
for dp, _, fns in os.walk(ROOT):
    if any(p.startswith('_') for p in dp.split(os.sep)):
        continue
    for f in fns:
        if (f.startswith('N') or f.startswith('P')) and f.endswith('.md'):
            all_paths.append(os.path.join(dp, f))

random.seed(20260914 + 1)
sample = random.sample(all_paths, 200)

# 用 git HEAD 对比 frontmatter 行数差异
def fm_line_count(text):
    if not text.startswith('---'):
        return -1, -1
    parts = text.split('---', 2)
    return parts[1].count('\n'), parts[1]

yaml_fail = 0
line_delta_list = []
fm_unchanged_field = 0  # 既有字段保持不变

# 检查 6 字段都存在
field_check = {f: 0 for f in ['personality', 'behavior_traits', 'risk_preference',
                              'birth_family', 'life_story', 'opening_hook']}

# 检查既有关键字段未被改
key_fields = ['id', 'name', 'schema_version', 'card_type', 'richness',
              'gender', 'age', 'birth_year', 'education', 'income_monthly']
key_field_modified = []

for p in sample:
    with open(p, encoding='utf-8') as f:
        cur = f.read()

    # PyYAML 解析
    parts = cur.split('---', 2)
    fm_str = parts[1].lstrip('\n')
    try:
        data = yaml.safe_load(fm_str)
        yaml_ok = True
    except Exception as e:
        yaml_fail += 1
        print(f'  YAML FAIL: {p}: {e}')
        continue

    # 检查 6 字段
    for fld in field_check:
        if data.get(fld) not in (None, '', [], {}):
            field_check[fld] += 1

    # 检查既有字段是否保持（与 HEAD 对比）
    rel = os.path.relpath(p, '/usr/local/LsmAgentGame/LsmAgentGame')
    r = subprocess.run(['git', 'show', f'HEAD:{rel}'],
                       capture_output=True, text=True,
                       cwd='/usr/local/LsmAgentGame/LsmAgentGame')
    if r.returncode != 0:
        continue
    head_text = r.stdout
    head_parts = head_text.split('---', 2)
    head_fm_str = head_parts[1].lstrip('\n')
    try:
        head_data = yaml.safe_load(head_fm_str) or {}
    except Exception:
        continue

    # 行数变化
    head_lines = head_fm_str.count('\n')
    cur_lines = fm_str.count('\n')
    delta = cur_lines - head_lines
    line_delta_list.append(delta)

    # 既有字段值是否相同
    for kf in key_fields:
        if kf in head_data and kf in data:
            if head_data[kf] != data[kf]:
                key_field_modified.append((rel, kf, head_data[kf], data[kf]))

print(f'\n=== 200 张 skeleton 抽样自检 ===')
print(f'  YAML 解析失败: {yaml_fail}')
print(f'  6 字段填充:')
for fld, c in field_check.items():
    print(f'    {fld}: {c}/200')

if line_delta_list:
    deltas = sorted(line_delta_list)
    print(f'  frontmatter 行数变化:')
    print(f'    min={deltas[0]} max={deltas[-1]} median={deltas[len(deltas)//2]} avg={sum(deltas)/len(deltas):.1f}')

if key_field_modified:
    print(f'  既有字段被改: {len(key_field_modified)}')
    for rel, kf, old, new in key_field_modified[:5]:
        print(f'    {rel}: {kf}: {old} -> {new}')
else:
    print(f'  既有 9 个关键字段全部保持原值：PASS')

if yaml_fail == 0 and all(c == 200 for c in field_check.values()) and not key_field_modified:
    print(f'\n=== 自检总览：PASS ===')
else:
    print(f'\n=== 自检总览：FAIL ===')
