#!/usr/bin/env python3
"""
build_substation — 批次 46「城市公用设施真实感」城区 10/35kV 配电站（civic/substation.glb）。

── 重制理由：批次 18-AA 的程序化变电站是玩具 ──────────────────────────
  现状 `civic/Substation.tsx` = 4 段 4×3 m、高 1.2 m 的「围栏」+ 2 个 ⌀1.0×1.2 m 圆筒
  「变压器」+ 2 根 ⌀0.1 m 电杆，总占地 **12 ㎡**、总高 8 m。真实城区 35kV 户外变电站
  常规占地 400~600 ㎡（紧凑布置 200~300 ㎡）、围墙 ≥2.2 m（GB 50059-2011 §2.0.5）——
  现状**体量差约 30 倍、围墙高度不合规**，且没有预制舱、没有散热器、没有出线构架。

── §27.0-1 真实形态调研（结论先行，逐条依据见方案 46 §2.1）──────────────
  | 部件 | 真实规格                                | 依据                          | 本件取值 |
  |------|------------------------------------------|-------------------------------|----------|
  | 站区 | 35kV 户外变电站 400~600 ㎡；紧凑 200~300 ㎡ | 35kV 变电站占地解密           | 18.0×14.0 = 252 ㎡（紧凑型）|
  | 围墙 | 屋外变电站**实体围墙** ≥2.2 m（国网运维宜 ≥2.3 m）| GB 50059-2011 §2.0.5     | 2.45 m（墙裙 0.45+墙板 1.80+压顶 0.20）|
  | 墙板 | 装配式预制墙板，壁柱间距 ≈ 一板宽 3.0 m       | 装配式围墙通用做法             | 壁柱 0.34×0.27，@3.0 m |
  | 箱变舱| 35kV 预制舱式约 6000×2500×3000 mm        | 预制舱厂家标准型              | 6.00×2.50×3.00（2 台）|
  | 主变 | 630~800 kVA 油浸 3000×2000×2500           | 国网欧式箱变分档表            | 2400×1900×2200 + 两侧 8 片散热器 |
  | 距围栏| 变压器外廓距围栏 ≥0.8 m；底部距地 ≥0.3 m  | GB 50053-2013 §4.2.2         | 1.6 m / 0.35 m 基座 |
  | 车道 | 消防车道净宽 4.0 m                       | GB 50059 §2.0.6              | 4.0 m 环形检修道 |
  | 电杆 | 10kV 架空线水泥杆 ⌀0.30，杆高 10~12 m     | 10kV 配网常用杆型             | ⌀0.30→⌀0.24 渐收，12.0 m，三层横担 |

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**、米制；导出自动 Yup ⇒ three.js 直立。
  · 建筑正面（大门朝向）= Blender **+Y** = three.js **-Z**（全批统一口径）。
  · 节点 identity（尺寸烘进顶点）；minY = 0（接地圆墩/杆脚最低点）。
  · 导出前 center_content_xz()：本作的「围墙方院 + 外挑电杆」组合天然不对称
    （沿 Y 多出 5.4 m 的杆+线段），必须按 §27.3 把内容盒在水平两轴居中。

── 材质槽（7，按材质分桶 ⇒ 7 个 primitive/DC）──────────────────────────
  Substation_Concrete / _ConcreteDark / _Steel / _SteelDark / _Vent /
  _Sign / _Rust

用法：
  blender --background --python build_substation.py -- \
    ClientWeb/src/assets/models/civic/substation.glb
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_taper,
    make_material, assign_material, join_objects,
    export_glb, center_content_xz, CITY_PALETTE,
)

# ── 尺寸（米）────────────────────────────────────────────────────────────
WALL_X, WALL_Y = 18.0, 14.0        # 围墙外轮廓（中心线）
SKIRT_H = 0.45                   # 墙裙高
WALL_TOP = 2.25                  # 墙板顶 = 压顶底
WALL_BOT = SKIRT_H              # 墙板底（= 墙裙顶）
WALL_PANEL_H = WALL_TOP - WALL_BOT  # 1.80
CAP_TOP = 2.45                     # 压顶顶面（围墙总高）
POST_SPACING = 3.0                  # 壁柱间距（兼作预制墙板分格缝）
GATE_W = 4.0                       # 大门净宽（+Y 侧居中）

CABIN_L, CABIN_D, CABIN_H = 6.00, 2.50, 3.00   # 预制舱 长(X)/深(Y)/高(Z)
CABIN_BASE = 0.30
CABIN_X = (-4.6, 4.6)              # 两台舱的 X 中心
CABIN_Y = 3.0                      # 舱中心 Y（+Y 侧；距 +Y 围墙 2.75 m）

XFMR_L, XFMR_D, XFMR_H = 2.40, 1.90, 2.20       # 主变油箱
XFMR_BASE = 0.35
XFMR_Y = -2.0
FIN_N, FIN_T = 8, 0.10             # 每侧散热器片数 / 片厚

SW_STAND_H = 4.50                  # 隔离开关构架杆高
SW_X = 1.20                        # 构架立杆半跨
SW_Y = -5.0
SW_ARM_Z = (3.00, 3.80)            # 两层横担标高

POLE_X = 5.0                       # 出线电杆 X（±）
POLE_Y = -9.5                      # 出线电杆 Y（围墙 -Y 侧外 2.5 m）
POLE_H = 12.0
POLE_R_B, POLE_R_T = 0.15, 0.12
POLE_ARM_Z = (9.00, 10.20, 11.40)  # 三层横担
POLE_WIRE_LEN = 3.0                # 自杆向北（-Y）引出的短段档距
ROAD_W = 4.0                       # 消防车道净宽


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)

    concrete, concrete_dark = [], []
    steel, steel_dark = [], []
    vent, sign, rust = [], [], []

    # ── ① 围墙：装配式实体围墙（墙裙 + 墙板 + 壁柱 + 压顶）────────────────
    #   依据 GB 50059-2011 §2.0.5「屋外变电站实体围墙不低于 2.2 m」——
    #   实体墙（而非栅栏）才是城郊/城区配电站的通行做法；壁柱每 3.0 m 一根
    #   兼作预制墙板的分格缝，避免远景下退化成一块空白板。
    hx, hy = WALL_X / 2.0, WALL_Y / 2.0
    cap_h = CAP_TOP - WALL_TOP
    wall_t, pil_w, pil_d = 0.20, 0.34, 0.27

    def wall_run(tag, axis, length, fixed, skip=()):
        """沿一条边生成 墙裙 + 墙板 + 压顶 三段（axis='x' 表示该边沿 X 延展）。"""
        sgn = 1.0 if fixed > 0 else -1.0
        for suffix, w, h, zc, bucket in (
            ('Skirt', wall_t + 0.06, SKIRT_H, SKIRT_H / 2.0, concrete_dark),
            ('Panel', wall_t, WALL_PANEL_H, (WALL_BOT + WALL_TOP) / 2.0, concrete),
            ('Cap', wall_t + 0.14, cap_h, WALL_TOP + cap_h / 2.0, concrete_dark),
        ):
            if axis == 'x':
                bucket.append(make_box(f'Wall{tag}_{suffix}', (length, w, h), (0, fixed, zc)))
            else:
                bucket.append(make_box(f'Wall{tag}_{suffix}', (w, length, h), (fixed, 0, zc)))
        # 壁柱（贯通全高，向两侧各出挑 0.035）
        n = int(length / POST_SPACING)
        for i in range(n + 1):
            t = -length / 2.0 + POST_SPACING * i
            if t in skip:
                continue
            if axis == 'x':
                concrete.append(make_box(f'Pil{tag}_{i}', (pil_w, pil_d, CAP_TOP),
                                         (t, fixed - sgn * 0.035, CAP_TOP / 2.0)))
            else:
                concrete.append(make_box(f'Pil{tag}_{i}', (pil_d, pil_w, CAP_TOP),
                                         (fixed - sgn * 0.035, t, CAP_TOP / 2.0)))

    seg_x = (WALL_X - GATE_W) / 2.0
    wall_run('NY', 'x', WALL_X, -hy)
    wall_run('PX', 'y', WALL_Y, hx)
    wall_run('NX', 'y', WALL_Y, -hx)
    # +Y 侧被大门切成两段
    for sx in (-1, 1):
        cx = sx * (GATE_W / 2.0 + seg_x / 2.0)
        for suffix, w, h, zc in (
            ('Skirt', wall_t + 0.06, SKIRT_H, SKIRT_H / 2.0),
            ('Panel', wall_t, WALL_PANEL_H, (WALL_BOT + WALL_TOP) / 2.0),
            ('Cap', wall_t + 0.14, cap_h, WALL_TOP + cap_h / 2.0),
        ):
            (concrete if suffix == 'Panel' else concrete_dark).append(
                make_box(f'WallPY{sx}_{suffix}', (seg_x, w, h), (cx, hy, zc)))
        n = int(seg_x / POST_SPACING)
        for i in range(n + 1):
            t = cx - seg_x / 2.0 + POST_SPACING * i
            concrete.append(make_box(f'PilPY{sx}_{i}', (pil_w, pil_d, CAP_TOP),
                                     (t, hy - 0.035, CAP_TOP / 2.0)))

    # ── ② 大门（+Y 侧，2 扇对开铁门 + 门柱 + 门楣）──────────────────────
    for sx in (-1, 1):
        concrete.append(make_box(f'GatePier_{sx}', (0.25, 0.25, CAP_TOP),
                                 (sx * (GATE_W / 2.0 + 0.12), hy, CAP_TOP / 2.0)))
        # 门扇 2.0 × 0.05 × 2.20，铰链在门柱侧，向内微开 12°
        leaf = make_box(f'GateLeaf_{sx}', (GATE_W / 2.0 - 0.06, 0.05, 2.20),
                        (sx * (GATE_W / 4.0), hy - 0.20, 1.10))
        leaf.rotation_euler = (0, 0, sx * 0.21)   # 半开姿态，避免"贴墙平板"剪影
        steel_dark.append(leaf)
    concrete.append(make_box('GateLintel', (GATE_W + 0.7, 0.30, 0.25),
                             (0, hy, CAP_TOP - 0.125)))

    # ── ③ 预制箱变舱 ×2 ───────────────────────────────────────────────
    for ci, cx in enumerate(CABIN_X):
        concrete_dark.append(make_box(f'CabinBase_{ci}', (CABIN_L + 0.6, CABIN_D + 0.6, CABIN_BASE),
                                      (cx, CABIN_Y, CABIN_BASE / 2.0)))
        # 墙板（勒脚 + 主体）
        concrete_dark.append(make_box(f'CabinPlinth_{ci}', (CABIN_L + 0.1, CABIN_D + 0.1, 0.25),
                                      (cx, CABIN_Y, CABIN_BASE + 0.125)))
        concrete.append(make_box(f'CabinBody_{ci}', (CABIN_L, CABIN_D, CABIN_H - 0.25),
                                 (cx, CABIN_Y, CABIN_BASE + 0.25 + (CABIN_H - 0.25) / 2.0)))
        # 单坡金属屋面（3% 排水坡，绕 X 轴负向倾 1.7°）
        roof = make_box(f'CabinRoof_{ci}', (CABIN_L + 0.3, CABIN_D + 0.3, 0.16),
                        (cx, CABIN_Y, CABIN_BASE + CABIN_H + 0.06))
        roof.rotation_euler = (-0.030, 0, 0)
        steel.append(roof)
        # 门（-Y 面）+ 百叶通风窗 ×2
        steel_dark.append(make_box(f'CabinDoor_{ci}', (1.00, 0.06, 2.10),
                                   (cx - 1.6, CABIN_Y - CABIN_D / 2.0 - 0.02, CABIN_BASE + 1.05)))
        for k, dx in enumerate((0.9, 2.2)):
            vent.append(make_box(f'CabinVent_{ci}_{k}', (1.00, 0.05, 0.70),
                                 (cx + dx, CABIN_Y - CABIN_D / 2.0 - 0.02, CABIN_BASE + 1.95)))
        # 屋面穿墙套管 ×3
        for k, dy in enumerate((-0.7, 0.0, 0.7)):
            rust.append(make_cylinder(f'CabinBushing_{ci}_{k}', 0.085, 0.085, 0.45, 8,
                                      (cx + 1.8, CABIN_Y + dy, CABIN_BASE + CABIN_H + 0.34)))
        # 侧向电缆桥架（落到地面）
        steel_dark.append(make_box(f'CabinTray_{ci}', (0.35, 0.25, CABIN_H * 0.6),
                                   (cx + CABIN_L / 2.0 + 0.20, CABIN_Y, CABIN_BASE + CABIN_H * 0.3)))

    # ── ④ 主变 ×1（油箱 + 双侧散热器 + 套管 + 控制箱）────────────────────
    bz = XFMR_BASE
    concrete_dark.append(make_box('XfmrBase', (XFMR_L + 0.6, XFMR_D + 0.5, XFMR_BASE),
                                  (0, XFMR_Y, XFMR_BASE / 2.0)))
    steel_dark.append(make_box('XfmrTank', (XFMR_L, XFMR_D, XFMR_H),
                               (0, XFMR_Y, bz + XFMR_H / 2.0)))
    # 顶盖凸缘
    steel_dark.append(make_box('XfmrLid', (XFMR_L + 0.16, XFMR_D + 0.16, 0.12),
                               (0, XFMR_Y, bz + XFMR_H + 0.06)))
    fin_span = (XFMR_L - 0.3) / (FIN_N - 1)
    for sy in (-1, 1):
        for i in range(FIN_N):
            steel.append(make_box(f'XfmrFin_{sy}_{i}', (FIN_T, 0.34, 1.40),
                                  (-(XFMR_L - 0.3) / 2.0 + fin_span * i,
                                   XFMR_Y + sy * (XFMR_D / 2.0 + 0.17),
                                   bz + 1.05)))
        # 散热器集管
        steel.append(make_cylinder(f'XfmrHeader_{sy}', 0.07, 0.07, XFMR_L - 0.2, 8,
                                   (0, XFMR_Y + sy * (XFMR_D / 2.0 + 0.17), bz + 1.78),
                                   rot=(0, math.pi / 2, 0)))
    # 高压套管 ×3
    for i, dx in enumerate((-0.7, 0.0, 0.7)):
        rust.append(make_taper(f'XfmrBushing_{i}', 0.115, 0.075, 0.55, 8,
                               (dx, XFMR_Y, bz + XFMR_H + 0.34)))
    # 控制箱（站用变附件）
    concrete.append(make_box('XfmrCtrl', (0.55, 0.40, 0.85),
                             (XFMR_L / 2.0 + 0.55, XFMR_Y, bz + 0.43)))

    # ── ⑤ 隔离开关构架（2 立杆 + 2 层横担 + 6 绝缘子串）──────────────────
    for sx in (-1, 1):
        steel_dark.append(make_cylinder(f'SwLeg_{sx}', 0.06, 0.06, SW_STAND_H, 8,
                                        (sx * SW_X, SW_Y, SW_STAND_H / 2.0)))
        concrete_dark.append(make_box(f'SwFoot_{sx}', (0.45, 0.45, 0.25),
                                      (sx * SW_X, SW_Y, 0.125)))
    for zi, az in enumerate(SW_ARM_Z):
        steel.append(make_box(f'SwArm_{zi}', (SW_X * 2.0 + 0.5, 0.09, 0.09),
                              (0, SW_Y, az)))
        for k, dx in enumerate((-0.75, 0.0, 0.75)):
            vent.append(make_cylinder(f'SwInsul_{zi}_{k}', 0.055, 0.055, 0.32, 6,
                                      (dx, SW_Y, az - 0.20)))

    # ── ⑥ 出线电杆 ×2（三层横担 + 9 绝缘子 + 3 相引下线）─────────────────
    for sx in (-1, 1):
        px = sx * POLE_X
        concrete_dark.append(make_box(f'PoleFoot_{sx}', (0.9, 0.9, 0.40), (px, POLE_Y, 0.20)))
        steel_dark.append(make_taper(f'Pole_{sx}', POLE_R_B, POLE_R_T, POLE_H, 10,
                                     (px, POLE_Y, 0.40 + POLE_H / 2.0)))
        for zi, az in enumerate(POLE_ARM_Z):
            steel.append(make_box(f'PoleArm_{sx}_{zi}', (1.8, 0.09, 0.09), (px, POLE_Y, az)))
            for k, dx in enumerate((-0.6, 0.0, 0.6)):
                vent.append(make_cylinder(f'PoleInsul_{sx}_{zi}_{k}', 0.05, 0.05, 0.26, 6,
                                          (px + dx, POLE_Y, az - 0.17)))
            # 3 相引下线：自横担垂下后向 -Y 伸出（薄板近似导线，30 mm 直径）
            steel_dark.append(make_box(f'PoleStay_{sx}_{zi}', (0.05, POLE_WIRE_LEN, 0.05),
                                       (px + dx, POLE_Y - POLE_WIRE_LEN / 2.0, az - 0.40)))

    # ── ⑦ 站内：4.0 m 消防车道 + 警示灯箱 ×2 + 铭牌 ───────────────────────
    concrete.append(make_box('RoadNS', (ROAD_W, WALL_Y - 0.5, 0.10),
                             (0, 0.0, 0.05)))
    concrete.append(make_box('RoadEW', (WALL_X - 0.5, ROAD_W, 0.10),
                             (0, 0.0, 0.09)))
    for sx in (-1, 1):
        sign.append(make_box(f'WarnBox_{sx}', (0.42, 0.14, 0.60),
                             (sx * 3.2, hy - 0.22, 1.55)))
        steel_dark.append(make_box(f'WarnPost_{sx}', (0.07, 0.07, 1.25),
                                   (sx * 3.2, hy - 0.22, 0.62)))
    steel.append(make_box('NamePlate', (1.10, 0.04, 0.35), (0.0, hy - 0.14, 2.05)))

    # ── ⑧ 按材质合并 ⇒ 7 primitive ───────────────────────────────────
    mat_c = make_material('Substation_Concrete_Mat', CITY_PALETTE['concrete'], rough=0.86, metal=0.0)
    mat_cd = make_material('Substation_ConcreteDark_Mat', CITY_PALETTE['concrete_dark'],
                           rough=0.88, metal=0.0)
    mat_s = make_material('Substation_Steel_Mat', CITY_PALETTE['steel'], rough=0.46, metal=0.75)
    mat_sd = make_material('Substation_SteelDark_Mat', CITY_PALETTE['steel_dark'],
                           rough=0.52, metal=0.70)
    mat_v = make_material('Substation_Vent_Mat', CITY_PALETTE['vent_louver'], rough=0.68, metal=0.30)
    mat_g = make_material('Substation_Sign_Mat', CITY_PALETTE['warn_amber'],
                          rough=0.42, metal=0.10, emissive=CITY_PALETTE['warn_amber'],
                          emissive_intensity=0.6)
    mat_r = make_material('Substation_Rust_Mat', CITY_PALETTE['rust'], rough=0.78, metal=0.45)
    groups = [
        (concrete, mat_c, 'Substation_Concrete'),
        (concrete_dark, mat_cd, 'Substation_ConcreteDark'),
        (steel, mat_s, 'Substation_Steel'),
        (steel_dark, mat_sd, 'Substation_SteelDark'),
        (vent, mat_v, 'Substation_Vent'),
        (sign, mat_g, 'Substation_Sign'),
        (rust, mat_r, 'Substation_Rust'),
    ]
    joined = [join_objects(objs, name) for objs, _m, name in groups]
    for (_objs, mat, _name), obj in zip(groups, joined):
        assign_material(obj, mat)

    # ⚠ 全件米 → 世界单位（×0.1）。顺序关键：join 保留**首个对象**的 location/scale
    #   变换（其余件烘进顶点），必须先 transform_apply flatten 再统一 ×0.1
    #   （见 build_park_pavilion 注释与 bake_transforms docstring 的三起事故）。
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 贴地钉扎：门柱/杆脚/基座的最低点应为 0（建模已保证），保险起见逐对象抬升。
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z < 0:
            obj.location.z = -0.1 * min_z

    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/substation.glb')
    export_glb(out_path)
    print(f'✅ substation.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
