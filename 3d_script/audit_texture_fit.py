#!/usr/bin/env python3
"""
audit_texture_fit — 虚拟城市「贴图 ↔ 几何」保真审计（批次 37 P2，纯 Python + Pillow）。

与 `verify_glb_aabb.py`（几何侧：轴线 / 贴地 / 尺度 / 节点 identity）互补，
本脚本只管**贴图侧**判据，把方案 §1 的手工排查固化为可重复执行的门禁。
**不依赖 Blender**（CI 可单独跑）；GLB 量测逻辑一律复用 verify_glb_aabb 的
`read_glb` / `world_aabb` / `check`，本脚本不另写一份。

判据（任一 FAIL ⇒ 退出码 1；WARN 不影响退出码；依赖缺失 ⇒ SKIP 不阻塞）：
  A1  资产登记表 texScale.ts 的 `tileMetersU/tileMetersV` == `texW/texH`   相对误差 < 0.5%
  A2  磁盘实际像素 == 登记表声明值                                          完全相等
  A3  被 repeat 平铺的贴图四边无缝（左右/上下边缘列行平均像素差，facade_tiles
      颜色图 **与其派生 _n/_r** 全量硬判；roofs/ground 只报不判）                 < 2/255
  A4  派生 PBR 图（`_n`/`_r`）与其颜色图的宽高比一致                        相对误差 < 0.5%
  A5  全量 PNG 物理纹素密度落在标称带内                                      60–120 px/m（WARN）
  A6  GLB：有 `Image Texture` 节点时须 UV 覆盖完整（primitives 带 TEXCOORD_0）≥ 90%
  A7  GLB：节点 transform identity + 贴地 + 尺度                            复用 verify_glb_aabb

用法：
  python3 3d_script/audit_texture_fit.py                      # 全量审计
  python3 3d_script/audit_texture_fit.py --emit-json          # 追加打印实测 JSON（stdout）
  python3 3d_script/audit_texture_fit.py --emit-json out.json # 写入文件
  python3 3d_script/audit_texture_fit.py --check-table t.json # 对齐批次 30 §3.7 既有开关命名
  python3 3d_script/audit_texture_fit.py --only A1 A3 A4      # 只跑部分判据
  python3 3d_script/audit_texture_fit.py --verbose             # 逐文件明细

环境变量：
  IMAGE_TILE_REGISTRY  默认 ClientWeb/src/components/virtualCity/texScale.ts
  VIRTUAL_CITY_IMAGES  默认 ClientWeb/src/assets/images/virtualCity
  GLB_MODELS_DIR       默认 ClientWeb/src/assets/models（与 verify_glb_aabb 共用）
  AUDIT_DENSITY_BAND   默认 "60:120"（A5 标称纹素密度带，px/m）

--emit-json 输出：
  {"judges": {"A1": {...}, ...}, "texels": [...], "models": {"<类别>/<名>": {...}},
   "summary": {"fail": [...], "warn": [...], "skip": [...]}}
  ⚠ 该 json 是一次性量测产物，用于出「修复资产清单」；不要当资产提交。
"""

import glob
import importlib.util
import json
import os
import re
import sys

try:
    from PIL import Image
except ImportError:  # pragma: no cover - 环境缺 Pillow 时给可执行提示而非裸 traceback
    sys.stderr.write('!! 缺 Pillow：pip install pillow 后重试\n')
    raise SystemExit(2)

try:
    import numpy as _np
except ImportError:  # pragma: no cover
    _np = None

# ── 路径（与既有脚本一致：基于脚本自身位置推导，任意 cwd 可运行）────────────
HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(HERE)
REGISTRY_TS = os.environ.get('IMAGE_TILE_REGISTRY') or os.path.join(
    REPO, 'ClientWeb', 'src', 'components', 'virtualCity', 'texScale.ts')
IMAGES_DIR = os.environ.get('VIRTUAL_CITY_IMAGES') or os.path.join(
    REPO, 'ClientWeb', 'src', 'assets', 'images', 'virtualCity')
MODELS_DIR = os.environ.get('GLB_MODELS_DIR') or os.path.join(
    REPO, 'ClientWeb', 'src', 'assets', 'models')

# ── 阈值（方案 §5.1 表）─────────────────────────────────────────────────────
ASPECT_REL_TOL = 0.005   # 0.5%：A1 宽高比不变式 / A4 派生图宽高比
SEAM_MAX_255 = 2.0       # 2/255：A3 四边无缝（0..255 量纲）
UV_COVERAGE_MIN = 0.90   # A6 UV 覆盖下限

