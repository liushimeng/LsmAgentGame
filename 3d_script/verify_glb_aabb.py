#!/usr/bin/env python3
"""
verify_glb_aabb — 虚拟城市 GLB 轴向 / 贴地 / 尺度 / 节点 identity 自检（纯 stdlib）。

背景（2026-09-27 批次 19 GLB 轴向+尺度回溯修正）：
  批次 19 的 11 个 GLB 由旧「作者自定 Y-up」脚本导出 —— bpy.ops.export_scene.gltf 的
  Yup 转换 (x,y,z)_b → (x,z,-y)_g 把作者的"上"映射到 glTF -Z ⇒ three.js 里整个模型侧躺；
  同时尺度过小（玩具车 1 m）或过大（楼体 1.5~2.5×）。本脚本是修正后的可复现验收工具。

判据（逐条打印实测 / 目标 / 偏差，任一 FAIL ⇒ 退出码 1）：
  1. 直立   —— 主包围轴 = Y（唯一例外：ocean/cargo_ship 与 ocean/sailboat 长轴 = X 属正确
               摆放；nature/snow_mountain 为地形基准尺度，只报不判）。
  2. 贴地   —— minY = 0（cargo_ship / sailboat 为吃水线，允许 < 0）。
  3. 尺寸   —— 对 TARGETS 中的 18 个目标件，三条边逐一比对（容差 ±5%）。
               余下 4 件（nature/snow_mountain + ocean/cargo_ship|sailboat|lighthouse）
               为地形 / 船体 / 海洋 edge 层基准，尺寸**只报不判**（见 REPORT_ONLY_SIZE）。
  4. 节点   —— 无节点携带非 identity 的 scale / rotation（蒙皮 mesh 顶点受 bind matrix 支配、
               骨骼节点天然带旋转，故 joint 节点与带 skin 的 mesh 节点豁免）；平移只允许出现在
               上述两类节点上。消费端 <Model> 零旋转零 scale 直挂，靠的就是这条。

用法：
  python3 3d_script/verify_glb_aabb.py                     # 全量 22 件，非零退出即失败
  python3 3d_script/verify_glb_aabb.py path/to.glb         # 只看指定文件
  python3 3d_script/verify_glb_aabb.py --emit-json         # 追加打印实测 AABB JSON（stdout）
  python3 3d_script/verify_glb_aabb.py --emit-json /tmp/glb_dims.json
  python3 3d_script/verify_glb_aabb.py --check-table 表.json   # 实测 vs 表值逐边比对（±5%）

CI 门禁（批次 30 起；主 Agent 可直接接 .github/workflows/ci.yml）：
  python3 3d_script/verify_glb_aabb.py                     # 全量 22 件，非零退出即失败
  python3 3d_script/verify_glb_aabb.py --emit-json /tmp/glb_dims.json
  find ClientWeb/src/assets/models -name '*.glb' -size +500k | grep . && exit 1   # 500KB 硬线

--emit-json 输出（世界单位；键 = `<类别>/<名>`，无 .glb 后缀）：
  { "<类别>/<名>": {
      "world": [x,y,z],            # 判据口径（per_part 文件 = 单件最大尺寸）
      "geo":   [x,y,z],            # 几何 accessor 口径（不施加节点变换）
      "minY":  n,
      "nodeScaleIssues": [...],    # 节点 scale ≠ 1 的告警（应恒为 []）
      "per_part": { "<节点名>": {"world":[x,y,z], "geo":[x,y,z], "minY":n} } } }
  ⚠ 该 json 是**一次性量测产物，不入库**（.gitignore 已含 3d_script/glb_measured_dims.json）；
    需要留档时写进 lag_docs 文档，不要提交 json 本身。

--check-table <json> 表格式（供前端 cityScale.ts::REAL_DIMS_M 等表做交叉校验，批次 29 方案 §3.7）：
  {
    "__unit__": "world",                      # 或 "m"（米制，比对前自动 ÷10）；默认 world
    "nature/oak_tree": {"world": [0.450, 1.010, 0.470]},   # 或直接 [x,y,z] / {"x":..,"y":..,"z":..}
    "nature/cactus":   [0.170, 0.290, 0.070]
  }
  键 = `<类别>/<名>`（推荐）、唯一 basename（如 `cactus`）、可带/不带 .glb 后缀；
  某轴写 null 表示该轴不参与比对。任一轴偏差 > ±5% 或表项查无实测/有歧义 ⇒ 非零退出。
  ⚠ 前端 REAL_DIMS_M 的 camelCase 键（oakTree/trashCan…）请先由前端侧改名为资产名再喂
  （本工具不建第二份名字表，防两表漂移；对照表见 lag_docs 批次 30 的 03 文档）。
"""
import glob
import json
import math
import os
import struct
import sys

# 模型根目录：默认仓库内的 ClientWeb/src/assets/models；可用 GLB_MODELS_DIR 覆盖
# （回归自检用：把 build_*.py 重导到临时目录后仍按 `<类别>/<名>.glb` 的 rel 命中目标表）。
MODELS_DIR = os.environ.get('GLB_MODELS_DIR') or os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    'ClientWeb', 'src', 'assets', 'models',
)

