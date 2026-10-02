#!/usr/bin/env python3
"""
build_park_pavilion — 批次 45「公园设施真实感」六角亭（civic/park_pavilion.glb，重制）。

── 重制理由：批次 18-AA 的程序化凉亭是玩具 ──────────────────────────────
  现状 `civic/ParkExtras.tsx::pavilionParts` = 6 根等径圆柱 + 1 个六棱锥 + 1 颗球，
  锥顶直接坐在柱头上 —— 无台基、无柱头枋、无美人靠、无檐口过渡、无宝顶，
  20 件合并共享 roughness 0.8 / metalness 0.25（批次 28 二轮取舍），木构/瓦面不可辨。

── §27.0-1 真实形态调研（清式小式亭榭比例 + 现代仿古做法）────────────────
  | 部件 | 真实规格                                  | 依据                          | 本件取值 |
  |------|-------------------------------------------|-------------------------------|----------|
  | 对边宽| 六角亭适用 3~5 m（对角 3.5~6 m）          | 景艺圈 / 景观亭规格汇总        | 3.60 m（柱心，对角 4.157）|
  | 柱高 | 清式小式「面宽一丈柱高八尺」= 面宽×0.8     | 《中国建筑史》清式做法         | 2.80 m（2.88 取整踏步归 2.8）|
  | 柱径 | 柱高≈柱径×10；现代仿古常收至 ⌀0.16~0.22   | 同上                          | ⌀0.176→0.160（收分）+ 柱础 ⌀0.26 |
  | 台基 | 石砌台明 30~60 cm                          | 《中国古建筑营造技术》         | 0.40 m（两层收边 + 一步台阶）|
  | 出檐 | 无斗栱小式上檐出 = 檐柱高 3/10 ⇒ 0.84      | 《古建筑木作营造技术》         | 0.85 m（角梁外挑至 0.95）|
  | 举高 | 宋举折 1/4.77~1/3，攒尖顶较陡              | 《营造法式》/ 亭榭探析         | 两段折面 0.72+0.73 = 1.45 m |
  | 宝顶 | 座 + 球尖，通高 0.35~0.5                   | 仿古做法汇总                   | 座 0.12 + 球 ⌀0.28 + 尖 0.15 |
  | 美人靠| 座面高 0.40~0.45；柱间满圈，入口留缺      | 园林坐凳美人靠做法             | 座板高 0.42、靠背倾角 10° |

  总高 ≈ 0.40（台基）+ 2.80（柱）+ 1.45（两段攒尖）+ 0.54（宝顶）≈ 5.19 m。
  屋面出檐使水平包围（对角 + 2×0.95 ≈ 6.06 m）> 总高 —— **属正确摆放**
  （宽亭矮檐是六角亭真实比例），verify_glb_aabb 走 axis='x_flat' 语义豁免。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 Z-up；米制；导出自动 Yup ⇒ three.js 直立；节点 identity；minY=0。
  · 材质槽 5 个：石浅（台基层1+台阶+柱础）/ 石深（层2+封檐板+宝顶座）/
    木构（柱+枋+美人靠+角梁）/ 瓦面（两段攒尖）/ 宝顶金属（球+尖）。
  · 入口台阶朝 Blender +Y 面（导出后 glTF -Z）；美人靠该面留缺。

用法：
  blender --background --python build_park_pavilion.py -- \
    ClientWeb/src/assets/models/civic/park_pavilion.glb
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
    make_box, make_taper, make_sphere,
    make_material, assign_material, join_objects,
    export_glb, CITY_PALETTE,
)

# ── 尺寸（米）────────────────────────────────────────────────────────────
R_COL = 3.60 / math.sqrt(3.0)   # 柱心外接圆半径：对边宽 3.60 ⇒ R = 2.078
SIDE = R_COL                    # 正六边形边长 = 外接圆半径（柱间距）
COL_H = 2.80                    # 柱高（面宽 3.60 × 0.8 = 2.88，归整 2.80）
COL_R_B, COL_R_T = 0.088, 0.080  # 柱径收分（⌀0.176 → ⌀0.160）
COL_BASE_R, COL_BASE_H = 0.13, 0.12   # 柱础 ⌀0.26 × 0.12
PLINTH_H1, PLINTH_H2 = 0.22, 0.18    # 台基两层（总 0.40）
PLINTH_R1, PLINTH_R2 = 2.43, 2.28    # 台明出沿 0.35 / 0.20
EAVE_E = 0.85                   # 上檐出（柱高 3/10 = 0.84）
HIP_EXT = 0.95                  # 角梁外挑（檐角 = 出檐 + 0.10 起翘余量）
ROOF_R_EAVE = R_COL + EAVE_E    # 下段攒尖底半径 2.928
ROOF_R_MID = 1.35               # 下段顶 = 上段底（两段折面：下缓上陡）
ROOF_H1, ROOF_H2 = 0.72, 0.73   # 举高 1.45
SEAT_H = 0.42                   # 美人靠座面高（台基顶起算）
BACK_H = 0.38                   # 靠背高
BACK_TILT = math.radians(10.0)  # 靠背倾角（100°~110° 公共排椅惯例的下沿）
ENTER_FACE_DEG = 90.0           # 入口台阶所在面（Blender +Y 面中点角 90°）

FIN_BALL_R = 0.14
FIN_TIP_H = 0.15


def main():
    reset_scene()
    set_unit_meters()

    stone_a, stone_b = [], []   # 石浅 / 石深
    timber, roof, finial = [], [], []

    z_plinth = PLINTH_H1 + PLINTH_H2          # 0.40 台基顶
    z_eave = z_plinth + COL_H                  # 3.20 檐口（柱顶）

    # ① 台基两层（六棱柱）+ 入口台阶
    stone_a.append(make_taper('Plinth_L1', PLINTH_R1, PLINTH_R1, PLINTH_H1, 6,
                              (0, 0, PLINTH_H1 / 2)))
    stone_b.append(make_taper('Plinth_L2', PLINTH_R2, PLINTH_R2, PLINTH_H2, 6,
                              (0, 0, PLINTH_H1 + PLINTH_H2 / 2)))
    step_y = PLINTH_R1 * math.cos(math.radians(30)) + 0.18
    stone_a.append(make_box('Step', (1.20, 0.36, PLINTH_H1),
                            (0, step_y, PLINTH_H1 / 2)))

    # ② 6 檐柱 + 柱础 + 柱头枋（柱角 0/60/…；面中角 = 柱角 + 30°）
    col_angles = [math.radians(60 * k) for k in range(6)]
    face_angles = [a + math.radians(30) for a in col_angles]
    for i, a in enumerate(col_angles):
        x, y = math.cos(a) * R_COL, math.sin(a) * R_COL
        stone_a.append(make_taper(f'ColBase_{i}', COL_BASE_R, COL_BASE_R,
                                  COL_BASE_H, 8, (x, y, z_plinth + COL_BASE_H / 2)))
        timber.append(make_taper(f'Column_{i}', COL_R_B, COL_R_T, COL_H, 8,
                                 (x, y, z_plinth + COL_BASE_H + COL_H / 2)))
    # 柱头枋：6 根梁圈在柱顶（面中位、沿边向）
    lintel_d = R_COL * math.cos(math.radians(30))     # 面心距 1.800
    for i, m in enumerate(face_angles):
        cx, cy = math.cos(m) * lintel_d, math.sin(m) * lintel_d
        timber.append(make_box(
            f'Lintel_{i}', (SIDE - 0.30, 0.18, 0.20),
            (cx, cy, z_eave - 0.10), rot=(0, 0, m + math.pi / 2)))

    # ③ 美人靠 6 段（座板 + 微倾靠背；入口面留缺 ⇒ 各 5 段）
    seat_d = lintel_d - 0.12
    for i, m in enumerate(face_angles):
        if math.degrees(m) == ENTER_FACE_DEG:
            continue
        cx, cy = math.cos(m) * seat_d, math.sin(m) * seat_d
        # 座板（板厚 0.05，座面高 0.42 ⇒ 板中心 0.42-0.025）
        timber.append(make_box(
            f'Seat_{i}', (SIDE - 0.36, 0.30, 0.05),
            (cx, cy, z_plinth + SEAT_H - 0.025), rot=(0, 0, m + math.pi / 2)))
        # 靠背：沿边向长条，顶向亭心内倾（绕自身长轴倾 BACK_TILT）
        bx, by = math.cos(m) * (seat_d - 0.13), math.sin(m) * (seat_d - 0.13)
        timber.append(make_box(
            f'Backrest_{i}', (SIDE - 0.36, 0.04, BACK_H),
            (bx, by, z_plinth + SEAT_H + BACK_H / 2 - 0.03),
            rot=(BACK_TILT, 0, m + math.pi / 2)))

    # ④ 檐口：封檐板 6 面（石深，檐下封边）+ 角梁 6 根（木构，脊线）
    fascia_d = ROOF_R_EAVE * math.cos(math.radians(30))
    for i, m in enumerate(face_angles):
        cx, cy = math.cos(m) * fascia_d, math.sin(m) * fascia_d
        stone_b.append(make_box(
            f'Fascia_{i}', (ROOF_R_EAVE - 0.04, 0.14, 0.10),
            (cx, cy, z_eave - 0.05), rot=(0, 0, m + math.pi / 2)))
    for i, a in enumerate(col_angles):
        # 角梁：自檐角（R_COL+HIP_EXT, z_eave-0.15）挑向顶心（0.20, 举高顶-0.10）
        run = R_COL + HIP_EXT - 0.20
        rise = (ROOF_H1 + ROOF_H2) - 0.15 + 0.10
        pitch = math.atan2(rise, run)
        length = math.hypot(run, rise)
        az = a + math.pi                       # 自角指向亭心
        mid_r = (R_COL + HIP_EXT + 0.20) / 2
        cx, cy = math.cos(a) * mid_r, math.sin(a) * mid_r
        cz = z_eave + (ROOF_H1 + ROOF_H2) / 2 - 0.03
        timber.append(make_box(
            f'HipBeam_{i}', (length, 0.09, 0.13), (cx, cy, cz),
            rot=(0, -pitch, az)))

    # ⑤ 两段攒尖屋面（下缓：2.928→1.35 / 上陡：1.35→0.10）
    # ⚠ Blender 6 段锥的顶点在 30°+60k，柱在 0°+60k —— 锥顶须绕 Z 转 −30°
    #   才能让攒尖**脊线（角）**对上柱位与角梁（首版实测 z 向 5.86 = 锥顶点
    #   顶在 90° 即此错位；对齐后 z 向 = 对边 5.24，x 向 = 对角+角梁 6.06）。
    roof.append(make_taper('Roof_Lower', ROOF_R_EAVE, ROOF_R_MID, ROOF_H1, 6,
                           (0, 0, z_eave + ROOF_H1 / 2), rot=(0, 0, -math.pi / 6)))
    roof.append(make_taper('Roof_Upper', ROOF_R_MID, 0.10, ROOF_H2, 6,
                           (0, 0, z_eave + ROOF_H1 + ROOF_H2 / 2), rot=(0, 0, -math.pi / 6)))

    # ⑥ 宝顶：座（石深）+ 球 + 尖（金属）
    z_apex = z_eave + ROOF_H1 + ROOF_H2        # 4.65
    stone_b.append(make_taper('FinialBase', 0.13, 0.11, 0.12, 8,
                              (0, 0, z_apex + 0.06)))
    finial.append(make_sphere('FinialBall', FIN_BALL_R, 10,
                              (0, 0, z_apex + 0.12 + FIN_BALL_R - 0.02)))
    finial.append(make_taper('FinialTip', 0.05, 0.0, FIN_TIP_H, 8,
                             (0, 0, z_apex + 0.12 + 2 * FIN_BALL_R - 0.04 + FIN_TIP_H / 2)))

    # ⑦ 按材质合并 ⇒ 5 primitive
    mat_stone_a = make_material('ParkPavilion_Stone_Mat', CITY_PALETTE['concrete'],
                                rough=0.88, metal=0.0)
    mat_stone_b = make_material('ParkPavilion_StoneDark_Mat', CITY_PALETTE['concrete_dark'],
                                rough=0.88, metal=0.0)
    mat_timber = make_material('ParkPavilion_Timber_Mat', CITY_PALETTE['pavilion_timber'],
                               rough=0.72, metal=0.0)
    mat_roof = make_material('ParkPavilion_Roof_Mat', CITY_PALETTE['roof_tile_slate'],
                             rough=0.62, metal=0.05)
    mat_finial = make_material('ParkPavilion_Finial_Mat', CITY_PALETTE['steel_dark'],
                               rough=0.45, metal=0.7)
    groups = [
        (stone_a, mat_stone_a, 'Pavilion_Stone'),
        (stone_b, mat_stone_b, 'Pavilion_StoneDark'),
        (timber, mat_timber, 'Pavilion_Timber'),
        (roof, mat_roof, 'Pavilion_Roof'),
        (finial, mat_finial, 'Pavilion_Finial'),
    ]
    joined = [join_objects(objs, name) for objs, _m, name in groups]
    for (_objs, mat, _name), obj in zip(groups, joined):
        assign_material(obj, mat)

    # ⚠ 全件米 → 世界单位（×0.1）：本脚本按真实米制书写，仓库 GLB 契约是世界单位
    # （1 世界单位 = 10 m，同 build_palm_tree 的 U=0.1 先例）。
    # ⚠ 顺序关键：join 会保留**首个对象**的 location/scale 变换（其余件烘进顶点），
    # 直接设 scale=0.1 会在带偏移的局部系里缩放（实测 Stone minY 0.099 = 0.11×0.9
    # 即此错）。必须先 transform_apply 把残留变换 flatten 进顶点，再统一 ×0.1；
    # export_glb 的 bake_transforms 最后把 0.1 烘进顶点（节点保持 identity）。
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 贴地钉扎：倾斜件的柱帽边缘会低于 0（秋千 A 腿 / 倾斜板条端头），
    # 按「本对象 bbox 最低点」整体抬升钉扎到 minY=0（位置由导出烘焙）。
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z < 0:
            obj.location.z = -0.1 * min_z

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/park_pavilion.glb')
    export_glb(out_path)
    print(f'✅ park_pavilion.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
