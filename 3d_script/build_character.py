#!/usr/bin/env python3
"""
build_character — 批次 34 参数化人物导出（8 archetype × 共享骨架 / 步态）。

用法（一次一个原型，逐个出图验收 —— CLAUDE.md §27.0-2）：
  blender --background --python build_character.py -- \\
      --archetype char_business /path/to/ClientWeb/src/assets/models/characters/char_business.glb
  blender --background --python build_character.py -- \\
      --archetype=char_elder   .../characters/char_elder.glb

输出（每个 .glb）：
  - Armature（与 build_pedestrian.py 同构 10 骨组 / 14 edit bone：
    root/hips/spine/chest/neck/head + 上下臂 ×2 + 上下腿 ×2）
  - 单 mesh（头 + 躯干 + 4 limb + 鞋 + 原型配件），**无 vertex group / 无 skin**
    （与 build_pedestrian.py 现状一致，本批次不引入蒙皮）
  - walk 动画 clip：24 帧循环（摆臂摆腿反相 ±30°，绕骨骼局部 X）——
    8 个原型共享同一 clip 定义，导出通道数与 pedestrian_walk.glb 一致（42 channels）

═══════════════════════════════════════════════════════════════════════════
结构调研结论（CLAUDE.md §27.0-1 / 03-建模生产线规范.md §3-①）
═══════════════════════════════════════════════════════════════════════════
⚠ 检索通道现状：本环境 WebSearch 空返回、WebFetch 域名被安全策略拦截（2026-09-29 实测），
  故按「WebSearch **或已有资产**」条款走已有资产路径，结构参考取自：
    (1) 既有资产 `build_pedestrian.py` 的人体测量常量体系（FOOT_Z/KNEE_Z/HIP_Z/WAIST_Z/
        CHEST_Z/SHOULDER_Z/NECK_Z/HEAD_Z/HEAD_R/ARM_X/LEG_X/TORSO_D）；
    (2) 设计文档 `lag_docs/虚拟城市/已实现/34-人物模型与人流分布与视觉操控/01-方案设计.md`
        §5.1 显式规格表（逐原型身高 / 肩宽 / 体态 / 发色）；
    (3) 美术解剖 / 男装 / 老年体态的通用常识（无网络时的兜底依据，逐条标注如下）。

【人体比例】基准取 build_pedestrian.py 常量（身高 1.67 m、肩宽 0.55 m、进深 0.35 m）：
  - 头顶 = 身高（0.167），头球心 0.153、头半径 0.014 ⇒ 头高 ≈ 身高/6~7（低模取大头保可读）；
  - 肩峰高 0.145 ≈ 0.87×身高；髋高 0.085 ≈ 0.51×身高；膝高 0.045 ≈ 0.27×身高
    —— 与成人人体测量（肩 ≈ 0.86~0.88H、大转子 ≈ 0.52H、髌高 ≈ 0.28H）同族；
  - 肩宽 0.55 m ≈ 身高的 1/3（含三角肌外缘），与成人肩宽 0.45~0.55 m 一致；
  - 躯干进深 0.35 m 为行走步幅包络（沿用 pedestrian 口径）。
【商务正装】（char_business / char_formal）
  - 西装上衣下摆到大腿上部（长于休闲衬衫）：躯干盒在 WAIST_Z 下再延 0.010~0.014；
  - 肩线平直略宽（肩宽 0.55~0.56）；深藏青 / 炭黑为权威色（CHAR_PALETTE.top_navy / top_black）；
  - 正装领带居胸骨中线，窄竖条（宽 ≈ 0.06×肩宽、长 ≈ 0.25×身高）；领口留颈盒露出（衬衫领暗示）。
【工装】（char_worker）
  - 建筑/制造工装宽松厚实：肩更宽（0.62 m）、四肢截面 +10~15%、鞋帮略高（靴）；
  - 工装蓝 / 灰为常规（top_workblue / pants_grey），发色深。
【老年体态】（char_elder）
  - 老年性驼背（kyphosis）：胸椎后凸增大 → 头前伸（head −Y 偏移）、上背隆起（背侧加小包）；
  - 身高较青年矮 5~10 cm（椎间盘压缩 + 驼背）：1.58 m（= 0.158 世界单位，−5.4% 于基准）；
  - 肩略窄、四肢略细；发色灰白（hair_grey）。
【青少年】（char_student）
  - 高瘦体型：身高 1.75 m（+4.8%）但肩宽收窄到 0.52、四肢截面 −10%；发色深。
【服务制服】（char_service）
  - 亮色上衣（服务红 / 运动绿）+ 深裤；帽（帽顶 = 身高顶点，帽檐横向）或背包暗示 —— 本实现取帽。
【柔和休闲】（char_parent）中等身材 1.65 m、躯干略厚（持家/照料人群更圆润）；发棕。

坐标与尺度规约（批次 19 全目录统一，全量继承）：
  - **Blender 原生 Z-up**：x = 左右，y = 前后（**-Y = 行进方向**），z = 高度（脚底 z = 0）。
    导出后 glTF X = 左右、glTF Y = 身高（脚底 minY = 0）、glTF Z = 前后（+Z = 行进方向）。
  - 世界单位：1 单位 = 10 m；身高 0.167 = 真实 1.67 m。
  - 目标包围盒（世界单位）：X ≈ 0.055（worker 0.062）× Z ≈ 0.035 × Y = 逐原型身高，
    脚底 minY = 0，X/Z 居中；导出节点 identity（export_glb::bake_transforms 兜底）。

材质（前端按名换色 —— 设计文档 §5.3；名字是契约，禁改）：
  PedestrianBody / PedestrianPants / PedestrianHead / PedestrianShoes 四个固定槽
  + PedestrianHair（第 5 槽，承载设计 §5.1 的「发色」通道；4 固定槽的命名与语义不变）。
  ⚠ 不走 weathered_pbr()/apply_pbr()：二者把材质命名为 `obj.name+'_Mat'`，会破坏按名
  换色契约；且 weathered_pbr 的 Noise 节点不经 glTF 导出（03-规范 §3 提示框）。
  颜色一律取本文件 CHAR_PALETTE（人物专用色常量表，一处集中），禁散落 hex。
  织物感：粗糙度 0.75~0.85，金属度 ≤ 0.05。
"""
import bpy
import math
import os
import sys

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (
    reset_scene, set_unit_meters,
    make_box, make_sphere, make_cylinder, make_material, join_objects, export_glb,
)

