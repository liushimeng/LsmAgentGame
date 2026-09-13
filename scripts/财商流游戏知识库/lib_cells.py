#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""财商流游戏知识库重构 —— 列语义映射与备注串解掩码（unmask）

旧档案共 10 种表头变体，列语义各不相同。本模块把「表头指纹 → 槽位列表」
固化下来，再把每个备注串解析为结构化字段。
"""
import re

# ── 槽位常量 ────────────────────────────────────────────────────
S_ID = 'id'
S_NAME = 'name'
S_NAME_AGE_BODY = 'name_age_body'
S_CITY_HOUSING = 'city_housing'
S_FAMILY = 'family'
S_CAREER_INCOME = 'career_income'
S_FINANCE = 'finance'
S_ASSET_PROTECTION = 'asset_protection'
S_PROFILE = 'profile'
S_GOAL_EVENT = 'goal_event'
S_PERSON = 'person'
S_OCCUPATION = 'occupation'
S_INCOME_ANCHOR = 'income_anchor'
S_DATA_SOURCE = 'data_source'
S_FAMILY_BODY_ASSET = 'family_body_asset'
S_TENSION = 'tension'
S_CITY = 'city'
S_AGE = 'age'
S_SALARY = 'salary'
S_ASSET_WAN = 'asset_wan'
S_MISC = 'misc'

# ── 表头指纹 → 槽位列表 ─────────────────────────────────────────
# 键为「去掉空格与 `编号 |` 前缀后的表头串」，值为槽位序列（不含 id）
HEADER_MAP = {
    '姓名·年龄/身体|城市与住房|家庭/抚养|职业·月收入|支出/储蓄/负债|资产与保障|财商画像':
        [S_NAME_AGE_BODY, S_CITY_HOUSING, S_FAMILY, S_CAREER_INCOME, S_FINANCE,
         S_ASSET_PROTECTION, S_PROFILE],
    '姓名·年龄/身体|城市与住房|家庭/抚养|职业·月收入|支出/储蓄/负债|资产与保障|财商画像|目标与开局事件':
        [S_NAME_AGE_BODY, S_CITY_HOUSING, S_FAMILY, S_CAREER_INCOME, S_FINANCE,
         S_ASSET_PROTECTION, S_PROFILE, S_GOAL_EVENT],
    '姓名·年龄/健康|城市与住房|家庭/抚养|职业·月收入|支出/储蓄/负债|资产与保障|财商画像|目标与开局事件':
        [S_NAME_AGE_BODY, S_CITY_HOUSING, S_FAMILY, S_CAREER_INCOME, S_FINANCE,
         S_ASSET_PROTECTION, S_PROFILE, S_GOAL_EVENT],
    '姓名·年龄/身体|城市/住房|家庭|职业·月收入|净现金与负债|核心经济暴露|财商切口':
        [S_NAME_AGE_BODY, S_CITY_HOUSING, S_FAMILY, S_CAREER_INCOME, S_FINANCE,
         S_PROFILE, S_PROFILE],
    '姓名·年龄/身体|城市/住房|家族/抚养|职业·月收入|支出/储蓄/负债|资产与保障|财商画像':
        [S_NAME_AGE_BODY, S_CITY_HOUSING, S_FAMILY, S_CAREER_INCOME, S_FINANCE,
         S_ASSET_PROTECTION, S_PROFILE],
    # 人物 | 职业（细分方向） | 月收入（含浮动） | 城市/住房 | 家庭/身体/资产要点
    '人物|职业（细分方向）|月收入（含浮动）|城市/住房|家庭/身体/资产要点':
        [S_PERSON, S_OCCUPATION, S_SALARY, S_CITY_HOUSING, S_FAMILY_BODY_ASSET],
    # 编号 | 姓名 | 职业 | 城市 | 年龄 | 月薪(元) | 资产(万元) | 核心张力
    '姓名|职业|城市|年龄|月薪(元)|资产(万元)|核心张力':
        [S_NAME, S_OCCUPATION, S_CITY, S_AGE, S_SALARY, S_ASSET_WAN, S_TENSION],
    '姓名|职业|城市/村|年龄|月薪(元)|资产(万元)|核心张力':
        [S_NAME, S_OCCUPATION, S_CITY, S_AGE, S_SALARY, S_ASSET_WAN, S_TENSION],
    # 编号 | 职业 | 收入锚点 | 主要数据来源
    '职业|收入锚点|主要数据来源':
        [S_OCCUPATION, S_INCOME_ANCHOR, S_DATA_SOURCE],
    # 编号 | 职业 | 财商教学切口 | 与相近卡池的边界
    '职业|财商教学切口|与相近卡池的边界':
        [S_OCCUPATION, S_PROFILE, S_MISC],
}


def norm_header(line):
    """把表头行归一化：去 `| 编号 |` 前缀、去空格、统一括号。"""
    body = line.strip().strip('|').strip()
    parts = [c.strip() for c in body.split('|')]
    if parts and parts[0] in ('编号', 'ID', 'id'):
        parts = parts[1:]
    s = '|'.join(parts)
    s = s.replace(' ', '').replace('　', '')
    return s


def lookup_slots(header_line):
    """返回槽位列表；未知表头返回 None。"""
    return HEADER_MAP.get(norm_header(header_line))


# ── 备注串解掩码 ────────────────────────────────────────────────

def parse_name_age_body(cell):
    """`厨店长 · 38/腰椎B+应酬肝B` / `谷砚秋 · 男42/咽喉B+腰B` /
    `苏曼凝 · 29/腰椎B+肩颈B+膝关节B` / `厨店长 · 38/腰椎B+应酬肝B`"""
    cell = cell.strip()
    # 姓名可含中间点（如 `柯蘅·芳`），故以「最后一个后接年龄的 ·」为界
    m = re.match(r'^(.*?)\s*[·・]\s*((?:[男女]\s*[·・]?\s*)?\d{1,3}\s*[/／].*)$', cell)
    if not m:
        m2 = re.match(r'^(.*?)\s*[·・]\s*((?:[男女]\s*[·・]?\s*)?\d{1,3}\s*岁?.*)$', cell)
        m = m2
    if not m:
        # 无年龄的形态：`黎亦辰 · 腕管B/过敏B/胃病B` —— 以首个点为界
        parts = re.split(r'\s*[·・]\s*', cell, maxsplit=1)
        if len(parts) == 2:
            name, rest = parts[0].strip(), parts[1].strip()
        else:
            return {'name': cell, 'age': None, 'gender': None, 'health_raw': '',
                    'health_grade': None, 'conditions': []}
    else:
        name = m.group(1).strip()
        rest = m.group(2).strip()
    gender = None
    # 支持 `38/...`、`男 38/...`、`男 · 38/...`、`男·38岁` 等写法
    gm = re.match(r'^(男|女)\s*[·・]?\s*(\d{1,3})', rest)
    if gm:
        gender, age, after = gm.group(1), int(gm.group(2)), rest[gm.end():]
    else:
        am = re.match(r'^(\d{1,3})', rest)
        age = int(am.group(1)) if am else None
        after = rest[am.end():] if am else rest
        if age is None:
            fm = re.match(r'^(男|女)\s*[·・]?\s*', rest)
            if fm:
                gender = fm.group(1)
                after = rest[fm.end():]
    health_raw = after.lstrip('/／ ').strip()
    grade = None
    gm2 = re.search(r'([ABC])(?:$|\+)', health_raw or 'Z')
    if health_raw:
        grades = re.findall(r'([ABC])(?=\+|$)', health_raw)
        if 'C' in grades:
            grade = 'C'
        elif 'B' in grades:
            grade = 'B'
        elif 'A' in grades:
            grade = 'A'
    conditions = [c for c in re.split(r'[+＋/／]', health_raw) if c] if health_raw else []
    conditions = [re.sub(r'[ABC]$', '', c).strip('，,;； ') for c in conditions]
    ABBR = {'颈', '腰', '胃', '肝', '咽', '眼', '耳', '肾', '肺', '肩', '腕'}
    conditions = [c for c in conditions if len(c) >= 2 or c in ABBR]
    return {'name': name, 'age': age, 'gender': gender, 'health_raw': health_raw,
            'health_grade': grade, 'conditions': conditions}


CITY_RE = re.compile(
    r'^(北京|上海|天津|重庆|广东|江苏|浙江|安徽|福建|江西|山东|河南|湖北|湖南|'
    r'河北|山西|内蒙古|辽宁|吉林|黑龙江|广西|海南|四川|贵州|云南|西藏|陕西|甘肃|'
    r'青海|宁夏|新疆|香港|澳门|台湾|美国|日本|新加坡|英国|德国|澳大利亚|加拿大|'
    r'韩国|法国|泰国|越南|马来|迪拜|阿联酋)')

CITY_TOKEN = re.compile(
    r'(北京|上海|天津|重庆|广州|深圳|杭州|南京|苏州|成都|武汉|西安|郑州|长沙|青岛|'
    r'厦门|宁波|无锡|合肥|福州|济南|沈阳|大连|哈尔滨|长春|昆明|贵阳|南宁|石家庄|'
    r'太原|兰州|银川|西宁|乌鲁木齐|呼和浩特|海口|三亚|佛山|东莞|珠海|中山|惠州|'
    r'温州|嘉兴|绍兴|台州|金华|泉州|漳州|烟台|潍坊|徐州|常州|南通|扬州|盐城|'
    r'洛阳|襄阳|宜昌|岳阳|常德|绵阳|德阳|宜宾|遵义|赣州|九江|芜湖|蚌埠|临沂|'
    r'聊城|菏泽|邯郸|唐山|保定|廊坊|包头|鄂尔多斯|大庆|齐齐哈尔|吉林市|鞍山|'
    r'抚顺|咸阳|宝鸡|天水|曲靖|桂林|柳州|北海|汕头|湛江|茂名|揭阳|莆田|龙岩|'
    r'香港|澳门|台北|新加坡|洛杉矶|纽约|旧金山|西雅图|东京|大阪|首尔|曼谷|'
    r'胡志明|雅加达|迪拜|伦敦|巴黎|柏林|悉尼|墨尔本|多伦多|温哥华|'
    r'南昌|安庆|拉萨|秦皇岛|淄博|济宁|泰安|德州|威海|日照|新乡|安阳|焦作|信阳|'
    r'驻马店|南阳|许昌|平顶山|商丘|周口|黄石|十堰|荆州|孝感|株洲|湘潭|衡阳|'
    r'邵阳|郴州|永州|怀化|娄底|韶关|梅州|汕尾|河源|阳江|清远|潮州|云浮|肇庆|'
    r'江门|茂名|揭阳|龙岩|三明|南平|宁德|宿迁|连云港|淮安|泰州|镇江|湖州|衢州|'
    r'丽水|舟山|铜陵|马鞍山|淮南|淮北|安庆|黄山|六安|阜阳|宿州|滁州|亳州|'
    r'开封|濮阳|漯河|三门峡|荆门|鄂州|黄冈|咸宁|随州|恩施|张家界|益阳|'
    r'梧州|钦州|贵港|玉林|百色|河池|来宾|崇左|贺州|儋州|琼海|文昌|万宁|'
    r'曲靖|玉溪|保山|昭通|丽江|普洱|临沧|遵义|六盘水|安顺|毕节|铜仁|'
    r'绵阳|德阳|宜宾|泸州|内江|乐山|南充|眉山|广元|遂宁|广安|达州|雅安|'
    r'咸阳|宝鸡|渭南|延安|榆林|汉中|安康|商洛|天水|白银|武威|张掖|酒泉|'
    r'西宁|银川|石嘴山|吴忠|固原|中卫|乌鲁木齐|克拉玛依|喀什|伊宁|'
    r'呼和浩特|包头|鄂尔多斯|赤峰|通辽|呼伦贝尔|'
    r'沈阳|大连|鞍山|抚顺|本溪|丹东|锦州|营口|盘锦|'
    r'哈尔滨|大庆|齐齐哈尔|牡丹江|佳木斯|'
    r'长春|吉林市|四平|通化)')

OVERSEAS_HINT = re.compile(
    r'(美国|日本|新加坡|英国|德国|澳大利亚|加拿大|韩国|法国|泰国|越南|马来|迪拜|'
    r'阿联酋|香港|澳门|台湾|海外|境外|非洲|欧洲|东南亚|新西兰|意大利|西班牙|荷兰|'
    r'瑞士|爱尔兰|葡萄牙|希腊|波兰|俄罗斯|巴西|阿根廷|墨西哥|南非|埃及|以色列|'
    r'土耳其|沙特|印度|印尼|菲律宾|柬埔寨|缅甸|老挝|蒙古|朝鲜|唐人街|华人区|'
    r'奥克兰|米兰|马德里|悉尼|墨尔本|伦敦|巴黎|柏林|东京|大阪|首尔|曼谷|雅加达|'
    r'胡志明|洛杉矶|纽约|旧金山|西雅图|多伦多|温哥华|开普敦|内罗毕|圣保罗)')


def parse_city_housing(cell):
    """`广东广州自有95m²,房贷余90万` / `上海张江合租3600` / `美国洛杉矶租房$1500/月`"""
    cell = (cell or '').strip()
    out = {'raw': cell, 'province': None, 'city': None, 'district': None,
           'tenure': None, 'detail': cell, 'mortgage_left': None,
           'housing_area': None, 'rent_monthly': None, 'overseas': False}
    if not cell:
        return out
    if OVERSEAS_HINT.search(cell):
        out['overseas'] = True
    # 省市
    prov = CITY_RE.match(cell)
    if prov:
        out['province'] = prov.group(1)
    cities = CITY_TOKEN.findall(cell)
    if cities:
        out['city'] = cities[0]
    elif out['province']:
        out['city'] = out['province']
    # 区
    dm = re.search(r'^([一-龥]{2,3})(区|县)', cell)
    if dm and not cities:
        out['district'] = dm.group(0)
    # 产权形态
    if re.search(r'自有|全款|无贷|单位分房|已购', cell):
        out['tenure'] = '自有'
    if re.search(r'自建', cell):
        out['tenure'] = '自建'
    if re.search(r'合租', cell):
        out['tenure'] = '合租'
    elif re.search(r'宿舍|公寓|公租房|人才房|周转房', cell) and not out['tenure']:
        out['tenure'] = '单位/保障住房'
    elif re.search(r'租|房租', cell) and not out['tenure']:
        out['tenure'] = '租赁'
    if re.search(r'父母同住|与父母|家中有|同住', cell) and not out['tenure']:
        out['tenure'] = '父母产权'
    # 房贷余额
    mm = re.search(r'(?:房贷|贷款|贷)余\s*([0-9.]+)\s*(万|万元)', cell)
    if mm:
        out['mortgage_left'] = int(float(mm.group(1)) * 10000)
    mm = re.search(r'(?:房贷|贷款)\s*([0-9.]+)\s*(万|万元)', cell)
    if mm and out['mortgage_left'] is None:
        out['mortgage_left'] = int(float(mm.group(1)) * 10000)
    # 面积
    am = re.search(r'([0-9.]+)\s*(?:m²|平米|㎡|平)', cell)
    if am:
        out['housing_area'] = float(am.group(1))
    # 租金
    rm = re.search(r'租\s*([0-9]{3,5})', cell)
    if rm:
        out['rent_monthly'] = int(rm.group(1))
    return out


FAMILY_KEYS = {
    '已婚': '已婚', '未婚': '未婚', '离异': '离异', '丧偶': '丧偶', '寡居': '丧偶',
}


def parse_family(cell):
    """`已婚,女儿10岁;父母县城务农` / `未婚;父母广东个体户` / `离异,儿子15岁;父母需赡养`"""
    cell = (cell or '').strip()
    out = {'raw': cell, 'marital': None, 'children': [], 'elders_dependent': None,
           'origin': '', 'spouse': '', 'detail': cell, 'newlywed': False}
    if not cell:
        return out
    for k, v in FAMILY_KEYS.items():
        if k in cell:
            out['marital'] = v
            break
    if out['marital'] is None and '新婚' in cell:
        out['marital'] = '已婚'
        out['newlywed'] = True
    # 子女（统一抽取）
    out['children'] = extract_children(cell)
    if '儿子大学毕业' in cell or '女儿大学毕业' in cell:
        out['children'].append({'count': 1, 'ages': [], 'note': '成年子女'})
    # 老人
    if re.search(r'父母|父亲|母亲|岳父|岳母|公公|婆婆|奶奶|爷爷', cell):
        if re.search(r'退休|健康|务农|个体|国企|县城|农村|老家|健在', cell) and \
           not re.search(r'需赡养|失能|重病|卧床|养老', cell):
            out['elders_dependent'] = 0
        else:
            out['elders_dependent'] = 2
    if '需赡养' in cell or '赡养' in cell:
        out['elders_dependent'] = 2
    if re.search(r'寡居|母亲\d+岁|母亲 \d+ 岁', cell):
        out['elders_dependent'] = 1
    out['origin'] = cell
    return out


def parse_career_income(cell):
    """`餐厅店长,15800(10000-25000)` / `瑜伽普拉提教练(连锁工作室资深),底薪5500+课时提成,月均12000(旺季18000/淡季9000)`"""
    cell = (cell or '').strip()
    out = {'raw': cell, 'occupation': '', 'income_monthly': None, 'income_range': [None, None],
           'income_note': '', 'structure_hint': ''}
    if not cell:
        return out
    parts = [p.strip() for p in re.split(r'[,，;；]', cell) if p.strip()]
    occ = parts[0] if parts else cell
    # 去掉职业里的括号等级后缀（保留关键词）
    out['occupation'] = re.sub(r'[（(][^）)]*[）)]', '', occ).strip()
    out['occupation_raw'] = occ
    nums = [int(x.replace(',', '')) for x in re.findall(r'(\d[\d,]{2,7})', cell)]
    nums = [n for n in nums if 500 <= n <= 5_000_000]
    rng = re.search(r'[（(]\s*([\d,]{3,9})\s*[-–~至]\s*([\d,]{3,9})', cell)
    if rng:
        lo = int(rng.group(1).replace(',', ''))
        hi = int(rng.group(2).replace(',', ''))
        out['income_range'] = [lo, hi]
        out['income_monthly'] = int(round((lo + hi) / 2))
    if '月均' in cell or '均' in cell:
        m = re.search(r'月均\s*([\d,]{3,9})', cell)
        if m:
            out['income_monthly'] = int(m.group(1).replace(',', ''))
    if out['income_monthly'] is None and nums:
        out['income_monthly'] = nums[0]
    out['income_note'] = ' '.join(parts[1:])[:200]
    out['structure_hint'] = cell
    return out


def parse_finance(cell):
    """`9000/120000/5200` → 支出/储蓄/负债存量"""
    cell = (cell or '').strip()
    out = {'raw': cell, 'expense': None, 'savings': None, 'debt': None, 'note': ''}
    if not cell:
        return out
    # 带文字标签的形态优先（`月支出3800,储蓄8万,负债0` / `储蓄78,000；房贷48万`）
    labelled = re.search(r'(月?支出|储蓄|存款|负债|房贷|月供)', cell)
    if not labelled:
        nums = re.findall(r'([0-9][0-9,]*(?:\.[0-9]+)?)\s*(万)?', cell)
        vals = []
        for v, wan in nums:
            try:
                x = float(v.replace(',', ''))
            except ValueError:
                continue
            if wan:
                x *= 10000
            vals.append(int(x))
        if len(vals) >= 3:
            out['expense'], out['savings'], out['debt'] = vals[0], vals[1], vals[2]
        elif len(vals) == 2:
            out['expense'], out['savings'] = vals[0], vals[1]
        elif len(vals) == 1:
            out['expense'] = vals[0]

    def _amt(pat):
        m = re.search(pat, cell)
        if not m:
            return None
        v = float(m.group(1).replace(',', '').replace('千', '') or 0)
        unit = m.group(2) if m.lastindex and m.lastindex >= 2 else None
        if unit == '万':
            v *= 10000
        elif unit == '千':
            v *= 1000
        return int(v)

    ex = _amt(r'(?:月?支出|月供|月还款)\s*([\d,.]+)\s*(万|千)?')
    if ex:
        out['expense'] = ex
    sv = _amt(r'(?:储蓄|存款)\s*([\d,.]+)\s*(万|千)?')
    if sv:
        out['savings'] = sv
    db = _amt(r'(?:负债|房贷余?额?|欠款)\s*([\d,.]+)\s*(万|千)?')
    if db:
        out['debt'] = db
    dm = re.search(r'房贷\s*([\d.,]+)\s*万', cell)
    if dm and out['debt'] is None:
        out['debt'] = int(float(dm.group(1).replace(',', '')) * 10000)
    out['note'] = ''
    return out


CERT_RE = re.compile(r'(证|资格|执照|证书|认证|CPA|CFA|FRM|ACCA|PMP|RYT|WSET|AEO|'
                     r'一级|二级|三级|中级|高级|注册|硕士|本科|大专|中专|高中|博士|初中|MBA|EMBA)')
INSUR_RE = re.compile(r'(医保|社保|公积金|新农合|商业医疗|重疾|意外险|养老险|年金|团险|'
                      r'补充医疗|工伤险|失业保险|生育险|寿险|医疗险)')


def parse_asset_protection(cell):
    """`本科;餐饮管理师;公积金双边1800` / `房值180万；基金6万；行内存单10万`"""
    cell = (cell or '').strip()
    out = {'raw': cell, 'education': None, 'certs': [], 'funds': [],
           'insurance': [], 'assets': []}
    if not cell:
        return out
    parts = [p.strip() for p in re.split(r'[;；,，]', cell) if p.strip()]
    for p in parts:
        if re.match(r'^(博士|硕士|本科|大专|中专|高中|初中|小学|MBA|EMBA)$', p):
            out['education'] = p
            continue
        if '公积金' in p:
            out['funds'].append(p)
            continue
        if INSUR_RE.search(p) and not re.search(r'[0-9]+\s*万', p):
            out['insurance'].append(p)
            continue
        if re.search(r'(万|元|存单|基金|股票|债|房产|铺面|房值|公寓|存款|定存|理财|黄金|车)', p):
            out['assets'].append(p)
            continue
        if CERT_RE.search(p):
            out['certs'].append(p)
            continue
        if len(p) <= 12:
            out['certs'].append(p)
        else:
            out['assets'].append(p)
    # 学历可能出现在任意片段
    if out['education'] is None:
        m = re.search(r'(博士|硕士|本科|大专|中专|高中|初中|MBA|EMBA)', cell)
        if m:
            out['education'] = m.group(1)
    return out


BIAS_DICT = [
    ('损失厌恶', r'损失|亏损|怕亏|割肉|亏怕'),
    ('过度自信', r'自信|乐观|侥幸|赌|执念|膨胀'),
    ('锚定效应', r'锚定|锚|参照|原价|锚点'),
    ('羊群效应', r'跟风|羊群|从众|跟买|扎堆'),
    ('心理账户', r'心理账户|专款|挪用|专户'),
    ('禀赋效应', r'禀赋|舍不得|惜售|不愿卖'),
    ('现状偏见', r'现状|惯性|路径依赖|稳定偏好|求稳'),
    ('确认偏误', r'确认偏误|只听|选择性|信息茧房'),
    ('沉没成本', r'沉没|已投入|回本|舍不得沉没'),
    ('即时满足', r'即时|拖延|月光|冲动|提前消费|精致穷'),
    ('风险厌恶', r'保守|不敢|稳健|厌恶风险'),
    ('风险寻求', r'激进|杠杆|加仓|豪赌|高风险'),
    ('心理韧性', r'韧性|抗压|坚韧'),
    ('情感劳动', r'情感透支|情感劳动|情绪劳动'),
]


def parse_profile(cell):
    """`门店客流下滑+食材成本上涨+竞争白热化+外卖平台抽佣`"""
    cell = (cell or '').strip()
    parts = [p.strip() for p in re.split(r'[+＋]', cell) if p.strip()]
    biases = []
    for b, pat in BIAS_DICT:
        if re.search(pat, cell):
            biases.append(b)
    return {'raw': cell, 'items': parts, 'biases': biases}


def parse_goal_event(cell):
    cell = (cell or '').strip()
    parts = [p.strip() for p in re.split(r'[;；]', cell) if p.strip()]
    return {'raw': cell, 'items': parts}


def parse_person(cell):
    """`王桂英 · 45 · 女`"""
    parts = [p.strip() for p in re.split(r'[·・]', cell or '')]
    out = {'name': parts[0] if parts else '', 'age': None, 'gender': None}
    for p in parts[1:]:
        if re.fullmatch(r'\d{1,3}', p):
            out['age'] = int(p)
        elif p in ('男', '女'):
            out['gender'] = p
    return out


# ── 打包列解析（变体表头「家庭/身体/资产要点」） ─────────────────
HEALTH_TOKEN = re.compile(r'([一-鿿]{2,8})\s*([ABC])(?=[、,，;；]|$)')
CHILD_GRADE = re.compile(r'(儿子|女儿|孩子|子女)([一-鿿]{0,6})')

CN_NUM = {'一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6}


def extract_children(cell):
    """统一抽取子女：支持 `1子5岁` / `1女2岁` / `双胞胎12岁` / `儿子初中` / `两娃` 等写法。

    返回 children 列表（`count` 恒为 1，重复条目表示多个孩子）。
    """
    out = []
    cell = cell or ''
    if not cell:
        return out
    m = re.search(r'(双胞胎|龙凤胎)\s*(\d{1,2})?\s*岁?', cell)
    if m:
        age = int(m.group(2)) if m.group(2) else None
        for _ in range(2):
            out.append({'count': 1, 'ages': [age] if age is not None else [],
                        'note': m.group(1)})
        return out
    # `1子5岁` / `2女` / `1子随己`
    for m in re.finditer(r'([0-9一二三四五六两])\s*([子女])\s*(\d{1,2})?\s*岁?', cell):
        g = m.group(1)
        n = int(g) if g.isdigit() else CN_NUM.get(g, 1)
        age = int(m.group(3)) if m.group(3) else None
        for _ in range(min(n, 4)):
            out.append({'count': 1, 'ages': [age] if age is not None else [],
                        'note': '儿子' if m.group(2) == '子' else '女儿'})
    if out:
        return out
    # `儿子初中` / `女儿大二`
    for m in CHILD_GRADE.finditer(cell):
        note = (m.group(2) or '').strip()
        if note and any(w in note for w in GRADE_WORDS):
            out.append({'count': 1, 'ages': [], 'note': m.group(1) + note})
    if out:
        return out
    # `女儿10岁`
    for m in re.finditer(r'(女儿|儿子|女孩|男孩)\s*(\d{1,2})?\s*岁', cell):
        age = int(m.group(2)) if m.group(2) else None
        out.append({'count': 1, 'ages': [age] if age is not None else [],
                    'note': m.group(1)})
    if out:
        return out
    if '两娃' in cell or '俩娃' in cell:
        for _ in range(2):
            out.append({'count': 1, 'ages': [], 'note': '两娃'})
    elif re.search(r'一娃|一个娃', cell):
        out.append({'count': 1, 'ages': [], 'note': '一娃'})
    elif re.search(r'独自抚养(孩子|子女|娃)|抚养孩子|带娃|孩子(上|读)|'
                   r'有(一|两个|个)?孩子|育有|生育|儿女', cell):
        out.append({'count': 1, 'ages': [], 'note': '未标明数量'})
    return out
GRADE_WORDS = ('小学', '初中', '高中', '大二', '大三', '大四', '大学', '幼儿园',
               '读研', '研究生', '中专', '大专', '本科', '中考', '高考', '毕业')


def parse_mixed(cell):
    """`已婚，儿子初中；丈夫货运司机；腰肌劳损 B；存款 5 万` → 结构化字段"""
    cell = (cell or '').strip()
    out = {'marital': None, 'children': [], 'conditions': [], 'health_grade': None,
           'savings': None, 'debt': None, 'expense': None, 'assets': [],
           'certs': [], 'insurance': [], 'education': None, 'spouse': '',
           'raw': cell}
    if not cell:
        return out
    for k, v in FAMILY_KEYS.items():
        if k in cell:
            out['marital'] = v
            break
    for m in HEALTH_TOKEN.finditer(cell):
        c = m.group(1).strip('，,;； ')
        if len(c) >= 2 and c not in out['conditions']:
            out['conditions'].append(c)
            g = m.group(2)
            if g == 'C' or (g == 'B' and out['health_grade'] != 'C'):
                out['health_grade'] = 'C' if g == 'C' else 'B'
            elif g == 'A' and out['health_grade'] is None:
                out['health_grade'] = 'A'
    out['children'] = extract_children(cell)
    m = re.search(r'存款\s*([\d.]+)\s*万', cell)
    if m:
        out['savings'] = int(float(m.group(1)) * 10000)
    m = re.search(r'储蓄\s*([\d.]+)\s*万', cell)
    if m and out['savings'] is None:
        out['savings'] = int(float(m.group(1)) * 10000)
    m = re.search(r'房贷\s*(?:余)?\s*([\d.]+)\s*万', cell)
    if m:
        out['debt'] = int(float(m.group(1)) * 10000)
    m = re.search(r'([\d.]+)\s*万\s*(房贷|车贷|经营贷|消费贷)', cell)
    if m and out['debt'] is None:
        out['debt'] = int(float(m.group(1)) * 10000)
    m = re.search(r'(?:月供|月还款)\s*([\d,]{3,6})', cell)
    if m:
        out['expense'] = int(m.group(1).replace(',', ''))
    for seg in re.split(r'[;；,，]', cell):
        seg = seg.strip()
        if not seg:
            continue
        if re.search(r'本科|硕士|大专|中专|高中|博士|研究生', seg) and len(seg) <= 12:
            out['education'] = re.search(
                r'本科|硕士|大专|中专|高中|博士|研究生', seg).group(0)
        if re.search(r'房|铺面|店面|自建房|公寓|车', seg) and '贷款' not in seg:
            out['assets'].append(seg)
        if re.search(r'公积金|医保|社保|保险|年金', seg):
            out['insurance'].append(seg)
        if re.search(r'丈夫|妻子|老婆|老公', seg):
            out['spouse'] = seg
    return out


def parse_asset_wan(cell):
    """`-18（船贷）` / `120` → (assets 描述, debt 数值)"""
    cell = (cell or '').strip()
    m = re.search(r'(-?[\d.]+)', cell)
    if not m:
        return None, None
    v = float(m.group(1))
    if v < 0:
        return None, int(abs(v) * 10000)
    return int(v * 10000), None
