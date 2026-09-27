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