# ── 目标表（世界单位；1 单位 = 10 m）────────────────────────────────────────
# 来源：前端程序化 fallback 的真实尺寸（civic/*Fallback + props/Vehicle.tsx VEHICLE_DIMS）。
# x/y/z 为 None 表示该轴不参与判据（仅报实测）。
TARGETS = {
    # 车辆：长轴 = X 属正确摆放（真实车长 4.6~12 m 必然大于车高），故 axis='x_flat'
    'vehicles/sedan.glb':     dict(x=0.460, y=0.145, z=0.182, axis='x_flat', pivot='轮底贴地 minY=0，X/Z 居中'),
    'vehicles/taxi.glb':      dict(x=0.470, y=0.150, z=0.185, axis='x_flat', pivot='轮底贴地 minY=0，X/Z 居中'),
    'vehicles/bus.glb':       dict(x=1.200, y=0.320, z=0.255, axis='x_flat', pivot='轮底贴地 minY=0，X/Z 居中'),
    'vehicles/truck.glb':     dict(x=0.850, y=0.340, z=0.250, axis='x_flat', pivot='轮底贴地 minY=0，X/Z 居中'),
    # 批次 41 涂装变体：尺寸同原型（taxi 顶灯计入 Y，+4.7% 在容差内）
    'vehicles/sedan_silver.glb': dict(x=0.460, y=0.145, z=0.182, axis='x_flat',
                                      pivot='≡ sedan（银灰涂装变体）'),
    'vehicles/truck_white.glb':  dict(x=0.850, y=0.340, z=0.250, axis='x_flat',
                                      pivot='≡ truck（白涂装变体）'),
    'characters/pedestrian_walk.glb': dict(x=0.055, y=0.167, z=0.035, pivot='身高 1.67 m，脚底 minY=0，X/Z 居中'),
    # ── 批次 34 八人物原型（build_character.py）─────────────────────────────
    # 表值 = 各原型设计身高/肩宽（设计 34 §5.1 + 任务书规格表），tol=0.08 对应
    # 「8 个原型允许 ±8% 身材差」（设计 34 §5.2）；Z 仍按 0.035 收（行走进深包络）。
    # worker 肩宽 0.62 m 属显式规格（超出 0.55 的 ±8% 包络），按原型自身表值判。
    'characters/char_casual.glb':   dict(x=0.055, y=0.167, z=0.035, tol=0.08,
                                         pivot='兜底休闲 ≡ pedestrian_walk（1.67 m / 肩 0.55）'),
    'characters/char_business.glb': dict(x=0.055, y=0.172, z=0.035, tol=0.08,
                                         pivot='标准商务 1.72 m，西装上衣略长'),
    'characters/char_worker.glb':   dict(x=0.062, y=0.172, z=0.035, tol=0.08,
                                         pivot='壮实工装：肩宽 0.62 m（显式规格）'),
    'characters/char_elder.glb':    dict(x=0.052, y=0.158, z=0.035, tol=0.08,
                                         pivot='银发退休 1.58 m，微驼背（头前伸 + 上背垫）'),
    'characters/char_student.glb':  dict(x=0.052, y=0.175, z=0.035, tol=0.08,
                                         pivot='高瘦学生 1.75 m，肩收窄'),
    'characters/char_service.glb':  dict(x=0.055, y=0.168, z=0.035, tol=0.08,
                                         pivot='服务制服 1.68 m，帽顶 = 身高顶点'),
    'characters/char_formal.glb':   dict(x=0.056, y=0.178, z=0.035, tol=0.08,
                                         pivot='挺拔正装 1.78 m，上衣更长 + 领带'),
    'characters/char_parent.glb':   dict(x=0.055, y=0.165, z=0.035, tol=0.08,
                                         pivot='柔和持家 1.65 m，躯干略厚'),
    # ── 批次 36 性别 × 年龄 15 变体（build_character.py --variant）──────────
    # 表值 = 设计 36 §4.2 规格表（身高 m /10 = 世界单位高 Y；肩宽 m /10 = X 包络）。
    # tol=0.10：性别肩宽差是显式规格，按逐件自身表值判（同 worker 0.62 先例），
    # 不硬套 pedestrian 0.55。Z 逐件取「行走进深 + 发型/驼背包络」的变体表值
    # （长发/马尾后垂可到 0.038~0.039；均未无节制膨胀，见 VARIANTS.z）。
    # 实测（2026-09-29）：15/15 最大偏差 7.1% < 10%，身高全表 ±0.6%。
    'characters/char_m_youth.glb':  dict(x=0.052, y=0.172, z=0.038, tol=0.10,
                                         pivot='男青年 1.72 m，偏瘦肩 0.52；蓬乱加高短发（R3）+ 连帽衫领（Z 0.038）'),
    'characters/char_m_young.glb':  dict(x=0.055, y=0.176, z=0.035, tol=0.10,
                                         pivot='男青壮 1.76 m，匀称肩 0.55；短发利落 + 衬衫领 + 腰带'),
    'characters/char_m_middle.glb': dict(x=0.056, y=0.175, z=0.037, tol=0.10,
                                         pivot='男中年 1.75 m，肩 0.56；微发福躯干厚 ×1.06（Z 放到 0.037）+ 腰带'),
    'characters/char_m_senior.glb': dict(x=0.054, y=0.171, z=0.038, tol=0.10,
                                         pivot='男中老年 1.71 m，肩 0.54；驼背 6° + 上背圆化 + 夹克领（终审回调 7°→6° 与 elder 拉开，Z 0.038）'),
    'characters/char_m_elder.glb':  dict(x=0.050, y=0.165, z=0.040, tol=0.10,
                                         pivot='男老年 1.65 m，瘦削肩 0.50；驼背 12° + 秃顶侧发 + 拐杖（贴身 X 包络内；终审加深后 Z 0.040）'),
    'characters/char_f_youth.glb':  dict(x=0.046, y=0.161, z=0.040, tol=0.10,
                                         pivot='女青年 1.61 m，纤细肩 0.46；粗马尾柱后垂到背中段（R1 加强，Z 0.040）+ 宽松上衣'),
    'characters/char_f_young.glb':  dict(x=0.046, y=0.165, z=0.037, tol=0.10,
                                         pivot='女青壮 1.65 m，纤细肩 0.46；长发后背片（Z 0.037）+ 收腰上衣'),
    'characters/char_f_middle.glb': dict(x=0.048, y=0.164, z=0.038, tol=0.10,
                                         pivot='女中年 1.64 m，丰满肩 0.48；中长发 + 中长上衣（躯干厚 ×1.06）'),
    'characters/char_f_senior.glb': dict(x=0.047, y=0.160, z=0.038, tol=0.10,
                                         pivot='女中老年 1.60 m，肩 0.47；发髻 + 驼背 6° + 上背圆化（终审回调 7°→6°）'),
    'characters/char_f_elder.glb':  dict(x=0.045, y=0.154, z=0.040, tol=0.10,
                                         pivot='女老年 1.54 m，瘦削肩 0.45；发髻 + 驼背 12° + 拐杖（规格表最低身高；终审加深后 Z 0.040）'),
    'characters/char_u_youth.glb':  dict(x=0.049, y=0.1665, z=0.036, tol=0.10,
                                         pivot='中性青年（男女均值 1.665 m / 肩 0.49）；齐耳中性短发 + 挺拔'),
    'characters/char_u_young.glb':  dict(x=0.0505, y=0.1705, z=0.036, tol=0.10,
                                         pivot='中性青壮（均值 1.705 m / 肩 0.505）；齐耳短发 + 衬衫领'),
    'characters/char_u_middle.glb': dict(x=0.052, y=0.1695, z=0.037, tol=0.10,
                                         pivot='中性中年（均值 1.695 m / 肩 0.52）；短发 + 躯干厚 ×1.06 + 腰带'),
    'characters/char_u_senior.glb': dict(x=0.0505, y=0.1655, z=0.038, tol=0.10,
                                         pivot='中性中老年（均值 1.655 m / 肩 0.505）；短发 + 驼背 6° + 上背圆化 + 夹克（终审回调 7°→6°）'),
    'characters/char_u_elder.glb':  dict(x=0.0475, y=0.1595, z=0.040, tol=0.10,
                                         pivot='中性老年（均值 1.595 m / 肩 0.475）；短发 + 驼背 12° + 拐杖（终审加深，Z 0.040）'),
    'civic/city_hall.glb':    dict(x=1.440, y=2.000, z=0.840, pivot='地面中心 minY=0'),
    'civic/comm_tower.glb':   dict(x=1.000, y=2.050, z=1.000, pivot='地面中心 minY=0'),
    'civic/water_tower.glb':  dict(x=0.360, y=1.330, z=0.360, pivot='地面中心 minY=0'),
    'civic/police_station.glb': dict(x=1.210, y=0.660, z=0.700, pivot='地面中心 minY=0'),
    'civic/fire_station.glb': dict(x=1.810, y=0.850, z=0.800, pivot='地面中心 minY=0'),
    # road_props.glb 原在此有判据（一文件 3 盏路灯沿 X 排开 ⇒ 整体长轴 = X 属正确；
    # 直立性逐件判：至少 3 个「主包围轴 = Y 且落地」的立体件，横臂/灯头不计）。
    # 批次 31：文件已删除（零消费点清理，设计 31 §4.2），判据随之移除。
    # trash_can.glb 一个文件含 2 只独立桶（x=0 / +0.12），目标值是**单桶**尺寸，
    # 故用 per_part 模式：对每个节点各求 AABB，取各轴最大值（而非整体包围盒）。
    'road/trash_can.glb':     dict(x=0.050, y=0.100, z=0.050, per_part=True, axis='per_part_y',
                                   min_parts=2, pivot='单桶 桶底 minY=0（两桶各自 minY=0）'),
    # ── 批次 38 运河/桥三件（build_canal_bank / build_canal_reed / build_bridge_rail）──
    # 表值 = 资产实测（1 世界单位 = 10 m）。尺寸判据 ±5%；直立豁免见
    # DOMINANT_AXIS_EXEMPT（canal_bank / bridge_rail 为横向线性构件，主轴 X）。
    # canal_reed 为丛状件，Y 主导（2.41 m 丛高）⇒ 走默认 'target' 直立判据。
    'road/canal_bank.glb':    dict(x=0.800, y=0.160, z=0.298,
                                   pivot='段底 minY=0，X/Z 居中；沿河 8 m 段可平铺'),
    'road/canal_reed.glb':    dict(x=0.195, y=0.241, z=0.191,
                                   pivot='丛底 minY=0，X/Z 居中；沿岸实例化点缀'),
    'road/bridge_rail.glb':   dict(x=0.200, y=0.115, z=0.012,
                                   pivot='段底 minY=0；沿桥长 2 m 段可平铺'),
    # ── 植被（nature）—— 批次 30 补齐（原 12 件无尺寸判据，只过直立/贴地/节点 identity）──
    # 表值来源：cityScale.ts::REAL_DIMS_M（米制）÷ 10 ≡ 批次 29 方案 §3.4-E。
    # oak_tree 与 _spring / _autumn 三变体同尺寸；_winter 单列（落叶冠幅本就细，表值 = 资产真值）。
    'nature/oak_tree.glb':        dict(x=0.450, y=1.010, z=0.470,
                                       pivot='地面中心 minY=0（与 _spring/_autumn 同尺寸）'),
    'nature/oak_tree_spring.glb': dict(x=0.450, y=1.010, z=0.470, pivot='地面中心 minY=0（三季变体同尺寸）'),
    'nature/oak_tree_autumn.glb': dict(x=0.450, y=1.010, z=0.470, pivot='地面中心 minY=0（三季变体同尺寸）'),
    'nature/oak_tree_winter.glb': dict(x=0.412, y=0.955, z=0.382,
                                       pivot='冬季落叶冠幅细；表值 = 资产真值（批次 29 §3.4-E 单列）'),
    # pine_tree 表值 = **资产现值**（5.4 m），非「真实云杉 8~12 m」拟值（批次 29 遗留 L3 已裁决）
    'nature/pine_tree.glb':       dict(x=0.340, y=0.540, z=0.340, pivot='地面中心 minY=0；表值 = 资产现值'),
    'nature/cactus.glb':          dict(x=0.170, y=0.290, z=0.070, pivot='地面中心 minY=0'),
}

