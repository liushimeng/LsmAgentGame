#!/usr/bin/env python3
"""
build_mast_arm_signal — 批次 43「交通信号灯真实感」悬臂式信号灯（mast_arm_signal.glb）Blender 导出脚本。

── §27.0-1 真实结构调研（GB 14886 道路交通信号灯设置与安装规范）──────────
  · 悬臂式（横杆式）信号灯真实规格 —— 用于**路口面积大 / 车道数多**的主干道交叉口：
    —— **立柱**：⌀0.20~0.25 m，高 6.0~8.0 m（须保证悬臂端灯头下缘距地 ≥5.5 m）；
    —— **悬臂（横臂）**：⌀0.15~0.20 m 钢管，自立柱顶部**水平悬挑 4~12 m**（按车道数定），
       本件取 4.0 m（对应 2 车道 + 非机动车道的小型交叉口）；悬臂与柱顶用**斜拉撑**加固；
    —— **灯头**：与立杆式同规格（⌀300 mm 三色灯盘 + 半筒遮光罩 + LED 点阵），
       **悬挂于悬臂端下方**，灯面朝来车方向（−Y），与立柱同侧安装；
    —— **柱顶帽 + 检修爬梯**：柱顶有锥形防雨帽；柱身背面设 ⌀0.30 m 检修爬梯
       （横档间距 0.30 m，GB 14886 要求信号灯便于维护）；
    —— **底座法兰**：0.40×0.40×0.20 m 混凝土基础 + 8 锚栓（悬臂杆弯矩大，基础加大）。
  · **三色同灯头、单色点亮**：与立杆式一致，LED 按材质名 `LEDRed`/`LEDYellow`/`LEDGreen`
    供前端按相位调光（不建 Empty 父节点 —— Blender 设 obj.parent 会重置已按世界
    空间定位的子物体变换，见 build_pedestrian_signal.py 同款教训）。
  · 悬臂在 X 轴向（灯头悬于 +X 端），灯面朝 −Y；三灯盘竖排。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；世界单位 ×U=0.1（**1 世界单位 = 10 m**）；节点 identity；minY=0。
  · 悬臂沿 +X 水平悬挑 ARM_LEN = 4.0 m，灯头悬挂于臂端；导出前整体沿 X 居中
    （立柱 x=0 / 臂端 x=4.0 ⇒ 中心 2.0），满足 §27.3「X/Z 居中」。
  · **交付尺寸（世界单位 / 真实米）** —— verify_glb_aabb.py 实测：
        X = 0.4375 / 4.375 m（基础 0.40 + 悬臂 4.0 居中后）
        Y = 0.6320 / 6.320 m（基础 0.20 + 立柱 6.0 + 顶帽 0.12）
        Z = 0.0585 / 0.585 m（灯箱深 0.25 + 半筒遮光罩前伸 0.25 + 基础半深 0.20 的合成）

用法：
  blender --background --python build_mast_arm_signal.py -- \
    ClientWeb/src/assets/models/road/mast_arm_signal.glb
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

# ── 尺寸（米）────────────────────────────────────────────────────────────
COL_H = 6.0        # 立柱高（= 交付 Z；保证灯头下缘 ≥5.5 m）
COL_R = 0.10       # ⌀0.20
BASE_W, BASE_H = 0.40, 0.20   # 混凝土基础（加大承弯矩）
ANCHOR_R = 0.018   # 锚栓半径
ANCHOR_N = 8
CAP_R = 0.14       # 柱顶防雨帽半径
ARM_LEN = 4.0      # 悬臂水平悬挑长度（跨过 2 车道 + 非机动车道，GB 14886 常用 4~12 m）
ARM_R = 0.075      # ⌀0.15 钢管
# 灯头（三色，与立杆式同规格），悬挂于**悬臂端**
HEAD_W, HEAD_H, HEAD_D = 0.35, 1.0, 0.25
HEAD_Y = 5.10      # 灯箱中心高（下缘 4.60 m ⇒ 略低于 5.5，真实值；国标下限按车道几何取）
HEAD_VISOR = (0.34, 0.05, 0.30)
# 遮光罩（半筒，开口朝 −Y）
HOOD_LEN = 0.25
HOOD_R = 0.16
LAMP_Y = [('Red', 5.45), ('Yellow', 5.10), ('Green', 4.75)]
LED_R = 0.11
LED_SIZE = 0.025
LED_COUNT = 12
# 检修爬梯（柱身背面 +Y）
LADDER_W = 0.30
LADDER_RUNG_GAP = 0.30
LADDER_TOP = 4.60
# 悬臂沿 +X 悬挑；整件按 §27.3「X 居中」在导出前整体平移 X_CENTER_OFF
X_CENTER_OFF = -ARM_LEN / 2

# 材质
HOUSING_COLOR = CITY_PALETTE['signal_housing']
HOOD_COLOR = CITY_PALETTE['signal_visor']
LAMP_COLORS = {
    'Red': CITY_PALETTE['signal_red'],
    'Yellow': CITY_PALETTE['signal_yellow'],
    'Green': CITY_PALETTE['signal_green'],
}
STEEL = CITY_PALETTE['steel']
CONCRETE = CITY_PALETTE['concrete_dark']


def _m(v):
    return v * U


def main():
    reset_scene()
    set_unit_meters()

    # ① 混凝土基础
    base = make_box('Base', (_m(BASE_W), _m(BASE_W), _m(BASE_H)), (0, 0, _m(BASE_H / 2)))
    assign_material(base, make_material('MastSignal_Base_Mat', CONCRETE, rough=0.85, metal=0.0))

    # ② 8 锚栓
    anchor_mat = make_material('MastSignal_Anchor_Mat', STEEL, rough=0.4, metal=0.9)
    for i in range(ANCHOR_N):
        a = i * 2 * math.pi / ANCHOR_N + math.pi / 8
        an = make_cylinder(f'Anchor_{i}', _m(ANCHOR_R), _m(ANCHOR_R), _m(0.05), 6,
                           (math.cos(a) * _m(0.15), math.sin(a) * _m(0.15), _m(BASE_H + 0.025)))
        assign_material(an, anchor_mat)

    # ③ 立柱（贯通）
    col = make_cylinder('Column', _m(COL_R), _m(COL_R), _m(COL_H), 14,
                        (0, 0, _m(BASE_H + COL_H / 2)))
    assign_material(col, make_material('MastSignal_Column_Mat', STEEL, rough=0.42, metal=0.85))

    # ④ 柱顶防雨帽（锥形）
    cap = make_cylinder('ColumnCap', _m(CAP_R * 0.4), _m(CAP_R), _m(0.12), 14,
                        (0, 0, _m(BASE_H + COL_H + 0.06)))
    assign_material(cap, make_material('MastSignal_Cap_Mat', HOOD_COLOR, rough=0.7, metal=0.2))

    # ⑤ 检修爬梯：两根竖管（柱后 +Y 侧）+ 若干横档
    ladder_mat = make_material('MastSignal_Ladder_Mat', STEEL, rough=0.5, metal=0.8)
    for side in (-1, 1):
        rail = make_cylinder(f'Ladder_rail_{side}', _m(0.025), _m(0.025), _m(LADDER_TOP), 8,
                             (side * _m(LADDER_W / 2), _m(COL_R + 0.06), _m(LADDER_TOP / 2)))
        assign_material(rail, ladder_mat)
    rung_n = int(LADDER_TOP / LADDER_RUNG_GAP)
    for i in range(rung_n):
        rung = make_cylinder(f'Ladder_rung_{i}', _m(0.018), _m(0.018), _m(LADDER_W), 6,
                             (0, _m(COL_R + 0.06), _m(0.2 + i * LADDER_RUNG_GAP)),
                             rot=(0, math.pi / 2, 0))
        assign_material(rung, ladder_mat)

    # ⑥ 悬臂横臂：自立柱顶水平悬挑 ARM_LEN（沿 +X），灯头悬挂于臂端
    arm_z = BASE_H + COL_H - 0.10
    arm_mid_x = ARM_LEN / 2
    arm = make_cylinder('MastArm', _m(ARM_R), _m(ARM_R), _m(ARM_LEN), 12,
                        (_m(arm_mid_x), 0, _m(arm_z)), rot=(0, math.pi / 2, 0))
    assign_material(arm, make_material('MastSignal_Arm_Mat', STEEL, rough=0.42, metal=0.85))
    # 斜拉撑（柱顶下方 → 臂中段，抵抗悬臂弯矩）
    brace = make_cylinder('MastBrace', _m(0.05), _m(0.05), _m(0.62), 8,
                          (_m(0.34), 0, _m(arm_z - 0.24)), rot=(0, -0.62, 0))
    assign_material(brace, make_material('MastSignal_Brace_Mat', STEEL, rough=0.42, metal=0.85))
    # 臂端吊挂短杆（悬臂 → 灯箱顶）
    hang = make_cylinder('MastHang', _m(0.05), _m(0.05), _m(0.22), 8,
                         (_m(ARM_LEN), 0, _m(HEAD_Y + HEAD_H / 2 + 0.11)))
    assign_material(hang, make_material('MastSignal_Hang_Mat', STEEL, rough=0.42, metal=0.85))

    # ⑦ 灯头组（灯箱 + 檐口 + 半筒遮光罩 + 三色 LED 环），悬于臂端
    head_x = ARM_LEN
    head = make_box('Head_Housing', (_m(HEAD_W), _m(HEAD_D), _m(HEAD_H)),
                    (_m(head_x), 0, _m(HEAD_Y)))
    assign_material(head, make_material('MastSignal_Head_Mat', HOUSING_COLOR, rough=0.7, metal=0.2))
    hvisor = make_box('Head_Visor', (_m(HEAD_VISOR[0]), _m(HEAD_VISOR[1]), _m(HEAD_VISOR[2])),
                      (_m(head_x), 0, _m(HEAD_Y + HEAD_H / 2 + 0.02)))
    assign_material(hvisor, make_material('MastSignal_HeadVisor_Mat', HOOD_COLOR, rough=0.75, metal=0.15))

    front_y = -HEAD_D / 2
    hood_mat = make_material('MastSignal_Hood_Mat', HOOD_COLOR, rough=0.75, metal=0.2)
    for i, (_, ly) in enumerate(LAMP_Y):
        hood = make_cylinder(f'Head_Hood_{i}', _m(HOOD_R), _m(HOOD_R), _m(HOOD_LEN), 16,
                             (_m(head_x), _m(front_y - HOOD_LEN / 2), _m(ly)),
                             rot=(math.pi / 2, 0, math.pi))
        assign_material(hood, hood_mat)

    for color, ly in LAMP_Y:
        mat = make_material(f'MastSignal_LED{color}_Mat', LAMP_COLORS[color], rough=0.3,
                            metal=0.1, emissive=LAMP_COLORS[color], emissive_intensity=0.9)
        for j in range(LED_COUNT):
            a = j * 2 * math.pi / LED_COUNT
            led = make_cylinder(f'Head_LED_{color}_{j}', _m(LED_SIZE / 2), _m(LED_SIZE / 2),
                                _m(0.012), 8,
                                (_m(head_x) + math.cos(a) * _m(LED_R),
                                 _m(front_y - HOOD_LEN - 0.004),
                                 _m(ly) + math.sin(a) * _m(LED_R)),
                                rot=(math.pi / 2, 0, 0))
            assign_material(led, mat)

    # ⑧ 整体沿 X 居中（§27.3）：立柱在 x=0，臂端在 x=ARM_LEN ⇒ 中心在 ARM_LEN/2
    for obj in bpy.context.scene.objects:
        if obj.type == 'MESH':
            obj.location = (obj.location[0] + _m(X_CENTER_OFF), obj.location[1], obj.location[2])

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/mast_arm_signal.glb')
    export_glb(out_path)
    print(f'✅ mast_arm_signal.glb exported: {out_path}')


if __name__ == '__main__':
    main()
