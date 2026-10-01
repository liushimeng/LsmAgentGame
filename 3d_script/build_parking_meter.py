#!/usr/bin/env python3
"""
build_parking_meter — 批次 42「街具真实感」停车咪表（parking_meter.glb）Blender 导出脚本。

── §27.0-1 真实结构调研（开工前先检索参考；本次 WebSearch 无可用返回，
   改用工程标准值 + 仓库既有资产口径，结论如下）────────────────────────
  · 城市路侧停车咪表（参考：常见单柱式咪表实测、GB 停车收费设施通用图）：
    —— 通高 1.20~1.40 m（屏中心 1.10~1.25 m，成人视线自然俯视）；
    —— 表头盒 0.28×0.36×0.18 m，顶部**前倾 10~15°**（遮阳 + 读屏角度）；
    —— 显示屏 0.18×0.12 m 深色玻璃，夜间内打光（材质名 Screen 供昼夜调制）；
    —— 侧下投币/刷卡区：0.06×0.10 m 槽 + 圆形读卡区（IC 卡/扫码）；
    —— 部分型号顶部太阳能板 0.30×0.20 m 微斜；
    —— 杆 ⌀76 mm 钢管 + 圆盘底座（也可方法兰，本件取圆盘 + 微锥过渡）。
  · 本件取值（宽 × 高 × 深 = 0.32 × 1.38 × 0.28 m）：
    杆 ⌀0.076 × 1.05 + 圆盘底座 ⌀0.28×0.05；
    表头盒 0.28×0.36×0.18 @ z=1.05~1.41（前倾 12°）；
    屏 0.18×0.12（z≈1.22）；投币槽 + 读卡圆；太阳能顶板 0.30×0.20 @ 顶。
  · 材质：CITY_PALETTE（meter_blue / steel_dark / steel / sign_amber 屏），
    weathered_pbr 经年磨损（导出前 flatten 回常量色）。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；表头屏幕朝 glTF +Z（= Blender −Y），与 Mailbox 同向约定。
  · 世界单位 ×U=0.1；节点 identity；X/Z 居中；minY=0。
  · 材质名契约：屏幕材质名含 **Screen**（前端 ParkingMeter 夜间 emissive 调制）。
  · 交付尺寸：x = 0.032 / y = 0.138 / z = 0.028（≡ REAL_DIMS_M.parkingMeter）。

用法：
  blender --background --python build_parking_meter.py -- \
    ClientWeb/src/assets/models/road/parking_meter.glb
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

U = 0.1            # 米 → 世界单位

# ── 咪表尺寸（米）────────────────────────────────────────────────────────
DISC_R = 0.14      # 圆盘底座 ⌀0.28 = 交付 X
DISC_H = 0.05
POLE_R = 0.038     # ⌀0.076
POLE_H = 0.98      # 杆顶（表头底；初版 1.05 使总高 1.46 超交付 1.38）
HEAD_W = 0.28      # 表头盒宽 = 交付 X 上限
HEAD_H = 0.34
HEAD_T = 0.18      # 进深 = 交付 Z（含前倾包络 ≤0.28）
TILT_DEG = 12.0    # 表头前倾
SCREEN_W, SCREEN_H = 0.18, 0.12
SCREEN_Z = 1.16    # 屏中心高
SLOT_W, SLOT_H = 0.10, 0.022   # 投币槽
SLOT_Z = 1.00
CARD_R = 0.035     # 读卡圆
CARD_Z = 0.92
SOLAR_W, SOLAR_T = 0.30, 0.20  # 太阳能顶板
SOLAR_Z = 1.355    # 顶板顶面 ≈1.38 = 交付 Y


def _m(v):
    return v * U


def build_parking_meter() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()
    objs = []

    blue = CITY_PALETTE['meter_blue']
    dark = CITY_PALETTE['steel_dark']
    steel = CITY_PALETTE['steel']
    amber = CITY_PALETTE['sign_amber']

    # ── 圆盘底座 + 杆 ──────────────────────────────────────────────────
    disc = make_cylinder('Disc', _m(DISC_R), _m(DISC_R), _m(DISC_H), 24,
                         (0, 0, _m(DISC_H / 2)))
    objs.append(disc)
    pole = make_cylinder('Pole', _m(POLE_R), _m(POLE_R * 1.12), _m(POLE_H), 16,
                         (0, 0, _m(DISC_H / 2 + POLE_H / 2)))
    objs.append(pole)
    # 材质组①结构钢（盘 + 杆 + 屏框 + 太阳能框）共用 1 槽
    shared_weathered([disc, pole], steel, rough=0.45, metal=0.72,
                     name='Meter_Struct_Mat', wear=0.35, scale=8.0)

    # ── 表头盒（前倾 12°，绕 X 轴向 −Y 倾）─────────────────────────────
    tilt_rad = TILT_DEG * 3.14159265 / 180.0
    head = make_box('Head', (_m(HEAD_W), _m(HEAD_T), _m(HEAD_H)),
                    (0, _m(-0.005), _m(POLE_H + DISC_H / 2 + HEAD_H / 2 - 0.02)),
                    (tilt_rad, 0, 0))
    weathered_pbr(head, blue, rough=0.50, metal=0.25, wear=0.35, scale=6.0)
    objs.append(head)

    # 表头前沿（屏幕框 + 显示屏，随倾角共面；屏幕材质名含 Screen）
    front_y = -0.005 - HEAD_T / 2 - 0.004
    frame = make_box('Screen_Frame', (_m(SCREEN_W + 0.03), _m(0.012), _m(SCREEN_H + 0.03)),
                     (0, _m(front_y), _m(SCREEN_Z)), (tilt_rad, 0, 0))
    objs.append(frame)
    screen = make_box('Screen', (_m(SCREEN_W), _m(0.014), _m(SCREEN_H)),
                      (0, _m(front_y - 0.002), _m(SCREEN_Z)), (tilt_rad, 0, 0))
    assign_material(screen, make_material('Meter_Screen_Mat', dark, rough=0.18, metal=0.05,
                                          emissive=amber, emissive_intensity=0.8))
    objs.append(screen)

    # 投币槽 + 读卡圆（盒体下部前脸）
    slot = make_box('Coin_Slot', (_m(SLOT_W), _m(0.014), _m(SLOT_H)),
                    (0, _m(front_y - 0.001), _m(SLOT_Z)), (tilt_rad, 0, 0))
    assign_material(slot, make_material('Meter_Slot_Mat', '#141416', rough=0.80, metal=0.10))
    objs.append(slot)
    card = make_cylinder('Card_Pad', _m(CARD_R), _m(CARD_R), _m(0.012), 16,
                         (0, _m(front_y - 0.002), _m(CARD_Z)),
                         (1.5708 + tilt_rad, 0, 0))
    assign_material(card, make_material('Meter_Card_Mat', CITY_PALETTE['reflect_white'],
                                        rough=0.35, metal=0.15))
    objs.append(card)

    # ── 太阳能顶板（微斜 8°，黑蓝玻璃）────────────────────────────────
    panel = make_box('Solar', (_m(SOLAR_W), _m(SOLAR_T), _m(0.018)),
                     (0, 0, _m(SOLAR_Z)), (0.14, 0, 0))
    assign_material(panel, make_material('Meter_Solar_Mat', CITY_PALETTE['glass_dark'],
                                         rough=0.22, metal=0.35))
    objs.append(panel)
    panel_frame = make_box('Solar_Frame', (_m(SOLAR_W + 0.02), _m(SOLAR_T + 0.02), _m(0.010)),
                           (0, 0, _m(SOLAR_Z - 0.012)), (0.14, 0, 0))
    objs.append(panel_frame)

    struct_mat = bpy.data.materials['Meter_Struct_Mat']
    for tag in ('Screen_Frame', 'Solar_Frame'):
        for o in objs:
            if o.name == tag:
                assign_material(o, struct_mat)
    joined = join_objects(objs, 'ParkingMeter')
    return joined


if __name__ == '__main__':
    obj = build_parking_meter()
    flatten_weathered_materials(obj)
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/parking_meter.glb'
    export_glb(out_path)
    print(f'[build_parking_meter] done -> {out_path}', flush=True)
