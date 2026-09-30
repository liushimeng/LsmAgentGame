/**
 * engine3d/geoMerge — 通用几何合并工具（批次 28 二轮 · DC 攻坚）。
 *
 * 背景：renderer.info 实测 draw calls ≈ mesh 数 ≈ geometry 数（4200–4800），
 * 瓶颈是「件件独立 mesh」的静物（楼栋细节 / 车辆附件 / 市政设施）。本模块提供
 * 两种**几何全等**的合并（不改三角形、不改 UV、不改顶点位置）：
 *
 *   1. mergeGrouped —— 多部件按材质索引分桶合并为「单 mesh 多 group」：
 *      DC = 材质数（同材质部件连续排布后收敛为一个 group）。
 *      适合：多贴图立面 box（侧A/侧B/顶面）+ 坡屋顶 + 玻璃 + 发光件这类
 *      「必须保留各自材质」的部件集合。
 *   2. mergeParts —— 多部件合并为「单 mesh 单材质」+ 可选顶点色：
 *      部件级颜色经顶点色 100% 保留（颜色逐部件一致，无渐变无失真），
 *      代价是粗糙度/金属度统一为单一值（调用方选定，视觉取舍需在交付报告中列出）。
 *      适合：女儿墙/空调外机/栏杆/门框/烟囱这类纯色点缀件。
 *
 * 约束：
 *   - 只依赖 three（引擎层禁止 import 游戏私有模块，CLAUDE.md §2.1 硬约束 5）。
 *   - 输入 geometry 若带 index 先 toNonIndexed（合并实现统一按顶点拼接；
 *     部件均为几十顶点级小件，非索引化顶点增量可忽略）。
 *   - 调用方传入的 geometry 不被本模块 dispose；内部临时副本用后即弃。
 *   - 合并产物为非索引几何：group 的 start/count 直接以顶点计数
 *     （three 对非索引几何按顶点范围 drawArrays）。
 */

import * as THREE from 'three';

/** 合并部件：局部几何 + 可选平移 / 绕 Z 旋转 / 任意矩阵 / 顶点色。 */
export interface MergePart {
  /** 部件几何（局部坐标；模块内部 clone/toNonIndexed，调用方保留所有权）。 */
  geo: THREE.BufferGeometry;
  /** 平移（部件几何中心偏移；等价原 JSX 的 position）。 */
  x?: number;
  y?: number;
  z?: number;
  /** 绕 Z 轴旋转（rad；先旋转再平移）。 */
  rotZ?: number;
  /**
   * 任意变换矩阵（compose 好的 TRS；优先于 x/y/z/rotZ）。
   * 覆盖原 JSX 嵌套 group 旋转的部件（如锯齿顶采光带的两次旋转）。
   */
  matrix?: THREE.Matrix4;
  /** 顶点色（mergeParts 逐部件颜色；缺省白）。 */
  color?: string;
}

/** 带材质索引的部件（mergeGrouped 用）。 */
export interface GroupedMergePart extends MergePart {
  /** 目标材质数组下标（同索引部件合并进同一 group）。 */
  mat: number;
}

/** BoxGeometry 非索引化后的面顶点切片：面序 [+X,-X,+Y,-Y,+Z,-Z]，每面 6 顶点。 */
const BOX_FACE_SLICES: Record<'A' | 'B' | 'top', [number, number]> = {
  A: [0, 12],    // ±X（侧 A）
  top: [12, 24], // ±Y（顶 + 底）
  B: [24, 36],   // ±Z（侧 B）
};

/** 非索引几何按顶点区间切三属性（position/normal/uv；缺 uv 补 0）。 */
function sliceVerts(src: THREE.BufferGeometry, start: number, end: number): THREE.BufferGeometry {
  const pos = src.getAttribute('position');
  const nor = src.getAttribute('normal');
  const uv = src.getAttribute('uv');
  const n = end - start;
  const out = new THREE.BufferGeometry();
  const cp = pos.array as Float32Array;
  const cn = nor ? (nor.array as Float32Array) : null;
  const cu = uv ? (uv.array as Float32Array) : null;
  out.setAttribute('position', new THREE.Float32BufferAttribute(cp.slice(start * 3, end * 3), 3));
  out.setAttribute('normal', new THREE.Float32BufferAttribute(
    cn ? cn.slice(start * 3, end * 3) : new Float32Array(n * 3), 3));
  const uvs = new Float32Array(n * 2);
  if (cu) for (let i = start; i < end; i++) {
    uvs[(i - start) * 2] = cu[i * 2];
    uvs[(i - start) * 2 + 1] = cu[i * 2 + 1];
  }
  out.setAttribute('uv', new THREE.Float32BufferAttribute(uvs, 2));
  return out;
}

/**
 * 单个 box 的指定面类切片（几何全等于 BoxGeometry 对应面组）：
 *   A = ±X（侧 A）/ top = ±Y（顶+底）/ B = ±Z（侧 B），
 * 与 building_shapes 原 useGroupBoxGeometry 的三组面序一致。
 */