# ══════════════════════════════════════════════════════════════════════════
# CHAR_PALETTE — 人物专用色常量表（唯一取色点；禁在下方裸写 hex）
# ══════════════════════════════════════════════════════════════════════════
# 取色原则（与 CITY_PALETTE 同族：饱和度压低、同族明度拉开 ≥12%、整城光照下可读）：
#   · 上衣 8 色对应设计 §5.1「财富档 × 域组」8 色板（深蓝/灰/白/卡其/工装蓝/服务红/运动绿/素黑）
#   · 裤 4 色对应「财富档」4 色板；肤 4 档；发 4 档（含老年灰白）
CHAR_PALETTE = {
    # 肤（4 档：亮 / 中 / 小麦 / 深）
    'skin_light':   '#e8c8a8',
    'skin_medium':  '#d8a878',
    'skin_tan':     '#c49068',
    'skin_deep':    '#a07050',
    # 发（4 档：深 / 棕 / 灰 / 白）
    'hair_dark':    '#2a221c',
    'hair_brown':   '#4a3222',
    'hair_grey':    '#b8b4ae',
    'hair_white':   '#d8d4ce',
    # 上衣（8 色板，设计 §5.1）
    'top_navy':     '#2a3a5c',
    'top_grey':     '#6a7078',
    'top_white':    '#d8d4cc',
    'top_khaki':    '#a08a5c',
    'top_workblue': '#3a5a7a',
    'top_service_red': '#b84038',
    'top_sport_green': '#3a6a4a',
    'top_black':    '#2a2c30',
    # 裤（4 档）
    'pants_dark':   '#2a2e36',
    'pants_grey':   '#5a5e66',
    'pants_khaki':  '#8a7654',
    'pants_navy':   '#2a3448',
    # 鞋
    'shoes_dark':   '#1a1a1a',
    'shoes_brown':  '#3a2c22',
}

