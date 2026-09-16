#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""lib_dim_v45.py —— v4.5「多维度人群档案」共享生成器（唯一事实来源）

背景（2026-09-16，v4.5）：
  v4.4 之前 `玩家职业设计/` 只有**一个维度**：行业维度（A–Z 26 域五层树，75,115 张卡）。
  真实世界的财富流动同样由「**非行业身份**」驱动 —— 在校学生、失业待业、退休、
  全职照料者、灵活就业/平台零工、食利与收租者、拆迁与继承暴富者、失信与高负债者、
  连续创业者、职业投资人……这些人群在行业树里**无处安放**，却是财商模拟的关键样本。

  v4.5 起引入 **维度②–⑥**（见 `_框架/12-多维度人群档案体系_v4.5.md`），
  与行业维度**并列**放在 `玩家职业设计/` 下，路径同样保持五段式：

      <维度根>/<维度L2>/<维度L3>/<L4地区档>/<L5年龄段>/<编号>-<姓名>.md

  ⚠️ 维度根目录**不得**以 `_` 开头：`ServerGo/game/wealth/profession/loader.go`
     对 `_` 前缀目录 SkipDir，下划线目录里的卡**不会进入游戏卡池**。

设计约束（所有维度批次必须遵守）：
  1. **一人一卡、一卡一树**：同一个人只存在于一个维度树中，禁止跨维度复制人物
     （跨维度切片靠 frontmatter 字段 + `_框架/10-维度索引/` 索引文档实现）。
  2. **化名与卡号预分配**：化名取自 `work/v45/names_<block>.txt`
     （由 `alloc_names_v45.py` 保证与既有 75k 卡零碰撞），卡号在各维度独占区间内递增。
  3. **Schema v1.1 出生即完整**：直接产出 6 个全息画像字段
     （personality / behavior_traits / risk_preference / birth_family / life_story /
     opening_hook），不再需要事后跑 `enrich_holistic_v44.py`。
  4. **加载器可玩性**：`income_monthly` 必须 > 0（语义 = 月度可支配现金流入，
     失业/学生/退休者以 `income_composition` 说明构成：失业保险金 / 家庭转移支付 /
     兼职零工 / 养老金 / 财产性收入），否则 Go 加载器会判定为无效卡并跳过。
  5. **行业归属仍可标注**：`industry_l1/l2/l3` 保留（无行业归属者置 `NA/NA00/NA0000`
     并写 `industry_note`），使维度卡同样能被行业维度索引切片。

用法（各维度 driver 只需 3 行）：
    from lib_dim_v45 import DimGenerator
    g = DimGenerator(dim='DIM2', start_id=9020000, batch_tag='v4.5-dim2', seed=20260916)
    report = g.gen_from_pool(POOLS, count=1500)