export function boxFaces(
  w: number, h: number, d: number,
  x: number, y: number, z: number,
  cls: 'A' | 'B' | 'top',
): THREE.BufferGeometry {
  const g = new THREE.BoxGeometry(w, h, d).toNonIndexed();
  const [s, e] = BOX_FACE_SLICES[cls];
  const sliced = sliceVerts(g, s, e);
  g.dispose();
  sliced.translate(x, y, z);
  return sliced;
}

// ── 批次 37 · P0：盒面物理尺寸 UV 投影（消除各向异性变形）────────────

/**
 * 盒面物理尺寸 UV 投影参数（单位 = **世界单位**，与 boxFaces 的 w/h/d 同标尺）。
 *
 * 背景：three 的 BoxGeometry 每面 UV 恒为 0..1，等价于「一个贴图周期 = 整个面」，
 * 同一张 512×1024 立面图被拉伸到 10 m 高的裙楼与 44 m 高的塔身时，
 * **两个体块的变形方向相反**（裙楼横向拉伸 3.40× / 塔身纵向拉伸 1.61×）。
 * 传入本参数后 UV 取值改为 `面尺寸 ÷ 周期尺寸`（可 > 1，配合 `wrap: 'repeat'`），
 * 只要 `tileU / tileV === texW / texH`，任意尺寸的面上横向/纵向纹素密度恒相等。
 */
export interface BoxUVSpec {
  /** 一个 U 周期覆盖的物理宽度（世界单位）。 */
  tileU: number;
  /** 一个 V 周期覆盖的物理高度（世界单位）。 */
  tileV: number;
  /**
   * V 方向「整周期吸附」步长（可选，单位 = UV 周期数）。
   * 贴图内容含楼层语义时（如立面开间图 1 周期 = 4 层），吸附可避免楼层在面顶被腰斩：
   * `v = max(1, floor(v / snapV)) * snapV`。**代价**是纵向纹素密度不再恒定，
   * 故缺省不开启（各向异性 = 1 的严格性优先）。
   */
  snapV?: number;
}

/** 面类的物理面宽/面高（世界单位）：A(±X) = d×h / B(±Z) = w×h / top(±Y) = w×d。 */
function faceSpan(
  w: number, h: number, d: number, cls: 'A' | 'B' | 'top',
): { fw: number; fh: number } {
  switch (cls) {
    case 'A': return { fw: d, fh: h };
    case 'B': return { fw: w, fh: h };
    case 'top': return { fw: w, fh: d };
  }
}

/**
 * 就地覆写盒面几何的 uv 为物理尺寸投影（原始 0..1 UV × 周期跨度）。
 * 只改 uv 属性，position / normal 逐位不动（切片仍走 sliceVerts）。
 */
function applyBoxUV(
  geo: THREE.BufferGeometry,
  w: number, h: number, d: number,
  cls: 'A' | 'B' | 'top',
  spec: BoxUVSpec,
): void {
  const { tileU, tileV, snapV } = spec;
  const { fw, fh } = faceSpan(w, h, d, cls);
  // 登记表写错（0 / 负数 / NaN）必须立刻炸，绝不静默退化成 0..1（渲染出一片拉伸贴图）
  if (!Number.isFinite(tileU) || tileU <= 0 || !Number.isFinite(tileV) || tileV <= 0) {
    throw new Error(
      `boxFacesUV: 非法 tile 尺寸 tileU=${tileU} / tileV=${tileV}（须为正有限数）；` +
      `面类 ${cls}，面尺寸 ${fw}×${fh} 世界单位。` +
      '请检查资产登记表（components/virtualCity/texScale.ts）的 tileMeters 与像素尺寸配比。',
    );
  }
  const spanU = fw / tileU;
  let spanV = fh / tileV;
  if (snapV !== undefined && Number.isFinite(snapV) && snapV >= 1) {
    spanV = Math.max(1, Math.floor(spanV / snapV)) * snapV;
  }
  const attr = geo.getAttribute('uv') as THREE.BufferAttribute | undefined;
  if (!attr) {
    throw new Error('boxFacesUV: 几何缺 uv 属性（boxFaces 产物恒有，属内部契约破坏）');
  }
  for (let i = 0; i < attr.count; i++) {
    attr.setXY(i, attr.getX(i) * spanU, attr.getY(i) * spanV);
  }
  attr.needsUpdate = true;
}

/**
 * 物理尺寸 UV 投影的盒面切片（position / normal 与 `boxFaces` **逐位一致**，仅 uv 不同）。
 *
 * 面向 ↔ tile 对应：
 *   `A`(±X，面宽 = d，面高 = h) / `B`(±Z，面宽 = w，面高 = h) / `top`(±Y，面宽 = w，面深 = d)
 *
 * @param uv 缺省时行为与 `boxFaces` **完全相同**（UV 恒 0..1）—— 非立面调用方零回归。
 *          传入时 UV 取值为 `面宽 / tileU` 与 `面高 / tileV`（**不是**归一化到 0..1，
 *          故 v 可以是 > 1 的多周期取值），调用方必须配套 `wrap: 'repeat'`。
 * @throws {Error} `uv.tileU` / `uv.tileV` 非正有限数时抛出（错误处理纪律，不静默退化）。
 */