# 四个契约材质槽的默认粗糙度 / 金属度（织物感 0.75~0.85，金属 ≤ 0.05）
FABRIC = (0.80, 0.0)
SKIN = (0.72, 0.0)

# ══════════════════════════════════════════════════════════════════════════
# 基准人体常量（照抄 build_pedestrian.py；身高 0.167 = 1.67 m，1 单位 = 10 m）
# ══════════════════════════════════════════════════════════════════════════
BASE_H = 0.167
BASE_SHOULDER = 0.055
BASE_FOOT_Z = 0.012        # 鞋面高
BASE_KNEE_Z = 0.045
BASE_HIP_Z = 0.085
BASE_WAIST_Z = 0.090
BASE_CHEST_Z = 0.120
BASE_SHOULDER_Z = 0.145
BASE_NECK_Z = 0.150
BASE_HEAD_R = 0.014
BASE_HEAD_Z = 0.153        # 头顶 = 0.167 = 目标身高
BASE_LEG_X = 0.014
BASE_TORSO_D = 0.035       # 躯干进深 ⇒ 目标 Z
BASE_TORSO_W = 0.040
BASE_ARM_UP_W = 0.014
BASE_ARM_LO_W = 0.012
BASE_LEG_UP_W = 0.026
BASE_LEG_LO_W = 0.022
BASE_SHOE_W = 0.026
BASE_SHOE_D = 0.030
BASE_NECK_W = 0.014


# ══════════════════════════════════════════════════════════════════════════
# 8 个 archetype（设计文档 §5.1 + 任务书规格；身高单位 = 世界单位 = 米/10）
# ══════════════════════════════════════════════════════════════════════════
# height      身高（世界单位）；shoulder 肩宽外缘（X 包围盒）；girth 四肢截面倍数
# hunch       驼背强度 0~1（头前伸 + 上背隆起）；coat 西装/大衣下摆再延（世界单位）
# hair        发型：'short' 短发 / 'bushy' 蓬松（老年） / 'neat' 整齐（商务）
# hat         服务帽；tie 领带；hump 上背隆起（驼背垫）
ARCHETYPES = {
    # 兜底休闲 ≡ pedestrian_walk（1.67 m / 肩 0.55）
    'char_casual': dict(
        height=0.167, shoulder=0.055, girth=1.00, hunch=0.0, coat=0.0,
        hair='short', hat=False, tie=False, hump=False,
        top='top_grey', pants='pants_dark', skin='skin_medium',
        shoes='shoes_dark', hair_c='hair_dark',
    ),
    # 标准商务 1.72 m、西装色（上衣略长）
    'char_business': dict(
        height=0.172, shoulder=0.055, girth=1.00, hunch=0.0, coat=0.010,
        hair='neat', hat=False, tie=True, hump=False,
        top='top_navy', pants='pants_dark', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_dark',
    ),
    # 壮实工装：肩更宽 0.62、四肢厚、靴帮略高、发深
    'char_worker': dict(
        height=0.172, shoulder=0.062, girth=1.15, hunch=0.0, coat=0.0,
        hair='short', hat=False, tie=False, hump=False,
        top='top_workblue', pants='pants_grey', skin='skin_tan',
        shoes='shoes_brown', hair_c='hair_dark',
    ),
    # 银发退休 1.58 m、微驼背、发灰白
    'char_elder': dict(
        height=0.158, shoulder=0.052, girth=0.92, hunch=1.0, coat=0.006,
        torso_d=0.032,
        hair='bushy', hat=False, tie=False, hump=True,
        top='top_grey', pants='pants_grey', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_grey',
    ),
    # 高瘦学生 1.75 m、发深
    'char_student': dict(
        height=0.175, shoulder=0.052, girth=0.88, hunch=0.0, coat=0.0,
        hair='short', hat=False, tie=False, hump=False,
        top='top_sport_green', pants='pants_dark', skin='skin_medium',
        shoes='shoes_dark', hair_c='hair_dark',
    ),
    # 服务制服 1.68 m、亮色上衣 + 帽
    'char_service': dict(
        height=0.168, shoulder=0.055, girth=1.00, hunch=0.0, coat=0.0,
        hair='short', hat=True, tie=False, hump=False,
        top='top_service_red', pants='pants_dark', skin='skin_medium',
        shoes='shoes_dark', hair_c='hair_dark',
    ),
    # 挺拔正装 1.78 m、上衣更长 + 领带
    'char_formal': dict(
        height=0.178, shoulder=0.056, girth=1.00, hunch=0.0, coat=0.014,
        hair='neat', hat=False, tie=True, hump=False,
        top='top_black', pants='pants_dark', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_dark',
    ),
    # 柔和持家 1.65 m、躯干略厚
    'char_parent': dict(
        height=0.165, shoulder=0.055, girth=1.05, hunch=0.0, coat=0.0,
        hair='short', hat=False, tie=False, hump=False,
        top='top_khaki', pants='pants_khaki', skin='skin_medium',
        shoes='shoes_brown', hair_c='hair_brown',
    ),
}


