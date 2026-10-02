#!/usr/bin/env python3
"""
build_pine_tree — 批次 44「树木真实感」**重制**：常绿针叶树（黑松/雪松 pine_tree.glb）。

── 重制起因（批次 44 T5 同类问题）────────────────────────────────────────
  v19 资产是 **3 个 8 边圆锥叠放，共 70 面 / 8.6 KB** —— 那是「圣诞树玩具」，
  不是松树：没有主干分枝结构、没有针叶簇、层与层之间是光滑的圆锥面相接。
  本次按真实形态重制：主干 + **4 层「大枝平展」针叶盘** + 顶塔尖。

── §27.0-1 真实结构调研（DB11/T 211—2017 北京地区园林绿化常用常绿乔木规格）──
  | 部件   | 真实规格                                | 依据                | 本件取值 |
  |--------|-----------------------------------------|---------------------|----------|
  | 株高   | 雪松 6.0~8.0 m 档                       | DB11/T 211—2017     | **7.0 m** |
  | 冠幅   | 雪松 6.0~8.0 m 档 ≥5.0(Ⅰ级) / ≥4.0(Ⅱ级)  | 同上                 | **4.0 m**（Ⅱ~Ⅰ 级之间）|
  | 地径   | 雪松该档 ≥15 cm                        | 同上                 | ⌀0.28 m |
  | 冠形   | 雪松**宽塔形**，**大枝平展**、小枝下垂    | 植物智 Cedrus deodara | 压扁叶盘 + 平展枝 |
  | 枝下高 | 雪松极低（≤0.3~1.0 m），下枝近地面平展   | DB11/T 211—2017 分枝点高栏 | 首层盘 z=2.0 m |

  **「大枝平展」是雪松/黑松与塔形冷杉的关键形态差异** —— 冷杉枝条向上斜出呈尖塔形，
  松属枝条近水平平展。本件用「压扁叶盘（Z 向 0.45）+ 近水平放射小枝」表达这一特征，
  而不是继续用圆锥层叠（圆锥 = 尖塔形 = 冷杉观感，**形态学上就选错了树**）。
  黑松（Pinus thunbergii）树冠为「宽圆锥状或伞形」，形态介于两者之间，层数少而更横展。

  ⚠ **旧表值是「资产现值」而非真实值**：`REAL_DIMS_M.pineTree` 原注释自认
  「本批不重导，故表值 = 资产现值（3.4×5.4×3.4 m），而非真实云杉 8~12 m 的拟值」。
  本次重制后该自认作废，表值改为本件的**真实** 4.0 × 7.0 × 4.0 m，
  并同步 `edge/EastForest` / `edge/NorthMountains` 的 sizeTarget 与
  `edge/proceduralFlora` 的 PINE_KX/PINE_KY 归一化常数（§27.3-7 三侧同步）。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；世界单位 ×U=0.1（**1 世界单位 = 10 m**）；节点 identity；minY=0。
  · 尺寸用米写经 `_m()` 换算；不靠 obj.scale 表达尺寸（唯一例外是叶盘的 Z 向压扁，
    建完立即 `transform_apply` 烘进几何，见 `_flatten`）。

用法：
  blender --background --python build_pine_tree.py -- \
    ClientWeb/src/assets/models/nature/pine_tree.glb
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
    make_taper, make_icosphere, make_cylinder,
    make_material, assign_material, join_objects,
    export_glb, center_content_xz, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位（1 世界单位 = 10 m）

# ── 尺寸（米）────────────────────────────────────────────────────────────
TRUNK_H = 6.50           # 主干高（顶塔尖再叠 0.5 ⇒ 交付 7.0 m）
TRUNK_R_B, TRUNK_R_T = 0.14, 0.09   # 地径 ⌀0.28 m → 顶部收分
TRUNK_SEGS = 10

SPIRE_H = 0.50           # 顶部塔尖高
SPIRE_R = 0.30           # 塔尖底半径

# ── 4 层针叶盘：(z 层高, 环半径, 中央叶盘半径, 盘 Z 压扁, 小枝数, 相位°) ──
#
# **半宽由「环半径 + 枝端簇半径」决定，不是中央叶盘**（叶盘在中心、不外伸）：
#     L0 半宽 = 1.78·cos15° + 0.30 = 2.019 m ⇒ 冠幅 4.04 m
# **相位 15° 是为了两轴等宽**：6 枝若从 0° 起布，max|sin| 只有 sin75°=0.966 的邻居、
# 另一个轴是 1.0，冠幅会变成椭圆；取 15°（= 30°/2）时 max|cos| = max|sin| = 0.966。
NEEDLE_TIERS = [
    (2.00, 1.78, 1.05, 0.45, 6, 15.0),
    (3.60, 1.45, 0.90, 0.45, 6, 15.0),
    (5.00, 1.10, 0.68, 0.45, 4, 0.0),
    (6.20, 0.70, 0.48, 0.45, 4, 0.0),
]
BRANCH_R_B, BRANCH_R_T = 0.045, 0.020   # 小枝（近水平平展，故极细）
BRANCH_SEGS = 6
CLUMP_R = 0.30           # 枝端针叶簇半径（**冠幅的真正外缘**）
CLUMP_FLATTEN = 0.55     # 簇的 Z 向压扁（针叶层是「片」不是「球」）
BRANCH_DROOP = 0.18      # 小枝末端下垂（雪松/黑松「小枝下垂」特征）
INNER_RING_RATIO = 0.52  # 内圈叶簇半径 / 外圈环半径（填中央叶盘与枝端簇之间的空档）

# ── 材质 ─────────────────────────────────────────────────────────────────
BARK = CITY_PALETTE['bark_pine']       # 老皮深棕红、鳞片状开裂
NEEDLE_DARK = CITY_PALETTE['needle_pine']   # 下层针叶（光照少，压暗）
NEEDLE_LIGHT = CITY_PALETTE['foliage']      # 上层针叶（受光多，用城市绿提亮）


def _m(v):
    return v * U


def _m3(v):
    return (_m(v[0]), _m(v[1]), _m(v[2]))


def _flatten(obj, z_scale):
    """把 Z 向缩放**立即烘进几何**（不是挂成 node scale —— §27.3-6）。"""
    obj.scale = (1.0, 1.0, z_scale)
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=False, rotation=False, scale=True)
    return obj


def main():
    reset_scene()
    set_unit_meters()

    bark_objs = []
    dark_objs = []
    light_objs = []

    bark_mat = make_material('PineTree_Bark_Mat', BARK, rough=0.95, metal=0.0)
    dark_mat = make_material('PineTree_Needle_Dark_Mat', NEEDLE_DARK, rough=0.88, metal=0.0)
    light_mat = make_material('PineTree_Needle_Light_Mat', NEEDLE_LIGHT, rough=0.88, metal=0.0)

    # ① 主干（收分圆台，贯通）
    bark_objs.append(make_taper('Trunk', _m(TRUNK_R_B), _m(TRUNK_R_T),
                                _m(TRUNK_H), TRUNK_SEGS, (0, 0, _m(TRUNK_H / 2))))

    # ② 4 层针叶盘 + 平展小枝
    for li, (z, ring_r, disc_r, squash, n_branch, phase_deg) in enumerate(NEEDLE_TIERS):
        target = dark_objs if li < 2 else light_objs
        phase = math.radians(phase_deg)

        # ②a 中央叶盘（Z 向压扁 ⇒ 表达「平展层」而非尖塔）
        disc = make_icosphere(f'Tier_{li}_Disc', _m(disc_r), 0, (0, 0, _m(z)))
        target.append(_flatten(disc, squash))

        # ②b 近水平平展的小枝 + 枝端针叶簇 + 内圈叶簇
        for j in range(n_branch):
            az = j * 2 * math.pi / n_branch + phase
            # 枝：从主干近水平伸出，末端下垂 BRANCH_DROOP
            tip_r = ring_r
            tip = (math.cos(az) * tip_r,
                   math.sin(az) * tip_r,
                   z - BRANCH_DROOP)
            length = math.sqrt(tip_r ** 2 + BRANCH_DROOP ** 2)
            # ⚠ `_branch_dir` 的 tilt 是**离铅垂**的夹角 ⇒ 水平外伸 ≈ 90°。
            #   曾写成 `atan2(BRANCH_DROOP, tip_r)`（两个参数对调 ⇒ 5.8°），
            #   结果「大枝平展」长成了**竖直的扫帚杆**。
            tilt = math.atan2(tip_r, BRANCH_DROOP)     # 水平远大于下垂 ⇒ 近乎平展
            center = (tip[0] / 2, tip[1] / 2, z - BRANCH_DROOP / 2)
            bark_objs.append(make_taper(
                f'Tier_{li}_Branch_{j}', _m(BRANCH_R_B), _m(BRANCH_R_T), _m(length),
                BRANCH_SEGS, _m3(center),
                rot=(tilt, 0.0, az + math.pi / 2)))

            clump = make_icosphere(f'Tier_{li}_Clump_{j}', _m(CLUMP_R), 0, _m3(tip))
            target.append(_flatten(clump, CLUMP_FLATTEN))

            # 内圈叶簇：填住「中央叶盘 ↔ 枝端簇」之间的空档。
            # 缺了它，每层针叶盘只剩一圈外缘的孤立绿球 + 中心一个小碟，中间透空。
            inner_r = ring_r * INNER_RING_RATIO
            inner = (math.cos(az + math.pi / n_branch) * inner_r,
                     math.sin(az + math.pi / n_branch) * inner_r,
                     z - BRANCH_DROOP * 0.35)
            inner_clump = make_icosphere(f'Tier_{li}_Inner_{j}', _m(CLUMP_R * 1.15), 0,
                                         _m3(inner))
            target.append(_flatten(inner_clump, CLUMP_FLATTEN))

    # ③ 顶部塔尖（塔形的收分）
    dark_objs.append(make_taper('Spire', _m(SPIRE_R), 0.0,
                                _m(SPIRE_H), 8,
                                (0, 0, _m(TRUNK_H + SPIRE_H / 2))))

    # ④ 按材质合并 ⇒ 3 个 primitive（不合并的话 30 个部件 = 30 个 instancedMesh）
    joined = [
        join_objects(bark_objs, 'PineTree_Bark'),
        join_objects(dark_objs, 'PineTree_Needle_Dark'),
        join_objects(light_objs, 'PineTree_Needle_Light'),
    ]
    for o, m in zip(joined, (bark_mat, dark_mat, light_mat)):
        assign_material(o, m)

    # ⑤ X/Z 内容盒居中（icosphere 面内非旋转对称 ⇒ 均布方位角仍会偏心，见 __common__ 注释）
    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/pine_tree.glb')
    export_glb(out_path)
    print(f'✅ pine_tree.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