# 登记表常量 -> (颜色图目录, 派生图 pbr 子目录)：A2 / A5 共用的唯一映射处，
# 前端改目录名时只改这里。刻意**不**登记 'facades'：pbr/facades/* 配对的是
# facades/<stem>_{base,mid}.png（整栋立面图，无物理周期语义），
# 拿 FACADE_TILE 的 6m×12m 去套它属口径错配，一律记 N/A。
TILE_TARGETS = {
    'FACADE_TILE': ('facade_tiles', 'facade_tiles'),
    'ROOF_TILE': ('roofs', 'roofs'),
}

# A4 派生后缀
PBR_SUFFIXES = ('_n', '_r')

# A3 判集：批次 37 新增的可平铺开间贴图 —— 颜色图 **与其派生 _n/_r** 一并硬判。
# 参考判集：既有的平铺贴图（roofs / ground），同样被 repeat，但属批次 18/26 的历史
# 资产，只报不判 FAIL，避免把既有结论硬翻红。
A3_FAIL_TILES = ('facade_tiles',)
A3_ADVISORY_GLOBS = ('roofs/*.png', 'ground/*_tile.png')

# color_dir -> pbr 子目录（TILE_TARGETS 的反向视图，避免第二份映射表）
COLOR_DIR_TO_PBR = {c: p for c, p in TILE_TARGETS.values()}

# 登记表里必须凑齐的四个字段
NUM_FIELDS = ('texW', 'texH', 'tileMetersU', 'tileMetersV')


def density_band():
    """A5 标称纹素密度带（px/m），可用 AUDIT_DENSITY_BAND 覆盖。"""
    raw = os.environ.get('AUDIT_DENSITY_BAND', '60:120')
    try:
        lo_s, hi_s = raw.split(':', 1)
        return float(lo_s), float(hi_s)
    except ValueError:
        sys.stderr.write('!! AUDIT_DENSITY_BAND 格式应为 "lo:hi"，回退 60:120\n')
        return 60.0, 120.0


def rel_err(a, b):
    """相对误差；分母取两者较大者，避免 0 分母。"""
    d = max(abs(a), abs(b))
    return 0.0 if d == 0 else abs(a - b) / d


def _load_verify():
    """动态 import verify_glb_aabb（纯 stdlib），取其 read_glb / world_aabb / check。"""
    path = os.path.join(HERE, 'verify_glb_aabb.py')
    if not os.path.isfile(path):
        return None
    spec = importlib.util.spec_from_file_location('verify_glb_aabb', path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)  # type: ignore[union-attr]
    return mod


VERIFY = _load_verify()


# ── 图片 IO 小工具 ─────────────────────────────────────────────────────────
def img_size(path):
    """只读 PNG 头部取像素尺寸（不整图解码，A5 遍历几百张时省内存/时间）。"""
    with Image.open(path) as im:
        return im.size


def to_array(path):
    """PNG -> float32 HxWx3（0..255）。无 numpy 时返回 None（相关判据降级为 SKIP）。"""
    if _np is None:
        return None
    with Image.open(path) as im:
        return _np.asarray(im.convert('RGB'), dtype=_np.float32)


def edge_diff(a):
    """四边无缝指标：返回 (左右差, 上下差, 相邻基线差)，均以 0..255 计。

    「相邻基线差」= 图内典型相邻像素差，作为噪声地板参照：真正平铺的图，
    跨缝一步的差应当与图内任意相邻一步同量级。
    """
    left = float(abs(a[:, 0, :] - a[:, -1, :]).mean())
    top = float(abs(a[0, :, :] - a[-1, :, :]).mean())
    base_lr = float(abs(a[:, 0, :] - a[:, 1, :]).mean())
    base_tb = float(abs(a[0, :, :] - a[1, :, :]).mean())
    return left, top, (base_lr + base_tb) / 2.0


# ── 判据 A1 / A2：资产登记表 ───────────────────────────────────────────────
TILE_BLOCK_RE = re.compile(
    r'export\s+const\s+(?P<name>[A-Z0-9_]+)\s*(?::\s*TextureTile)?\s*=\s*\{(?P<body>[^}]*)\}',
    re.S,
)