def landmarks(spec: dict) -> dict:
    """按身高/肩宽推导人体关键高度（沿用 build_pedestrian.py 常量体系）。

    竖向 Z 一律按 k = height/0.167 等比；头顶恰 = spec['height']。
    横向由 shoulder（外缘宽）+ girth（四肢截面）决定。
    """
    k = spec['height'] / BASE_H
    g = spec['girth']
    arm_up_w = BASE_ARM_UP_W * g * (spec['shoulder'] / BASE_SHOULDER) ** 0.5
    arm_lo_w = BASE_ARM_LO_W * g * (spec['shoulder'] / BASE_SHOULDER) ** 0.5
    leg_up_w = BASE_LEG_UP_W * g
    leg_lo_w = BASE_LEG_LO_W * g
    head_r = BASE_HEAD_R * k
    # 驼背：头颈整体向 -Y（行进方向 / 前）伸；强度 0~1
    hunch = spec['hunch']
    head_dy = -0.0045 * hunch * k
    neck_dy = -0.0025 * hunch * k
    torso_dy = -0.0012 * hunch * k
    # 躯干进深：默认 0.035；驼背原型略收（倾角会放大 Z 包围盒，见 _build_mesh）
    torso_d = spec.get('torso_d', BASE_TORSO_D)
    return dict(
        k=k,
        FOOT_Z=BASE_FOOT_Z * k,
        KNEE_Z=BASE_KNEE_Z * k,
        HIP_Z=BASE_HIP_Z * k,
        WAIST_Z=BASE_WAIST_Z * k,
        CHEST_Z=BASE_CHEST_Z * k,
        SHOULDER_Z=BASE_SHOULDER_Z * k,
        NECK_Z=BASE_NECK_Z * k,
        HEAD_R=head_r,
        HEAD_Z=spec['height'] - head_r,      # 头顶 = 目标身高
        ARM_X=spec['shoulder'] / 2 - arm_up_w / 2,
        LEG_X=BASE_LEG_X * k * g,
        TORSO_D=torso_d,
        TORSO_W=BASE_TORSO_W * (spec['shoulder'] / BASE_SHOULDER),
        ARM_UP_W=arm_up_w,
        ARM_LO_W=arm_lo_w,
        LEG_UP_W=leg_up_w,
        LEG_LO_W=leg_lo_w,
        NECK_W=BASE_NECK_W * k,
        SHOE_W=BASE_SHOE_W * g,
        SHOE_D=BASE_SHOE_D,
        head_dy=head_dy,
        neck_dy=neck_dy,
        torso_dy=torso_dy,
    )


# ── 骨架（与 build_pedestrian.py 逐骨同构；位置随身高缩放）──────────────────
def _bone(eb, name, head, tail, parent=None):
    b = eb.new(name)
    b.head = head
    b.tail = tail
    if parent is not None:
        b.parent = parent
        b.use_connect = False
    return b


