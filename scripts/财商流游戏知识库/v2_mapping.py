#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""v2.0 数字编号映射表（v4.0 字母 → v2.0 数字）

来源：tmpPlan/财商流游戏-玩家职业设计-v2.0-数字编号规范-20260913-01.md §4

提供:
    L1_TO_NUM         dict  v4.0 L1 字母 -> v2.0 L1 数字 (例 'A' -> '01')
    NUM_TO_L1         dict  v2.0 L1 数字 -> v4.0 L1 字母 (例 '01' -> 'A')
    NUM_TO_L1_NAME    dict  v2.0 L1 数字 -> 行业中文名
    SPLIT_RULES       dict  拆分判定词表（F/I/G/H/J/K 拆到 19）
    dual_lookup()     双模式查表函数
    resolve_l1()      对强行合并的行业，按判定词决定最终数字 L1

过渡期所有脚本都应 import 本模块而不是硬编码行业码。
"""
import re

# === v4.0 L1 字母 → v2.0 L1 数字（一对一直接对应）===
L1_TO_NUM = {
    'A': '01', 'C': '03', 'O': '12', 'M': '13',
    'Q': '07', 'R': '14', 'S': '15', 'T': '09', 'V': '11',
    # 强行合并（默认归入主号，拆分由 resolve_l1() 完成）
    'B': '02', 'D': '04', 'E': '04', 'F': '10', 'G': '02',
    'H': '02', 'I': '06', 'J': '05', 'K': '02', 'L': '17',
    'N': '05', 'P': '06', 'U': '10', 'W': '16', 'X': '16',
    'Y': '17', 'Z': '19',
}

NUM_TO_L1 = {v: k for k, v in L1_TO_NUM.items()}

# === v2.0 L1 数字 → 行业中文名 ===
NUM_TO_L1_NAME = {
    '01': '农林牧渔',
    '02': '采矿与能源',
    '03': '食品饮料',
    '04': '纺织服饰',
    '05': '交通运输',
    '06': '信息技术与互联网',
    '07': '金融与保险',
    '08': '房地产与租赁',
    '09': '教育与培训',
    '10': '医疗卫生',
    '11': '文化传媒与内容创作',
    '12': '住宿与餐饮',
    '13': '零售与电商',
    '14': '专业服务',
    '15': '科研与技术服务',
    '16': '公共管理与事业单位',
    '17': '居民生活服务',
    '18': '灵活就业与平台经济',
    '19': '新兴职业',
    '20': '学生/待业/退休/无业',
    '99': '跨行业混合身份',
}

# === 拆分判定词表（v4.0 → 19 行业）===
# 关键词命中 → 该卡归入 19 新兴职业；否则按 L1_TO_NUM 默认归入主号
#
# 原则：判定词必须指向**真正的新兴职业**，而非通用工艺词。
# 例如"数控"在 H02-金属加工里是传统工种（归 02），不是新兴；
# 但"工业机器人"在 H10-数控与智能制造里是新兴特征（归 19）。
# 因此判定规则按 (L3 目录名 + 职业字段) 同时命中才归 19。
SPLIT_RULES = {
    'F': {  # 医药与生物制造
        'to_19': [r'CRO', r'CDMO', r'生物制造', r'基因工程', r'合成生物学', r'细胞培养'],
    },
    'G': {  # 化工与新材料
        'to_19': [r'半导体材料', r'石墨烯', r'碳纤维', r'新能源材料', r'光刻胶', r'锂电材料'],
    },
    'H': {  # 金属制品与通用机械
        # 仅在 L3 目录名含"机器人/工业母机"时归 19；通用"数控加工"归 02
        'to_19': [r'工业机器人', r'机器人', r'工业母机', r'减速机', r'协作机器人'],
    },
    'I': {  # 电子半导体与仪器仪表
        # 半导体/晶圆/光刻/封测是新兴；通用电子制造归 06
        'to_19': [r'半导体', r'晶圆', r'光刻', r'封测', r'\bIC\b', r'芯片设计', r'传感器研发'],
    },
    'J': {  # 汽车与交通装备
        # 整车/4S 归 05；轨交/船舶/航空/低空归 19
        'to_19': [r'轨道', r'高铁', r'地铁', r'船舶', r'航空', r'航天', r'无人机', r'低空经济'],
    },
    'K': {  # 能源与电力
        # 光伏/风电/储能/氢能归 19；传统发电/输变电归 02
        'to_19': [r'光伏', r'风电', r'储能', r'充电桩', r'氢能', r'新能源运维'],
    },
}


def dual_lookup(value: str) -> str:
    """双模式查表：输入字母返回数字，输入数字返回字母。

    Args:
        value: 'A' 或 '01' 等

    Returns:
        对应的另一种形式

    Examples:
        >>> dual_lookup('A')
        '01'
        >>> dual_lookup('01')
        'A'
    """
    if not value:
        return value
    v = str(value).strip()
    if re.match(r'^[A-Z]$', v):
        return L1_TO_NUM.get(v, v)
    if re.match(r'^\d{2}$', v):
        return NUM_TO_L1.get(v, v)
    return v


def resolve_l1(letter: str, occupation: str = '', l3_name: str = '') -> str:
    """对强行合并的行业（F/I/G/H/J/K/Z），按判定词决定最终数字 L1。

    判定原则：仅在 occupation 字段命中关键词时归 19，**不**依赖 l3_name。
    原因：l3_name 是分类标签（如"数控加工"），覆盖过宽；occupation 字段
    更精确（如"工业机器人调试工程师"）。

    Args:
        letter: v4.0 L1 字母 (e.g. 'F')
        occupation: 职业字符串 (e.g. 'CRO 临床监查员')
        l3_name: L3 中文名 (e.g. 'CRO 服务')，保留参数兼容性但暂不使用

    Returns:
        v2.0 L1 数字 (e.g. '19' 或 '10')

    Notes:
        - 直接对应的字母 (A/C/O/M/Q/R/S/T/V/N/P/W/X) 走 L1_TO_NUM
        - B/D/E/L/U/Y 走 L1_TO_NUM（已确定归并）
        - F/I/G/H/J/K 走判定词：occupation 命中 to_19 关键词 → '19'；否则按 L1_TO_NUM
        - Z 默认归 19（Z 是新兴交叉职业大箩筐）
    """
    if not letter:
        return ''
    letter = str(letter).strip().upper()
    # 直接对应或已确定的强行合并
    if letter not in SPLIT_RULES:
        if letter == 'Z':
            return '19'  # Z 默认归 19
        return L1_TO_NUM.get(letter, '')
    # F/I/G/H/J/K 走判定词（**仅 occupation 字段**，l3_name 不用）
    text = occupation or ''
    for kw_re in SPLIT_RULES[letter]['to_19']:
        if re.search(kw_re, text, re.IGNORECASE):
            return '19'
    return L1_TO_NUM[letter]


def l2_num_for(l1_letter: str, l1_num: str, l2_letter: str) -> str:
    """构造 v2.0 L2 数字码 = <L1 数字 2 位><L2 序号 2 位>。

    Args:
        l1_letter: v4.0 L1 字母 (e.g. 'F')
        l1_num: v2.0 L1 数字 (e.g. '10' 或 '19')
        l2_letter: v4.0 L2 字母 (e.g. 'F03' → 取末 2 位 '03')

    Returns:
        v2.0 L2 数字码 (e.g. '1003')
    """
    if not l2_letter:
        return l1_num + '00'
    s = str(l2_letter).strip()
    m = re.search(r'(\d{2})$', s)
    seq = m.group(1) if m else '00'
    return f'{l1_num}{seq}'


def l3_num_for(l2_num: str, l3_letter: str) -> str:
    """构造 v2.0 L3 数字码 = <L2 数字 4 位><L3 序号 2 位>。

    Args:
        l2_num: v2.0 L2 数字 (e.g. '1003')
        l3_letter: v4.0 L3 字母 (e.g. 'F0305' → 取末 2 位 '05')

    Returns:
        v2.0 L3 数字码 (e.g. '100305')
    """
    if not l3_letter:
        return l2_num + '00'
    s = str(l3_letter).strip()
    m = re.search(r'(\d{2})$', s)
    seq = m.group(1) if m else '00'
    return f'{l2_num}{seq}'


if __name__ == '__main__':
    # 简单自测
    import sys
    print('=== v2.0 行业映射自测 ===')
    print(f'L1_TO_NUM 覆盖: {len(L1_TO_NUM)} 个 v4.0 L1')
    print(f'NUM_TO_L1_NAME 覆盖: {len(NUM_TO_L1_NAME)} 个 v2.0 L1')
    print(f'SPLIT_RULES 覆盖: {len(SPLIT_RULES)} 个拆分行业')
    print()
    print('=== dual_lookup 自测 ===')
    for v in ['A', 'Z', '01', '99']:
        print(f'  {v} → {dual_lookup(v)}')
    print()
    print('=== resolve_l1 自测 ===')
    test_cases = [
        ('F', 'CRO 临床监查员', 'CRO 服务', '19'),
        ('F', '药剂师', '医院药房', '10'),
        ('I', '半导体晶圆工程师', '晶圆制造', '19'),
        ('I', '嵌入式软件工程师', '消费电子', '06'),
        ('K', '光伏运维工程师', '光伏电站', '19'),
        ('K', '火力发电运行员', '燃煤发电', '02'),
        ('Z', 'AI 训练师', 'AI 标注', '19'),
        ('A', '水稻种植户', '水稻种植', '01'),
    ]
    for letter, occ, l3_name, expected in test_cases:
        got = resolve_l1(letter, occ, l3_name)
        flag = '✓' if got == expected else '✗'
        print(f'  {flag} resolve_l1({letter!r}, {occ!r}) = {got} (expected {expected})')
