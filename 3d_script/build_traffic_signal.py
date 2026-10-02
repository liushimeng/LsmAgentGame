#!/usr/bin/env python3
"""
build_traffic_signal — 批次 43「交通信号灯真实感」机动车信号灯（traffic_signal.glb）Blender 导出脚本。

── §27.0-1 真实结构调研（GB 14886 / GB 14887 道路交通信号灯）────────────────
  · 机动车信号灯（竖式三色灯）真实规格：
    —— 灯盘直径 300 mm（⌀0.30 m，GB 14887 标准档）；
    —— 红 / 黄 / 绿三色灯**竖向排列于同一灯头**，单组灯盘含边框总高约 1.0 m；
    —— 遮光罩（遮阳罩）：⌀300 mm 灯盘的筒罩自灯箱前表面**前伸 0.20~0.30 m**
       （约 0.8 倍灯径，兼作侧向遮阳 + 防雨）；黑色金属，前端开口；
    —— 灯箱：宽 0.35 m × 高 1.0 m × 深 0.25 m，近黑箱体；
    —— 立杆：⌀0.15 m × 高 5.5 m（信号灯下缘距地面 ≥5.5 m，GB 14886 安装要求）；
    —— LED 点阵面：每个灯盘内 12 颗小 LED 环形排布（国标 LED 阵列）；
    —— 灯箱顶沿出檐檐口 0.34×0.05×0.30 m；底座法兰 0.20²×0.10 m + 4 膨胀螺栓。
  · **单灯头三色（真实性核心）**：真实信号灯**同一时刻只点亮一色**，另两色为熄灭
    暗态（灯盘罩体仍在，仅 LED 不发光）。故本 GLB 出单个灯头，三色 LED 分组为
    三个可命名节点组 `LED_Red` / `LED_Yellow` / `LED_Green`（Empty 父节点），
    前端按相位对三组分别调制 emissive（沿用 TrafficSignals 既有 setColorAt 架构）。
    **不**做成三个空间变体——那样同屏会出现三只灯头，不符合真实。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；灯面朝 glTF +Z（= Blender −Y）。
  · 世界单位 ×U=0.1；节点 identity（export_glb 内 bake_transforms 兜底）。
  · X/Z 居中、minY=0；交付尺寸：
        x = 0.035 / y = 0.550 / z = 0.053  （真实 0.35 × 5.50 × 0.53 m）
    进深 0.53 m 口径 = 灯箱深 0.25 + 遮光罩前伸 0.25 + LED 面 0.03。

用法：
  blender --background --python build_traffic_signal.py -- \
    ClientWeb/src/assets/models/road/traffic_signal.glb
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
    export_glb, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位

# ── 信号灯尺寸（米）──────────────────────────────────────────────────────
POLE_H = 5.5       # 杆高（= 交付 Y；GB 14886 安装高度下限）
POLE_R = 0.075     # ⌀0.15
BASE_W, BASE_H = 0.20, 0.10   # 法兰底座
BOLT_OFF, BOLT_R, BOLT_H = 0.07, 0.012, 0.03
HOUSING_W, HOUSING_H, HOUSING_D = 0.35, 1.0, 0.25
HOUSING_Y = 4.7    # 灯箱中心高
VISOR_W, VISOR_H, VISOR_D = 0.34, 0.05, 0.30
VISOR_Y = 5.26     # 灯箱顶沿出檐檐口中心高
# 遮光罩：自灯箱前表面（y = −HOUSING_D/2 = −0.125）向前伸 0.25 m
HOOD_LEN = 0.25
HOOD_R = 0.16      # ⌀0.32 筒（略大于 ⌀0.30 灯盘，罩住盘缘）
# 三灯盘中心高：红上 / 黄中 / 绿下，竖向间隔 0.35 m（键序即红黄绿）
LAMP_Y = [('Red', 5.05), ('Yellow', 4.7), ('Green', 4.35)]
LED_R = 0.11       # LED 环形排布半径
LED_SIZE = 0.025   # 单颗 LED 直径
LED_COUNT = 12

# 材质基色（CITY_PALETTE 批次 43 信号灯族）
HOUSING_COLOR = CITY_PALETTE['signal_housing']
HOOD_COLOR = CITY_PALETTE['signal_visor']
LAMP_COLORS = {
    'Red': CITY_PALETTE['signal_red'],
    'Yellow': CITY_PALETTE['signal_yellow'],
    'Green': CITY_PALETTE['signal_green'],
}


def _m(v):
    return v * U


def main():
    reset_scene()
    set_unit_meters()
    steel = CITY_PALETTE['steel']

    # ① 立杆（贯穿全高）
    pole = make_cylinder('Pole', _m(POLE_R), _m(POLE_R), _m(POLE_H), 16,
                         (0, 0, _m(POLE_H / 2)))
    assign_material(pole, make_material('Signal_Pole_Mat', steel, rough=0.42, metal=0.85))

    # ② 法兰底座 + 4 膨胀螺栓
    base = make_box('Base', (_m(BASE_W), _m(BASE_W), _m(BASE_H)), (0, 0, _m(BASE_H / 2)))
    assign_material(base, make_material('Signal_Base_Mat', steel, rough=0.5, metal=0.8))
    bolt_mat = make_material('Signal_Bolt_Mat', steel, rough=0.35, metal=0.9)
    for i in range(4):
        a = i * math.pi / 2 + math.pi / 4
        bolt = make_cylinder(f'Bolt_{i}', _m(BOLT_R), _m(BOLT_R), _m(BOLT_H), 8,
                             (math.cos(a) * _m(BOLT_OFF), math.sin(a) * _m(BOLT_OFF),
                              _m(BASE_H + BOLT_H / 2)))
        assign_material(bolt, bolt_mat)

    # ③ 灯箱（近黑箱体）
    housing = make_box('Housing', (_m(HOUSING_W), _m(HOUSING_D), _m(HOUSING_H)),
                       (0, 0, _m(HOUSING_Y)))
    assign_material(housing, make_material('Signal_Housing_Mat', HOUSING_COLOR,
                                           rough=0.7, metal=0.2))

    # ④ 灯箱顶沿出檐檐口
    visor = make_box('VisorTop', (_m(VISOR_W), _m(VISOR_D), _m(VISOR_H)), (0, 0, _m(VISOR_Y)))
    assign_material(visor, make_material('Signal_Visor_Mat', HOOD_COLOR, rough=0.75, metal=0.15))

    # ⑤ 三个遮光罩（**开口朝 −Y 的半筒**，自灯箱前表面前伸）
    #    ⚠️ 不可用实心圆柱：会遮死罩内的 LED 环面（首版即此缺陷）。
    #    做法：整筒按 π 角度旋转到只剩**下半圈 + 后壁**，
    #    顶点俯视呈「U」形，正面完全敞开。
    front_y = -HOUSING_D / 2
    hood_mat = make_material('Signal_Hood_Mat', HOOD_COLOR, rough=0.75, metal=0.2)
    for i, (_, ly) in enumerate(LAMP_Y):
        hood = make_cylinder(f'Hood_{i}', _m(HOOD_R), _m(HOOD_R), _m(HOOD_LEN), 16,
                             (0, _m(front_y - HOOD_LEN / 2), _m(ly)),
                             rot=(math.pi / 2, 0, math.pi))
        assign_material(hood, hood_mat)

    # ⑥ 三灯盘 LED 点阵（每盘 12 颗环形），按色分材质供前端按**材质名** LEDRed /
    #    LEDYellow / LEDGreen 命中调光（同批次 41 车辆灯机制）。
    #    ⚠️ 不建 Empty 父节点：Blender 设 obj.parent 会重置已按世界空间定位的子物体
    #    变换（matrix_parent_inverse 默认单位阵），会把 LED 甩离灯盘。
    for color, ly in LAMP_Y:
        mat = make_material(f'Signal_LED{color}_Mat', LAMP_COLORS[color], rough=0.3,
                            metal=0.1, emissive=LAMP_COLORS[color], emissive_intensity=0.9)
        for j in range(LED_COUNT):
            a = j * 2 * math.pi / LED_COUNT
            led = make_cylinder(f'LED_{color}_{j}', _m(LED_SIZE / 2), _m(LED_SIZE / 2), _m(0.012), 8,
                                (math.cos(a) * _m(LED_R),
                                 _m(front_y - HOOD_LEN - 0.004),
                                 _m(ly) + math.sin(a) * _m(LED_R)),
                                rot=(math.pi / 2, 0, 0))
            assign_material(led, mat)

    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/traffic_signal.glb'
    export_glb(out_path)
    print(f'✅ traffic_signal.glb exported: {out_path}')


if __name__ == '__main__':
    main()
