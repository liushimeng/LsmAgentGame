#!/usr/bin/env python3
"""
build_palm_tree — 批次 44「树木真实感」棕榈（Trachycarpus fortunei，俗名老人葵 palm_tree.glb）。

── 选种理由：补齐 §130「声明了却从不接线」的 `palm` 死变体 ─────────────────
  `StreetPropsLayer.tsx:184` 的 `TREE_VARIANTS = ['oak', 'pine', 'palm']` 声明了三变体，
  但写入点 `L325/L727` 的 variant 在合并点 `L1224/L1226` 被丢弃 ⇒ 全城 1800 株
  实际只有一种树；而 `palm` **既无 GLB、也无程序化 fallback、`REAL_DIMS_M` 亦无对应条目**
  —— 是纯粹的空中变体。本件补上这条链路的资产端。

  **为什么选棕榈而不是蒲葵**（两者都是华南/东南沿海行道树，但耐寒性差一个量级）：
    · 蒲葵 *Livistona chinensis*：能耐 0 ℃ 左右，成年株**仅**短期耐 −5 ℃，
      「寒地多作盆栽观赏」，原产生于**沿海沙质土壤低地林**；
    · 棕榈 *Trachycarpus fortunei*：**耐 −15 ℃ 较强**（部分资料称 −26 ℃），
      耐风抗旱，**粗放**，耐寒区 7–11 区，**长江流域可露地越冬，华北（冬季最低温
      高于 −15 ℃）首选**。
  本城是温带内陆（城中有积雪山脊与冬季降雪），选耐寒更强的棕榈而非蒲葵。

── §27.0-1 真实形态调研 ────────────────────────────────────────────────
  | 部件 | 真实规格                                     | 依据                | 本件取值 |
  |------|----------------------------------------------|---------------------|----------|
  | 树高 | 8~12 m（最高 20 m）                          | 棕榈形态志          | 杆 3.6 + 叶 ⇒ **5.2 m** |
  | 头径 | **50~90 cm，基部显著膨大**，红褐色          | 棕榈形态志          | 底 ⌀0.60 → 顶 ⌀0.40 m |
  | 杆高（净干高）| 3.5~4.0 m（工程常用规格）              | 苗木市场分级        | **3.6 m** |
  | 冠幅 | 3~5 m（叶面积指数约 2.5）；工程苗 2.5~3.0 m   | 棕榈形态志 / 工程招标 | **3.0 m** |
  | 叶   | 掌状圆扇形，裂片间有白色丝状卷曲纤维（「白发」故名老人葵）| 棕榈形态志 | 11 片外展下垂扇叶 |
  | 干表面| **棕褐色、具环状叶痕与纵裂纹**               | 棕榈/蒲葵形态志     | **9 道环状叶痕**（最强识别特征）|

  **「基部膨大 + 环状叶痕 + 掌状扇形叶」三件套是棕榈科的形态身份证**。
  现状城里没有棕榈；本件把这三件都做实，而不是只做一个绿色圆柱 + 几个绿球。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；世界单位 ×U=0.1（**1 世界单位 = 10 m**）；节点 identity；minY=0。
  · 尺寸用米写经 `_m()` 换算；扇叶的 Z 向压扁与朝向在建完立即烘进几何。

用法：
  blender --background --python build_palm_tree.py -- \
    ClientWeb/src/assets/models/nature/palm_tree.glb
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
    export_glb, center_content_xz, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位（1 世界单位 = 10 m）

# ── 尺寸（米）────────────────────────────────────────────────────────────
TRUNK_H = 3.60          # 杆高（净干高）= 冠下高
TRUNK_R_B, TRUNK_R_T = 0.30, 0.20   # 头径 50~90 cm ⇒ 底 ⌀0.60 m 属该区间下沿
TRUNK_SEGS = 12

SCAR_N = 9              # 环状叶痕道数（棕榈最强识别特征）
SCAR_H = 0.055          # 叶痕凸起高
SCAR_PROUD = 0.025      # 叶痕凸出树皮的量

FROND_N = 12            # 掌状扇叶片数（成株 10 片以上为正常规格）
# **12 片而不是 11 片**：11 片在 360° 上不能均分方位角（360/11 = 32.7°），
# 两轴极值不等 ⇒ 冠幅变椭圆（实测 2.40 × 2.21 m）。12 片 30° 均布 + 15° 相位时
# max|cos| = max|sin| = 0.966，两轴等宽。
FROND_PHASE_DEG = 15.0
FROND_PETIOLE = 1.50    # 叶柄长（真实 1~2 m）
FROND_BLADE_R = 0.55    # 扇叶半径（真实单叶直径 1~1.8 m ⇒ 半径 0.5~0.9 m）
FROND_BLADE_FLATTEN = 0.32   # 扇叶沿叶柄方向的压扁（掌状叶是「片」不是「球」）
FROND_TILT_MIN = 20.0   # 倾角区间（离铅垂）：棕榈叶**外展后下垂**，不向上翘
FROND_TILT_MAX = 58.0
PETIOLE_R_B, PETIOLE_R_T = 0.038, 0.024
# 冠幅自校验：半宽 ≈ 1.50·sin58°·cos15° + 叶片横向 ≈ 1.23 + 0.40 = 1.49 m
# ⇒ 冠幅 2.98 m（对应 REAL_DIMS_M.palmTree.x/z = 3.0 m）

SPEAR_H = 0.80          # 顶芽（未展开的中央新叶）—— 棕榈顶心特征
SPEAR_R = 0.11

# ── 材质 ─────────────────────────────────────────────────────────────────
TRUNK_C = CITY_PALETTE['palm_trunk']     # 红褐树干（基部膨大 + 红褐是棕榈标志）
SCAR_C = CITY_PALETTE['palm_trunk']      # 叶痕同族（见下）
FROND_C = CITY_PALETTE['palm_frond']     # 掌状扇叶青绿（蜡质叶面，比阔叶更亮）


def _m(v):
    return v * U


def _m3(v):
    return (_m(v[0]), _m(v[1]), _m(v[2]))


def _bake(obj, scale=None, rot=None, loc=None):
    """把 scale / rotation / location **立即烘进几何**（不挂成 node transform，§27.3-6）。"""
    if scale is not None:
        obj.scale = scale
    if rot is not None:
        obj.rotation_euler = rot
    if loc is not None:
        obj.location = loc
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return obj


def _rot_for(azimuth, tilt):
    """让局部 +Z 指向 (azimuth, tilt) 对应的方向（R = Rz(az+π/2)·Rx(tilt)）。"""
    return (tilt, 0.0, azimuth + math.pi / 2)


def main():
    reset_scene()
    set_unit_meters()

    trunk_objs = []
    scar_objs = []
    frond_objs = []

    trunk_mat = make_material('PalmTree_Trunk_Mat', TRUNK_C, rough=0.90, metal=0.0)
    # 叶痕用略浅的同族色：真实环状叶痕是**纤维层**，比周围树皮浅一档
    scar_mat = make_material('PalmTree_Scar_Mat', SCAR_C, rough=0.82, metal=0.0)
    frond_mat = make_material('PalmTree_Frond_Mat', FROND_C, rough=0.72, metal=0.0)

    # ① 树干（基部显著膨大的收分圆台）
    trunk_objs.append(make_taper('Trunk', _m(TRUNK_R_B), _m(TRUNK_R_T),
                                 _m(TRUNK_H), TRUNK_SEGS, (0, 0, _m(TRUNK_H / 2))))

    # ② 环状叶痕 ×9 —— 沿树干按收分轮廓外凸，随高度变半径（不能等径，否则下部会浮空）
    for i in range(SCAR_N):
        z = TRUNK_H * (i + 0.5) / SCAR_N
        t = z / TRUNK_H
        r_local = TRUNK_R_B + (TRUNK_R_T - TRUNK_R_B) * t
        scar_objs.append(make_taper(
            f'Scar_{i}', _m(r_local + SCAR_PROUD), _m(r_local + SCAR_PROUD),
            _m(SCAR_H), TRUNK_SEGS, (0, 0, _m(z))))

    # ③ 冠心（叶片着生处的膨大团）
    trunk_objs.append(make_taper('CrownBoss', _m(TRUNK_R_T + 0.06), _m(TRUNK_R_T - 0.04),
                                 _m(0.30), TRUNK_SEGS, (0, 0, _m(TRUNK_H + 0.10))))

    # ④ 掌状扇叶 ×11 —— 自冠心外展下垂，倾角在 [20°, 58°] 间均布并交错
    crown_z = TRUNK_H + 0.18
    for i in range(FROND_N):
        # ⚠ **倾角必须与方位角去相关** —— 若按 `lerp(min, max, i/(N-1))` 线性排布，
        #   最长的叶（i=11）恰好落在 az=345°（几乎正 +X），而 Z 轴方向最长的叶
        #   （az≈75°/105°）倾角只有 27°/31° ⇒ 冠幅被系统性偏成
        #   2.68 × 2.43 m 的椭圆。这里用固定互质步长做**确定性置换**，
        #   把倾角序列打散到方位角上（与城市其余布点的 mulberry32 同款可复现思路）。
        slot = (i * 7919) % FROND_N
        tilt = math.radians(FROND_TILT_MIN
                            + (FROND_TILT_MAX - FROND_TILT_MIN) * (slot / (FROND_N - 1)))
        az = i * 2 * math.pi / FROND_N + math.radians(FROND_PHASE_DEG)
        st = math.sin(tilt)
        d = (math.cos(az) * st, math.sin(az) * st, math.cos(tilt))

        # ④a 叶柄（自冠心外伸）
        center = (d[0] * FROND_PETIOLE / 2, d[1] * FROND_PETIOLE / 2,
                  crown_z + d[2] * FROND_PETIOLE / 2)
        trunk_objs.append(make_taper(
            f'Petiole_{i}', _m(PETIOLE_R_B), _m(PETIOLE_R_T), _m(FROND_PETIOLE),
            6, _m3(center), rot=_rot_for(az, tilt)))

        # ④b 扇叶叶片：沿叶柄方向压扁的「桨」，长在叶柄末端
        tip = (d[0] * FROND_PETIOLE, d[1] * FROND_PETIOLE, crown_z + d[2] * FROND_PETIOLE)
        blade = make_icosphere(f'Blade_{i}', _m(FROND_BLADE_R), 0, (0, 0, 0))
        _bake(blade,
              scale=(1.0, 1.0, FROND_BLADE_FLATTEN),
              rot=_rot_for(az, tilt),
              loc=_m3(tip))
        frond_objs.append(blade)

    # ⑤ 顶芽（中央未展开新叶，棕榈顶心特征）
    frond_objs.append(make_taper('Spear', _m(SPEAR_R), 0.0, _m(SPEAR_H), 8,
                                 (0, 0, _m(crown_z + SPEAR_H / 2 - 0.1))))

    # ⑥ 按材质合并 ⇒ 3 个 primitive
    joined = [
        join_objects(trunk_objs, 'PalmTree_Trunk'),
        join_objects(scar_objs, 'PalmTree_Scar'),
        join_objects(frond_objs, 'PalmTree_Frond'),
    ]
    for o, m in zip(joined, (trunk_mat, scar_mat, frond_mat)):
        assign_material(o, m)

    # ⑦ X/Z 内容盒居中（11 片叶的方位角均布但 icosphere 面内非对称 ⇒ 仍会偏心）
    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/palm_tree.glb')
    export_glb(out_path)
    print(f'✅ palm_tree.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