def parse_registry(path):
    """解析 texScale.ts 里的 `export const XXX: TextureTile = { texW: .., ... }`。

    返回 (dict[name] -> fields, err)。文件缺失返回 ({}, '缺文件')，**不硬失败**：
    texScale.ts 由 frontend-dev 并行产出，缺失时 A1/A2 记 SKIP。
    """
    if not os.path.isfile(path):
        return {}, '文件不存在: %s' % os.path.relpath(path, REPO)
    with open(path, 'r', encoding='utf-8') as fh:
        src = fh.read()
    table, missing = {}, []
    for m in TILE_BLOCK_RE.finditer(src):
        body = m.group('body')
        vals = {}
        for f in NUM_FIELDS:
            fm = re.search(r'\b%s\s*:\s*([0-9]+(?:\.[0-9]+)?)' % f, body)
            if fm:
                vals[f] = float(fm.group(1))
            else:
                missing.append('%s.%s' % (m.group('name'), f))
        if vals:
            table[m.group('name')] = vals
    if not table:
        return {}, '未解析到任何 TextureTile 常量'
    return table, ('字段缺失: ' + ', '.join(missing)) if missing else ''


def judge_a1_a2(table, reg_err):
    """A1 宽高比不变式 + A2 实际像素 == 声明。返回 (a1, a2) 两条结果。"""
    res = {k: {'status': 'skip', 'items': [], 'problems': []} for k in ('A1', 'A2')}
    if reg_err or not table:
        res['A1']['status'] = 'skip'
        res['A1']['reason'] = res['A2']['reason'] = reg_err or '登记表为空'
        sys.stderr.write(
            '⚠ [A1/A2] SKIP —— 资产登记表不可用：%s\n'
            '  （texScale.ts 由 frontend-dev 产出；未到位时本判据不阻塞，到位后自动生效）\n'
            % res['A1']['reason'])
        return res['A1'], res['A2']
    for name, v in sorted(table.items()):
        missing = [f for f in NUM_FIELDS if f not in v]
        if missing:
            res['A1']['problems'].append('%s 缺字段 %s' % (name, ','.join(missing)))
            continue
        decl_asp = v['texW'] / v['texH']
        phys_asp = v['tileMetersU'] / v['tileMetersV']
        e = rel_err(decl_asp, phys_asp)
        res['A1']['items'].append({
            'name': name, 'texW': v['texW'], 'texH': v['texH'],
            'tileMetersU': v['tileMetersU'], 'tileMetersV': v['tileMetersV'],
            'declaredAspect': round(decl_asp, 6), 'physicalAspect': round(phys_asp, 6),
            'relErr': round(e, 6)})
        if e >= ASPECT_REL_TOL:
            res['A1']['problems'].append(
                '%s 宽高比不变式破坏: texW/texH=%.6f vs tileU/tileV=%.6f（相对误差 %.3f%%）'
                % (name, decl_asp, phys_asp, e * 100))
    # A2：登记的每个像素尺寸去磁盘上找对应目录的全量 PNG
    for name, v in sorted(table.items()):
        if any(f not in v for f in NUM_FIELDS) or name not in TILE_TARGETS:
            continue
        for sub in TILE_TARGETS[name][0:1]:
            d = os.path.join(IMAGES_DIR, sub)
            files = sorted(glob.glob(os.path.join(d, '*.png')))
            if not files:
                res['A2']['items'].append({'name': name, 'dir': sub, 'files': 0, 'mismatch': 0})
                res['A2']['problems'].append('%s 声明的目录无 PNG: %s/' % (name, sub))
                continue
            bad = 0
            for p in files:
                w, h = img_size(p)
                if (w, h) != (int(v['texW']), int(v['texH'])):
                    bad += 1
                    res['A2']['problems'].append(
                        '%s: %s 实测 %dx%d != 声明 %dx%d'
                        % (name, os.path.relpath(p, IMAGES_DIR), w, h,
                           int(v['texW']), int(v['texH'])))
            res['A2']['items'].append({'name': name, 'dir': sub,
                                       'files': len(files), 'mismatch': bad})
    for k in ('A1', 'A2'):
        res[k]['status'] = 'fail' if res[k]['problems'] else 'pass'
    return res['A1'], res['A2']


