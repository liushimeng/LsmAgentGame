#!/usr/bin/env python3
"""
render_preview — 虚拟城市 GLB 的**可执行渲染检查**（headless 预览图）。

背景：本仓库长期只有「导出」没有「渲染」—— 22 个 build_*.py 导出 GLB 后，
唯一的自动验收是 verify_glb_aabb.py 的纯几何判据（轴向/贴地/尺度/节点 identity），
**没有任何一条判据能看出「颜色发黑」「材质糊了」「构图看不全」**。本脚本补上这一环。

用法：
  blender --background --python 3d_script/render_preview.py -- <in.glb> <out.png> \\
      [--view front|side|iso|top] [--width 1280] [--height 960]
      [--engine eevee|cycles] [--samples 32] [--no-ground] [--view-transform Standard|AgX|...]

行为约定：
  · **包围盒量测复用 verify_glb_aabb 的口径**（read_glb + world_aabb），不另写一份
    量测逻辑 —— 两处数字口径一旦漂移，预览图的自适应构图就会和 CI 的几何判据说两套话。
  · 导入走 `bpy.ops.import_scene.gltf`（Blender 5.2.1 实测可用）；glTF 的 Y-up 由导入器
    转成 Blender 的 Z-up，本脚本内部**一律在 Blender 系（Z-up）里摆相机**，与游戏内
    「模型直立」的观感一致（导出态 Z-up，见 CLAUDE.md §27.3 判据 5）。
  · 相机距离按包围盒**逐角点精确求解**（不是「包围球半径 / sin(fov)」那种保守估法），
    保证长条物件（road_props 3 盏灯沿 X 排开）在正视/侧视下也不会缩成小芝麻。
  · 视角名按 **glTF 轴**命名，不是按「车头/车尾」：`front` = 站在 glTF +Z 面往回看。
    车辆的长轴是 X（见 verify_glb_aabb 的 axis='x_flat' 判据），所以对 sedan / taxi
    来说 `--view front` 出来的其实是**车的正侧面**，`--view side` 才是车头。不用记，
    反正 iso 视角对车辆最常用。
  · 三点布光全部用 **SUN**（key/fill/rim）—— 太阳光强与距离无关，天然尺度无关，
    从 0.05 单位的行人到 20 单位的货轮不用改任何参数。
  · 默认色彩变换 **Standard**（不是 Blender 5.x 的 AgX）：预览图的用途是核对
    「颜色对不对 / 亮不亮 / 会不会糊」，Standard 保色相与饱和度，AgX 会把高饱和的
    红绿压成灰奶白、且把纯白压成米色，掩盖材质问题。代价是过曝即纯白，所以灯强
    是按「Standard 下不过曝」反推的（见 setup 三点布光处的注释）。
  · 退出码 0 = 成功；非 0 = 失败（原因打 stderr）。用 os._exit 保证进程码真的传出去
    （Blender 5.x 下 sys.exit 未必被转成进程退出码）。

性能（本机实测，无 GPU / EGL_BAD_MATCH 告警，走 llvmpipe 软件光栅）：
  1280x960 @ --samples 32 单图约 60~100 s；samples 16 约 40~60 s。
  EEVEE 的屏幕空间 raytracing 已显式关闭（开着单帧慢一个数量级且对资产质检无收益）。
  批量出图请串行跑（软件光栅吃满 CPU，并发只会互相拖慢）。

⚠ 本脚本只读 GLB，**不写回任何资产**，也不被任何 build_*.py import —— 既有 22 个
导出脚本的行为零变化。
"""
import math
import os
import sys
import traceback

import bpy
from mathutils import Vector

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

# 量测口径的唯一事实来源（不复制粘贴一份）
import verify_glb_aabb as V   # noqa: E402

# 相机朝向（Blender 系 Z-up）：dir = 从目标指向相机的单位方向
VIEWS = {
    'iso':   Vector((0.75, -1.00, 0.62)).normalized(),   # 右前上方 3/4 视角，仰角 ≈26°
    'front': Vector((0.00, -1.00, 0.00)).normalized(),   # glTF +Z 面（→ Blender -Y）
    'side':  Vector((1.00, 0.00, 0.00)).normalized(),    # glTF +X 面
    'top':   Vector((0.00, 0.00, 1.00)).normalized(),    # 顶视
}

