#!/usr/bin/env python3
"""
build_bus_stop — 批次 42「街具真实感」公交候车亭（bus_stop.glb）Blender 导出脚本。

── §27.0-1 真实结构调研（开工前先检索参考；本次 WebSearch 无可用返回，
   改用工程标准值 + 仓库既有资产口径，结论如下）────────────────────────
  · 城市公交候车亭常规规格（参考：CJJ/T 《城市道路公共交通站、场、厂工程设计规范》
    候车亭通用图、北京/上海/深圳街头常见不锈钢候车亭实测口径）：
    —— 顶棚离地 2.5~2.8 m（净高 2.3 m+），出檐 0.3~0.5 m，2~3° 排水微坡；
    —— 立柱 ⌀76~114 mm 镀锌钢管/不锈钢，2~4 根，顶部法兰与棚顶栓接；
    —— 背板/侧板 8~12 mm 钢化玻璃，高 2.0~2.2 m，金属框收边；
    —— 座椅高 0.42~0.45 m、深 0.40~0.45 m，铝板/防腐木条 3~5 横档；
    —— 站牌/线路图灯箱宽 0.25~0.35 m、高 1.2~1.8 m，顶齐 2.2~2.5 m，夜间内打光。
  · 本件取值（长 × 高 × 深 = 5.00 × 2.70 × 1.80 m）：
    棚顶 5.00×1.80×0.12 @ z=2.64（微坡 2° 经顶面双段错高表达，肉眼可辨）；
    檐口色带（bus_teal）高 0.14；立柱 ⌀0.09 ×3 根（后沿 y=+0.72）；
    背玻璃 4.60×2.20（z 0.30~2.50）+ 侧玻璃 1.50×2.20 ×2（alpha 0.35）；
    铝条座椅 2.20×0.42（面高 0.45，3 横档 + 2 腿）；
    站牌灯箱 0.35×1.50（z 0.75~2.25，emissive 琥珀，材质名含 Lightbox 供昼夜调制）；
    棚底灯带 4.20×0.15×0.04（emissive 同灯箱材质）。
  · 材质：CITY_PALETTE（steel / bus_teal / glass_panel / bench_alu / sign_amber），
    weathered_pbr 经年磨损（glTF 不导出程序化噪声，导出前 flatten 回常量色）。

── 坐标与尺度规约（CLAUDE.md §27.3，全目录统一）─────────────────────────
  · Blender 原生 **Z-up**：X = 亭长（沿道路方向），Y = 进深（+Y 背板侧 / −Y 街道侧），
    Z = 高度（地面 z = 0）。导出 glTF 后 X = 亭长、Y = 高（minY=0）、Z = 进深。
  · 世界单位：**1 单位 = 10 m** ⇒ 全部尺寸 ×U=0.1 后写入（cityScale.METERS_PER_UNIT）。
  · 节点变换 identity —— 尺寸一律 bake 进顶点（export_glb 内置 bake_transforms）。
  · 原点：minY = 0 贴地、X/Z 居中。
  · 材质名契约：灯箱材质名含 **Lightbox**（前端 BusStop 夜间 emissive 调制按名命中）。

── 交付尺寸（世界单位 / 真实米）──────────────────────────────────────────
  x = 0.500（5.00 m 亭长）；y = 0.270（2.70 m 总高）；z = 0.180（1.80 m 进深）。
  对应前端 cityScale.REAL_DIMS_M.busStop（批次 42 §3.1）。

用法：
  blender --background --python build_bus_stop.py -- \
    ClientWeb/src/assets/models/road/bus_stop.glb
"""
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_material, assign_material,
    join_objects, export_glb, weathered_pbr, shared_weathered,
    flatten_weathered_materials, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位（1 u = 10 m）

# ── 亭体尺寸（米）────────────────────────────────────────────────────────
LEN = 5.00         # 亭长（X）
DEP = 1.80         # 进深（Y）
CANOPY_T = 0.12    # 棚顶厚
CANOPY_Z = 2.64    # 棚顶中心高（顶面 2.70 = 交付总高）
FASCIA_H = 0.14    # 檐口色带高
POLE_R = 0.045     # 立柱半径（⌀0.09）
POLE_Y = 0.72      # 立柱中线（后沿）
GLASS_BACK_Y = 0.78
GLASS_H = 2.20     # 玻璃高（z 0.30~2.50）
GLASS_Z0 = 0.30
BENCH_H = 0.45     # 座面高
BENCH_D = 0.42     # 座面深
BENCH_LEN = 2.20
BENCH_Y = 0.35     # 座面中线（贴背板一侧）
SLAT_T = 0.05
SIGN_W = 0.35      # 站牌灯箱宽
SIGN_T = 0.12      # 灯箱厚
SIGN_Z0 = 0.75     # 灯箱底
SIGN_H = 1.50      # 灯箱高（顶 2.25）
SIGN_X = -2.15     # 灯箱（亭左端内侧）
SIGN_Y = 0.55
STRIP_L = 4.20     # 棚底灯带
STRIP_T = 0.04


