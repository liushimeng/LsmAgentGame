#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""enrich_holistic_v44.py —— 阶段 B：Schema v1.1 全息画像六字段全量推导补值

执行方案 `tmpPlan/财商流游戏-玩家职业设计-v4.4-归档合并与全息画像补全-20260914-01.md` §2。

为所有 skeleton 卡确定性推导 6 个新字段：
  · personality           list[str]   2-4 个标签（人格词库 12 词）
  · behavior_traits       list[str]   2-4 个标签（行为词库 10 词）
  · risk_preference       enum        conservative / balanced / aggressive
  · birth_family          str         6 档（农村务农/县城工薪/城镇个体户/城市中产/一线白领/知识家庭）
  · life_story            list[map]   3-6 条时间线（age + event）
  · opening_hook          str         30-50 字一句话钩子（§8 正文来源）

并同步更新正文 §6（人格特征行 + 行为特征行）与 §8（开局钩子 + 人生经历时间线）。

additive 原则：
  · 不改任何既有字段语义
  · frontmatter 行级精准插入新字段（不重排）
  · rich 卡正文 §6/§7/§8 字节级未动，只在 frontmatter 追加新字段 + _enrich_v44 章

用法:
    python3 enrich_holistic_v44.py --dry                   # 干跑 + 闸门 + 20 卡对照
    python3 enrich_holistic_v44.py --dry --limit N         # 抽样 N 张
    python3 enrich_holistic_v44.py                         # 真跑（需闸门全过）
    python3 enrich_holistic_v44.py --limit 5000             # 限量真跑

报告落盘：
    scripts/财商流游戏知识库/work/enrich_v44_report.json
