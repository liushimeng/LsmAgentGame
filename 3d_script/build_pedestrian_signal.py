#!/usr/bin/env python3
"""
build_pedestrian_signal — 批次 43「交通信号灯真实感」行人信号灯（pedestrian_signal.glb）Blender 导出脚本。

── §27.0-1 真实结构调研（GB 14886 / GB 14887 道路交通信号灯）────────────────
  · 行人信号灯真实规格：
    —— 灯盘 **方形** 0.30 × 0.30 m（GB 14887 行人信号灯标准档），双色显示：
       **红色站立人形**（禁止通行）/ **绿色行走人形**（允许通行）；
    —— 人形图案为实心剪影（头 = 圆 + 躯干/四肢折线），非像素字形；
    —— 部分型号带**倒计时显示器**（0.30 × 0.15 m，显示剩余秒数），装于灯盘下方；
    —— 立杆：⌀0.08 m × 高 2.5 m（灯盘中心高约 2.0~2.5 m，GB 14886 安装高度）；
    —— **行人过街请求按钮盒**：0.12 × 0.15 × 0.08 m 黄色箱体，立杆旁
      高约 1.0~1.2 m（便于手臂触及），带按钮面与防雨檐；
    —— 灯箱 + 遮光罩：方盘外加前伸遮阳罩（与机动车灯同款逻辑，尺度按盘径折算）。
  · **单灯头双色（真实性核心）**：与机动车灯同——同一时刻只点亮一色
    （红人亮 = 禁止通行；绿人亮 = 允许通行）。故双色 LED 分组为两个可命名节点组
    `LED_Red` / `LED_Green`（Empty 父节点），前端按相位对两组分别调制 emissive。
  · 按钮盒为**常亮提示态**（黄色箱体 + 微发光按钮面），夜间可视。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；灯面朝 glTF +Z（= Blender −Y）。
  · 世界单位 ×U=0.1；节点 identity（export_glb 内 bake_transforms 兜底）。
  · X/Z 居中、minY=0；交付尺寸：
        x = 0.32 / y = 0.250 / z = 0.115  （真实 0.32 × 2.50 × 1.15 m）
    x 0.32 口径 = 灯箱宽 0.30 + 按钮盒外探至 0.32（按钮盒侧挂）。

用法：
  blender --background --python build_pedestrian_signal.py -- \
    ClientWeb/src/assets/models/road/pedestrian_signal.glb
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
POLE_H = 2.5       # 立杆高（= 交付 Y）
POLE_R = 0.04      # ⌀0.08
BASE_W, BASE_H = 0.16, 0.08    # 法兰底座
HOUSING_W, HOUSING_H, HOUSING_D = 0.30, 0.30, 0.16   # 方盘灯箱
HOUSING_Y = 2.05   # 灯箱中心高（GB 14886 行人灯安装 2.0~2.5 m）
# 遮光罩：方盘外的前伸遮阳罩（按盘宽 0.30 折算 ≈ 0.8 倍）
HOOD_LEN = 0.12
HOOD_W, HOOD_H = 0.34, 0.34
# 人形图案（红人 / 绿人）：盘面内实心剪影，按 GB 14887 附图比例
FIG_W, FIG_H = 0.15, 0.23     # 人形整体宽高
FIG_Y = 0.0                   # 盘面局部 z 中心
# 倒计时显示器：0.22 × 0.11 m，嵌于灯箱下方（紧贴，不悬空）
CD_W, CD_H, CD_D = 0.20, 0.09, 0.04
CD_Y = 1.85                    # 倒计时箱中心高（灯箱底 1.90 ⇒ 紧下方）
# 行人过街请求按钮盒：0.12 × 0.15 × 0.08 m，侧挂于立杆
BTN_W, BTN_H, BTN_D = 0.08, 0.15, 0.12   # x 深 / y 宽 / z 高（盒体）
BTN_X = 0.10                   # 侧挂中心 x（+0.10 ⇒ 整件 x 半宽 0.10+0.04=0.14 ≈ 0.16 见底座）
BTN_Y = 1.10                   # 按钮盒中心高（手臂可及）
BTN_FACE_W, BTN_FACE_H = 0.045, 0.045      # 按钮面

# 材质基色（复用批次 43 信号灯族 + 街具族）
HOUSING_COLOR = CITY_PALETTE['signal_housing']
HOOD_COLOR = CITY_PALETTE['signal_visor']
RED_COLOR = CITY_PALETTE['signal_red']
GREEN_COLOR = CITY_PALETTE['signal_green']
CD_COLOR = CITY_PALETTE['sign_amber']       # 倒计时琥珀屏
BUTTON_YELLOW = '#e8b930'                   # 按钮盒警示黄（同工地围挡工程黄族）
BUTTON_FACE = CITY_PALETTE['reflect_white'] # 按钮面反光白
STEEL = CITY_PALETTE['steel']


def _m(v):
    return v * U


def _figure_parts(x0, y0, z0, color_tag, mat):
    """在 (x0, y0, z0) 处摆一组人形剪影（红人/绿人共形，仅色不同）。

    真实行人信号灯人形象（GB 14887 附图）比例要点 —— 这是「可辨识」的关键：
      · 头 **大而圆**，直径约占人形总高的 1/4（国标图标非写实人体）；
      · 躯干短粗，与头之间**留颈部空隙**（不与头粘连）；
      · 双臂**斜向外下方张开**，与躯干之间留可见间隙；
      · 双腿**并立略分开**，中间留缝。
    之前版本四肢过宽且偏移量重叠 ⇒ 糊成一块红砖，本次逐项拆开。
    """
    parts = []
    t = 0.010          # 剪影厚度（朝 ±Y）
    HEAD_R = 0.040     # 头半径 0.040 ⇒ ⌀0.080（占 FIG_H 0.23 的 1/2.9，接近国标观感）
    NECK_GAP = 0.008   # 头与躯干间隙

    def add(nm, sx, sz, dx, dz):
        """sx/sz = 盒宽/高（米），dx/dz = **绝对**偏移（米，相对灯盘中心 x0/z0）。

        ⚠️ dz 必须是相对 z0 的偏移量；调用方不得传已含 z0 的绝对高度
        （曾致 torso/leg 二次累加 z0，把腿甩到 4 m 空中）。
        ⚠️ 全部部件**轴对齐**摆位，不对已定位物体设欧拉角：Blender 旋转中心在物体
        自身原点，对带偏移的件设 rotation 会把它甩离人形。
        斜向观感改用「阶梯盒」——用 2 段小盒错位拼出斜线。
        """
        parts.append((make_box(f'{color_tag}_{nm}', (_m(sx), _m(t), _m(sz)),
                               (_m(x0 + dx), _m(y0), _m(z0 + dz))), mat))

    # 以下高度变量一律为**相对 z0 的偏移**（负值向下），杜绝绝对/相对混用。
    z_head_c = FIG_H / 2 - HEAD_R            # 头心
    z_torso_up = FIG_H / 2 - 2 * HEAD_R - NECK_GAP - 0.025   # 上躯干心
    z_torso_lo = FIG_H / 2 - 2 * HEAD_R - NECK_GAP - 0.070   # 下躯干心
    z_sh_up = FIG_H / 2 - 2 * HEAD_R - NECK_GAP - 0.014 - 0.019  # 上臂心
    z_sh_lo = FIG_H / 2 - 2 * HEAD_R - NECK_GAP - 0.014 - 0.052  # 前臂心
    z_hip = FIG_H / 2 - 2 * HEAD_R - NECK_GAP - 0.090             # 胯
    z_leg_up = z_hip - 0.021
    z_leg_lo = z_hip - 0.062

    # ① 头（圆，置于顶端）
    parts.append((make_cylinder(f'{color_tag}_head', _m(HEAD_R), _m(HEAD_R), _m(t), 14,
                                (_m(x0), _m(y0), _m(z0 + z_head_c)),
                                rot=(math.pi / 2, 0, 0)), mat))

    # ② 躯干（头下留颈隙；上 0.070 宽、下 0.055 收窄 ⇒ 两块叠出收分）
    add('torso_up', 0.070, 0.050, 0, z_torso_up)
    add('torso_lo', 0.055, 0.040, 0, z_torso_lo)

    # ③ 双臂：肩部窄盒 + 前臂外撇盒（阶梯拼出斜向外下，与躯干留可见间隙）
    for side, tag in ((-1, 'l'), (1, 'r')):
        add(f'arm_{tag}_up', 0.022, 0.038, side * 0.036, z_sh_up)
        add(f'arm_{tag}_lo', 0.020, 0.038, side * 0.056, z_sh_lo)

    # ④ 双腿：胯部起并立略分开（中间留缝），各 2 段折出微屈
    for side, tag in ((-1, 'l'), (1, 'r')):
        add(f'leg_{tag}_up', 0.026, 0.042, side * 0.018, z_leg_up)
        add(f'leg_{tag}_lo', 0.023, 0.040, side * 0.024, z_leg_lo)
    return parts


def main():
    reset_scene()
    set_unit_meters()

    # ① 立杆
    pole = make_cylinder('Pole', _m(POLE_R), _m(POLE_R), _m(POLE_H), 12,
                         (0, 0, _m(POLE_H / 2)))
    assign_material(pole, make_material('PedSignal_Pole_Mat', STEEL, rough=0.42, metal=0.85))

    # ② 法兰底座
    base = make_box('Base', (_m(BASE_W), _m(BASE_W), _m(BASE_H)), (0, 0, _m(BASE_H / 2)))
    assign_material(base, make_material('PedSignal_Base_Mat', STEEL, rough=0.5, metal=0.8))

    # ③ 灯箱（方盘，近黑）
    housing = make_box('Housing', (_m(HOUSING_W), _m(HOUSING_D), _m(HOUSING_H)),
                       (0, 0, _m(HOUSING_Y)))
    assign_material(housing, make_material('PedSignal_Housing_Mat', HOUSING_COLOR,
                                           rough=0.7, metal=0.2))

    # ④ 遮光罩：**U 形开口罩**（上/下/左/右四边围合，前面朝 −Y 开口），
    #    自灯箱前表面前伸 HOOD_LEN。⚠️ 不可用实心盒 —— 会把罩内的人形完全遮死
    #    （首版即此缺陷，出图只见黑方块不见红人）。
    front_y = -HOUSING_D / 2
    hood_mat = make_material('PedSignal_Hood_Mat', HOOD_COLOR, rough=0.75, metal=0.2)
    rim_t = 0.022                       # 罩壁厚
    hood_mid_y = front_y - HOOD_LEN / 2
    hood_mid_z = HOUSING_Y
    hood_parts = [
        # 上沿
        make_box('Hood_top', (_m(HOOD_W), _m(HOOD_LEN), _m(rim_t)),
                 (0, _m(hood_mid_y), _m(hood_mid_z + HOOD_H / 2 - rim_t / 2))),
        # 下沿
        make_box('Hood_bot', (_m(HOOD_W), _m(HOOD_LEN), _m(rim_t)),
                 (0, _m(hood_mid_y), _m(hood_mid_z - HOOD_H / 2 + rim_t / 2))),
        # 左沿
        make_box('Hood_l', (_m(rim_t), _m(HOOD_LEN), _m(HOOD_H - 2 * rim_t)),
                 (-_m(HOOD_W / 2 - rim_t / 2), _m(hood_mid_y), _m(hood_mid_z))),
        # 右沿
        make_box('Hood_r', (_m(rim_t), _m(HOOD_LEN), _m(HOOD_H - 2 * rim_t)),
                 (_m(HOOD_W / 2 - rim_t / 2), _m(hood_mid_y), _m(hood_mid_z))),
    ]
    for h in hood_parts:
        assign_material(h, hood_mat)

    # ⑤ 红人 / 绿人（两组人形，前端按**材质名** LEDRed / LEDGreen 命中调光）
    #    人形贴在遮光罩**内腔后壁**（罩深 HOOD_LEN 内），而非罩外 ⇒ 读作"罩内亮起"
    #    ⚠️ 不建 Empty 父节点分组：Blender 设 obj.parent 会把已按世界空间定位的
    #    子物体世界变换重置（matrix_parent_inverse 默认单位阵），曾致人形飞到空中。
    #    改用材质名分组 —— 与批次 41 车辆 Headlight/Taillight 同一套命中机制。
    face_y = front_y - HOOD_LEN + 0.006
    for color, hexcol in (('Red', RED_COLOR), ('Green', GREEN_COLOR)):
        mat = make_material(f'PedSignal_LED{color}_Mat', hexcol, rough=0.3, metal=0.1,
                            emissive=hexcol, emissive_intensity=0.9)
        for obj, m in _figure_parts(0, face_y, HOUSING_Y + FIG_Y, color, mat):
            assign_material(obj, m)

    # ⑥ 倒计时显示器（琥珀屏嵌于灯箱下方前板，emissive）
    cd = make_box('Countdown', (_m(CD_W), _m(CD_D), _m(CD_H)),
                  (0, _m(front_y - CD_D / 2), _m(CD_Y)))
    assign_material(cd, make_material('PedSignal_Countdown_Mat', CD_COLOR, rough=0.3, metal=0.1,
                                       emissive=CD_COLOR, emissive_intensity=0.7))
    cd_box = make_box('CountdownBox', (_m(CD_W + 0.03), _m(CD_D + 0.01), _m(CD_H + 0.02)),
                      (0, _m(front_y - (CD_D + 0.01) / 2), _m(CD_Y)))
    assign_material(cd_box, make_material('PedSignal_CountdownBox_Mat', HOUSING_COLOR,
                                          rough=0.7, metal=0.2))

    # ⑦ 行人过街请求按钮盒（黄色箱体 + 反光按钮面，防雨檐）
    btn_box = make_box('ButtonBox', (_m(BTN_D), _m(BTN_W), _m(BTN_H)),
                       (_m(BTN_X), 0, _m(BTN_Y)))
    assign_material(btn_box, make_material('PedSignal_ButtonBox_Mat', BUTTON_YELLOW,
                                           rough=0.55, metal=0.15))
    btn_face = make_box('ButtonFace', (_m(0.02), _m(BTN_FACE_W), _m(BTN_FACE_H)),
                        (_m(BTN_X + BTN_D / 2 + 0.008), 0, _m(BTN_Y)))
    assign_material(btn_face, make_material('PedSignal_ButtonFace_Mat', BUTTON_FACE, rough=0.35,
                                            metal=0.1, emissive=BUTTON_FACE,
                                            emissive_intensity=0.25))
    btn_roof = make_box('ButtonRoof', (_m(BTN_D + 0.03), _m(BTN_W + 0.02), _m(0.02)),
                        (_m(BTN_X), 0, _m(BTN_Y + BTN_H / 2 + 0.01)))
    assign_material(btn_roof, make_material('PedSignal_ButtonRoof_Mat', HOOD_COLOR,
                                            rough=0.75, metal=0.2))

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/pedestrian_signal.glb')
    export_glb(out_path)
    print(f'✅ pedestrian_signal.glb exported: {out_path}')


if __name__ == '__main__':
    main()
