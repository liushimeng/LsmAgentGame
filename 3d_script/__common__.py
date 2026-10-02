"""
__common__ — Blender headless 导出工具集（19-Blender3D模型集成）。

被 build_*.py 脚本统一 import；不直接执行。

公开 API:
  reset_scene()                 — 清空场景（删全部 mesh/lamp/material，但保留 world）
  set_unit_meters(scale=1.0)    — 把场景单位改成 meters（默认 1 blender unit = 1 m）
  make_box(name, size, pos, rot=None)        — 返回 bpy.types.Object
  make_cylinder(name, r_top, r_bot, h, segs, pos, rot=None)
  make_cone(name, r, h, segs, pos, rot=None)
  make_sphere(name, r, segs, pos)
  apply_pbr(obj, base_color, rough, metal, emissive=None, emissive_intensity=0.0)
  make_material(name, base_color, rough, metal, emissive=None, emissive_intensity=0.0) — 返回 mat
  weathered_pbr(obj, base_color, rough, metal, *, wear=0.35, grime='#3a352c', scale=6.0)
  CITY_PALETTE                  — 虚拟城市统一色板（2026-09-28 新增，见下）
  assign_material(obj, mat)
  join_objects(objs, name)      — join 多个 object 到一个，删除中间产物
  bake_transforms()             — 把「尺寸挂在 object transform 上」的静态 mesh 烘焙成 identity
  export_glb(out_path, apply=True, animations=True) — 走 bpy.ops.export_scene.gltf（导出前自动 bake_transforms）

约束:
  - 全部用 bpy.ops.primitive_*,避免直接构造 mesh.vertices / faces
  - PBR 颜色用 hex "#rrggbb",bpy 4.x 支持
  - 单位:全部 Blender 单位 = 米,导出后 GLB 自动以 meter 为基准
  - **导出节点必须 identity**（无 scale / rotation / translation）—— 见 bake_transforms 的说明
"""
import bpy
import math


def reset_scene() -> None:
    """删除所有 mesh / light / camera / material（保留 world 与 render settings）。"""
    # 删 object
    bpy.ops.object.select_all(action='SELECT')
    bpy.ops.object.delete(use_global=False)
    # 删 orphan data
    for col in (bpy.data.meshes, bpy.data.materials, bpy.data.images,
                bpy.data.cameras, bpy.data.lights, bpy.data.armatures,
                bpy.data.actions):
        for it in list(col):
            col.remove(it)


def set_unit_meters(scale: float = 1.0) -> None:
    """1 Blender unit = 1 meter（scale 参数对应 Blender Scene Properties → Unit Scale）。"""
    scene = bpy.context.scene
    scene.unit_settings.system = 'METRIC'
    scene.unit_settings.scale_length = scale
    scene.unit_settings.length_unit = 'METERS'


def _new_object(name: str, data, pos, rot=None):
    obj = bpy.data.objects.new(name, data)
    bpy.context.collection.objects.link(obj)
    obj.location = (pos[0], pos[1], pos[2])
    if rot is not None:
        obj.rotation_euler = (rot[0], rot[1], rot[2])
    return obj