# ── 判据 A3：四边无缝 ─────────────────────────────────────────────────────
def judge_a3():
    """主判集 = 被 repeat 平铺的开间贴图 facade_tiles/*.png（方案 §4.1）。
    参考判集 = roofs/*.png 与 ground/*_tile.png（同样被 repeat，但属既有资产，
    只报 WARN 不判 FAIL，避免把批次 18/26 的历史结论硬翻红）。"""
    res = {'status': 'pass', 'items': [], 'problems': [], 'advisory': []}
    if _np is None:
        res['status'] = 'skip'
        res['reason'] = '缺 numpy，A3 需整图逐像素比对'
        sys.stderr.write('⚠ [A3] SKIP —— 缺 numpy（pip install numpy）\n')
        return res
    main_set = []
    for color_dir in A3_FAIL_TILES:
        main_set += glob.glob(os.path.join(IMAGES_DIR, color_dir, '*.png'))
        pbr_dir = COLOR_DIR_TO_PBR.get(color_dir)
        if pbr_dir:
            main_set += glob.glob(os.path.join(IMAGES_DIR, 'pbr', pbr_dir, '*.png'))
    main_set = sorted(main_set)
    if not main_set:
        res['status'] = 'skip'
        res['reason'] = 'facade_tiles/ 为空或不存在（批次 37 P1 资产未生成）'
        sys.stderr.write('⚠ [A3] SKIP —— %s\n' % res['reason'])
        return res
    for p in main_set:
        a = to_array(p)
        left, top, base = edge_diff(a)
        ok = max(left, top) < SEAM_MAX_255
        res['items'].append({'path': os.path.relpath(p, IMAGES_DIR),
                             'leftDiff': round(left, 3), 'topDiff': round(top, 3),
                             'adjacentBaseline': round(base, 3), 'ok': ok})
        if not ok:
            res['problems'].append(
                '%s 接缝过大: 左右 %.2f / 上下 %.2f（阈值 %.1f，相邻基线 %.2f）'
                % (os.path.relpath(p, IMAGES_DIR), left, top, SEAM_MAX_255, base))
    for pat in A3_ADVISORY_GLOBS:
        for p in sorted(glob.glob(os.path.join(IMAGES_DIR, pat))):
            a = to_array(p)
            left, top, base = edge_diff(a)
            res['advisory'].append({
                'path': os.path.relpath(p, IMAGES_DIR),
                'leftDiff': round(left, 3), 'topDiff': round(top, 3),
                'adjacentBaseline': round(base, 3),
                'ok': max(left, top) < SEAM_MAX_255})
    res['status'] = 'fail' if res['problems'] else 'pass'
    return res


# ── 判据 A4：派生 PBR 图与颜色图宽高比一致 ───────────────────────────────
def judge_a4():
    """遍历 pbr/**/*.png：`pbr/<cat>/<stem>{_n,_r}.png` 的宽高比须与颜色图
    `<cat>/<stem>.png` 一致（相对误差 < 0.5%）。分档不同可以，宽高比不同即 FAIL。"""
    res = {'status': 'pass', 'items': [], 'problems': [], 'orphans': []}
    pbr_root = os.path.join(IMAGES_DIR, 'pbr')
    files = sorted(glob.glob(os.path.join(pbr_root, '**', '*.png'), recursive=True))
    if not files:
        res['status'] = 'skip'
        res['reason'] = 'pbr/ 为空'
        return res
    for p in files:
        rel = os.path.relpath(p, IMAGES_DIR).replace(os.sep, '/')
        base = os.path.basename(p)[:-4]
        kind = next((s for s in PBR_SUFFIXES if base.endswith(s)), None)
        stem = base[: -len(kind)] if kind else base
        cat = os.path.basename(os.path.dirname(p))
        color = os.path.join(IMAGES_DIR, cat, stem + '.png')
        w, h = img_size(p)
        if cat == 'synth':
            # synth 材质按设计**无颜色图**（见 cityPbr.ts 头注），不算孤儿
            res['items'].append({'path': rel, 'px': [w, h], 'color': None,
                                 'colorPx': None, 'relErr': 0.0, 'kind': kind or '-',
                                 'synth': True})
            continue
        if not os.path.isfile(color):
            res['orphans'].append(rel)
            continue
        cw, ch = img_size(color)
        e = rel_err(w / h, cw / ch)
        res['items'].append({'path': rel, 'px': [w, h],
                             'color': os.path.relpath(color, IMAGES_DIR),
                             'colorPx': [cw, ch], 'relErr': round(e, 6),
                             'kind': kind or '-'})
        if e >= ASPECT_REL_TOL:
            res['problems'].append(
                '%s 宽高比与颜色图不符: %dx%d（%.4f）vs %s %dx%d（%.4f），相对误差 %.3f%%'
                % (rel, w, h, w / h, os.path.relpath(color, IMAGES_DIR), cw, ch, cw / ch, e * 100))
    res['status'] = 'fail' if res['problems'] else 'pass'
    return res


