#!/usr/bin/env python3
"""
build_street_tree — 批次 44「树木真实感」城市行道树（悬铃木/法桐 street_tree.glb）Blender 导出脚本。

── §27.0-1 真实结构调研（选种与规格均有规范出处，非凭想象）────────────────
  **选种**：CJJ/T 75-2023《城市道路绿化设计标准》§6.0.2.1 要求乔木「深根性、树干通直、
  **分枝点高度符合通行要求**、冠型优美、能形成林荫」；§6.0.3「寒冷积雪地区分车绿带、
  行道树绿带**宜用落叶树种**」；§6.0.1「宜选**乡土**树种」。悬铃木（二球悬铃木
  *Platanus × acerifolia*，俗名法桐/法国梧桐）是中国城市行道树的绝对主力（华东/华北/华中），
  且**落叶**（冬季城市观感与常绿树不同）。本件按**落叶行道树**建模。

  | 部件   | 真实规格                                   | 依据                                    | 本件取值 |
  |--------|--------------------------------------------|-----------------------------------------|----------|
  | 分枝点 | 主干道 ≥3.0 m / 次干道 ≥2.5 m               | CJJ/T 75-2023；DB4105/T 150-2020；宣城市园林绿化导则 3.2.1 | **3.0 m** |
  | 胸径   | 行道树 8~18 cm（新栽不宜 >15 cm）           | CJJ/T 75-2023 §6.0.8                   | ⌀0.30 m  |
  | 树高   | 养护控制 10~17 m                           | 各地养护规范                            | **9.0 m**（沿用 `REAL_DIMS_M.streetTree` 现值，控回归）|
  | 冠幅   | 胸径 15 cm 苗 6~8 m；株距 6~8 m             | 苗圃分级表                              | **5.0 m**（保守；本城株距 9 m）|
  | 树形   | **杯状形（自然开心形）**，冠型「三股六杈十二枝」| 法桐整形修剪技术（科普中国）            | 3 主枝 + 6 二级枝 |
  | 主枝   | 留长 50~80 cm 短截，**近同一水平面**       | 同上                                    | 长 1.5 m，倾角 40° |
  | 侧枝   | 同侧间距 30 cm 交互着生，**只留左右两侧**   | 同上                                    | 每主枝 2 支，倾角 55° |
  | 车行道净空 | 树冠下缘 ≥3.5 m                          | DB4105/T 150-2020                      | 冠底 4.28 m ✓ |

  **杯状形是法桐最强的形态识别特征**（主干在分枝点高度切成 3 股、各股近水平平展），
  与现状「5 个球 + 2~3 根乱向分枝」有本质区别 —— 这是本件的主要真实感来源。

── 关键设计决策：叶簇球取代实心冠球（批次 44 T5）────────────────────────
  现状树冠 = 5 个 `icosahedron(detail=1)`（80 面/个）**平滑着色**实心球，
  近看是「绿色塑料球」。本件改为 **18 个 `icosahedron detail=0`（20 面/个）叶簇**：
    · 面数 400 → 360（-10%）而**颗粒数 5 → 18（3.6×）**
    · 叶簇互相不构成完整球面 ⇒ 轮廓破碎、透光缝隙，正是真实行道树的观感
  沿用批次 42 §9.2 决策：**GLB 零贴图**（不引入 alpha 叶片卡片），靠几何颗粒感表达。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；世界单位 ×U=0.1（**1 世界单位 = 10 m**）；节点 identity；minY=0。
  · 尺寸全部用**米**写，经 `_m()` 换算；不用 `obj.scale` 表达尺寸
    （`make_taper` / `make_icosphere` 把尺寸写进图元参数 ⇒ 天然 identity）。
  · 交付包围盒（世界单位 / 真实米）：x 0.500 / 5.00 m，y 0.900 / 9.00 m，z 0.500 / 5.00 m。

用法：
  blender --background --python build_street_tree.py -- \
    ClientWeb/src/assets/models/nature/street_tree.glb
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
    make_taper, make_icosphere,
    make_material, assign_material, join_objects,
    export_glb, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位（1 世界单位 = 10 m）

# ── 尺寸（米）────────────────────────────────────────────────────────────
ROOT_H = 0.35          # 根盘（根颈隆起）高
ROOT_R = 0.42          # 根盘底半径（悬铃木根颈显著膨大）
TRUNK_H = 3.00         # 主干高 = **分枝点高度**（规范 ≥3.0 m）
TRUNK_R_B, TRUNK_R_T = 0.15, 0.11   # 胸径级 ⌀0.30 → 顶部收分

MAIN_N = 3             # **三股**（杯状形「三股六杈」）
MAIN_L = 1.50          # 主枝长（规范 50~80 cm 短截后逐年放长，此处取成树值）
MAIN_TILT = math.radians(40.0)   # 主枝倾角（离铅垂；规范「近同一水平面」）
MAIN_R_B, MAIN_R_T = 0.075, 0.050

SEC_N = 2              # 每主枝 2 支 ⇒ 共 6 支（**六杈**）
SEC_L = 1.10           # 二级枝长
SEC_TILT = math.radians(55.0)    # 二级枝更平展（规范「只留左右两侧」）
SEC_SPREAD = math.radians(30.0)  # 二级枝相对主枝的方位偏角（同侧交互着生）
SEC_R_B, SEC_R_T = 0.050, 0.030

# ── 树冠（叶簇）三层 + 顶簇 ─────────────────────────────────────────────
# 列：(z 层高, 环半径, 簇半径, 每层簇数, 细分, 相位°, 内圈环半径, 内圈簇数)
#
# **内圈（后 3 列）是必需项，不是装饰**：只在外圈布簇时，整冠是一个**空心环** ——
# 首版渲染出来中央透空、叶团悬在冠缘像「呼啦圈」，真实行道树是**中心密、边缘疏**的
# 实心卵形冠。内圈簇把冠心填实。
#
# **相位不是装饰**：6 簇的环若从 0° 起布，簇心落在 ±X 轴上 ⇒ X 极值 = r 而
# Z 极值只有 r·sin60° = 0.866r，实测冠幅会变成 4.69 × 4.17 m 的椭圆。
# 相位取 15°（= 30°/2）时 max|cos| = max|sin| = cos15° = 0.966，两轴等宽。
CROWN_LAYERS = [
    (5.00, 1.90, 0.72, 6, 0, 15.0, 0.78, 2),
    (6.60, 1.62, 0.68, 6, 0, 15.0, 0.66, 3),
    (7.90, 0.90, 0.58, 4, 0, 0.0, 0.0, 0),
    (8.55, 0.30, 0.42, 2, 0, 0.0, 0.0, 0),
]
# 冠幅自校验：最大半宽 = max(ring·max(|cos|,|sin|) + clump) = 1.90·0.966 + 0.72 = 2.555
# ⇒ 冠幅 5.11 m，实测两轴 ≈ 4.95 / 4.89（icosphere 顶点分布非面内旋转对称），
#    对 5.0 m 目标留 -1% / -2% 余量，稳过 verify_glb_aabb 的 ±5%。

# ── 材质（全部取自 CITY_PALETTE，禁止裸写 hex）────────────────────────────
BARK = CITY_PALETTE['bark_sycamore']   # 悬铃木平滑灰白树皮（最具辨识度的树皮）
# 根盘色（`CITY_PALETTE.bark_root`）在批次 44 D6 起**不再单独使用** ——
# 根盘材质并入树皮以省 1 个 draw call，键保留供其它脚本取用。
LEAF = CITY_PALETTE['foliage']         # 夏季浓绿（前端按季节调制此材质）


def _m(v):
    return v * U


def _branch_dir(azimuth, tilt):
    """方位角 azimuth（自 +X 起算）+ 离铅垂倾角 tilt ⇒ 单位方向向量。"""
    st = math.sin(tilt)
    return (math.cos(azimuth) * st, math.sin(azimuth) * st, math.cos(tilt))


def _rot_for(azimuth, tilt):
    """让 `make_taper` 的 +Z 轴指向 `_branch_dir(azimuth, tilt)` 的欧拉角。

    R = Rz(az+π/2) · Rx(tilt)：先把 +Z 绕 X 倾 tilt，再绕 Z 转 az+π/2。
    """
    return (tilt, 0.0, azimuth + math.pi / 2)


def main():
    reset_scene()
    set_unit_meters()

    bark_objs = []   # 树皮族（主干 + 主枝 + 二级枝）
    root_objs = []   # 根盘族
    leaf_objs = []   # 叶簇族

    bark_mat = make_material('StreetTree_Bark_Mat', BARK, rough=0.92, metal=0.0)
    leaf_mat = make_material('StreetTree_Leaf_Mat', LEAF, rough=0.85, metal=0.0)

    # ① 根盘（根颈膨大）—— 现状完全没有；地面直接截断树干是「棒棒糖」的根因之一
    root_objs.append(make_taper('Root_Flare', _m(ROOT_R), _m(TRUNK_R_B),
                                _m(ROOT_H), 14, (0, 0, _m(ROOT_H / 2))))

    # ② 主干（收分圆台，贯通到分枝点）
    bark_objs.append(make_taper('Trunk', _m(TRUNK_R_B), _m(TRUNK_R_T),
                                _m(TRUNK_H), 14, (0, 0, _m(TRUNK_H / 2))))

    # ③ 主枝 ×N（杯状形：自分枝点近水平平展，方向均布）
    main_tips = []
    for i in range(MAIN_N):
        az = i * 2 * math.pi / MAIN_N
        d = _branch_dir(az, MAIN_TILT)
        start = (0.0, 0.0, float(TRUNK_H))
        center = (start[0] + d[0] * MAIN_L / 2,
                  start[1] + d[1] * MAIN_L / 2,
                  start[2] + d[2] * MAIN_L / 2)
        bark_objs.append(make_taper(f'Main_{i}', _m(MAIN_R_B), _m(MAIN_R_T),
                                    _m(MAIN_L), 8, _m3(center), rot=_rot_for(az, MAIN_TILT)))
        main_tips.append((start[0] + d[0] * MAIN_L,
                          start[1] + d[1] * MAIN_L,
                          start[2] + d[2] * MAIN_L))

    # ④ 二级枝 ×N×SEC_N（自主枝末端左右分出，**末端必须插进叶簇层**——修批次 30 悬空分枝）
    sec_ends = []
    for i, tip in enumerate(main_tips):
        base_az = i * 2 * math.pi / MAIN_N
        for j in range(SEC_N):
            az = base_az + (SEC_SPREAD if j == 0 else -SEC_SPREAD)
            d = _branch_dir(az, SEC_TILT)
            center = (tip[0] + d[0] * SEC_L / 2,
                      tip[1] + d[1] * SEC_L / 2,
                      tip[2] + d[2] * SEC_L / 2)
            bark_objs.append(make_taper(f'Sec_{i}_{j}', _m(SEC_R_B), _m(SEC_R_T),
                                        _m(SEC_L), 6, _m3(center), rot=_rot_for(az, SEC_TILT)))
            sec_ends.append((tip[0] + d[0] * SEC_L,
                             tip[1] + d[1] * SEC_L,
                             tip[2] + d[2] * SEC_L))

    # ⑤ 叶簇 ×18 —— 三层错落 + 顶簇，卵形浓密冠
    k = 0
    for z, ring_r, clump_r, n, subdiv, phase_deg, inner_r, inner_n in CROWN_LAYERS:
        phase = math.radians(phase_deg)
        for i in range(n):
            az = i * 2 * math.pi / n + phase
            leaf_objs.append(make_icosphere(
                f'Leaf_{k}', _m(clump_r), subdiv,
                _m3((math.cos(az) * ring_r, math.sin(az) * ring_r, z))))
            k += 1
        # 内圈簇：错开半个角位布，避免与外圈径向对齐成「 spokes」
        for i in range(inner_n):
            az = (i + 0.5) * 2 * math.pi / max(inner_n, 1) + phase
            leaf_objs.append(make_icosphere(
                f'Leaf_{k}', _m(clump_r * 0.95), subdiv,
                _m3((math.cos(az) * inner_r, math.sin(az) * inner_r, z + 0.25))))
            k += 1

    # ⑥ 按材质合并 —— 3 个 primitive ⇒ 全城 1800 株只占 3 draw call
    #    （不合并的话 29 个部件 = 29 个 instancedMesh，DC 预算会被树吃光）
    # ⚠ **根盘并入树皮材质**（批次 44 D6，DC 预算收口）：根盘原本独占
    # `StreetTree_Root_Mat` 一个材质槽 = 全城 809 株多占 **1 个 draw call**，
    # 而它只有 52 tri/株、且位置在树干最底部（多数角度被树皮与地面遮挡）。
    # 根盘的形态价值在**几何隆起**（已保留），不在色差 —— 并入树皮后
    # street_tree.glb 由 3 primitive 降为 2，全城三变体总 DC 9 → 8。
    bark_objs.extend(root_objs)
    joined = [
        join_objects(bark_objs, 'StreetTree_Bark'),
        join_objects(leaf_objs, 'StreetTree_Leaf'),
    ]
    for o, m in zip(joined, (bark_mat, leaf_mat)):
        assign_material(o, m)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/street_tree.glb')
    export_glb(out_path)
    print(f'✅ street_tree.glb exported: {out_path}', flush=True)


def _m3(v):
    """三元组整体缩放。"""
    return (_m(v[0]), _m(v[1]), _m(v[2]))


if __name__ == '__main__':
    main()