"""
import collections
import json
import os
import random
import re
import sys
import time

import yaml

THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if THIS_DIR not in sys.path:
    sys.path.insert(0, THIS_DIR)

import gen_batch  # noqa: E402
from gen_batch import Generator, PROVINCES, _seg  # noqa: E402
import enrich_holistic_v44 as H  # noqa: E402

REPO_ROOT = os.path.abspath(os.path.join(THIS_DIR, '..', '..'))
CARD_ROOT = os.path.join(REPO_ROOT, 'docs', '财商流游戏', '玩家职业设计')
NAMES_DIR = os.path.join(THIS_DIR, 'work', 'v45')

# ── 维度注册表（唯一事实来源；新增维度必须在此登记）──────────────────
# id_start/id_end：卡号独占区间（含端点），跨维度不得重叠。
# block：`work/v45/names_<block>.txt` 化名块名。
DIMENSIONS = {
    'DIM1': {
        'root': None,  # 维度① = 行业维度（A–Z 既有五层树），不由本生成器写
        'name': '行业维度',
        'id_start': 1, 'id_end': 9019999,
        'block': None,
        'doc': '03-行业分类体系-ICG.md',
    },
    'DIM2': {
        'root': '维度2-身份与就业状态',
        'name': '身份与就业状态',
        'id_start': 9020000, 'id_end': 9024999,
        'block': 'dim2',
        'doc': '12-多维度人群档案体系_v4.5.md',
    },
    'DIM3': {
        'root': '维度3-财富阶层与资产负债',
        'name': '财富阶层与资产负债',
        'id_start': 9025000, 'id_end': 9029999,
        'block': 'dim3',
        'doc': '12-多维度人群档案体系_v4.5.md',
    },
    'DIM4': {
        'root': '维度4-家庭生活与消费财商',
        'name': '家庭生活与消费财商',
        'id_start': 9030000, 'id_end': 9034999,
        'block': 'dim4',
        'doc': '12-多维度人群档案体系_v4.5.md',
    },
    'DIM5': {
        'root': '维度5-财商观念与行为偏差',
        'name': '财商观念与行为偏差',
        'id_start': 9035000, 'id_end': 9039999,
        'block': 'dim5',
        'doc': '12-多维度人群档案体系_v4.5.md',
    },
    'DIM6': {
        'root': '维度6-调研驱动真实人群样本',
        'name': '调研驱动真实人群样本',
        'id_start': 9040000, 'id_end': 9044999,
        'block': 'dim6',
        'doc': '13-2026真实世界人群与财商数据锚点.md',
    },
    'DIM7': {
        'root': '维度7-行业维度扩容新职业族',
        'name': '行业维度扩容（新职业族）',
        'id_start': 9045000, 'id_end': 9049999,
        'block': 'dim7',
        'doc': '12-多维度人群档案体系_v4.5.md',
    },
}

# 无行业归属人群的行业码占位（v2.0 数字编号 '20' = 学生/待业/退休/无业）
NA_INDUSTRY = ('NA', 'NA00', 'NA0000')
NA_OCC_NUM = '20'


def dimension(dim_code):
    """取维度注册项（不存在即报错，避免拼写导致目录漂移）。"""
    key = dim_code.strip().upper()
    if key not in DIMENSIONS:
        raise KeyError('未登记维度 %r；请先在 lib_dim_v45.DIMENSIONS 注册' % dim_code)
    return DIMENSIONS[key]


def load_names(block, needed):
    """读取预分配化名块，返回 [(name, gender), ...]。

    兼容两种行格式：`<化名>\\t<性别>`（v4.5 标准）与纯 `<化名>`（性别未知 → '?')。
    """
    fp = os.path.join(NAMES_DIR, 'names_%s.txt' % block)
    if not os.path.exists(fp):
        raise FileNotFoundError('化名块缺失：%s（先跑 alloc_names_v45.py）' % fp)
    with open(fp, encoding='utf-8') as f:
        names = []
        for ln in f:
            ln = ln.rstrip('\n')
            if not ln.strip():
                continue
            parts = ln.split('\t')
            nm = parts[0].strip()
            gd = parts[1].strip() if len(parts) > 1 else '?'
            if nm:
                names.append((nm, gd))
    if len(names) < needed:
        raise ValueError('化名块 %s 仅 %d 名，不足 %d' % (block, len(names), needed))
    return names


# ── 城市 → 省份（籍贯/现居地一致性用；覆盖 gen_batch.CITIES 全部城市）──────
CITY_PROVINCE = {
    '北京': '北京', '天津': '天津', '石家庄': '河北', '太原': '山西',
    '呼和浩特': '内蒙古', '唐山': '河北', '保定': '河北', '邯郸': '河北',
    '沈阳': '辽宁', '大连': '辽宁', '哈尔滨': '黑龙江', '长春': '吉林',
    '大庆': '黑龙江', '吉林': '吉林',
    '上海': '上海', '南京': '江苏', '苏州': '江苏', '杭州': '浙江', '宁波': '浙江',
    '合肥': '安徽', '福州': '福建', '厦门': '福建', '济南': '山东', '青岛': '山东',
    '南昌': '江西',
    '郑州': '河南', '武汉': '湖北', '长沙': '湖南', '洛阳': '河南', '襄阳': '湖北',
    '宜昌': '湖北',
    '广州': '广东', '深圳': '广东', '佛山': '广东', '东莞': '广东', '南宁': '广西',
    '海口': '海南', '珠海': '广东', '惠州': '广东',
    '重庆': '重庆', '成都': '四川', '贵阳': '贵州', '昆明': '云南', '绵阳': '四川',
    '宜宾': '四川',
    '西安': '陕西', '兰州': '甘肃', '西宁': '青海', '银川': '宁夏',
    '乌鲁木齐': '新疆', '咸阳': '陕西',
    '香港': '香港', '澳门': '澳门', '台北': '台湾', '新加坡': '新加坡',
    '东京': '日本', '悉尼': '澳大利亚', '温哥华': '加拿大',
}
OVERSEAS_CITIES = {'香港', '澳门', '台北', '新加坡', '东京', '悉尼', '温哥华'}

# 非工资就业形态 → 工作强度/职业阶段/单位/公积金 的现实口径修正
NON_WORKING = {
    '退休': {'weekly_hours': 0, 'overtime': '无', 'risk': '低', 'stage': '已退休',
             'employer': '原单位（已退休）', 'funds': []},
    '学生': {'weekly_hours': 12, 'overtime': '无', 'risk': '低', 'stage': '在校',
             'employer': '在读院校', 'funds': []},
    '失业': {'weekly_hours': 0, 'overtime': '无', 'risk': '低', 'stage': '待业',
             'employer': '无（失业登记）', 'funds': []},
    '待业': {'weekly_hours': 0, 'overtime': '无', 'risk': '低', 'stage': '待业',
             'employer': '无（求职中）', 'funds': []},
    '无业': {'weekly_hours': 0, 'overtime': '无', 'risk': '低', 'stage': '无业',
             'employer': '无', 'funds': []},
    '全职照料': {'weekly_hours': 60, 'overtime': '高', 'risk': '中', 'stage': '家庭照料',
                 'employer': '家庭（无酬照料）', 'funds': []},
}

# 人生目标兜底池（按人生阶段；`derive_goals` 对高龄/无业常返回空 → 必须补）
GOALS_BY_STAGE = {
    '在校': [('1 年内', '把专业课绩点刷到保研线'), ('1 年内', '攒下第一笔 5000 元实习存款'),
             ('5 年内', '毕业三年内在一线城市站稳脚跟')],
    '待业': [('3 个月内', '拿到一份能覆盖生活费的 offer'), ('1 年内', '还清助学/消费分期'),
             ('5 年内', '建立 6 个月应急金')],
    '已退休': [('1 年内', '把慢病用药支出压到养老金 15% 以内'), ('1 年内', '完成一次家庭资产盘点与遗嘱安排'),
               ('5 年内', '保持每年一次长途旅行且不动用本金')],
    '家庭照料': [('1 年内', '给自己配一份百万医疗险'), ('1 年内', '建立每月 800 元的个人小金库'),
                 ('5 年内', '重返职场或做成一门居家副业')],
    '无业': [('3 个月内', '找到稳定现金流来源'), ('1 年内', '攒下 1 万元应急金'),
             ('5 年内', '完成一次职业技能转型')],
    'default': [('1 年内', '把储蓄率提到 20% 以上'), ('1 年内', '配齐家庭保障（医疗+意外）'),
                ('5 年内', '攒下第一个 10 万元投资本金')],
}


class DimGenerator(Generator):
    """维度卡生成器：复用 gen_batch.Generator 的字段推导，改写路径/化名/Schema。"""

    def __init__(self, dim, start_id=None, batch_tag='v4.5', seed=20260916,
                 out_root=None, names_file=None):
        self.dim_code = dim.strip().upper()
        self.dim = dimension(self.dim_code)
        if self.dim['root'] is None:
            raise ValueError('维度① 行业维度请用 gen_batch.py / run_v43_batch.py 生成')
        id_start = self.dim['id_start']
        if start_id is None:
            start_id = id_start
        if not (id_start <= start_id <= self.dim['id_end']):
            raise ValueError('start_id %d 不在维度 %s 独占区间 [%d, %d]'
                             % (start_id, self.dim_code, id_start, self.dim['id_end']))
        root = out_root or CARD_ROOT
        super().__init__(root, start_id=start_id, batch_tag=batch_tag, seed=seed)
        self.id_end = self.dim['id_end']
        self.dim_root = os.path.join(root, self.dim['root'])
        self._names = None
        self._names_m = collections.deque()
        self._names_f = collections.deque()
        self._names_x = collections.deque()
        self._last_name = None
        self._names_file = names_file
        self.created_at = time.strftime('%Y-%m-%d')

    # ── 化名：只从预分配块取，绝不自行随机（跨 Agent 零碰撞）──────
    # ── 化名：只从预分配块取，绝不自行随机（跨 Agent 零碰撞）──────
    def _bind_names(self, needed):
        if self._names_file:
            with open(self._names_file, encoding='utf-8') as f:
                pool = []
                for ln in f:
                    if not ln.strip():
                        continue
                    parts = ln.split('\t')
                    pool.append((parts[0].strip(),
                                 parts[1].strip() if len(parts) > 1 else '?'))
        else:
            pool = load_names(self.dim['block'], needed)
        self._names_m = collections.deque(n for n, g in pool if g == '男')
        self._names_f = collections.deque(n for n, g in pool if g == '女')
        self._names_x = collections.deque(n for n, g in pool if g not in ('男', '女'))
        # 同时塞进父类 used_names，防止父类兜底路径复用同名
        self.used_names.update(n for n, _g in pool)

    def _fresh_name(self, gender):  # noqa: D102 — 覆写父类：按性别取名
        dq, tag = self._pick_deque(gender)
        if not dq:
            raise RuntimeError('化名块耗尽（%s）：请扩大 alloc_names_v45.py 的 block 配额' % tag)
        name = dq.popleft()
        self._last_name = (name, tag)
        return name

    def _pick_deque(self, gender):
        if gender == '女':
            if self._names_f:
                return self._names_f, '女'
            return self._names_x, '?'
        if self._names_m:
            return self._names_m, '男'
        return self._names_x, '?'

    def _return_name(self, saved):
        """约束不满足需重试时，把刚取出的化名退回队首（不浪费配额）。"""
        if not saved:
            return
        name, tag = saved
        {'男': self._names_m, '女': self._names_f, '?': self._names_x}[tag].appendleft(name)
        self._last_name = None

    def _new_id(self):  # noqa: D102 — 覆写父类：先占号不递增，成功落卡才 commit
        # 人群约束（age_range/gender/marital）需要重试生成；若每次重试都吃掉一个卡号，
        # 1500 张卡会消耗上万个号段。故改为「预读当前号 → make_card 成功后 _commit_id()」。
        if self.next_id > self.id_end:
            raise RuntimeError('卡号 %d 超出维度 %s 区间上限 %d'
                               % (self.next_id, self.dim_code, self.id_end))
        return 'N%d' % self.next_id

    def _commit_id(self):
        """卡片通过约束校验后提交卡号（号段严格连续、零浪费）。"""
        if self.next_id > self.id_end:
            raise RuntimeError('卡号 %d 超出维度 %s 区间上限 %d'
                               % (self.next_id, self.dim_code, self.id_end))
        nid = self.next_id
        self.next_id += 1
        return nid

    # ── 人群约束（真实世界人群画像必须自洽）────────────────────
    @staticmethod
    def _constraints_ok(card, group):
        """校验 age_range / gender / marital_in / age_min_children 等人群约束。"""
        ar = group.get('age_range')
        if ar and not (int(ar[0]) <= int(card.get('age') or 0) <= int(ar[1])):
            return False
        g = group.get('gender')
        if g and card.get('gender') != g:
            return False
        mi = group.get('marital_in')
        if mi and card.get('marital') not in mi:
            return False
        if group.get('no_children') and int(card.get('children_count') or 0) > 0:
            return False
        if group.get('min_children'):
            if int(card.get('children_count') or 0) < int(group['min_children']):
                return False
        return True

    # ── 单卡生成 ────────────────────────────────────────────────
    def make_card(self, group, occ_info, max_tries=80):
        """按维度 L3 组 + 职业实例生成一张 Schema v1.1 卡（dict）。

        group: {'code','name','l2','l2_name','industry':(l1,l2)|None,
                'age_range'?, 'gender'?, 'marital_in'?, 'no_children'?, 'min_children'?,
                'tag'?, 'note'?, 'income_composition'?}
        occ_info: (occ_name, employment, inc_mid, inc_swing, certs, health_risks, stress)
        """
        industry = group.get('industry')
        gen_l1, gen_l2 = (industry if industry else ('Z', 'Z07'))
        card = None
        for _ in range(max_tries):
            self._last_name = None
            cand, l1, l2 = self.generate_one(gen_l1, gen_l2, occ_info)
            if self._constraints_ok(cand, group):
                card = cand
                break
            self._return_name(self._last_name)
        if card is None:
            # 约束过紧：放宽为「最接近年龄区间端点」的一次生成，避免整批失败
            self._last_name = None
            card, l1, l2 = self.generate_one(gen_l1, gen_l2, occ_info)
            ar = group.get('age_range')
            if ar:
                age = int(card.get('age') or ar[0])
                card['age'] = min(max(age, int(ar[0])), int(ar[1]))
                import lib_derive as _D
                card['age_band'] = _D.age_band(card['age'])
                card['birth_year'] = 2026 - card['age']
                card['generation'] = _D.generation(card['birth_year'])
            card['_constraint_relaxed'] = True
        self._commit_id()  # 约束通过 → 卡号落地（保证号段连续）

        # 1) 行业归属：无行业人群改置 NA 占位（保留 v2.0 数字编号 '20'）
        if not industry:
            card['industry_l1'], card['industry_l2'], card['industry_l3'] = NA_INDUSTRY
            card['industry_note'] = group.get('note') or '非行业归属人群（维度%s）' % self.dim_code[-1]
            card['occ_industry_num'] = NA_OCC_NUM
            card['occ_l2_num'] = NA_OCC_NUM + '00'
            card['occ_l3_num'] = NA_OCC_NUM + '0000'
        else:
            card['industry_l3'] = '%s%02d' % (l2, self.rng.randint(1, 99))

        # 2) 维度归属（v4.5 新增字段族）
        card['dimension'] = self.dim_code
        card['dimension_name'] = self.dim['name']
        card['dim_l2'] = group['l2']
        card['dim_l2_name'] = group['l2_name']
        card['dim_l3'] = group['code']
        card['dim_l3_name'] = group['name']
        card['dim_tag'] = group.get('tag') or card['dim_l3_name']
        # source_file 指向维度池（父类会写成内部生成用字母码，对无行业人群是噪声）
        card['source_file'] = '%s-%s-%s.md' % (self.dim_code, group['code'],
                                               _seg(group['name']))
        card['source_kind'] = 'dimension_pool_v4.5'

        # 3) 加载器可玩性：income_monthly 必须 > 0（= 月度可支配现金流入）
        income = int(card.get('income_monthly') or 0)
        if income <= 0:
            income = max(600, int((occ_info[2] or 0) * 0.4))
            card['income_monthly'] = income
        card['income_semantics'] = '月度可支配现金流入（含工资/经营/财产/转移性收入）'
        comp = group.get('income_composition') or self._default_composition(card, income)
        card['income_composition'] = [
            {'source': str(c[0]), 'share': round(float(c[1]), 3)} for c in comp]

        # 3.5) 人群自洽修正（就业形态/地理一致性/财务锚点/人生目标兜底）
        self._coherence_fix(card, group)

        # 4) Schema v1.1 全息画像六字段（出生即完整）
        card['schema_version'] = '1.1'
        card['personality'] = H.derive_personality(card)
        card['behavior_traits'] = H.derive_behavior_traits(card)
        card['risk_preference'] = H.derive_risk_preference(card)
        card['birth_family'] = H.derive_birth_family(card)
        card['life_story'] = H.derive_life_story(card)
        card['opening_hook'] = H.derive_opening_hook(card)
        # v4.5 钩子清洗：父钩子 `goal_text[:14]` 会把目标语句拦腰截断（例
        # "把慢病用药支出压到养老金 15% 以内" → "把慢病用药支出压到养老金 1"）。
        # 此处用干净的截断 + 兜底句重写，避免出现读不懂的残句。
        goal_text = ''
        gs = card.get('goals_short') or []
        if gs and isinstance(gs[0], dict):
            goal_text = (gs[0].get('text') or '').strip()
        if goal_text and ('「%s」' % goal_text[:14].rstrip()) in card['opening_hook']:
            short = goal_text if len(goal_text) <= 18 else goal_text[:17].rstrip('，,。 ') + '…'
            card['opening_hook'] = card['opening_hook'].replace(
                '「%s」' % goal_text[:14].rstrip(), '「%s」' % short)
        # opening_hook 需 ∈[20,60] rune（Go 侧 Card.Validate 硬校验）
        hook = card['opening_hook'] or ''
        if len(hook) > 60:
            hook = hook[:59] + '…'
        if len(hook) < 20:
            hook = '%s，%d 岁的%s正为「%s」盘算下一步取舍' % (
                card.get('housing_city') or '这座城市', card.get('age') or 30,
                card.get('occupation') or '普通人',
                (card.get('goals_short') or [{}])[0].get('text', '第一桶金')
                if isinstance((card.get('goals_short') or [{}])[0], dict)
                else str((card.get('goals_short') or ['第一桶金'])[0])[:12])
        card['opening_hook'] = hook

        # 5) 溯源章（与 v4.4 卡片风格一致，但不写重复 YAML 键）
        card['source_batch'] = self.batch_tag
        card['created_at'] = self.created_at
        card['_legacy_ids'] = [card['id']]
        card['_raw'] = {
            'src_line': 'auto-gen-v45|%s|%s|%s岁|%s|%d' % (
                card['name'], card['dim_l3'], card['age'],
                card['occupation'], income),
            'dimension': self.dim_code,
            'name_block': self.dim['block'],
        }
        card['_gen_v45'] = {
            'ts': time.strftime('%Y-%m-%dT%H:%M:%S+08:00'),
            'script': 'lib_dim_v45.py',
            'schema_version': '1.1',
            'fields_holistic': ['personality', 'behavior_traits', 'risk_preference',
                                'birth_family', 'life_story', 'opening_hook'],
            'method': 'pool_driven_derivation',
        }
        return card, l1, l2

    @staticmethod
    def _default_composition(card, income):
        """按就业形态推导收入构成（真实世界口径）。"""
        emp = str(card.get('employment') or '')
        age = int(card.get('age') or 30)
        if '学生' in emp:
            return [('家庭转移支付', 0.50), ('兼职零工', 0.35), ('奖助学金', 0.15)]
        if '退休' in emp or age >= 60:
            return [('养老金', 0.75), ('财产性收入', 0.15), ('家庭转移支付', 0.10)]
        if '失业' in emp or '待业' in emp:
            return [('失业保险金', 0.30), ('家庭转移支付', 0.30), ('临时零工', 0.40)]
        if emp in ('个体经营', '自由职业', '平台就业'):
            return [('经营/接单收入', 0.85), ('财产性收入', 0.15)]
        if '照料' in emp or emp == '无业':
            return [('配偶收入转移', 0.60), ('财产性收入', 0.20), ('家庭支持', 0.20)]
        return [('工资性收入', 0.90), ('财产性收入', 0.10)]

    # ── 人群自洽修正（v4.5 新增；父类生成器按「在职职工」假设填值，维度人群需纠偏）─
    def _coherence_fix(self, card, group):
        """把父类产出的「通用在职职工」卡修正为与人群身份自洽的档案。

        覆盖 4 类真实世界一致性问题：
          1. 非工资就业形态（学生/退休/失业/待业/无业/全职照料）的工时、职业阶段、
             单位、公积金口径 —— 父类会给出「每周 55 小时 · 骨干 · 公积金双边 1200」
             这类明显失真值。
          2. 籍贯与现居地一致性 + 人口流动标注（本地/省内流动/跨省流动/海外）。
          3. 组级财务锚点（savings_range / debt_range / expense_ratio / housing_tenure
             / assets_extra）—— 维度③财富阶层完全依赖此项拉开层次。
          4. 人生目标兜底（`derive_goals` 对高龄/无业常返回空 → §7 空章）。
        """
        emp = str(card.get('employment') or '')
        prof = NON_WORKING.get(emp)
        if prof:
            card['work_intensity'] = {'weekly_hours': prof['weekly_hours'],
                                      'overtime': prof['overtime'],
                                      'risk': prof['risk']}
            card['career_stage'] = prof['stage']
            card['employer'] = prof['employer']
            card['funds'] = list(prof['funds'])
            card['income_structure'] = {
                '退休': '养老金', '学生': '家庭供给+兼职', '失业': '失业保险金+零工',
                '待业': '家庭供给+零工', '无业': '转移性收入',
                '全职照料': '配偶收入转移',
            }.get(emp, card.get('income_structure') or '非工资性收入')
            card['income_stability'] = '退休' if emp == '退休' else '低'
            # 参保口径：学生/无业/待业走城乡居民医保，退休走职工医保+职工养老，
            # 灵活就业者按「灵活就业人员」参保（真实世界社保分层，父类一律按在职职工填）
            card['insurance'] = {
                '学生': ['城乡居民医保', '校方责任险'],
                '退休': ['城镇职工医保', '城镇职工养老', '大病互助'],
                '失业': ['城乡居民医保', '失业保险金领取中'],
                '待业': ['城乡居民医保'],
                '无业': ['城乡居民医保'],
                '全职照料': ['城乡居民医保', '灵活就业人员养老'],
            }.get(emp, card.get('insurance') or ['城乡居民医保'])

        # 2) 地理一致性：现居城市 → 省份；籍贯按流动概率另取
        city = card.get('housing_city') or ''
        prov = CITY_PROVINCE.get(city)
        if prov:
            card['housing_province'] = prov
            card['l4_basis'] = 'residence'  # v4.5 维度卡 L4 = 现居地区档
            r = self.rng.random()
            if r < 0.55:
                card['birth_province'] = prov
                card['birth_city'] = city
                card['migration_status'] = '本地人'
            elif r < 0.75:
                same = [c for c, p in CITY_PROVINCE.items()
                        if p == prov and c != city and c not in OVERSEAS_CITIES]
                card['birth_city'] = self.rng.choice(same) if same else city
                card['birth_province'] = prov
                card['migration_status'] = '省内流动'
            else:
                others = [c for c, p in CITY_PROVINCE.items()
                          if p != prov and c not in OVERSEAS_CITIES]
                bc = self.rng.choice(others)
                card['birth_city'] = bc
                card['birth_province'] = CITY_PROVINCE[bc]
                card['migration_status'] = ('海外流动' if city in OVERSEAS_CITIES
                                            else '跨省流动')
            card['housing_detail'] = '%s%s' % (
                city, {'自有': '自住房', '租赁': '租房', '合租': '合租',
                       '单位/保障住房': '单位保障房', '父母产权': '父母产权房'}.get(
                           card.get('housing_tenure'), '居住'))

        # 3) 组级财务锚点
        fin = group.get('finance') or {}
        sr = fin.get('savings_range')
        if sr:
            card['savings_stock'] = int(self.rng.uniform(float(sr[0]), float(sr[1])))
        elif int(card.get('savings_stock') or 0) <= 0:
            # 无组级锚点且父类给出 0 储蓄 → 按「0.5–8 个月现金流入」兜底，
            # 避免整批卡净值为 0（财商模拟需要初始资产差异）
            base = int(card.get('income_monthly') or 3000)
            card['savings_stock'] = int(base * self.rng.uniform(0.5, 8))
        dr = fin.get('debt_range')
        if dr:
            card['debt_stock'] = int(self.rng.uniform(float(dr[0]), float(dr[1])))
        er = fin.get('expense_ratio')
        if er:
            card['monthly_expense'] = max(
                300, int(int(card.get('income_monthly') or 0)
                         * float(self.rng.uniform(er[0], er[1]))))
        ht = fin.get('housing_tenure')
        if ht:
            card['housing_tenure'] = self.rng.choice(list(ht)) if isinstance(ht, (list, tuple)) else ht
        inc = int(card.get('income_monthly') or 0)
        exp = int(card.get('monthly_expense') or 0)
        sav = int(card.get('savings_stock') or 0)
        debt = int(card.get('debt_stock') or 0)
        card['savings_rate'] = round((inc - exp) / inc, 3) if inc > 0 else 0.0
        card['net_worth'] = sav - debt
        # 资产/负债明细与存量对齐（父类只写「现金/存款 = savings_stock」）
        assets = [{'type': '现金/存款', 'desc': '活期+定期+货币基金', 'value_cny': sav}]
        if card.get('housing_tenure') == '自有':
            hv = fin.get('home_value_range') or (inc * 60, inc * 220)
            assets.append({'type': '自住房', 'desc': '%s自住' % (card.get('housing_city') or '现居'),
                           'value_cny': int(self.rng.uniform(float(hv[0]), float(hv[1])))})
        for extra in (fin.get('assets_extra') or []):
            lo, hi = float(extra['range'][0]), float(extra['range'][1])
            if self.rng.random() < float(extra.get('prob', 0.6)):
                assets.append({'type': extra['type'], 'desc': extra.get('desc', extra['type']),
                               'value_cny': int(self.rng.uniform(lo, hi))})
        card['assets'] = assets
        debts = []
        if debt > 0:
            parts = group.get('debt_mix') or [('消费贷/信用卡', 0.4), ('房贷', 0.6)]
            left = debt
            for i, (label, share) in enumerate(parts):
                amt = left if i == len(parts) - 1 else int(debt * float(share))
                left -= amt
                if amt > 0:
                    # 键名与 lib_derive.build_debts / gen_batch.render_body 对齐：
                    # type / balance / monthly / source 四项缺一不可（否则正文渲染 KeyError）
                    debts.append({'type': label, 'balance': int(amt),
                                  'monthly': int(amt * self.rng.uniform(0.012, 0.035)),
                                  'source': 'v4.5 维度财务锚点',
                                  'rate_apr': round(self.rng.uniform(0.035, 0.18), 4)})
            card['debt_stock'] = sum(d['balance'] for d in debts)
            card['debt_service_ratio'] = round(
                sum(d['monthly'] for d in debts) / inc, 3) if inc > 0 else 0.0
        card['debts'] = debts
        card['net_worth'] = sum(a['value_cny'] for a in assets) - card.get('debt_stock', 0)
        card['mortgage_left'] = next(
            (d['balance'] for d in debts if '房贷' in d['type']), 0)
        # 资产明细中的现金项需与最终 savings_stock 一致（上面可能被锚点改写）
        for a in card['assets']:
            if a.get('type') == '现金/存款':
                a['value_cny'] = int(card.get('savings_stock') or 0)
        card['net_worth'] = sum(int(a.get('value_cny') or 0) for a in card['assets']) \
            - int(card.get('debt_stock') or 0)

        # 4) 人生目标兜底（§7 不得空章；维度组可自带 goals 池覆盖）
        if not card.get('goals_short'):
            stage = card.get('career_stage') or 'default'
            pool = group.get('goals') or GOALS_BY_STAGE.get(stage) or GOALS_BY_STAGE['default']
            card['goals_short'] = [{'horizon': h, 'text': t} for h, t in pool][:3]
        if not card.get('opportunities'):
            card['opportunities'] = ['技能与认知升级', '家庭资产结构优化', '副业与被动收入']

        # 5) 溯源自洽：_missing 只列真正为空的字段（v4.4 曾出现「已填字段被列缺失」）
        cand = ['birth_city', 'birth_province', 'housing_city', 'health_risks',
                'certs', 'funds', 'biases', 'children_ages', 'debt_stock']
        miss = [k for k in cand if not card.get(k) and card.get(k) != 0]
        card['_missing'] = miss
        src = card.get('_sources') or {}
        src['missing'] = len(miss)
        card['_sources'] = src

    # ── 写盘：维度五段式路径 ────────────────────────────────────
    def write_card_dim(self, card, l1, l2, group):
        # v4.5：L4 地区档以**现居省**为准（`l4_basis: residence`），与籍贯解耦，
        # 使「跨省流动人口」这一真实财富现象在路径上可见。
        region = PROVINCES.get(card.get('housing_province')
                               or card.get('birth_province'), 'XX-未知')
        if card.get('housing_city') in OVERSEAS_CITIES:
            region = 'OV-海外'
        d = os.path.join(self.dim_root,
                         '%s-%s' % (group['l2'], _seg(group['l2_name'])),
                         '%s-%s' % (group['code'], _seg(group['name'])),
                         _seg(region),
                         # L5 年龄段必须与既有 75k 卡完全一致（`65+` 不可被 _seg 削成 `65`）
                         str(card['age_band'] or 'XX-未知').replace('/', '_'))
        os.makedirs(d, exist_ok=True)
        body = self.render_body(card, l1, l2)
        # §6 人格/行为特征、§8 开局钩子/人生时间线：与 v4.4 全库富化后形态一致
        body, _s6 = H.update_section_6(body, card, False)
        body, _s8 = H.update_section_8(body, card, False)
        body += self._render_dim_section(card, group)
        fm = {k: v for k, v in card.items() if not k.startswith('_')}
        prov = {k: v for k, v in card.items() if k.startswith('_')}
        text = '---\n' + yaml.dump(fm, allow_unicode=True, sort_keys=False,
                                   default_flow_style=None, width=10000).rstrip() + '\n'
        text += yaml.dump(prov, allow_unicode=True, sort_keys=False,
                          default_flow_style=None, width=10000).rstrip() + '\n'
        text += '---\n\n' + body
        fn = '%s-%s.md' % (card['id'], re.sub(r'[/\\]', '_', card['name']))
        fp = os.path.join(d, fn)
        with open(fp, 'w', encoding='utf-8') as fh:
            fh.write(text)
        # 落盘即自检：YAML 可解析 + 无重复键（v4.4 曾出现 legacy_name 重复键）
        self._self_check(fp)
        return fp

    @staticmethod
    def _self_check(fp):
        with open(fp, encoding='utf-8') as f:
            txt = f.read()
        parts = txt.split('---', 2)
        if len(parts) < 3:
            raise ValueError('frontmatter 结构异常：%s' % fp)
        data = yaml.safe_load(parts[1])
        if not isinstance(data, dict):
            raise ValueError('frontmatter 非映射：%s' % fp)
        for key in ('id', 'name', 'occupation', 'income_monthly', 'opening_hook',
                    'dimension', 'dim_l3', 'risk_preference', 'health_grade'):
            if key not in data:
                raise ValueError('缺字段 %s：%s' % (key, fp))
        if int(data['income_monthly']) <= 0:
            raise ValueError('income_monthly 必须 > 0：%s' % fp)

    @staticmethod
    def _render_dim_section(card, group):
        comp = card.get('income_composition') or []
        comp_txt = '；'.join('%s %d%%' % (c.get('source'), round(float(c.get('share', 0)) * 100))
                            for c in comp) or '未采集'
        ind = ('%s / %s / %s' % (card.get('industry_l1'), card.get('industry_l2'),
                                card.get('industry_l3')))
        if card.get('industry_l1') == 'NA':
            ind = '无行业归属（%s）' % card.get('industry_note', '非行业人群')
        return (
            '\n## 10. 维度归属（v4.5 多维度人群档案）\n\n'
            '- **维度**：`%s` %s\n'
            '- **维度细分**：`%s` %s\n'
            '- **维度人群族**：`%s` %s\n'
            '- **人群标签**：%s\n'
            '- **行业归属**：%s\n'
            '- **收入语义**：%s\n'
            '- **收入构成**：%s\n' % (
                card.get('dimension'), card.get('dimension_name'),
                card.get('dim_l2'), card.get('dim_l2_name'),
                card.get('dim_l3'), card.get('dim_l3_name'),
                card.get('dim_tag'), ind,
                card.get('income_semantics'), comp_txt)
        )

    # ── 批量入口 ────────────────────────────────────────────────
    def gen_from_pool(self, pools, count, quota_map=None, report_path=None):
        """按维度池批量生成 count 张卡。

        pools: [{'l2','l2_name','l3':[{'code','name','industry','occupations':[occ_info,...],
                 'weight'?,'tag'?,'income_composition'?,'note'?}, ...]}, ...]
        occ_info: (occ_name, employment, inc_mid, inc_swing, certs, health_risks, stress)
        quota_map: {l3_code: 张数}，缺省按 weight 均分。
        """
        groups = []
        for p in pools:
            for g in p['l3']:
                gg = dict(g)
                gg['l2'] = p['l2']
                gg['l2_name'] = p['l2_name']
                if not gg.get('occupations'):
                    raise ValueError('L3 组 %s 无 occupations' % gg.get('code'))
                groups.append(gg)
        if not groups:
            raise ValueError('维度 %s 池为空' % self.dim_code)

        if quota_map:
            quotas = {k: int(v) for k, v in quota_map.items()}
        else:
            weights = [float(g.get('weight', 1)) for g in groups]
            total_w = sum(weights) or 1.0
            quotas = {}
            acc = 0
            for g, w in zip(groups, weights):
                q = int(round(count * w / total_w))
                quotas[g['code']] = q
                acc += q
            # 余数补到权重最大的组
            if acc != count:
                big = max(groups, key=lambda g: g.get('weight', 1))
                quotas[big['code']] += (count - acc)
        plan_total = sum(quotas.values())
        self._bind_names(plan_total)
        print('[%s] 计划生成 %d 张（%d 个 L3 组，卡号 N%d–N%d，化名块 %s）'
              % (self.dim_code, plan_total, len(groups), self.next_id,
                 self.next_id + plan_total - 1, self.dim['block']))

        written = []
        per_group = collections.Counter()
        ids = []
        t0 = time.time()
        for g in groups:
            q = quotas.get(g['code'], 0)
            occs = g['occupations']
            for i in range(q):
                occ_info = occs[i % len(occs)] if len(occs) > 1 else occs[0]
                # 职业轮换 + 随机扰动，避免同 L3 组内卡片高度雷同
                if len(occs) > 1:
                    occ_info = self.rng.choice(occs)
                card, l1, l2 = self.make_card(g, occ_info)
                fp = self.write_card_dim(card, l1, l2, g)
                written.append(fp)
                ids.append(card['id'])
                per_group[g['code']] += 1
        report = {
            'dimension': self.dim_code,
            'dimension_name': self.dim['name'],
            'root': self.dim['root'],
            'batch_tag': self.batch_tag,
            'planned': plan_total,
            'written': len(written),
            'id_range': [min(ids) if ids else None, max(ids) if ids else None],
            'id_next': self.next_id,
            'per_l3': dict(per_group),
            'elapsed_sec': round(time.time() - t0, 1),
            'schema_version': '1.1',
            'ts': time.strftime('%Y-%m-%dT%H:%M:%S+08:00'),
        }
        if report_path:
            os.makedirs(os.path.dirname(report_path), exist_ok=True)
            with open(report_path, 'w', encoding='utf-8') as f:
                json.dump(report, f, ensure_ascii=False, indent=2)
        print('[%s] 完成 %d 张，用时 %.1fs；报告 %s'
              % (self.dim_code, len(written), report['elapsed_sec'], report_path))
        return report