LENS = 50.0          # mm
SENSOR = 36.0        # mm（sensor_fit=HORIZONTAL ⇒ 水平半角 = atan(18/50)）
MARGIN = 1.16        # 留白系数：占画幅 ~86%
METERS_PER_UNIT = 10.0   # 1 世界单位 = 10 m（与 verify_glb_aabb 一致）


# ── GLB 量测（复用 verify_glb_aabb）────────────────────────────────────────
def measure(glb_path):
    """返回 Blender 系（Z-up）的 (lo, hi) 向量组。glTF Y-up → Blender Z-up:
    (x, y, z)_gltf → (x, -z, y)_blender。"""
    js, blob = V.read_glb(glb_path)
    box = V.world_aabb(js, blob)
    if not box['details']:
        raise ValueError('GLB 内无几何（无 POSITION primitive）：%s' % glb_path)
    lo, hi = box['lo'], box['hi']
    # 逐轴取 min/max（-z 会翻越 min/max，直接搬 lo/hi 会得到 lo>hi 的退化盒）
    c_lo = Vector((lo[0], -lo[2], lo[1]))
    c_hi = Vector((hi[0], -hi[2], hi[1]))
    b_lo = Vector((min(c_lo.x, c_hi.x), min(c_lo.y, c_hi.y), min(c_lo.z, c_hi.z)))
    b_hi = Vector((max(c_lo.x, c_hi.x), max(c_lo.y, c_hi.y), max(c_lo.z, c_hi.z)))
    if any(b_hi[i] - b_lo[i] <= 0 for i in range(3)):
        raise ValueError('包围盒退化（某轴长度为 0）：lo=%s hi=%s' % (tuple(b_lo), tuple(b_hi)))
    return b_lo, b_hi, box


def corners(lo, hi):
    return [Vector((lo.x if not (k & 1) else hi.x,
                    lo.y if not (k & 2) else hi.y,
                    lo.z if not (k & 4) else hi.z)) for k in range(8)]


# ── 场景搭建 ──────────────────────────────────────────────────────────────
def import_glb(glb_path):
    before = set(bpy.data.objects)
    bpy.ops.import_scene.gltf(filepath=glb_path)
    new = [o for o in bpy.context.scene.objects if o not in before]
    meshes = [o for o in new if o.type == 'MESH']
    if not meshes:
        raise RuntimeError('import_scene.gltf 未导入任何 mesh：%s' % glb_path)
    return meshes


def add_sun(name, energy, color, direction, shadow=True):
    """direction = 从太阳射向场景的方向（单位向量）。"""
    data = bpy.data.lights.new(name, 'SUN')
    data.energy = energy
    data.color = color
    data.angle = math.radians(3.0)
    data.use_shadow = shadow
    obj = bpy.data.objects.new(name, data)
    bpy.context.collection.objects.link(obj)
    obj.location = -Vector(direction).normalized() * 10.0
    obj.rotation_mode = 'QUATERNION'
    obj.rotation_quaternion = Vector(direction).normalized().to_track_quat('-Z', 'Y')
    return obj


def add_ground(z, size):
    """浅灰漫反射地面（给阴影一个落点，构图更易判读）。尺寸随包围盒缩放。"""
    me = bpy.data.meshes.new('GroundMesh')
    me.from_pydata([(-1, -1, 0), (1, -1, 0), (1, 1, 0), (-1, 1, 0)], [], [(0, 1, 2, 3)])
    me.update()
    obj = bpy.data.objects.new('PreviewGround', me)
    bpy.context.collection.objects.link(obj)
    obj.location = (0, 0, z)
    obj.scale = (size, size, 1)
    mat = bpy.data.materials.new('PreviewGroundMat')
    mat.use_nodes = True
    b = mat.node_tree.nodes.get('Principled BSDF')
    # 反照率压到 0.22：地面在三点布光下若用 0.35+ 会亮到接近纯白，
    # 浅色资产（如 concrete 建筑）就"贴"在地面上失去轮廓。
    b.inputs['Base Color'].default_value = (0.22, 0.23, 0.24, 1.0)
    b.inputs['Roughness'].default_value = 0.92
    me.materials.append(mat)
    return obj