def make_box(name: str, size, pos, rot=None):
    """size = (x, y, z) meters。"""
    bpy.ops.mesh.primitive_cube_add(size=1, location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.scale = (size[0], size[1], size[2])
    obj.location = (pos[0], pos[1], pos[2])
    if rot is not None:
        obj.rotation_euler = (rot[0], rot[1], rot[2])
    return obj


def make_cylinder(name: str, r_top: float, r_bot: float, h: float, segs: int, pos, rot=None):
    """圆柱（r_top == r_bot == h 为细管，r_top 0 / r_bot r 为圆锥）。"""
    bpy.ops.mesh.primitive_cylinder_add(vertices=max(segs, 8), radius=1, depth=1,
                                        location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    # Blender cylinder 默认沿 Z 轴,radius=1 depth=1 → 我们缩放到目标
    obj.scale = (r_bot if r_bot > 0 else r_top,
                 r_bot if r_bot > 0 else r_top,
                 h)
    obj.location = (pos[0], pos[1], pos[2])
    if rot is not None:
        obj.rotation_euler = (rot[0], rot[1], rot[2])
    return obj


def make_cone(name: str, r: float, h: float, segs: int, pos, rot=None):
    """圆锥（底半径 r，高 h），默认底朝下尖朝上。"""
    return make_cylinder(name, r_top=0.0, r_bot=r, h=h, segs=segs, pos=pos, rot=rot)


def make_sphere(name: str, r: float, segs: int, pos):
    bpy.ops.mesh.primitive_uv_sphere_add(radius=r,
                                         segments=max(segs, 8),
                                         ring_count=max(segs // 2, 4),
                                         location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.location = (pos[0], pos[1], pos[2])
    return obj


def make_icosphere(name: str, r: float, subdiv: int, pos):
    """正二十面体细分球（subdiv=0 → 20 面；1 → 80 面）。

    批次 44 新增。`make_sphere` 走 UV 球（segs/2 圈纬线），面数随 segs 线性涨且
    极点密集；树冠叶簇要的是「低面数、棱面明显、彼此不成球」的颗粒感，
    ico 球 subdiv=0（20 面）是性价比最高的叶团表达。
    尺寸写在 radius 上（不走 obj.scale），天然满足 §27.3-6「节点变换 identity」。
    """
    bpy.ops.mesh.primitive_ico_sphere_add(subdivisions=max(int(subdiv), 0),
                                           radius=r,
                                           location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.location = (pos[0], pos[1], pos[2])
    return obj


def make_taper(name: str, r_bot: float, r_top: float, h: float, segs: int, pos, rot=None):
    """**上下不同半径**的圆台（树干收分 / 根盘 / 枝条渐细 / 锥形树冠层）。

    批次 44 新增。`make_cylinder` 的 `r_top` **被静默忽略**（它对 X/Y 用同一个
    `r_bot` 做等比缩放），因此它只能做正圆柱 —— 想做带收分的树干/根盘/枝条必须用本函数。
    尺寸直接写进 `radius1`/`radius2`/`depth`（不走 obj.scale），
    天然满足 §27.3-6「节点变换 identity」。

    r_top = 0 ⇒ 真正的圆锥；r_bot = r_top ⇒ 等价于 make_cylinder（但本函数走 cone 图元，
    侧面三角化方式与 cylinder 略有差异，混用时以本函数为准）。
    """
    bpy.ops.mesh.primitive_cone_add(vertices=max(segs, 6),
                                    radius1=r_bot,
                                    radius2=max(float(r_top), 0.0),
                                    depth=h,
                                    location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.location = (pos[0], pos[1], pos[2])
    if rot is not None:
        obj.rotation_euler = (rot[0], rot[1], rot[2])
    return obj


def center_content_xz(objs=None):
    """把整件内容盒在 **X/Z 上居中**（§27.3「原点 minY = 0 贴地、X/Z 居中」）。

    批次 44 新增。为何不能靠"对称布点"自然居中：叶簇/枝端用的是 **icosahedron
    （正二十面体）**，它在面内**不是旋转对称**的 —— 即使枝条方位角在数学上
    均布（6 枝 60°、4 枝 90°），每个 icosphere 的自身顶点分布仍随朝向变化，
    合起来就是一个**偏心**的内容盒。实测 pine_tree 曾因此出现
    X ∈ [-0.148, +0.172]（中心 +0.012 u = 12 cm 偏移）。

    这在纯装饰件上无害，但会破坏 §27.3 的原点约定，且消费端
    `edge/glbInstanced.tsx::collectGlbMeshParts` 直接用 `matrixWorld`
    （**不像** `glbInstances.ts` 那样做 `pairPivotOffset` 内容盒归一化），
    偏移量会原样进世界。

    本函数按**几何世界包围盒**（含节点变换）求并集，再整体平移。
    Y 方向不动 —— 贴地对齐由建模时的绝对高度保证。
    """
    import mathutils
    objs = list(objs) if objs is not None else list(bpy.context.scene.objects)
    meshes = [o for o in objs if o.type == 'MESH']
    if not meshes:
        return (0.0, 0.0)
    bpy.context.view_layer.update()
    lo = mathutils.Vector((float('inf'), float('inf'), float('inf')))
    hi = mathutils.Vector((float('-inf'), float('-inf'), float('-inf')))
    for o in meshes:
        for corner in o.bound_box:
            p = o.matrix_world @ mathutils.Vector(corner)
            for i in range(3):
                lo[i] = min(lo[i], p[i])
                hi[i] = max(hi[i], p[i])
    # ⚠ **Blender 是 Z-up** —— 要居中的是两个**水平**轴 (X, Y)，**不是** (X, Z)。
    #   曾把 Z（垂直轴）当水平轴居中，结果整棵树沿垂直方向平移了半个树高
    #   （palm_tree 实测 minY = -0.256 m，贴地判据直接 FAIL）。
    dx = -(lo.x + hi.x) / 2.0
    dy = -(lo.y + hi.y) / 2.0
    for o in meshes:
        o.location = (o.location[0] + dx, o.location[1] + dy, o.location[2])
    bpy.context.view_layer.update()
    return (dx, dy)


def make_material(name: str, base_color: str, rough: float, metal: float,
                  emissive=None, emissive_intensity: float = 0.0,
                  alpha: float = 1.0):
    """创建 PBR 材质（Principled BSDF）。base_color 接受 "#rrggbb"。

    alpha < 1.0 → 接线 Principled Alpha 输入，glTF 导出为 alphaMode BLEND
    （批次 41 车窗玻璃用；既有调用点缺省 1.0，行为逐字节不变）。
    """
    mat = bpy.data.materials.new(name)
    mat.use_nodes = True
    bsdf = mat.node_tree.nodes.get('Principled BSDF')
    if bsdf is None:
        raise RuntimeError('Principled BSDF node missing')
    # Base Color
    r, g, b = _hex_to_rgb(base_color)
    bsdf.inputs['Base Color'].default_value = (r, g, b, 1.0)
    bsdf.inputs['Roughness'].default_value = rough
    bsdf.inputs['Metallic'].default_value = metal
    if emissive is not None:
        er, eg, eb = _hex_to_rgb(emissive)
        bsdf.inputs['Emission Color'].default_value = (er, eg, eb, 1.0)
        bsdf.inputs['Emission Strength'].default_value = emissive_intensity
    if alpha < 1.0:
        bsdf.inputs['Alpha'].default_value = alpha
    return mat


def assign_material(obj, mat):
    """把 mat 赋给 obj 的所有 face（覆盖默认）。"""
    obj.data.materials.clear()
    obj.data.materials.append(mat)


def apply_pbr(obj, base_color: str, rough: float, metal: float,
              emissive=None, emissive_intensity: float = 0.0,
              alpha: float = 1.0):
    """便利方法：make_material + assign_material 一体。"""
    mat = make_material(obj.name + '_Mat', base_color, rough, metal,
                        emissive=emissive, emissive_intensity=emissive_intensity,
                        alpha=alpha)
    assign_material(obj, mat)
    return obj


def join_objects(objs, name: str):
    """把多个 obj join 成一个，保留 name 作为最终 object 名。"""
    bpy.ops.object.select_all(action='DESELECT')
    for o in objs:
        o.select_set(True)
    bpy.context.view_layer.objects.active = objs[0]
    bpy.ops.object.join()
    joined = bpy.context.active_object
    joined.name = name
    return joined


def _hex_to_rgb(hex_str: str):
    """'#rrggbb' / 'rrggbb' → (r, g, b) 浮点 0..1。"""
    s = hex_str.lstrip('#')
    if len(s) != 6:
        raise ValueError(f'bad hex color: {hex_str}')
    return (int(s[0:2], 16) / 255.0,
            int(s[2:4], 16) / 255.0,
            int(s[4:6], 16) / 255.0)


def bake_transforms() -> list:
    """把「几何在 mesh 里、尺寸挂在 object transform 上」的静态对象烘焙成 identity。

    ## 为什么必须有这一步（真实事故 ×3）

    `make_box` / `make_cylinder` / `make_sphere` 用 `obj.scale` 表达尺寸，而
    `join_objects` 只把其他对象的几何折算进**活动对象**的局部空间 —— 活动对象自身那份
    `location` / `scale` 会**留在结果对象上**。于是导出的 GLB 里：

        几何顶点 = 单位尺度（accessor 尺寸如 [2, 1, 2]）
        nodes[i].scale = 真实尺寸（如 [0.0375, 0.09, 0.0375]）

    两者相乘才是世界尺寸。**任何在节点以下取局部矩阵的消费端都会把 scale 抵消掉**：
    `RoadsideBins.collectPairs()` 算的是 `rootInv × mesh.matrixWorld`（root = 变体节点），
    rootInv 正好抵消节点上的 scale ⇒ instancedMesh 按原始单位几何渲染 ⇒
    场景里 168 只垃圾桶每只 **≈20 m 直径 × 10 m 高**（用户反馈"垃圾桶比楼房还大"）。
    同类事故另有两起：批次 26 的 10× 雪山、批次 19 的玩具车尺度。

    `bpy.ops.export_scene.gltf(export_apply=True)` 是 **Apply Modifiers**，
    **不会**烘焙 object 的 transform —— 别指望它。

    ## 保守作用域

    只处理「静态、叶子、无骨骼、无形变」的对象：

      · `type == 'MESH'`
      · 无 children（不破坏父子层级语义）
      · 自身与祖先链上都没有 ARMATURE（不动骨骼链：行人 armature + 骨骼平移必须原样导出，
        否则 walk clip 与绑定关系被破坏）
      · 无 ARMATURE modifier（蒙皮 mesh 的顶点受 inverseBindMatrices 支配）
      · 无 shape key（transform_apply 会丢形态键）

    幂等：对已经 identity 的对象是无副作用的 no-op（先判 identity 再调用，字节级零变化）。

    返回：被烘焙的对象名列表（未烘焙的不计入），便于日志与自检。
    """
    baked = []
    for obj in list(bpy.context.scene.objects):
        if obj.type != 'MESH':
            continue
        if obj.children:
            continue
        if obj.data.shape_keys is not None:
            continue
        if any(m.type == 'ARMATURE' for m in obj.modifiers):
            continue
        # 祖先链上不得有 ARMATURE（骨骼子级保持原样）
        anc, has_arm_ancestor = obj.parent, False
        while anc is not None:
            if anc.type == 'ARMATURE':
                has_arm_ancestor = True
                break
            anc = anc.parent
        if has_arm_ancestor:
            continue
        # 幂等：已是 identity 则跳过（避免任何多余的 mesh 数据触碰）
        loc, rot, scl = obj.location, obj.rotation_euler, obj.scale
        if (all(abs(v) < 1e-9 for v in loc)
                and all(abs(v) < 1e-9 for v in rot)
                and all(abs(v - 1.0) < 1e-9 for v in scl)):
            continue
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        baked.append(obj.name)
    return baked


def export_glb(out_path: str, apply: bool = True, animations: bool = True) -> None:
    """导出当前场景为 .glb。

    apply=True:  导出前 apply 所有**修饰器**（视觉与 gltf-viewer 一致）
    animations=True: 导出当前所有 armature action（行人 walk 动画依赖此）

    ⚠️ `apply` 走的是 `bpy.ops.export_scene.gltf(export_apply=...)`，语义是
    **Apply Modifiers**，**不会**烘焙 object 的 transform。节点 identity 由导出前
    统一调用的 `bake_transforms()` 保证（见其 docstring：这是"尺寸挂在节点变换上"
    的生产源，已造成三起事故，不要再在调用点依赖 export_apply）。
    """
    import os
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    # 1. 先烘焙「尺寸挂在 object transform 上」的静态对象（幂等；骨骼/蒙皮链跳过）
    baked = bake_transforms()
    if baked:
        print(f'[export_glb] baked object transform -> identity: {", ".join(baked)}', flush=True)
    # 2. 选中全部
    bpy.ops.object.select_all(action='DESELECT')
    for o in bpy.context.scene.objects:
        if o.type in ('MESH', 'ARMATURE', 'EMPTY'):
            o.select_set(True)
    bpy.context.view_layer.objects.active = bpy.context.scene.objects[0] if bpy.context.scene.objects else None
    bpy.ops.export_scene.gltf(
        filepath=out_path,
        export_format='GLB',
        export_apply=apply,
        export_animations=animations,
        export_materials='EXPORT',
        export_skins=animations,
        export_morph=False,
        export_lights=False,
        export_cameras=False,
        use_selection=True,
    )
    print(f'[export_glb] wrote {out_path}', flush=True)


# ══════════════════════════════════════════════════════════════════════════
# 统一色调 + 经年磨损（2026-09-28 新增；本节为**纯增补**，未改动上方任何既有函数）
# ══════════════════════════════════════════════════════════════════════════

# 虚拟城市统一色板。
#
# 动机（CLAUDE.md §27.3 判据：全城材质色调统一）：现有 20 个 build_*.py 里散落着
# **70+ 个各写各的 hex** —— `#2f7a3a` / `#3a8a45` / `#2e8b57` / `#35793a` 都是"树的绿"，
# `#888888` / `#8a8d92` / `#7a8088` 都是"灰"。同一语义多套近似色，正是"统一色调"要收口的地方。
#
# 用法：`apply_pbr(obj, CITY_PALETTE['concrete'], 0.75, 0.0)`，或
#       `weathered_pbr(obj, CITY_PALETTE['steel'], 0.45, 0.9)`。
# 新增 build 脚本**一律从本表取色**，不要就地写 hex；确需例外时先加语义键，别加裸 hex。
#
# 取色原则（整城一套光照下的可读性优先，兼顾 §26 对比度硬阈值）：
#   · 饱和度整体压低（城市是水泥/沥青/钢，不是糖果）—— 最亮绿也只到 ~0.55 明度；
#   · 同语义族的明度拉开 ≥ 12%，保证远近两件同类资产在同屏里能分辨；
#   · 金属色只用于 `steel*` / `rust`，且 metallic=1 时基色偏暗（漫反射趋 0，全靠反射）。
CITY_PALETTE = {
    # ── 地面与铺装 ────────────────────────────────────────────────────────
    'asphalt':        '#3f4247',   # 城市主路：比水泥暗一档，避免与建筑主体抢视觉
    'concrete':       '#b6b3ad',   # 建筑主体 / 人行道（整城最亮的中性色）
    'concrete_dark':  '#8d8a85',   # 女儿墙 / 基座 / 承重构件
    'brick':          '#9c5a44',   # 红砖：暖色只此一档，防止整城发橙
    'paint_road':     '#e6e2d6',   # 道路标线 / 斑马线（略偏米白，纯白在暗色沥青上刺眼）
    'paint_yellow':   '#d9b13f',   # 车道线 / 警示带
    # ── 金属 ──────────────────────────────────────────────────────────────
    'steel':          '#8d949c',   # 结构钢 / 护栏 / 灯杆（金属度高的浅灰）
    'steel_dark':     '#4e545c',   # 桁架 / 机械 / 窗框
    'rust':           '#8a4a2c',   # 锈蚀面：金属度降到 0.6，锈本身是绝缘氧化层
    # ── 玻璃与塑料 ────────────────────────────────────────────────────────
    'glass':          '#9fc4cf',   # 窗玻璃：浅青，metallic=0，靠低粗糙度出反射
    'glass_dark':     '#33454f',   # 幕墙 / 驾驶室玻璃
    'plastic':        '#c8ccd0',   # 车壳 / 塑料件（中性偏冷，与钢区分靠粗糙度）
    # ── 有机物 ────────────────────────────────────────────────────────────
    'foliage':        '#4a7a3f',   # 夏季树叶：城市绿，饱和度压到不刺眼
    'foliage_autumn': '#b8792f',   # 秋叶（oak_tree_autumn）
    'wood':           '#7a5a3a',   # 木板 / 树皮 / 栈桥
    'soil':           '#5a4634',   # 树池 / 地被土
    # ── 车辆涂装（批次 41 新增；涂装是「品牌语义」不是「城市材质」，单列一族）──
    'tire':           '#1c1e22',   # 轮胎橡胶（全车型共用）
    'vehicle_glass':  '#2c4356',   # 车窗玻璃基色（透明度走 alpha 通道）
    'vehicle_dark':   '#22262c',   # 下裙 / 格栅 / 内饰 / 底板（车用暗塑料）
    'sedan_blue':     '#3a5a8a',   # 私家轿车主流蓝（批次 41 沿用 v19 基色）
    'sedan_silver':   '#b8bcc2',   # 私家轿车银灰变体
    'taxi_yellow':    '#e8c020',   # 出租车涂装黄
    'bus_teal':       '#2a8c4a',   # 公交下裙带（青绿系，沿用 v19 基色）
    'bus_white':      '#e8eaec',   # 公交上装白
    'truck_red':      '#c83a2c',   # 货车驾驶室红（消防红同族）
    'truck_cargo':    '#c8ccd0',   # 货车货厢浅灰
    'sign_amber':     '#ffb43a',   # 公交路牌屏 emissive 琥珀
    # ── 街具（批次 42 新增；城市家具是「市政语义」不是建筑/车辆材质，单列一族）──
    'post_green':     '#0a6b45',   # 中国邮政绿（柱式邮筒主色）
    'post_green_d':   '#084f34',   # 邮政绿暗部（顶盖/铭牌底）
    'sign_blue':      '#1a4f9c',   # 路名牌蓝底（交通指路牌国标色系）
    'sign_green':     '#0d6b3f',   # 信息牌绿底
    'meter_blue':     '#3a5a8a',   # 停车咪表机身蓝灰
    'glass_panel':    '#a8c8d0',   # 候车亭玻璃（比 glass 略暖，与建筑窗区分）
    'bench_alu':      '#9aa2a8',   # 铝条座椅（阳极氧化铝）
    'reflect_white':  '#e8eaec',   # 反光膜牌面/标识白
    # ── 交通信号灯（批次 43 新增；信号灯是「交通语义」不是建筑/街具材质，单列一族）──
    'signal_housing': '#17191d',   # 灯箱近黑箱体
    'signal_visor':   '#101216',   # 遮光罩黑
    'signal_red':     '#ff2d2d',   # 红色 LED
    'signal_yellow':  '#ffc40f',   # 黄色 LED
    'signal_green':   '#2ecc71',   # 绿色 LED
    # ── 植被（批次 44 新增；树皮/针叶/棕榈是「植物语义」不是建筑材质，单列一族）──
    # 悬铃木（法桐）行道树：浅灰褐平滑树皮（悬铃木最显著特征 —— 剥落后呈灰白斑驳）
    'bark_sycamore':  '#9a9187',
    # 根盘 / 根颈：比干皮深一档的土褐（根颈长期贴地潮湿积污）
    'bark_root':      '#6d6357',
    # 松树皮：深棕红、鳞片状开裂（黑松/雪松老皮）
    'bark_pine':      '#6b4a35',
    # 松针：深墨绿（黑松针叶 2 针一束，色比阔叶深暗）
    'needle_pine':    '#2c4a2c',
    # 棕榈干：红褐（棕榈「基部显著膨大 + 红褐色树干 + 环状叶痕」形态志特征）
    'palm_trunk':     '#7a4b34',
    # 棕榈扇叶：比阔叶更亮更黄的青绿（棕榈科叶面蜡质）
    'palm_frond':     '#4c8a46',
    # ── 公园设施（批次 45 新增；公园家具是「园林语义」不是街具/建筑材质，单列一族）──
    # 仿古木构（六角亭檐柱/枋/美人靠）：深褐红木构——比通用 wood 更红更暗，
    # 是「油漆过的仿古 timber」而非原木
    'pavilion_timber': '#5e3a2a',
    # 青灰瓦屋面（江小青瓦烧成色：青灰偏蓝，比混凝土暗一档带冷调）
    'roof_tile_slate': '#4a4e55',
    # 儿童游乐塑料三原色（GB 6675 游乐设施主流配色：滑道红/结构蓝）
    'play_plastic_red':  '#c0392b',
    'play_plastic_blue': '#2b6cb0',
    # 游乐场镀锌钢构立柱（黄花镀锌漆 + 安全警示色系统；预览判读后提亮一档，
    # 原值 #d9a521 在预览打光下偏橄榄绿）
    'play_steel_yellow': '#e3b426',
    # 全民健身器材绿（体彩/全民健身工程标准涂装：器材绿）
    'park_steel_green': '#2f7d4f',
    # 健身踏板/转盘面/单杠杠面（深灰塑料包覆层）
    'fitness_pad':     '#3a3f47',
    # 长椅铸铝弓形脚（市政街道家具深灰铸铝）
    'bench_leg':       '#494e57',
    # ── 城市公用设施（批次 46 新增；变电/加油/停机坪是「市政设施语义」，
    #    不复用街具族也不复用建筑族，单列一族）──────────────────────────
    # 预制箱变舱墙板（户外 PC 夹芯板本色：比通用 concrete 冷一档、亮半档）
    'xfmr_panel':      '#c0c2c0',
    # 预制舱金属屋面（深灰，比 wall 板暗两档，屋面在俯视下主导）
    'xfmr_roof':       '#7b7f83',
    # 箱变舱百叶通风窗（深灰格栅，比舱体暗两档）
    'vent_louver':     '#3f454c',
    # 变电围栏网片（热镀锌网片灰蓝：比结构钢冷一档，与立柱拉开明度）
    'fence_mesh':      '#5d646c',
    # 加油站罩棚板（经年褪色到砖红 —— 不取消防红，避免与消防车抢视觉）
    'canopy_red':      '#a83b32',
    # 加油站便利店外墙涂料（商业外墙常用高明度米白，比 concrete 亮）
    'shop_white':      '#e2e0da',
    # 加油机机身（整机统一冷白灰）
    'pump_body':       '#d2d4d0',
    # 停机坪坪面涂层（浅色防滑涂层，比城市主路 asphalt 亮一档）
    'helipad_deck':    '#5b5f63',
    # 停机坪风向袋红段（红白相间 5 段里的红）
    'windsock_red':    '#c0392b',
    # 设施警示黄（比车道黄更暖更饱和：设备防撞柱/警示灯箱专用语义）
    'warn_amber':      '#e0a020',
    # 形象牌灯箱乳白亚克力（高杆牌/门头牌的**透光板**本色，夜间背光泛暖白）
    'sign_lightbox':   '#e8e2d0',
    # TLOF 着陆区内嵌绿灯（ICAO/CAP.437 规定的绿色，与信号灯绿同色但语义不同：
    # 信号灯绿表示"通行"，着陆区绿表示"可接地"）
    'tlof_green':      '#2ecc71',
    # 助航灯暖白（白炽/LED 泛光灯的暖白，与标线 reflect_white 的中性白区分）
    'lamp_warm':       '#fff4e0',
}
# 白/黑/警示红单列在表外（不是"材质语义"而是全城通用）：黑橡胶轮胎、红消防标识。

# weathered_pbr 的缺省脏污色（与 CITY_PALETTE 解耦：污渍色要同时压住所有基色）
DEFAULT_GRIME = '#3a352c'


def _in(node, name, stype):
    """按 (name, type) 在节点输入里取 socket。

    Blender 4.x+ 的 `ShaderNodeMix` 一份节点里有 4 组同名 A/B/Factor
    （VALUE / VECTOR / RGBA / ROTATION），按名字取会拿错组，故按类型二次定位。
    """
    for s in node.inputs:
        if s.name == name and s.type == stype:
            return s
    raise RuntimeError('%s 上找不到输入 %s(%s)' % (node.bl_idname, name, stype))


def _out(node, name, stype):
    """按 (name, type) 在节点输出里取 socket（理由同 _in）。"""
    for s in node.outputs:
        if s.name == name and s.type == stype:
            return s
    raise RuntimeError('%s 上找不到输出 %s(%s)' % (node.bl_idname, name, stype))


def weathered_pbr(obj, base_color: str, rough: float, metal: float, *,
                  wear: float = 0.35, grime: str = DEFAULT_GRIME, scale: float = 6.0):
    """在 apply_pbr 之上叠加**经年磨损**：污渍/色斑混入 Base Color + 同一噪声调制 Roughness。

    节点链（wear > 0 时才建）：
        TexCoord.Object ──► Noise(scale, detail=12) ──► ColorRamp(0.42→0.78)
                                                        ├─► ×wear ──► Mix(base, grime) ──► Base Color
                                                        └─► ×wear×(1-rough) ──► +rough ──► Roughness
    即：脏的地方同时**变暗**（污渍色混入）和**变粗糙**（漫反射吃掉高光），
    这与真实污渍的观感一致（只变暗不变粗糙会像"贴了张半透明纸"）。

    wear = 0（默认恒真）⇒ **不建任何额外节点**，输出与 apply_pbr 逐字段等价
    （同样的 Base Color / Roughness / Metallic 默认值，无任何连线覆盖）。

    scale：Object 空间下的噪声频率。程序化网格导出前物体尺度 = 米，6.0 ≈ 每 17 cm 一块
    污渍斑；做整体风化取 2~4，做"脏兮兮的金属件"取 8~15。

    ⚠ **程序化噪声不会随 glTF 导出**：Noise Texture 在 glTF 规范里没有对应物，
    export_glb 会静默丢弃节点图，导出的 GLB 只剩 make_material 设的纯色基值。
    因此本函数的价值在 (a) Blender 内的预览 / 打光评估、(b) 后续烘焙成贴图的源；
    要让磨损真的进游戏，必须烘成 basecolor/roughness 贴图再挂（尚未实现）。
    """
    wear = float(wear)
    if wear <= 0.0:
        return apply_pbr(obj, base_color, rough, metal)

    mat = make_material(obj.name + '_Mat', base_color, rough, metal)
    nt = mat.node_tree
    bsdf = nt.nodes.get('Principled BSDF')
    if bsdf is None:
        raise RuntimeError('Principled BSDF node missing')

    coord = nt.nodes.new('ShaderNodeTexCoord')
    coord.location = (-1100, 0)
    noise = nt.nodes.new('ShaderNodeTexNoise')
    noise.location = (-900, 0)
    noise.inputs['Scale'].default_value = scale
    noise.inputs['Detail'].default_value = 12.0      # 高 detail ⇒ 边缘破碎，不像橡皮擦
    noise.inputs['Roughness'].default_value = 0.62   # 让噪声本身更"絮状"一点
    ramp = nt.nodes.new('ShaderNodeValToRGB')
    ramp.location = (-700, 0)
    # 高对比 ramp：多数区域保持干净基色，只有 ~25% 面积被判定为"脏"，
    # 避免 wear 一调大就整面糊成脏色。
    ramp.color_ramp.elements[0].position = 0.42
    ramp.color_ramp.elements[0].color = (0, 0, 0, 1)
    ramp.color_ramp.elements[1].position = 0.78
    ramp.color_ramp.elements[1].color = (1, 1, 1, 1)
    nt.links.new(coord.outputs['Object'], noise.inputs['Vector'])
    nt.links.new(noise.outputs['Factor'], ramp.inputs['Fac'])

    # 因子 = ramp × wear（钳到 1：wear 与 ramp 相乘天然 ≤ 1，钳位只为防调用方传 >1）
    fac = nt.nodes.new('ShaderNodeMath')
    fac.location = (-500, -120)
    fac.operation = 'MULTIPLY'
    fac.inputs[1].default_value = min(wear, 1.0)
    nt.links.new(ramp.outputs['Color'], fac.inputs[0])

    # Base Color = mix(base, grime, fac)
    mix = nt.nodes.new('ShaderNodeMix')
    mix.data_type = 'RGBA'
    mix.location = (-300, 120)
    _in(mix, 'A', 'RGBA').default_value = (*_hex_to_rgb(base_color), 1.0)
    _in(mix, 'B', 'RGBA').default_value = (*_hex_to_rgb(grime), 1.0)
    _in(mix, 'Factor', 'VALUE').default_value = 0.0
    nt.links.new(fac.outputs[0], _in(mix, 'Factor', 'VALUE'))
    nt.links.new(_out(mix, 'Result', 'RGBA'), bsdf.inputs['Base Color'])

    # Roughness = rough + fac × (1 - rough)
    span = nt.nodes.new('ShaderNodeMath')
    span.location = (-300, -300)
    span.operation = 'MULTIPLY_ADD'
    span.inputs[1].default_value = 1.0 - float(rough)
    span.inputs[2].default_value = float(rough)
    nt.links.new(fac.outputs[0], span.inputs[0])
    nt.links.new(span.outputs[0], bsdf.inputs['Roughness'])

    assign_material(obj, mat)
    return obj


def flatten_weathered_materials(obj):
    """weathered_pbr 的 Mix/Noise 节点链会让 glTF 导出**丢掉 Base Color**（导出白模）：
    Principled BSDF 的 Base Color 被链路占用后，导出器读不到常量 ⇒ baseColorFactor 缺省
    = 纯白（批次 38 实测：canal_bank/canal_reed 首版 9/51 个材质全白）。
    导出前解链回常量：基色取 Mix 的 A 输入（weathered_pbr 写入的 CITY_PALETTE 值），
    粗糙度取 Math MULTIPLY_ADD 的 addend（= 原 rough）。磨损只存在于 Blender 场景
    预览（glTF 无 Noise 对应物，weathered_pbr 文档已注明），游戏内即纯色基值。

    2026-10-02 批次 42：自 build_canal_bank.py 的私有副本上收为共享工具
    （新街具 5 件都需要；原副本保留不动，行为逐字节一致）。
    """
    mats = obj.data.materials if hasattr(obj, 'data') else []
    for mat in mats:
        if not mat or not mat.use_nodes:
            continue
        bsdf = mat.node_tree.nodes.get('Principled BSDF')
        if bsdf is None:
            continue
        bc = bsdf.inputs['Base Color']
        if bc.is_linked:
            node = bc.links[0].from_node
            col = (1.0, 1.0, 1.0, 1.0)
            if node.bl_idname == 'ShaderNodeMix':
                for s in node.inputs:
                    if s.name == 'A' and s.type == 'RGBA':
                        col = tuple(s.default_value)
                        break
            for lk in list(bc.links):
                mat.node_tree.links.remove(lk)
            bc.default_value = col
        rc = bsdf.inputs['Roughness']
        if rc.is_linked:
            node = rc.links[0].from_node
            rough = 0.8
            if node.bl_idname == 'ShaderNodeMath':
                rough = float(node.inputs[2].default_value)   # MULTIPLY_ADD 的 addend
            for lk in list(rc.links):
                mat.node_tree.links.remove(lk)
            rc.default_value = rough


def shared_weathered(objs, base_color: str, rough: float, metal: float, *, name: str = '', **kw):
    """多件共享**同一** weathered 材质（批次 42）：先给首件建 weathered_pbr 材质，
    再把同一 datablock 赋给其余件 —— join 后材质槽按 datablock 去重，
    每个语义组只占 1 个 primitive/DC（32 实例 × 11 槽的邮筒会爆 DC 预算）。
    磨损噪声只在首件材质节点图里（glTF 一律丢弃，flatten 后即纯色）。"""
    if not objs:
        return
    weathered_pbr(objs[0], base_color, rough, metal, **kw)
    mat = objs[0].data.materials[0]
    if name:
        mat.name = name
    for o in objs[1:]:
        assign_material(o, mat)