# ── 判据 A5：物理纹素密度（WARN）───────────────────────────────────────────
def judge_a5(table, reg_err):
    """纹素密度 = 像素宽 / 该贴图一个周期覆盖的米数。

    物理米数只对**登记表覆盖的平铺贴图**有定义（facade_tiles / roofs 及其派生图）；
    精灵图（props/agents/goods/weather）无物理尺寸语义，记 N/A 不参与判定 ——
    凭空给它们安一个米数才是真正的造假。
    """
    lo, hi = density_band()
    res = {'status': 'pass', 'band': [lo, hi], 'items': [], 'problems': [], 'na': 0}
    if reg_err or not table:
        res['status'] = 'skip'
        res['reason'] = reg_err or '登记表为空（无物理尺寸事实来源）'
        return res

    def record(p, v, name):
        w, h = img_size(p)
        du, dv = w / v['tileMetersU'], h / v['tileMetersV']
        dens = min(du, dv)
        it = {'path': os.path.relpath(p, IMAGES_DIR), 'px': [w, h],
              'meters': [v['tileMetersU'], v['tileMetersV']],
              'densityU': round(du, 2), 'densityV': round(dv, 2),
              'density': round(dens, 2), 'inBand': lo <= dens <= hi, 'registry': name}
        res['items'].append(it)
        if not it['inBand']:
            it['group'] = '%s %dx%d@%gm x %gm' % (name, w, h, v['tileMetersU'], v['tileMetersV'])
            res['problems'].append(
                '%s 纹素密度 %.1f px/m 越出标称带 [%.0f, %.0f]（U %.1f / V %.1f，%s）'
                % (it['path'], dens, lo, hi, du, dv, name))

    for name, v in sorted(table.items()):
        if any(f not in v for f in NUM_FIELDS) or name not in TILE_TARGETS:
            continue
        color_dir, pbr_dir = TILE_TARGETS[name]
        for p in sorted(glob.glob(os.path.join(IMAGES_DIR, color_dir, '*.png'))):
            record(p, v, name)
        # 派生图：仅当**配对颜色图本身就是平铺贴图**才适用同一物理周期。
        # 配对规则与 A4 严格一致：pbr/<cat>/<stem>{_n,_r}.png <-> <cat>/<stem>.png，
        # 少了颜色图（或颜色图不在平铺目录里）一律记 N/A，不硬套米数。
        for p in sorted(glob.glob(os.path.join(IMAGES_DIR, 'pbr', pbr_dir, '*.png'))):
            base = os.path.basename(p)[:-4]
            stem = next((base[: -len(sfx)] for sfx in PBR_SUFFIXES if base.endswith(sfx)), base)
            if not os.path.isfile(os.path.join(IMAGES_DIR, color_dir, stem + '.png')):
                continue
            record(p, v, name)
    # 同一「登记表 + 像素 + 物理尺寸」组合聚合成一行（_n/_r 全量同类刷屏无信息量）
    groups = {}
    for it in res['items']:
        if it['inBand']:
            continue
        g = groups.setdefault(it.get('group', it['registry']),
                              {'count': 0, 'density': it['density'], 'registry': it['registry'],
                               'px': it['px'], 'meters': it['meters'], 'example': it['path']})
        g['count'] += 1
    res['groups'] = list(groups.values())
    judged = {it['path'] for it in res['items']}
    for p in glob.glob(os.path.join(IMAGES_DIR, '**', '*.png'), recursive=True):
        if os.path.relpath(p, IMAGES_DIR) not in judged:
            res['na'] += 1
    res['status'] = 'warn' if res['problems'] else 'pass'
    return res


# ── 判据 A6 / A7：GLB 侧 ──────────────────────────────────────────────────
def glb_files():
    """扫 ClientWeb/src/assets/models/**/*.glb，跳过 dist/ 与备份。"""
    out = []
    for p in sorted(glob.glob(os.path.join(MODELS_DIR, '**', '*.glb'), recursive=True)):
        rel = os.path.relpath(p, MODELS_DIR).replace(os.sep, '/')
        if rel.startswith('dist/') or '/dist/' in rel or rel.endswith('.bak.glb'):
            continue
        out.append(p)
    return out