def setup_world(strength=0.40):
    """渐变世界背景：Incoming.Z → MapRange → ColorRamp（地平线暖灰 → 天顶冷蓝）。"""
    world = bpy.data.worlds.get('PreviewWorld') or bpy.data.worlds.new('PreviewWorld')
    bpy.context.scene.world = world
    world.use_nodes = True
    nt = world.node_tree
    nt.nodes.clear()
    out = nt.nodes.new('ShaderNodeOutputWorld')
    bg = nt.nodes.new('ShaderNodeBackground')
    bg.inputs['Strength'].default_value = strength
    geo = nt.nodes.new('ShaderNodeNewGeometry')
    sep = nt.nodes.new('ShaderNodeSeparateXYZ')
    mr = nt.nodes.new('ShaderNodeMapRange')
    mr.inputs['From Min'].default_value = -0.45
    mr.inputs['From Max'].default_value = 0.85
    ramp = nt.nodes.new('ShaderNodeValToRGB')
    ramp.color_ramp.elements[0].position = 0.0
    ramp.color_ramp.elements[0].color = (0.55, 0.53, 0.50, 1.0)   # 地平线：暖灰
    ramp.color_ramp.elements[1].position = 1.0
    ramp.color_ramp.elements[1].color = (0.20, 0.31, 0.47, 1.0)   # 天顶：冷蓝
    mid = ramp.color_ramp.elements.new(0.32)
    mid.color = (0.42, 0.47, 0.55, 1.0)
    nt.links.new(geo.outputs['Incoming'], sep.inputs['Vector'])
    nt.links.new(sep.outputs['Z'], mr.inputs['Value'])
    nt.links.new(mr.outputs['Result'], ramp.inputs['Fac'])
    nt.links.new(ramp.outputs['Color'], bg.inputs['Color'])
    nt.links.new(bg.outputs['Background'], out.inputs['Surface'])
    return world


def place_camera(center, radius, box_corners, view, scene):
    """相机 + 自适应距离：逐角点解出「最近仍完整入画」的距离，再乘留白。"""
    d_vec = VIEWS[view]
    quat = d_vec.to_track_quat('Z', 'Y')        # 相机局部 +Z 指向 d_vec ⇒ -Z 为视轴
    right = quat @ Vector((1, 0, 0))
    up = quat @ Vector((0, 1, 0))
    fwd = -d_vec

    tan_h = (SENSOR * 0.5) / LENS
    aspect = scene.render.resolution_y / float(scene.render.resolution_x)
    tan_v = tan_h * aspect
    th, tv = tan_h / MARGIN, tan_v / MARGIN

    dist = 0.0
    for c in box_corners:
        rel = c - center
        along = rel.dot(fwd)                    # 沿视轴的偏心（可正可负）
        dist = max(dist,
                   abs(rel.dot(right)) / th - along,
                   abs(rel.dot(up)) / tv - along)
    dist = max(dist, radius * 0.05, 1e-4)

    cam_data = bpy.data.cameras.new('PreviewCam')
    cam_data.type = 'PERSP'
    cam_data.lens = LENS
    cam_data.sensor_fit = 'HORIZONTAL'
    cam_data.sensor_width = SENSOR
    cam_data.clip_start = max(dist * 0.001, 1e-5)
    cam_data.clip_end = dist * 20.0 + radius * 40.0
    cam = bpy.data.objects.new('PreviewCam', cam_data)
    bpy.context.collection.objects.link(cam)
    cam.location = center + d_vec * dist
    cam.rotation_mode = 'QUATERNION'
    cam.rotation_quaternion = quat
    scene.camera = cam
    return cam, dist