# 直立判据的例外：长轴落在水平轴属正确摆放。
# 批次 38 追加两件**横向线性构件**：驳岸挡墙段（8 m 长 / 1.6 m 高 / 3 m 深）与
# 桥栏杆段（2 m 长 / 1.15 m 高 / 0.12 m 厚）—— 长度沿 X 平铺，与船体同类，
# 非「应当直立」类物件（树/人/楼/桶才走主轴=Y 判据）；直立性由 Y 尺寸判据保证。
DOMINANT_AXIS_EXEMPT = {
    'ocean/cargo_ship.glb': 'X', 'ocean/sailboat.glb': 'X',
    'road/canal_bank.glb': 'X', 'road/bridge_rail.glb': 'X',
}
# 地形基准：只报实测，不参与直立 / 贴地判据。
TERRAIN_BASELINE = {'nature/snow_mountain.glb'}
# 吃水线：minY < 0 属正确。
WATERLINE = {'ocean/cargo_ship.glb', 'ocean/sailboat.glb'}

# ── 尺寸只报不判（批次 30 口径）────────────────────────────────────────────
# 地形 / 船体 / 海洋 edge 层基准：有独立的浮沉、吃水、display-scale 约定，
# 不是「物件」尺寸契约的适用对象（CLAUDE.md §27.3 判据 7「GLB ≡ fallback 同尺寸」
# 只覆盖 REAL_DIMS_M 里的物件）。这 4 件仍跑几何≡世界、节点 identity、体积
# 硬线；直立/贴地按**现状**保留（snow_mountain 走 TERRAIN_BASELINE 豁免、
# cargo_ship/sailboat 走 x_flat + 吃水线、lighthouse 直立贴地照常判），
# 尺寸不设目标值，只把实测值打出来留档（详见 lag_docs 批次 30 的 03 文档）。
REPORT_ONLY_SIZE = {
    'nature/snow_mountain.glb': '地形基准（批次 26）：前端 scale 6~10 出 150~250 m 变化，尺寸刻意非真实',
    'ocean/cargo_ship.glb': '船体基准：吃水线 + 长轴 X；未入 REAL_DIMS_M（消费端 edge 层 display scale）',
    'ocean/sailboat.glb': '船体基准：同 cargo_ship；未入 REAL_DIMS_M（批次 29 §3.4-F 冻结现状）',
    'ocean/lighthouse.glb': '海洋 edge 层 display scale，未入 REAL_DIMS_M（批次 29 §3.4-F 冻结现状）',
}