def judge_a6_a7():
    a6 = {'status': 'pass', 'items': [], 'problems': []}
    a7 = {'status': 'pass', 'items': [], 'problems': []}
    if VERIFY is None:
        sys.stderr.write('⚠ [A6/A7] SKIP —— 同目录缺 verify_glb_aabb.py'
                         '（GLB 量测复用它，不另写一份）\n')
        a6['status'] = a7['status'] = 'skip'
        a6['reason'] = a7['reason'] = 'verify_glb_aabb.py 缺失'
        return a6, a7, {}
    models = {}
    for p in glb_files():
        rel = os.path.relpath(p, MODELS_DIR).replace(os.sep, '/')
        try:
            js, _blob = VERIFY.read_glb(p)
        except Exception as exc:  # noqa: BLE001
            a7['problems'].append('%s 解析失败：%r' % (rel, exc))
            continue
        # A6：Image Texture 节点数 + UV 覆盖
        img_idx = set()
        for m in js.get('materials', []):
            pbr = m.get('pbrMetallicRoughness', {}) or {}
            for slot in (pbr, m):
                for kv, val in slot.items():
                    if kv.endswith('Texture') and isinstance(val, dict) and 'index' in val:
                        img_idx.add(val['index'])
        prims = [pr for me in js.get('meshes', []) for pr in me.get('primitives', [])]
        with_uv = sum(1 for pr in prims if 'TEXCOORD_0' in (pr.get('attributes') or {}))
        cov = (with_uv / len(prims)) if prims else 1.0
        it = {'model': rel, 'imageTextures': len(img_idx), 'primitives': len(prims),
              'primitivesWithUV': with_uv, 'uvCoverage': round(cov, 4)}
        a6['items'].append(it)
        models[rel] = dict(it)
        if img_idx and cov < UV_COVERAGE_MIN:
            a6['problems'].append(
                '%s 引用了 %d 个 Image Texture 节点但 UV 覆盖仅 %.1f%%（阈值 %.0f%%）'
                % (rel, len(img_idx), cov * 100, UV_COVERAGE_MIN * 100))
        # A7：直接复用既有几何判据
        try:
            problems, info = VERIFY.check(p, rel)
        except Exception as exc:  # noqa: BLE001
            a7['problems'].append('%s 几何判据异常：%r' % (rel, exc))
            continue
        a7['items'].append({'model': rel, 'problems': list(problems)})
        models[rel]['aabbProblems'] = list(problems)
        models[rel]['worldSize'] = ([round(v, 4) for v in info['size']]
                                    if info and info.get('size') else None)
        for pr in problems:
            a7['problems'].append('%s: %s' % (rel, pr))
    a6['status'] = 'fail' if a6['problems'] else 'pass'
    a7['status'] = 'fail' if a7['problems'] else 'pass'
    return a6, a7, models


# ── 输出 ───────────────────────────────────────────────────────────────────
MARK = {'pass': 'PASS', 'fail': 'FAIL', 'warn': 'WARN', 'skip': 'SKIP'}

TITLES = {
    'A1': '登记表宽高比不变式 tileU/tileV == texW/texH',
    'A2': '磁盘实际像素 == 登记表声明值',
    'A3': '被 repeat 平铺的贴图四边无缝（< 2/255）',
    'A4': '派生 PBR 图（_n/_r）与其颜色图宽高比一致',
    'A5': '物理纹素密度落在 60–120 px/m 标称带（WARN 级）',
    'A6': 'GLB 有 Image Texture 节点时须 UV 覆盖完整（≥ 90%）',
    'A7': 'GLB 节点 identity / 贴地 / 尺度（复用 verify_glb_aabb）',
}


