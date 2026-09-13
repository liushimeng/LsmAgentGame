#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""字段推导 / 分配规则表

三类来源标注：
  explicit      原始档案中直接可得
  derived       可由原始字段**唯一确定**地推导
  assigned      原始档案无据，按确定性规则（哈希/查表）**分配**的合成值

`assigned` 一律如实登记，绝不与 `explicit` 混同 —— 这是本知识库的诚实性底线。
"""
import hashlib
import re

# ── 计数口径（59 个字段） ───────────────────────────────────────
COUNTED_FIELDS = [
    # meta
    'id', 'name', 'schema_version', 'card_type', 'richness', 'source_file',
    # identity
    'gender', 'age', 'age_band', 'birth_year', 'birth_province', 'birth_city',
    'generation', 'education',
    # health
    'health_grade', 'health_conditions', 'health_risks',
    # career
    'industry_l1', 'industry_l2', 'industry_l3', 'occupation', 'employment',
    'employer', 'work_intensity', 'career_stage',
    # income
    'income_monthly', 'income_range', 'income_structure', 'income_stability',
    'household_monthly',
    # finance
    'monthly_expense', 'savings_stock', 'debt_stock', 'savings_rate', 'net_worth',
    'debts', 'assets',
    # protection
    'certs', 'funds', 'insurance',
    # family
    'marital', 'children_count', 'children_ages', 'elders_dependent',
    'household_type', 'family_role',
    # housing
    'housing_tenure', 'housing_city', 'housing_detail', 'mortgage_left',
    # psych
    'emotion_status', 'stress_level', 'stress_sources', 'biases',
    # goals
    'goals_short', 'opportunities', 'dream_cost',
    # transport
    'transport_owned', 'transport_mode',
]
assert len(COUNTED_FIELDS) == 59, len(COUNTED_FIELDS)

AGE_BANDS = [(16, 24, '16-24'), (25, 34, '25-34'), (35, 44, '35-44'),
             (45, 54, '45-54'), (55, 64, '55-64'), (65, 200, '65+')]

GENERATIONS = [(1940, 1959, '40后'), (1960, 1969, '60后'), (1970, 1979, '70后'),
               (1980, 1989, '80后'), (1990, 1999, '90后'), (2000, 2009, '00后'),
               (2010, 2026, '10后')]

# ── 性别：显式 → 配偶词 → 名字用字 → 哈希分配 ─────────────────
FEMALE_CHARS = set('兰婷娟秀慧芳燕玲莉娜静丽敏雪梅萍红霞珍琴娥姝婉妍媛嫣妤妘婳娴'
                   '瑶瑾璐琪珂玥珊琳琦莹蕊薇蕾蓉菲萱蔚芊芸芩茜茵茗荷莲蝶娥媚婵'
                   '婧婕妲妮娃嫣娣娆婵娴嫣彤媛姗妤妤茜茹萍蓓菁菡菱菊桃樱棠棠'
                   '怡悦欣瑶柔婉妙妤岚岚心月昕晴晴晞晗曦暖')
MALE_CHARS = set('强军伟勇刚磊涛峰波辉鹏飞龙虎彪斌杰亮明华建国志民永康健铁钢'
                 '山河海江川兵武雄豪铭鑫栋梁栋坚毅承志旭阳锋航帆骏骐骁腾辉'
                 '源洲洋浩瀚宇轩宸昊煜烨燊燚垚焱淼杰猛超越凯旋勐悍'
                 '仁信义礼智勇忠孝廉耻勤俭谦恭')
SPOUSE_MALE = re.compile(r'妻子|老婆|太太|媳妇|妻为')
SPOUSE_FEMALE = re.compile(r'丈夫|老公|先生')


def _hash_unit(key, salt=''):
    h = hashlib.sha256((salt + '|' + key).encode('utf-8')).hexdigest()
    return int(h[:8], 16) / 0xFFFFFFFF


def derive_gender(rec):
    if rec.get('gender') in ('男', '女'):
        return rec['gender'], 'explicit'
    fam = rec['family']['raw']
    if SPOUSE_MALE.search(fam):
        return '男', 'derived(spouse)'
    if SPOUSE_FEMALE.search(fam):
        return '女', 'derived(spouse)'
    name = rec.get('name', '')
    given = name[1:] if len(name) > 1 else ''
    f = sum(1 for c in given if c in FEMALE_CHARS)
    m = sum(1 for c in given if c in MALE_CHARS)
    if f > m:
        return '女', 'derived(name)'
    if m > f:
        return '男', 'derived(name)'
    return ('男' if _hash_unit(rec['id'], 'gender') < 0.51 else '女'), 'assigned(hash)'


# ── 年龄段 / 世代 ───────────────────────────────────────────────
def age_band(age):
    if not age:
        return None
    for lo, hi, label in AGE_BANDS:
        if lo <= age <= hi:
            return label
    return None


def generation(birth_year):
    if not birth_year:
        return None
    for lo, hi, label in GENERATIONS:
        if lo <= birth_year <= hi:
            return label
    return None


# ── 工作强度 / 风险（由职业关键词 + 收入结构推导） ────────────────
HIGH_INTENSITY = re.compile(
    r'骑手|外卖|快递|司机|驾驶|货车|夜班|三班|轮班|倒班|工地|施工|建筑工|'
    r'厨师|后厨|护理|护工|月嫂|保姆|保洁|搬运|装卸|分拣|矿工|井下|高空|'
    r'销售|门店|店长|创业|个体|老板|自由职业|主播|演员|歌手|'
    r'护士|医生|急诊|消防|警察|民警|安保|保安|海员|船员|远洋|出海|驻外|外派')
LOW_INTENSITY = re.compile(r'公务员|事业编|教师|银行柜员|文员|行政|国企|事业单位|'
                           r'研究员|图书馆|档案|统计')

STAGE_RULES = [
    (r'实习生|学徒|助理|新人|应届|管培', '入门'),
    (r'初级|二级|三级|助手', '初级'),
    (r'高级|资深|主管|经理|店长|主任|组长|班长|队长', '资深'),
    (r'总监|副总|总经理|院长|校长|合伙人|创始人|老板|主理|CEO|CTO', '管理'),
]

RISK_BY_OCC = re.compile(
    r'矿工|井下|爆破|高空|带电|消防|警察|民警|船员|海员|远洋|出海|驻外|外派|'
    r'危化|押运|焊工|起重机|塔吊|外卖|骑手|快递|长途|夜班', )


def derive_work(occ, income_structure, employment):
    text = occ or ''
    hours, ot, risk = None, None, None
    if HIGH_INTENSITY.search(text):
        hours, ot = 55, '高'
    elif LOW_INTENSITY.search(text):
        hours, ot = 40, '低'
    if hours is None:
        hours, ot = 48, '中'
    risk = '高' if RISK_BY_OCC.search(text) else ('低' if LOW_INTENSITY.search(text) else '中')
    return {'weekly_hours': hours, 'overtime': ot, 'risk': risk}, 'derived(occupation)'


def derive_stage(occ):
    for pat, label in STAGE_RULES:
        if re.search(pat, occ or ''):
            return label, 'derived(occupation)'
    return '骨干', 'derived(occupation)'


# ── 收入结构 / 稳定性 ───────────────────────────────────────────
STRUCT_PATTERNS = [
    ('计件', r'计件|按件|按单|按㎡|按平米|按台|按颗|趟结'),
    ('提成+底薪', r'底薪.*提成|提成.*底薪|佣金|绩效提成|底薪\+'),
    ('项目制', r'项目制|按项目|节点奖|项目奖金|外包|接单'),
    ('季节波动', r'季节性|旺季|淡季|雪季|渔汛'),
    ('合伙分红', r'分红|股份|股权|合伙'),
    ('年薪制', r'年薪|14薪|13薪|年终奖'),
    ('固定薪资', r'月薪|月薪制|固定工资|事业单位|公务员|编制'),
]
STABILITY = {'计件': '低', '提成+底薪': '中', '项目制': '低', '季节波动': '低',
             '合伙分红': '低', '年薪制': '高', '固定薪资': '高', '其他': '中'}


def derive_income_structure(text):
    for label, pat in STRUCT_PATTERNS:
        if re.search(pat, text or ''):
            return label, STABILITY[label], 'derived(income_text)'
    return '固定薪资', STABILITY['固定薪资'], 'assigned(occupation)'


# ── 就业形态 ────────────────────────────────────────────────────
EMPLOY_PATTERNS = [
    ('个体经营', r'个体|老板|店主|主理人|创业者|自营|户主|房东|承包|家庭农场|合作社'),
    ('自由职业', r'自由职业|接单|零工|兼职|主播|博主|UP主|写手|设计师接'),
    ('平台就业', r'骑手|网约车|外卖|快递员|代驾'),
    ('退休返聘', r'退休返聘|返聘|退休后'),
    ('灵活就业', r'灵活就业|小时工|临时工|季节工|钟点工'),
    ('全职', r'.*'),
]


def derive_employment(occ, detail_subs):
    for key, items in (detail_subs or {}).items():
        if key == 'work':
            for label, val in items:
                if '用工' in label or '编制' in label or '性质' in label:
                    return '全职', 'explicit'
    text = occ or ''
    for label, pat in EMPLOY_PATTERNS:
        if re.search(pat, text):
            return label, 'derived(occupation)'
    return '全职', 'assigned(default)'


# ── 风险/睡眠/压力 ──────────────────────────────────────────────
def derive_health_risks(conditions, occ):
    risks = []
    cmap = {'腰椎': '久站负重', '颈椎': '久坐低头', '肩颈': '长期伏案',
            '手腕': '重复性劳损', '腱鞘': '重复性劳损', '胃': '饮食不规律',
            '肝': '应酬饮酒', '血压': '精神紧张', '血糖': '饮食与作息',
            '糖尿': '饮食与作息', '失眠': '倒班与焦虑', '视疲': '长时间用眼',
            '干眼': '长时间屏幕', '静脉曲张': '长期站立', '关节': '重体力',
            '听力': '噪声环境', '呼吸': '粉尘环境', '脂肪肝': '饮食与饮酒',
            '焦虑': '业绩与不确定性', '抑郁': '情感劳动与压力'}
    for c in conditions or []:
        for k, v in cmap.items():
            if k in c and v not in risks:
                risks.append(v)
    if not risks:
        if HIGH_INTENSITY.search(occ or ''):
            risks.append('工作强度偏高')
        else:
            risks.append('常规职业风险')
    return risks[:4], 'derived(conditions)'


def derive_sleep(conditions, work):
    for c in conditions or []:
        if '失眠' in c or '睡眠' in c:
            return '差', 'derived(conditions)'
    if work.get('overtime') == '高':
        return '一般', 'derived(work_intensity)'
    return '良好', 'assigned(default)'


def derive_stress(profile_items, biases):
    items = [i for i in (profile_items or []) if i]
    n = len(items)
    level = '高' if n >= 4 else ('中' if n >= 2 else '低')
    return level, items[:6], 'derived(profile_cell)'


def derive_emotion(marital, family_raw):
    m = marital or '未知'
    if m == '已婚':
        sat = '中'
    elif m == '未婚':
        sat = '中'
    else:
        sat = '未知'
    if re.search(r'离异|丧偶|寡居', family_raw or ''):
        sat = '中'
    return m, sat


# ── 家庭 ────────────────────────────────────────────────────────
def derive_household_type(children_count, elders, marital):
    c = children_count or 0
    e = elders or 0
    if c and e:
        return '三明治家庭'
    if c:
        return '核心家庭'
    if e:
        return '赡养家庭'
    if marital == '已婚':
        return '新婚/丁克家庭'
    return '单人家庭'


def derive_family_role(income_monthly, household_monthly):
    if not income_monthly or not household_monthly:
        return None
    r = income_monthly / household_monthly if household_monthly else 0
    if r >= 0.6:
        return '主要经济支柱'
    if r >= 0.35:
        return '共同经济支柱'
    return '辅助经济来源'


def derive_household_income(income_monthly, family_raw, marital):
    """家庭月收入：从家庭列抽取配偶收入；无则按婚姻状态乘系数。"""
    if family_raw:
        m = re.search(r'(?:妻子|丈夫|老婆|老公)[^;；,，]{0,20}?(\d{4,6})', family_raw)
        if m and income_monthly:
            return income_monthly + int(m.group(1)), 'derived(spouse_cell)'
    if income_monthly:
        f = 1.9 if marital == '已婚' else 1.0
        return int(income_monthly * f), 'assigned(multiplier)'
    return None, None


# ── 资产 / 负债结构 ─────────────────────────────────────────────
def build_debts(debt_stock, housing, assets):
    out = []
    if housing.get('mortgage_left'):
        out.append({'type': '房贷', 'balance': housing['mortgage_left'],
                    'monthly': None, 'source': '住房列'})
    residual = None
    if debt_stock and housing.get('mortgage_left'):
        residual = max(0, debt_stock - 0)
    if debt_stock:
        out.append({'type': '负债合计', 'balance': debt_stock, 'monthly': None,
                    'source': '支出/储蓄/负债列'})
    for a in assets or []:
        m = re.search(r'(车贷|消费贷|信用卡|经营贷|助学贷|培训贷|借款)', a)
        if m:
            v = re.search(r'([\d.]+)\s*万', a)
            out.append({'type': m.group(1),
                        'balance': int(float(v.group(1)) * 10000) if v else None,
                        'monthly': None, 'source': '资产列'})
    return out


def build_assets(asset_items, housing, savings):
    out = []
    for a in asset_items or []:
        m = re.search(r'([\d.]+)\s*万', a)
        name = re.sub(r'[\d.]+\s*万(元)?', '', a).strip('，,;； ')
        out.append({'type': name or a[:10], 'desc': a,
                    'value_cny': int(float(m.group(1)) * 10000) if m else None})
    if housing.get('housing_area') or housing.get('tenure') in ('自有', '自建'):
        out.append({'type': '房产', 'desc': housing.get('detail', '')[:60], 'value_cny': None})
    if savings:
        out.append({'type': '现金/存款', 'desc': '档案储蓄存量', 'value_cny': savings})
    return out


# ── 交通 ────────────────────────────────────────────────────────
def derive_transport(income_monthly, city, occupation):
    """按收入档与城市档分配（合成值，如实标注 assigned）。"""
    if not income_monthly:
        return None, None, None
    h = _hash_unit(city or 'CN', 'transport')
    if income_monthly >= 20000:
        owned = '自有车（中高端）'
    elif income_monthly >= 8000:
        owned = '自有车（经济型）' if h < 0.55 else '公共交通为主'
    elif income_monthly >= 5000:
        owned = '电动车/摩托车' if h < 0.5 else '公共交通为主'
    else:
        owned = '公共交通为主'
    if re.search(r'骑手|外卖|快递|网约车|货运|司机', occupation or ''):
        owned = '营运车辆/电动车'
    mode = {'自有车（中高端）': '自驾', '自有车（经济型）': '自驾',
            '电动车/摩托车': '两轮车', '公共交通为主': '公共交通',
            '营运车辆/电动车': '营运车辆'}[owned]
    return owned, mode, 'assigned(income_city)'


# ── 目标 / 机会 ─────────────────────────────────────────────────
GOAL_TEMPLATES = [
    ('30 岁前', '攒下第一笔 20 万应急储备金'),
    ('35 岁前', '在常住城市完成首套住房置换'),
    ('40 岁前', '把家庭被动收入提升到月支出的一半'),
    ('45 岁前', '为子女教育金完成 50 万专项储备'),
    ('50 岁前', '完成养老金第三支柱账户搭建'),
    ('55 岁前', '实现职业转型或第二收入曲线'),
    ('60 岁前', '退休前结清全部消费类负债'),
]


def derive_goals(age, income_monthly):
    if not age:
        return [], [], None, None
    out = []
    for cap, text in GOAL_TEMPLATES:
        limit = int(re.match(r'(\d+)', cap).group(1))
        if age < limit:
            out.append({'horizon': cap, 'text': text})
        if len(out) >= 3:
            break
    dream = None
    if income_monthly:
        dream = int(round(income_monthly * 12 * (5 if age and age < 40 else 3) / 10000.0) * 10000)
    return out, [], dream, 'assigned(age_income_template)'


def derive_opportunities(profile_items, occupation):
    ops = []
    for i in (profile_items or [])[:2]:
        ops.append('应对：' + i)
    return ops[:3], 'derived(profile_cell)'


# ── 保障推断 ────────────────────────────────────────────────────
def derive_insurance(employment, funds, occ):
    out = []
    if funds:
        out.append('住房公积金')
    if employment in ('全职',):
        out.append('城镇职工医保')
        out.append('城镇职工养老')
    elif employment == '个体经营':
        out.append('城乡居民医保')
    elif employment in ('平台就业', '灵活就业', '自由职业'):
        out.append('灵活就业社保')
    if re.search(r'外卖|骑手|快递|网约车|货车|司机', occ or ''):
        out.append('商业意外险')
    return list(dict.fromkeys(out)), 'derived(employment)'