def _m(v):
    return v * U


def build_bus_stop() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()
    objs = []

    steel = CITY_PALETTE['steel']
    teal = CITY_PALETTE['bus_teal']
    glass = CITY_PALETTE['glass_panel']
    alu = CITY_PALETTE['bench_alu']
    amber = CITY_PALETTE['sign_amber']

    # ── 棚顶（两段错高 2° 微坡：后段高、前段低，排水坡肉眼可辨）──────────
    # 后段（背板侧）：y ∈ [0, 0.9]，顶 2.70；前段（街道侧）：y ∈ [-0.9, 0]，顶 2.67
    roof_back = make_box('Roof_Back',
                         (_m(LEN), _m(DEP / 2), _m(CANOPY_T)),
                         (0, _m(DEP / 4), _m(CANOPY_Z)))
    objs.append(roof_back)
    roof_front = make_box('Roof_Front',
                          (_m(LEN), _m(DEP / 2), _m(CANOPY_T)),
                          (0, _m(-DEP / 4), _m(CANOPY_Z - 0.015)))
    objs.append(roof_front)
    # 材质组①顶棚（批次 42 材质槽收口：同语义共用 1 槽，见文件头 DC 注）
    shared_weathered([roof_back, roof_front], steel, rough=0.55, metal=0.55,
                     name='BusStop_Roof_Mat', wear=0.30, scale=3.0)

    # ── 檐口色带（前缘 + 左右端；青绿识别带）────────────────────────────
    fascia_front = make_box('Fascia_Front',
                            (_m(LEN), _m(0.10), _m(FASCIA_H)),
                            (0, _m(-DEP / 2 + 0.05), _m(CANOPY_Z - CANOPY_T / 2 - FASCIA_H / 2 - 0.015)))
    objs.append(fascia_front)
    fascia_ends = []
    for side, tag in ((-1, 'L'), (1, 'R')):
        fascia_end = make_box(f'Fascia_{tag}',
                              (_m(0.10), _m(DEP - 0.10), _m(FASCIA_H)),
                              (_m(side * (LEN / 2 - 0.05)), 0,
                               _m(CANOPY_Z - CANOPY_T / 2 - FASCIA_H / 2 - 0.015)))
        objs.append(fascia_end)
        fascia_ends.append(fascia_end)
    # 材质组②檐口色带
    shared_weathered([fascia_front] + fascia_ends, teal, rough=0.45, metal=0.25,
                     name='BusStop_Fascia_Mat', wear=0.35, scale=5.0)

    # ── 立柱 ×3（后沿 y=+0.72，x = -1.9 / 0 / +1.9）──────────────────────
    # ⚠ make_cylinder 的 r/h 是**世界单位**（与位置同域），必须 _m() —— 漏乘会做出 25.8 m 巨柱
    pole_h = CANOPY_Z - CANOPY_T / 2
    poles = []
    for i, px in enumerate((-1.9, 0.0, 1.9)):
        pole = make_cylinder(f'Pole{i}', _m(POLE_R), _m(POLE_R), _m(pole_h), 12,
                             (_m(px), _m(POLE_Y), _m(pole_h / 2)))
        objs.append(pole)
        poles.append(pole)

    # ── 背玻璃 + 侧玻璃（alpha 0.35）+ 金属框收边 ────────────────────────
    glass_mat = make_material('BusStop_Glass_Mat', glass, rough=0.12, metal=0.0, alpha=0.35)
    # 材质组③结构钢：立柱 + 框 + 灯箱框/杆 共用 1 槽
    #（先建后取 —— v2 曾在 shared_weathered 之前 get() 拿到 None）
    shared_weathered(poles, steel, rough=0.42, metal=0.85,
                     name='BusStop_Struct_Mat', wear=0.25, scale=8.0)
    frame_mat = bpy.data.materials['BusStop_Struct_Mat']

    back = make_box('Glass_Back',
                    (_m(4.60), _m(0.02), _m(GLASS_H)),
                    (0, _m(GLASS_BACK_Y), _m(GLASS_Z0 + GLASS_H / 2)))
    assign_material(back, glass_mat)
    objs.append(back)
    # 背板框：上下横档 + 两侧竖档
    for tag, sz, ps in (
        ('Frame_Back_T', (4.64, 0.05, 0.06), (0, GLASS_BACK_Y, GLASS_Z0 + GLASS_H + 0.03)),
        ('Frame_Back_B', (4.64, 0.05, 0.06), (0, GLASS_BACK_Y, GLASS_Z0 - 0.03)),
    ):
        fr = make_box(tag, (_m(sz[0]), _m(sz[1]), _m(sz[2])),
                      (_m(ps[0]), _m(ps[1]), _m(ps[2])))
        assign_material(fr, frame_mat)
        objs.append(fr)

    for side, tag in ((-1, 'L'), (1, 'R')):
        sg = make_box(f'Glass_{tag}',
                      (_m(0.02), _m(1.50), _m(GLASS_H)),
                      (_m(side * (LEN / 2 - 0.10)), _m(0.05), _m(GLASS_Z0 + GLASS_H / 2)))
        assign_material(sg, glass_mat)
        objs.append(sg)
        sf = make_box(f'Frame_{tag}',
                      (_m(0.05), _m(1.54), _m(0.06)),
                      (_m(side * (LEN / 2 - 0.10)), _m(0.05), _m(GLASS_Z0 + GLASS_H + 0.03)))
        assign_material(sf, frame_mat)
        objs.append(sf)

    # ── 铝条座椅（3 横档 + 2 腿 + 靠背 2 档）────────────────────────────
    bench_mat = make_material('BusStop_Bench_Mat', alu, rough=0.48, metal=0.60)
    for i in range(3):
        sy = BENCH_Y - BENCH_D / 2 + BENCH_D * (i + 0.5) / 3
        slat = make_box(f'Bench_Slat{i}',
                        (_m(BENCH_LEN), _m(BENCH_D / 3 - 0.02), _m(SLAT_T)),
                        (0, _m(sy), _m(BENCH_H - SLAT_T / 2)))
        assign_material(slat, bench_mat)
        objs.append(slat)
    for i in range(2):
        by = BENCH_Y - BENCH_D / 2 + BENCH_D * (i + 0.5) / 2
        for sx in (-BENCH_LEN / 2 + 0.12, BENCH_LEN / 2 - 0.12):
            leg = make_box(f'Bench_Leg{i}_{sx:+.1f}',
                           (_m(0.05), _m(0.05), _m(BENCH_H - SLAT_T)),
                           (_m(sx), _m(by), _m((BENCH_H - SLAT_T) / 2)))
            assign_material(leg, bench_mat)
            objs.append(leg)
    for i in range(2):
        ry = GLASS_BACK_Y - 0.10
        rz = BENCH_H + 0.12 + i * 0.16
        rail = make_box(f'Bench_Rail{i}',
                        (_m(BENCH_LEN), _m(0.04), _m(0.06)),
                        (0, _m(ry), _m(rz)))
        assign_material(rail, bench_mat)
        objs.append(rail)
        for sx in (-BENCH_LEN / 2 + 0.12, BENCH_LEN / 2 - 0.12):
            stile = make_box(f'Bench_Stile{i}_{sx:+.1f}',
                             (_m(0.05), _m(0.05), _m(0.34)),
                             (_m(sx), _m(ry), _m(BENCH_H + 0.22)))
            assign_material(stile, bench_mat)
            objs.append(stile)

    # ── 站牌/线路图灯箱（材质名含 Lightbox）+ 棚底灯带 ──────────────────
    light_mat = make_material('BusStop_Lightbox_Mat', amber, rough=0.40, metal=0.05,
                              emissive=amber, emissive_intensity=1.1)
    sign_box = make_box('Lightbox_Panel',
                        (_m(SIGN_W), _m(SIGN_T), _m(SIGN_H)),
                        (_m(SIGN_X), _m(SIGN_Y), _m(SIGN_Z0 + SIGN_H / 2)))
    assign_material(sign_box, light_mat)
    objs.append(sign_box)
    # 灯箱框 + 立杆
    sign_frame = make_box('Lightbox_Frame',
                          (_m(SIGN_W + 0.05), _m(SIGN_T + 0.03), _m(0.05)),
                          (_m(SIGN_X), _m(SIGN_Y), _m(SIGN_Z0 + SIGN_H + 0.025)))
    assign_material(sign_frame, frame_mat)
    objs.append(sign_frame)
    sign_pole = make_cylinder('Lightbox_Pole', _m(0.03), _m(0.03), _m(SIGN_Z0), 10,
                              (_m(SIGN_X), _m(SIGN_Y), _m(SIGN_Z0 / 2)))
    assign_material(sign_pole, frame_mat)
    objs.append(sign_pole)

    strip = make_box('Lightbox_Strip',
                     (_m(STRIP_L), _m(0.15), _m(STRIP_T)),
                     (0, _m(-0.15), _m(CANOPY_Z - CANOPY_T / 2 - STRIP_T / 2 - 0.005)))
    assign_material(strip, light_mat)
    objs.append(strip)

    joined = join_objects(objs, 'BusStop')
    return joined


if __name__ == '__main__':
    obj = build_bus_stop()
    # 导出前把 weathered_pbr 节点链压回常量色（否则 GLB 白模，见 flatten 文档）
    flatten_weathered_materials(obj)
    # 显式烘焙（与 export_glb 内置 bake 双保险）
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/bus_stop.glb'
    export_glb(out_path)
    print(f'[build_bus_stop] done -> {out_path}', flush=True)
