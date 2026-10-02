#!/usr/bin/env python3
"""
build_construction_site — 批次 50「施工工地真实感」（civic/construction_site.glb）。

── 重制理由（逐条依据见 lag_docs 设计 50 §1.1）───────────────────────────
  现状 `props/ConstructionSite.tsx`（148 行 / 3 mesh）的塔吊是
  「1.6 m 见方 × 9 m 高的**一根实心柱** + 一根 7.2 m 的 10 cm 细杆当起重臂」，
  而 QTZ80 独立式的实际参数是：标准节 1.8×1.8×2.5 m **格构**、14~18 节
  （起升高度 40.5~46.2 m、总高约 55.9 m）、起重臂 50/55/60 m **三角形桁架**、
  平衡臂 12.4~13.4 m、配重 11.75~18 t（5~7 块）、承台 4.0×4.0×1.2 m。
  差了一整个数量级 —— 城市天际线「在生长」的那个符号是错的。

── §27.0-1 真实形态调研（结论先行，完整表见设计 50 §2.1）────────────────
  基准型号：方圆 QTZ80(TC6010) + TC5015 臂长档。
  | 部件        | 真实规格                    | 本件取值          |
  |-------------|-----------------------------|-------------------|
  | 标准节      | 1.8×1.8×2.5 m，Q345B 主肢   | 1.80×1.80×2.50    |
  | 标准节数    | 14~18 节（起升 40.5~46.2 m）| 14 节 = 35.0 m    |
  | 起重臂      | 50 m，三角桁架桁高 1.5~2.0  | 50.0 m，桁高 1.70 |
  | 平衡臂      | 12.39~13.39 m               | 13.0 m            |
  | 配重        | 11.75~18 t（5~7 块）        | 6 块 ≈14 t        |
  | 基础        | 承台 4.0×4.0×1.2 m / 底架8×8| 5.2×5.2×1.20      |
  | 拉索        | 前 2 道 + 后 2 道           | 同                |
  | 围挡        | 高 2.0~2.5 m，竖向压条      | 2.50 m            |

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender Z-up、米制。X = 东西（场坪 50 m），Y = 南北（场坪 44 m），Z = 高。
  · **塔吊回转中心在 (-9.0, +6.0)**（Blender 米），起重臂朝 +X。
  · `minY = 0` 由场坪底面保证（裸土面 -0.10 m 见下）→ **场坪做成 0.10 m 厚的板，
    底面落在 z=0**，既贴地又比沥青面（0.15 m 抬高）低一档，读成「土面被挖开一层」。

── ⚠ Blender `make_box(size)` 的轴序（批次 47 教训 #9）────────────────────
  **是 (X 宽, Y 水平进深, Z 高度)**，与 three `BoxGeometry(w,h,d)` 的中间槽位相反。
  本文件所有 make_box 一律按 (X 宽, Y 进深, Z 高) 书写。

── ⚠ 斜杆一律走 `make_strut`（批次 48 教训：别手算欧拉角）──────────────
  格构塔身与桁架臂的全部腹杆/斜撑都是斜的，手写 `rotation_euler` 在 XYZ 欧拉序
  下「同时绕两轴转」会得到意料之外的朝向且不报错。

── 材质槽（11）─────────────────────────────────────────────────────────
  Site_Earth / Site_Hoard / Crane_Yellow / Crane_Metal / Crane_Weight /
  Crane_Footing / Site_Concrete / Site_Cabin / Site_Material / Site_Warn /
  Site_Net（半透绿安全网，alpha 0.34）

用法：
  blender --background --python build_construction_site.py -- \
    ClientWeb/src/assets/models/civic/construction_site.glb
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
    make_box, make_cylinder, make_material, make_strut, strip_uvs, weld_split_vertices,
    export_glb, reset_scene, set_unit_meters,
)

# ── 场区常量（米）────────────────────────────────────────────────────────
SITE_W = 50.0              # X 向场坪
SITE_D = 44.0              # Y 向场坪
HX, HY = SITE_W / 2.0, SITE_D / 2.0
GROUND_T = 0.10            # 裸土板厚（底面贴 z=0 ⇒ minY=0）

# 围挡
HOARD_H = 2.50
HOARD_T = 0.12
HOARD_RIB_W = 0.16         # 竖向压条
HOARD_RIB_PITCH = 1.20
GATE_W = 7.00              # 南面门洞
GATE_H = 2.30

# ── 塔机（方圆 QTZ80(TC6010) + TC5015 臂长档）────────────────────────────
CR_X, CR_Y = -9.0, 6.0     # 回转中心（Blender XY）
SEC = 2.50                 # 标准节高
SEC_S = 1.80               # 标准节截面（1.8×1.8）
SEC_N = 14                 # 标准节数 ⇒ 塔身 35.0 m
MAST_TOP = SEC_N * SEC     # 35.0
CHORD = 0.16               # 主肢 / 弦杆截面
SLEW_Z = MAST_TOP          # 回转支承面 = 塔身顶
HEAD_H = 8.00              # 塔帽高
HEAD_TOP = SLEW_Z + HEAD_H  # 44.2
JIB_L = 50.0
JIB_BAY = 5.0
JIB_N = int(JIB_L / JIB_BAY)          # 10 节
JIB_D = 1.70                          # 桁高（上弦到下弦）
JIB_ROOT_Z = SLEW_Z + 5.60            # 40.6：臂根弦杆标高
CJIB_L = 13.0                         # 平衡臂
CJIB_N = 4
CJIB_ROOT_Z = SLEW_Z + 4.20
CW_N = 6                            # 配重块数
CW_SX, CW_SY, CW_SZ = 2.40, 0.55, 1.90
FOOT_L = 5.20
FOOT_H = 1.20
CABIN = (1.60, 2.20, 2.00)            # X, Z, Y（操作室）
TROLLEY_X = 30.0                      # 变幅小车位置（幅度 30 m）
HOOK_DROP = 24.0                      # 吊钩垂下长度

# ── 在建结构（现浇框架 3 层）────────────────────────────────────────────
BLD_HX, BLD_HY = 9.0, 6.0
BLD_FLOORS = 3
FLOOR_H = 3.60
BLD_TOP = BLD_FLOORS * FLOOR_H         # 10.8
BLD_CX, BLD_CY = 6.0, -6.0             # 相对场坪中心
COL_X, COL_Y = 0.45, 0.60

# ── 临建与材料堆场 ─────────────────────────────────────────────────────
CABIN_L, CABIN_W, CABIN_H = 6.06, 2.44, 2.59     # ISO 668 20 ft
CABIN_X, CABIN_Y = 13.0, 9.0
CABIN_STACK = 2
REBAR_X = 15.0
AGG_X = 15.5


def build() -> list:
    earth, hoard, yellow, metal, weight = [], [], [], [], []
    footing, concrete, cabin, material, warn, net = [], [], [], [], [], []

    # ── ① 裸土场坪 + 车辙 + 洗车槽 ─────────────────────────────────────
    earth.append(make_box('SiteSlab', (SITE_W, SITE_D, GROUND_T), (0, 0, GROUND_T / 2.0)))
    # 碾压车辙 2 道（沿 X 贯穿，路面被压出两道深色槽）
    for sy in (-7.0, 7.0):
        concrete.append(make_box(f'Rut_{sy}', (SITE_W - 8.0, 0.90, 0.05),
                                 (0, sy, GROUND_T + 0.02)))
    # 洗车槽（大门内侧，混凝土池 + 集水坑）
    concrete.append(make_box('WashSlab', (4.20, 7.00, 0.22), (0, -HY + 5.0, GROUND_T + 0.11)))
    concrete.append(make_box('WashPit', (3.20, 4.20, 0.10), (0, -HY + 5.0, GROUND_T + 0.12)))
    # 现场道路（场坪内的临时硬化路面，避免全场都是裸土）
    for sy in (-HY + 9.0, HY - 8.0):
        concrete.append(make_box(f'HardRoad_{sy}', (SITE_W - 6.0, 5.00, 0.14),
                                 (0, sy, GROUND_T + 0.07)))

    # ── ② 围挡 4 面（南面留 7.0 m 门洞）+ 压条 + 门扇 ────────────────
    def hoarding_run(cx, cy, length, along_x):
        """沿 X（along_x=True）或沿 Y 的一段围挡。"""
        if length <= 0.1:
            return
        size = (length, HOARD_T, HOARD_H) if along_x else (HOARD_T, length, HOARD_H)
        hoard.append(make_box(f'Hoard_{cx}_{cy}', size, (cx, cy, HOARD_H / 2.0)))
        # 顶部檐条
        cap = (length, HOARD_T + 0.10, 0.14) if along_x else (HOARD_T + 0.10, length, 0.14)
        hoard.append(make_box(f'HoardCap_{cx}_{cy}', cap, (cx, cy, HOARD_H + 0.07)))
        # 竖向压条
        n = int(length / HOARD_RIB_PITCH)
        for i in range(n + 1):
            t = -length / 2.0 + i * HOARD_RIB_PITCH
            if t > length / 2.0:
                break
            rib = (HOARD_RIB_W, HOARD_T + 0.05, HOARD_H - 0.20) if along_x \
                else (HOARD_T + 0.05, HOARD_RIB_W, HOARD_H - 0.20)
            px = cx + t if along_x else cx
            py = cy if along_x else cy + t
            yellow.append(make_box(f'Rib_{cx}_{cy}_{i}', rib, (px, py, (HOARD_H - 0.20) / 2.0 + 0.10)))

    hoarding_run(0, HY, SITE_W, True)                      # 北面整段
    hoarding_run(-HX, 0, SITE_D, False)                    # 西面整段
    hoarding_run(HX, 0, SITE_D, False)                     # 东面整段
    seg = (SITE_W - GATE_W) / 2.0
    hoarding_run(-(GATE_W / 2.0 + seg / 2.0), -HY, seg, True)   # 南面左段
    hoarding_run(+(GATE_W / 2.0 + seg / 2.0), -HY, seg, True)   # 南面右段
    # 门柱 + 2 扇门扇（半开）
    for sx in (-1, 1):
        yellow.append(make_box(f'GatePost_{sx}', (0.30, 0.30, GATE_H + 0.55),
                               (sx * GATE_W / 2.0, -HY, (GATE_H + 0.55) / 2.0)))
    for sx, ang in ((-1, 26.0), (1, -26.0)):
        leaf = GATE_W / 2.0 - 0.20
        metal.append(make_box(f'GateLeaf_{sx}', (leaf, 0.08, GATE_H - 0.20),
                              (sx * (GATE_W / 2.0 - leaf / 2.0) - sx * math.sin(math.radians(ang)) * leaf / 2.0,
                               -HY + math.sin(math.radians(ang)) * leaf / 2.0,
                               (GATE_H - 0.20) / 2.0 + 0.10),
                              rot=(0.0, 0.0, math.radians(-ang * sx))))
    # 大门上方「施工重地」灯箱骨架（不放字，留给后续贴图批次）
    metal.append(make_box('GateSign', (GATE_W + 0.60, 0.14, 0.70),
                          (0, -HY, GATE_H + 0.95)))

    # ── ③ 塔机基础：承台 + 地脚螺栓 + 防护栏 ─────────────────────────
    footing.append(make_box('Footing', (FOOT_L, FOOT_L, FOOT_H),
                            (CR_X, CR_Y, FOOT_H / 2.0)))
    warn.append(make_box('FootingBand', (FOOT_L + 0.08, FOOT_L + 0.08, 0.36),
                         (CR_X, CR_Y, 0.42)))
    for sx in (-1, 1):
        for sy in (-1, 1):
            metal.append(make_cylinder(f'AnchorBolt_{sx}_{sy}', 0.07, 0.07, 0.55, 8,
                                       (CR_X + sx * 0.72, CR_Y + sy * 0.72, FOOT_H + 0.20)))
    # 基础周边防护栏（1.2 m，2 根横杆 + 立柱）
    for k in range(4):
        if k == 0:
            a = (CR_X - 4.2, CR_Y - 4.2, CR_X + 4.2, CR_Y - 4.2)
        elif k == 1:
            a = (CR_X + 4.2, CR_Y - 4.2, CR_X + 4.2, CR_Y + 4.2)
        elif k == 2:
            a = (CR_X - 4.2, CR_Y + 4.2, CR_X + 4.2, CR_Y + 4.2)
        else:
            a = (CR_X - 4.2, CR_Y - 4.2, CR_X - 4.2, CR_Y + 4.2)
        for t in (0.0, 0.5, 1.0):
            px = a[0] + (a[2] - a[0]) * t
            py = a[1] + (a[3] - a[1]) * t
            yellow.append(make_cylinder(f'GuardPost_{k}_{t}', 0.05, 0.05, 1.20, 6, (px, py, 0.70)))
        for hz in (0.70, 1.25):
            metal.append(make_strut(f'GuardRail_{k}_{hz}',
                                    (a[0], a[1], hz), (a[2], a[3], hz), 0.05))

    # ── ④ 格构塔身 14 节：4 主肢 + 每节 4 横撑 + 4 斜撑 ──────────────
    c = SEC_S / 2.0
    corners = [(c, c), (c, -c), (-c, -c), (-c, c)]
    # 主肢：**4 根通长角钢**，不是每节一根短柱 —— 真实塔机的 4 根主肢是贯通的，
    # 标准节之间靠节点板+螺栓连接（节点已在下面按节画出）。这样既更真实，
    # 又把 60 个立方体降到 4 个（批次 50 的 500 KB 硬线主要就卡在杆件数上）。
    for k, (dx, dy) in enumerate(corners):
        yellow.append(make_box(f'Chord_{k}', (CHORD, CHORD, MAST_TOP),
                               (CR_X + dx, CR_Y + dy, FOOT_H + MAST_TOP / 2.0)))
    # 标准节节点板（每节 4 个角处的水平连接板，破「一根光柱」剪影）
    for i in range(1, SEC_N + 1):
        z = FOOT_H + i * SEC
        for k in range(4):
            a0 = corners[k]
            a1 = corners[(k + 1) % 4]
            yellow.append(make_strut(f'NodePlate_{i}_{k}',
                                     (CR_X + a0[0], CR_Y + a0[1], z),
                                     (CR_X + a1[0], CR_Y + a1[1], z), 0.20))
    for i in range(SEC_N):
        z0 = FOOT_H + i * SEC
        z1 = z0 + SEC
        for k in range(4):
            a0 = corners[k]
            a1 = corners[(k + 1) % 4]
            metal.append(make_strut(f'Horiz_{i}_{k}', (CR_X + a0[0], CR_Y + a0[1], z1),
                                    (CR_X + a1[0], CR_Y + a1[1], z1), 0.09))
            # 斜撑（人字形：交替方向）
            if (i + k) % 2 == 0:
                metal.append(make_strut(f'Diag_{i}_{k}', (CR_X + a0[0], CR_Y + a0[1], z0),
                                        (CR_X + a1[0], CR_Y + a1[1], z1), 0.075))
            else:
                metal.append(make_strut(f'Diag_{i}_{k}', (CR_X + a1[0], CR_Y + a1[1], z0),
                                        (CR_X + a0[0], CR_Y + a0[1], z1), 0.075))
    # 回转支承 + 上支座
    yellow.append(make_box('SlewLower', (2.60, 2.60, 0.90),
                           (CR_X, CR_Y, FOOT_H + MAST_TOP + 0.45)))
    metal.append(make_cylinder('SlewRing', 1.30, 1.30, 0.35, 16,
                               (CR_X, CR_Y, FOOT_H + MAST_TOP + 1.05)))
    yellow.append(make_box('SlewUpper', (2.20, 2.20, 1.60),
                           (CR_X, CR_Y, FOOT_H + MAST_TOP + 2.00)))
    Z = FOOT_H + MAST_TOP          # 回转面标高 = 1.2 + 35.0 = 36.2

    # ── ⑤ 塔帽：格构 A 架 + 平台 + 拉索锚点 ──────────────────────────
    top = (Z + HEAD_H)
    yellow.append(make_box('HeadDeck', (2.40, 2.00, 0.16), (CR_X, CR_Y, Z + HEAD_H - 0.10)))
    head_corners = [(-0.85, -0.70), (0.85, -0.70), (0.85, 0.70), (-0.85, 0.70)]
    for k, (dx, dy) in enumerate(head_corners):
        yellow.append(make_strut(f'HeadLeg_{k}', (CR_X + dx, CR_Y + dy, Z + 0.90),
                                 (CR_X + dx * 0.45, CR_Y + dy * 0.45, top), 0.14))
    for k in range(4):
        a0 = head_corners[k]
        a1 = head_corners[(k + 1) % 4]
        metal.append(make_strut(f'HeadRing_{k}', (CR_X + a0[0], CR_Y + a0[1], top - 0.30),
                                (CR_X + a1[0], CR_Y + a1[1], top - 0.30), 0.09))
        metal.append(make_strut(f'HeadX_{k}', (CR_X + a0[0], CR_Y + a0[1], Z + 1.60),
                                (CR_X + a1[0], CR_Y + a1[1], top - 0.30), 0.07))

    # ── ⑥ 起重臂：三角形桁架 10 节（上弦 2 + 下弦 1 + 腹杆）──────────
    def jib_member(name, x0, x1, y0, y1, z0, z1, r, bucket):
        bucket.append(make_strut(name, (CR_X + x0, CR_Y + y0, z0), (CR_X + x1, CR_Y + y1, z1), r))

    JZ = Z + 5.60                     # 41.8：上弦标高
    JD = JZ - JIB_D                   # 40.1：下弦标高（单根，位于回转中心面）
    for i in range(JIB_N):
        x0 = i * JIB_BAY
        x1 = (i + 1) * JIB_BAY
        # 上弦 2 根
        for sy in (-1, 1):
            jib_member(f'JibTop_{i}_{sy}', x0, x1, sy * 0.75, sy * 0.75, JZ, JZ, CHORD, yellow)
        # 下弦 1 根
        jib_member(f'JibBot_{i}', x0, x1, 0.0, 0.0, JD, JD, CHORD, yellow)
        # 竖腹杆（上弦→下弦）
        jib_member(f'JibVert_{x1}', x1, x1, -0.75, 0.0, JZ, JD, 0.08, metal)
        jib_member(f'JibVert2_{x1}', x1, x1, 0.75, 0.0, JZ, JD, 0.08, metal)
        # 斜腹杆
        jib_member(f'JibDiag_{i}', x0, x1, 0.0, 0.0, JD, JZ, 0.075, metal)
        jib_member(f'JibDiag2_{i}', x0, x1, -0.75, 0.75, JZ, JZ, 0.06, metal)
        # 上弦横撑
        jib_member(f'JibTie_{x1}', x1, x1, -0.75, 0.75, JZ, JZ, 0.07, metal)
    # 臂端封头 + 端部滑轮组
    yellow.append(make_box('JibTip', (0.60, 2.00, JIB_D + 0.20), (CR_X + JIB_L, CR_Y, JZ - JIB_D / 2.0)))
    for sy in (-1, 1):
        metal.append(make_cylinder(f'JibPulley_{sy}', 0.28, 0.28, 0.14, 10,
                                   (CR_X + JIB_L - 0.40, CR_Y + sy * 0.32, JD + 0.30)))

    # ── ⑦ 平衡臂：同截面桁架 4 节 + 拉索 ───────────────────────────
    CZ = Z + 4.20                     # 上弦标高
    CD = CZ - 1.20
    for i in range(CJIB_N):
        x0 = -i * (CJIB_L / CJIB_N)
        x1 = -(i + 1) * (CJIB_L / CJIB_N)
        for sy in (-1, 1):
            jib_member(f'CJTop_{i}_{sy}', x0, x1, sy * 0.70, sy * 0.70, CZ, CZ, 0.14, yellow)
        jib_member(f'CJBot_{i}', x0, x1, 0.0, 0.0, CD, CD, 0.14, yellow)
        jib_member(f'CJV_{x1}', x1, x1, -0.70, 0.0, CZ, CD, 0.075, metal)
        jib_member(f'CJD_{i}', x0, x1, 0.0, 0.0, CD, CZ, 0.065, metal)
        jib_member(f'CJT_{x1}', x1, x1, -0.70, 0.70, CZ, CZ, 0.065, metal)
    yellow.append(make_box('CJTail', (1.20, 1.80, 1.60), (CR_X - CJIB_L, CR_Y, CZ - 0.60)))
    # 卷扬机 + 司机室操纵台
    metal.append(make_box('Winch', (1.60, 1.10, 0.90), (CR_X - 2.20, CR_Y, CD + 0.50)))

    # ── ⑧ 配重 6 块（叠 2 层 × 3 列，压在平衡臂尾部）────────────────
    # 3 列 × 2 层（真实配重就是「若干块同规格铸铁/混凝土块摞在平衡臂尾部」）
    for i in range(CW_N):
        layer, col = i // 3, i % 3
        weight.append(make_box(f'Counterweight_{i}', (CW_SX, CW_SY, CW_SZ),
                               (CR_X - CJIB_L + 1.10,
                                CR_Y + (col - 1) * (CW_SY + 0.05),
                                CD - CW_SZ / 2.0 - 0.03 - layer * (CW_SZ + 0.04))))

    # ── ⑨ 前拉索 ×2（塔帽顶→臂 1/3、2/3）+ 后拉索 ×2 ───────────────
    for k, fx in enumerate((JIB_L / 3.0, JIB_L * 2.0 / 3.0)):
        metal.append(make_strut(f'FrontStay_{k}', (CR_X, CR_Y, top - 0.30),
                                (CR_X + fx, CR_Y, JZ + 0.10), 0.07))
    for k, fx in enumerate((-CJIB_L * 0.45, -CJIB_L)):
        metal.append(make_strut(f'BackStay_{k}', (CR_X, CR_Y, top - 0.30),
                                (CR_X + fx, CR_Y, CZ + 0.10), 0.075))

    # ── ⑩ 变幅小车 + 钢缆 + 吊钩组 + 操作室 + 爬梯 ──────────────────
    tx = CR_X + TROLLEY_X
    metal.append(make_box('Trolley', (1.60, 1.80, 0.55), (tx, CR_Y, JD - 0.42)))
    for sy in (-1, 1):
        metal.append(make_cylinder(f'TrolleyWheel_{sy}', 0.22, 0.22, 0.12, 10,
                                   (tx + sy * 0.62, CR_Y, JD - 0.08)))
    # 4 根起升钢缆（从臂端滑轮到小车再到吊钩）
    hook_top = JD - 0.70 - HOOK_DROP
    for sy in (-1, 1):
        metal.append(make_strut(f'HoistRope_{sy}', (CR_X + JIB_L - 0.40, CR_Y + sy * 0.32, JD + 0.30),
                                (tx, CR_Y + sy * 0.14, JD - 0.70), 0.035))
        metal.append(make_strut(f'HoistFall_{sy}', (tx, CR_Y + sy * 0.14, JD - 0.70),
                                (tx, CR_Y + sy * 0.14, hook_top), 0.035))
    # 吊钩组：滑轮架 3 轮 + 钩体 + 防脱钩舌
    metal.append(make_box('HookBlock', (1.10, 0.55, 0.85), (tx, CR_Y, hook_top - 0.42)))
    for k, oy in enumerate((-0.18, 0.0, 0.18)):
        metal.append(make_cylinder(f'BlockSheave_{k}', 0.26, 0.26, 0.10, 10,
                                   (tx, CR_Y + oy, hook_top - 0.42)))
    metal.append(make_cylinder('HookShank', 0.10, 0.10, 0.55, 8, (tx, CR_Y, hook_top - 1.12)))
    metal.append(make_box('HookBody', (0.46, 0.30, 0.55), (tx, CR_Y, hook_top - 1.60)))
    warn.append(make_box('HookLatch', (0.34, 0.06, 0.20), (tx + 0.26, CR_Y, hook_top - 1.42)))
    # 操作室（挂在回转平台上、伸到臂侧）
    cabin.append(make_box('CraneCabin', (CABIN[0], CABIN[2], CABIN[1]),
                          (CR_X + 1.90, CR_Y - 1.30, Z + 1.10)))
    warn.append(make_box('CraneCabinGlass', (CABIN[0] - 0.14, CABIN[1] - 0.70, CABIN[2] - 0.10),
                         (CR_X + 1.90, CR_Y - 1.30, Z + 1.30)))
    metal.append(make_box('CraneCabinRoof', (CABIN[0] + 0.30, CABIN[1] + 0.10, CABIN[2] + 0.10),
                          (CR_X + 1.90, CR_Y - 1.30, Z + 2.20)))
    # 塔身爬梯（每节 1 段直梯 + 护笼环）
    for sy in (-1, 1):
        metal.append(make_strut(f'LadderRail_{sy}', (CR_X - 0.24, CR_Y + sy * 0.20, FOOT_H),
                                (CR_X - 0.24, CR_Y + sy * 0.20, FOOT_H + MAST_TOP), 0.05))
    for i in range(SEC_N):
        z0 = FOOT_H + i * SEC
        for k in range(2):
            metal.append(make_box(f'LadderRung_{i}_{k}', (0.05, 0.44, 0.05),
                                  (CR_X - 0.24, CR_Y, z0 + SEC * (0.25 + 0.5 * k))))
    # 塔机编号牌（挂在基础防护栏上）
    warn.append(make_box('CranePlate', (1.20, 0.06, 0.70),
                         (CR_X + 2.0, CR_Y - 4.25, 1.55)))

    # ── ⑪ 在建框架：3 层 × 3.60 m，4 柱 + 梁板 + 外脚手架 ──────────
    bx, byy = BLD_CX, BLD_CY
    col_xs = (bx - BLD_HX, bx - BLD_HX / 3.0, bx + BLD_HX / 3.0, bx + BLD_HX)
    col_ys = (byy - BLD_HY, byy + BLD_HY)
    for f in range(BLD_FLOORS + 1):
        z = GROUND_T + f * FLOOR_H
        for cx in col_xs:
            for cy in col_ys:
                concrete.append(make_box(f'Col_{f}_{cx}_{cy}',
                                         (COL_X, COL_Y, FLOOR_H),
                                         (cx, cy, z + FLOOR_H / 2.0)))
        # 每层楼板（顶层只做局部露台感：留一个 6×5 m 洞）
        if f < BLD_FLOORS:
            for cy in col_ys:
                concrete.append(make_box(f'Slab_{f}_{cy}', (BLD_HX * 2 + COL_X, BLD_HY - 1.4, 0.24),
                                         (bx, cy, z + FLOOR_H - 0.12)))
        # 层间梁
        for cy in col_ys:
            concrete.append(make_box(f'Beam_{f}_{cy}', (BLD_HX * 2, 0.35, 0.45),
                                     (bx, cy, z + FLOOR_H - 0.45)))
        for cx in col_xs:
            concrete.append(make_box(f'BeamY_{f}_{cx}', (0.35, BLD_HY * 2, 0.45),
                                     (cx, byy, z + FLOOR_H - 0.45)))
    # 楼梯间（现浇楼梯：斜板 + 休息平台）
    for f in range(BLD_FLOORS):
        z0 = GROUND_T + f * FLOOR_H
        concrete.append(make_strut(f'Stair_{f}',
                                   (bx - BLD_HX / 3.0 + 1.2, byy - BLD_HY + 0.5, z0 + 0.20),
                                   (bx - BLD_HX / 3.0 + 1.2, byy - BLD_HY + 5.2, z0 + FLOOR_H - 0.20),
                                   1.30, 0.22))
    # 外脚手架（沿 X 两面 + Y 两面，竖杆 @1.8 m + 水平杆 @1.8 m）
    for side in (-1, 1):
        yq = byy + side * (BLD_HY + 0.90)
        n = int(BLD_HX * 2 / 1.8) + 1
        for i in range(n):
            px = bx - BLD_HX + i * 1.8
            metal.append(make_strut(f'ScafV_{side}_{i}', (px, yq, GROUND_T),
                                    (px, yq, BLD_TOP + 1.60), 0.055))
        for k in range(1, 5):
            hz = GROUND_T + k * (FLOOR_H / 2.0)
            metal.append(make_strut(f'ScafH_{side}_{k}', (bx - BLD_HX, yq, hz),
                                    (bx + BLD_HX, yq, hz), 0.05))
        for k in range(1, 4):
            hz = GROUND_T + k * (FLOOR_H / 2.0) + FLOOR_H / 4.0
            metal.append(make_strut(f'ScafD_{side}_{k}', (bx - BLD_HX, yq, hz - 0.9),
                                    (bx + BLD_HX, yq, hz + 0.9), 0.045))
        xq = bx + side * (BLD_HX + 0.90)
        n2 = int(BLD_HY * 2 / 1.8) + 1
        for i in range(n2):
            py = byy - BLD_HY + i * 1.8
            metal.append(make_strut(f'ScafVy_{side}_{i}', (xq, py, GROUND_T),
                                    (xq, py, BLD_TOP + 1.60), 0.055))
        for k in range(1, 5):
            hz = GROUND_T + k * (FLOOR_H / 2.0)
            metal.append(make_strut(f'ScafHy_{side}_{k}', (xq, byy - BLD_HY, hz),
                                    (xq, byy + BLD_HY, hz), 0.05))
    # 安全网：⚠ **必须半透**。首版把它塞进 `warn`（不透明工程黄）⇒ 两片 18×10.4 m
    # 的黄板把整栋在建框架挡得一丝不露，出图判读时读成「工地里有两堵黄墙」。
    # 真实施工安全网是密目型**绿色网布**，能看见后面的立柱与楼板。
    for side in (-1, 1):
        net.append(make_box(f'Netting_{side}', (BLD_HX * 2, 0.04, BLD_TOP - 0.4),
                            (bx, byy + side * (BLD_HY + 0.94), GROUND_T + (BLD_TOP - 0.4) / 2.0)))
    for side in (-1, 1):
        net.append(make_box(f'NettingX_{side}', (0.04, BLD_HY * 2, BLD_TOP - 0.4),
                            (bx + side * (BLD_HX + 0.94), byy, GROUND_T + (BLD_TOP - 0.4) / 2.0)))

    # ── ⑫ 临建集装箱办公（2 组 × 2 层）+ 外挂楼梯 ───────────────────
    for g in range(2):
        gx = CABIN_X + g * (CABIN_L + 1.20)
        for lv in range(CABIN_STACK):
            gz = GROUND_T + lv * (CABIN_H + 0.06) + CABIN_H / 2.0
            cabin.append(make_box(f'OfficeCabin_{g}_{lv}', (CABIN_L, CABIN_W, CABIN_H),
                                  (gx, CABIN_Y, gz)))
            # 窗带 + 门
            for sy in (-1, 1):
                warn.append(make_box(f'CabinWin_{g}_{lv}_{sy}', (CABIN_L - 1.0, 0.05, 0.85),
                                     (gx, CABIN_Y + sy * (CABIN_W / 2.0 + 0.02), gz + 0.35)))
            metal.append(make_box(f'CabinDoor_{g}_{lv}', (0.90, 0.06, 2.00),
                                  (gx + CABIN_L / 2.0 - 0.70, CABIN_Y - CABIN_W / 2.0 - 0.02, GROUND_T + 1.05 + lv * (CABIN_H + 0.06))))
        # 外挂楼梯
        metal.append(make_strut(f'CabinStair_{g}',
                                (gx - CABIN_L / 2.0 - 1.6, CABIN_Y, GROUND_T + 0.10),
                                (gx - CABIN_L / 2.0 - 0.2, CABIN_Y, GROUND_T + CABIN_H + 0.06),
                                1.10, 0.18))
        # 上层栏杆
        for sy in (-1, 1):
            metal.append(make_strut(f'CabinRail_{g}_{sy}',
                                    (gx - CABIN_L / 2.0, CABIN_Y + sy * 0.60, GROUND_T + CABIN_H + 1.10),
                                    (gx + CABIN_L / 2.0, CABIN_Y + sy * 0.60, GROUND_T + CABIN_H + 1.10),
                                    0.05))

    # ── ⑬ 材料堆场：钢筋捆 + 砂石堆 + 模板垛 + 安全通道 ─────────────
    for i in range(2):
        for k in range(4):
            material.append(make_cylinder(f'Rebar_{i}_{k}', 0.28, 0.28, 9.0, 6,
                                          (REBAR_X, 4.0 + k * 0.62, GROUND_T + 0.30),
                                          rot=(0.0, math.radians(90.0), 0.0)))
        for k in range(3):
            material.append(make_cylinder(f'Rebar2_{i}_{k}', 0.28, 0.28, 9.0, 6,
                                          (REBAR_X, 4.0 + k * 0.62, GROUND_T + 0.86),
                                          rot=(0.0, math.radians(90.0), 0.0)))
    # 砂石堆（截锥）
    for i, (ax, ay, r, h) in enumerate(((AGG_X, -6.0, 3.20, 2.10), (AGG_X + 5.0, -6.0, 2.60, 1.70))):
        material.append(make_cylinder(f'Agg_{i}', r * 0.35, r, h, 12, (ax, ay, GROUND_T + h / 2.0)))
    # 模板垛（木方 + 覆膜板）
    for i in range(2):
        px = REBAR_X + 8.0
        for k in range(6):
            material.append(make_box(f'Formwork_{i}_{k}', (4.0, 2.4, 0.09),
                                     (px, -8.0 + i * 3.2, GROUND_T + 0.06 + k * 0.11)))
    # 安全通道（绿色防护棚廊，沿西侧）
    for i in range(4):
        py = 4.0 + i * 3.0
        metal.append(make_strut(f'CanopyPost_{i}', (-HX + 1.2, py, GROUND_T),
                                (-HX + 1.2, py, GROUND_T + 2.40), 0.07))
    cabin.append(make_box('SafetyCanopy', (2.20, 12.0, 0.10), (-HX + 1.2, 8.5, GROUND_T + 2.45)))
    # 洗轮机 + 料架
    material.append(make_box('ScaffoldStack', (3.0, 1.2, 1.6), (REBAR_X - 6.0, 8.0, GROUND_T + 0.80)))

    return [
        (earth, 'Site_Earth'),
        (hoard, 'Site_Hoard'),
        (yellow, 'Crane_Yellow'),
        (metal, 'Crane_Metal'),
        (weight, 'Crane_Weight'),
        (footing, 'Crane_Footing'),
        (concrete, 'Site_Concrete'),
        (cabin, 'Site_Cabin'),
        (material, 'Site_Material'),
        (warn, 'Site_Warn'),
        (net, 'Site_Net'),
    ]


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)
    groups = build()

    p = CITY_PALETTE
    mat = {
        'Site_Earth': make_material('Site_Earth_Mat', p['site_earth'], rough=0.98, metal=0.0),
        'Site_Hoard': make_material('Site_Hoard_Mat', p['hoarding_yellow'], rough=0.62, metal=0.05),
        'Crane_Yellow': make_material('Crane_Yellow_Mat', p['crane_yellow'], rough=0.58, metal=0.15),
        'Crane_Metal': make_material('Crane_Metal_Mat', p['crane_steel'], rough=0.44, metal=0.72),
        'Crane_Weight': make_material('Crane_Weight_Mat', p['crane_weight'], rough=0.90, metal=0.0),
        'Crane_Footing': make_material('Crane_Footing_Mat', p['concrete_dark'], rough=0.90, metal=0.0),
        'Site_Concrete': make_material('Site_Concrete_Mat', p['site_concrete'], rough=0.92, metal=0.0),
        'Site_Cabin': make_material('Site_Cabin_Mat', p['cabin_blue'], rough=0.55, metal=0.25),
        'Site_Material': make_material('Site_Material_Mat', p['site_rebar'], rough=0.80, metal=0.30),
        'Site_Warn': make_material('Site_Warn_Mat', p['warn_amber'], rough=0.60, metal=0.10),
        'Site_Net': make_material('Site_Net_Mat', p['site_netting'], rough=0.85, metal=0.0,
                                  alpha=0.34),
    }
    joined = []
    for objs, name in groups:
        if not objs:
            continue
        obj = join_objects(objs, name)
        assign_material(obj, mat[name])
        joined.append(obj)

    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 压进 §27.3 的 500 KB 硬线（本件无任何贴图 ⇒ UV 是纯死数据，剥掉省 ~25%）
    for obj in joined:
        a, b = weld_split_vertices(obj)
        if a != b:
            print(f'[weld] {obj.name}: {a} → {b} 顶点', flush=True)
        n_uv = strip_uvs(obj)
        if n_uv:
            print(f'[uv] {obj.name}: 剥 {n_uv} 层 UV → {len(obj.data.vertices)} 顶点', flush=True)

    PIN_TOLERANCE_M = 0.05
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z >= 0:
            continue
        if -min_z > PIN_TOLERANCE_M:
            print(f'[pin] ⚠ {obj.name} 最低点在 {min_z:.3f} m（超过容差），**不钉扎**', flush=True)
            continue
        obj.location.z = -0.1 * min_z

    _dx, _dy = center_content_xz(joined)

    bpy.context.view_layer.update()
    import mathutils  # noqa: PLC0415
    pts = [o.matrix_world @ mathutils.Vector(c) for o in joined for c in o.bound_box]
    lo = [min(p[i] for p in pts) for i in range(3)]
    hi = [max(p[i] for p in pts) for i in range(3)]
    print('[info] 工地包围盒（世界单位） '
          f'X {hi[0] - lo[0]:.2f} u｜Y {hi[1] - lo[1]:.2f} u｜高 {lo[2]:.3f}~{hi[2]:.3f} u'
          f'（{(hi[0] - lo[0]) * 10:.1f} × {(hi[1] - lo[1]) * 10:.1f} × {hi[2] * 10:.1f} m）', flush=True)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/construction_site.glb')
    export_glb(out_path)
    print(f'✅ construction_site.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
