#!/usr/bin/env python3
"""
build_character — 批次 34 参数化人物导出（8 archetype × 共享骨架 / 步态）
                + 批次 36 性别 × 年龄 15 变体（--variant 参数化路径）。

用法（一次一个原型/变体，逐个出图验收 —— CLAUDE.md §27.0-2）：
  # 批次 34：8 archetype（行为与输出零改动）
  blender --background --python build_character.py -- \\
      --archetype char_business /path/to/ClientWeb/src/assets/models/characters/char_business.glb
  blender --background --python build_character.py -- \\
      --archetype=char_elder   .../characters/char_elder.glb
  # 批次 36：15 性别×年龄变体（gender/age 参数驱动 landmarks + 躯干/发型构建器）
  blender --background --python build_character.py -- \\
      --variant char_m_youth .../characters/char_m_youth.glb

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


# ══════════════════════════════════════════════════════════════════════════
# 批次 36 · 性别 × 年龄 15 变体（--variant 路径）
# ══════════════════════════════════════════════════════════════════════════
# 设计原则（设计 36 §4.1）：几何 = 谁（性别/年龄），颜色 = 干什么（职业/财富）。
# 15 个变体由 GENDER_PLAN × AGE_PLAN 两张参数表 + VARIANTS 逐件规格表驱动，
# landmarks() 与躯干/发型构建器全部走参数 —— 禁止 15 份硬编码几何复制粘贴。
#
# ── 性别体态计划（设计 36 §4.3-5）────────────────────────────────────────
#   hip_ratio  髋宽 / 肩宽躯干比：m 肩>髋（倒梯）、f 髋≥肩（梯形）、u 直筒
#   chest      胸线凸起强度 0~1（f 轻微胸线盒；m/u 无）
#   shoulder_q 肩部饱满度（三角肌球半径倍数；m 略宽厚）
GENDER_PLAN = {
    'm': dict(hip_ratio=0.90, chest=0.0, shoulder_q=1.10),
    'f': dict(hip_ratio=1.10, chest=0.85, shoulder_q=0.92),
    'u': dict(hip_ratio=1.00, chest=0.0, shoulder_q=1.00),
}

# ── 年龄体态计划（设计 36 §4.3-6；R2 返工加强驼背）───────────────────────
#   girth       四肢截面倍数（youth 略细长 / elder 瘦削）
#   torso_thick 躯干进深倍数（middle 微发福 ×1.06；senior/elder 随前倾角加大而
#               递减，补偿旋转放大的 Y 包络，保 Z 判据）
#   hunch       驼背强度 0~1（head/neck/torso 前伸，landmarks() 消费）
#   torso_rot   躯干前倾角（度）：middle 1.5° / senior ~6° / elder ~12°
#               （R2 评审：4°/6° 与 youth 差异肉眼勉强可辨，已加强并 senior 补 hump；
#                终审发现 7°/10° 下 elder 与 senior 反序，再拉开为 6°/12° + hunch 差）
#   hump/cane   上背隆起垫 / 拐杖道具（senior 起圆化上背；elder 再加拐杖）
AGE_PLAN = {
    'youth':  dict(girth=0.90, torso_thick=0.98, hunch=0.00, torso_rot=0.0,
                   hump=False, cane=False),
    'young':  dict(girth=1.00, torso_thick=1.00, hunch=0.00, torso_rot=0.0,
                   hump=False, cane=False),
    'middle': dict(girth=1.02, torso_thick=1.06, hunch=0.18, torso_rot=1.5,
                   hump=False, cane=False),
    'senior': dict(girth=0.98, torso_thick=0.92, hunch=0.60, torso_rot=6.0,
                   hump=True,  cane=False),
    'elder':  dict(girth=0.88, torso_thick=0.84, hunch=1.25, torso_rot=12.0,
                   hump=True,  cane=True),
}

# ── 发型几何计划（设计 36 §4.3-2：发帽 + 刘海/鬓角/后发片分层）─────────────
#   cap_r/cap_sz/cap_dy/cap_dz  发帽（贴头壳压扁球）。
#     ⚠ cap_dz + cap_r*cap_sz ≥ 1.06（头球顶）否则头顶穿出发帽成「头皮洞」——
#       批次 36 首件打样即踩此坑，已按不等式校准（bald 刻意 < 1.06 露头皮）。
#   extra_top  发型在头心以上可达的最大高度（hr 倍数；含顶部碎发）——
#     variant_landmarks 以 max(头顶, cap 顶, extra_top) 对齐设计身高，
#     故蓬乱发型「加高」不撑破 Y 包络（头顶相应下移）。
#   top_tufts  顶部碎发小块（messy 专用，剪影「蓬乱加高」）。
#   bangs      刘海（前额横片）；sideburns 1=鬓角小片 2=齐耳侧发片 0=无（稀薄/后移）
#   back       后发：None / 'tuft'(短碎) / 'mid'(中长) / 'long'(后背长发片+颈侧发绺)
#              / 'ponytail'(后发片+粗马尾柱) / 'bun'(后上发髻球)
#   bald       秃顶感：发帽缩小抬高 + 侧发保留（cap 顶刻意低于头顶 ⇒ 露头皮）
# 批次 36 R1/R3 返工口径：长发/马尾须剪影级可辨（后垂到背中段、马尾柱粗 ≥ 脖颈）；
#   m 系四段差异化：messy(蓬乱加高) / neat(利落贴服) / receded(发际后移) / thin(稀薄贴服)。
HAIR_PLAN = {
    'messy':    dict(cap_r=1.22, cap_sz=0.62, cap_dy=0.06, cap_dz=0.55, extra_top=1.40,
                     bangs=True, sideburns=1, back='tuft', bald=False, top_tufts=True),
    'neat':     dict(cap_r=1.10, cap_sz=0.74, cap_dy=0.04, cap_dz=0.30, extra_top=0.0,
                     bangs=True, sideburns=1, back=None, bald=False, top_tufts=False),
    'short':    dict(cap_r=1.14, cap_sz=0.70, cap_dy=0.04, cap_dz=0.35, extra_top=0.0,
                     bangs=True, sideburns=1, back=None, bald=False, top_tufts=False),
    # 发际后移：无刘海无鬓角 + 发帽后移（cap_dy+）+ 两侧露出颞部
    'receded':  dict(cap_r=1.08, cap_sz=0.62, cap_dy=0.10, cap_dz=0.44, extra_top=0.0,
                     bangs=False, sideburns=0, back=None, bald=False, top_tufts=False),
    # 稀薄贴服：小半径薄壳、无刘海无鬓角（覆盖面显著小于 short）
    'thin':     dict(cap_r=1.07, cap_sz=0.70, cap_dy=0.08, cap_dz=0.38, extra_top=0.0,
                     bangs=False, sideburns=0, back=None, bald=False, top_tufts=False),
    'bald':     dict(cap_r=0.98, cap_sz=0.45, cap_dy=0.12, cap_dz=0.52, extra_top=0.0,
                     bangs=False, sideburns=2, back=None, bald=True, top_tufts=False),
    'ponytail': dict(cap_r=1.14, cap_sz=0.70, cap_dy=0.04, cap_dz=0.35, extra_top=0.0,
                     bangs=True, sideburns=1, back='ponytail', bald=False, top_tufts=False),
    # 长发几何参数（终审判读：后垂须与 mid 拉开层次，到肩胛下）——back_* 为后背
    # 大发片（宽/厚/长 hr 倍数 + 中心 z/hr、中心 y 偏移），lock_* 为颈侧发绺
    # （|x| 中心/宽/长 hr 倍数 + 中心 z）。lock 外缘 1.625hr < 腿外缘 ⇒ X 包络不受影响。
    'long':     dict(cap_r=1.14, cap_sz=0.70, cap_dy=0.04, cap_dz=0.35, extra_top=0.0,
                     bangs=True, sideburns=1, back='long', bald=False, top_tufts=False,
                     back_w=2.30, back_d=0.50, back_len=4.10, back_cz=-1.70, back_dy=1.10,
                     lock_x=1.30, lock_w=0.65, lock_len=3.40, lock_cz=-1.40),
    'mid':      dict(cap_r=1.14, cap_sz=0.70, cap_dy=0.04, cap_dz=0.35, extra_top=0.0,
                     bangs=True, sideburns=1, back='mid', bald=False, top_tufts=False),
    'bun':      dict(cap_r=1.10, cap_sz=0.70, cap_dy=0.05, cap_dz=0.36, extra_top=0.0,
                     bangs=True, sideburns=1, back='bun', bald=False, top_tufts=False),
    'ear_len':  dict(cap_r=1.14, cap_sz=0.68, cap_dy=0.04, cap_dz=0.36, extra_top=0.0,
                     bangs=True, sideburns=2, back='mid', bald=False, top_tufts=False),
}

# ── 15 变体逐件规格（设计 36 §4.2 规格表；身高/肩宽单位 = 世界单位 = 米/10）──
# height / shoulder  —— 规格表设计值（m ÷ 10）
# hair               —— HAIR_PLAN 键；collar —— hoodie/shirt/jacket/plain
# belt / hem         —— 腰带（Pants 色细环）/ 上衣下摆加长（中长款）
# z                  —— 进深 target 备忘（verify TARGETS 用；长发/马尾可 0.038~0.042）
VARIANTS = {
    'char_m_youth': dict(
        gender='m', age='youth', height=0.172, shoulder=0.052,
        hair='messy', collar='hoodie', belt=False, hem=0.0,
        top='top_sport_green', pants='pants_dark', skin='skin_medium',
        shoes='shoes_dark', hair_c='hair_dark', z=0.038),
    'char_m_young': dict(
        gender='m', age='young', height=0.176, shoulder=0.055,
        hair='neat', collar='shirt', belt=True, hem=0.0,
        top='top_navy', pants='pants_navy', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_dark', z=0.035),
    'char_m_middle': dict(
        gender='m', age='middle', height=0.175, shoulder=0.056,
        hair='receded', collar='shirt', belt=True, hem=0.0,
        top='top_khaki', pants='pants_khaki', skin='skin_medium',
        shoes='shoes_brown', hair_c='hair_dark', z=0.037),
    'char_m_senior': dict(
        gender='m', age='senior', height=0.171, shoulder=0.054,
        hair='thin', collar='jacket', belt=True, hem=0.008,
        top='top_grey', pants='pants_grey', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_dark', z=0.038),
    'char_m_elder': dict(
        gender='m', age='elder', height=0.165, shoulder=0.050,
        hair='bald', collar='jacket', belt=False, hem=0.006,
        top='top_grey', pants='pants_grey', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_grey', z=0.040),
    'char_f_youth': dict(
        gender='f', age='youth', height=0.161, shoulder=0.046,
        hair='ponytail', collar='plain', belt=False, hem=0.012,
        top='top_white', pants='pants_grey', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_brown', z=0.040),
    'char_f_young': dict(
        gender='f', age='young', height=0.165, shoulder=0.046,
        hair='long', collar='plain', belt=True, hem=0.008,
        top='top_service_red', pants='pants_dark', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_dark', z=0.037),
    'char_f_middle': dict(
        gender='f', age='middle', height=0.164, shoulder=0.048,
        hair='mid', collar='plain', belt=False, hem=0.014,
        top='top_khaki', pants='pants_khaki', skin='skin_medium',
        shoes='shoes_brown', hair_c='hair_brown', z=0.038),
    'char_f_senior': dict(
        gender='f', age='senior', height=0.160, shoulder=0.047,
        hair='bun', collar='plain', belt=False, hem=0.010,
        top='top_navy', pants='pants_grey', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_grey', z=0.038),
    'char_f_elder': dict(
        gender='f', age='elder', height=0.154, shoulder=0.045,
        hair='bun', collar='jacket', belt=False, hem=0.008,
        top='top_grey', pants='pants_grey', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_white', z=0.040),
    # u = 未知/其它：身高/肩宽取对应男女均值，发型中性（短发/齐耳）
    'char_u_youth': dict(
        gender='u', age='youth', height=0.1665, shoulder=0.049,
        hair='ear_len', collar='hoodie', belt=False, hem=0.0,
        top='top_sport_green', pants='pants_grey', skin='skin_medium',
        shoes='shoes_dark', hair_c='hair_dark', z=0.036),
    'char_u_young': dict(
        gender='u', age='young', height=0.1705, shoulder=0.0505,
        hair='ear_len', collar='shirt', belt=True, hem=0.0,
        top='top_navy', pants='pants_dark', skin='skin_tan',
        shoes='shoes_dark', hair_c='hair_dark', z=0.036),
    'char_u_middle': dict(
        gender='u', age='middle', height=0.1695, shoulder=0.052,
        hair='short', collar='shirt', belt=True, hem=0.004,
        top='top_workblue', pants='pants_grey', skin='skin_medium',
        shoes='shoes_brown', hair_c='hair_brown', z=0.037),
    'char_u_senior': dict(
        gender='u', age='senior', height=0.1655, shoulder=0.0505,
        hair='short', collar='jacket', belt=True, hem=0.006,
        top='top_grey', pants='pants_dark', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_dark', z=0.038),
    'char_u_elder': dict(
        gender='u', age='elder', height=0.1595, shoulder=0.0475,
        hair='short', collar='jacket', belt=False, hem=0.006,
        top='top_grey', pants='pants_grey', skin='skin_light',
        shoes='shoes_dark', hair_c='hair_grey', z=0.040),
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


def variant_landmarks(spec: dict) -> dict:
    """批次 36：在 landmarks() 之上叠加性别/年龄体态参数（archetype 路径零改动）。

    新增键：
      HIP_W        髋宽 = TORSO_W × hip_ratio（m 倒梯 / f 梯形 / u 直筒）
      TORSO_ROT    躯干前倾角（弧度，elder/senior 4~8°）
      CHEST        胸线强度 0~1（f）
      SHOULDER_Q   肩部饱满度（三角肌球倍数）
      LIMB_W_MUL   四肢截面附加倍数（由 girth 已进 landmarks，此处保留扩展点）
    """
    lm = landmarks(spec)
    # 头稍大（低模五官可读性，设计 36 §4.3-1）
    hr = lm['HEAD_R'] * 1.08
    lm['HEAD_R'] = hr
    # 头顶（含发型）恰 = spec['height'] —— 发帽顶 = HEAD_Z + hr*(cap_dz+cap_r*cap_sz)，
    # 若只按「头顶=身高」摆，发型会再冒 ~1.3%（打样实测全表 Y 偏高 1.3~1.8%）。
    # extra_top（顶部碎发等超出发帽的部分）一并计入 ⇒ 蓬乱「加高」不撑破 Y 包络。
    hp = HAIR_PLAN[spec['hair']]
    hair_top = max(hp['cap_dz'] + hp['cap_r'] * hp['cap_sz'],
                   float(hp.get('extra_top', 0.0)))
    top_rel = max(hr * 1.06, hr * hair_top)
    lm['HEAD_Z'] = spec['height'] - top_rel
    # 腿围随肩宽收放：窄肩（f/u）变体上腿外缘 = LEG_X + LEG_UP_W/2 不得超过 shoulder/2，
    # 否则 AABB X 被腿间距主导（f_young 实测 0.0537 vs 肩宽 0.046，+17%）。
    sq = (spec['shoulder'] / BASE_SHOULDER) ** 0.5
    lm['LEG_X'] *= sq
    leg_w = min(1.0, sq ** 1.2)
    lm['LEG_UP_W'] *= leg_w
    lm['LEG_LO_W'] *= leg_w
    lm['SHOE_W'] *= leg_w
    lm['HIP_W'] = lm['TORSO_W'] * float(spec.get('hip_ratio', 1.0))
    lm['TORSO_ROT'] = math.radians(float(spec.get('torso_rot', 0.0)))
    lm['CHEST'] = float(spec.get('chest', 0.0))
    lm['SHOULDER_Q'] = float(spec.get('shoulder_q', 1.0))
    return lm


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


# ── 批次 36 变体网格构建器（gender/age 参数驱动；五官/发型/服饰/手/拐杖）────
def _build_mesh_variant(spec: dict, lm: dict, mats: dict):
    """性别 × 年龄 参数化人物网格。返回 join 后单 mesh（5 材质槽契约不变）。

    细致清单（设计 36 §4.3，每件必达）：
      1 五官：鼻（楔形凸起）/ 耳（双侧小球）/ 眉弓（浅盒）/ 下颌（头非正球）
      2 发型分层：发帽 + 刘海/鬓角/后发片（长发后背片 / 马尾下垂柱 / 发髻后上球 / 秃顶侧发）
      3 服饰：衣领（双侧斜盒）/ 门襟线 / 腰带（Pants 色细环）/ 上衣下摆
      4 手：球掌 + 拇指小球
      5 性别体态：m 倒梯（髋<肩）/ f 梯形+胸线 / u 直筒
      6 年龄体态：elder/senior 驼背前倾 + elder 拐杖（贴身不超出肩宽 X 包络）
    """
    parts = []

    def add(obj, slot):
        obj.data.materials.append(mats[slot])
        for poly in obj.data.polygons:
            poly.material_index = 0
        parts.append(obj)
        return obj

    hr = lm['HEAD_R']
    k = lm['k']
    hunch = spec.get('hunch', 0.0)
    hy, ny, ty = lm['head_dy'], lm['neck_dy'], lm['torso_dy']

    # ── 头（略非正球：Y 向略收 = 下颌收、Z 向略长）────────────────────────
    head = make_sphere('HeadMesh', hr, 12, (0, hy, lm['HEAD_Z']))
    head.scale = (1.0, 0.92, 1.06)
    add(head, 'head')
    # 下颌盒：头前下部补方，侧影不像纯球
    add(make_box('JawMesh', (hr * 1.15, hr * 0.86, hr * 0.52),
                 (0, hy - hr * 0.28, lm['HEAD_Z'] - hr * 0.62)), 'head')
    # 颈
    add(make_box('NeckMesh', (lm['NECK_W'], lm['NECK_W'], 0.014 * k),
                 (0, ny, lm['NECK_Z'] - 0.005 * k)), 'head')

    # ── 五官（设计 36 §4.3-1）──────────────────────────────────────────────
    # 鼻：楔形凸起（两段递减小盒，朝行进方向 -Y）
    add(make_box('NoseBase', (hr * 0.30, hr * 0.34, hr * 0.34),
                 (0, hy - hr * 0.88, lm['HEAD_Z'] - hr * 0.10)), 'head')
    add(make_box('NoseTip', (hr * 0.22, hr * 0.22, hr * 0.20),
                 (0, hy - hr * 1.02, lm['HEAD_Z'] - hr * 0.22)), 'head')
    # 耳：双侧小球（略压扁）
    for sx in (-1, 1):
        ear = make_sphere('EarMesh', hr * 0.28, 8,
                          (sx * hr * 0.96, hy + hr * 0.06, lm['HEAD_Z'] - hr * 0.12))
        ear.scale = (0.55, 1.0, 1.15)
        add(ear, 'head')
    # 眉弓：浅盒（额下）
    add(make_box('BrowMesh', (hr * 1.28, hr * 0.22, hr * 0.13),
                 (0, hy - hr * 0.82, lm['HEAD_Z'] + hr * 0.28)), 'head')

    # ── 发型分层（设计 36 §4.3-2）─────────────────────────────────────────
    hp = HAIR_PLAN[spec['hair']]
    cap = make_sphere('HairCapMesh', hr * hp['cap_r'], 12,
                      (0, hy + hr * hp['cap_dy'], lm['HEAD_Z'] + hr * hp['cap_dz']))
    cap.scale = (1.0, 1.0, hp['cap_sz'])
    add(cap, 'hair')
    if hp.get('top_tufts'):
        # 顶部碎发（R3「蓬乱加高」）：3 块错位小盒，顶 ≤ extra_top 预算
        for i, (tx, tyf, tz, s) in enumerate(((-0.50, 0.05, 1.12, 0.42),
                                              (0.42, -0.10, 1.16, 0.36),
                                              (0.05, 0.28, 1.18, 0.30))):
            add(make_box(f'TopTuft{i}', (hr * s * 0.85, hr * s * 0.85, hr * s),
                         (hr * tx, hy + hr * tyf, lm['HEAD_Z'] + hr * tz)), 'hair')
    if hp['bangs']:
        # 刘海：前额横片（贴发帽前缘）
        add(make_box('BangsMesh', (hr * 1.55, hr * 0.24, hr * 0.30),
                     (0, hy - hr * 0.82, lm['HEAD_Z'] + hr * 0.52)), 'hair')
    if hp['sideburns'] >= 1:
        for sx in (-1, 1):
            add(make_box(f'Sideburn{sx}', (hr * 0.16, hr * 0.30, hr * 0.38),
                         (sx * hr * 0.92, hy - hr * 0.18, lm['HEAD_Z'] + hr * 0.10)),
                'hair')
    if hp['sideburns'] >= 2:
        # 齐耳侧发片（中性/秃顶保留侧发）
        for sx in (-1, 1):
            add(make_box(f'SideHair{sx}', (hr * 0.22, hr * 0.55, hr * 0.85),
                         (sx * hr * 0.88, hy + hr * 0.18, lm['HEAD_Z'] - hr * 0.05)),
                'hair')
    back = hp['back']
    if back == 'tuft':
        # 短碎后发：3 个不齐的小块（「略乱」）
        for i, (dy, dz, s) in enumerate(((0.86, 0.30, 0.34), (0.78, 0.02, 0.30),
                                         (0.92, -0.18, 0.26))):
            add(make_box(f'Tuft{i}', (hr * 0.62, hr * 0.30, hr * s),
                         (0, hy + hr * dy, lm['HEAD_Z'] + hr * dz)), 'hair')
    elif back == 'mid':
        # 中长后发片：垂到颈/肩
        add(make_box('BackHairMid', (hr * 1.55, hr * 0.34, hr * 1.30),
                     (0, hy + hr * 0.88, lm['HEAD_Z'] - hr * 0.42)), 'hair')
    elif back == 'long':
        # 长发（R1 剪影级可辨）：后背大发片 + 颈侧两缕发绺，几何尺寸全部来自
        # HAIR_PLAN['long'] 的 back_*/lock_* 参数（终审加长加厚到肩胛下）。
        add(make_box('BackHairLong',
                     (hr * hp.get('back_w', 1.85), hr * hp.get('back_d', 0.42),
                      hr * hp.get('back_len', 3.00)),
                     (0, hy + hr * hp.get('back_dy', 1.05),
                      lm['HEAD_Z'] + hr * hp.get('back_cz', -1.30))), 'hair')
        for sx in (-1, 1):
            add(make_box(f'SideLock{sx}',
                         (hr * hp.get('lock_w', 0.55), hr * 0.85,
                          hr * hp.get('lock_len', 2.30)),
                         (sx * hr * hp.get('lock_x', 1.12), hy + hr * 0.18,
                          lm['HEAD_Z'] + hr * hp.get('lock_cz', -1.05))), 'hair')
    elif back == 'ponytail':
        # 马尾（R1 剪影级可辨）：后发片 + 粗马尾柱 —— 柱径 ≥ 脖颈
        # （NECK_W ≈ 0.926hr；柱径 1.00~1.12hr），后凸出背 ~0.36hr 保侧影
        add(make_box('BackHairPony', (hr * 1.45, hr * 0.32, hr * 1.30),
                     (0, hy + hr * 0.90, lm['HEAD_Z'] - hr * 0.35)), 'hair')
        add(make_cylinder('PonyMesh', hr * 0.50, hr * 0.56, hr * 2.40, 8,
                          (0, hy + hr * 1.05, lm['HEAD_Z'] - hr * 1.15)), 'hair')
    elif back == 'bun':
        # 发髻：后上球
        bun = make_sphere('BunMesh', hr * 0.58, 8,
                          (0, hy + hr * 0.95, lm['HEAD_Z'] + hr * 0.62))
        bun.scale = (0.92, 0.88, 1.0)
        add(bun, 'hair')
    if hp['bald']:
        # 秃顶感：发帽已缩小抬高；再补一圈侧发带（头顶全露）
        for sx in (-1, 1):
            add(make_box(f'BaldSide{sx}', (hr * 0.26, hr * 0.70, hr * 0.55),
                         (sx * hr * 0.82, hy + hr * 0.30, lm['HEAD_Z'] + hr * 0.05)),
                'hair')

    # ── 躯干（性别体态：m 倒梯 / f 梯形 / u 直筒；年龄：前倾 + 厚度）───────
    z0 = lm['WAIST_Z'] - spec.get('coat', 0.0)
    z1 = lm['SHOULDER_Z']
    torso = make_box('BodyMesh', (lm['TORSO_W'], lm['TORSO_D'], z1 - z0),
                     (0, ty, (z0 + z1) / 2))
    if lm['TORSO_ROT'] != 0.0 or hunch > 0:
        torso.rotation_euler = (lm['TORSO_ROT'] or math.radians(2.5 * hunch), 0, 0)
    add(torso, 'body')
    # R2 驼背：肩线随躯干前倾而前移 —— 手臂/手/三角肌/拐杖挂 arm_y（前倾肩正下方），
    # 不随倾的手臂会悬在倾后肩后侧（10° 时错位 ≈ 0.005）。
    arm_y = ty - (z1 - (z0 + z1) / 2) * math.sin(lm['TORSO_ROT'])
    # 髋/骨盆（裤色，宽度体现性别）：m 髋<肩、f 髋≥肩、u 相当
    add(make_box('PelvisMesh', (lm['HIP_W'], lm['TORSO_D'] * 0.96, 0.024 * k),
                 (0, ty, lm['HIP_Z'] + 0.006 * k)), 'pants')
    # 三角肌（肩饱满度）—— 半径/位置收在肩宽 X 包络内（|x|+r ≤ shoulder/2）
    for sx in (-1, 1):
        delt_r = lm['ARM_UP_W'] * 0.50 * lm['SHOULDER_Q']
        delt = make_sphere('DeltMesh', delt_r, 8,
                           (sx * (lm['ARM_X'] - lm['ARM_UP_W'] * 0.08),
                            arm_y, lm['SHOULDER_Z'] - 0.006 * k))
        add(delt, 'body')
    if spec.get('hump'):
        add(make_box('HumpMesh', (lm['TORSO_W'] * 0.72, 0.009 * k, 0.022 * k),
                     (0, ty + lm['TORSO_D'] * 0.36, lm['CHEST_Z'] - 0.004 * k)),
            'body')
    # f 轻微胸线
    if lm['CHEST'] > 0:
        for sx in (-1, 1):
            ch = make_sphere('ChestMesh', hr * 0.52 * lm['CHEST'], 8,
                             (sx * hr * 0.62, ty - lm['TORSO_D'] * 0.40,
                              lm['CHEST_Z'] - 0.006 * k))
            ch.scale = (1.0, 0.72, 1.0)
            add(ch, 'body')

    # ── 服饰（设计 36 §4.3-3）──────────────────────────────────────────────
    # 衣领落在颈根（NECK_Z 下方 ~0.011k），不得盖到下颌（打样曾整脸被领吞掉）
    collar = spec.get('collar', 'plain')
    if collar in ('shirt', 'jacket', 'hoodie'):
        thick = {'shirt': 0.90, 'jacket': 1.10, 'hoodie': 1.20}[collar]
        for sx in (-1, 1):
            col = make_box(f'CollarMesh{sx}',
                           (0.0095 * k * thick, 0.0075 * k * thick, 0.0085 * k * thick),
                           (sx * lm['NECK_W'] * 0.85,
                            ny - lm['NECK_W'] * 0.42, lm['NECK_Z'] - 0.011 * k))
            col.rotation_euler = (0, 0, math.radians(-22 * sx))
            add(col, 'body')
        if collar == 'hoodie':
            # 连帽衫领：颈后小帽兜暗示（贴颈根，不抬高）
            add(make_box('HoodMesh', (lm['NECK_W'] * 1.75, 0.010 * k, 0.011 * k),
                         (0, ny + lm['NECK_W'] * 0.80, lm['NECK_Z'] - 0.012 * k)),
                'body')
    # 门襟线（前中细盒，微凸避 z-fighting）
    add(make_box('PlacketMesh', (0.0032 * k, 0.0012 * k, 0.040 * k),
                 (0, ty - lm['TORSO_D'] / 2 - 0.0004, lm['CHEST_Z'] - 0.004 * k)),
        'body')
    # 腰带（Pants 色细环）
    if spec.get('belt'):
        add(make_box('BeltMesh', (lm['TORSO_W'] * 1.05, lm['TORSO_D'] * 1.05, 0.0048 * k),
                     (0, ty, lm['WAIST_Z'] + 0.002 * k)), 'pants')
    # 上衣下摆（中长款）
    hem = float(spec.get('hem', 0.0))
    if hem > 0:
        add(make_box('HemMesh', (lm['TORSO_W'] * 1.04, lm['TORSO_D'] * 1.02, hem),
                     (0, ty, lm['WAIST_Z'] - spec.get('coat', 0.0) - hem / 2 + 0.003)),
            'body')

    # ── 四肢 + 手（球掌 + 拇指）───────────────────────────────────────────
    for side, sx in (('L', -1), ('R', 1)):
        add(make_box(f'UpperArm{side}Mesh',
                     (lm['ARM_UP_W'], lm['ARM_UP_W'], 0.045 * k),
                     (sx * lm['ARM_X'], arm_y, 0.1175 * k)), 'body')
        add(make_box(f'LowerArm{side}Mesh',
                     (lm['ARM_LO_W'], lm['ARM_LO_W'], 0.040 * k),
                     (sx * lm['ARM_X'], arm_y, 0.075 * k)), 'body')
        # 手：球掌 + 拇指小球（臂端）—— |x|+r 收在 shoulder/2 内（AABB 判据）
        hand_z = 0.052 * k
        palm = make_sphere(f'Palm{side}', lm['ARM_LO_W'] * 0.58, 8,
                           (sx * lm['ARM_X'], arm_y, hand_z))
        palm.scale = (0.95, 0.85, 1.20)
        add(palm, 'head')
        add(make_sphere(f'Thumb{side}', lm['ARM_LO_W'] * 0.30, 6,
                        (sx * (lm['ARM_X'] + lm['ARM_LO_W'] * 0.20),
                         arm_y - lm['ARM_LO_W'] * 0.55, hand_z + lm['ARM_LO_W'] * 0.22)),
            'head')
    for side, sx in (('L', -1), ('R', 1)):
        add(make_box(f'UpperLeg{side}Mesh',
                     (lm['LEG_UP_W'], lm['LEG_UP_W'], 0.040 * k),
                     (sx * lm['LEG_X'], 0, 0.065 * k)), 'pants')
        add(make_box(f'LowerLeg{side}Mesh',
                     (lm['LEG_LO_W'], lm['LEG_LO_W'], 0.035 * k),
                     (sx * lm['LEG_X'], 0, 0.0275 * k)), 'pants')
        add(make_box(f'Shoe{side}',
                     (lm['SHOE_W'], lm['SHOE_D'], lm['FOOT_Z']),
                     (sx * lm['LEG_X'], -0.0025, lm['FOOT_Z'] / 2)), 'shoes')

    # ── 拐杖（elder；细圆柱 + 柄，Shoes 色，贴身不超出肩宽 X 包络）─────────
    if spec.get('cane'):
        cx = lm['ARM_X']                 # 右手位；|ARM_X| < shoulder/2 ⇒ X 包络内
        hand_z = 0.052 * k
        add(make_cylinder('CaneShaftMesh', 0.0016 * k, 0.0016 * k, hand_z + 0.002 * k,
                          8, (cx, arm_y - lm['ARM_LO_W'] * 0.30,
                              (hand_z + 0.002 * k) / 2)),
            'shoes')
        handle = make_box('CaneHandleMesh', (0.011 * k, 0.0042 * k, 0.0034 * k),
                          (cx - 0.0035 * k, arm_y - lm['ARM_LO_W'] * 0.30,
                           hand_z + 0.0022 * k))
        add(handle, 'shoes')

    joined = join_objects(parts, 'CharacterMesh')
    joined.data.name = 'CharacterMesh'
    return joined


def variant_spec(key: str) -> dict:
    """VARIANTS[key] × GENDER_PLAN × AGE_PLAN → build 规格（landmarks 消费口径）。"""
    if key not in VARIANTS:
        raise SystemExit('unknown variant %r（可选：%s）'
                         % (key, ', '.join(sorted(VARIANTS))))
    v = VARIANTS[key]
    g, a = GENDER_PLAN[v['gender']], AGE_PLAN[v['age']]
    return dict(
        height=v['height'], shoulder=v['shoulder'],
        girth=a['girth'],
        hunch=a['hunch'], torso_rot=a['torso_rot'],
        torso_d=BASE_TORSO_D * a['torso_thick'],
        coat=v.get('hem', 0.0) * 0.35,          # 下摆归入 coat 延伸量（配合 hem 盒）
        hip_ratio=g['hip_ratio'], chest=g['chest'], shoulder_q=g['shoulder_q'],
        hair=v['hair'], collar=v.get('collar', 'plain'),
        belt=bool(v.get('belt')), hem=float(v.get('hem', 0.0)),
        hump=a['hump'], cane=a['cane'],
        top=v['top'], pants=v['pants'], skin=v['skin'],
        shoes=v['shoes'], hair_c=v['hair_c'],
    )


def build_variant(key: str) -> bpy.types.Object:
    """批次 36 入口：性别 × 年龄 参数化人物（armature + 10 骨 + 24 帧 walk clip）。"""
    spec = variant_spec(key)
    lm = variant_landmarks(spec)

    reset_scene()
    set_unit_meters()

    arm_obj = _build_armature(lm)      # 骨架定义与 archetype 同构（零改动）

    mats = {
        'body':  make_material('PedestrianBody', CHAR_PALETTE[spec['top']], *FABRIC),
        'pants': make_material('PedestrianPants', CHAR_PALETTE[spec['pants']], *FABRIC),
        'head':  make_material('PedestrianHead', CHAR_PALETTE[spec['skin']], *SKIN),
        'shoes': make_material('PedestrianShoes', CHAR_PALETTE[spec['shoes']], *FABRIC),
        'hair':  make_material('PedestrianHair', CHAR_PALETTE[spec['hair_c']], *FABRIC),
    }
    body_joined = _build_mesh_variant(spec, lm, mats)

    body_joined.parent = arm_obj
    mod = body_joined.modifiers.new(name='Armature', type='ARMATURE')
    mod.object = arm_obj

    # walk clip：与 build_character(archetype) 完全同一组关键帧（42 channels）
    scene = bpy.context.scene
    scene.frame_start = 1
    scene.frame_end = 24
    bpy.context.view_layer.objects.active = arm_obj
    bpy.ops.object.mode_set(mode='POSE')
    pbones = arm_obj.pose.bones

    def keyframe(bone_name, axis, frames_degrees):
        pb = pbones[bone_name]
        pb.rotation_mode = 'XYZ'
        for f, deg in frames_degrees:
            scene.frame_set(f)
            pb.rotation_euler[axis] = math.radians(deg)
            pb.keyframe_insert(data_path='rotation_euler', index=axis, frame=f)

    keyframe('upper_leg.L', 0, [(1, -30), (13, 30), (24, -30)])
    keyframe('upper_leg.R', 0, [(1, 30), (13, -30), (24, 30)])
    keyframe('upper_arm.L', 0, [(1, 30), (13, -30), (24, 30)])
    keyframe('upper_arm.R', 0, [(1, -30), (13, 30), (24, -30)])

    bpy.ops.object.mode_set(mode='OBJECT')
    scene.frame_set(1)
    return arm_obj


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
    """`-- --archetype <key> <out.glb>` 或 `-- --variant <key> <out.glb>`（= 亦可）。

    返回 (archetype, variant, out_path)；variant 非空时走批次 36 变体路径。
    """
    archetype, variant, out_path = 'char_casual', None, None
    i = 0
    while i < len(argv):
        a = argv[i]
        if a == '--archetype' and i + 1 < len(argv):
            archetype, i = argv[i + 1], i + 2
        elif a.startswith('--archetype='):
            archetype, i = a.split('=', 1)[1], i + 1
        elif a == '--variant' and i + 1 < len(argv):
            variant, i = argv[i + 1], i + 2
        elif a.startswith('--variant='):
            variant, i = a.split('=', 1)[1], i + 1
        elif a in ('-h', '--help'):
            print(__doc__)
            raise SystemExit(0)
        else:
            out_path, i = a, i + 1
    return archetype, variant, out_path


if __name__ == '__main__':
    raw = sys.argv[sys.argv.index('--') + 1:] if '--' in sys.argv else []
    archetype, variant, out_path = _parse_cli(raw)
    if not out_path:
        raise SystemExit('缺输出路径：… build_character.py -- --archetype|--variant <key> <out.glb>')
    if variant:
        build_variant(variant)
        label = variant
    else:
        build_character(archetype)
        label = archetype
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
    print(f'[size] {size} bytes ({label})', flush=True)