SIZE_TOL = 0.05        # ±5%
EPS = 1e-4

_COMPONENT = {5120: ('b', 1), 5121: ('B', 1), 5122: ('h', 2),
              5123: ('H', 2), 5125: ('I', 4), 5126: ('f', 4)}
_NCOMP = {'SCALAR': 1, 'VEC2': 2, 'VEC3': 3, 'VEC4': 4, 'MAT4': 16}


# ── GLB 解析 ──────────────────────────────────────────────────────────────
def read_glb(path):
    with open(path, 'rb') as fh:
        data = fh.read()
    magic, _ver, length = struct.unpack('<III', data[:12])
    if magic != 0x46546C67:
        raise ValueError('not a GLB: ' + path)
    off, js, blobs = 12, None, []
    while off < length:
        clen, ctype = struct.unpack('<II', data[off:off + 8])
        chunk = data[off + 8:off + 8 + clen]
        if ctype == 0x4E4F534A:
            js = json.loads(chunk.decode('utf-8'))
        elif ctype == 0x004E4942:
            blobs.append(chunk)
        off += 8 + clen
    return js, (blobs[0] if blobs else b'')


def read_accessor(js, blob, idx):
    """返回 [(x,y,z), ...]（仅 POSITION，VEC3 float）。"""
    acc = js['accessors'][idx]
    bv = js['bufferViews'][acc['bufferView']]
    fmt, size = _COMPONENT[acc['componentType']]
    n = _NCOMP[acc['type']]
    start = bv.get('byteOffset', 0) + acc.get('byteOffset', 0)
    stride = bv.get('byteStride') or (size * n)
    out = []
    for i in range(acc['count']):
        o = start + i * stride
        vals = struct.unpack_from('<' + fmt * n, blob, o)
        out.append(vals[:3] if n >= 3 else (vals[0], 0.0, 0.0))
    return out


def _mat_mul(a, b):
    return [[sum(a[i][k] * b[k][j] for k in range(4)) for j in range(4)] for i in range(4)]


def _mat_apply(m, v):
    x, y, z = v
    return (m[0][0] * x + m[0][1] * y + m[0][2] * z + m[0][3],
            m[1][0] * x + m[1][1] * y + m[1][2] * z + m[1][3],
            m[2][0] * x + m[2][1] * y + m[2][2] * z + m[2][3])


IDENT = [[1.0 if i == j else 0.0 for j in range(4)] for i in range(4)]


def node_local_matrix(node):
    """nodes[i] 的局部矩阵（matrix 优先，否则 TRS 合成）。"""
    if 'matrix' in node:
        m = node['matrix']          # glTF 列主序
        return [[m[0], m[4], m[8], m[12]],
                [m[1], m[5], m[9], m[13]],
                [m[2], m[6], m[10], m[14]],
                [m[3], m[7], m[11], m[15]]]
    tx, ty, tz = node.get('translation', (0.0, 0.0, 0.0))
    qx, qy, qz, qw = node.get('rotation', (0.0, 0.0, 0.0, 1.0))
    sx, sy, sz = node.get('scale', (1.0, 1.0, 1.0))
    r = [[1 - 2 * (qy * qy + qz * qz), 2 * (qx * qy - qz * qw), 2 * (qx * qz + qy * qw)],
         [2 * (qx * qy + qz * qw), 1 - 2 * (qx * qx + qz * qz), 2 * (qy * qz - qx * qw)],
         [2 * (qx * qz - qy * qw), 2 * (qy * qz + qx * qw), 1 - 2 * (qx * qx + qy * qy)]]
    m = [[r[i][j] * (sx, sy, sz)[j] for j in range(3)] + [0.0] for i in range(3)]
    m.append([0.0, 0.0, 0.0, 1.0])
    m[0][3], m[1][3], m[2][3] = tx, ty, tz
    return m


def is_identity(m):
    for i in range(4):
        for j in range(4):
            if abs(m[i][j] - (1.0 if i == j else 0.0)) > EPS:
                return False
    return True