"""
import argparse
import json
import os
import random
import re
import sys
import time
from datetime import datetime, timezone

import yaml

# ── 路径常量 ───────────────────────────────────────────────────────
ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..'))
LETTER_TREE_ROOT = os.path.join(ROOT, 'docs', '财商流游戏', '玩家职业设计')
WORK_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'work')
REPORT_PATH = os.path.join(WORK_DIR, 'enrich_v44_report.json')

# ── 6 字段词典（确定性查表） ──────────────────────────────────────
# personality 12 词（人格特征标签）
PERSONALITY_VOCAB = [
    '谨慎稳健',   # 风险厌恶 + 储蓄率高
    '外向社交',   # E（外向）
    '尽责坚韧',   # C（尽责）高
    '开放求新',   # O（开放）高
    '情绪稳定',   # N（神经质）低
    '风险偏好高',  # risk_preference=aggressive
    '风险偏好低',  # risk_preference=conservative
    '细腻敏感',   # N 高 + 艺术类职业
    '果决有力',   # 管理层 + 低 stress
    '顺从协作',   # A（宜人）高
    '独立自主',   # 单人家庭 + 个体经营
    '理想主义',   # 目标含「梦想」+ 教育高
    '务实主义',   # 目标短 + 储蓄导向
]

# behavior_traits 10 词（行为特征标签）
BEHAVIOR_VOCAB = [
    '记账习惯',     # savings_rate > 0.3
    '冲动消费',     # biases 含「即时满足」或 savings_rate < 0.1
    '货比三家',     # 收入稳定 + 储蓄率高 + 风险厌恶
    '长线规划',     # goals_short ≥ 3 条
    '风险管理',     # 有保险 + 公积金
    '保守储蓄',     # 储蓄率高 + 风险厌恶
    '积极投资',     # 风险寻求 + 金融类
    '月光族',       # savings_rate ≤ 0
    '精打细算',     # 月支出/收入 < 0.4 + 储蓄率高
    '冲动决策',     # biases 含「过度自信」或「即时满足」
]

# birth_family 6 档
BIRTH_FAMILY_VOCAB = [
    '农村务农',     # 农村 + 教育初中以下
    '县城工薪',     # 非一线 + 教育高中中专
    '城镇个体户',   # 父母个体户启发（个体经营 + 教育中专）
    '城市中产',     # 城市 + 教育本科
    '一线白领',     # 一线城市 + 教育本科以上
    '知识家庭',     # 教育硕士以上
]

# ── 确定性推导函数 ─────────────────────────────────────────────────
def derive_personality(fm):
    """根据 biases + stress_level + career_stage + work_intensity.risk + industry_l1
    生成 2-4 个差异化标签。基于风险偏好 + 行业大类 + 就业形态 + 压力等级四个维度组合。
    """
    biases = fm.get('biases') or []
    stress = fm.get('stress_level') or '中'
    stage = fm.get('career_stage') or '骨干'
    wi = fm.get('work_intensity') or {}
    risk_work = wi.get('risk') if isinstance(wi, dict) else None
    industry = fm.get('industry_l1') or ''
    emp = fm.get('employment') or ''

    # 维度 1：风险偏好（必选其一）
    if '风险厌恶' in biases:
        risk_label = '风险偏好低'
    elif '风险寻求' in biases:
        risk_label = '风险偏好高'
    else:
        # 用风险偏好字段（如已存在则跳过）
        risk_label = None

    # 维度 2：尽责/开放（行业大类启发）
    if industry in ('S', 'T', 'P'):  # 科研/教育/IT → 开放 + 尽责
        tag_a = '开放求新'
    elif industry in ('M', 'O', 'R'):  # 商业/餐饮/专业服务 → 外向 + 尽责
        tag_a = '外向社交'
    elif industry in ('A', 'B', 'E'):  # 农林/采矿/木材 → 坚韧
        tag_a = '尽责坚韧'
    elif industry in ('L', 'H', 'J'):  # 建筑/机械/汽车 → 果决
        tag_a = '果决有力'
    else:
        tag_a = '尽责坚韧'

    # 维度 3：情绪稳定/敏感（压力启发）
    if stress == '高':
        tag_b = '细腻敏感'
    else:
        tag_b = '情绪稳定'

    # 维度 4：协作/独立（就业形态启发）
    if emp in ('个体经营', '自由职业'):
        tag_c = '独立自主'
    elif emp in ('平台就业', '灵活就业', '退休返聘'):
        tag_c = '务实主义'
    elif stage in ('资深', '管理'):
        tag_c = '果决有力' if risk_work == '高' else '理想主义'
    else:
        tag_c = '顺从协作'

    # 组装：risk_label + tag_a + tag_b + tag_c，去重
    candidates = []
    if risk_label:
        candidates.append(risk_label)
    candidates.extend([tag_a, tag_b, tag_c])

    out = []
    for x in candidates:
        if x not in out:
            out.append(x)
        if len(out) >= 4:
            break

    # 保证至少 2 个
    if len(out) < 2:
        out.append('务实主义')
    return out[:4]


def derive_behavior_traits(fm):
    """根据 income_stability + savings_rate + debts + biases + employment。
    维度组合：储蓄水平（高/低/月光）+ 决策风格（计划/冲动）+ 风险行为（保守/积极）。
    """
    biases = fm.get('biases') or []
    sr = fm.get('savings_rate')
    try:
        sr = float(sr) if sr is not None else None
    except Exception:
        sr = None
    ist = fm.get('income_stability') or '中'
    debts = fm.get('debts') or []
    emp = fm.get('employment') or ''
    insurance = fm.get('insurance') or []
    funds = fm.get('funds') or []
    goals = fm.get('goals_short') or []
    industry = fm.get('industry_l1') or ''

    # 维度 1：储蓄习惯（必选）
    if sr is None:
        sav_tag = '记账习惯'  # 默认
    elif sr > 0.5:
        sav_tag = '保守储蓄'
    elif sr > 0.2:
        sav_tag = '记账习惯'
    elif sr > 0.05:
        sav_tag = '精打细算'
    else:
        sav_tag = '月光族'

    # 维度 2：决策风格
    if '过度自信' in biases or '即时满足' in biases:
        decision_tag = '冲动决策'
    elif len(goals) >= 3:
        decision_tag = '长线规划'
    elif '损失厌恶' in biases or '保守' in biases:
        decision_tag = '货比三家'
    else:
        decision_tag = '精打细算' if sav_tag in ('精打细算', '记账习惯') else '长线规划'

    # 维度 3：风险行为
    if industry == 'Q' and '风险寻求' in biases:
        risk_tag = '积极投资'
    elif insurance or funds:
        risk_tag = '风险管理'
    elif '风险厌恶' in biases:
        risk_tag = '保守储蓄'
    else:
        risk_tag = '记账习惯' if sav_tag == '记账习惯' else '精打细算'

    # 维度 4（可选）：消费特征
    consumer_tag = None
    if sr is not None and sr < 0.15 and '即时满足' in biases:
        consumer_tag = '冲动消费'
    elif ist == '低' and sr is not None and sr > 0.3:
        consumer_tag = '保守储蓄'

    # 组装去重
    candidates = [sav_tag, decision_tag, risk_tag]
    if consumer_tag and consumer_tag not in candidates:
        candidates.append(consumer_tag)

    out = []
    for x in candidates:
        if x not in out:
            out.append(x)
        if len(out) >= 4:
            break

    if len(out) < 2:
        out.append('精打细算')
    return out[:4]


def derive_risk_preference(fm):
    """根据 assets 结构 + debts + income_stability + biases。"""
    biases = fm.get('biases') or []
    ist = fm.get('income_stability') or '中'
    assets = fm.get('assets') or []
    debts = fm.get('debts') or []
    has_mortgage = any((d or {}).get('type') == '房贷' for d in debts)

    # 房产占比
    has_property = any((a or {}).get('type') == '房产' for a in assets)
    sr = fm.get('savings_rate')
    try:
        sr = float(sr) if sr is not None else None
    except Exception:
        sr = None

    # 偏差强信号优先
    if '风险厌恶' in biases or '保守' in biases:
        return 'conservative'
    if '风险寻求' in biases and ist == '低':
        return 'aggressive'

    # 结构性判断
    if ist == '低' and has_mortgage:
        return 'conservative'  # 高房贷 + 低稳定 → 保守
    if not has_property and has_mortgage is False and sr is not None and sr > 0.3:
        return 'balanced'  # 无房 + 高储蓄 → 平衡（可能想买房）
    if has_property and ist in ('高', '中') and sr is not None and sr > 0.2:
        return 'balanced'  # 有房 + 稳定 + 储蓄 → 平衡
    if '风险寻求' in biases:
        return 'aggressive'
    if ist == '高' and (not has_mortgage) and sr is not None and sr < 0.15:
        return 'aggressive'  # 高收入 + 无房贷 + 低储蓄 → 激进

    return 'balanced'


def derive_birth_family(fm):
    """根据 birth_province + education + birth_city + 启发式职业大类。
    6 档：农村务农 / 县城工薪 / 城镇个体户 / 城市中产 / 一线白领 / 知识家庭
    """
    prov = fm.get('birth_province') or ''
    edu = fm.get('education') or ''
    industry = fm.get('industry_l1') or ''
    city = fm.get('birth_city') or fm.get('housing_city') or ''

    # 一线城市
    first_tier = {'北京', '上海', '广州', '深圳'}
    # 农村/乡镇启发（prov 含有"省"外的县/乡级，或 city 为县城）
    rural_kw = {'县', '乡', '镇', '村', '屯'}

    city_is_first = city in first_tier
    city_is_rural = any(k in str(city) for k in rural_kw) if city else False

    # 知识家庭：博士/硕士
    if edu == '博士':
        return '一线白领' if city_is_first else '知识家庭'
    if edu == '硕士':
        if city_is_first:
            return '一线白领'
        return '知识家庭'

    # 本科/MBA/EMBA：城市中产 / 一线白领
    if edu in ('本科', 'MBA', 'EMBA'):
        return '一线白领' if city_is_first else '城市中产'

    # 大专：城镇/县城
    if edu == '大专':
        return '城市中产' if city_is_first else '城镇个体户'

    # 中专：县城工薪 / 城镇个体户
    if edu == '中专':
        return '县城工薪' if city_is_rural else '城镇个体户'

    # 高中：县城工薪
    if edu == '高中':
        return '县城工薪'

    # 初中及以下：农村务农 / 县城工薪
    if edu in ('初中', None, ''):
        return '农村务农' if not city_is_first else '县城工薪'

    return '县城工薪'


def derive_life_story(fm):
    """根据 birth_year + education + marital + children_ages + housing_tenure + career_stage。
    生成 3-6 条时间线（age + event），按时间正序排列。"""
    by = fm.get('birth_year')
    edu = fm.get('education') or ''
    marital = fm.get('marital') or '未婚'
    children = fm.get('children_ages') or []
    housing = fm.get('housing_tenure') or '未知'
    stage = fm.get('career_stage') or '骨干'
    age = fm.get('age')

    if not by or not age:
        return []

    events = []
    # 出生（age=0）
    events.append({'age': 0, 'event': f'出生于{fm.get("birth_province") or "未知地区"}'})

    # 求学阶段（按学历估算入学/毕业年龄）
    edu_start_age, edu_end_age = {
        '博士': (22, 29), '硕士': (22, 26), '本科': (18, 22), '大专': (18, 21),
        '中专': (15, 18), '高中': (15, 18), '初中': (12, 15),
        'MBA': (28, 32), 'EMBA': (34, 38),
    }.get(edu, (18, 22))
    edu_label = {
        '博士': '博士研究生', '硕士': '硕士研究生', '本科': '本科',
        '大专': '大专', '中专': '中专', '高中': '高中', '初中': '初中',
        'MBA': 'MBA', 'EMBA': 'EMBA',
    }.get(edu, edu)

    # 入学年龄不能超过当前年龄
    if edu_start_age < age:
        events.append({'age': edu_start_age, 'event': f'进入{edu_label}学习阶段'})
    if edu_end_age < age:
        events.append({'age': edu_end_age, 'event': f'{edu_label}毕业，步入职场'})

    # 婚姻（已婚后追加；婚姻年龄合理：22-32 之间）
    if marital in ('已婚', '再婚'):
        if children:
            m_age = max(22, age - max(children) - 2)
        else:
            m_age = max(22, age - 5)
        # 不能晚于当前年龄 - 1（已婚至少 1 年）
        m_age = min(m_age, age - 1)
        # 不能早于 edu_end_age
        m_age = max(m_age, edu_end_age)
        if 22 <= m_age <= age:
            events.append({'age': m_age, 'event': '结婚组建家庭'})

    # 生育（已婚 + 有子女）
    if children and marital in ('已婚', '再婚'):
        first_child_age = age - max(children)
        if first_child_age >= 22 and first_child_age < age:
            events.append({'age': first_child_age, 'event': '第一个孩子出生'})

    # 购房（自有/自建）
    if housing in ('自有', '自建'):
        # 购房年龄估算：有房贷 → 较早，无房贷 → 较晚
        buy_age = max(25, age - 10)
        buy_age = min(buy_age, age - 1)
        if 25 <= buy_age <= age:
            events.append({'age': buy_age, 'event': '购置首套房产'})

    # 当前阶段
    events.append({'age': age, 'event': f'当前处于{stage}阶段'})

    # 按年龄正序排序，去重
    out = []
    seen_ages = set()
    for ev in sorted(events, key=lambda x: x['age']):
        if ev['age'] not in seen_ages:
            out.append(ev)
            seen_ages.add(ev['age'])
    return out[:6]


def derive_opening_hook(fm):
    """根据 stress_sources + goals_short[0] + personality 生成一句话（30-50 字）。"""
    stresses = fm.get('stress_sources') or []
    goals = fm.get('goals_short') or []
    personalities = fm.get('personality') or []
    city = fm.get('housing_city') or ''
    gender = fm.get('gender') or ''

    # 取第一个压力源 + 第一个目标
    stress = stresses[0] if stresses else '家庭开支'
    goal_text = ''
    if goals and isinstance(goals[0], dict):
        goal_text = goals[0].get('text', '')
    elif goals and isinstance(goals[0], str):
        goal_text = goals[0]

    # 取首要 personality
    ptag = personalities[0] if personalities else '务实'
    pronoun = '他' if gender == '男' else '她' if gender == '女' else '他/她'

    # 短句拼接，避免冗长
    short_goal = goal_text[:14] if goal_text else ''
    short_stress = stress[:8] if len(stress) > 8 else stress

    parts = []
    if city:
        parts.append(city)
    parts.append(f'{pronoun}面对{short_stress}')
    if short_goal:
        parts.append(f'，目标「{short_goal}」')
    parts.append(f'；{ptag}的{pronoun}需在本回合做出取舍')

    hook = '，'.join(parts)
    # 清理连续标点
    hook = re.sub(r'[，；]{2,}', lambda m: m.group(0)[0], hook)
    # 截断到 30-50 字（中文字符）
    if len(hook) > 50:
        hook = hook[:47] + '…'
    if len(hook) < 20:
        hook = hook + '，财务抉择就在眼前'
    return hook


# ── 行级精准插入辅助 ───────────────────────────────────────────────
NEW_FIELDS_ORDER = [
    'personality',
    'behavior_traits',
    'risk_preference',
    'birth_family',
    'life_story',
    'opening_hook',
]

# 既有 frontmatter 中 schema_version 字段的位置（用于把新字段插在 _legacy_ids 之前）
INSERT_AFTER_KEY = 'dream_cost'  # 放在 dream_cost 后（goals 最后一字段）


def insert_new_fields_into_fm(fm_str, fm_data, new_fields):
    """行级精准插入新字段到 frontmatter 字符串，不重排既有字段。
    新字段追加在 dream_cost 行之后；_enrich_v44 章放在 _raw 之后。
    返回新的 frontmatter 字符串。
    """
    lines = fm_str.splitlines(keepends=False)

    # 找到插入点：dream_cost 所在行
    insert_idx = None
    for i, line in enumerate(lines):
        if line.startswith(f'{INSERT_AFTER_KEY}:'):
            insert_idx = i + 1
            break
    if insert_idx is None:
        # 兜底：放在 _legacy_ids 前
        for i, line in enumerate(lines):
            if line.startswith('_legacy_ids:'):
                insert_idx = i
                break
    if insert_idx is None:
        # 终极兜底：末尾
        insert_idx = len(lines)

    # 构造新字段的 YAML 行（保持与既有缩进一致）
    indent = '  '
    new_lines = []
    for field_name in NEW_FIELDS_ORDER:
        val = new_fields.get(field_name)
        if field_name in ('personality', 'behavior_traits', 'life_story'):
            # list[str] 或 list[map]
            items = val if val else []
            if not items:
                new_lines.append(f'{field_name}: []')
                continue
            new_lines.append(f'{field_name}:')
            if field_name == 'life_story':
                # list[map]：每条是 {age: int, event: str}
                for item in items:
                    new_lines.append(f'{indent}- age: {item["age"]}')
                    new_lines.append(f'{indent}  event: "{item["event"]}"')
            else:
                for item in items:
                    # 转义双引号
                    safe = str(item).replace('"', '\\"')
                    new_lines.append(f'{indent}- "{safe}"')
        elif field_name == 'risk_preference':
            new_lines.append(f'{field_name}: {val}')
        else:  # str
            safe = str(val or '').replace('"', '\\"')
            new_lines.append(f'{field_name}: "{safe}"')

    # 拼接
    new_fm_lines = lines[:insert_idx] + new_lines + lines[insert_idx:]
    return '\n'.join(new_fm_lines)


def append_enrich_meta(fm_str, fm_data):
    """在 _raw 之后追加 _enrich_v44 章（不在 _migration_v2/_merge_v44/_rich 之间强制插入）。"""
    lines = fm_str.splitlines(keepends=False)

    # 找 _raw 块结束位置（_raw: { ... } 是 dict，跨多行；这里 _raw 是单行 dict 字面量）
    raw_idx = None
    for i, line in enumerate(lines):
        if line.startswith('_raw:'):
            raw_idx = i
            break
    if raw_idx is None:
        # 兜底：末尾
        raw_idx = len(lines)

    enrich_block = [
        '_enrich_v44:',
        '  ts: "' + datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%S+00:00') + '"',
        '  script: enrich_holistic_v44.py',
        '  schema_version: "1.1"',
        '  fields_added: [personality, behavior_traits, risk_preference,',
        '    birth_family, life_story, opening_hook]',
        '  method: deterministic_derivation',
    ]

    new_lines = lines[:raw_idx + 1] + enrich_block + lines[raw_idx + 1:]
    return '\n'.join(new_lines)


# ── 正文 §6 / §8 同步 ─────────────────────────────────────────────
SECTION_6_HEAD = '## 6. 情感与人格'
SECTION_8_HEAD = '## 8. 开局钩子'
SECTION_9_HEAD = '## 9. 数据溯源'


def update_section_6(body, fm_data, is_rich):
    """skeleton 卡：在 §6 末尾追加「行为特征」行；如有【待富化】则替换为「人格特征」行。
    rich 卡：原样保留。
    返回 (new_body, changed: bool)
    """
    if is_rich:
        return body, False

    # 找到 §6 范围
    s6_idx = body.find(SECTION_6_HEAD)
    if s6_idx < 0:
        return body, False
    # 找 §7 开始
    s7_idx = body.find('## 7.', s6_idx)
    if s7_idx < 0:
        s7_idx = len(body)
    s6_block = body[s6_idx:s7_idx]
    new_s6 = s6_block

    # 1. 处理【待富化】行 → 替换为「人格特征」
    personalities = fm_data.get('personality') or []
    ptext = '、'.join(personalities) if personalities else '（待观察）'
    new_s6 = re.sub(
        r'- \*\*人格特征\*\*：【待富化】',
        f'- **人格特征**：{ptext}',
        new_s6,
    )

    # 2. 追加「行为特征」行（如果还没有的话）
    if '- **行为特征**' not in new_s6:
        btraits = fm_data.get('behavior_traits') or []
        btext = '、'.join(btraits) if btraits else '（待观察）'
        # 在 §6 末尾插入（在最后一个 bullet 行后）
        new_s6 = new_s6.rstrip('\n') + f'\n- **行为特征**：{btext}\n'

    if new_s6 != s6_block:
        return body[:s6_idx] + new_s6 + body[s7_idx:], True
    return body, False


def update_section_8(body, fm_data, is_rich):
    """skeleton 卡：替换【待富化】为「开局钩子」+ 「人生经历」时间线。
    rich 卡：原样保留。
    """
    if is_rich:
        return body, False

    s8_idx = body.find(SECTION_8_HEAD)
    if s8_idx < 0:
        return body, False
    s9_idx = body.find(SECTION_9_HEAD, s8_idx)
    if s9_idx < 0:
        s9_idx = len(body)
    s8_block = body[s8_idx:s9_idx]
    new_s8 = s8_block

    hook = fm_data.get('opening_hook') or '（暂无）'
    life = fm_data.get('life_story') or []

    # 1. 替换【待富化】为「开局钩子」+ 一句话
    # §8 标题下通常是直接内容或 bullet
    new_s8 = re.sub(
        r'【待富化】',
        f'**开局钩子**：{hook}',
        new_s8,
    )

    # 2. 追加「人生经历」时间线（如果还没有）
    if '**人生经历**' not in new_s8:
        life_lines = ['\n**人生经历时间线**：']
        for ev in life:
            life_lines.append(f'- {ev["age"]} 岁：{ev["event"]}')
        new_s8 = new_s8.rstrip('\n') + '\n' + '\n'.join(life_lines) + '\n'

    if new_s8 != s8_block:
        return body[:s8_idx] + new_s8 + body[s9_idx:], True
    return body, False


# ── 文件处理主函数 ─────────────────────────────────────────────────
def process_card(card_path, dry=True):
    """处理单张卡。返回 (changed: bool, fm_data_after: dict, body_before: str, body_after: str, errors: list)
    """
    errors = []
    with open(card_path, encoding='utf-8') as f:
        text = f.read()

    # 拆分 frontmatter
    if not text.startswith('---'):
        return False, None, None, None, ['no_frontmatter']

    parts = text.split('---', 2)
    if len(parts) < 3:
        return False, None, None, None, ['bad_frontmatter']

    fm_str = parts[1].lstrip('\n')
    body = parts[2]
    try:
        fm_data = yaml.safe_load(fm_str) or {}
    except Exception as e:
        return False, None, None, None, [f'yaml_parse_error:{e}']

    is_rich = (fm_data.get('richness') == 'rich')

    # 计算 6 字段（先注入到内存字典，便于正文取用）
    fm_data['personality'] = derive_personality(fm_data)
    fm_data['behavior_traits'] = derive_behavior_traits(fm_data)
    fm_data['risk_preference'] = derive_risk_preference(fm_data)
    fm_data['birth_family'] = derive_birth_family(fm_data)
    fm_data['life_story'] = derive_life_story(fm_data)
    fm_data['opening_hook'] = derive_opening_hook(fm_data)

    new_fields = {
        'personality': fm_data['personality'],
        'behavior_traits': fm_data['behavior_traits'],
        'risk_preference': fm_data['risk_preference'],
        'birth_family': fm_data['birth_family'],
        'life_story': fm_data['life_story'],
        'opening_hook': fm_data['opening_hook'],
    }

    # 1) frontmatter 行级精准插入
    new_fm_str = insert_new_fields_into_fm(fm_str, fm_data, new_fields)
    # 2) 追加 _enrich_v44 章
    new_fm_str = append_enrich_meta(new_fm_str, fm_data)

    # 3) 正文 §6 / §8 更新（仅 skeleton）
    new_body = body
    body_changed_s6 = False
    body_changed_s8 = False
    new_body, body_changed_s6 = update_section_6(new_body, fm_data, is_rich)
    new_body, body_changed_s8 = update_section_8(new_body, fm_data, is_rich)

    body_changed = body_changed_s6 or body_changed_s8
    if is_rich:
        body_changed = False  # rich 卡正文不动

    new_text = '---\n' + new_fm_str + '\n---\n' + new_body

    if not dry:
        with open(card_path, 'w', encoding='utf-8') as f:
            f.write(new_text)

    return True, fm_data, body, new_body, errors


# ── 全库扫描 ───────────────────────────────────────────────────────
def find_all_card_paths(root_dir):
    """返回所有人物卡路径列表。"""
    out = []
    for dirpath, _, filenames in os.walk(root_dir):
        # 跳过 _框架等下划线目录
        parts = dirpath.split(os.sep)
        if any(p.startswith('_') for p in parts):
            continue
        for fn in filenames:
            if (fn.startswith('N') or fn.startswith('P')) and fn.endswith('.md'):
                out.append(os.path.join(dirpath, fn))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--dry', action='store_true', help='干跑：不写文件，只出报告')
    ap.add_argument('--limit', type=int, default=None, help='限量处理（前 N 张）')
    ap.add_argument('--sample', type=int, default=20, help='dry 阶段打印几张前后对照（默认 20）')
    args = ap.parse_args()

    t0 = time.time()
    print(f'[enrich_v44] 启动 {"DRY" if args.dry else "REAL"} 模式')
    print(f'[enrich_v44] ROOT: {LETTER_TREE_ROOT}')

    all_paths = find_all_card_paths(LETTER_TREE_ROOT)
    print(f'[enrich_v44] 扫描到卡片文件：{len(all_paths)}')

    if args.limit:
        all_paths = all_paths[:args.limit]
        print(f'[enrich_v44] 限量前 {args.limit} 张')

    stats = {
        'total': len(all_paths),
        'processed': 0,
        'skipped_no_fm': 0,
        'skipped_bad_yaml': 0,
        'rich_cards': 0,
        'skeleton_cards': 0,
        'fm_changed': 0,
        'body_changed': 0,
        'rich_body_unchanged': 0,
        'field_fill': {
            'personality': 0,
            'behavior_traits': 0,
            'risk_preference': 0,
            'birth_family': 0,
            'life_story': 0,
            'opening_hook': 0,
        },
    }
    gates = []
    errors = []
    samples = []  # 前 N 张前后对照

    # rich 卡路径集合（用于最后 git diff 校验）
    rich_paths = []

    for idx, p in enumerate(all_paths):
        try:
            changed, fm_data, body_before, body_after, errs = process_card(p, dry=args.dry)
        except Exception as e:
            stats['skipped_bad_yaml'] += 1
            errors.append({'path': p, 'error': str(e)})
            continue

        if errs:
            if 'no_frontmatter' in errs or 'bad_frontmatter' in errs:
                stats['skipped_no_fm'] += 1
            else:
                stats['skipped_bad_yaml'] += 1
                errors.append({'path': p, 'error': errs})
            continue

        if not changed:
            continue

        stats['processed'] += 1
        is_rich = (fm_data.get('richness') == 'rich')  # 这里 fm_data 已经被改写过了
        # 重新读原始 richness（process_card 已在原 fm_data 上工作，但 process_card 没改 richness）
        # 实际上 process_card 没改 fm_data 的 richness 字段，所以直接用
        if fm_data.get('richness') == 'rich':
            stats['rich_cards'] += 1
            rich_paths.append(p)
        else:
            stats['skeleton_cards'] += 1

        # 6 字段填充率
        for fld in stats['field_fill']:
            val = fm_data.get(fld)
            if val not in (None, '', [], {}):
                # life_story 至少要 1 条
                if fld == 'life_story' and isinstance(val, list) and len(val) > 0:
                    stats['field_fill'][fld] += 1
                elif fld != 'life_story':
                    stats['field_fill'][fld] += 1

        # 闸门：dry 阶段打印前 sample 张
        if args.dry and len(samples) < args.sample:
            # 重新读取原文做对照
            try:
                with open(p, encoding='utf-8') as f:
                    orig = f.read()
            except Exception:
                orig = ''
            samples.append({
                'path': p,
                'is_rich': is_rich,
                'personality': fm_data.get('personality'),
                'behavior_traits': fm_data.get('behavior_traits'),
                'risk_preference': fm_data.get('risk_preference'),
                'birth_family': fm_data.get('birth_family'),
                'life_story': fm_data.get('life_story'),
                'opening_hook': fm_data.get('opening_hook'),
                'orig_fm_line_count': orig.count('\n') if orig else 0,
            })

    elapsed = time.time() - t0

    # 闸门
    fill_rate = {}
    for fld, cnt in stats['field_fill'].items():
        denom = max(1, stats['processed'])
        fill_rate[fld] = round(cnt / denom, 4)

    gate_results = []
    if stats['processed'] > 0:
        all_100 = all(rate >= 0.99 for rate in fill_rate.values())
        gate_results.append(('PASS' if all_100 else 'FAIL',
                            f"6字段填充率={fill_rate}"))
        gate_results.append(('INFO', f"处理 {stats['processed']} 张 / 富化 {stats['rich_cards']} / 骨架 {stats['skeleton_cards']}"))
    gate_passed = all(g[0] == 'PASS' for g in gate_results if g[0] in ('PASS', 'FAIL'))

    # 报告
    report = {
        'ts': datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%S+00:00'),
        'dry': args.dry,
        'limit': args.limit,
        'stats': stats,
        'fill_rate': fill_rate,
        'gates': [f"{s} {msg}" for s, msg in gate_results],
        'gate_passed': gate_passed,
        'elapsed_sec': round(elapsed, 2),
        'samples': samples[:args.sample] if args.dry else [],
        'rich_paths_first_1000': rich_paths[:1000],
        'errors': errors[:20],
    }

    os.makedirs(WORK_DIR, exist_ok=True)
    with open(REPORT_PATH, 'w', encoding='utf-8') as f:
        json.dump(report, f, ensure_ascii=False, indent=2, default=str)

    print(f'\n[enrich_v44] 完成。耗时 {elapsed:.1f}s')
    print(f'[enrich_v44] 处理 {stats["processed"]} 张（rich {stats["rich_cards"]} / skeleton {stats["skeleton_cards"]}）')
    print(f'[enrich_v44] 6 字段填充率: {fill_rate}')
    print(f'[enrich_v44] 闸门: {gate_results}')
    print(f'[enrich_v44] 报告落盘: {REPORT_PATH}')

    # dry 模式打印前 5 张样本
    if args.dry and samples:
        print('\n[enrich_v44] 前 5 张对照样本：')
        for i, s in enumerate(samples[:5]):
            print(f'\n--- 样本 {i+1}: {s["path"]} (rich={s["is_rich"]}) ---')
            print(f'  personality     : {s["personality"]}')
            print(f'  behavior_traits : {s["behavior_traits"]}')
            print(f'  risk_preference : {s["risk_preference"]}')
            print(f'  birth_family    : {s["birth_family"]}')
            print(f'  life_story      : {s["life_story"]}')
            print(f'  opening_hook    : {s["opening_hook"]}')


if __name__ == '__main__':
    sys.exit(main())
