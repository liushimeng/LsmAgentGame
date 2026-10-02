#!/usr/bin/env python3
"""
build_gas_station — 批次 46「城市公用设施真实感」加油站（civic/gas_station.glb）。

── 重制理由：批次 18-AA 的程序化加油站是玩具 ────────────────────────────
  现状 `civic/GasStation.tsx` = 1 块 22×14 m 红板（罩棚）+ 4 根圆柱（柱）+
  8 个 box（加油机）+ 1 块 6×3 m 白盒（便利店）+ 1 块发光板（招牌），
  共 18 件合 3 mesh。**罩棚净高 5.0 m 与 22×14 m 投影本身合规**（GB 50156），
  但：无网架层次、无檐口/天沟/采光带、无加油岛、无防撞柱、无雨棚、
  站房无幕墙/门/女儿墙、加油机无罩/屏/枪管 —— 近景全是光板与圆柱。

── §27.0-1 真实形态调研（结论先行，逐条依据见方案 46 §2.2）──────────────
  | 部件 | 真实规格                              | 依据                          | 本件取值 |
  |------|---------------------------------------|-------------------------------|----------|
  | 罩棚 | 非燃烧材料（钢网架 + 金属屋面板）      | GB 50156-2012 强制性条文      | 钢结构网架 + 缓坡金属屋面 |
  | 净高 | 罩棚下**有效高度不应小于 4.5 m**       | GB 50156-2012                 | 檐口底 4.70 m |
  | 悬挑 | 罩棚边缘与加油机投影距离不宜小于 2 m   | GB 50156-2012                 | 岛端到罩棚边 3.5 m |
  | 加油岛| 高出场地 0.15~0.20 m，宽不小于 1.2 m  | GB 50156-2012                 | 11.0×1.40×0.15 m |
  | 防撞 | 罩棚柱应有防撞措施，栏高 ≥0.5 m       | GB 50156-2012                 | ⌀0.20×0.80 防撞柱 8 根 |
  | 站房 | 常见 70~365 ㎡；作业区内不宜超 300 ㎡  | GB 50156 / DB11/T 2538        | 12.0×4.50 = 54 ㎡ 单层 |
  | 层高 | 单层商业净高 3.3~3.6 m               | 商业建筑常用                   | 3.60 m |
  | 招牌 | 高杆灯箱 8~12 m                       | 加油站形象牌惯例              | 杆 9.0 m + 灯箱 2.60 m（顶 9.30）|

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**、米制；导出自动 Yup ⇒ three.js 直立。
  · **进站口（站房正面）= Blender -Y = three.js +Z**；形象牌在 Blender -X 侧。
  · 节点 identity；minY = 0（罩棚柱脚 / 加油岛 / 站房勒脚）。
  · 导出前 center_content_xz()：罩棚（X 主导）× 侧挂形象牌（X 向出挑）×
    后置站房（Y 向出挑）三向不对称，必须按 §27.3 内容盒水平居中。

── 材质槽（9，按材质分桶 ⇒ 9 个 primitive/DC）──────────────────────────
  GasStation_Concrete / _ConcreteDark / _Steel / _SteelDark / _CanopyRed /
  _ShopWhite / _Glass / _PUMP_Screen / _SIGN_Panel

用法：
  blender --background --python build_gas_station.py -- \
    ClientWeb/src/assets/models/civic/gas_station.glb
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

# ── 罩棚（GB 50156：净高 ≥4.5 m）────────────────────────────────────────
CAN_X, CAN_Y = 22.0, 14.0          # 罩棚投影
CLEAR_H = 4.70                     # 罩棚下有效净高（梁底）
COL_R, COL_X, COL_Y = 0.18, 9.60, 5.60   # 钢柱半径 / 柱位半跨
EAVE_Z, RIDGE_Z = 5.23, 5.58       # 屋面檐口 / 脊（2.5% 缓坡）
PURLIN_Z = 5.10                    # 檩条顶
MAINBEAM_Z = 4.98                  # 主梁顶

# ── 加油岛（高出场地 0.15 m，宽 1.40 m）─────────────────────────────────
ISL_L, ISL_W, ISL_H = 11.0, 1.40, 0.15
ISL_Y = 3.20                       # 两岛中心 ±3.2（岛间净距 5.0 m 车道）
PUMP_X = 5.00                      # 每岛 2 台机，中心距 10 m

# ── 站房（便利店 12.0 × 4.5 × 3.6）──────────────────────────────────────
SHOP_L, SHOP_D, SHOP_H = 12.0, 4.50, 3.60
SHOP_Y = 10.0                      # 站房中心 Y（罩棚 +Y 边 7.0 → 留 0.75 m 缝）
SHOP_PLINTH = 0.30

# ── 形象牌（高杆灯箱）───────────────────────────────────────────────────
TOTEM_X, TOTEM_Y = -12.60, 0.0
TOTEM_POLE_R, TOTEM_POLE_H, TOTEM_BASE = 0.15, 9.00, 0.30
TOTEM_PANEL_L, TOTEM_PANEL_H, TOTEM_PANEL_T = 4.20, 2.60, 0.35
TOTEM_PANEL_Z = 8.00

BOLLARD_R, BOLLARD_H = 0.10, 0.80


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)

    concrete, concrete_dark = [], []
    steel, steel_dark = [], []
    canopy_red, shop_white, glass = [], [], []
    pump_screen, totem_panel = [], []

    # ── ① 罩棚钢柱 + 柱脚基础 + 防撞柱 ───────────────────────────────────
    for sx in (-1, 1):
        for sy in (-1, 1):
            px, py = sx * COL_X, sy * COL_Y
            concrete_dark.append(make_box(f'ColBase_{sx}{sy}', (1.00, 1.00, 0.35),
                                          (px, py, 0.175)))
            steel.append(make_cylinder(f'Col_{sx}{sy}', COL_R, COL_R, CLEAR_H, 12,
                                      (px, py, 0.35 + CLEAR_H / 2.0)))
            # 柱顶牛腿（把主梁的荷载收进柱顶）
            steel_dark.append(make_box(f'Corbel_{sx}{sy}', (0.70, 0.30, 0.28),
                                       (px, py, CLEAR_H + 0.14)))
            # 防撞柱 ×2（罩棚柱应有防车辆碰撞措施，GB 50156）
            for k, dx in enumerate((-1.10, 1.10)):
                yy = py + sy * 0.80
                steel_dark.append(make_cylinder(f'Bollard_{sx}{sy}{k}', BOLLARD_R, BOLLARD_R,
                                                BOLLARD_H, 8, (px + dx, yy, BOLLARD_H / 2.0)))
                canopy_red.append(make_cylinder(f'BollardBand_{sx}{sy}{k}', BOLLARD_R + 0.01,
                                                BOLLARD_R + 0.01, 0.12, 8,
                                                (px + dx, yy, BOLLARD_H - 0.22)))

    # ── ② 罩棚网架：檐口梁 + 主梁 + 次梁 + 檩条 ───────────────────────────
    hx, hy = CAN_X / 2.0, CAN_Y / 2.0
    for sy in (-1, 1):
        steel.append(make_box(f'Fascia_{sy}', (CAN_X, 0.26, 0.55), (0, sy * hy, CLEAR_H + 0.275)))
    for sx in (-1, 1):
        steel.append(make_box(f'Edge_{sx}', (0.26, CAN_Y, 0.55),
                              (sx * hx, 0, CLEAR_H + 0.275)))
    for i, x in enumerate((-COL_X, -3.20, 3.20, COL_X)):
        steel.append(make_box(f'MainBeam_{i}', (0.24, CAN_Y, 0.62), (x, 0, MAINBEAM_Z)))
    for i, y in enumerate((-COL_Y, -2.00, 2.00, COL_Y)):
        steel_dark.append(make_box(f'SecBeam_{i}', (CAN_X, 0.14, 0.30), (0, y, PURLIN_Z - 0.22)))
    for i in range(10):
        x = -hx + 0.60 + i * (CAN_X - 1.2) / 9.0
        steel_dark.append(make_box(f'Purlin_{i}', (0.10, CAN_Y, 0.16), (x, 0, PURLIN_Z)))

    # ── ③ 屋面：缓双坡金属板（白）+ 红色立缝 + 屋脊压顶 + 檐沟 + 采光带 ────
    #   真实加油站罩棚几乎都是**浅色金属板 + 品牌色立缝/檐口带**（中石化白底红带、
    #   中石油黄底蓝带），灰钢板在俯视下会整片压过全场，故取浅色底 + 红立缝。
    slope = math.atan2(RIDGE_Z - EAVE_Z, hy)
    slope_len = hy / math.cos(slope)
    for sy in (-1, 1):
        panel = make_box(f'RoofPanel_{sy}', (CAN_X, slope_len, 0.10),
                         (0, sy * hy / 2.0, (RIDGE_Z + EAVE_Z) / 2.0))
        panel.rotation_euler = (-sy * slope, 0, 0)
        shop_white.append(panel)
        # 红色立缝（沿坡向，@1.30 m 一道）—— 破除"整块白板"，兼作品牌带
        for i in range(17):
            x = -hx + 0.65 + i * (CAN_X - 1.3) / 16.0
            rib = make_box(f'Seam_{sy}_{i}', (0.09, slope_len, 0.09),
                           (x, sy * hy / 2.0, (RIDGE_Z + EAVE_Z) / 2.0 + 0.08))
            rib.rotation_euler = (-sy * slope, 0, 0)
            canopy_red.append(rib)
        # 檐口封边板（红带：加油站品牌面）
        canopy_red.append(make_box(f'RoofFascia_{sy}', (CAN_X + 0.10, 0.10, 0.30),
                                   (0, sy * (hy + 0.06), EAVE_Z - 0.10)))
        # 天沟
        steel_dark.append(make_box(f'Gutter_{sy}', (CAN_X + 0.10, 0.22, 0.18),
                                   (0, sy * (hy + 0.14), EAVE_Z - 0.26)))
        # 落水管 ×2
        for sx in (-1, 1):
            steel_dark.append(make_cylinder(f'DownPipe_{sy}{sx}', 0.06, 0.06, EAVE_Z - 0.35, 8,
                                            (sx * COL_X, sy * (hy + 0.14), (EAVE_Z - 0.35) / 2.0)))
        # 采光带（半透明白板，贴在屋面板面上、嵌在两排立缝之间）
        glass.append(make_box(f'Skylight_{sy}', (CAN_X - 3.0, 0.70, 0.06),
                              (0, sy * 2.40, RIDGE_Z - 2.40 * slope + 0.07)))
    canopy_red.append(make_box('RidgeCap', (CAN_X + 0.10, 0.42, 0.14), (0, 0, RIDGE_Z + 0.05)))

    # ── ④ 加油岛 ×2（+ 黄色端部警示带）──────────────────────────────────
    for sy in (-1, 1):
        iy = sy * ISL_Y
        concrete.append(make_box(f'Island_{sy}', (ISL_L, ISL_W, ISL_H), (0, iy, ISL_H / 2.0)))
        for s2 in (-1, 1):
            steel_dark.append(make_box(f'IslandCurb_{sy}{s2}', (ISL_L, 0.16, ISL_H + 0.03),
                                       (0, iy + s2 * (ISL_W / 2.0 - 0.08), (ISL_H + 0.03) / 2.0)))
            steel.append(make_box(f'IslandWarn_{sy}{s2}', (1.60, 0.17, ISL_H + 0.05),
                                  (s2 * (ISL_L / 2.0 - 0.80), iy + s2 * (ISL_W / 2.0 - 0.08),
                                   (ISL_H + 0.05) / 2.0)))

    # ── ⑤ 加油机 ×4（机身 + 底座 + 机顶罩 + 显示屏 + 2 支枪）──────────────
    for sy in (-1, 1):
        for sx in (-1, 1):
            px, py = sx * PUMP_X, sy * ISL_Y
            steel_dark.append(make_box(f'PumpBase_{sx}{sy}', (1.34, 0.84, 0.16),
                                       (px, py, ISL_H + 0.08)))
            shop_white.append(make_box(f'PumpBody_{sx}{sy}', (1.20, 0.70, 1.90),
                                       (px, py, ISL_H + 0.16 + 0.95)))
            # 下裙板（深色）：把机身从「一根白柱」救成「加油机柜体」
            steel_dark.append(make_box(f'PumpSkirt_{sx}{sy}', (1.24, 0.74, 0.55),
                                       (px, py, ISL_H + 0.16 + 0.275)))
            # 机顶罩：两侧立柱托一块顶板（真实加油机顶棚为独立罩体，非"帽子"）
            for k, dx in enumerate((-0.68, 0.68)):
                steel_dark.append(make_box(f'PumpHoodPost_{sx}{sy}{k}', (0.09, 0.09, 0.30),
                                           (px + dx, py, ISL_H + 2.06 + 0.15)))
            canopy_red.append(make_box(f'PumpHood_{sx}{sy}', (1.60, 0.90, 0.14),
                                       (px, py, ISL_H + 2.06 + 0.37)))
            # 显示屏（朝 -Y = 进站口方向）
            pump_screen.append(make_box(f'PumpScreen_{sx}{sy}', (0.60, 0.04, 0.36),
                                        (px, py - 0.37, ISL_H + 1.62)))
            steel_dark.append(make_box(f'PumpScreenBezel_{sx}{sy}', (0.70, 0.05, 0.46),
                                       (px, py - 0.35, ISL_H + 1.62)))
            # 加油枪 ×2 + 枪管
            for k, dx in enumerate((-0.34, 0.34)):
                steel_dark.append(make_box(f'Nozzle_{sx}{sy}{k}', (0.14, 0.10, 0.26),
                                           (px + dx, py - 0.38, ISL_H + 1.10)))
                steel_dark.append(make_box(f'NozzleStem_{sx}{sy}{k}', (0.05, 0.05, 0.55),
                                           (px + dx * 0.4, py - 0.34, ISL_H + 1.48)))

    # ── ⑥ 站房（便利店）：勒脚 + 墙体 + 幕墙 + 门 + 雨棚 + 女儿墙 + 屋面 ────
    pz = SHOP_PLINTH
    concrete_dark.append(make_box('ShopPlinth', (SHOP_L + 0.30, SHOP_D + 0.30, SHOP_PLINTH),
                                  (0, SHOP_Y, SHOP_PLINTH / 2.0)))
    shop_white.append(make_box('ShopBody', (SHOP_L, SHOP_D, SHOP_H - SHOP_PLINTH),
                               (0, SHOP_Y, pz + (SHOP_H - SHOP_PLINTH) / 2.0)))
    # 幕墙带（占 -Y 面墙高 60%）
    glass.append(make_box('ShopGlass', (SHOP_L - 1.2, 0.06, 1.90),
                          (0, SHOP_Y - SHOP_D / 2.0 - 0.01, pz + 1.35)))
    steel_dark.append(make_box('ShopDoor', (1.20, 0.08, 2.30), (2.2, SHOP_Y - SHOP_D / 2.0 - 0.04, pz + 1.15)))
    steel_dark.append(make_box('ShopDoorGlass', (1.00, 0.04, 1.70),
                               (2.2, SHOP_Y - SHOP_D / 2.0 - 0.09, pz + 1.45)))
    # 幕墙竖挺（每 1.5 m 一根，破除"整块玻璃"感）
    for i in range(-3, 4):
        steel_dark.append(make_box(f'Mullion_{i}', (0.08, 0.10, 1.90),
                                   (i * 1.5, SHOP_Y - SHOP_D / 2.0 - 0.03, pz + 1.35)))
    # 出檐雨棚（挑出 1.8 m）+ 托架
    canopy_red.append(make_box('ShopAwning', (SHOP_L + 1.0, 1.80, 0.25),
                               (0, SHOP_Y - SHOP_D / 2.0 - 0.90, pz + 3.00)))
    for i in range(4):
        sx = -4.5 + i * 3.0
        steel_dark.append(make_box(f'AwningBracket_{i}', (0.10, 1.70, 0.30),
                                   (sx, SHOP_Y - SHOP_D / 2.0 - 0.85, pz + 2.80)))
    # 门头灯箱（夜间自发光）+ 红色边框（白板悬在白墙上必须靠边框立住）
    totem_panel.append(make_box('ShopSign', (6.00, 0.22, 0.55),
                                (0, SHOP_Y - SHOP_D / 2.0 - 0.32, pz + 3.52)))
    canopy_red.append(make_box('ShopSignFrame', (6.36, 0.16, 0.85),
                               (0, SHOP_Y - SHOP_D / 2.0 - 0.24, pz + 3.52)))
    # 墙脚品牌色横带（破除站房整块白墙）
    canopy_red.append(make_box('ShopBaseBand', (SHOP_L + 0.06, SHOP_D + 0.06, 0.45),
                               (0, SHOP_Y, pz + 0.22)))
    # 女儿墙 + 屋面 + 屋面设备
    concrete_dark.append(make_box('ShopParapet', (SHOP_L + 0.30, SHOP_D + 0.30, 0.30),
                                  (0, SHOP_Y, pz + SHOP_H - SHOP_PLINTH + 0.15)))
    steel.append(make_box('ShopRoof', (SHOP_L + 0.40, SHOP_D + 0.40, 0.16),
                          (0, SHOP_Y, pz + SHOP_H - SHOP_PLINTH + 0.38)))
    for i, dx in enumerate((-4.2, -3.0)):
        steel_dark.append(make_box(f'ACUnit_{i}', (0.95, 0.42, 0.75),
                                   (dx, SHOP_Y + SHOP_D / 2.0 + 0.30, pz + 0.70)))
        steel_dark.append(make_box(f'ACBracket_{i}', (1.05, 0.55, 0.10),
                                   (dx, SHOP_Y + SHOP_D / 2.0 + 0.30, pz + 0.30)))

    # ── ⑦ 高杆形象牌：基础 + 杆 + 灯箱 + 边框 + 检修梯 ─────────────────────
    concrete_dark.append(make_box('TotemBase', (1.30, 1.30, TOTEM_BASE),
                                  (TOTEM_X, TOTEM_Y, TOTEM_BASE / 2.0)))
    steel.append(make_taper('TotemPole', TOTEM_POLE_R + 0.05, TOTEM_POLE_R,
                            TOTEM_POLE_H, 12,
                            (TOTEM_X, TOTEM_Y, TOTEM_BASE + TOTEM_POLE_H / 2.0)))
    totem_panel.append(make_box('TotemPanel', (TOTEM_PANEL_L, TOTEM_PANEL_T, TOTEM_PANEL_H),
                                (TOTEM_X, TOTEM_Y, TOTEM_PANEL_Z)))
    # 品牌色横带（形象牌远景识别度全靠这条带，纯白板在灰天里读不出来）
    for s2 in (-1, 1):
        canopy_red.append(make_box(f'TotemBand_{s2}',
                                   (TOTEM_PANEL_L - 0.30, TOTEM_PANEL_T + 0.03, 0.55),
                                   (TOTEM_X, TOTEM_Y + s2 * 0.01, TOTEM_PANEL_Z - 0.55)))
    for s2 in (-1, 1):
        steel_dark.append(make_box(f'TotemFrameV_{s2}', (0.10, TOTEM_PANEL_T + 0.06, TOTEM_PANEL_H + 0.20),
                                   (TOTEM_X + s2 * (TOTEM_PANEL_L / 2.0 + 0.05), TOTEM_Y, TOTEM_PANEL_Z)))
    for s2 in (-1, 1):
        steel_dark.append(make_box(f'TotemFrameH_{s2}', (TOTEM_PANEL_L + 0.20, TOTEM_PANEL_T + 0.06, 0.10),
                                   (TOTEM_X, TOTEM_Y, TOTEM_PANEL_Z + s2 * (TOTEM_PANEL_H / 2.0 + 0.05))))
    # 灯箱与杆的连接臂
    for s2 in (-1, 1):
        steel_dark.append(make_box(f'TotemArm_{s2}', (0.16, TOTEM_PANEL_T, 0.16),
                                   (TOTEM_X, TOTEM_Y, TOTEM_PANEL_Z + s2 * 0.85)))
    # 检修梯（双立杆 + 6 踏步）
    for s2 in (-1, 1):
        steel_dark.append(make_box(f'LadderRail_{s2}', (0.06, 0.06, 6.60),
                                   (TOTEM_X + 0.42, TOTEM_Y + s2 * 0.22, 3.60)))
    for i in range(6):
        steel_dark.append(make_box(f'LadderRung_{i}', (0.06, 0.50, 0.05),
                                   (TOTEM_X + 0.42, TOTEM_Y, 0.90 + i * 1.10)))

    # ── ⑧ 按材质合并 ⇒ 9 primitive ───────────────────────────────────
    mat_c = make_material('GasStation_Concrete_Mat', CITY_PALETTE['concrete'], rough=0.88, metal=0.0)
    mat_cd = make_material('GasStation_ConcreteDark_Mat', CITY_PALETTE['concrete_dark'],
                           rough=0.90, metal=0.0)
    mat_s = make_material('GasStation_Steel_Mat', CITY_PALETTE['steel'], rough=0.45, metal=0.75)
    mat_sd = make_material('GasStation_SteelDark_Mat', CITY_PALETTE['steel_dark'],
                           rough=0.52, metal=0.65)
    mat_cr = make_material('GasStation_CanopyRed_Mat', CITY_PALETTE['canopy_red'],
                           rough=0.58, metal=0.15)
    mat_sw = make_material('GasStation_ShopWhite_Mat', CITY_PALETTE['shop_white'],
                           rough=0.72, metal=0.0)
    mat_gl = make_material('GasStation_Glass_Mat', CITY_PALETTE['glass'],
                           rough=0.12, metal=0.0)
    # ⚠ 材质名必须与节点名 `GasStation_PUMP_Screen` 同源（+ `_Mat` 后缀）：前端
    #   `CivicGlbPiece` 的 litMaterials 靠**材质名** `.includes()` 匹配，
    #   两者一旦漂移 ⇒ 声明了却从不接线（§130），CDP 数值审计才查得出来。
    mat_ps = make_material('GasStation_PUMP_Screen_Mat', CITY_PALETTE['sign_amber'],
                           rough=0.30, metal=0.0, emissive=CITY_PALETTE['sign_amber'],
                           emissive_intensity=0.8)
    mat_tp = make_material('GasStation_SIGN_Panel_Mat', CITY_PALETTE['sign_lightbox'],
                           rough=0.35, metal=0.0, emissive=CITY_PALETTE['sign_lightbox'],
                           emissive_intensity=0.5)
    groups = [
        (concrete, mat_c, 'GasStation_Concrete'),
        (concrete_dark, mat_cd, 'GasStation_ConcreteDark'),
        (steel, mat_s, 'GasStation_Steel'),
        (steel_dark, mat_sd, 'GasStation_SteelDark'),
        (canopy_red, mat_cr, 'GasStation_CanopyRed'),
        (shop_white, mat_sw, 'GasStation_ShopWhite'),
        (glass, mat_gl, 'GasStation_Glass'),
        (pump_screen, mat_ps, 'GasStation_PUMP_Screen'),
        (totem_panel, mat_tp, 'GasStation_SIGN_Panel'),
    ]
    joined = [join_objects(objs, name) for objs, _m, name in groups]
    for (_objs, mat, _name), obj in zip(groups, joined):
        assign_material(obj, mat)

    # ⚠ 全件米 → 世界单位（×0.1）。顺序关键：join 保留首个对象的 location/scale，
    #   必须先 transform_apply flatten 再统一 ×0.1（详见 bake_transforms docstring）。
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z < 0:
            obj.location.z = -0.1 * min_z

    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/gas_station.glb')
    export_glb(out_path)
    print(f'✅ gas_station.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
