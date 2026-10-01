#!/usr/bin/env python3
"""
build_vehicle_sedan — 轿车（sedan）Blender headless 导出脚本（批次 41 重制）。

v19 原版（536 面「两盒叠加」积木）→ 批次 41 截面放样重制：
  - 下车身：9 站超椭圆（n=4.5 圆角矩形）放样 —— 弧线过渡的车头/车尾、
    机盖坡度（beltline 0.95 m → 前端 0.74 m 渐落）、真实离地 0.15 m；
  - 座舱玻璃体：6 站椭圆（n=2.4）放样 —— 弧线车顶 + 前后风挡 rake +
    tumblehome（顶宽 0.73 < 肩宽 0.91），alpha 0.5 透明玻璃；
  - 车顶漆板 + B/C 柱 + 车内可见内饰（地板/仪表台/前后座椅）；
  - 轮拱：boolean 减出贯穿两侧的开口（r=0.40 m），胎外缘与车身侧面齐平；
  - 细节件：格栅 5 条 + 下进气口 + 前大灯 ×2 + 贯穿式尾灯 + 车牌 ×2 +
    后视镜 ×2 + 门把手 ×4 + 门线 ×3×2 + 鲨鱼鳍天线 + 排气口 + 底板。

真实比例参考（检索结论，2026-10-01）：
  车身全高 ≈ 2.25-2.5× 轮径（本车 1.45/0.65 = 2.23）；beltline 850-1000 mm
  （本车 950）；机盖 800-950 mm（本车 840-900）；离地 135-168 mm（本车 150）；
  C 级轴距 2.54-2.7 m = 59% 车长（本车 2.94 m = 64%，轮位 ±0.32× 车长与
  fallback Vehicle.tsx wheelPositions 同口径）。

对应组件 ClientWeb/src/components/virtualCity/props/Vehicle.tsx（变体 'sedan'）。
坐标与尺度规约与 v19 一致：Blender Z-up（x=车长/y=车宽/z=高，地面 z=0）；
导出 Yup ⇒ glTF X=车长、Y=车高（minY=0）、Z=车宽；1 单位 = 10 m。

目标包围盒（世界单位）：X 0.460 × Z 0.182 × Y 0.145，轮底 minY = 0。
导出 2 个涂装：sedan.glb（蓝）+ sedan_silver.glb（银灰）。
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)

from __common__ import (  # noqa: E402
    CITY_PALETTE, export_glb, join_objects, make_box, make_cylinder,
    make_material, assign_material, reset_scene, set_unit_meters,
)
from vehicle_loft import build_wheel, loft_stations, wheel_well_cut  # noqa: E402

# ── 尺寸（世界单位；1 单位 = 10 m）────────────────────────────────────────
L, W, H = 0.460, 0.182, 0.145            # 目标包围盒（REAL_DIMS_M.sedan）
WHEEL_R, WHEEL_W = 0.0325, 0.022         # 胎 ⌀0.65 m / 胎宽 0.22 m
WHEEL_X = 0.32 * L                       # ±0.147（与 fallback wheelPositions 同口径）
ARCH_R = 0.040                           # 轮拱开口半径（胎 + 7.5 mm）
BELT = 0.095                             # beltline（0.95 m）

# ── 截面站表（见模块 docstring；a=半宽 zc=高度中心 b=半高 n=方次）─────────
BODY_STATIONS = [
    dict(x=-0.228, a=0.078, zc=0.0370, b=0.0220, n=4.5),  # 后杠端面
    dict(x=-0.215, a=0.086, zc=0.0480, b=0.0330, n=4.5),
    dict(x=-0.160, a=0.090, zc=0.0530, b=0.0380, n=4.5),  # 尾厢
    dict(x=-0.070, a=0.091, zc=0.0550, b=0.0400, n=4.5),  # 后门（肩宽最大）
    dict(x=+0.040, a=0.091, zc=0.0550, b=0.0400, n=4.5),  # 前门
    dict(x=+0.110, a=0.090, zc=0.0530, b=0.0370, n=4.5),  # 雨刮根（机盖起）
    dict(x=+0.170, a=0.088, zc=0.0510, b=0.0330, n=4.5),  # 机盖
    dict(x=+0.215, a=0.084, zc=0.0470, b=0.0270, n=4.5),
    dict(x=+0.228, a=0.078, zc=0.0400, b=0.0190, n=4.5),  # 前杠端面
]
GLASS_STATIONS = [
    dict(x=-0.162, a=0.057, zc=0.0995, b=0.0085, n=2.4),  # 后风挡下缘
    dict(x=-0.105, a=0.068, zc=0.1095, b=0.0235, n=2.4),
    dict(x=-0.035, a=0.0725, zc=0.1125, b=0.0255, n=2.4),  # 顶（0.138）
    dict(x=+0.045, a=0.073, zc=0.1130, b=0.0250, n=2.4),
    dict(x=+0.095, a=0.069, zc=0.1060, b=0.0200, n=2.4),
    dict(x=+0.128, a=0.060, zc=0.0985, b=0.0090, n=2.4),  # 前风挡下缘
]
ROOF_STATIONS = [
    dict(x=-0.100, a=0.069, zc=0.1375, b=0.0075, n=3.0),  # 顶 0.145
    dict(x=-0.030, a=0.0735, zc=0.1385, b=0.0065, n=3.0),
    dict(x=+0.040, a=0.0730, zc=0.1380, b=0.0070, n=3.0),
    dict(x=+0.090, a=0.0640, zc=0.1330, b=0.0060, n=3.0),
]
N_LOOP = 20


def build_sedan_family(paint_hex: str, roof_sign: bool = False):
    """按涂装色构建整车（join 后单 object）。taxi 脚本复用本函数换色 + 顶灯。"""
    parts = []

    # ── 材质（语义共享实例；join 后按槽位保序）────────────────────────────
    mat_paint = make_material('Paint', paint_hex, 0.32, 0.75)
    mat_paint_dark = make_material('PaintDark', paint_hex, 0.38, 0.65)
    mat_glass = make_material('Glass', CITY_PALETTE['vehicle_glass'],
                              0.08, 0.10, alpha=0.5)
    mat_dark = make_material('VehicleDark', CITY_PALETTE['vehicle_dark'], 0.85, 0.05)
    mat_chrome = make_material('Chrome', '#c9ced6', 0.18, 1.0)
    mat_tire = make_material('Tire', CITY_PALETTE['tire'], 0.92, 0.0)
    mat_plate = make_material('Plate', '#e9ebee', 0.5, 0.0)
    mat_head = make_material('Headlight_L', '#fff6e0', 0.25, 0.0,
                             emissive='#fff2cc', emissive_intensity=1.0)
    mat_tail = make_material('Taillight_Bar', '#c01010', 0.3, 0.0,
                             emissive='#ff2418', emissive_intensity=0.8)

    # ── 三大放样体 ────────────────────────────────────────────────────────
    body = loft_stations(BODY_STATIONS, N_LOOP, 'Body')
    glass = loft_stations(GLASS_STATIONS, N_LOOP, 'Glasshouse')
    roof = loft_stations(ROOF_STATIONS, N_LOOP, 'RoofPanel')
    assign_material(body, mat_paint)
    assign_material(glass, mat_glass)
    assign_material(roof, mat_paint_dark)

    # 轮拱开口（贯穿两侧；在 join 前对 body 单独做）
    for cx in (+WHEEL_X, -WHEEL_X):
        wheel_well_cut(body, cx, WHEEL_R, ARCH_R, W + 0.04)

    parts += [body, glass, roof]

    # ── 底板（暗色，遮轮拱透視 + 落影感）──────────────────────────────────
    under = make_box('Underbody', (0.355, 0.128, 0.020), (0.0, 0.0, 0.029))
    assign_material(under, mat_dark)
    parts.append(under)

    # ── 内饰（透过玻璃可见：地板/仪表台/前座×2/后座）────────────────────
    for nm, sz, pos in (
        ('IntFloor', (0.235, 0.118, 0.004), (-0.010, 0.0, 0.088)),
        ('IntDash', (0.030, 0.108, 0.024), (0.098, 0.0, 0.098)),
        ('IntSeatFL', (0.052, 0.046, 0.030), (0.012, 0.032, 0.104)),
        ('IntSeatFR', (0.052, 0.046, 0.030), (0.012, -0.032, 0.104)),
        ('IntBench', (0.095, 0.108, 0.026), (-0.090, 0.0, 0.100)),
    ):
        p = make_box(nm, sz, pos)
        assign_material(p, mat_dark)
        parts.append(p)

    # ── B / C 柱（漆条贴玻璃侧面）────────────────────────────────────────
    for nm, x, y, half in (('PillarB', -0.030, 0.0735, 0.074),
                           ('PillarC', -0.115, 0.070, 0.072)):
        for sy in (1, -1):
            p = make_box(f'{nm}_{sy}', (0.011, 0.006, 0.046),
                         (x, sy * half, 0.1115))
            assign_material(p, mat_paint)
            parts.append(p)

    # ── 格栅 / 进气口 / 灯组 / 车牌 / 排气 ────────────────────────────────
    for i in range(5):  # 格栅 5 横条
        g = make_box(f'Grille{i}', (0.004, 0.098, 0.0045),
                     (0.2275, 0.0, 0.031 + i * 0.0062))
        assign_material(g, mat_dark)
        parts.append(g)
    intake = make_box('Intake', (0.003, 0.104, 0.011), (0.2280, 0.0, 0.0205))
    assign_material(intake, mat_dark)
    parts.append(intake)
    for sy in (1, -1):
        hl = make_box(f'Headlight_{sy}', (0.005, 0.031, 0.009),
                      (0.2260, sy * 0.052, 0.0625))
        assign_material(hl, mat_head)
        parts.append(hl)
    tail = make_box('Taillight', (0.006, 0.130, 0.009), (-0.2215, 0.0, 0.0700))
    assign_material(tail, mat_tail)
    parts.append(tail)
    plate_f = make_box('PlateF', (0.002, 0.040, 0.011), (0.2290, 0.0, 0.0365))
    plate_r = make_box('PlateR', (0.002, 0.040, 0.011), (-0.2285, 0.0, 0.0365))
    assign_material(plate_f, mat_plate)
    assign_material(plate_r, mat_plate)
    parts += [plate_f, plate_r]
    exh = make_cylinder('Exhaust', 0.008, 0.008, 0.018, 10, (-0.222, 0.045, 0.019),
                        rot=(0.0, math.pi / 2, 0.0))
    assign_material(exh, mat_dark)
    parts.append(exh)

    # ── 后视镜 / 门把手 / 门线 / 鲨鱼鳍 ───────────────────────────────────
    for sy in (1, -1):
        arm = make_box(f'MirrorArm_{sy}', (0.013, 0.004, 0.003),
                       (0.098, sy * 0.0895, 0.0995))
        shell = make_box(f'MirrorShell_{sy}', (0.017, 0.005, 0.011),
                         (0.098, sy * 0.0895, 0.1025))
        assign_material(arm, mat_paint)
        assign_material(shell, mat_paint)
        parts += [arm, shell]
    for sy in (1, -1):
        for x in (-0.045, 0.020):
            hd = make_box(f'Handle_{sy}_{x}', (0.013, 0.002, 0.003),
                          (x, sy * 0.0908, 0.0825))
            assign_material(hd, mat_chrome)
            parts.append(hd)
    for sy in (1, -1):
        for x in (-0.070, -0.005, 0.085):
            ln = make_box(f'DoorLine_{sy}_{x}', (0.0022, 0.0012, 0.058),
                          (x, sy * 0.0906, 0.0560))
            assign_material(ln, mat_dark)
            parts.append(ln)
    fin = make_box('SharkFin', (0.026, 0.014, 0.007), (-0.085, 0.0, 0.1415))
    assign_material(fin, mat_paint_dark)
    parts.append(fin)

    # ── taxi 顶灯（roof_sign；taxi 脚本传 True）──────────────────────────
    if roof_sign:
        mat_sign = make_material('TaxiSign', '#2a2008', 0.4, 0.0,
                                 emissive='#ffd98a', emissive_intensity=1.4)
        base = make_box('TaxiSignBase', (0.055, 0.028, 0.0035), (-0.020, 0.0, 0.1465))
        lamp = make_box('TaxiSignLamp', (0.050, 0.024, 0.004), (-0.020, 0.0, 0.1495))
        assign_material(base, mat_dark)
        assign_material(lamp, mat_sign)
        parts += [base, lamp]

    # ── 车轮 ×4（胎外缘与车身侧面齐平：y=±(W/2−胎宽/2)）─────────────────
    wheel_y = W / 2 - WHEEL_W / 2
    for name, wx, sy in (('WheelFL', +WHEEL_X, +1), ('WheelFR', +WHEEL_X, -1),
                         ('WheelRL', -WHEEL_X, +1), ('WheelRR', -WHEEL_X, -1)):
        for p in build_wheel(name, wx, sy * wheel_y, WHEEL_R, WHEEL_R, WHEEL_W):
            is_hub = '_Hub' in p.name or '_Spoke' in p.name
            is_tire = p.name.endswith('_Tire')
            assign_material(p, mat_chrome if is_hub else
                            (mat_tire if is_tire else mat_dark))
            parts.append(p)

    return join_objects(parts, 'VehicleSedan')


def _export(obj, out_path: str) -> None:
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    export_glb(out_path)


if __name__ == '__main__':
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv \
        else '/tmp/vehicle_sedan.glb'
    silver_path = os.path.join(os.path.dirname(out_path), 'sedan_silver.glb')

    reset_scene()
    set_unit_meters()
    _export(build_sedan_family(CITY_PALETTE['sedan_blue']), out_path)
    print(f'[build_vehicle_sedan] wrote {out_path}', flush=True)

    reset_scene()
    set_unit_meters()
    _export(build_sedan_family(CITY_PALETTE['sedan_silver']), silver_path)
    print(f'[build_vehicle_sedan] wrote {silver_path}', flush=True)