# ── 引擎 ─────────────────────────────────────────────────────────────────
def setup_engine(scene, want, samples):
    """优先 EEVEE（Blender 5.2.1 的 engine id 是 BLENDER_EEVEE；EEVEE_NEXT 只在 4.2
    的过渡版本里出现过，本机 5.2.1 已回并为 BLENDER_EEVEE）⇒ 回落 Cycles CPU 低采样。"""
    attempts = []
    if want in (None, 'auto', 'eevee'):
        attempts.append('BLENDER_EEVEE')
    if want in (None, 'auto', 'cycles'):
        attempts.append('CYCLES')
    for eng in attempts:
        try:
            scene.render.engine = eng
        except (TypeError, AttributeError) as exc:
            print('[render_preview] 引擎 %s 不可用（%s）' % (eng, exc), file=sys.stderr)
            continue
        if eng == 'CYCLES':
            scene.cycles.device = 'CPU'
            scene.cycles.samples = samples
            scene.cycles.use_denoising = True
        else:
            ee = getattr(scene, 'eevee', None)
            if ee is not None:
                if hasattr(ee, 'taa_render_samples'):
                    ee.taa_render_samples = samples
                if hasattr(ee, 'use_shadows'):
                    ee.use_shadows = True
                # 屏幕空间 raytracing（SSGI/反射）在本机 **软件 OpenGL** 上极慢
                # （1280x960 单帧 ~10 s → 单图 2.5 min），且对资产质检收益很低：关掉。
                if hasattr(ee, 'use_raytracing'):
                    ee.use_raytracing = False
        print('[render_preview] 渲染引擎 = %s（采样 %d）' % (eng, samples), flush=True)
        return eng
    raise RuntimeError('没有可用渲染引擎（试过 %s）' % ', '.join(attempts))


# ── CLI ──────────────────────────────────────────────────────────────────
def parse_cli(argv):
    opts = dict(view='iso', width=1280, height=960, engine='auto',
                samples=32, ground=True, view_transform='Standard', glb=None, out=None)
    i = 0
    while i < len(argv):
        a = argv[i]
        if a == '--view':
            opts['view'] = argv[i + 1]; i += 2
        elif a.startswith('--view='):
            opts['view'] = a.split('=', 1)[1]; i += 1
        elif a == '--width':
            opts['width'] = int(argv[i + 1]); i += 2
        elif a.startswith('--width='):
            opts['width'] = int(a.split('=', 1)[1]); i += 1
        elif a == '--height':
            opts['height'] = int(argv[i + 1]); i += 2
        elif a.startswith('--height='):
            opts['height'] = int(a.split('=', 1)[1]); i += 1
        elif a == '--engine':
            opts['engine'] = argv[i + 1]; i += 2
        elif a.startswith('--engine='):
            opts['engine'] = a.split('=', 1)[1]; i += 1
        elif a == '--samples':
            opts['samples'] = int(argv[i + 1]); i += 2
        elif a.startswith('--samples='):
            opts['samples'] = int(a.split('=', 1)[1]); i += 1
        elif a == '--view-transform':
            opts['view_transform'] = argv[i + 1]; i += 2
        elif a.startswith('--view-transform='):
            opts['view_transform'] = a.split('=', 1)[1]; i += 1
        elif a == '--no-ground':
            opts['ground'] = False; i += 1
        elif a in ('-h', '--help'):
            print(__doc__)
            raise SystemExit(0)
        elif opts['glb'] is None:
            opts['glb'] = a; i += 1
        elif opts['out'] is None:
            opts['out'] = a; i += 1
        else:
            raise ValueError('多余实参：%s' % a)
    if opts['glb'] is None or opts['out'] is None:
        raise ValueError('用法：render_preview.py <in.glb> <out.png> [选项]')
    if opts['view'] not in VIEWS:
        raise ValueError('--view 只能是 %s' % '/'.join(sorted(VIEWS)))
    if opts['width'] < 16 or opts['height'] < 16:
        raise ValueError('--width/--height 至少 16')
    return opts


def clear_scene():
    for obj in list(bpy.data.objects):
        bpy.data.objects.remove(obj, do_unlink=True)
    for coll in (bpy.data.meshes, bpy.data.materials, bpy.data.lights, bpy.data.cameras):
        for it in list(coll):
            coll.remove(it)