def _build_armature(lm: dict) -> bpy.types.Object:
    """10 骨组 / 14 edit bone（Blender Z-up：骨骼"向下"= 沿 -Z，躯干沿 +Z 生长）。"""
    bpy.ops.object.armature_add(enter_editmode=False, location=(0, 0, 0))
    arm_obj = bpy.context.active_object
    arm_obj.name = 'PedestrianArmature'
    bpy.ops.object.mode_set(mode='EDIT')

    eb = arm_obj.data.edit_bones
    for b in list(eb):
        eb.remove(b)

    root = _bone(eb, 'root', (0, 0, 0), (0, 0, 0.020 * lm['k']))
    hips = _bone(eb, 'hips', (0, 0, lm['HIP_Z']), (0, 0, lm['HIP_Z'] + 0.010 * lm['k']), root)
    spine = _bone(eb, 'spine', (0, 0, lm['WAIST_Z']), (0, 0, lm['CHEST_Z']), hips)
    chest = _bone(eb, 'chest', (0, 0, lm['CHEST_Z']), (0, 0, lm['SHOULDER_Z']), spine)
    neck = _bone(eb, 'neck', (0, 0, lm['SHOULDER_Z']), (0, 0, lm['NECK_Z']), chest)
    _bone(eb, 'head', (0, 0, lm['NECK_Z']), (0, 0, lm['HEAD_Z'] + lm['HEAD_R']), neck)

    arm_x, leg_x = lm['ARM_X'], lm['LEG_X']
    for side, sx in (('L', -1), ('R', 1)):
        ua = _bone(eb, f'upper_arm.{side}',
                   (sx * arm_x, 0, lm['SHOULDER_Z'] - 0.005 * lm['k']),
                   (sx * arm_x, 0, lm['HIP_Z'] + 0.010 * lm['k']), chest)
        _bone(eb, f'lower_arm.{side}',
              (sx * arm_x, 0, lm['HIP_Z'] + 0.010 * lm['k']),
              (sx * arm_x, 0, lm['HIP_Z'] - 0.030 * lm['k']), ua)
        ul = _bone(eb, f'upper_leg.{side}',
                   (sx * leg_x, 0, lm['HIP_Z']),
                   (sx * leg_x, 0, lm['KNEE_Z']), hips)
        _bone(eb, f'lower_leg.{side}',
              (sx * leg_x, 0, lm['KNEE_Z']),
              (sx * leg_x, 0, 0.005 * lm['k']), ul)

    bpy.ops.object.mode_set(mode='OBJECT')
    return arm_obj


