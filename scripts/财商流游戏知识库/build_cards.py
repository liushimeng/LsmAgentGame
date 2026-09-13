#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""阶段 C：渲染人物卡 —— persons.jsonl + 分类结果 → 新目录树下的 .md

用法:
    python3 build_cards.py --persons work/persons.jsonl \
                           --classified work/classified \
                           --out <玩家职业设计目录> [--limit 200]
"""
import argparse
import collections
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import yaml  # noqa: E402
import lib_derive as D  # noqa: E402
import lib_cells as L  # noqa: E402
from icg_l2 import l2_index, L2  # noqa: E402
from l1_rules import L1_NAMES  # noqa: E402

REGION = {
    '北京': 'CN-N-华北', '天津': 'CN-N-华北', '河北': 'CN-N-华北', '山西': 'CN-N-华北',
    '内蒙古': 'CN-N-华北',
    '辽宁': 'CN-NE-东北', '吉林': 'CN-NE-东北', '黑龙江': 'CN-NE-东北',
    '上海': 'CN-E-华东', '江苏': 'CN-E-华东', '浙江': 'CN-E-华东', '安徽': 'CN-E-华东',
    '福建': 'CN-E-华东', '江西': 'CN-E-华东', '山东': 'CN-E-华东',
    '河南': 'CN-C-华中', '湖北': 'CN-C-华中', '湖南': 'CN-C-华中',
    '广东': 'CN-S-华南', '广西': 'CN-S-华南', '海南': 'CN-S-华南',
    '重庆': 'CN-SW-西南', '四川': 'CN-SW-西南', '贵州': 'CN-SW-西南',
    '云南': 'CN-SW-西南', '西藏': 'CN-SW-西南',
    '陕西': 'CN-NW-西北', '甘肃': 'CN-NW-西北', '青海': 'CN-NW-西北',
    '宁夏': 'CN-NW-西北', '新疆': 'CN-NW-西北',
}
UNKNOWN_REGION = 'XX-未知'

# 城市 → 地区档（原档常只写城市不写省）
CITY_REGION = {
    '北京': 'CN-N-华北', '天津': 'CN-N-华北', '石家庄': 'CN-N-华北', '唐山': 'CN-N-华北',
    '保定': 'CN-N-华北', '廊坊': 'CN-N-华北', '邯郸': 'CN-N-华北', '太原': 'CN-N-华北',
    '呼和浩特': 'CN-N-华北', '包头': 'CN-N-华北', '鄂尔多斯': 'CN-N-华北',
    '沈阳': 'CN-NE-东北', '大连': 'CN-NE-东北', '鞍山': 'CN-NE-东北', '抚顺': 'CN-NE-东北',
    '哈尔滨': 'CN-NE-东北', '大庆': 'CN-NE-东北', '齐齐哈尔': 'CN-NE-东北',
    '长春': 'CN-NE-东北', '吉林市': 'CN-NE-东北',
    '上海': 'CN-E-华东', '南京': 'CN-E-华东', '苏州': 'CN-E-华东', '无锡': 'CN-E-华东',
    '常州': 'CN-E-华东', '南通': 'CN-E-华东', '扬州': 'CN-E-华东', '盐城': 'CN-E-华东',
    '徐州': 'CN-E-华东', '杭州': 'CN-E-华东', '宁波': 'CN-E-华东', '温州': 'CN-E-华东',
    '嘉兴': 'CN-E-华东', '绍兴': 'CN-E-华东', '台州': 'CN-E-华东', '金华': 'CN-E-华东',
    '合肥': 'CN-E-华东', '芜湖': 'CN-E-华东', '蚌埠': 'CN-E-华东',
    '福州': 'CN-E-华东', '厦门': 'CN-E-华东', '泉州': 'CN-E-华东', '漳州': 'CN-E-华东',
    '莆田': 'CN-E-华东', '龙岩': 'CN-E-华东',
    '南昌': 'CN-E-华东', '赣州': 'CN-E-华东', '九江': 'CN-E-华东',
    '济南': 'CN-E-华东', '青岛': 'CN-E-华东', '烟台': 'CN-E-华东', '潍坊': 'CN-E-华东',
    '临沂': 'CN-E-华东', '聊城': 'CN-E-华东', '菏泽': 'CN-E-华东',
    '郑州': 'CN-C-华中', '洛阳': 'CN-C-华中', '武汉': 'CN-C-华中', '襄阳': 'CN-C-华中',
    '宜昌': 'CN-C-华中', '长沙': 'CN-C-华中', '岳阳': 'CN-C-华中', '常德': 'CN-C-华中',
    '广州': 'CN-S-华南', '深圳': 'CN-S-华南', '佛山': 'CN-S-华南', '东莞': 'CN-S-华南',
    '珠海': 'CN-S-华南', '中山': 'CN-S-华南', '惠州': 'CN-S-华南', '汕头': 'CN-S-华南',
    '湛江': 'CN-S-华南', '茂名': 'CN-S-华南', '揭阳': 'CN-S-华南',
    '南宁': 'CN-S-华南', '桂林': 'CN-S-华南', '柳州': 'CN-S-华南', '北海': 'CN-S-华南',
    '海口': 'CN-S-华南', '三亚': 'CN-S-华南',
    '重庆': 'CN-SW-西南', '成都': 'CN-SW-西南', '绵阳': 'CN-SW-西南', '德阳': 'CN-SW-西南',
    '宜宾': 'CN-SW-西南', '贵阳': 'CN-SW-西南', '遵义': 'CN-SW-西南',
    '昆明': 'CN-SW-西南', '曲靖': 'CN-SW-西南',
    '西安': 'CN-NW-西北', '咸阳': 'CN-NW-西北', '宝鸡': 'CN-NW-西北',
    '兰州': 'CN-NW-西北', '天水': 'CN-NW-西北', '西宁': 'CN-NW-西北',
    '银川': 'CN-NW-西北', '乌鲁木齐': 'CN-NW-西北',
    '南昌': 'CN-E-华东', '安庆': 'CN-E-华东', '景德镇': 'CN-E-华东', '上饶': 'CN-E-华东',
    '拉萨': 'CN-SW-西南', '淄博': 'CN-E-华东', '济宁': 'CN-E-华东', '泰安': 'CN-E-华东',
    '威海': 'CN-E-华东', '日照': 'CN-E-华东', '德州': 'CN-E-华东', '新乡': 'CN-C-华中',
    '安阳': 'CN-C-华中', '焦作': 'CN-C-华中', '信阳': 'CN-C-华中', '南阳': 'CN-C-华中',
    '许昌': 'CN-C-华中', '商丘': 'CN-C-华中', '周口': 'CN-C-华中', '驻马店': 'CN-C-华中',
    '黄石': 'CN-C-华中', '十堰': 'CN-C-华中', '荆州': 'CN-C-华中', '孝感': 'CN-C-华中',
    '株洲': 'CN-C-华中', '湘潭': 'CN-C-华中', '衡阳': 'CN-C-华中', '邵阳': 'CN-C-华中',
    '韶关': 'CN-S-华南', '梅州': 'CN-S-华南', '江门': 'CN-S-华南', '肇庆': 'CN-S-华南',
    '清远': 'CN-S-华南', '潮州': 'CN-S-华南', '云浮': 'CN-S-华南', '河源': 'CN-S-华南',
    '汕尾': 'CN-S-华南', '阳江': 'CN-S-华南', '宿迁': 'CN-E-华东', '连云港': 'CN-E-华东',
    '淮安': 'CN-E-华东', '泰州': 'CN-E-华东', '镇江': 'CN-E-华东', '湖州': 'CN-E-华东',
    '衢州': 'CN-E-华东', '丽水': 'CN-E-华东', '舟山': 'CN-E-华东', '铜陵': 'CN-E-华东',
    '马鞍山': 'CN-E-华东', '淮南': 'CN-E-华东', '阜阳': 'CN-E-华东', '六安': 'CN-E-华东',
    '曲靖': 'CN-SW-西南', '玉溪': 'CN-SW-西南', '保山': 'CN-SW-西南', '昭通': 'CN-SW-西南',
    '丽江': 'CN-SW-西南', '六盘水': 'CN-SW-西南', '安顺': 'CN-SW-西南', '毕节': 'CN-SW-西南',
    '泸州': 'CN-SW-西南', '乐山': 'CN-SW-西南', '南充': 'CN-SW-西南', '达州': 'CN-SW-西南',
    '渭南': 'CN-NW-西北', '延安': 'CN-NW-西北', '榆林': 'CN-NW-西北', '汉中': 'CN-NW-西北',
    '克拉玛依': 'CN-NW-西北', '喀什': 'CN-NW-西北', '石嘴山': 'CN-NW-西北', '吴忠': 'CN-NW-西北',
    '赤峰': 'CN-N-华北', '通辽': 'CN-N-华北', '本溪': 'CN-NE-东北', '丹东': 'CN-NE-东北',
    '锦州': 'CN-NE-东北', '营口': 'CN-NE-东北', '牡丹江': 'CN-NE-东北', '佳木斯': 'CN-NE-东北',
    '四平': 'CN-NE-东北', '梧州': 'CN-S-华南', '钦州': 'CN-S-华南', '玉林': 'CN-S-华南',
    '百色': 'CN-S-华南', '儋州': 'CN-S-华南', '琼海': 'CN-S-华南',
    '香港': 'OV-海外', '澳门': 'OV-海外', '台北': 'OV-海外',
}

# 姓名再造用字池（按性别）
POOL_F = list('兰婷娟秀慧芳燕玲莉娜静丽敏雪梅萍红霞珍琴婉妍媛瑶瑾琪珊琳琦莹蕊薇蕾蓉菲萱芸茜茵荷莲怡悦欣晴岚心月昕晞晗曦暖妤姝娴婵婧婕姣娅娴嫣彤姗茹蓓菁菡菱菊桃樱棠柔妙')
POOL_M = list('强军伟勇刚磊涛峰波辉鹏飞龙虎彪斌杰亮明华建国志民永康健铁钢山河海江川兵武雄豪铭鑫栋梁坚毅承旭阳锋航帆骏骐腾源洲洋浩宇轩宸昊煜烨燊垚焱淼猛超越凯旋仁信义礼智忠孝勤俭谦恭')

SPECIAL = {'P01', 'P02', 'P03', 'P04', 'P05', 'P06', 'P07', 'P08', 'P09', 'P10',
           'P11', 'P12', 'P13', 'P14', 'P15', 'P16', 'P17', 'P18'}


def load_classification(d):
    out = {}
    if not os.path.isdir(d):
        return out
    for fn in sorted(os.listdir(d)):
        if not fn.endswith('.jsonl'):
            continue
        with open(os.path.join(d, fn), encoding='utf-8') as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    o = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if o.get('file'):
                    out[o['file']] = o
    return out


def build_l3_index(cls_map):
    """为 (l2, l3名) 分配 L3 码：<L2码><2位序号>（按 l3 名字典序稳定编号）。"""
    by_l2 = collections.defaultdict(set)
    for o in cls_map.values():
        l2 = o.get('l2')
        l3 = (o.get('l3') or '').strip()
        if l2 and l3:
            by_l2[l2].add(l3)
    idx = {}
    for l2 in sorted(by_l2):
        for i, name in enumerate(sorted(by_l2[l2]), 1):
            idx[(l2, name)] = '%s%02d' % (l2, i)
    return idx


def resolve_ids(recs):
    """消除编号冲突：首见保留原号，其余改入 N9000000+ 保留段。"""
    used = collections.Counter(r['id'] for r in recs)
    taken = set(r['id'] for r in recs)
    nxt = [9000001]
    collided = 0
    seen = set()
    for r in recs:
        cid = r['id']
        if used[cid] == 1:
            r['_new_id'] = cid
            continue
        if cid not in seen:
            seen.add(cid)
            r['_new_id'] = cid
            continue
        while ('N%d' % nxt[0]) in taken:
            nxt[0] += 1
        new = 'N%d' % nxt[0]
        taken.add(new)
        nxt[0] += 1
        r['_new_id'] = new
        r['_legacy_dup'] = cid
        collided += 1
    return collided


SURNAMES = list('赵钱孙李周吴郑王冯陈褚卫蒋沈韩杨朱秦尤许何吕施张孔曹严华金魏陶姜'
                '戚谢邹喻柏水窦章云苏潘葛奚范彭郎鲁韦昌马苗凤花方俞任袁柳鲍史唐费'
                '廉岑薛雷贺倪汤滕殷罗毕郝邬安常乐于时傅皮齐康伍余元卜顾孟平黄和穆'
                '萧尹姚邵湛汪祁毛禹狄米贝明臧计伏成戴谈宋茅庞熊纪舒屈项祝董梁杜阮'
                '蓝闵席季麻强贾路娄危江童颜郭梅盛林刁钟徐邱骆高夏蔡田樊胡凌霍虞万'
                '支柯管卢莫房裘缪干解应宗丁宣邓郁单杭洪包诸左石崔吉钮龚程嵇邢滑裴'
                '陆荣翁荀羊惠甄曲封芮羿储靳汲邴糜松井段富巫乌焦巴弓牧隗山谷车侯逢'
                '全班仰秋仲伊宫宁仇栾暴甘厉戎祖武符刘景詹束龙叶幸司韶郜黎蓟薄印宿'
                '白怀蒲台从鄂索咸籍赖卓蔺屠蒙池乔阴胥能苍双闻莘党翟谭贡劳逄姬申扶'
                '堵冉宰郦雍璩桑桂濮牛寿通边扈燕冀郏浦尚农温别庄晏柴瞿阎充慕连茹习'
                '宦艾鱼容向古易慎戈廖庾终暨居衡步都耿满弘匡国文寇广禄阙东欧殳沃利'
                '蔚越夔隆师巩厍聂晁勾敖融冷訾辛阚那简饶空曾毋沙乜养鞠须丰巢关蒯相'
                '查後荆红游竺权逯盖益桓公')
SURNAMES = list(dict.fromkeys(SURNAMES))

_nameref = [0]


def _fresh_name(gender, used):
    """从姓氏池 + 性别用字池生成一个全局唯一的新名字。"""
    pool = POOL_F if gender == '女' else POOL_M
    i = _nameref[0]
    for _ in range(len(SURNAMES) * len(pool) * len(pool)):
        i += 1
        sur = SURNAMES[i % len(SURNAMES)]
        a = pool[(i // len(SURNAMES)) % len(pool)]
        b = pool[(i // (len(SURNAMES) * len(pool))) % len(pool)]
        cand = sur + a + b
        if cand not in used:
            _nameref[0] = i
            used.add(cand)
            return cand
    # 三字池耗尽时扩到四字
    j = 0
    while True:
        j += 1
        cand = SURNAMES[j % len(SURNAMES)] + pool[j % len(pool)] + \
            pool[(j * 3) % len(pool)] + pool[(j * 7) % len(pool)]
        if cand not in used:
            used.add(cand)
            return cand


def build_occ_substrings(recs):
    """职业/岗位词子串集合（≥2 字），用于识别「姓+职业词」式伪姓名。"""
    sub = set()
    for r in recs:
        for o in (r['career'].get('occupation'),
                  r['career'].get('occupation_raw'), r['career'].get('income_note')):
            for m in re.finditer(r'[一-鿿]{2,8}', o or ''):
                sub.add(m.group(0))
    return sub


def is_plausible_name(n, occ_sub):
    if not n or not re.fullmatch(r'[一-鿿]{2,4}', n):
        return False
    given = n[1:]
    if given in occ_sub:
        return False
    if len(given) >= 3 and (given[:2] in occ_sub or given[1:] in occ_sub):
        return False
    return True


def resolve_names(recs, occ_sub):
    """消除姓名冲突：合法且唯一者保留；异常或重复者重命名（保持性别取向）。"""
    used = set()
    fixed = 0
    rebuilt = 0
    for r in recs:
        n = r.get('name', '')
        valid = is_plausible_name(n, occ_sub)
        if valid and n not in used:
            used.add(n)
            r['_new_name'] = n
            continue
        if not valid:
            r['_new_name'] = _fresh_name(r.get('_gender'), used)
            r['_legacy_name'] = n
            rebuilt += 1
            continue
        pool = POOL_F if r.get('_gender') == '女' else POOL_M
        got = None
        for pos in range(len(n) - 1, 0, -1):
            for ch in pool:
                cand = n[:pos] + ch + n[pos + 1:]
                if cand not in used:
                    got = cand
                    break
            if got:
                break
        if not got:
            got = _fresh_name(r.get('_gender'), used)
        else:
            used.add(got)
        r['_new_name'] = got
        r['_legacy_name'] = n
        fixed += 1
    return fixed, rebuilt


def region_of(rec):
    ch = rec['city_housing']
    if ch.get('overseas'):
        return 'OV-海外'
    prov = ch.get('province')
    if prov:
        for k, v in REGION.items():
            if k in str(prov):
                return v
    city = ch.get('city')
    if city:
        if city in CITY_REGION:
            return CITY_REGION[city]
        for k, v in REGION.items():
            if k in str(city):
                return v
    return UNKNOWN_REGION


def detail_items(rec, key):
    subs = rec.get('detail_subs') or {}
    return subs.get(key) or []


def detail_text(rec, key):
    items = detail_items(rec, key)
    return '；'.join('%s：%s' % (k, v) for k, v in items if v)


def build(rec, cls, l3idx):
    """把一条旧记录转成 v1.0 卡片字典。"""
    src = rec['src_file']
    o = cls.get(src) or {}
    l1 = o.get('l1') or 'Z'
    l2 = o.get('l2') or 'Z09'
    l3name = (o.get('l3') or '').strip() or '其他未归类'
    if not l2.startswith(l1):
        l1 = l2[0]
    l3 = l3idx.get((l2, l3name), l2 + '01')

    card = {}
    prov = {}
    card['id'] = rec['_new_id']
    card['name'] = rec['_new_name']
    card['schema_version'] = '1.0'
    card['card_type'] = 'archetype' if rec['id'] in SPECIAL else 'person'
    card['richness'] = 'skeleton'
    card['source_file'] = src
    card['source_batch'] = _batch_of(src)
    card['created_at'] = '2026-09-13'

    # ── identity ──
    age = rec.get('age')
    age_src = 'explicit'
    if not age:
        age = assign_age(rec)
        age_src = 'assigned(stage_hash)'
    gender, gsrc = derive_gender_safe(rec)
    rec['_gender'] = gender
    card['gender'] = gender
    card['age'] = age
    card['age_band'] = D.age_band(age)
    birth_year = (2026 - age) if age else None
    card['birth_year'] = birth_year
    ch = rec['city_housing']
    home = _hometown(rec)
    card['birth_province'] = home or ch.get('province')
    card['birth_city'] = ch.get('city')
    card['generation'] = D.generation(birth_year)
    edu = rec['asset'].get('education')
    if not edu:
        for label, val in detail_items(rec, 'base'):
            if '学历' in label:
                m = re.search(r'(博士|硕士|本科|大专|中专|高中|初中|MBA|EMBA)', val)
                edu = m.group(1) if m else None
    card['education'] = edu

    # ── health ──
    grade = rec.get('health_grade')
    if grade is None:
        txt = detail_text(rec, 'health') + detail_text(rec, 'base')
        if 'C' in txt and re.search(r'C\b|[ABC]（', txt):
            pass
    card['health_grade'] = grade
    conds = rec.get('conditions') or []
    if not conds:
        for label, val in detail_items(rec, 'base'):
            if '身体' in label or '健康' in label:
                conds = [c.strip() for c in re.split(r'[+＋]', val) if c.strip()]
    card['health_conditions'] = conds
    risks, _ = D.derive_health_risks(conds, rec['career']['occupation'])
    card['health_risks'] = risks

    # ── career ──
    occ = rec['career']['occupation'] or (l3name)
    card['industry_l1'] = l1
    card['industry_l2'] = l2
    card['industry_l3'] = l3
    card['occupation'] = occ
    emp, empsrc = D.derive_employment(occ, rec.get('detail_subs'))
    card['employment'] = emp
    card['employer'] = _employer(occ, ch, emp)
    work, _ = D.derive_work(occ, rec['career'].get('structure_hint'), emp)
    card['work_intensity'] = work
    card['career_stage'] = D.derive_stage(occ)[0]

    # ── income ──
    inc = rec['career']['income_monthly']
    card['income_monthly'] = inc
    rng = rec['career']['income_range']
    card['income_range'] = rng if rng[0] else ([int(inc * 0.85), int(inc * 1.2)] if inc else None)
    st, stab, _ = D.derive_income_structure(rec['career'].get('structure_hint', '') + occ)
    card['income_structure'] = st
    card['income_stability'] = stab
    fam_raw = rec['family']['raw']
    hh, _ = D.derive_household_income(inc, fam_raw, rec['family'].get('marital'))
    card['household_monthly'] = hh

    # ── finance ──
    fin = rec['finance']
    card['monthly_expense'] = fin.get('expense')
    card['savings_stock'] = fin.get('savings')
    card['debt_stock'] = fin.get('debt')
    if inc and fin.get('expense'):
        card['savings_rate'] = round((inc - fin['expense']) / inc, 3)
    else:
        card['savings_rate'] = None
    assets = D.build_assets(rec['asset'].get('assets'), ch, fin.get('savings'))
    card['assets'] = assets
    card['debts'] = D.build_debts(fin.get('debt'), ch, rec['asset'].get('assets'))
    card['net_worth'] = (fin.get('savings') or 0) - (fin.get('debt') or 0) \
        if (fin.get('savings') is not None or fin.get('debt') is not None) else None

    # ── protection ──
    certs = rec['asset'].get('certs') or []
    funds = rec['asset'].get('funds') or []
    ins = rec['asset'].get('insurance') or []
    if not ins:
        ins, _ = D.derive_insurance(emp, funds, occ)
    card['certs'] = certs
    card['funds'] = funds
    card['insurance'] = ins

    # ── family ──
    fam = rec['family']
    kids = fam.get('children') or []
    card['marital'] = fam.get('marital')
    card['children_count'] = sum(k.get('count', 1) for k in kids) if kids else 0
    ages = []
    for k in kids:
        for a in k.get('ages') or []:
            if a is not None:
                ages.append(a)
    card['children_ages'] = sorted(ages)
    elders = fam.get('elders_dependent')
    card['elders_dependent'] = elders if elders is not None else 0
    card['household_type'] = D.derive_household_type(
        card['children_count'], card['elders_dependent'], card['marital'])
    role = D.derive_family_role(inc, hh)
    card['family_role'] = role or '未采集'

    # ── housing ──
    card['housing_tenure'] = ch.get('tenure') or '未知'
    card['housing_city'] = ch.get('city') or ('海外' if ch.get('overseas') else '未知')
    card['housing_detail'] = ch.get('detail') or ''
    card['mortgage_left'] = ch.get('mortgage_left')

    # ── psych ──
    st_status, st_sat = D.derive_emotion(card['marital'], fam_raw)
    card['emotion_status'] = st_status
    prof = rec['profile']
    lvl, srcs, _ = D.derive_stress(prof.get('items'), prof.get('biases'))
    card['stress_level'] = lvl
    card['stress_sources'] = srcs
    card['biases'] = prof.get('biases') or []

    # ── goals ──
    goals, opps, dream, _ = D.derive_goals(age, inc)
    ge = rec.get('goal_event') or {}
    if ge.get('items'):
        goals = [{'horizon': '开局', 'text': t} for t in ge['items'][:3]]
    card['goals_short'] = goals
    o2, _ = D.derive_opportunities(prof.get('items'), occ)
    card['opportunities'] = opps or o2
    card['dream_cost'] = dream

    # ── transport ──
    owned, mode, _ = D.derive_transport(inc, ch.get('city'), occ)
    card['transport_owned'] = owned
    card['transport_mode'] = mode

    # ── provenance ──
    srcs_used = {}
    for f in D.COUNTED_FIELDS:
        v = card.get(f)
        srcs_used[f] = 'explicit' if _nonempty(v) else 'missing'
    # 覆盖为已知的推导/分配来源
    srcs_used.update({
        'gender': gsrc, 'age': age_src,
        'age_band': 'derived(age)', 'birth_year': 'derived(age)',
        'generation': 'derived(age)', 'health_risks': 'derived(conditions)',
        'employer': 'assigned(template)', 'work_intensity': 'derived(occupation)',
        'career_stage': 'derived(occupation)', 'income_range': 'derived(income)',
        'income_structure': 'derived(income_text)', 'income_stability': 'derived(structure)',
        'household_monthly': 'derived(spouse_cell)' if hh else 'missing',
        'savings_rate': 'derived(income-expense)',
        'net_worth': 'derived(savings-debt)',
        'insurance': 'explicit' if rec['asset'].get('insurance') else 'derived(employment)',
        'household_type': 'derived(family)', 'family_role': 'derived(income)',
        'emotion_status': 'derived(marital)', 'stress_level': 'derived(profile_cell)',
        'stress_sources': 'derived(profile_cell)', 'biases': 'derived(profile_cell)',
        'goals_short': 'assigned(age_income_template)',
        'opportunities': 'derived(profile_cell)',
        'dream_cost': 'assigned(age_income_template)',
    })
    missing = [f for f, v in srcs_used.items() if v == 'missing'
               and f not in ('card_type', 'richness', 'schema_version')]
    explicit = [f for f, v in srcs_used.items() if v == 'explicit']
    derived = [f for f, v in srcs_used.items() if v.startswith('derived')]
    assigned = [f for f, v in srcs_used.items() if v.startswith('assigned')]
    card['_sources'] = {'explicit': explicit, 'derived': derived,
                        'assigned': assigned, 'missing': missing}
    card['_source_counts'] = {'explicit': len(explicit), 'derived': len(derived),
                              'assigned': len(assigned), 'missing': len(missing)}
    card['_completeness'] = round(
        (len(D.COUNTED_FIELDS) - len(missing)) / len(D.COUNTED_FIELDS), 3)
    card['_grounded'] = round(len(explicit) / len(D.COUNTED_FIELDS), 3)
    card['_legacy_ids'] = [rec['id']] + ([rec['_legacy_dup']] if rec.get('_legacy_dup') else [])
    card['_raw'] = {'src_line': rec['src_line'][:400]}
    if rec.get('_legacy_name'):
        card['_raw']['legacy_name'] = rec['_legacy_name']
    card['_family_raw'] = fam_raw or ''
    card['_detail_hook'] = _hook_text(rec)
    return card, {'l1': l1, 'l2': l2, 'l3': l3, 'l3name': l3name,
                  'region': region_of(rec), 'age_band': card['age_band'] or 'XX-未知'}


def _hook_text(rec):
    for label, val in detail_items(rec, 'hook'):
        if val:
            return val
    it = (rec.get('goal_event') or {}).get('items') or []
    return '；'.join(it[:3])


def assign_age(rec):
    """原始档案未标年龄时，按职业阶段关键词 + 编号哈希分配（合成值，如实标注）。"""
    occ = rec['career'].get('occupation') or ''
    stage = D.derive_stage(occ)[0]
    bands = {'入门': (22, 27), '初级': (24, 32), '骨干': (28, 45),
             '资深': (33, 50), '管理': (38, 55)}
    lo, hi = bands.get(stage, (25, 50))
    h = D._hash_unit(rec['id'], 'age')
    return lo + int(h * (hi - lo))


def _seg(s):
    """路径段净化：斜杠、反斜杠、冒号等替换为下划线，避免产生额外目录层级。"""
    return re.sub(r'[/\\:*?"<>|\s]+', '_', str(s)).strip('_') or '未命名'


def _nonempty(v):
    if v is None or v == '' or v == [] or v == {}:
        return False
    if isinstance(v, str) and v in ('未知', '未采集'):
        return False
    return True


def _batch_of(src):
    m = re.match(r'^(\d+)', src)
    n = int(m.group(1)) if m else 0
    if n >= 1620:
        return 'v3.4'
    if n >= 1152:
        return 'v3.2'
    if n >= 152:
        return 'v2.57'
    if n >= 100:
        return 'v2.55'
    return 'v1.1'


def derive_gender_safe(rec):
    try:
        return D.derive_gender(rec)
    except Exception:
        return '男', 'assigned(hash)'


def _hometown(rec):
    for label, val in detail_items(rec, 'base'):
        if '籍贯' in label:
            m = re.match(r'^([一-龥]{2,4}?)(省|市|自治区)?', val)
            if m:
                return m.group(1)
    return None


def _employer(occ, ch, emp):
    city = ch.get('city') or '本地'
    if emp == '个体经营':
        return '%s自营主体' % city
    if emp in ('平台就业',):
        return '平台用工（众包/加盟）'
    if emp == '自由职业':
        return '自由职业/接单'
    return '%s用人单位' % city


def yaml_dump(obj):
    return yaml.safe_dump(obj, allow_unicode=True, sort_keys=False,
                          default_flow_style=None, width=10000).rstrip()


def render(card, pathinfo):
    fm = {k: v for k, v in card.items() if not k.startswith('_')}
    body = render_body(card, pathinfo)
    prov = {
        '_legacy_ids': card['_legacy_ids'],
        '_completeness': card['_completeness'],
        '_grounded': card['_grounded'],
        '_sources': card['_source_counts'],
        '_missing': card['_sources']['missing'],
        '_raw': card['_raw'],
    }
    text = '---\n' + yaml_dump(fm) + '\n' + yaml_dump(prov) + '\n---\n\n' + body
    return text


def render_body(card, pi):
    L1n = L1_NAMES.get(pi['l1'], '')
    l2n = l2_index().get(pi['l2'], (None, ''))[1]
    out = []
    out.append('# %s · %s · %s 岁 · %s' % (
        card['name'], card['gender'] or '未知',
        card['age'] if card['age'] else '未知', card['occupation'] or pi['l3name']))
    out.append('')
    out.append('> **一句话画像**：%s%s · %s。' % (
        card['housing_city'] or '未知城市',
        ('（%s）' % card['housing_tenure']) if card['housing_tenure'] not in ('未知', None) else '',
        '、'.join(card['stress_sources'][:2]) if card['stress_sources'] else '职业与家庭的双重压力'))
    out.append('')
    out.append('| 项 | 值 |')
    out.append('|---|---|')
    out.append('| 行业域 | `%s` %s |' % (pi['l1'], L1n))
    out.append('| 行业细分 | `%s` %s |' % (pi['l2'], l2n))
    out.append('| 职业族 | `%s` %s |' % (pi['l3'], pi['l3name']))
    out.append('| 原始职业 | %s |' % (card['occupation'] or '未采集'))
    out.append('| 月收入 | %s |' % _money(card['income_monthly']))
    out.append('| 收入结构 | %s（稳定性：%s） |' % (card['income_structure'], card['income_stability']))
    out.append('| 健康档 | %s |' % (card['health_grade'] or '未知'))
    out.append('| 完整度 | %.0f%% ｜ 有据率 %.0f%% |' % (
        card['_completeness'] * 100, card['_grounded'] * 100))
    out.append('')
    # 1
    out.append('## 1. 基础档案')
    out.append('')
    out.append('- **年龄**：%s 岁（%s）' % (card['age'] or '未知', card['age_band'] or '未知'))
    out.append('- **性别**：%s' % (card['gender'] or '未知'))
    out.append('- **出生**：%s 年（%s）' % (card['birth_year'] or '未知', card['generation'] or '未知'))
    out.append('- **籍贯**：%s' % (card['birth_province'] or '未采集'))
    out.append('- **学历**：%s' % (card['education'] or '未采集'))
    out.append('- **现居**：%s ｜ %s' % (card['housing_city'], card['housing_detail'] or '未采集'))
    out.append('- **身体**：%s' % ('、'.join(card['health_conditions']) or '未采集'))
    out.append('')
    # 2
    out.append('## 2. 家庭与抚养')
    out.append('')
    out.append('- **婚姻**：%s ｜ **情感状况**：%s' % (card['marital'] or '未采集', card['emotion_status']))
    out.append('- **子女**：%d 人%s' % (
        card['children_count'],
        ('（年龄 %s）' % '、'.join(str(a) for a in card['children_ages'])) if card['children_ages'] else ''))
    out.append('- **需赡养老人**：%d 人' % card['elders_dependent'])
    out.append('- **家庭类型**：%s ｜ **家庭角色**：%s' % (card['household_type'], card['family_role']))
    out.append('- **原档家庭描述**：%s' % (get_family_raw(card) or '未采集'))
    out.append('')
    # 3
    out.append('## 3. 职业与收入')
    out.append('')
    out.append('- **就业形态**：%s ｜ **用人单位**：%s ｜ **职业阶段**：%s' % (
        card['employment'], card['employer'], card['career_stage']))
    out.append('- **月收入**：%s（区间 %s）｜ **家庭月收入**：%s' % (
        _money(card['income_monthly']),
        ('%s–%s' % (_money(card['income_range'][0]), _money(card['income_range'][1])))
        if card['income_range'] else '未采集',
        _money(card['household_monthly'])))
    out.append('- **工作强度**：每周约 %s 小时 ｜ 加班 %s ｜ 职业风险 %s' % (
        card['work_intensity']['weekly_hours'], card['work_intensity']['overtime'],
        card['work_intensity']['risk']))
    out.append('')
    # 4
    out.append('## 4. 财务快照')
    out.append('')
    out.append('- **月支出**：%s ｜ **月结余**：%s ｜ **储蓄率**：%s' % (
        _money(card['monthly_expense']),
        _money((card['income_monthly'] or 0) - (card['monthly_expense'] or 0))
        if card['income_monthly'] and card['monthly_expense'] else '未采集',
        ('%.0f%%' % (card['savings_rate'] * 100)) if card['savings_rate'] is not None else '未采集'))
    out.append('- **储蓄存量**：%s ｜ **负债存量**：%s ｜ **净结余**：%s' % (
        _money(card['savings_stock']), _money(card['debt_stock']), _money(card['net_worth'])))
    out.append('- **房贷余额**：%s' % (_money(card['mortgage_left']) if card['mortgage_left'] else '无'))
    if card['debts']:
        out.append('- **负债明细**：')
        for d in card['debts']:
            out.append('  - %s：%s（来源：%s）' % (d.get('type'), _money(d.get('balance')), d.get('source')))
    if card['assets']:
        out.append('- **资产明细**：')
        for a in card['assets'][:6]:
            out.append('  - %s：%s' % (a.get('type'), a.get('desc') or _money(a.get('value_cny'))))
    out.append('- **学历/证书**：%s ｜ **公积金/基金**：%s ｜ **保障**：%s' % (
        '、'.join(card['certs'][:6]) or '未采集',
        '、'.join(card['funds']) or '无',
        '、'.join(card['insurance']) or '未采集'))
    out.append('')
    # 5
    out.append('## 5. 健康与压力')
    out.append('')
    out.append('- **健康档位**：%s（A 稳定 / B 可控慢病或劳损 / C 重大风险）' % (card['health_grade'] or '未知'))
    out.append('- **健康状况**：%s' % ('、'.join(card['health_conditions']) or '未采集'))
    out.append('- **健康风险**：%s' % ('、'.join(card['health_risks'])))
    out.append('- **压力等级**：%s' % card['stress_level'])
    out.append('- **压力源**：%s' % ('、'.join(card['stress_sources']) or '未采集'))
    out.append('')
    # 6
    out.append('## 6. 情感与人格')
    out.append('')
    out.append('- **情感状况**：%s' % card['emotion_status'])
    out.append('- **行为金融偏差**：%s' % ('、'.join(card['biases']) or '未采集'))
    out.append('- **人格特征**：%s' % ('【待富化】' if card['richness'] == 'skeleton' else card.get('personality', '')))
    out.append('')
    # 7
    out.append('## 7. 人生目标与机会')
    out.append('')
    if card['goals_short']:
        for g in card['goals_short']:
            out.append('- **%s**：%s' % (g.get('horizon'), g.get('text')))
    else:
        out.append('- 未采集')
    if card['dream_cost']:
        out.append('- **梦想成本估算**：约 %s' % _money(card['dream_cost']))
    if card['opportunities']:
        out.append('- **机会/应对**：%s' % '；'.join(card['opportunities']))
    out.append('')
    # 8
    out.append('## 8. 开局钩子')
    out.append('')
    hook = detail_text_named(card, 'hook')
    out.append(hook or '【待富化】')
    out.append('')
    # 9
    out.append('## 9. 数据溯源')
    out.append('')
    sc = card['_source_counts']
    out.append('- **旧编号**：%s ｜ **来源档案**：`%s`' % (
        '、'.join(card['_legacy_ids']), card['source_file']))
    out.append('- **字段来源统计**：有据 %d ｜ 推导 %d ｜ 分配（合成） %d ｜ 缺失 %d（共 %d 字段）'
               % (sc['explicit'], sc['derived'], sc['assigned'], sc['missing'],
                  len(D.COUNTED_FIELDS)))
    if card['_sources']['missing']:
        out.append('- **缺失字段**：%s' % '、'.join(card['_sources']['missing']))
    out.append('- **原档行**：`%s`' % card['_raw']['src_line'].replace('|', '\\|')[:300])
    out.append('')
    return '\n'.join(out)


def get_family_raw(card):
    return card.get('_family_raw', '')


def detail_text_named(card, key):
    return card.get('_detail_hook', '')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--persons', required=True)
    ap.add_argument('--classified', required=True)
    ap.add_argument('--out', required=True)
    ap.add_argument('--limit', type=int, default=0)
    ap.add_argument('--dry', action='store_true')
    args = ap.parse_args()

    recs = [json.loads(l) for l in open(args.persons, encoding='utf-8')]
    if args.limit:
        recs = recs[:args.limit]
    cls = load_classification(args.classified)
    l3idx = build_l3_index(cls)
    print('人物记录:', len(recs), ' 分类结果:', len(cls), ' L3 族:', len(l3idx))

    # 1) 性别先行（姓名去重需要）
    for r in recs:
        r['_gender'] = derive_gender_safe(r)[0]
    # 2) 编号冲突
    n_id = resolve_ids(recs)
    # 3) 姓名冲突
    n_name, n_rebuilt = resolve_names(recs, build_occ_substrings(recs))
    print('编号重号修复:', n_id, ' 姓名重名修复:', n_name, ' 姓名重建:', n_rebuilt)

    stats = collections.Counter()
    missing_hist = []
    unmapped = collections.Counter()
    written = 0
    for r in recs:
        card, pi = build(r, cls, l3idx)
        if not cls.get(r['src_file']):
            unmapped[r['src_file']] += 1
        stats['completeness_ok' if card['_completeness'] >= 0.80 else 'completeness_low'] += 1
        missing_hist.append(card['_completeness'])
        stats['grade_%s' % (card['health_grade'] or 'X')] += 1
        stats['region_%s' % pi['region']] += 1
        stats['l1_%s' % pi['l1']] += 1
        if args.dry:
            written += 1
            continue
        d = os.path.join(args.out,
                         '%s-%s' % (pi['l1'], _seg(L1_NAMES.get(pi['l1'], ''))),
                         '%s-%s' % (pi['l2'], _seg(l2_index().get(pi['l2'], (None, ''))[1])),
                         '%s-%s' % (pi['l3'], _seg(pi['l3name'])),
                         _seg(pi['region']), _seg(pi['age_band']))
        os.makedirs(d, exist_ok=True)
        rf = r['_new_name'] or r['_new_id']
        fn = '%s-%s.md' % (card['id'], re.sub(r'[/\\]', '_', rf))
        with open(os.path.join(d, fn), 'w', encoding='utf-8') as fh:
            fh.write(render(card, pi))
        written += 1

    print('写出卡片:', written)
    for k in sorted(stats):
        if k.startswith(('completeness', 'grade')):
            print('  %-22s %d' % (k, stats[k]))
    if missing_hist:
        print('  完整度 min/avg: %.3f / %.3f' %
              (min(missing_hist), sum(missing_hist) / len(missing_hist)))
    if unmapped:
        print('  未分类文件数:', len(unmapped))
        for f, c in unmapped.most_common(10):
            print('    ', f, c)
    if not args.dry:
        print('\nL1 分布:')
        for k in sorted(stats):
            if k.startswith('l1_'):
                print('   %s %-16s %6d' % (k[3:], L1_NAMES.get(k[3:], ''), stats[k]))


def _money(v):
    if v is None:
        return '未采集'
    try:
        v = float(v)
    except (TypeError, ValueError):
        return str(v)
    if v >= 10000:
        return '%.1f 万' % (v / 10000)
    return '%d 元' % int(v)


if __name__ == '__main__':
    sys.exit(main())
