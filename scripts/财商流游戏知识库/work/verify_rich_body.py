#!/usr/bin/env python3
"""硬性自检：所有 1000 张 rich 卡正文 §6/§7/§8 字节级未变（与 git HEAD 对比）"""
import os, subprocess

ROOT = '/usr/local/LsmAgentGame/LsmAgentGame/docs/财商流游戏/玩家职业设计'

# 找出所有 rich 卡路径
rich_paths = []
for dp, _, fns in os.walk(ROOT):
    if any(p.startswith('_') for p in dp.split(os.sep)):
        continue
    for f in fns:
        if (f.startswith('N') or f.startswith('P')) and f.endswith('.md'):
            fp = os.path.join(dp, f)
            with open(fp, encoding='utf-8') as fh:
                if 'richness: rich' in fh.read():
                    rich_paths.append(fp)

print(f'rich 卡数: {len(rich_paths)}')

# 用 git show HEAD:<path> 拿到原始内容，与当前文件 §6/§7/§8 对比
def get_s678(text):
    out = {}
    for h in ('## 6. 情感与人格', '## 7. 人生目标', '## 8. 开局钩子'):
        i = text.find(h)
        if i < 0:
            out[h] = ''
            continue
        j = text.find('## ', i + len(h))
        out[h] = text[i:j] if j > 0 else text[i:]
    return out

pass_n = 0
fail = []
for fp in rich_paths:
    rel = os.path.relpath(fp, '/usr/local/LsmAgentGame/LsmAgentGame')
    # 当前内容
    with open(fp, encoding='utf-8') as f:
        cur_text = f.read()
    # HEAD 内容
    r = subprocess.run(['git', 'show', f'HEAD:{rel}'],
                       capture_output=True, text=True,
                       cwd='/usr/local/LsmAgentGame/LsmAgentGame')
    if r.returncode != 0:
        fail.append((rel, 'git show failed'))
        continue
    head_text = r.stdout

    cur_s = get_s678(cur_text)
    head_s = get_s678(head_text)

    if cur_s == head_s:
        pass_n += 1
    else:
        fail.append((rel, f"diff: cur={ {k: len(v) for k,v in cur_s.items()} } head={ {k: len(v) for k,v in head_s.items()} }"))

print(f'PASS: {pass_n}/{len(rich_paths)}')
if fail:
    print(f'FAIL: {len(fail)}')
    for rel, info in fail[:10]:
        print(f'  {rel}: {info}')
else:
    print('所有 rich 卡正文 §6/§7/§8 字节级未变！')