def prim_local_aabb(js, blob, prim):
    """单个 primitive 的局部 AABB（优先 accessor min/max，缺失则解码顶点）。"""
    idx = prim.get('attributes', {}).get('POSITION')
    if idx is None:
        return None
    acc = js['accessors'][idx]
    if 'min' in acc and 'max' in acc:
        return tuple(acc['min']), tuple(acc['max'])
    verts = read_accessor(js, blob, idx)
    if not verts:
        return None
    lo = [min(v[i] for v in verts) for i in range(3)]
    hi = [max(v[i] for v in verts) for i in range(3)]
    return tuple(lo), tuple(hi)


def world_aabb(js, blob):
    """遍历 scene 节点树，同时求两个口径的包围盒：

      - **世界口径**：accessor AABB 经节点 TRS 链变换后并起来（= 浏览器 Box3 所见）；
      - **几何口径**：直接并 accessor AABB，**不施加任何节点变换**。

    两者相等 ⇔ 节点链为 identity ⇔ 「几何顶点本身就是最终世界尺寸」。
    （HEAD 的 trash_can.glb 就是两者不等的反例：几何 [2,1,2] × 节点 scale 才是 0.078。）
    """
    scene = js['scenes'][js.get('scene', 0)]
    joints = set()
    for sk in js.get('skins', []):
        joints.update(sk.get('joints', []))
    # 动画驱动的节点 = 骨骼节点，天然带旋转/平移，豁免 identity 判据
    anim_targets = set()
    for a in js.get('animations', []):
        for ch in a.get('channels', []):
            if 'node' in ch.get('target', {}):
                anim_targets.add(ch['target']['node'])
    details, node_boxes, geom_node_boxes, bad_nodes, soft_nodes = [], [], [], [], []
    acc_lo, acc_hi = [math.inf] * 3, [-math.inf] * 3
    geo_lo, geo_hi = [math.inf] * 3, [-math.inf] * 3

    def visit(ni, parent):
        node = js['nodes'][ni]
        m = _mat_mul(parent, node_local_matrix(node))
        # 蒙皮 mesh 的顶点受 inverseBindMatrices 支配，其规范里忽略其节点变换
        skinned = 'skin' in node
        name = node.get('name', 'node%d' % ni)
        if ni not in joints and ni not in anim_targets and not skinned:
            s = node.get('scale', (1.0, 1.0, 1.0))
            if any(abs(v - 1.0) > EPS for v in s):
                # 硬判据：节点 scale ≠ 1 ⇒ 尺寸挂在节点变换上
                # （HEAD 的 trash_can.glb 正是 scale=[0.0375,0.09,0.0375] × 单位几何）
                bad_nodes.append('%s(scale=%s)' % (name, [round(v, 4) for v in s]))
            elif not is_identity(node_local_matrix(node)):
                # 软判据：仅 rotation / translation 偏离（骨骼节点已由动画目标集合豁免）
                soft_nodes.append(name)
        if 'mesh' in node:
            nlo, nhi = [math.inf] * 3, [-math.inf] * 3
            gnlo, gnhi = [math.inf] * 3, [-math.inf] * 3
            for prim in js['meshes'][node['mesh']].get('primitives', []):
                box = prim_local_aabb(js, blob, prim)
                if box is None:
                    continue
                lo, hi = box
                for i in range(3):        # 几何（accessor）口径：不施加任何节点变换
                    geo_lo[i] = min(geo_lo[i], lo[i])
                    geo_hi[i] = max(geo_hi[i], hi[i])
                    gnlo[i] = min(gnlo[i], lo[i])
                    gnhi[i] = max(gnhi[i], hi[i])
                corners = [(lo[0] if not (k & 1) else hi[0],
                            lo[1] if not (k & 2) else hi[1],
                            lo[2] if not (k & 4) else hi[2]) for k in range(8)]
                ws = [_mat_apply(m, c) for c in corners]
                plo = [min(w[i] for w in ws) for i in range(3)]
                phi = [max(w[i] for w in ws) for i in range(3)]
                for i in range(3):
                    acc_lo[i] = min(acc_lo[i], plo[i])
                    acc_hi[i] = max(acc_hi[i], phi[i])
                    nlo[i] = min(nlo[i], plo[i])
                    nhi[i] = max(nhi[i], phi[i])
                details.append((name, plo, phi))
            if nlo[0] < math.inf:
                # 节点级并集（单节点多 primitive 时用于"单件"判据，如垃圾桶一个 mesh 分 3 材质）
                node_boxes.append((name, nlo, nhi))
            if gnlo[0] < math.inf:
                geom_node_boxes.append((name, gnlo, gnhi))
        for c in node.get('children', []):
            visit(c, m)

    for ni in scene['nodes']:
        visit(ni, IDENT)
    return dict(lo=acc_lo, hi=acc_hi, details=details, node_boxes=node_boxes,
                bad_nodes=bad_nodes, soft_nodes=soft_nodes,
                exempt=sorted(joints | anim_targets),
                geom_lo=geo_lo, geom_hi=geo_hi, geom_node_boxes=geom_node_boxes)


