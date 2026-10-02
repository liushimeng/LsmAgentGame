#!/usr/bin/env python3
"""
build_rail_station — 批次 49「高架铁路真实感」120 m 侧式站台车站（civic/rail_station.glb）。

── 为什么车站是「自持结构」而不是架在标准跨上 ──────────────────────────
  侧式站台车站的桥面要**横向加宽到 14.4 m**（两条线中各夹 4 m 站台），
  而标准跨模块的桥面只有 9.0 m。把站台直接压在模块上会出两处硬伤：
    ① 模块的**声屏障（y=±4.42，顶 14.46 m）会从站台板里穿出来**；
    ② 站台板（顶 12.94 m）与模块桥面（11.36 m）之间是空的，没有传力路径。
  真实做法是车站段用**独立的站区箱梁 + 加宽盖梁 + 加大承台**，
  于是 `RailViaduct.tsx` 直接**跳过车站覆盖的 4 个标准跨模块**，由本件顶上。

── §27.0-1 真实形态调研（逐条依据见 lag_docs 设计 49 §2.2）────────────────
  | 部件       | 真实规格                          | 依据            | 本件取值   |
  |------------|-----------------------------------|-----------------|------------|
  | 站台长度   | 100~120 m（4 辆 B 型车编组 + 余量）| 地铁设计规范     | 120.0 m    |
  | 站台面高   | 高出轨顶 1.05~1.25 m              | GB 50157-2013   | 1.10 m     |
  | 侧式站台宽 | 4.0~5.0 m，双线站桥面加宽 13~15 m| 工程实例        | 4.00 ×2 / 14.40 |
  | 车体间隙   | ≈0.07 m（半宽 1.33 + 线间距 1.8）| —               | 内缘 y=±3.20|
  | 屏蔽门     | 半高 1.2~1.5 m                    | 新型城轨普遍配置 | 1.40 m     |
  | 雨棚柱距   | 10~15 m                           | —               | 15.0 m     |

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender Z-up、米制。X = 走廊方向（X∈[-60,60] 居中），Y = 横向，Z = 0 为地面。
  · 标高与 `build_rail_span.py` **逐位对齐**（否则站台与轨道会错位）：
    桥面顶 11.36 / 轨顶 11.836 / 接触线 16.836 / 承力索 17.296。
  · **+Y 面（three 的负 z / 南）** 与标准跨的声屏障同侧：出入口塔朝
    体育新城 / 湿地公园一侧，符合「出入口面向主要客流方向」。
  · `minY = 0` 由两座出入口塔的塔基保证。

── ⚠ Blender `make_box(size)` 的轴序（批次 47 教训 #9）：(X 宽, Y 进深, Z 高)

── 材质槽（7 ⇒ 7 draw call，2 座车站共用）──────────────────────────────
  Rail_Concrete / _ConcreteDark / _Steel / _Glass / _Warn / _Light /
  RailStation_Sign

用法：
  blender --background --python build_rail_station.py -- \
    ClientWeb/src/assets/models/civic/rail_station.glb
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)

from __common__ import (  # noqa: E402
    CITY_PALETTE, assign_material, center_content_xz, join_objects,
    make_box, make_material, make_strut, export_glb, reset_scene, set_unit_meters,
)

# ── 与 build_rail_span.py 共用的标高（改动须两侧同改）────────────────────
GIRDER_BOT = 9.36
GIRDER_H = 2.00
GIRDER_TOP = GIRDER_BOT + GIRDER_H          # 11.36
BALLAST_T = 0.30
RAIL_H = 0.176
RAILTOP = GIRDER_TOP + BALLAST_T + RAIL_H   # 11.836
CONTACT_Z = RAILTOP + 5.00                  # 16.836
MESSENGER_Z = RAILTOP + 5.46                # 17.296

TRACK_C = 1.80
BALLAST_HW = 1.30
GAUGE = 1.435
RAIL_W = 0.075

# ── 站区几何 ────────────────────────────────────────────────────────────
HALF_L = 60.0               # 站台半长 ⇒ 站台 120 m
DECK_HW = 7.20              # 站区桥面半宽 ⇒ 14.40 m
PLAT_IN = 3.20              # 站台内缘（与车体留 0.07 m 缝）
PLAT_HW = 2.00              # 站台半宽 ⇒ 4.00 m
PLAT_TOP = RAILTOP + 1.10   # 12.936
PSD_H = 1.40
PSD_T = 0.05
PSD_PITCH = 3.00
CANOPY_Z = 16.20            # 雨棚顶
CANOPY_T = 0.22
CANOPY_PITCH = 15.0
CONCOURSE_BOT = MESSENGER_Z + 0.90          # 18.196（净空 0.90 m 过承力索）
CONCOURSE_H = 4.40
CONCOURSE_HX = 12.0
PIER_X = (-40.0, 0.0, 40.0)
CAP_L = 5.20                # 站区承台（比区间墩大：盖梁加宽到 15.6 m）
CAP_TOP = 1.60
SHAFT_TOP = 6.60
HEAD_TOP = 8.00
BEAM_HX = 2.00
BEAM_HY = 7.80              # 15.60 m（比区间盖梁宽，因站区桥面 14.4 m）
BEAM_BOT = 7.80
BEAM_TOP = 9.20
BOT_W = 8.40                # 站区底板宽
WEB_T = 0.45

# 出入口塔
TOWER_X = (-38.0, 38.0)
TOWER_HX = 5.00
TOWER_HY = 2.50
TOWER_Y = 9.50              # 塔中心（挑出桥面外侧）
STAIR_W = 1.60
STAIR_T = 0.22

LAMP_N = 4                  # 每侧站台灯数
SIGN_HX = 3.00
BOX_HX = 1.20               # 广告灯箱半长


def build() -> list:
    concrete, concrete_dark, steel, glass, warn, light, sign = [], [], [], [], [], [], []

    # ── ① 站区桥墩 ×3 + 盖梁 + 支座 ──────────────────────────────────
    for px in PIER_X:
        concrete.append(make_box('Cap', (CAP_L, CAP_L, CAP_TOP), (px, 0.0, CAP_TOP / 2.0)))
        concrete.append(make_box('CapChamfer', (CAP_L - 0.50, CAP_L - 0.50, 0.30),
                                 (px, 0.0, CAP_TOP + 0.15)))
        warn.append(make_box('CapWarn', (CAP_L + 0.06, CAP_L + 0.06, 0.34),
                             (px, 0.0, 0.62)))
        concrete.append(make_box('Shaft', (1.80, 2.40, SHAFT_TOP - CAP_TOP),
                                 (px, 0.0, (CAP_TOP + SHAFT_TOP) / 2.0)))
        concrete.append(make_box('PierHead', (1.80, 3.60, HEAD_TOP - SHAFT_TOP),
                                 (px, 0.0, (SHAFT_TOP + HEAD_TOP) / 2.0)))
        concrete.append(make_box('PierCap', (BEAM_HX * 2, BEAM_HY * 2, BEAM_TOP - BEAM_BOT),
                                 (px, 0.0, (BEAM_BOT + BEAM_TOP) / 2.0)))
        for sy in (-1, 1):
            for ek in (-1, 1):
                concrete.append(
                    make_box(f'CapHaunch_{sy}_{ek}', (BEAM_HX * 2, 1.30, 0.34),
                             (px, sy * (BEAM_HY - 0.65 + ek * 0.30), BEAM_BOT + 0.17),
                             rot=(sy * math.radians(17.0), 0.0, 0.0)))
                steel.append(make_box(f'Bearing_{sy}_{ek}', (0.60, 0.60, 0.16),
                                      (px + ek * 0.60, sy * 2.45, BEAM_TOP + 0.08)))

    # ── ② 站区箱梁（顶板 14.40 宽，底板 8.40 宽）────────────────────
    concrete.append(make_box('GirderFloor', (2 * HALF_L, BOT_W, 0.35),
                             (0.0, 0.0, GIRDER_BOT + 0.175)))
    concrete.append(make_box('GirderSlab', (2 * HALF_L, DECK_HW * 2, 0.25),
                             (0.0, 0.0, GIRDER_TOP - 0.125)))
    dz = GIRDER_H - 0.25 - 0.35
    dy = 4.20 - 3.95
    web_len = math.hypot(dy, dz)
    web_ang = math.atan2(dy, dz)
    for sy in (-1, 1):
        concrete.append(make_box(f'GirderWeb_{sy}', (2 * HALF_L, WEB_T, web_len),
                                 (0.0, sy * (4.20 + 3.95) / 2.0, GIRDER_BOT + 0.35 + dz / 2.0),
                                 rot=(sy * web_ang, 0.0, 0.0)))
        concrete.append(make_box(f'HaunchTop_{sy}', (2 * HALF_L, 1.10, 0.34),
                                 (0.0, sy * 3.85, GIRDER_TOP - 0.25 - 0.13),
                                 rot=(sy * math.radians(38), 0.0, 0.0)))

    # ── ③ 桥面伸缩缝 + 整体道床 + 钢轨（贯通，站台不打断轨道）────────
    for px in PIER_X:
        concrete_dark.append(make_box(f'Joint_{px}', (0.12, DECK_HW * 2, 0.07),
                                      (px, 0.0, GIRDER_TOP + 0.035)))
    for sy in (-1, 1):
        concrete_dark.append(make_box(f'Ballast_{sy}', (2 * HALF_L, BALLAST_HW * 2, BALLAST_T),
                                      (0.0, sy * TRACK_C, GIRDER_TOP + BALLAST_T / 2.0)))
        for ek in (-1, 1):
            concrete_dark.append(
                make_box(f'Shoulder_{sy}_{ek}', (2 * HALF_L, 0.34, BALLAST_T + 0.06),
                         (0.0, sy * TRACK_C + ek * (BALLAST_HW + 0.17),
                          GIRDER_TOP + (BALLAST_T + 0.06) / 2.0)))
        for gk in (-1, 1):
            steel.append(make_box(f'RailHead_{sy}_{gk}', (2 * HALF_L, RAIL_W, RAIL_H),
                                  (0.0, sy * TRACK_C + gk * GAUGE / 2.0,
                                   GIRDER_TOP + BALLAST_T + RAIL_H / 2.0)))
        # 接触线 + 承力索（车站段同样受电；站厅底 18.196 高于承力索 0.90 m）
        steel.append(make_box(f'ContactWire_{sy}', (2 * HALF_L, 0.045, 0.045),
                              (0.0, sy * TRACK_C, CONTACT_Z)))
        steel.append(make_box(f'Messenger_{sy}', (2 * HALF_L, 0.055, 0.055),
                              (0.0, sy * TRACK_C, MESSENGER_Z)))

    # ── ④ 站台板 ×2 + 站台面防滑层 ──────────────────────────────────
    for sy in (-1, 1):
        plat_cy = sy * (PLAT_IN + PLAT_HW)
        concrete.append(make_box(f'Platform_{sy}', (2 * HALF_L, PLAT_HW * 2, PLAT_TOP - GIRDER_TOP),
                                 (0.0, plat_cy, (GIRDER_TOP + PLAT_TOP) / 2.0)))
        concrete_dark.append(make_box(f'PlatFloor_{sy}', (2 * HALF_L, PLAT_HW * 2 - 0.24, 0.05),
                                      (0.0, plat_cy, PLAT_TOP + 0.025)))
        # 站台面盲道（黄色警示带，贴站台内缘 0.60 m）
        warn.append(make_box(f'Tactile_{sy}', (2 * HALF_L, 0.60, 0.04),
                             (0.0, sy * (PLAT_IN + 0.30), PLAT_TOP + 0.02)))

    # ── ⑤ 站台屏蔽门（半高 1.40 m）+ 立柱 @3.0 m ────────────────────
    for sy in (-1, 1):
        y = sy * (PLAT_IN + 0.10)
        glass.append(make_box(f'PSD_{sy}', (2 * HALF_L - 1.0, PSD_T, PSD_H),
                              (0.0, y, PLAT_TOP + PSD_H / 2.0)))
        steel.append(make_box(f'PSDRail_{sy}', (2 * HALF_L - 1.0, 0.10, 0.08),
                              (0.0, y, PLAT_TOP + PSD_H + 0.04)))
        n = int((2 * HALF_L - 2.0) / PSD_PITCH) + 1
        for i in range(n):
            px = -HALF_L + 1.0 + i * PSD_PITCH
            steel.append(make_box(f'PSDPost_{sy}_{i}', (0.10, 0.16, PSD_H + 0.12),
                                  (px, y, PLAT_TOP + (PSD_H + 0.12) / 2.0)))

    # ── ⑥ 雨棚 ×2 + 立柱（柱距 15 m）+ 檐口 + 排水槽 ────────────────
    n_col = int(2 * HALF_L / CANOPY_PITCH)
    for sy in (-1, 1):
        y = sy * (PLAT_IN + PLAT_HW - 0.30)
        concrete_dark.append(make_box(f'Canopy_{sy}', (2 * HALF_L, 4.60, CANOPY_T),
                                      (0.0, y, CANOPY_Z)))
        # 采光带（真实站棚是「混凝土肋 + 中间采光带」，不是一整块白板）
        glass.append(make_box(f'CanopySkylight_{sy}', (2 * HALF_L - 2.0, 1.40, 0.10),
                              (0.0, y, CANOPY_Z + CANOPY_T / 2.0 + 0.05)))
        # 檐口（外挑一档 + 深色滴水）
        concrete_dark.append(make_box(f'Fascia_{sy}', (2 * HALF_L, 0.30, 0.34),
                                      (0.0, y + sy * 2.45, CANOPY_Z - 0.20)))
        for i in range(n_col + 1):
            px = -HALF_L + i * CANOPY_PITCH
            steel.append(make_box(f'CanopyCol_{sy}_{i}', (0.24, 0.24, CANOPY_Z - PLAT_TOP),
                                  (px, y, (PLAT_TOP + CANOPY_Z) / 2.0)))
            steel.append(make_box(f'CanopyArm_{sy}_{i}', (0.16, 3.20, 0.16),
                                  (px, y - sy * 1.60, CANOPY_Z - 0.19),
                                  rot=(0.0, 0.0, 0.0)))
        # 站台灯（吊装在雨棚下）
        for i in range(LAMP_N):
            lx = -HALF_L + (i + 0.5) * (2 * HALF_L / LAMP_N)
            steel.append(make_box(f'LampBody_{sy}_{i}', (1.40, 0.26, 0.14),
                                  (lx, y - sy * 0.90, CANOPY_Z - 0.20)))
            light.append(make_box(f'LampLens_{sy}_{i}', (1.30, 0.22, 0.05),
                                  (lx, y - sy * 0.90, CANOPY_Z - 0.29)))

    # ── ⑦ 站厅（跨双线的连接体，底高于承力索 0.90 m）+ 玻璃幕墙 ──────
    concrete.append(make_box('ConcourseSlab', (CONCOURSE_HX * 2, DECK_HW * 2, 0.35),
                             (0.0, 0.0, CONCOURSE_BOT + 0.175)))
    concrete.append(make_box('ConcourseWall', (CONCOURSE_HX * 2, DECK_HW * 2, CONCOURSE_H - 0.35),
                             (0.0, 0.0, CONCOURSE_BOT + 0.35 + (CONCOURSE_H - 0.35) / 2.0)))
    # 幕墙：外挑 0.12 并高 2.60 m（真实施工是「窗带凸出勒脚/檐口」的单元式幕墙）
    glass.append(make_box('ConcourseGlass', (CONCOURSE_HX * 2 + 0.24, DECK_HW * 2 + 0.24, 2.60),
                          (0.0, 0.0, CONCOURSE_BOT + 2.05)))
    # 竖挺（每 3.0 m 一道，破「白盒子」剪影）
    for i in range(-3, 4):
        steel.append(make_box(f'Mullion_{i}', (0.14, DECK_HW * 2 + 0.34, 2.60),
                              (i * 3.0, 0.0, CONCOURSE_BOT + 2.05)))
    # 檐口线脚 + 女儿墙
    concrete_dark.append(make_box('ConcourseCornice', (CONCOURSE_HX * 2 + 0.50, DECK_HW * 2 + 0.50, 0.26),
                                  (0.0, 0.0, CONCOURSE_BOT + CONCOURSE_H - 0.14)))
    concrete_dark.append(make_box('ConcourseRoof', (CONCOURSE_HX * 2 + 0.80, DECK_HW * 2 + 0.80, 0.30),
                                  (0.0, 0.0, CONCOURSE_BOT + CONCOURSE_H + 0.15)))
    concrete.append(make_box('ConcourseParapet', (CONCOURSE_HX * 2 + 0.50, DECK_HW * 2 + 0.50, 0.70),
                             (0.0, 0.0, CONCOURSE_BOT + CONCOURSE_H + 0.65)))
    # 入口雨篷（站厅南立面外挑，标高与站台层齐平）
    concrete_dark.append(make_box('ConcourseEave', (CONCOURSE_HX * 2, 3.00, 0.24),
                                  (0.0, -(DECK_HW + 1.50), CONCOURSE_BOT - 0.55)))
    for sx in (-1, 1):
        steel.append(make_box(f'ConcourseEavePost_{sx}', (0.16, 0.16, 0.55),
                              (sx * 9.0, -(DECK_HW + 2.70), CONCOURSE_BOT - 0.83)))
    for sx in (-1, 1):
        for sy in (-1, 1):
            concrete.append(
                make_box(f'ConcourseCol_{sx}_{sy}', (0.55, 0.55, CONCOURSE_BOT - PLAT_TOP),
                         (sx * 10.0, sy * 5.20, (PLAT_TOP + CONCOURSE_BOT) / 2.0)))

    # ── ⑧ 出入口塔 ×2（**开敞钢架** + 两跑楼梯 + 雨篷；不做成实心箱）──
    #    实心箱会把楼梯整个埋掉，从街面看只是一根白柱子；开敞架 + 斜梯
    #    才是真实高架站出入口的形态。
    for tx in TOWER_X:
        for sx in (-1, 1):
            for sy in (-1, 1):
                concrete.append(
                    make_box(f'TowerCol_{tx}_{sx}_{sy}', (0.55, 0.55, PLAT_TOP + 0.30),
                             (tx + sx * (TOWER_HX - 0.28), TOWER_Y + sy * (TOWER_HY - 0.28),
                              (PLAT_TOP + 0.30) / 2.0)))
        # 街面侧包板（只包 +Y 一面，既挡透视又不埋掉楼梯）
        concrete.append(make_box('TowerClad', (TOWER_HX * 2, 0.14, PLAT_TOP * 0.55),
                                 (tx, TOWER_Y + TOWER_HY, PLAT_TOP * 0.275)))
        glass.append(make_box('TowerCladGlass', (TOWER_HX * 2 - 1.2, 0.10, 3.20),
                              (tx, TOWER_Y + TOWER_HY + 0.06, PLAT_TOP * 0.72)))
        # 顶部出口板（与站台层齐平）
        concrete_dark.append(make_box('TowerHead', (TOWER_HX * 2, TOWER_HY * 2, 0.40),
                                      (tx, TOWER_Y, PLAT_TOP - 0.20)))
        # 上/下行楼梯 + 中间休息平台（make_strut 求朝向 —— 批次 48 教训）
        for sk, sx in ((0, -1.30), (1, 1.30)):
            concrete.append(make_strut(
                f'Stair_{tx}_{sk}',
                (tx + sx, TOWER_Y - TOWER_HY - 0.20, PLAT_TOP - 0.40),
                (tx + sx, TOWER_Y + TOWER_HY + 0.40, 0.55),
                STAIR_W, STAIR_T))
            steel.append(make_strut(
                f'StairRail_{tx}_{sk}',
                (tx + sx, TOWER_Y - TOWER_HY - 0.20, PLAT_TOP + 0.70),
                (tx + sx, TOWER_Y + TOWER_HY + 0.40, 1.65),
                0.08, 0.08))
        concrete_dark.append(make_box(f'StairLanding_{tx}', (TOWER_HX * 2 - 0.4, 1.20, 0.24),
                                      (tx, TOWER_Y, PLAT_TOP * 0.48)))
        # 塔顶雨篷 + 地面雨篷
        concrete_dark.append(make_box(f'TowerCanopy_{tx}', (TOWER_HX * 2 + 1.6, 5.60, 0.24),
                                      (tx, TOWER_Y, PLAT_TOP + 0.52)))
        for sx in (-1, 1):
            steel.append(make_box(f'TowerCanopyPost_{tx}_{sx}', (0.16, 0.16, PLAT_TOP + 0.40),
                                  (tx + sx * (TOWER_HX - 0.40), TOWER_Y + 2.20,
                                   (PLAT_TOP + 0.40) / 2.0)))
        concrete_dark.append(make_box(f'GroundCanopy_{tx}', (TOWER_HX * 2 + 1.2, 4.20, 0.22),
                                      (tx, TOWER_Y + 3.40, 3.10)))
        for sx in (-1, 1):
            steel.append(make_box(f'GroundCanopyCol_{tx}_{sx}', (0.18, 0.18, 3.00),
                                  (tx + sx * (TOWER_HX - 0.30), TOWER_Y + 5.20, 1.50)))
        # 站台连廊（塔顶 → 站台）
        concrete_dark.append(make_box(f'TowerLink_{tx}', (2.60, TOWER_Y - TOWER_HY - 5.20, 0.30),
                                      (tx, (TOWER_Y - TOWER_HY + 5.20) / 2.0, PLAT_TOP - 0.15)))
        # 塔身防撞警示带：**贴在包板上的一条带**（首版做成 11.3×6.3 m 的整块黄地台，
        # 出图判读时读成「站台上一块黄色地板」—— 警示带是墙面语义，不是地面语义）
        warn.append(make_box(f'TowerWarn_{tx}', (TOWER_HX * 2 + 0.04, 0.18, 0.45),
                             (tx, TOWER_Y + TOWER_HY + 0.06, 1.05)))
        for sx in (-1, 1):
            warn.append(make_box(f'TowerWarnSide_{tx}_{sx}', (0.18, TOWER_HY * 2, 0.45),
                                 (tx + sx * (TOWER_HX + 0.02), TOWER_Y, 1.05)))
        # 塔顶航空障碍灯
        light.append(make_box(f'TowerBeacon_{tx}', (0.20, 0.20, 0.16),
                              (tx, TOWER_Y, PLAT_TOP + 0.72)))

    # ── ⑨ 站名牌 ×2 + 悬挂指示牌 + 广告灯箱 ×2 ─────────────────────
    for sy in (-1, 1):
        for px in (-24.0, 24.0):
            steel.append(make_box(f'SignPost_{sy}_{px}', (0.10, 0.10, 2.30),
                                  (px, sy * 6.60, PLAT_TOP + 1.15)))
            sign.append(make_box(f'SignBoard_{sy}_{px}', (SIGN_HX * 2, 0.10, 0.90),
                                 (px, sy * 6.60, PLAT_TOP + 2.20)))
        for bx in (-46.0, 46.0):
            sign.append(make_box(f'AdBox_{sy}_{bx}', (BOX_HX * 2, 0.30, 1.70),
                                 (bx, sy * 6.90, PLAT_TOP + 1.30)))
            steel.append(make_box(f'AdBoxFrame_{sy}_{bx}', (BOX_HX * 2 + 0.16, 0.16, 1.86),
                                  (bx, sy * 6.98, PLAT_TOP + 1.30)))

    return [
        (concrete, 'Rail_Concrete'),
        (concrete_dark, 'Rail_ConcreteDark'),
        (steel, 'Rail_Steel'),
        (glass, 'Rail_Glass'),
        (warn, 'Rail_Warn'),
        (light, 'Rail_Light'),
        (sign, 'RailStation_Sign'),
    ]


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)
    groups = build()

    mat = {
        'Rail_Concrete': make_material('Rail_Concrete_Mat', CITY_PALETTE['concrete'],
                                       rough=0.90, metal=0.0),
        'Rail_ConcreteDark': make_material('Rail_ConcreteDark_Mat', CITY_PALETTE['rail_ballast'],
                                           rough=0.88, metal=0.0),
        'Rail_Steel': make_material('Rail_Steel_Mat', CITY_PALETTE['rail_mast'],
                                    rough=0.46, metal=0.80),
        'Rail_Glass': make_material('Rail_Glass_Mat', CITY_PALETTE['rail_barrier'],
                                    rough=0.22, metal=0.0, alpha=0.34),
        'Rail_Warn': make_material('Rail_Warn_Mat', CITY_PALETTE['warn_amber'],
                                   rough=0.62, metal=0.10),
        'Rail_Light': make_material('Rail_Light_Mat', CITY_PALETTE['station_light'],
                                    rough=0.30, metal=0.0,
                                    emissive=CITY_PALETTE['station_light'],
                                    emissive_intensity=0.4),
        'RailStation_Sign': make_material('RailStation_Sign_Mat', CITY_PALETTE['sign_blue'],
                                          rough=0.42, metal=0.10,
                                          emissive=CITY_PALETTE['sign_blue'],
                                          emissive_intensity=0.25),
    }
    joined = []
    for objs, name in groups:
        if not objs:
            continue
        obj = join_objects(objs, name)
        assign_material(obj, mat[name])
        joined.append(obj)

    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    PIN_TOLERANCE_M = 0.05
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z >= 0:
            continue
        if -min_z > PIN_TOLERANCE_M:
            print(f'[pin] ⚠ {obj.name} 最低点在 {min_z:.3f} m（超过容差），**不钉扎**', flush=True)
            continue
        obj.location.z = -0.1 * min_z

    _dx, _dy = center_content_xz(joined)

    bpy.context.view_layer.update()
    import mathutils  # noqa: PLC0415
    pts = [o.matrix_world @ mathutils.Vector(c) for o in joined for c in o.bound_box]
    lo = [min(p[i] for p in pts) for i in range(3)]
    hi = [max(p[i] for p in pts) for i in range(3)]
    print('[info] 车站包围盒（世界单位） '
          f'X(走廊) {hi[0] - lo[0]:.2f} u = {(hi[0] - lo[0]) * 10:.2f} m'
          f'｜Y(横向) {hi[1] - lo[1]:.2f} u = {(hi[1] - lo[1]) * 10:.2f} m'
          f'｜Z(高) {lo[2]:.3f}~{hi[2]:.3f} u = {hi[2] * 10:.2f} m', flush=True)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/rail_station.glb')
    export_glb(out_path)
    print(f'✅ rail_station.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
