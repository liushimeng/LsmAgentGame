#!/usr/bin/env python3
"""
build_vehicle_bus — 公交车（bus）Blender headless 导出脚本（批次 41 重制）。

v19 原版（704 面「单盒 + 16 侧窗贴片」）→ 批次 41 双层放样重制：
  - 车身分**上下两段放样**（同 x 站表、同 a，y 在 beltline 0.125 处分色）：
    下段青绿（bus_teal，裙板色带）/ 上段白（bus_white）—— 连续曲率无贴片感；
  - 前脸 7 站渐收 + 顶部后倾（低地板城市客车的近竖风挡 rake）；
  - 侧窗连续玻璃带（8 档白立柱分隔）+ 倾斜前风挡 + 后窗，alpha 0.5 透明；
  - 车门 ×2（右侧前/中，内凹：上 2/3 玻璃 + 下 1/3 裙板色）；
  - 车顶空调包（顶面含 3 条散热格栅；计入总高 3.20 m）+ 前上/右前路牌屏
    （emissive 琥珀，材质名 'Sign_F'/'Sign_S'，前端按名昼夜调制）；
  - 轮拱：boolean 减出贯穿两侧开口（r=0.58 m）+ 底板遮透视 + 车内座椅排
    （透过侧窗带可见 6 排）+ 大臂后视镜 ×2。

真实比例参考（检索结论，2026-10-01）：12 m 低地板城市客车 12.13×2.55×3.22 m
（STREETWAY 全电 12m 规格书）；轮 ⌀1.0 m；门 2 个（前 + 中，同右侧）；
离地 0.30-0.35 m；侧窗带 y 1.35-2.35 m。

对应组件 ClientWeb/src/components/virtualCity/props/Vehicle.tsx（变体 'bus'）。
坐标与尺度规约与 v19 一致（Blender Z-up / 1 单位 = 10 m / 导出 Yup）。
目标包围盒（世界单位）：X 1.200 × Z 0.255 × Y 0.320（空调计入），轮底 minY = 0。
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)

from __common__ import (  # noqa: E402
    CITY_PALETTE, assign_material, export_glb, join_objects, make_box,
    make_cylinder, make_material, reset_scene, set_unit_meters,
)
from vehicle_loft import build_wheel, loft_stations, wheel_well_cut  # noqa: E402

# ── 尺寸（世界单位；1 单位 = 10 m）────────────────────────────────────────
L, W, H = 1.200, 0.255, 0.320              # 目标包围盒（REAL_DIMS_M.bus；H 含空调）
BODY_TOP = 0.300                           # 车身顶（空调 0.294..0.318 计入 H）
BELT_SPLIT = 0.125                         # 上白下青分色线
WHEEL_R, WHEEL_W = 0.050, 0.030            # 胎 ⌀1.0 m / 胎宽 0.30 m
WHEEL_X_F, WHEEL_X_R = +0.37, -0.30        # 前轴 / 后轴（轴距 6.7 m）
ARCH_R = 0.058                             # 轮拱开口半径
WHEEL_Y = W / 2 - WHEEL_W / 2 - 0.002      # 胎外缘略内收（0.1255 < 车宽半 0.1275）

# ── 车身站表（x, a=半宽, top=顶高；底 0.030 起）──────────────────────────
# 前脸 0.52→0.60 渐收 + 顶后倾 = 低地板客车近竖风挡 rake；端面站 a 收窄。
STATIONS = [
    dict(x=-0.600, a=0.118, top=0.285),
    dict(x=-0.560, a=0.125, top=0.300),
    dict(x=-0.200, a=0.1275, top=0.300),
    dict(x=+0.300, a=0.1275, top=0.300),
    dict(x=+0.520, a=0.125, top=0.293),
    dict(x=+0.575, a=0.121, top=0.283),
    dict(x=+0.600, a=0.115, top=0.270),
]
N_LOOP = 20
BOTTOM = 0.030


def _loft_half(name, z0, z1, n=4.5):
    """按 STATIONS 放样 y∈[z0,z1] 的半段车身（zc/b 由区间推导）。"""
    sts = [dict(x=s['x'], a=s['a'], zc=(z0 + z1) / 2, b=(z1 - z0) / 2, n=n)
           for s in STATIONS]
    return loft_stations(sts, N_LOOP, name)


def build_vehicle_bus():
    parts = []

    # ── 材质（语义共享实例）───────────────────────────────────────────────
    mat_white = make_material('BusWhite', CITY_PALETTE['bus_white'], 0.35, 0.15)
    mat_teal = make_material('BusTeal', CITY_PALETTE['bus_teal'], 0.45, 0.1)
    mat_glass = make_material('Glass', CITY_PALETTE['vehicle_glass'],
                              0.08, 0.10, alpha=0.5)
    mat_dark = make_material('VehicleDark', CITY_PALETTE['vehicle_dark'], 0.85, 0.05)
    mat_chrome = make_material('Chrome', '#c9ced6', 0.18, 1.0)
    mat_tire = make_material('Tire', CITY_PALETTE['tire'], 0.92, 0.0)
    mat_plate = make_material('Plate', '#e9ebee', 0.5, 0.0)
    mat_head = make_material('Headlight_L', '#fff6e0', 0.25, 0.0,
                             emissive='#fff2cc', emissive_intensity=1.0)
    mat_tail = make_material('Taillight', '#c01010', 0.3, 0.0,
                             emissive='#ff2418', emissive_intensity=0.8)
    mat_sign = make_material('Sign_F', '#3a2a10', 0.4, 0.0,
                             emissive=CITY_PALETTE['sign_amber'],
                             emissive_intensity=1.2)

    # ── 上下两段放样（beltline 0.125 分色，同站表同 a ⇒ 连续曲率）────────
    lower = _loft_half('BodyLower', BOTTOM, BELT_SPLIT)
    upper = _loft_half('BodyUpper', BELT_SPLIT, BODY_TOP)
    # 轮拱开口（贯穿两侧；在 join 前对 lower 单独做——upper 在窗带以上不碰拱）
    for cx in (WHEEL_X_F, WHEEL_X_R):
        wheel_well_cut(lower, cx, WHEEL_R, ARCH_R, W + 0.04)
    assign_material(lower, mat_teal)
    assign_material(upper, mat_white)
    parts += [lower, upper]

    # ── 底板（遮轮拱透视）+ 车内（地板 + 6 排座椅，透过窗带可见）──────────
    under = make_box('Underbody', (1.05, 0.19, 0.026), (0.0, 0.0, 0.045))
    assign_material(under, mat_dark)
    parts.append(under)
    floor = make_box('IntFloor', (1.00, 0.20, 0.004), (0.0, 0.0, 0.115))
    assign_material(floor, mat_dark)
    parts.append(floor)
    for k in range(6):
        seat = make_box(f'IntSeatRow{k}', (0.014, 0.19, 0.030),
                        (-0.35 + k * 0.12, 0.0, 0.142))
        assign_material(seat, mat_dark)
        parts.append(seat)

    # ── 侧窗带（两侧连续玻璃 + 8 档白立柱）───────────────────────────────
    for sy in (1, -1):
        band = make_box(f'WindowBand_{sy}', (0.98, 0.0012, 0.095),
                        (0.0, sy * 0.1278, 0.185))
        assign_material(band, mat_glass)
        parts.append(band)
        for k in range(8):
            pil = make_box(f'Pillar_{sy}_{k}', (0.014, 0.002, 0.095),
                           (-0.44 + k * 0.125, sy * 0.1281, 0.185))
            assign_material(pil, mat_white)
            parts.append(pil)

    # ── 前风挡（近竖 rake ~10° 后倾）+ 后窗 ───────────────────────────────
    ws = make_box('Windshield', (0.006, 0.205, 0.142), (0.574, 0.0, 0.172))
    ws.rotation_euler = (0.0, math.radians(10.0), 0.0)
    assign_material(ws, mat_glass)
    parts.append(ws)
    rw = make_box('RearWindow', (0.003, 0.180, 0.105), (-0.5975, 0.0, 0.175))
    assign_material(rw, mat_glass)
    parts.append(rw)

    # ── 车门 ×2（右侧前/中：上 2/3 玻璃 + 下 1/3 裙板色，内凹 0.001）──────
    for name, dx in (('DoorFront', +0.24), ('DoorMid', -0.13)):
        gl = make_box(f'{name}_Glass', (0.088, 0.0015, 0.128), (dx, 0.1265, 0.166))
        lo = make_box(f'{name}_Lower', (0.090, 0.0015, 0.062), (dx, 0.1265, 0.066))
        assign_material(gl, mat_glass)
        assign_material(lo, mat_teal)
        parts += [gl, lo]

    # ── 车顶空调（顶 3 格栅条；顶面 0.318 ≤ H 0.320）+ 路牌屏 ─────────────
    ac = make_box('RoofAC', (0.26, 0.19, 0.024), (0.10, 0.0, 0.306))
    assign_material(ac, mat_dark)
    parts.append(ac)
    for k in range(3):
        vent = make_box(f'ACVent{k}', (0.20, 0.012, 0.004), (0.10, -0.06 + k * 0.06, 0.3185))
        assign_material(vent, mat_chrome)
        parts.append(vent)
    sign_f = make_box('Sign_F', (0.005, 0.20, 0.026), (0.585, 0.0, 0.260))
    assign_material(sign_f, mat_sign)
    parts.append(sign_f)
    sign_s = make_box('Sign_S', (0.20, 0.003, 0.024), (0.48, 0.1280, 0.258))
    assign_material(sign_s, mat_sign)
    parts.append(sign_s)

    # ── 灯组 / 车牌 / 后视镜 ─────────────────────────────────────────────
    for sy in (1, -1):
        hl = make_box(f'Headlight_{sy}', (0.005, 0.030, 0.020),
                      (0.5985, sy * 0.075, 0.075))
        assign_material(hl, mat_head)
        parts.append(hl)
    for sy in (1, -1):
        tl = make_box(f'Taillight_{sy}', (0.004, 0.015, 0.060),
                      (-0.5985, sy * 0.105, 0.100))
        assign_material(tl, mat_tail)
        parts.append(tl)
    plate_f = make_box('PlateF', (0.002, 0.040, 0.011), (0.5990, 0.0, 0.048))
    plate_r = make_box('PlateR', (0.002, 0.040, 0.011), (-0.5990, 0.0, 0.048))
    assign_material(plate_f, mat_plate)
    assign_material(plate_r, mat_plate)
    parts += [plate_f, plate_r]
    for sy in (1, -1):
        # 镜臂/镜壳外缘 ≤ 0.1335（总宽 0.267 ≤ 0.255+5% 容差；真实公交镜臂
        # 外挑更多，此处按 REAL_DIMS_M 合同收紧）
        arm = make_box(f'MirrorArm_{sy}', (0.004, 0.006, 0.003),
                       (0.575, sy * 0.1288, 0.235))
        shell = make_box(f'MirrorShell_{sy}', (0.018, 0.005, 0.045),
                         (0.575, sy * 0.1310, 0.245))
        assign_material(arm, mat_white)
        assign_material(shell, mat_white)
        parts += [arm, shell]

    # ── 车轮 ×4 ──────────────────────────────────────────────────────────
    for name, wx, sy in (('WheelFL', WHEEL_X_F, +1), ('WheelFR', WHEEL_X_F, -1),
                         ('WheelRL', WHEEL_X_R, +1), ('WheelRR', WHEEL_X_R, -1)):
        for p in build_wheel(name, wx, sy * WHEEL_Y, WHEEL_R, WHEEL_R, WHEEL_W):
            is_hub = '_Hub' in p.name or '_Spoke' in p.name
            is_tire = p.name.endswith('_Tire')
            assign_material(p, mat_chrome if is_hub else
                            (mat_tire if is_tire else mat_dark))
            parts.append(p)

    return join_objects(parts, 'VehicleBus')


if __name__ == '__main__':
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv \
        else '/tmp/vehicle_bus.glb'
    reset_scene()
    set_unit_meters()
    obj = build_vehicle_bus()
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    export_glb(out_path)
    print(f'[build_vehicle_bus] wrote {out_path}', flush=True)