# ── 判据 ─────────────────────────────────────────────────────────────────
def check(path, rel):
    js, blob = read_glb(path)
    box = world_aabb(js, blob)
    lo, hi, details, node_boxes = box['lo'], box['hi'], box['details'], box['node_boxes']
    bad, soft_nodes = box['bad_nodes'], box['soft_nodes']
    if not details:
        return ['FAIL  无几何'], None
    size = [hi[i] - lo[i] for i in range(3)]
    g_lo, g_hi = list(box['geom_lo']), list(box['geom_hi'])
    problems, notes = [], []
    target = TARGETS.get(rel)
    exempt_axis = DOMINANT_AXIS_EXEMPT.get(rel)
    terrain = rel in TERRAIN_BASELINE
    report_only = REPORT_ONLY_SIZE.get(rel)

    # 判据 (b)：几何（accessor）口径 —— 不加任何节点变换直接量顶点。
    # 世界的世界口径必须与它逐轴一致，否则说明「尺寸挂在节点变换上」
    # （HEAD 的 trash_can.glb：几何单位尺度 × node scale 0.0375 → 消费端 collectPairs
    #   抵消 scale 后按 20 m 直径渲染。这就是"垃圾桶比车还大"的真因。）
    size_geo = [g_hi[i] - g_lo[i] for i in range(3)]
    geo_lo = list(g_lo)
    if target and target.get('per_part'):
        # 一文件多件（trash_can 双桶）：按节点（= 单只桶，含其 3 个材质 primitive 的并集）
        # 逐件求 AABB，取各轴最大值 + minY 最小值
        size = [max(b[2][i] - b[1][i] for b in node_boxes) for i in range(3)]
        size_geo = [max(b[2][i] - b[1][i] for b in box['geom_node_boxes']) for i in range(3)]
        lo_fix = list(lo)
        lo_fix[1] = min(b[1][1] for b in node_boxes)
        lo = lo_fix
        geo_lo[1] = min(b[1][1] for b in box['geom_node_boxes'])
        notes.append('per-part 判据：本文件含 %d 个节点（%d 材质 primitive），'
                     '取单节点最大尺寸 / 最低 minY' % (len(node_boxes), len(details)))
    skew = [abs(size[i] - size_geo[i]) for i in range(3)]
    if max(skew) > 1e-6:
        problems.append('FAIL  几何口径与世界口径不等（Δ=%.4f/%.4f/%.4f）—— 尺寸挂在节点变换上'
                        % (skew[0], skew[1], skew[2]))
    else:
        notes.append('几何(accessor)口径 ≡ 世界口径（Δ < 1e-6）⇒ 几何顶点本身即最终世界尺寸')

    # 1. 直立（高度轴落在 glTF Y）
    dom = max(range(3), key=lambda i: size[i])
    dom_name = 'XYZ'[dom]
    mode = 'x_flat' if exempt_axis else (target or {}).get('axis', 'target' if target else 'y')
    # 目标件默认直立性 = 「Y 实测 = 声明高度 ±5%」+「minY = 0」（二者合起来即"站得住"），
    # 由尺寸 / 贴地判据承载，主包围轴只作提示（18 m 宽的消防站长轴本来就是 X）。
    if terrain:
        notes.append('地形基准：直立判据跳过（主包围轴 %s）' % dom_name)
    elif mode == 'target':
        notes.append('直立：高度轴 = Y（实测 %.3f vs 目标高 %.3f，见 Y 判据）；主包围轴 = %s'
                     % (size[1], target['y'], dom_name))
    elif mode == 'x_flat':
        # 长轴 = X 属正确摆放（船体 / 车长）：真实车长 4.6~12 m 必然大于车高，
        # 故直立性改由 Y 尺寸判据保证（Y 实测 = 目标车高才判 ok；侧躺模型的 Y 会落到车宽/车长上）
        if dom_name != 'X':
            problems.append('FAIL  直立：主包围轴 = %s（长轴件应为 X：船体/车长）' % dom_name)
        else:
            notes.append('长轴 X 属正确摆放（船体/车长）；直立性由 Y 尺寸判据保证（Y = 高度轴）')
    elif mode == 'per_part_y':
        # 「立体件」= 主包围轴 = Y、落地（minY=0）、且高度 ≥ 目标高 50% 的件
        h_floor = 0.5 * ((target or {}).get('y') or 0.5)
        upright = [d for d in details
                   if max(range(3), key=lambda i: (d[2][i] - d[1][i])) == 1
                   and abs(d[1][1]) <= 1e-3
                   and (d[2][1] - d[1][1]) >= h_floor]
        need = (target or {}).get('min_parts', 1)
        if len(upright) < need:
            problems.append('FAIL  直立：落地且主包围轴 = Y 的立体件仅 %d 个（需 ≥ %d）'
                            % (len(upright), need))
        else:
            notes.append('直立：%d 个立体件主包围轴 = Y 且落地（需 ≥ %d）：%s'
                         % (len(upright), need, ', '.join(d[0] for d in upright)))
    elif dom_name != 'Y':
        problems.append('FAIL  直立：主包围轴 = %s（应为 Y；X=%.3f Y=%.3f Z=%.3f）'
                        % (dom_name, size[0], size[1], size[2]))

    # 2. 贴地
    if not terrain:
        if rel in WATERLINE:
            if lo[1] >= 0:
                problems.append('FAIL  贴地：吃水线类应 minY < 0，实测 %.4f' % lo[1])
        elif abs(lo[1]) > 1e-3:
            problems.append('FAIL  贴地：minY = %.4f（应为 0）' % lo[1])

    # 3. 尺寸（(a) 世界口径 与 (b) 几何口径 双查 —— 二者一致时数值相同）
    if target:
        # 逐件容差：人物原型按设计 34 §5.2 允许 ±8%（target['tol']），其余沿用全局 ±5%
        tol = float(target.get('tol', SIZE_TOL))
        if tol != SIZE_TOL:
            notes.append('尺寸容差：±%.0f%%（本件 target.tol 覆盖全局 ±%.0f%%）'
                         % (tol * 100, SIZE_TOL * 100))
        for i, key in enumerate('xyz'):
            want = target[key]
            if want is None:
                notes.append('%s 轴不设判据：实测 %.3f' % (key.upper(), size[i]))
                continue
            dev = size[i] - want
            rel_dev = abs(dev) / want
            tag = 'ok  ' if rel_dev <= tol else 'FAIL'
            notes.append('%s 实测 %.4f / 目标 %.3f  偏差 %+.4f (%+.2f%%)  %s'
                         % (key.upper(), size[i], want, dev, rel_dev * 100, tag))
            notes.append('   └ 几何(accessor)口径 %.4f（Δ %+.4f）'
                         % (size_geo[i], size_geo[i] - want))
            if rel_dev > tol:
                problems.append('FAIL  尺寸 %s：实测 %.4f 目标 %.3f（偏差 %+.2f%%）'
                                % (key.upper(), size[i], want, rel_dev * 100))
            if abs(size_geo[i] - want) / want > tol:
                problems.append('FAIL  几何口径尺寸 %s：%.4f 目标 %.3f'
                                % (key.upper(), size_geo[i], want))
    elif report_only:
        notes.append('尺寸只报不判：%s（实测 X %.4f / Y %.4f / Z %.4f，见 --emit-json 可留档）'
                     % (report_only, size[0], size[1], size[2]))

    # 4. 节点 identity（硬判据 = scale；软判据 = 非骨骼节点的 rotation/translation）
    if bad:
        problems.append('FAIL  节点 scale 非 1 ⇒ 尺寸挂在节点变换上：%s' % ', '.join(bad))
    else:
        notes.append('节点 scale：全部 1（%d 节点；%d 个骨骼/蒙皮节点按规范豁免）'
                     % (len(js['nodes']), len(box['exempt'])))
    if soft_nodes:
        notes.append('提示（软判据，非 FAIL）：%d 个非骨骼节点带 rotation/translation：%s'
                     % (len(soft_nodes), ', '.join(soft_nodes[:6])))

    anims = js.get('animations', [])
    if anims:
        notes.append('动画 clip：%s（%d channels）'
                     % (', '.join(a.get('name', '?') for a in anims),
                        sum(len(a['channels']) for a in anims)))
    # 逐节点明细（--emit-json 的 per_part）：world / geom_node_boxes 按构建顺序一一对应；
    # 同名节点（road_props 的 3 盏灯都叫 RoadProps）加 #n 后缀消歧。
    parts, seen = {}, {}
    for (name, nlo, nhi), (_gname, gnlo, gnhi) in zip(node_boxes, box['geom_node_boxes']):
        seen[name] = seen.get(name, 0) + 1
        key = name if seen[name] == 1 else '%s#%d' % (name, seen[name])
        parts[key] = dict(world=[nhi[i] - nlo[i] for i in range(3)],
                          geo=[gnhi[i] - gnlo[i] for i in range(3)],
                          minY=nlo[1])
    return problems, dict(size=size, lo=lo, hi=hi, notes=notes, details=details,
                          geom_lo=geo_lo, geom_hi=g_hi, size_geo=size_geo,
                          kb=os.path.getsize(path) / 1024.0, target=target, rel=rel,
                          parts=parts, node_scale_issues=list(bad))