export function boxFacesUV(
  w: number, h: number, d: number,
  x: number, y: number, z: number,
  cls: 'A' | 'B' | 'top',
  uv?: BoxUVSpec,
): THREE.BufferGeometry {
  const geo = boxFaces(w, h, d, x, y, z, cls);
  if (uv) applyBoxUV(geo, w, h, d, cls, uv);
  return geo;
}

/** 部件几何就位（clone → 变换），返回非索引副本。内部使用。 */
function placedGeometry(part: MergePart): THREE.BufferGeometry {
  const g = part.geo.index ? part.geo.toNonIndexed() : part.geo.clone();
  if (part.matrix) g.applyMatrix4(part.matrix);
  else {
    if (part.rotZ) g.rotateZ(part.rotZ);
    g.translate(part.x ?? 0, part.y ?? 0, part.z ?? 0);
  }
  return g;
}

/** 把一个部件的三属性（+可选颜色）追加大数组。内部使用。 */
function appendPart(
  g: THREE.BufferGeometry,
  part: MergePart,
  positions: number[], normals: number[], uvs: number[], colors: number[] | null,
  tmpColor: THREE.Color,
): void {
  const pos = g.getAttribute('position');
  const nor = g.getAttribute('normal');
  const uv = g.getAttribute('uv');
  tmpColor.set(part.color ?? '#ffffff');
  for (let i = 0; i < pos.count; i++) {
    positions.push(pos.getX(i), pos.getY(i), pos.getZ(i));
    if (nor) normals.push(nor.getX(i), nor.getY(i), nor.getZ(i));
    else normals.push(0, 1, 0);
    if (uv) uvs.push(uv.getX(i), uv.getY(i));
    else uvs.push(0, 0);
    if (colors) colors.push(tmpColor.r, tmpColor.g, tmpColor.b);
  }
}

/**
 * 按材质索引分桶合并：同 mat 部件顶点连续排布 → 每 mat 一个 group。
 * 返回非索引几何 + groups（start/count 为顶点数），配合
 * `<mesh geometry={geo} material={材料数组} />` 得到 DC = 材质数。
 */
export function mergeGrouped(parts: GroupedMergePart[]): THREE.BufferGeometry {
  // 桶：mat → 部件列表（保持部件传入次序，材质桶按 mat 升序输出）
  const buckets = new Map<number, MergePart[]>();
  for (const p of parts) {
    const list = buckets.get(p.mat);
    if (list) list.push(p);
    else buckets.set(p.mat, [p]);
  }
  const positions: number[] = [];
  const normals: number[] = [];
  const uvs: number[] = [];
  const tmpColor = new THREE.Color();
  const out = new THREE.BufferGeometry();
  const mats = [...buckets.keys()].sort((a, b) => a - b);
  for (const mat of mats) {
    const start = positions.length / 3;
    for (const part of buckets.get(mat)!) {
      const g = placedGeometry(part);
      appendPart(g, part, positions, normals, uvs, null, tmpColor);
      g.dispose();
    }
    const count = positions.length / 3 - start;
    if (count > 0) out.addGroup(start, count, mat);
  }
  out.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
  out.setAttribute('normal', new THREE.Float32BufferAttribute(normals, 3));
  out.setAttribute('uv', new THREE.Float32BufferAttribute(uvs, 2));
  return out;
}

/**
 * 单材质合并：全部部件拼为一个无 group 几何；任一部件带 color 时输出
 * 顶点色属性（配合材质 `vertexColors: true` + 白色基色，逐部件颜色全保留）。
 */
export function mergeParts(parts: MergePart[]): THREE.BufferGeometry {
  const positions: number[] = [];
  const normals: number[] = [];
  const uvs: number[] = [];
  const colors: number[] | null = parts.some((p) => p.color) ? [] : null;
  const tmpColor = new THREE.Color();
  for (const part of parts) {
    const g = placedGeometry(part);
    appendPart(g, part, positions, normals, uvs, colors, tmpColor);
    g.dispose();
  }
  const out = new THREE.BufferGeometry();
  out.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
  out.setAttribute('normal', new THREE.Float32BufferAttribute(normals, 3));
  out.setAttribute('uv', new THREE.Float32BufferAttribute(uvs, 2));
  if (colors) out.setAttribute('color', new THREE.Float32BufferAttribute(colors, 3));
  return out;
}

/** 便捷工厂：box 部件（等价 `<mesh position><boxGeometry args={[w,h,d]}/>`）。 */
export function boxPart(w: number, h: number, d: number, x = 0, y = 0, z = 0, color?: string): MergePart {
  return { geo: new THREE.BoxGeometry(w, h, d), x, y, z, color };
}

/** 便捷工厂：竖直圆柱部件（等价无旋转 `<cylinderGeometry args={[rt,rb,h,seg]}/>`）。 */
export function cylPart(rt: number, rb: number, h: number, seg: number, x = 0, y = 0, z = 0, color?: string): MergePart {
  return { geo: new THREE.CylinderGeometry(rt, rb, h, seg), x, y, z, color };
}
