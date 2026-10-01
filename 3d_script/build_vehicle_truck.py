#!/usr/bin/env python3
"""
build_vehicle_truck — 货车（truck）Blender headless 导出脚本（批次 41 重制）。

v19 原版（720 面「驾驶室盒 + 货厢盒」）→ 批次 41 重制：
  - 驾驶室 4 站放样（COE 平头：近竖大风挡 rake、前脸渐收、顶 2.90 m）+
    顶导流罩（前高后低两段，衔接货厢高度）；
  - 货厢 0.53 长（5.3 m）浅灰箱体：两侧各 7 条 + 尾部 3 条竖向楞条（瓦楞感）、
    后双开门线 + 竖把 + 红白反光条；
  - 底盘纵梁（深色）+ 侧挂油箱（镀铬圆柱）+ 立式排气管（驾驶室右后）；
  - 车轮 ×6（前轴 + 双后轴 tandem，⌀1.0 m）、前轴轮拱 boolean 开口 +
    后双轴挡泥板；风挡/侧窗 alpha 0.5 透明 + 车内座椅剪影。

真实比例参考（检索结论，2026-10-01）：COE 平头卡车驾驶室高 2.26-2.9 m
（Isuzu NPR 2.26，中型更高）；本车 8.5 m 中卡：驾驶室 2.9 m、货厢顶 3.4 m；
轮 ⌀1.0 m；车架离地 0.55-0.7 m；前轴 +3.0 m、双后轴 −1.4/−3.0 m。

对应组件 ClientWeb/src/components/virtualCity/props/Vehicle.tsx（变体 'truck'）。
坐标与尺度规约与 v19 一致（Blender Z-up / 1 单位 = 10 m / 导出 Yup）。
目标包围盒（世界单位）：X 0.850 × Z 0.250 × Y 0.340，轮底 minY = 0。
导出 2 个涂装：truck.glb（红）+ truck_white.glb（白，警用/市政系）。
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
L, W, H = 0.850, 0.250, 0.340              # 目标包围盒（REAL_DIMS_M.truck）
CAB_TOP = 0.290                            # 驾驶室顶（COE 中卡 2.9 m）
CAB_FRONT, CAB_REAR = 0.425, 0.125         # 驾驶室 x 范围（3.0 m 长）
WHEEL_R, WHEEL_W = 0.050, 0.030            # 胎 ⌀1.0 m / 胎宽 0.30 m
WHEEL_X_F, WHEEL_X_R1, WHEEL_X_R2 = +0.30, -0.14, -0.30
ARCH_R = 0.058
WHEEL_Y = W / 2 - WHEEL_W / 2 - 0.004      # 胎外缘内收（0.121 < 车宽半 0.125）

# ── 驾驶室站表（x, a=半宽, z0=底, z1=顶；n=4.5 圆角矩形）────────────────
CAB_STATIONS = [
    dict(x=+0.425, a=0.112, z0=0.055, z1=0.285),
    dict(x=+0.400, a=0.120, z0=0.055, z1=0.290),
    dict(x=+0.200, a=0.122, z0=0.055, z1=0.290),
    dict(x=+0.125, a=0.120, z0=0.055, z1=0.285),
]
N_LOOP = 20


def build_truck_family(cab_hex: str):
    """按驾驶室涂装色构建整车（join 后单 object）。truck_white 复用换色。"""
    parts = []

    # ── 材质（语义共享实例）───────────────────────────────────────────────
    mat_cab = make_material('CabPaint', cab_hex, 0.35, 0.4)
    mat_cargo = make_material('Cargo', CITY_PALETTE['truck_cargo'], 0.6, 0.1)
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

    # ── 驾驶室放样 + 前轴轮拱 ────────────────────────────────────────────
    cab = loft_stations(
        [dict(x=s['x'], a=s['a'], zc=(s['z0'] + s['z1']) / 2,
              b=(s['z1'] - s['z0']) / 2, n=4.5) for s in CAB_STATIONS],
        N_LOOP, 'Cab')
    wheel_well_cut(cab, WHEEL_X_F, WHEEL_R, ARCH_R, W + 0.04)
    assign_material(cab, mat_cab)
    parts.append(cab)

    # ── 顶导流罩（两段：前段高 0.318 / 后段衔接货厢 0.335）───────────────
    defl1 = make_box('DeflectorF', (0.10, 0.20, 0.026), (0.185, 0.0, 0.3020))
    defl2 = make_box('DeflectorR', (0.05, 0.20, 0.020), (0.110, 0.0, 0.3250))
    assign_material(defl1, mat_cab)
    assign_material(defl2, mat_cab)
    parts += [defl1, defl2]

    # ── 风挡（近竖 rake 8°）+ 侧窗 ×2 + 车内座椅 ─────────────────────────
    ws = make_box('Windshield', (0.005, 0.196, 0.110), (0.416, 0.0, 0.200))
    ws.rotation_euler = (0.0, math.radians(8.0), 0.0)
    assign_material(ws, mat_glass)
    parts.append(ws)
    for sy in (1, -1):
        sw = make_box(f'SideWindow_{sy}', (0.100, 0.0012, 0.075),
                      (0.300, sy * 0.1225, 0.200))
        assign_material(sw, mat_glass)
        parts.append(sw)
    for sy in (1, -1):
        seat = make_box(f'IntSeat_{sy}', (0.045, 0.042, 0.045),
                        (0.240, sy * 0.033, 0.150))
        assign_material(seat, mat_dark)
        parts.append(seat)

    # ── 格栅（3 镀铬横条）+ 保险杠 + 大灯 + 车牌 ──────────────────────────
    for k in range(3):
        gr = make_box(f'Grille{k}', (0.004, 0.194, 0.012),
                      (0.4215, 0.0, 0.176 + k * 0.020))
        assign_material(gr, mat_chrome)
        parts.append(gr)
    intake = make_box('Intake', (0.004, 0.200, 0.030), (0.4215, 0.0, 0.118))
    assign_material(intake, mat_dark)
    parts.append(intake)
    bumper = make_box('Bumper', (0.020, 0.238, 0.045), (0.415, 0.0, 0.0775))
    assign_material(bumper, mat_dark)
    parts.append(bumper)
    for sy in (1, -1):
        hl = make_box(f'Headlight_{sy}', (0.005, 0.026, 0.018),
                      (0.4235, sy * 0.078, 0.075))
        assign_material(hl, mat_head)
        parts.append(hl)
    plate_f = make_box('PlateF', (0.002, 0.040, 0.011), (0.4245, 0.0, 0.129))
    plate_r = make_box('PlateR', (0.002, 0.040, 0.011), (-0.4245, 0.0, 0.060))
    assign_material(plate_f, mat_plate)
    assign_material(plate_r, mat_plate)
    parts += [plate_f, plate_r]

    # ── 货厢（5.3 m 浅灰箱体）+ 瓦楞楞条 + 后门线/把/反光条 ─────────────
    cargo = make_box('CargoBox', (0.530, 0.235, 0.245), (-0.160, 0.0, 0.2175))
    assign_material(cargo, mat_cargo)
    parts.append(cargo)
    for sy in (1, -1):
        for k in range(7):
            rib = make_box(f'Rib_{sy}_{k}', (0.004, 0.0012, 0.220),
                           (-0.380 + k * 0.073, sy * 0.1185, 0.220))
            assign_material(rib, mat_dark)
            parts.append(rib)
    for k in range(3):
        rib = make_box(f'RibR_{k}', (0.0012, 0.060, 0.220),
                       (-0.4235, -0.058 + k * 0.058, 0.220))
        assign_material(rib, mat_dark)
        parts.append(rib)
    doorline = make_box('RearDoorLine', (0.0015, 0.002, 0.220), (-0.4235, 0.0, 0.220))
    assign_material(doorline, mat_dark)
    parts.append(doorline)
    for sy in (1, -1):
        handle = make_box(f'RearHandle_{sy}', (0.003, 0.010, 0.020),
                          (-0.4245, sy * 0.030, 0.150))
        assign_material(handle, mat_chrome)
        parts.append(handle)
    refl_r = make_box('ReflectorR', (0.002, 0.150, 0.012), (-0.4245, 0.0, 0.110))
    mat_refl = make_material('ReflectorRed', '#c01010', 0.4, 0.0,
                             emissive='#ff2418', emissive_intensity=0.35)
    assign_material(refl_r, mat_refl)
    parts.append(refl_r)
    refl_w = make_box('ReflectorW', (0.002, 0.150, 0.006), (-0.4245, 0.0, 0.098))
    assign_material(refl_w, mat_plate)
    parts.append(refl_w)
    for sy in (1, -1):
        tl = make_box(f'Taillight_{sy}', (0.004, 0.016, 0.052),
                      (-0.4245, sy * 0.098, 0.155))
        assign_material(tl, mat_tail)
        parts.append(tl)

    # ── 底盘纵梁 + 油箱 + 储气罐 + 立式排气管 ────────────────────────────
    rail = make_box('ChassisRail', (0.780, 0.150, 0.028), (0.0, 0.0, 0.070))
    assign_material(rail, mat_dark)
    parts.append(rail)
    tank = make_cylinder('FuelTank', 0.030, 0.030, 0.100, 16, (-0.020, -0.100, 0.075),
                         rot=(0.0, math.pi / 2, 0.0))
    assign_material(tank, mat_chrome)
    parts.append(tank)
    airt = make_cylinder('AirTank', 0.018, 0.018, 0.080, 12, (0.020, -0.108, 0.048),
                         rot=(0.0, math.pi / 2, 0.0))
    assign_material(airt, mat_chrome)
    parts.append(airt)
    stack = make_cylinder('ExhaustStack', 0.010, 0.010, 0.240, 12,
                          (0.112, 0.108, 0.1775))
    assign_material(stack, mat_chrome)
    parts.append(stack)

    # ── 后视镜（外缘 ≤ 0.129 ⇒ 总宽 0.258 ≤ 0.250+5%）───────────────────
    for sy in (1, -1):
        arm = make_box(f'MirrorArm_{sy}', (0.004, 0.006, 0.003),
                       (0.400, sy * 0.1255, 0.245))
        shell = make_box(f'MirrorShell_{sy}', (0.016, 0.005, 0.048),
                         (0.400, sy * 0.1275, 0.255))
        assign_material(arm, mat_cab)
        assign_material(shell, mat_dark)
        parts += [arm, shell]

    # ── 后双轴挡泥板（每轴一块横跨全宽的暗板，防泥水飞溅观感）────────────
    for ax in (WHEEL_X_R1, WHEEL_X_R2):
        flap = make_box(f'MudFlap_{ax}', (0.004, 0.200, 0.058),
                        (ax - 0.075, 0.0, 0.029))
        assign_material(flap, mat_dark)
        parts.append(flap)

    # ── 车轮 ×6（前轴 + 双后轴 tandem）──────────────────────────────────
    for name, wx, sy in (
        ('WheelFL', WHEEL_X_F, +1), ('WheelFR', WHEEL_X_F, -1),
        ('WheelRL1', WHEEL_X_R1, +1), ('WheelRR1', WHEEL_X_R1, -1),
        ('WheelRL2', WHEEL_X_R2, +1), ('WheelRR2', WHEEL_X_R2, -1),
    ):
        for p in build_wheel(name, wx, sy * WHEEL_Y, WHEEL_R, WHEEL_R, WHEEL_W):
            is_hub = '_Hub' in p.name or '_Spoke' in p.name
            is_tire = p.name.endswith('_Tire')
            assign_material(p, mat_chrome if is_hub else
                            (mat_tire if is_tire else mat_dark))
            parts.append(p)

    return join_objects(parts, 'VehicleTruck')


def _export(obj, out_path: str) -> None:
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    export_glb(out_path)


if __name__ == '__main__':
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv \
        else '/tmp/vehicle_truck.glb'
    white_path = os.path.join(os.path.dirname(out_path), 'truck_white.glb')

    reset_scene()
    set_unit_meters()
    _export(build_truck_family(CITY_PALETTE['truck_red']), out_path)
    print(f'[build_vehicle_truck] wrote {out_path}', flush=True)

    reset_scene()
    set_unit_meters()
    _export(build_truck_family('#d8dade'), white_path)
    print(f'[build_vehicle_truck] wrote {white_path}', flush=True)