# ── --emit-json / --check-table ──────────────────────────────────────────
def measured_payload(infos):
    """infos（rel → check 结果）→ 任务书规定的 JSON 形状（世界单位）。"""
    out = {}
    for rel in sorted(infos):
        info = infos[rel]
        if not info:
            continue
        key = rel[:-4] if rel.endswith('.glb') else rel
        out[key] = dict(
            world=[round(v, 6) for v in info['size']],
            geo=[round(v, 6) for v in info['size_geo']],
            minY=round(info['lo'][1], 6),
            nodeScaleIssues=list(info['node_scale_issues']),
            per_part={k: dict(world=[round(v, 6) for v in p['world']],
                              geo=[round(v, 6) for v in p['geo']],
                              minY=round(p['minY'], 6))
                      for k, p in info['parts'].items()},
        )
    return out


def _lookup_info(key, infos):
    """表键 → 实测结果。接受 `cat/name`、`cat/name.glb`、唯一 basename（如 `cactus`）。

    注：前端 `REAL_DIMS_M` 的 camelCase 键（oakTree / trashCan …）不在映射范围——
    本工具刻意**不建第二份名字表**（防两表漂移，批次 30 立项动机之一）；
    前端侧 diff 脚本把键重命名为资产名（见 03 文档的对照表）后再喂进来。
    """
    rel = key if key.endswith('.glb') else key + '.glb'
    if rel in infos:
        return infos[rel], None
    base = rel.split('/')[-1]
    hits = [r for r in infos if r.split('/')[-1] == base]
    if len(hits) == 1:
        return infos[hits[0]], None
    if len(hits) > 1:
        return None, '表项 %s 有歧义（%d 个同名资产：%s），请写全 <类别>/<名>' % (
            key, len(hits), ', '.join(sorted(hits)))
    return None, '表项 %s 查无实测（键应为 <类别>/<名> 或唯一 basename，可带 .glb）' % key


def _table_entry_xyz(raw):
    """表值 → [x, y, z]（None = 该轴不比对）。接受 [x,y,z] / {"world":[..]} / {"x","y","z"}。"""
    if isinstance(raw, (list, tuple)) and len(raw) == 3:
        return list(raw)
    if isinstance(raw, dict):
        if 'world' in raw:
            return list(raw['world'])
        return [raw.get('x'), raw.get('y'), raw.get('z')]
    return None


def check_table(table_path, infos):
    """实测 vs 外部表值逐边比对（±5%）。返回 problem 列表（空 = 通过）。

    这就是批次 29 方案 §3.7 想要的交叉校验：把前端 cityScale.ts::REAL_DIMS_M
    （米制，`__unit__: "m"`）或任何副本表丢进来，即可自动 diff「表值 vs 资产真值」。
    """
    with open(table_path, 'r', encoding='utf-8') as fh:
        table = json.load(fh)
    if not isinstance(table, dict):
        return ['--check-table：%s 顶层必须是 JSON 对象' % table_path]
    unit = str(table.get('__unit__', 'world')).lower()
    div = 10.0 if unit in ('m', 'meter', 'meters', '米') else 1.0
    problems, rows = [], []
    for key in sorted(table):
        if key.startswith('__') or key.startswith('//'):
            continue
        want = _table_entry_xyz(table[key])
        if want is None:
            problems.append('表项 %s 的值无法解析：%r' % (key, table[key]))
            continue
        want = [None if w is None else w / div for w in want]
        info, err = _lookup_info(key, infos)
        if not info:
            problems.append(err or ('表项 %s 查无实测' % key))
            continue
        got = info['size']
        for i, axis in enumerate('xyz'):
            w = want[i]
            if w is None:
                rows.append('%-32s %s 轴不比对（实测 %.4f）' % (key, axis.upper(), got[i]))
                continue
            if w == 0:
                rows.append('%-32s %s 轴表值 0，跳过（实测 %.4f）' % (key, axis.upper(), got[i]))
                continue
            rel_dev = abs(got[i] - w) / abs(w)
            tag = 'ok  ' if rel_dev <= SIZE_TOL else 'FAIL'
            rows.append('%-32s %s 实测 %.4f / 表值 %.4f  偏差 %+.2f%%  %s'
                        % (key, axis.upper(), got[i], w, (got[i] - w) / abs(w) * 100, tag))
            if rel_dev > SIZE_TOL:
                problems.append('表值比对 %s %s：实测 %.4f 表值 %.4f（偏差 %+.2f%%）'
                                % (key, axis.upper(), got[i], w, (got[i] - w) / abs(w) * 100))
    print('\n' + '=' * 108)
    print('--check-table %s（单位：%s，容差 ±%d%%）' % (table_path, unit, int(SIZE_TOL * 100)))
    print('=' * 108)
    for r in rows:
        print('    · %s' % r)
    return problems


