#!/usr/bin/env python3
"""
build_bike_rack — 批次 42「街具真实感」倒 U 停车架 + 斜靠自行车（bike_rack.glb）。

── §27.0-1 真实结构调研（开工前先检索参考；本次 WebSearch 无可用返回，
   改用工程标准值 + 仓库既有资产口径，结论如下）────────────────────────
  · 自行车停放架（参考：城市人行道不锈钢倒 U 架通用图、共享单车停放点实测）：
    —— 倒 U 架高 0.75~0.85 m、跨距 0.80~0.90 m、⌀32~38 mm 不锈钢圆管；
    —— 两架沿道路向（X）并排，中心距 0.85~0.90 m；地面膨胀螺栓圆盘。
  · 26″ 城市自行车（参考：国标通勤车外形尺寸 GB/T 17118 口径 + 常见实测）：
    —— 全长 1.75 m、全高 1.05~1.10 m（把高）；轮径 0.66 m（26″）、胎冠宽 0.035 m；
    —— 车架三角桁架管 ⌀0.025~0.032 m；五通高 0.28 m；座高 0.88 m；把宽 0.55 m；
    —— 细节：辐条 12 根/轮、曲柄 + 脚踏、把立 + 车把、座垫、前后挡泥板、链罩、脚撑。
  · 本件取值（宽 × 高 × 深 = 1.80 × 1.10 × 0.62 m）：
    **布局**：两倒 U 架沿 X 并排（中心 x=±0.44，跨距沿 X = 0.85），
    自行车长边沿 X 停放于架前、向架侧（+Y）倾 15°、脚撑撑地。
    X = 架外缘 1.81（≈车长 1.75 同量级）；Y = 把高倾角投影 1.10；Z = 车宽 + 倾角包络 0.62。
  · 材质：CITY_PALETTE（steel / tire / vehicle_dark / bus_teal 车架）。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；自行车前后轮沿 **X 轴**排布（长边 = 交付 X，沿道路向）。
  · 世界单位 ×U=0.1；节点 identity；minY=0；X/Z 居中。
  · 两命名节点：BikeRack（架）/ BikeRack_Bike（斜靠自行车）。
  · 交付尺寸：x = 0.180 / y = 0.110 / z = 0.062（≡ REAL_DIMS_M.bikeRack）。

用法：
  blender --background --python build_bike_rack.py -- \
    ClientWeb/src/assets/models/road/bike_rack.glb
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
    make_box, make_cylinder, make_material, assign_material,
    join_objects, export_glb, weathered_pbr, shared_weathered,
    flatten_weathered_materials, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位

# ── 停车架（米）：两倒 U 沿 X 并排，跨距沿 X，架面在 Y=+0.18 ────────────
RACK_H = 0.80
RACK_SPAN = 0.85   # 单架跨距（沿 X）
RACK_R = 0.017
RACK_XC = 0.44     # 两架中心（±0.44）⇒ 外缘 0.865 ≈ 交付 X 1.80 含脚盘
RACK_Y = 0.18

# ── 自行车（米，沿 X：前轮 +X / 后轮 −X；向 +Y 倾 15°）────────────────
WHEEL_R = 0.33     # 26″ 轮外径 0.66
TIRE_R = 0.035
RIM_MAJ = 0.275    # 轮辋环主半径
RIM_MIN = 0.012
HUB_R = 0.028
WHEEL_DX = 0.545   # 半轴距
BB_H = 0.28
SEAT_H = 0.88
BAR_H = 1.10       # 把立顶（倾角后 ≈ 交付 Y 1.10）
TUBE_R = 0.016
TILT = math.radians(12.0)   # 侧倾 12°（15° 时把横把侧推到 Z=0.069 超交付 0.062）
BIKE_Y = -0.05     # 车身中线（架前）
N_SPOKES = 12


def _m(v):
    return v * U


def make_torus(name, major, minor, pos, rot, segs_maj=24, segs_min=8, color=None, rough=0.5, metal=0.1):
    bpy.ops.mesh.primitive_torus_add(
        major_radius=_m(major), minor_radius=_m(minor),
        major_segments=segs_maj, minor_segments=segs_min,
        location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.location = (_m(pos[0]), _m(pos[1]), _m(pos[2]))
    obj.rotation_euler = (rot[0], rot[1], rot[2])
    if color:
        apply = make_material(name + '_Mat', color, rough, metal)
        assign_material(obj, apply)
    return obj


def make_tube(name, p0, p1, r, color=None, rough=0.45, metal=0.6, segs=8):
    """细圆管沿 p0→p1（Blender Z-up）。Euler XYZ：Z 轴转到方向向量
    （ry=极角、rz=方位角）—— 早期版本多加 π/2 导致车架管方向错乱（v1 返工点）。"""
    x0, y0, z0 = p0
    x1, y1, z1 = p1
    dx, dy, dz = x1 - x0, y1 - y0, z1 - z0
    length = math.sqrt(dx * dx + dy * dy + dz * dz)
    t = make_cylinder(name, _m(r), _m(r), _m(length), segs,
                      (_m((x0 + x1) / 2), _m((y0 + y1) / 2), _m((z0 + z1) / 2)))
    ry = math.atan2(math.sqrt(dx * dx + dy * dy), dz)
    rz = math.atan2(dy, dx)
    t.rotation_euler = (0.0, ry, rz)
    if color:
        assign_material(t, make_material(name + '_Mat', color, rough, metal))
    return t


def _join_applied(objs, name):
    """join 前逐件 transform_apply —— join 只把其他件折进**活动对象**的局部系，
    活动对象自身的 rotation/scale 会留在结果上。本脚本大量件带旋转（torus 轮胎
    π/2、辐条/车架管方向角），若不先 apply，join 后整车几何会被活动件的变换
    拧乱（v2 实测：包围盒 0.181×0.082×0.115、整车侧躺）。"""
    bpy.ops.object.select_all(action='DESELECT')
    for o in objs:
        o.select_set(True)
    bpy.context.view_layer.objects.active = objs[0]
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return join_objects(objs, name)


def build_rack() -> bpy.types.Object:
    objs = []
    steel = CITY_PALETTE['steel']
    foot_mat = make_material('Rack_Foot_Mat', CITY_PALETTE['steel_dark'], rough=0.55, metal=0.70)
    rack_parts = []
    for i, xc in enumerate((-RACK_XC, RACK_XC)):
        for sx in (-1, 1):
            lx = xc + sx * RACK_SPAN / 2
            leg = make_cylinder(f'Rack{i}_Leg{sx:+d}', _m(RACK_R), _m(RACK_R), _m(RACK_H), 12,
                                (_m(lx), _m(RACK_Y), _m(RACK_H / 2)))
            objs.append(leg)
            rack_parts.append(leg)
            foot = make_cylinder(f'Rack{i}_Foot{sx:+d}', _m(0.036), _m(0.038), _m(0.012), 12,
                                 (_m(lx), _m(RACK_Y), _m(0.006)))
            assign_material(foot, foot_mat)
            objs.append(foot)
        top = make_tube(f'Rack{i}_Top',
                        (xc - RACK_SPAN / 2, RACK_Y, RACK_H),
                        (xc + RACK_SPAN / 2, RACK_Y, RACK_H),
                        RACK_R, steel, rough=0.38, metal=0.90, segs=12)
        objs.append(top)
        rack_parts.append(top)
    # 架管共用 1 槽（批次 42 材质槽收口：8 腿/顶管不再各占一槽）
    shared_weathered(rack_parts, steel, rough=0.38, metal=0.90,
                     name='Rack_Steel_Mat', wear=0.25, scale=10.0)
    joined = _join_applied(objs, 'BikeRack')
    return joined


def build_bike() -> bpy.types.Object:
    objs = []
    tire = CITY_PALETTE['tire']
    steel = CITY_PALETTE['steel']
    dark = CITY_PALETTE['steel_dark']
    frame_hex = CITY_PALETTE['bus_teal']
    frame_mat = make_material('Bike_Frame_Mat', frame_hex, rough=0.42, metal=0.35)
    tire_mat = make_material('Bike_Tire_Mat', tire, rough=0.85, metal=0.0)
    rim_mat = make_material('Bike_Rim_Mat', dark, rough=0.40, metal=0.75)
    hub_mat = rim_mat   # 轮组金属共用 1 槽（批次 42 材质槽收口）
    spoke_mat = rim_mat

    def wheel(tag, cx):
        out = []
        # 胎环（torus，轴 = Y ⇒ 转 π/2 绕 X）
        t = make_torus(f'{tag}_Tire', WHEEL_R - TIRE_R, TIRE_R, (cx, 0, WHEEL_R),
                       (math.pi / 2, 0, 0), segs_maj=24, segs_min=8)
        assign_material(t, tire_mat)
        out.append(t)
        # 轮辋环 + 花鼓 + 辐条（真实轮组：不是实心盘）—— 同一金属槽
        rim = make_torus(f'{tag}_Rim', RIM_MAJ, RIM_MIN, (cx, 0, WHEEL_R),
                         (math.pi / 2, 0, 0), segs_maj=20, segs_min=6)
        assign_material(rim, rim_mat)
        out.append(rim)
        hub = make_cylinder(f'{tag}_Hub', _m(HUB_R), _m(HUB_R), _m(0.075), 12,
                            (_m(cx), 0, _m(WHEEL_R)), (math.pi / 2, 0, 0))
        assign_material(hub, rim_mat)
        out.append(hub)
        for k in range(N_SPOKES):
            a = k * 2 * math.pi / N_SPOKES + 0.13
            p0 = (cx, 0, WHEEL_R)
            p1 = (cx + math.cos(a) * RIM_MAJ, 0, WHEEL_R + math.sin(a) * RIM_MAJ)
            sp = make_tube(f'{tag}_Spoke{k}', p0, p1, 0.0038, segs=6)
            assign_material(sp, rim_mat)
            out.append(sp)
        return out

    objs += wheel('FW', WHEEL_DX)
    objs += wheel('RW', -WHEEL_DX)

    # ── 车架三角 ───────────────────────────────────────────────────────
    bb = (0.0, 0.0, BB_H)
    seat_top = (-0.16, 0.0, SEAT_H - 0.06)
    head_top = (0.36, 0.0, 0.90)
    head_bot = (0.40, 0.0, 0.70)
    for tag, p0, p1, r in (
        ('DownTube', bb, head_bot, 0.018),
        ('TopTube', seat_top, head_top, 0.015),
        ('SeatTube', bb, seat_top, 0.015),
        ('HeadTube', head_bot, head_top, 0.019),
    ):
        t = make_tube('Bike_' + tag, p0, p1, r)
        assign_material(t, frame_mat)
        objs.append(t)
    for sy in (-1, 1):
        for tag, p0, p1, r in (
            (f'Stay{sy:+d}', (-WHEEL_DX, sy * 0.045, WHEEL_R), seat_top, 0.010),
            (f'ChainStay{sy:+d}', (-WHEEL_DX, sy * 0.045, WHEEL_R), bb, 0.011),
            (f'Fork{sy:+d}', (WHEEL_DX, sy * 0.045, WHEEL_R), head_bot, 0.011),
        ):
            t = make_tube('Bike_' + tag, p0, p1, r)
            assign_material(t, frame_mat)
            objs.append(t)

    # ── 车把 + 把立 + 把套 ─────────────────────────────────────────────
    metal_dark = make_material('Bike_MetalDark_Mat', dark, rough=0.45, metal=0.60)
    stem = make_tube('Bike_Stem', head_top, (0.33, 0.0, BAR_H), 0.013)
    assign_material(stem, metal_dark)
    objs.append(stem)
    bar = make_cylinder('Bike_Bar', _m(0.013), _m(0.013), _m(0.51), 10,
                        (_m(0.33), 0, _m(BAR_H)), (math.pi / 2, 0, 0))
    assign_material(bar, metal_dark)
    objs.append(bar)
    grip_mat = make_material('Bike_Dark_Mat', CITY_PALETTE['vehicle_dark'], rough=0.80, metal=0.0)
    for sy in (-1, 1):
        grip = make_cylinder(f'Bike_Grip{sy:+d}', _m(0.017), _m(0.017), _m(0.11), 8,
                             (_m(0.33), _m(sy * 0.20), _m(BAR_H)), (math.pi / 2, 0, 0))
        assign_material(grip, grip_mat)
        objs.append(grip)

    # ── 座垫 + 座管 ────────────────────────────────────────────────────
    post = make_tube('Bike_SeatPost', seat_top, (-0.185, 0.0, SEAT_H), 0.013)
    assign_material(post, metal_dark)
    objs.append(post)
    saddle = make_box('Bike_Saddle', (_m(0.22), _m(0.09), _m(0.035)),
                      (_m(-0.20), 0, _m(SEAT_H + 0.012)))
    assign_material(saddle, grip_mat)
    objs.append(saddle)

    # ── 曲柄 + 脚踏 ────────────────────────────────────────────────────
    crank_mat = make_material('Bike_Crank_Mat', steel, rough=0.40, metal=0.85)
    # 曲柄亮钢单独 1 槽（与把立暗钢区分）
    for sy, phase in ((-1, 0.35), (1, 0.35 + math.pi)):
        px, pz = math.cos(phase) * 0.085, BB_H + math.sin(phase) * 0.085
        crank = make_box(f'Bike_Crank{sy:+d}', (_m(0.15), _m(0.020), _m(0.032)),
                         (_m(px / 2), _m(sy * 0.070), _m(BB_H + (pz - BB_H) / 2)))
        crank.rotation_euler = (0, -phase, 0)
        assign_material(crank, crank_mat)
        objs.append(crank)
        pedal = make_box(f'Bike_Pedal{sy:+d}', (_m(0.10), _m(0.055), _m(0.016)),
                         (_m(px), _m(sy * 0.115), _m(pz)))
        assign_material(pedal, grip_mat)
        objs.append(pedal)

    # ── 链罩 + 前后挡泥板 ──────────────────────────────────────────────
    chain = make_box('Bike_ChainGuard', (_m(0.30), _m(0.028), _m(0.10)),
                     (_m(-0.20), _m(-0.052), _m(BB_H + 0.02)))
    assign_material(chain, metal_dark)
    objs.append(chain)
    fender_mat = frame_mat   # 挡泥板与车架同漆同槽
    for tag, cx in (('FenderF', WHEEL_DX), ('FenderR', -WHEEL_DX)):
        fend = make_box(f'Bike_{tag}', (_m(0.42), _m(0.052), _m(0.016)),
                        (_m(cx), 0, _m(WHEEL_R + 0.048)))
        assign_material(fend, fender_mat)
        objs.append(fend)

    # ── 脚撑 ──────────────────────────────────────────────────────────
    stand = make_tube('Bike_Stand', (-0.32, -0.02, BB_H - 0.02), (-0.38, -0.14, 0.01), 0.010)
    assign_material(stand, metal_dark)
    objs.append(stand)

    joined = _join_applied(objs, 'BikeRack_Bike')
    # 向 +Y（架侧）倾 15°（Rx 负角 ⇒ 顶部偏 +Y，模拟斜靠）
    joined.rotation_euler = (-TILT, 0, 0)
    bpy.ops.object.select_all(action='DESELECT')
    joined.select_set(True)
    bpy.context.view_layer.objects.active = joined
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    from mathutils import Vector
    bb_min = min((joined.matrix_world @ Vector(c)).z for c in joined.bound_box)
    joined.location = (joined.location[0], _m(BIKE_Y), joined.location[2] - bb_min)
    bpy.context.view_layer.update()
    return joined


if __name__ == '__main__':
    reset_scene()
    set_unit_meters()
    rack = build_rack()
    bike = build_bike()
    for ob in (rack, bike):
        flatten_weathered_materials(ob)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/bike_rack.glb'
    export_glb(out_path)
    print(f'[build_bike_rack] done -> {out_path}', flush=True)