# ── 网格（每件先挂材质再 join ⇒ 材质槽按名保留进 glTF）─────────────────────
def _build_mesh(spec: dict, lm: dict, mats: dict):
    """返回 join 后的单 mesh。mats: slot 名 → 材质 datablock（共享，join 时去重）。"""
    parts = []          # (obj, slot_key)

    def add(obj, slot):
        obj.data.materials.append(mats[slot])
        for poly in obj.data.polygons:
            poly.material_index = 0
        parts.append(obj)
        return obj

    hr = lm['HEAD_R']
    hunch = spec['hunch']
    # 头（球心抬到 HEAD_Z ⇒ 头顶 = 目标身高）
    add(make_sphere('HeadMesh', hr, 12, (0, lm['head_dy'], lm['HEAD_Z'])), 'head')
    # 颈（皮肤色，藏于头/躯干之间）
    add(make_box('NeckMesh', (lm['NECK_W'], lm['NECK_W'], 0.014 * lm['k']),
                 (0, lm['neck_dy'], lm['NECK_Z'] - 0.005 * lm['k'])), 'head')

    # 躯干（西装/大衣下摆 coat 再往下延；竖向 WAIST_Z−coat .. SHOULDER_Z）
    z0 = lm['WAIST_Z'] - spec['coat']
    z1 = lm['SHOULDER_Z']
    torso = make_box('BodyMesh', (lm['TORSO_W'], lm['TORSO_D'], z1 - z0),
                     (0, lm['torso_dy'], (z0 + z1) / 2))
    if hunch > 0:
        # 驼背：胸段向前（-Y）倾约 2.5°（hunch=1）。倾角绕盒心旋转会放大 Z 包围盒
        # （半高 × sin θ 叠到进深上），故角度保守 + torso_d 已收 0.032（Z 预算 0.035）。
        torso.rotation_euler = (math.radians(2.5 * hunch), 0, 0)
    add(torso, 'body')
    # 上背隆起（驼背垫）—— 背侧（+Y）小包；背面 ≈ 躯干背面 +1 mm 微凸（Z 预算内可见侧影）
    if spec['hump']:
        add(make_box('HumpMesh', (lm['TORSO_W'] * 0.72, 0.009 * lm['k'], 0.022 * lm['k']),
                     (0, lm['torso_dy'] + lm['TORSO_D'] * 0.36,
                      lm['CHEST_Z'] - 0.004 * lm['k'])), 'body')

    # 领带（正装暗示）：胸骨中线窄竖条，深色（裤料同色 = formal 深色领带）。
    # 微凸 0.5~1 mm 避免 z-fighting；再深就会撑破 Z=0.035 包围盒预算。
    if spec['tie']:
        add(make_box('TieMesh', (0.0072 * lm['k'], 0.0012 * lm['k'], 0.036 * lm['k']),
                     (0, lm['torso_dy'] - lm['TORSO_D'] / 2 - 0.0002,
                      lm['CHEST_Z'] - 0.006 * lm['k'])), 'pants')

    # 上下臂 ×2（身体色）
    for side, sx in (('L', -1), ('R', 1)):
        add(make_box(f'UpperArm{side}Mesh',
                     (lm['ARM_UP_W'], lm['ARM_UP_W'], 0.045 * lm['k']),
                     (sx * lm['ARM_X'], lm['torso_dy'], 0.1175 * lm['k'])), 'body')
        add(make_box(f'LowerArm{side}Mesh',
                     (lm['ARM_LO_W'], lm['ARM_LO_W'], 0.040 * lm['k']),
                     (sx * lm['ARM_X'], lm['torso_dy'], 0.075 * lm['k'])), 'body')
    # 上下腿 ×2（裤色）
    for side, sx in (('L', -1), ('R', 1)):
        add(make_box(f'UpperLeg{side}Mesh',
                     (lm['LEG_UP_W'], lm['LEG_UP_W'], 0.040 * lm['k']),
                     (sx * lm['LEG_X'], 0, 0.065 * lm['k'])), 'pants')
        add(make_box(f'LowerLeg{side}Mesh',
                     (lm['LEG_LO_W'], lm['LEG_LO_W'], 0.035 * lm['k']),
                     (sx * lm['LEG_X'], 0, 0.0275 * lm['k'])), 'pants')
        # 鞋（脚尖朝 -Y = 行进方向）
        add(make_box(f'Shoe{side}',
                     (lm['SHOE_W'], lm['SHOE_D'], lm['FOOT_Z']),
                     (sx * lm['LEG_X'], -0.0025, lm['FOOT_Z'] / 2)), 'shoes')

    # 发（贴头壳的压扁球 = "碗盖"式发帽）：XY 与头同心、上移压扁 ⇒
    # 只在头顶/侧上部露出一圈发际（≈ 眉线以上），正脸/下颌全露皮肤。
    # 不做向后偏置的整壳（会成"歪贝雷帽"剪影）。发后缘 = (dy + r)×hr ≤ 1.30hr
    # ⇒ 落在躯干背面（0.0175）以内，不撑破 Z=0.035 预算。
    if spec['hair'] == 'bushy':
        hair_r, hair_sz, hair_dy, hair_dz = 1.20, 0.48, 0.06, 0.42
    elif spec['hair'] == 'neat':
        hair_r, hair_sz, hair_dy, hair_dz = 1.06, 0.52, 0.04, 0.45
    else:   # short
        hair_r, hair_sz, hair_dy, hair_dz = 1.08, 0.52, 0.04, 0.44
    hair = make_sphere('HairMesh', hr * hair_r, 12,
                       (0, lm['head_dy'] + hr * hair_dy, lm['HEAD_Z'] + hr * hair_dz))
    hair.scale = (1.0, 1.0, hair_sz)
    add(hair, 'hair')

    # 服务帽（帽顶 = 身高顶点；帽檐横向，y 向收窄保 Z 预算）
    if spec['hat']:
        hat_r = hr * 1.12
        hat_h = hr * 0.86
        hat_cz = lm['HEAD_Z'] + hr * 0.57       # 顶 = HEAD_Z + hr*1.00 = 身高
        add(make_cylinder('HatCrownMesh', hat_r, hat_r, hat_h, 12,
                          (0, lm['head_dy'], hat_cz)), 'hair')
        add(make_box('HatBrimMesh', (hr * 2.55, hr * 1.80, hr * 0.11),
                     (0, lm['head_dy'] - hr * 0.22, hat_cz - hat_h / 2 + hr * 0.05)),
            'hair')

    joined = join_objects(parts, 'CharacterMesh')
    joined.data.name = 'CharacterMesh'   # join 会残留首件数据名（如 'Sphere'）
    return joined