def parse_cli(argv):
    """极简 CLI（零第三方依赖）：返回 (files, emit_path, table_path)。

    --emit-json 后若下一个 token 以 .glb 结尾则视为文件实参（省略路径 ⇒ 打 stdout）。
    """
    args = list(argv)
    files, emit_path, table_path = [], None, None
    i = 0
    while i < len(args):
        a = args[i]
        if a == '--emit-json':
            nxt = args[i + 1] if i + 1 < len(args) else None
            if nxt is not None and not nxt.endswith('.glb') and not nxt.startswith('--'):
                emit_path, i = nxt, i + 2
            else:
                emit_path, i = '-', i + 1
        elif a.startswith('--emit-json='):
            emit_path, i = a.split('=', 1)[1] or '-', i + 1
        elif a == '--check-table':
            if i + 1 >= len(args):
                raise SystemExit('--check-table 需要一个 JSON 表路径参数')
            table_path, i = args[i + 1], i + 2
        elif a.startswith('--check-table='):
            table_path, i = a.split('=', 1)[1], i + 1
        elif a in ('-h', '--help'):
            print(__doc__)
            raise SystemExit(0)
        else:
            files.append(a)
            i += 1
    return files, emit_path, table_path


def main(argv):
    try:
        cli_files, emit_path, table_path = parse_cli(argv)
    except SystemExit as exc:
        return exc.code if isinstance(exc.code, int) else 2
    if cli_files:
        files = sorted(os.path.abspath(p) for p in cli_files)
    else:
        files = sorted(glob.glob(os.path.join(MODELS_DIR, '*', '*.glb')))
    if not files:
        print('no .glb found')
        return 2
    total_fail = 0
    infos = {}
    print('=' * 108)
    print('verify_glb_aabb — GLB 轴线 / 贴地 / 尺度 / 节点 identity 自检（1 世界单位 = 10 m）')
    print('判据：(a) 世界包围盒 = 目标 ±5%  (b) 几何 accessor 包围盒 = 目标（≡(a) 才说明'
          '尺寸没挂在节点变换上）  (c) 节点 scale 全 1（骨骼/蒙皮节点豁免）  (d) 直立 / 贴地')
    print('=' * 108)
    for path in files:
        rel = os.path.relpath(path, MODELS_DIR).replace(os.sep, '/')
        try:
            problems, info = check(path, rel)
        except Exception as exc:                       # noqa: BLE001
            print('\n### %-34s 解析失败：%s' % (rel, exc))
            total_fail += 1
            continue
        infos[rel] = info
        mark = 'PASS' if not problems else 'FAIL'
        if info is None:
            print('\n### %-34s %s' % (rel, mark))
            total_fail += len(problems)
            continue
        lo, hi, size = info['lo'], info['hi'], info['size']
        print('\n### %-34s [%s]  %6.1f KB' % (rel, mark, info['kb']))
        print('    包围盒   X %.4f  Y %.4f  Z %.4f     minY %+.4f   (X %.3f..%.3f / Z %.3f..%.3f)'
              % (size[0], size[1], size[2], lo[1], lo[0], hi[0], lo[2], hi[2]))
        if info['target']:
            tz = info['target']['z']
            print('    目标     X %.3f  Y %.3f  Z %s   pivot: %s'
                  % (info['target']['x'], info['target']['y'],
                     ('%.3f' % tz) if tz is not None else '--',
                     info['target']['pivot']))
        for n in info['notes']:
            print('    · %s' % n)
        for p in problems:
            print('    ✗ %s' % p)
        if info['kb'] > 500:
            print('    ✗ FAIL  体积 %.1f KB > 500 KB（CLAUDE.md §27.3）' % info['kb'])
            problems.append('size')
        total_fail += len(problems)
        # 逐件明细（便于定位是哪一件定义包围盒）
        if os.environ.get('VERIFY_VERBOSE'):
            for name, nlo, nhi in info['details']:
                print('      └ %-28s X %.3f..%.3f  Y %.3f..%.3f  Z %.3f..%.3f'
                      % (name, nlo[0], nhi[0], nlo[1], nhi[1], nlo[2], nhi[2]))
    if emit_path is not None:
        payload = json.dumps(measured_payload(infos), ensure_ascii=False, indent=2, sort_keys=True)
        if emit_path == '-':
            print('\n' + '=' * 108)
            print('--emit-json（世界单位；一次性量测产物，勿入库）')
            print('=' * 108)
            print(payload)
        else:
            with open(emit_path, 'w', encoding='utf-8') as fh:
                fh.write(payload + '\n')
            print('\n--emit-json 已写入 %s（%d 件；一次性量测产物，勿入库）'
                  % (os.path.abspath(emit_path), len(measured_payload(infos))))
    if table_path is not None:
        t_problems = check_table(table_path, infos)
        for p in t_problems:
            print('    ✗ %s' % p)
        total_fail += len(t_problems)
    print('\n' + '=' * 108)
    print('扫描 %d 个 GLB，%s' % (len(files), '全部通过 ✓' if not total_fail else '%d 项不符合 ✗' % total_fail))
    return 1 if total_fail else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