def print_block(code, res, verbose):
    st = res.get('status', 'skip')
    print('\n### %-4s %-46s [%s]' % (code, TITLES[code], MARK.get(st, st.upper())))
    if res.get('reason'):
        print('    · %s' % res['reason'])
    if code == 'A1' and verbose:
        for it in res['items']:
            print('    · %-14s %gx%g px / %gm x %gm  声明比 %.4f 物理比 %.4f 误差 %.3f%%'
                  % (it['name'], it['texW'], it['texH'], it['tileMetersU'], it['tileMetersV'],
                     it['declaredAspect'], it['physicalAspect'], it['relErr'] * 100))
    if code == 'A2':
        for it in res['items']:
            print('    · %-14s %-16s %3d 张，%d 张不符声明'
                  % (it['name'], it['dir'], it['files'], it['mismatch']))
    if code == 'A3':
        if res['items']:
            n_fail = sum(1 for i in res['items'] if not i['ok'])
            print('    · 主判集 facade_tiles（颜色 + _n/_r）：%d 张，超阈 %d 张%s'
                  % (len(res['items']), n_fail, '，全部 ≤ %.1f/255 ✓' % SEAM_MAX_255 if not n_fail else ''))
        if res.get('advisory'):
            bad = [a for a in res['advisory'] if not a['ok']]
            print('    · 参考判集 roofs/ground（只报不判）：%d 张，%d 张超阈'
                  % (len(res['advisory']), len(bad)))
        if verbose:
            for it in res['items'] + res.get('advisory', []):
                print('      · %-40s 左右 %6.2f 上下 %6.2f（相邻基线 %5.2f）%s'
                      % (it['path'], it['leftDiff'], it['topDiff'], it['adjacentBaseline'],
                         'OK' if it['ok'] else '✗'))
    if code == 'A4':
        print('    · 派生图 %d 张，孤儿（无对应颜色图）%d 张'
              % (len(res['items']), len(res['orphans'])))
        if verbose:
            for it in res['items']:
                print('      · %-46s %dx%d vs %s %dx%d 误差 %.3f%%'
                      % (it['path'], it['px'][0], it['px'][1], it['color'],
                         it['colorPx'][0], it['colorPx'][1], it['relErr'] * 100))
            for o in res['orphans']:
                print('      · 孤儿派生图：%s' % o)
    if code == 'A5':
        print('    · 标称带 [%.0f, %.0f] px/m；参与判定 %d 张，无物理尺寸语义（记 N/A）%d 张'
              % (res['band'][0], res['band'][1], len(res['items']), res['na']))
        for g in res.get('groups', []):
            print('      · 聚合：%s %s / %gm x %gm → %.1f px/m，共 %d 张（例：%s）'
                  % (g['registry'], '%dx%d' % (g['px'][0], g['px'][1]),
                     g['meters'][0], g['meters'][1], g['density'], g['count'], g['example']))
        if verbose:
            for it in res['items']:
                print('      · %-40s %dx%d / %gm x %gm  密度 U %6.1f V %6.1f %s'
                      % (it['path'], it['px'][0], it['px'][1], it['meters'][0], it['meters'][1],
                         it['densityU'], it['densityV'], 'OK' if it['inBand'] else 'WARN'))
    if code == 'A6':
        n_img = sum(1 for i in res['items'] if i['imageTextures'])
        print('    · 扫描 %d 个 GLB，其中带 Image Texture 节点的 %d 个'
              '（方案 §1.6 D4：现为纯色材质，预期 0）' % (len(res['items']), n_img))
        if verbose:
            for it in res['items']:
                print('      · %-34s ImageTexture %d  UV 覆盖 %5.1f%%（%d/%d primitives）'
                      % (it['model'], it['imageTextures'], it['uvCoverage'] * 100,
                         it['primitivesWithUV'], it['primitives']))
    if code == 'A7':
        print('    · 复用 verify_glb_aabb 判据，%d 个 GLB，%d 项不符'
              % (len(res['items']), len(res['problems'])))
        if verbose:
            for it in res['items']:
                if it['problems']:
                    print('      · %-34s ✗ %s' % (it['model'], '; '.join(it['problems'])))
    # A5 是 WARN 级且同一原因会命中上百张文件，聚合行已在上方打印，这里不重复刷屏
    shown = [] if code == 'A5' else res['problems'][:40]
    for p in shown:
        print('    ✗ %s' % p)
    if len(res['problems']) > len(shown) and code != 'A5':
        print('    ✗ …另有 %d 项（--verbose 不展开问题清单，用 --emit-json 取全量）'
              % (len(res['problems']) - len(shown)))


def check_table(path, payload):
    """对齐批次 30 §3.7 的 `--check-table` 语义：实测表 vs 外部表逐项比对。

    表格式（宽松）：{"<相对路径 | basename>": {"pxW": .., "pxH": ..}} 或直接 [w, h]。
    实测查无 / 像素不符 ⇒ 非零退出。
    """
    with open(path, 'r', encoding='utf-8') as fh:
        table = json.load(fh)
    index = {}
    for it in payload.get('texels', []):
        index[it['path']] = it
        index[os.path.basename(it['path'])] = it
    problems = []
    for k, v in table.items():
        if k.startswith('__'):
            continue
        hit = index.get(k) or index.get(os.path.basename(k))
        if hit is None:
            problems.append('表项 %s 查无实测' % k)
            continue
        want = tuple(v) if isinstance(v, (list, tuple)) else (v.get('pxW'), v.get('pxH'))
        if tuple(hit['px']) != want:
            problems.append('%s 实测 %dx%d != 表值 %sx%s'
                            % (k, hit['px'][0], hit['px'][1], want[0], want[1]))
    for p in problems:
        print('    ✗ %s' % p)
    return problems