def run(argv):
    opts = parse_cli(argv)
    glb = os.path.abspath(opts['glb'])
    out = os.path.abspath(opts['out'])
    if not os.path.isfile(glb):
        raise ValueError('找不到 GLB：%s' % glb)
    out_dir = os.path.dirname(out)
    if out_dir:
        os.makedirs(out_dir, exist_ok=True)

    # 先量测（失败早于导入，不留半截场景）
    lo, hi, box = measure(glb)
    size = hi - lo
    center = (lo + hi) * 0.5
    radius = size.length * 0.5
    ctrs = corners(lo, hi)

    clear_scene()
    meshes = import_glb(glb)
    tri = sum(len(o.data.polygons) for o in meshes)

    scene = bpy.context.scene
    scene.render.resolution_x = opts['width']
    scene.render.resolution_y = opts['height']
    scene.render.resolution_percentage = 100
    scene.render.image_settings.file_format = 'PNG'
    scene.render.image_settings.color_mode = 'RGBA'
    scene.render.film_transparent = False
    scene.render.filepath = out
    if opts['view_transform']:
        try:
            scene.view_settings.view_transform = opts['view_transform']
        except TypeError:
            print('[render_preview] 未知 view_transform %s，沿用默认 %s'
                  % (opts['view_transform'], scene.view_settings.view_transform), file=sys.stderr)
    setup_world()
    # 三点布光（全部 SUN ⇒ 尺度无关）
    # 能量取「Standard 色彩变换下不过曝」的上限：Lambert 面正对太阳的辐射 ≈ E/π，
    # E=2.2 ⇒ 0.70，白色建材刚好留出高光层次；三灯合计 ~4.2 略高于 AgX 常用值，
    # 是因为 Standard 没有 AgX 的高光滚降，压得太低会显得灰蒙蒙。
    # 顶视时关阴影：投影在俯视画面里会盖住 2~3 倍于物体本身的地面面积，
    # 把主体埋掉（实测 city_hall 顶视：屋顶只占画幅 20%，影子占 60%）。
    cast = opts['view'] != 'top'
    add_sun('Key', 2.2, (1.00, 0.96, 0.90), Vector((-0.55, -0.80, -1.00)).normalized(), cast)
    add_sun('Fill', 0.6, (0.72, 0.82, 1.00), Vector((0.95, -0.35, -0.25)).normalized(), cast)
    add_sun('Rim', 1.4, (0.95, 0.98, 1.00), Vector((0.35, 0.90, -0.45)).normalized(), cast)
    if opts['ground']:
        add_ground(lo.z, max(radius * 24.0, size.x, size.y) * 1.6)

    setup_engine(scene, opts['engine'], opts['samples'])
    _cam, dist = place_camera(center, radius, ctrs, opts['view'], scene)
    bpy.context.view_layer.update()
    bpy.ops.render.render(write_still=True)

    if not os.path.isfile(out):
        raise RuntimeError('渲染结束但未产出文件：%s' % out)
    warn = box['bad_nodes']
    if warn:
        print('[render_preview] ⚠ 节点 scale ≠ 1（尺寸挂在节点变换上，预览可能与游戏内不一致）：%s'
              % ', '.join(warn), file=sys.stderr)
    print('[render_preview] 视角=%s 网格=%d 面=%d 相机距=%.4f 世界单位(%.2f m)'
          % (opts['view'], len(meshes), tri, dist, dist * METERS_PER_UNIT), flush=True)
    # 摘要按 **verify_glb_aabb 的口径**（glTF 系 X/Y/Z，Y = 高度）打印，
    # 这样预览图的这行数字可以和 verify_glb_aabb.py 的「包围盒 X/Y/Z」逐位对读。
    # ⚠ 千万不要用 Blender 系（Z-up）的 size 打：Blender 的 Y 是**进深**不是高度，
    #  直接打会变成「宽×深×高」，与判据表对不上（city_hall 会打成 1.440x0.840x2.000）。
    gs = [box['hi'][i] - box['lo'][i] for i in range(3)]
    print('预览渲染完成 %s 包围盒=%.3fx%.3fx%.3f'
          % (out, gs[0], gs[1], gs[2]), flush=True)
    return 0


def main():
    argv = sys.argv[sys.argv.index('--') + 1:] if '--' in sys.argv else sys.argv[1:]
    try:
        code = run(argv)
    except SystemExit as exc:
        code = exc.code if isinstance(exc.code, int) else 0
    except BaseException as exc:                                # noqa: BLE001
        traceback.print_exc()
        print('render_preview 失败：%s' % exc, file=sys.stderr)
        code = 1
    sys.stdout.flush()
    sys.stderr.flush()
    os._exit(code)          # 保证进程退出码真的传出去


if __name__ == '__main__':
    main()
