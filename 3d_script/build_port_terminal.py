#!/usr/bin/env python3
"""
build_port_terminal — 批次 48「港口码头真实感」内河支线集装箱码头（civic/port_terminal.glb）。

── 重制理由：批次 18-AA 的程序化码头有三处硬伤 ────────────────────────────
  1) **尺度与落位双错**：`PlaneGeometry(u(80), u(25))` @ (-30, -9.5) rot π
     ⇒ 世界 x ∈ [-70, +10]、**800 m 长的码头面**，伸出城界（x=-60）100 m，另一端一直
     铺到 CBD 脚下。
  2) **集装箱尺寸错一半**：`boxPart(u(6), u(2.6), u(2.4))` —— 40 ft 国际标准箱是
     **12.19 × 2.44 × 2.59 m**（20 ft 为 6.06 × 2.44 × 2.59）。现状既不是 40 ft 也不
     是 20 ft（宽 2.6 > 2.44，两头都不对）。
  3) **岸吊是门架玩具**：4 根 ⌀0.5 m 圆柱 + 10 m 悬臂 + 22 m 横梁 + 1 个吊具。

── §27.0-1 真实形态调研（结论先行，逐条依据见设计 48 §3）──────────────────
  | 部件 | 真实规格                  | 依据                | 本件取值 |
  |------|---------------------------|---------------------|----------|
  | 轨距 | 16 m（宽轨 26 m）          | DB36/T 1833-2023    | 16.0 m   |
  | 轨下起升 | ≥25 m（可堆 4 层高箱）   | 岸桥通用参数        | 26.0 m   |
  | 轨上总高 | 约 37 m                  | 岸桥通用参数        | 34.0 m   |
  | 外伸距 | 35~38 m（沿海）           | 岸桥通用参数        | 26.0 m（支线港档）|
  | 内伸距 | ≥8.5 m                   | 岸桥通用参数        | 12.0 m   |
  | 门架净空 | 约 10 m（过跨运车 9 m）  | 岸桥通用参数        | 10.0 m   |
  | 吊具 | 跨 40 ft = 12.19 m + 导板 | ISO 668             | 12.5 m   |
  | 40 ft 箱 | 12.19 × 2.44 × 2.59 m   | ISO 668             | 同       |
  | 岸壁线→海侧轨 | 2~3 m             | 码头前沿宽度定义    | 3.0 m    |
  | 系船柱间距 | 15~25 m                 | 港口工程通用        | 6 只 @12 m |

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 Z-up、米制；导出自动 Yup ⇒ three.js 直立。
  · **岸壁线（陆海分界）= Blender -Y**；海侧（港池）在 -Y 方向。**站房/闸口在 +Y 侧**。
  · 节点 identity；minY = 0（码头面贴地；岸壁挡墙向下延伸部分由 Y 负值表达，
    会被贴地钉扎按容差规则处理 —— 见文件尾的说明）。
  · 布局沿 X 轴对称（两台岸镜）⇒ `center_content_xz()` 仅消除系船柱的微小偏心。

── ⚠ Blender `make_box(size)` 的轴序（批次 47 教训 #9）────────────────────
  **是 (X 宽, Y 水平进深, Z 高度)**，与 three `BoxGeometry(w,h,d)` 的中间槽位相反。
  本文件所有 make_box 一律按 (X 宽, Y 进深, Z 高) 书写。

── 材质槽（12，按材质分桶 ⇒ 12 个 primitive/DC）──────────────────────────
  Port_Concrete / _ConcreteDark / _Steel / _Crane / _CraneFrame /
  _Container / _Warn / _NavLight

用法：
  blender --background --python build_port_terminal.py -- \
    ClientWeb/src/assets/models/civic/port_terminal.glb
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
    make_box, make_cylinder, make_taper, make_strut,
    make_material, assign_material, join_objects,
    export_glb, center_content_xz, CITY_PALETTE,
)


def hex_rgba(h: str) -> tuple:
    """'#rrggbb' → (r, g, b, a) 0..1。用于箱体顶点色（`make_material` 只吃纯色）。"""
    s2 = h.lstrip('#')
    return (int(s2[0:2], 16) / 255.0, int(s2[2:4], 16) / 255.0, int(s2[4:6], 16) / 255.0, 1.0)

# ── 码头几何（米）────────────────────────────────────────────────────────
BERTH_L = 80.0                    # 泊位长（X）
QUAY_Y = 0.0                      # 岸壁线（Blender Y = 0；海侧 -Y，陆侧 +Y）
# ⚠ **朝向**：岸壁线 = Blender Y 0，海侧（港池）在 **+Y**，陆地在 -Y。
#   岸桥的**外伸（悬臂）必须伸向 +Y 罩住水面**，后伸伸向 -Y 压住堆场。
#   首版把 `y_out = y_sea - CRANE_OUT` 写成陆侧 ⇒ 26 m 大梁压在集装箱堆场上方，
#   而水面上空空无一物 —— 出图才看出来，俯视/正视都发现不了（正视图沿 Y 看，悬臂正好
#   端面朝镜头）。
DECK_D = 34.0                     # 码头面进深（Y）
RAIL_SEA = -3.0                    # 海侧轨（岸壁线陆侧 3 m）
RAIL_LAND = -19.0                  # 陆侧轨（轨距 16 m）
YARD_Y = -26.0                     # 堆场中心 Y（陆侧轨再向陆 7 m）
YARD_ROWS, YARD_COLS, YARD_TIERS = 4, 5, 2
BOX_L, BOX_W, BOX_H = 12.19, 2.44, 2.59     # 40 ft 箱（ISO 668）
BOX_GAP = 0.40                     # 列/排间距

# ── 岸桥 STS ───────────────────────────────────────────────────────────
CRANE_X = (-20.0, 20.0)            # 两台，沿泊位
CRANE_GAUGE = 16.0                 # 轨距
CRANE_BASE = 18.0                  # 基距（门架腿纵向跨距）
CRANE_CLEAR = 10.0                 # 门架净空
CRANE_GIRDER = 26.0                # 大梁底标高（轨下起升）
CRANE_OUT = 26.0                   # 外伸距（向 -Y 海侧）
CRANE_BACK = 12.0                  # 内伸距（向 +Y 陆侧）
CRANE_APEX = 34.0                  # A 字架顶
CRANE_SPAN = 12.5                  # 吊具跨距（40 ft）
CRANE_LEG = 0.90                   # 门架腿截面

# ── 水工 ───────────────────────────────────────────────────────────────
# ⚠ 城市地面与水面几乎共面（地面 y=0 / 水面 y≈0.03），**模型不得低于 y=0**
#   （`verify_glb_aabb` 的贴地判据要求 minY=0，且地下的几何全被地面遮住）。
#   故真实码头里「埋在水面以下的重力式岸壁 + 水下护舷」在这里改为
#   **地面之上的胸墙 + 悬挂式橡胶护舷** —— 俯视与近景的观感一致，且不浪费几何。
WALL_TOP = 0.70                    # 岸壁胸墙顶（码头面 0.30 + 0.40 胸高）
WALL_D = 0.45                      # 胸墙厚
BOLLARD_N = 6                      # 系船柱
FENDER_N = 6                       # 护舷
FENDER_Z = 0.0                     # 护舷（悬于胸墙水侧面，底部触水面）

# ── 场区 ───────────────────────────────────────────────────────────────
DECK_T = 0.30                      # 码头面厚
LIGHT_MAST_H = 30.0                # 堆场照明塔
LIGHT_X = (-34.0, 34.0)
GATE_L, GATE_D, GATE_H = 10.0, 7.0, 6.0   # 港区闸口办公楼
RMG_SPAN, RMG_H = 20.0, 18.0       # 轮胎式龙门吊跨度 / 高


def sts_crane(x: float, body: list, frame_m: list, warn: list, nav: list) -> None:
    """一台岸桥（STS）。所有件按 (X 宽, Y 进深, Z 高) 书写。"""
    y_sea = RAIL_SEA
    y_land = RAIL_LAND
    y_sill = (y_sea + y_land) / 2.0

    # ① 轨道梁（每条轨下）+ 门架腿 ×4
    for y in (y_sea, y_land):
        frame_m.append(make_box(f'RailBeam_{x}_{y}', (CRANE_BASE + 6.0, 1.30, 1.20),
                                (x, y, 0.90)))
    for sx in (-1, 1):
        for y in (y_sea, y_land):
            body.append(make_box(f'Leg_{x}_{sx}_{y}', (CRANE_LEG, CRANE_LEG, CRANE_GIRDER - 1.5),
                                 (x + sx * CRANE_BASE / 2.0, y, 1.5 + (CRANE_GIRDER - 1.5) / 2.0)))
            # 柱脚铰座
            warn.append(make_box(f'LegBase_{x}_{sx}_{y}', (1.60, 1.60, 0.60),
                                 (x + sx * CRANE_BASE / 2.0, y, 0.30)))

    # ② 门架横梁（sill beam，净空 10 m）
    for sx in (-1, 1):
        body.append(make_box(f'Sill_{x}_{sx}', (1.00, abs(y_land - y_sea), 1.60),
                             (x + sx * CRANE_BASE / 2.0, y_sill, CRANE_CLEAR + 0.80)))

    # ③ 桁架大梁：海侧悬臂 + 陆侧后伸 + 上弦双肢 + 腹杆
    z_g = CRANE_GIRDER
    y_out = y_sea + CRANE_OUT          # 外伸：伸向 **+Y 海侧**
    y_back = y_land - CRANE_BACK      # 后伸：伸向 -Y 陆侧
    for dz in (0.0, 2.20):
        body.append(make_box(f'Chord_{x}_{dz}', (1.20, y_back - y_out, 0.45),
                             (x, (y_out + y_back) / 2.0, z_g + 1.0 + dz)))
    n_bay = 13
    for i in range(n_bay):
        t = i / (n_bay - 1)
        y = y_out + (y_back - y_out) * t
        # 腹杆：斜撑（每跨一根）
        frame_m.append(make_box(f'Web_{x}_{i}', (0.35, 0.30, 3.65),
                                (x, y, z_g + 2.10)))
    # 端部封板
    for y, tag in ((y_out, 'Out'), (y_back, 'Back')):
        frame_m.append(make_box(f'GirderEnd_{x}_{tag}', (1.30, 0.50, 3.30),
                                (x, y, z_g + 2.10)))

    # ④ A 字架 + 前后拉杆（一律走 make_strut：两点直连，不手算欧拉）
    apex = (x, y_land + 3.0, CRANE_APEX)
    body.append(make_box(f'AHead_{x}', (1.40, 6.60, 0.90), (x, apex[1], CRANE_APEX - 0.45)))
    for sy, y_leg in ((-1, y_sea), (1, y_land)):
        for sx in (-1, 1):
            body.append(make_strut(f'ALeg_{x}_{sx}_{sy}', (x + sx * (CRANE_BASE / 2.0), y_leg, z_g + 1.2), apex, 0.55))
    # 前拉杆（A 架顶 → 大梁海端）与后拉杆（A 架顶 → 大梁陆端）
    frame_m.append(make_strut(f'StayF_{x}', (x, apex[1] - 0.4, CRANE_APEX - 0.9), (x, y_out + 2.0, z_g + 3.2), 0.28))
    frame_m.append(make_strut(f'StayB_{x}', (x, apex[1] + 0.4, CRANE_APEX - 0.9), (x, y_back - 2.0, z_g + 3.2), 0.28))

    # ⑤ 小车 + 吊具（吊具跨 40 ft 12.5 m）+ 4 根钢缆
    y_trolley = y_sea + CRANE_OUT * 0.55
    body.append(make_box(f'Trolley_{x}', (2.60, 3.20, 1.40), (x, y_trolley, z_g + 0.70)))
    warn.append(make_box(f'Spreader_{x}', (CRANE_SPAN, 2.70, 0.90),
                         (x, y_trolley, z_g - 8.60)))
    for sx in (-1, 1):
        for sy in (-1, 1):
            frame_m.append(make_box(f'Hoist_{x}_{sx}_{sy}', (0.10, 0.10, 8.0),
                                    (x + sx * (CRANE_SPAN / 2 - 0.6), y_trolley + sy * 1.1, z_g - 4.6)))
    # 导板（吊具两端的斜导板）
    for sx in (-1, 1):
        warn.append(make_box(f'Guide_{x}_{sx}', (0.30, 2.40, 1.60),
                             (x + sx * (CRANE_SPAN / 2 + 0.20), y_trolley, z_g - 9.60)))

    # ⑥ 机器房 + 司机室 + 爬梯 + 航空障碍灯
    body.append(make_box(f'MachineHouse_{x}', (5.20, 7.00, 3.20), (x, y_back - 4.5, z_g + 4.60)))
    warn.append(make_box(f'Cab_{x}', (2.20, 2.40, 2.20), (x + 3.4, y_trolley - 1.8, z_g - 1.60)))
    for i in range(9):
        frame_m.append(make_box(f'Ladder_{x}_{i}', (0.09, 0.50, 0.09),
                                (x - 1.0, y_land + 0.9, 1.2 + i * 2.4)))
    nav.append(make_cylinder(f'ObLight_{x}', 0.22, 0.22, 0.40, 8, (x, y_sill, CRANE_APEX + 0.40)))


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)

    concrete, concrete_dark = [], []
    steel, crane, crane_frame = [], [], []
    container, warn, nav = [], [], []

    # ── ① 码头面（重载混凝土铺面）+ 岸壁挡墙 ────────────────────────────
    concrete.append(make_box('Deck', (BERTH_L, DECK_D, DECK_T),
                             (0, QUAY_Y - DECK_D / 2.0, DECK_T / 2.0)))
    # 岸壁挡墙：顶 +0.40，墙身向下 2.0（部分没入码头面以下，渲染时被水/地遮住）
    concrete.append(make_box('QuayParapet', (BERTH_L, WALL_D, WALL_TOP - DECK_T),
                             (0, QUAY_Y - WALL_D / 2.0, DECK_T + (WALL_TOP - DECK_T) / 2.0)))
    concrete_dark.append(make_box('QuayCap', (BERTH_L, WALL_D + 0.20, 0.12),
                                  (0, QUAY_Y - WALL_D / 2.0, WALL_TOP + 0.06)))

    # ── ② 系船柱 ×6 + 护舷 ×6 ────────────────────────────────────────
    for i in range(BOLLARD_N):
        bx = -BERTH_L / 2.0 + (BERTH_L / (BOLLARD_N - 1)) * i
        steel.append(make_cylinder(f'Bollard_{i}', 0.28, 0.34, 0.80, 10,
                                   (bx, QUAY_Y - 1.10, WALL_TOP + 0.40)))
        steel.append(make_cylinder(f'BollardCap_{i}', 0.40, 0.34, 0.18, 10,
                                   (bx, QUAY_Y - 1.10, WALL_TOP + 0.88)))
    for i in range(FENDER_N):
        fx = -BERTH_L / 2.0 + (BERTH_L / (FENDER_N - 1)) * i
        # 悬挂在胸墙水侧面的橡胶护舷（底部刚好触到水面 y≈0.03）
        # 悬挂在胸墙**水侧**（+Y）面的橡胶护舷，底部刚好触到水面 y≈0.03
        warn.append(make_box(f'Fender_{i}', (1.60, 0.70, 0.95), (fx, QUAY_Y + 0.30, 0.50)))
        steel.append(make_box(f'FenderChain_{i}', (0.16, 0.16, 0.70), (fx, QUAY_Y + 0.10, 0.90)))

    # ── ③ 轨道 ×2（轨枕 + 轨条）────────────────────────────────────────
    for y in (RAIL_SEA, RAIL_LAND):
        n_sleep = int(BERTH_L / 0.8)
        for i in range(n_sleep):
            sx = -BERTH_L / 2.0 + 0.4 + i * 0.8
            concrete_dark.append(make_box(f'Sleeper_{y}_{i}', (0.24, 2.60, 0.18),
                                          (sx, y, 0.09)))
        for sy in (-1, 1):
            steel.append(make_box(f'Rail_{y}_{sy}', (BERTH_L, 0.10, 0.16),
                                  (0, y + sy * 0.75, 0.26)))

    # ── ④ 岸桥 ×2 ─────────────────────────────────────────────────────
    for x in CRANE_X:
        sts_crane(x, crane, crane_frame, warn, nav)

    # ── ⑤ 集装箱堆场：40 ft 箱 4 排 × 5 列 × 2 层（确定性色序）──────────
    col_pitch = BOX_L + BOX_GAP
    row_pitch = BOX_W + BOX_GAP
    x0 = -(YARD_COLS - 1) * col_pitch / 2.0
    y0 = YARD_Y - (YARD_ROWS - 1) * row_pitch / 2.0
    # 5 种确定性箱色（航运公司的调色习惯）。⚠ **按色分 5 个材质组**而不是用顶点色 ——
    #   Blender 5 的 glTF 导出器在 `join_objects` 之后不导出自建的 `color_attributes`
    #   （实测 COLOR_0 全部刷成 255,255,255,255，`active_color` / `render_color_index`
    #   均无效）。港区是全城少有的彩色地标，箱色必须真的出来，5 个额外 primitive 可接受。
    TINT = (CITY_PALETTE['container_red'], CITY_PALETTE['container_shade'],
            '#3f6b8a', '#7a7f52', '#8a6a3a')
    container = [[] for _ in TINT]
    idx = 0
    for r in range(YARD_ROWS):
        for c in range(YARD_COLS):
            bx = x0 + c * col_pitch
            by = y0 + r * row_pitch
            for t in range(YARD_TIERS):
                z = DECK_T + BOX_H / 2.0 + t * BOX_H
                k = (r * YARD_COLS + c + t * 2) % len(TINT)
                container[k].append(make_box(f'Box_{r}_{c}_{t}', (BOX_L, BOX_W, BOX_H), (bx, by, z)))
                # 箱端门（端面深一档，兼作「这是端门不是侧板」的可读性）
                concrete_dark.append(make_box(f'BoxDoor_{r}_{c}_{t}',
                                              (0.08, BOX_W - 0.16, BOX_H - 0.22),
                                              (bx - BOX_L / 2.0 - 0.02, by, z)))
                idx += 1

    # ── ⑥ 轮胎式龙门吊 RMG ×1（跨堆场）────────────────────────────────
    rmg_y = YARD_Y
    for sx in (-1, 1):
        for sy in (-1, 1):
            crane.append(make_box(f'RMGLeg_{sx}{sy}', (0.70, 0.70, RMG_H - 2.5),
                                  (sx * (RMG_SPAN / 2.0), rmg_y + sy * 4.0, 0.30 + (RMG_H - 2.5) / 2.0)))
    crane.append(make_box('RMGBeam', (RMG_SPAN + 3.0, 2.0, 1.50), (0, rmg_y, RMG_H - 0.75)))
    warn.append(make_box('RMGTrolley', (2.20, 1.80, 1.20), (3.0, rmg_y, RMG_H - 2.4)))
    warn.append(make_box('RMGSpreader', (BOX_L + 0.4, BOX_W + 0.2, 0.80), (3.0, rmg_y, RMG_H - 6.6)))
    for sx in (-1, 1):
        crane.append(make_box(f'RMGTire_{sx}', (1.10, 0.60, 1.10),
                              (sx * (RMG_SPAN / 2.0), rmg_y + 4.0, 0.55)))

    # ── ⑦ 堆场照明塔 ×2（30 m）+ 港区闸口办公楼 ────────────────────────
    y_back_area = YARD_Y - (YARD_ROWS - 1) * (BOX_W + BOX_GAP) / 2.0 - 4.0
    for mx in LIGHT_X:
        concrete_dark.append(make_box(f'LightBase_{mx}', (1.60, 1.60, 0.50), (mx, y_back_area, 0.25)))
        steel.append(make_taper(f'LightMast_{mx}', 0.34, 0.18, LIGHT_MAST_H, 8,
                                (mx, y_back_area, 0.50 + LIGHT_MAST_H / 2.0)))
        steel.append(make_box(f'LightHead_{mx}', (3.20, 1.20, 0.40), (mx, y_back_area, LIGHT_MAST_H + 0.6)))
        for k in range(3):
            nav.append(make_box(f'LightLens_{mx}_{k}', (0.90, 0.80, 0.30),
                                (mx - 1.1 + k * 1.1, y_back_area, LIGHT_MAST_H + 0.35)))
    concrete.append(make_box('GateHouse', (GATE_L, GATE_D, GATE_H),
                             (BERTH_L / 2.0 - GATE_L / 2.0 - 1.5, y_back_area, GATE_H / 2.0)))
    concrete_dark.append(make_box('GateRoof', (GATE_L + 0.5, GATE_D + 0.5, 0.25),
                                  (BERTH_L / 2.0 - GATE_L / 2.0 - 1.5, y_back_area, GATE_H + 0.12)))
    # 港区道路（堆场陆侧环形通道）
    concrete.append(make_box('PortRoad', (BERTH_L - 6.0, 6.0, 0.10), (0, y_back_area, 0.05)))

    # ── ⑧ 按材质合并 ⇒ 8 primitive ───────────────────────────────────
    mat_c = make_material('Port_Concrete_Mat', CITY_PALETTE['concrete'], rough=0.90, metal=0.0)
    mat_cd = make_material('Port_ConcreteDark_Mat', CITY_PALETTE['concrete_dark'],
                           rough=0.92, metal=0.0)
    mat_s = make_material('Port_Steel_Mat', CITY_PALETTE['steel'], rough=0.50, metal=0.75)
    mat_cr = make_material('Port_Crane_Mat', CITY_PALETTE['crane_body'], rough=0.52, metal=0.45)
    mat_cf = make_material('Port_CraneFrame_Mat', CITY_PALETTE['crane_frame'], rough=0.58, metal=0.55)
    # 箱体材质：5 色各 1 槽（见 TINT 处的注释）
    box_mats = [make_material(f'Port_Container{i}_Mat', col, rough=0.72, metal=0.30)
                for i, col in enumerate(TINT)]
    mat_w = make_material('Port_Warn_Mat', CITY_PALETTE['crane_warn'], rough=0.60, metal=0.20)
    mat_n = make_material('Port_NavLight_Mat', CITY_PALETTE['floodlight'],
                          rough=0.28, metal=0.0, emissive=CITY_PALETTE['floodlight'],
                          emissive_intensity=0.4)

    groups = [
        (concrete, mat_c, 'Port_Concrete'),
        (concrete_dark, mat_cd, 'Port_ConcreteDark'),
        (steel, mat_s, 'Port_Steel'),
        (crane, mat_cr, 'Port_Crane'),
        (crane_frame, mat_cf, 'Port_CraneFrame'),
        (warn, mat_w, 'Port_Warn'),
        (nav, mat_n, 'Port_NavLight'),
    ]
    joined = [join_objects(objs, name) for objs, _m, name in groups]
    for (_objs, mat, _name), obj in zip(groups, joined):
        assign_material(obj, mat)

    # 箱体：按色 5 组 join
    for k, objs_k in enumerate(container):
        if not objs_k:
            continue
        obj = join_objects(objs_k, f'Port_Container{k}')
        assign_material(obj, box_mats[k])
        joined.append(obj)

    # ⚠ 全件米 → 世界单位（×0.1）；顺序：transform_apply flatten → ×0.1 → 钉扎 → 居中
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 贴地钉扎（**带容差**，见批次 47 教训 #10）。本件按设计所有几何都在 y ≥ 0，
    # 这里应当完全无动作；真出现告警即说明有件把坐标写错了轴位。
    PIN_TOLERANCE_M = 0.05
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z >= 0:
            continue
        if -min_z > PIN_TOLERANCE_M:
            print(f'[pin] ⚠ {obj.name} 最低点在 {min_z:.3f} m（超过容差），**不钉扎** —— '
                  f'查该组是否有把水平偏移写进 Z 的件', flush=True)
            continue
        obj.location.z = -0.1 * min_z

    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/port_terminal.glb')
    export_glb(out_path)
    print(f'✅ port_terminal.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
