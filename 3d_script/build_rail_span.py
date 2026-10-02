#!/usr/bin/env python3
"""
build_rail_span — 批次 49「高架铁路真实感」30.5 m 标准跨模块（civic/rail_span.glb）。

── 为什么必须是「模块 + 实例化」而不是一整条 610 m ───────────────────────
  高架走廊 610 m，一次做成一件 GLB 必然超 §27.3 的 500 KB 硬线，且 20 段完全相同。
  真实工程本身就是「标准跨预制箱梁逐孔架设」⇒ **一件标准跨 GLB × 20 实例**
  （`edge/glbInstanced.tsx` ⇒ draw call = 子网格数，与实例数无关）。
  车站处整跨跳过，由 `build_rail_station.py` 的自持站区结构代替。

── §27.0-1 真实形态调研（逐条依据见 lag_docs 设计 49 §2.1）────────────────
  | 部件            | 真实规格                       | 依据                 | 本件取值 |
  |-----------------|--------------------------------|----------------------|----------|
  | 标准跨          | 一般 25~30 m，跨路口加大        | GB/T 51234-2017      | 30.5 m   |
  | 梁型 / 梁高     | 单箱单室简支箱梁，2.0 m        | 市域铁路推荐值       | 2.00 m   |
  | 桥面总宽（双线）| 8.65~9.0 m                     | 北京地铁 5 号线      | 9.00 m   |
  | 双线墩          | 2.4 × 1.8 m                    | 工程实例统计         | 墩根 2.4(Y)×1.7(X)  |
  | 盖梁            | 纵 3.2~3.4 × 横 10.37 m        | 圆端形墩盖梁         | 3.40×10.40 |
  | 承台            | 5.0×5.0（四桩）等              | GB 50157-2013        | 4.60×4.60×1.60 |
  | 桥下净空        | ≥4.5 m（跨城市道路）          | GB 50157-2013        | 7.40 m   |
  | 轨距 / 钢轨     | 1435 mm / 60 kg/m              | GB/T 51234-2017      | 同       |
  | 双线线间距      | 3.6 m                          | 同                   | 3.60 m   |
  | 轨道结构        | 高架线用整体道床（无砟）        | 同                   | 板 0.30 m |
  | 接触线高度      | 5.0~5.3 m（轨顶以上，80 km/h） | GB 50157-2013        | 5.00 m   |

  ⚠ 30.5 m 是**非整数跨距**，看着像凑数。它是设计 49 §1.2 那个净距数值解的
    **唯一可行解**：干道 arterial-z±26 距 z=46 走廊只有 520 m，均匀桥墩阶梯
    在 25~35 m 全区间内逐个网格搜索，只有 30.5 m 能让 20 根墩全部离路缘 ≥3.1 m。
    工程先例：某地铁区间碎石道床段即 25.815 m。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 Z-up、米制；导出自动 Yup ⇒ three.js 直立。
  · **X = 走廊方向**：本模块梁跨自 x=0 延伸到 x=+30.5，**桥墩在 x=0**。
    `center_content_xz()` 后墩心落在局部 x ≈ -14.1（消费端 `RAIL_PIER_LOCAL_X` 同步）。
  · **Y = 横向**：双线对称于 y=0（线间距 ±1.80）。**+Y 面 = 声屏障侧** ——
    three 里 z = -Blender y，所以 +Y 落在世界的**负 z**（南）侧，正对 z=40 的
    体育新城 / 湿地公园（噪声敏感面）。真实城轨只朝敏感侧装声屏障。
  · Z = 0 为地面；`minY = 0` 由承台底保证（桥墩从地面长起来，天然贴地）。

── ⚠ Blender `make_box(size)` 的轴序（批次 47 教训 #9）────────────────────
  **是 (X 宽, Y 水平进深, Z 高度)**，与 three `BoxGeometry(w,h,d)` 的中间槽位相反。
  本文件所有 make_box 一律按 (X 宽, Y 进深, Z 高) 书写。

── 材质槽（6 ⇒ 6 个 primitive，20 个实例共用同一批 ⇒ 6 draw call）────────
  Rail_Concrete / _ConcreteDark / _Steel / _Glass / _Warn / _Light

用法：
  blender --background --python build_rail_span.py -- \
    ClientWeb/src/assets/models/civic/rail_span.glb
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)

from __common__ import (  # noqa: E402
    CITY_PALETTE, assign_material, center_content_xz, join_objects,
    make_box, make_material, export_glb, reset_scene, set_unit_meters,
)

# ── 几何常量（米）────────────────────────────────────────────────────────
SPAN = 30.5               # 标准跨
X0 = 0.0                  # 桥墩中心（本模块局部 x）

# 桥墩
CAP_L = 4.60              # 承台边长
CAP_TOP = 1.60            # 承台顶
SHAFT_HX = 0.85           # 墩身纵向半宽（1.70 m）
SHAFT_HY = 1.20           # 墩身横向半宽（2.40 m）
SHAFT_TOP = 6.60
HEAD_HX = 0.85
HEAD_HY = 1.80            # 墩顶扩大头 3.60 m 横向
HEAD_TOP = 8.00

# 盖梁 + 支座
BEAM_HX = 1.70            # 3.40 m 纵向
BEAM_HY = 5.20            # 10.40 m 横向
BEAM_BOT = 7.80           # 桥下净空 7.80 m（≥4.5 强条）
BEAM_TOP = 9.20
BEARING_HX = 0.30
BEARING_HY = 0.30
BEARING_H = 0.16
BEARING_Y = 2.45          # 支座落在腹板线上

# 单箱单室箱梁
GIRDER_BOT = BEAM_TOP + BEARING_H      # 9.36
FLOOR_T = 0.30           # 底板厚
SLAB_T = 0.25            # 顶板厚
GIRDER_H = 2.00           # L/15.25
GIRDER_TOP = GIRDER_BOT + GIRDER_H      # 11.36
BOT_W = 5.20              # 底板宽
TOP_W = 9.00              # 顶板宽
WEB_T = 0.40
WEB_Y_BOT = 2.60
WEB_Y_TOP = 2.45

# 轨道
TRACK_C = 1.80            # 线间距 3.60 m
BALLAST_HW = 1.30         # 整体道床半宽 2.60
BALLAST_T = 0.30
GAUGE = 1.435
RAIL_H = 0.176            # 60 kg/m 钢轨高
RAIL_W = 0.075
RAILTOP = GIRDER_TOP + BALLAST_T + RAIL_H     # 11.836

# 桥面系
WALK_Y = 3.50             # 检修走道中心
WALK_W = 0.70
PARAPET_HY = 4.32         # 防撞墙中心
PARAPET_HT = 0.36
PARAPET_H = 1.10
BARRIER_HY = 4.42         # 声屏障中心（在防撞墙顶面上）
BARRIER_HT = 0.06
BARRIER_H = 2.00
BARRIER_X0 = 1.40         # 让开门架柱：屏障自本模块 x=1.40 起
BARRIER_POST_PITCH = 3.00

# 接触网（门式，架在防撞墙顶上）
MAST_HY = 4.32
MAST_W = 0.28
MAST_BASE = GIRDER_TOP + PARAPET_H          # 12.46
MAST_TOP = 17.90
BEAM_Z = 17.60            # 门架横梁中心
CONTACT_Z = RAILTOP + 5.00                  # 16.836（轨顶以上 5.00 m）
MESSENGER_Z = RAILTOP + 5.46                # 17.296
STAY_DZ = 0.45                              # 腕臂斜拉在立柱侧的高差（挂点须低于门架横梁底）

# 灯与标
LAMP_H = 0.26
WARN_BAND_Z = 0.62         # 承台防撞警示带中心


def build() -> list:
    """返回按材质分桶的 [(objects, material_name), ...]。"""
    concrete, concrete_dark, steel, glass, warn, light = [], [], [], [], [], []

    cx = X0 + SPAN / 2.0   # 跨中（所有沿 X 均布的构件以它为中心）

    # ── ① 承台 4.60×4.60×1.60 ────────────────────────────────────────
    concrete.append(make_box('Cap', (CAP_L, CAP_L, CAP_TOP), (X0, 0.0, CAP_TOP / 2.0)))
    # 承台顶收进（八度倒角的低成本近似：加一圈 0.30 m 高的踢面）
    concrete.append(make_box('CapChamfer', (CAP_L - 0.50, CAP_L - 0.50, 0.30),
                             (X0, 0.0, CAP_TOP + 0.15)))
    # 承台侧防撞黄黑警示带（真实桥墩底部 0.5~1.0 m 高的警示漆）
    warn.append(make_box('CapWarn', (CAP_L + 0.06, CAP_L + 0.06, 0.34),
                         (X0, 0.0, WARN_BAND_Z)))

    # ── ② 墩身 2.40(Y)×1.70(X) + 墩顶扩大头 4.00(Y) ──────────────────
    concrete.append(make_box('Shaft', (SHAFT_HX * 2, SHAFT_HY * 2, SHAFT_TOP - CAP_TOP),
                             (X0, 0.0, (CAP_TOP + SHAFT_TOP) / 2.0)))
    concrete.append(make_box('PierHead', (HEAD_HX * 2, HEAD_HY * 2, HEAD_TOP - SHAFT_TOP),
                             (X0, 0.0, (SHAFT_TOP + HEAD_TOP) / 2.0)))
    # 墩身正面里程标牌（真实桥墩有「XX 公里 + 桩号」的白底黑字牌）
    warn.append(make_box('MilePost', (0.04, 0.70, 0.45), (X0 - SHAFT_HX - 0.02, 0.0, 3.20)))

    # ── ③ 盖梁 3.40(X)×10.40(Y) + 4 块板式支座 ──────────────────────
    concrete.append(make_box('PierCap', (BEAM_HX * 2, BEAM_HY * 2, BEAM_TOP - BEAM_BOT),
                             (X0, 0.0, (BEAM_BOT + BEAM_TOP) / 2.0)))
    # 盖梁悬臂端斜托（牛腿）：真实盖梁端头有 1:3 左右的斜面收头，不是一块光板
    for sy in (-1, 1):
        concrete.append(make_box(f'CapHaunch_{sy}', (BEAM_HX * 2, 1.10, 0.34),
                                 (X0, sy * (BEAM_HY - 0.55), BEAM_BOT + 0.17),
                                 rot=(sy * math.radians(17.0), 0.0, 0.0)))
    for sx in (-0.55, 0.55):
        for sy in (-1, 1):
            steel.append(make_box(f'Bearing_{sx}_{sy}', (BEARING_HX * 2, BEARING_HY * 2, BEARING_H),
                                  (X0 + sx, sy * BEARING_Y, BEAM_TOP + BEARING_H / 2.0)))

    # ── ④ 单箱单室箱梁：底板 + 斜腹板 + 顶板 ─────────────────────────
    concrete.append(make_box('GirderFloor', (SPAN, BOT_W, FLOOR_T),
                             (cx, 0.0, GIRDER_BOT + FLOOR_T / 2.0)))
    concrete.append(make_box('GirderSlab', (SPAN, TOP_W, SLAB_T),
                             (cx, 0.0, GIRDER_TOP - SLAB_T / 2.0)))
    # 斜腹板：底 y=±2.60 → 顶 y=±2.45，绕 **X 轴**倾 5.90°（批次 48 教训：斜杆别手算欧拉）
    dz = (GIRDER_H - SLAB_T - FLOOR_T)
    dy = WEB_Y_BOT - WEB_Y_TOP
    web_len = math.hypot(dy, dz)
    web_ang = math.atan2(dy, dz)
    for sy in (-1, 1):
        concrete.append(make_box(f'GirderWeb_{sy}', (SPAN, WEB_T, web_len),
                                 (cx, sy * (WEB_Y_BOT + WEB_Y_TOP) / 2.0,
                                  GIRDER_BOT + FLOOR_T + dz / 2.0),
                                 rot=(sy * web_ang, 0.0, 0.0)))
        # 梗腋（腹板-顶板过渡的三角倒角）：45° 小斜块，破「光板」剪影
        concrete.append(make_box(f'HaunchTop_{sy}', (SPAN, 0.62, 0.34),
                                 (cx, sy * 2.36, GIRDER_TOP - SLAB_T - 0.13),
                                 rot=(sy * math.radians(38), 0.0, 0.0)))

    # ── ⑤ 桥面伸缩缝（两端各一道；相邻模块在墩顶对接 ⇒ 缝合成墩顶一整条）──
    for sx in (X0, X0 + SPAN):
        concrete_dark.append(make_box(f'ExpansionJoint_{sx}', (0.12, TOP_W, 0.07),
                                      (sx, 0.0, GIRDER_TOP + 0.035)))

    # ── ⑥ 整体道床 + 钢轨 ×4（轨距 1435，线间距 3600）─────────────────
    for sy in (-1, 1):
        concrete_dark.append(make_box(f'Ballast_{sy}', (SPAN, BALLAST_HW * 2, BALLAST_T),
                                      (cx, sy * TRACK_C, GIRDER_TOP + BALLAST_T / 2.0)))
        # 挡肩（道床两侧高出 0.06 m 的混凝土肩）
        for ek in (-1, 1):
            concrete_dark.append(
                make_box(f'Shoulder_{sy}_{ek}', (SPAN, 0.34, BALLAST_T + 0.06),
                         (cx, sy * TRACK_C + ek * (BALLAST_HW + 0.17),
                          GIRDER_TOP + (BALLAST_T + 0.06) / 2.0)))
        for gk in (-1, 1):
            steel.append(make_box(f'RailHead_{sy}_{gk}', (SPAN, RAIL_W, RAIL_H),
                                  (cx, sy * TRACK_C + gk * GAUGE / 2.0,
                                   GIRDER_TOP + BALLAST_T + RAIL_H / 2.0)))

    # ── ⑦ 检修走道 + 防撞墙 ×2 + 电缆槽 ─────────────────────────────
    for sy in (-1, 1):
        concrete_dark.append(make_box(f'Walkway_{sy}', (SPAN, WALK_W, 0.08),
                                      (cx, sy * WALK_Y, GIRDER_TOP + 0.04)))
        steel.append(make_box(f'Trough_{sy}', (SPAN, 0.26, 0.22),
                              (cx, sy * 4.00, GIRDER_TOP + 0.11)))
        concrete.append(make_box(f'Parapet_{sy}', (SPAN, PARAPET_HT, PARAPET_H),
                                 (cx, sy * PARAPET_HY, GIRDER_TOP + PARAPET_H / 2.0)))
        # 防撞墙顶压顶（外侧带 6° 坡，真实预制栏板顶面）
        concrete_dark.append(make_box(f'ParapetCap_{sy}', (SPAN, PARAPET_HT + 0.10, 0.12),
                                      (cx, sy * PARAPET_HY, GIRDER_TOP + PARAPET_H + 0.06)))

    # ── ⑧ 声屏障（**仅 +Y 侧**）+ 立柱 ───────────────────────────────
    bar_len = SPAN - BARRIER_X0
    bar_cx = BARRIER_X0 + bar_len / 2.0
    z_bar0 = GIRDER_TOP + PARAPET_H + 0.12
    glass.append(make_box('Barrier', (bar_len, BARRIER_HT, BARRIER_H),
                          (bar_cx, BARRIER_HY, z_bar0 + BARRIER_H / 2.0)))
    n_post = int(bar_len / BARRIER_POST_PITCH) + 1
    for i in range(n_post):
        px = BARRIER_X0 + i * BARRIER_POST_PITCH
        if px > SPAN - 0.2:
            break
        steel.append(make_box(f'BarrierPost_{i}', (0.09, 0.14, BARRIER_H + 0.26),
                              (px, BARRIER_HY, z_bar0 + (BARRIER_H + 0.26) / 2.0 - 0.13)))
    # 屏障顶封边 + 底部横挡
    steel.append(make_box('BarrierRail', (bar_len, 0.12, 0.08),
                          (bar_cx, BARRIER_HY, z_bar0 + BARRIER_H + 0.04)))

    # ── ⑨ 接触网门架（立在防撞墙顶上）+ 腕臂 + 承力索 + 接触线 ───────
    for sy in (-1, 1):
        steel.append(make_box(f'Mast_{sy}', (MAST_W, MAST_W, MAST_TOP - MAST_BASE),
                              (X0, sy * MAST_HY, (MAST_BASE + MAST_TOP) / 2.0)))
        # 腕臂斜拉：自立柱（y=±4.32, z=CONTACT_Z+0.50）下拉到腕臂外端（y=±2.52）。
        # ⚠ 欧拉角不能手算成 atan2(dy, dz)：rotate_X 把局部 +Z 映到 (0,-sinθ,cosθ)，
        #   这里要的是**朝下且朝内**的 (0,-0.89,-0.45)，θ 必须落在第二象限。
        #   批次 48 教训「斜杆别手算欧拉」的正解是 make_strut；此处杆件是轴对齐盒体
        #   且要与门架横梁避让（挂点 17.34 < 横梁底 17.43），故仍显式解 θ。
        steel.append(make_box(f'ArmStay_{sy}', (0.10, 0.10, math.hypot(MAST_HY - 2.52, STAY_DZ)),
                              (X0, sy * (MAST_HY + 2.52) / 2.0, CONTACT_Z + 0.05 + STAY_DZ / 2.0),
                              rot=(sy * math.atan2(MAST_HY - 2.52, -STAY_DZ), 0.0, 0.0)))
        steel.append(make_box(f'Arm_{sy}', (0.09, MAST_HY - TRACK_C, 0.09),
                              (X0, sy * (MAST_HY + TRACK_C) / 2.0, CONTACT_Z + 0.05)))
        # 绝缘子（腕臂根部）
        concrete_dark.append(make_box(f'Insulator_{sy}', (0.14, 0.14, 0.44),
                                      (X0, sy * (MAST_HY - 0.26), CONTACT_Z + 0.72)))
        # 承力索吊弦（横梁 → 承力索）
        steel.append(make_box(f'Dropper_{sy}', (0.05, 0.05, BEAM_Z - MESSENGER_Z - 0.18),
                              (X0, sy * TRACK_C, (MESSENGER_Z + 0.09 + BEAM_Z) / 2.0)))
        # 接触线 + 承力索：贯穿全跨（真实施工是整跨锚固在两端）
        steel.append(make_box(f'ContactWire_{sy}', (SPAN, 0.045, 0.045),
                              (cx, sy * TRACK_C, CONTACT_Z)))
        steel.append(make_box(f'Messenger_{sy}', (SPAN, 0.055, 0.055),
                              (cx, sy * TRACK_C, MESSENGER_Z)))
    steel.append(make_box('PortalBeam', (0.30, MAST_HY * 2 + 0.40, 0.34),
                          (X0, 0.0, BEAM_Z)))

    # ── ⑩ 桥面照明（防撞墙顶，反光罩 + 发光面）+ 桥墩航空障碍灯 ───────
    for sy in (-1, 1):
        steel.append(make_box(f'LampArm_{sy}', (0.08, 0.55, 0.08),
                              (cx, sy * (PARAPET_HY - 0.28), GIRDER_TOP + PARAPET_H + 0.30)))
        light.append(make_box(f'LampLens_{sy}', (0.34, 0.44, 0.06),
                              (cx, sy * (PARAPET_HY - 0.50), GIRDER_TOP + PARAPET_H + 0.24)))
    light.append(make_box('PierBeacon', (0.16, 0.16, 0.14), (X0, 0.0, HEAD_TOP + 0.07)))

    groups = [
        (concrete, 'Rail_Concrete'),
        (concrete_dark, 'Rail_ConcreteDark'),
        (steel, 'Rail_Steel'),
        (glass, 'Rail_Glass'),
        (warn, 'Rail_Warn'),
        (light, 'Rail_Light'),
    ]
    return groups


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)
    groups = build()

    mat = {
        'Rail_Concrete': make_material('Rail_Concrete_Mat', CITY_PALETTE['concrete'],
                                       rough=0.90, metal=0.0),
        'Rail_ConcreteDark': make_material('Rail_ConcreteDark_Mat', CITY_PALETTE['rail_ballast'],
                                           rough=0.88, metal=0.0),
        'Rail_Steel': make_material('Rail_Steel_Mat', CITY_PALETTE['rail_mast'],
                                    rough=0.46, metal=0.80),
        'Rail_Glass': make_material('Rail_Glass_Mat', CITY_PALETTE['rail_barrier'],
                                    rough=0.22, metal=0.0, alpha=0.34),
        'Rail_Warn': make_material('Rail_Warn_Mat', CITY_PALETTE['warn_amber'],
                                   rough=0.62, metal=0.10),
        'Rail_Light': make_material('Rail_Light_Mat', CITY_PALETTE['station_light'],
                                    rough=0.30, metal=0.0,
                                    emissive=CITY_PALETTE['station_light'],
                                    emissive_intensity=0.4),
    }
    joined = []
    for objs, name in groups:
        if not objs:
            continue
        obj = join_objects(objs, name)
        assign_material(obj, mat[name])
        joined.append(obj)

    # ⚠ 全件米 → 世界单位（×0.1）；顺序：transform_apply flatten → ×0.1 → 钉扎 → 居中
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 贴地钉扎（**带容差**，批次 47 教训 #10）。本件承台底就是 z=0，应完全无动作。
    PIN_TOLERANCE_M = 0.05
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z >= 0:
            continue
        if -min_z > PIN_TOLERANCE_M:
            print(f'[pin] ⚠ {obj.name} 最低点在 {min_z:.3f} m（超过容差），**不钉扎**', flush=True)
            continue
        obj.location.z = -0.1 * min_z

    # `center_content_xz` 返回的 dx 就是**墩心（原建模 x=0）在居中后的局部 x**。
    # 消费端 `RAIL_PIER_LOCAL_X` 必须与此逐位一致，否则 20 个实例的墩会整体错位。
    dx, _dy = center_content_xz(joined)

    bpy.context.view_layer.update()
    import mathutils  # noqa: PLC0415
    lo = min((o.matrix_world @ mathutils.Vector(c)).x for o in joined for c in o.bound_box)
    hi = max((o.matrix_world @ mathutils.Vector(c)).x for o in joined for c in o.bound_box)
    zmax = max((o.matrix_world @ mathutils.Vector(c)).z for o in joined for c in o.bound_box)
    print(f'[info] 模块 X 包围盒 [{lo:.4f}, {hi:.4f}] u（宽 {hi - lo:.4f} u = '
          f'{(hi - lo) * 10:.2f} m）｜墩心局部 x = {dx:+.4f} u'
          f'｜全高 {zmax:.4f} u = {zmax * 10:.2f} m', flush=True)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/rail_span.glb')
    export_glb(out_path)
    print(f'✅ rail_span.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