def build_character(archetype: str) -> bpy.types.Object:
    if archetype not in ARCHETYPES:
        raise SystemExit('unknown archetype %r（可选：%s）'
                         % (archetype, ', '.join(sorted(ARCHETYPES))))
    spec = ARCHETYPES[archetype]
    lm = landmarks(spec)

    reset_scene()
    set_unit_meters()

    arm_obj = _build_armature(lm)

    # 材质槽（名字 = 前端按名换色契约；5 槽 = 4 固定 + PedestrianHair）
    mats = {
        'body':  make_material('PedestrianBody', CHAR_PALETTE[spec['top']], *FABRIC),
        'pants': make_material('PedestrianPants', CHAR_PALETTE[spec['pants']], *FABRIC),
        'head':  make_material('PedestrianHead', CHAR_PALETTE[spec['skin']], *SKIN),
        'shoes': make_material('PedestrianShoes', CHAR_PALETTE[spec['shoes']], *FABRIC),
        'hair':  make_material('PedestrianHair', CHAR_PALETTE[spec['hair_c']], *FABRIC),
    }
    body_joined = _build_mesh(spec, lm, mats)

    # Armature modifier（无 vertex group ⇒ 导出不含 skin；与 pedestrian 一致）
    body_joined.parent = arm_obj
    mod = body_joined.modifiers.new(name='Armature', type='ARMATURE')
    mod.object = arm_obj

    # walk clip（24 帧循环，摆臂摆腿反相 ±30°，绕骨骼局部 X）—— 8 原型共用定义
    scene = bpy.context.scene
    scene.frame_start = 1
    scene.frame_end = 24
    bpy.context.view_layer.objects.active = arm_obj
    bpy.ops.object.mode_set(mode='POSE')
    pbones = arm_obj.pose.bones

    def key(bone_name, axis, frames_degrees):
        pb = pbones[bone_name]
        pb.rotation_mode = 'XYZ'
        for f, deg in frames_degrees:
            scene.frame_set(f)
            pb.rotation_euler[axis] = math.radians(deg)
            pb.keyframe_insert(data_path='rotation_euler', index=axis, frame=f)

    key('upper_leg.L', 0, [(1, -30), (13, 30), (24, -30)])
    key('upper_leg.R', 0, [(1, 30), (13, -30), (24, 30)])
    key('upper_arm.L', 0, [(1, 30), (13, -30), (24, 30)])
    key('upper_arm.R', 0, [(1, -30), (13, 30), (24, -30)])

    bpy.ops.object.mode_set(mode='OBJECT')
    scene.frame_set(1)
    return arm_obj


def _parse_cli(argv):
    """`-- --archetype <key> <out.glb>` 或 `-- --archetype=<key> <out.glb>`。"""
    archetype, out_path = 'char_casual', None
    i = 0
    while i < len(argv):
        a = argv[i]
        if a == '--archetype' and i + 1 < len(argv):
            archetype, i = argv[i + 1], i + 2
        elif a.startswith('--archetype='):
            archetype, i = a.split('=', 1)[1], i + 1
        elif a in ('-h', '--help'):
            print(__doc__)
            raise SystemExit(0)
        else:
            out_path, i = a, i + 1
    return archetype, out_path


if __name__ == '__main__':
    raw = sys.argv[sys.argv.index('--') + 1:] if '--' in sys.argv else []
    archetype, out_path = _parse_cli(raw)
    if not out_path:
        raise SystemExit('缺输出路径：… build_character.py -- --archetype <key> <out.glb>')
    build_character(archetype)
    # 烘焙 mesh 的 object transform（join 残留首件 location/scale；含驼背倾角）
    # ⇒ 导出节点 identity；armature 本身建在原点，保持 identity。
    for ob in bpy.context.scene.objects:
        if ob.type != 'MESH':
            continue
        bpy.ops.object.select_all(action='DESELECT')
        ob.select_set(True)
        bpy.context.view_layer.objects.active = ob
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    export_glb(out_path)
    size = os.path.getsize(out_path)
    print(f'[size] {size} bytes ({archetype})', flush=True)