def parse_cli(argv):
    args = {'emit': None, 'table': None, 'only': None, 'verbose': False}
    i = 0
    while i < len(argv):
        a = argv[i]
        if a == '--emit-json':
            nxt = argv[i + 1] if i + 1 < len(argv) else None
            if nxt is not None and not nxt.startswith('--'):
                args['emit'], i = nxt, i + 2
            else:
                args['emit'], i = '-', i + 1
        elif a.startswith('--emit-json='):
            args['emit'] = a.split('=', 1)[1] or '-'
            i += 1
        elif a == '--check-table':
            if i + 1 >= len(argv):
                raise SystemExit('--check-table 需要一个 JSON 表路径参数')
            args['table'] = argv[i + 1]
            i += 2
        elif a == '--only':
            # 允许 `--only A1 A3`（空格分隔多个）与 `--only A1,A3` 两种写法
            vals = []
            while i + 1 < len(argv) and not argv[i + 1].startswith('-'):
                vals.append(argv[i + 1])
                i += 1
            if not vals:
                raise SystemExit('--only 需要至少一个判据编号')
            args['only'] = ','.join(vals)
            i += 1
        elif a.startswith('--check-table='):
            args['table'] = a.split('=', 1)[1]
            i += 1
        elif a.startswith('--only='):
            args['only'] = a.split('=', 1)[1]
            i += 1
        elif a in ('-v', '--verbose'):
            args['verbose'] = True
            i += 1
        elif a in ('-h', '--help'):
            print(__doc__)
            raise SystemExit(0)
        else:
            raise SystemExit('未知参数：%s（-h 看用法）' % a)
    return args


def main(argv):
    args = parse_cli(argv)
    only = {x.strip().upper() for x in args['only'].split(',')} if args['only'] else None

    def want(code):
        return only is None or code in only

    print('=' * 100)
    print('audit_texture_fit — 虚拟城市贴图↔几何保真审计（批次 37 P2）')
    print('贴图根：%s' % os.path.relpath(IMAGES_DIR, REPO))
    print('登记表：%s' % os.path.relpath(REGISTRY_TS, REPO))
    print('模型根：%s' % os.path.relpath(MODELS_DIR, REPO))
    print('=' * 100)

    table, reg_err = parse_registry(REGISTRY_TS)
    judges, models, texels = {}, {}, []

    if want('A1') or want('A2'):
        a1, a2 = judge_a1_a2(table, reg_err)
        if want('A1'):
            judges['A1'] = a1
        if want('A2'):
            judges['A2'] = a2
    if want('A3'):
        judges['A3'] = judge_a3()
    if want('A4'):
        judges['A4'] = judge_a4()
    if want('A5'):
        judges['A5'] = judge_a5(table, reg_err)
        texels = judges['A5']['items']
    if want('A6') or want('A7'):
        a6, a7, models = judge_a6_a7()
        if want('A6'):
            judges['A6'] = a6
        if want('A7'):
            judges['A7'] = a7

    for code in ('A1', 'A2', 'A3', 'A4', 'A5', 'A6', 'A7'):
        if code in judges:
            print_block(code, judges[code], args['verbose'])

    failed = [c for c, r in judges.items() if r.get('status') == 'fail']
    warned = [c for c, r in judges.items() if r.get('status') == 'warn']
    skipped = [c for c, r in judges.items() if r.get('status') == 'skip']

    payload = {'judges': judges, 'texels': texels, 'models': models,
               'summary': {'fail': failed, 'warn': warned, 'skip': skipped}}
    if args['emit'] is not None:
        blob = json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True)
        if args['emit'] == '-':
            print('\n' + '=' * 100)
            print('--emit-json（一次性量测产物，出「修复资产清单」用，勿入库）')
            print('=' * 100)
            print(blob)
        else:
            with open(args['emit'], 'w', encoding='utf-8') as fh:
                fh.write(blob + '\n')
            print('\n--emit-json 已写入 %s' % os.path.abspath(args['emit']))

    if args['table']:
        check_table(args['table'], payload)
        failed = failed + ['check-table']

    print('\n' + '=' * 100)
    print('判据汇总：%s' % '  '.join(
        '%s=%s' % (c, MARK.get(judges[c].get('status'), '?')) for c in judges))
    if failed:
        print('结论：%d 项 FAIL（%s）✗' % (len(failed), ','.join(failed)))
        return 1
    note = ('（WARN：%s）' % ','.join(warned)) if warned else ''
    note += ('（SKIP：%s —— 依赖未就位，不阻塞）' % ','.join(skipped)) if skipped else ''
    print('结论：全部通过 ✓%s' % note)
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
