/**
 * boxFacesUV 断言测试（批次 37 · P0 验收判据 §6.2「几何零回归」）。
 *
 * 项目无测试框架（不引入新依赖），故用 node 原生断言 + `--experimental-strip-types`
 * 单文件直跑：
 *     node --experimental-strip-types src/engine3d/__test__/boxFacesUV.assert.ts
 *
 * 覆盖：
 *   ① 零回归：缺省 uv 时 boxFacesUV 的 position / normal / uv 与 boxFaces **逐位相等**；
 *   ② 各向异性 = 1：给定 uv 时，任意 w/h/d 下横向与纵向 px/m 比值 == 1；
 *   ③ 面向尺寸现算：A 面用 d、B 面用 w、top 面用 d（不得硬编码米数）；
 *   ④ 错误纪律：tileU/tileV <= 0 抛错（不静默退化）。
 */

import * as THREE from 'three';
import { boxFaces, boxFacesUV, type BoxUVSpec } from '../geoMerge.ts';

let failed = 0;
let passed = 0;

function ok(cond: boolean, msg: string): void {
  if (cond) {
    passed++;
  } else {
    failed++;
    console.error(`  ✗ FAIL: ${msg}`);
  }
}

function attrArray(geo: THREE.BufferGeometry, name: 'position' | 'normal' | 'uv'): number[] {
  return Array.from((geo.getAttribute(name) as THREE.BufferAttribute).array as ArrayLike<number>);
}

function sameArray(a: number[], b: number[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;  // 逐位（严格 ===）相等
  return true;
}

/** 相对容差比较（uv 存在 Float32BufferAttribute，跨度值必有 f32 舍入）。 */
function near(a: number, b: number, rel = 1e-6): boolean {
  return Math.abs(a - b) <= rel * Math.max(1, Math.abs(b));
}

const CLASSES: Array<'A' | 'B' | 'top'> = ['A', 'B', 'top'];
const DIMS: Array<[number, number, number]> = [
  [1.7, 1.0, 1.35],   // slab / house 主体（世界单位）
  [1.7, 1.1, 1.1],    // tower 裙楼
  [1.36, 4.4, 1.08],  // tower 塔身
  [2.04, 0.147, 1.5], // shed 大跨低层
  [0.9, 0.63, 0.9],   // pavilion
];

// ── ① 零回归：缺省 uv ⇒ position / normal / uv 逐位一致 ─────────────
console.log('[1] 零回归：boxFacesUV(缺省 uv) vs boxFaces');
for (const [w, h, d] of DIMS) {
  for (const cls of CLASSES) {
    for (const [x, y, z] of [[0, 0, 0], [1.5, 0.5, -2.5]] as Array<[number, number, number]>) {
      const a = boxFaces(w, h, d, x, y, z, cls);
      const b = boxFacesUV(w, h, d, x, y, z, cls);
      ok(sameArray(attrArray(a, 'position'), attrArray(b, 'position')),
        `position 不等（${w}x${h}x${d} cls=${cls} @${x},${y},${z}）`);
      ok(sameArray(attrArray(a, 'normal'), attrArray(b, 'normal')),
        `normal 不等（${w}x${h}x${d} cls=${cls}）`);
      ok(sameArray(attrArray(a, 'uv'), attrArray(b, 'uv')),
        `uv 不等（${w}x${h}x${d} cls=${cls}）`);
      a.dispose();
      b.dispose();
    }
  }
}
console.log(`  · 断言 ${passed} 通过 / ${failed} 失败`);

// ── ② 各向异性 = 1：横向 px/m == 纵向 px/m ──────────────────────
// 纹素密度 = 贴图像素数 × UV 跨度 / 面物理尺寸；
// 不变式 tileU/tileV === texW/texH 成立时，横向与纵向密度必须相等。
console.log('[2] 各向异性 = 1（tileU/tileV === texW/texH）');
const TILES: Array<{ spec: BoxUVSpec; texW: number; texH: number; name: string }> = [
  // 立面组必须与 components/virtualCity/texScale.ts::FACADE_TILE 同步
  // （批次 37 返工：1024x2048 → **512x1024**，bundle 体量降 4 倍，tileMeters 6m x 12m 不动）。
  { spec: { tileU: 0.6, tileV: 1.2 }, texW: 512, texH: 1024, name: '立面 6m x 12m (512x1024)' },
  { spec: { tileU: 1.0, tileV: 1.0 }, texW: 512, texH: 512, name: '屋顶 10m x 10m' },
  { spec: { tileU: 0.3, tileV: 0.6 }, texW: 256, texH: 512, name: '半档立面 3m x 6m' },
  { spec: { tileU: 1.2, tileV: 2.4 }, texW: 1024, texH: 2048, name: '高档立面 12m x 24m' },
];
for (const { spec, texW, texH, name } of TILES) {
  for (const [w, h, d] of DIMS) {
    for (const cls of CLASSES) {
      const g = boxFacesUV(w, h, d, 0, 0, 0, cls, spec);
      const uv = g.getAttribute('uv') as THREE.BufferAttribute;
      const uMax = Math.max(...Array.from({ length: uv.count }, (_, i) => uv.getX(i)));
      const vMax = Math.max(...Array.from({ length: uv.count }, (_, i) => uv.getY(i)));
      const faceW = cls === 'A' ? d : w;         // A(±X) 面宽 = d / B(±Z) 与 top 面宽 = w
      const faceH = cls === 'top' ? d : h;       // A/B 面高 = h / top 面深 = d
      const uSpan = cls === 'A' ? d : w;
      const vSpan = cls === 'top' ? d : h;
      ok(near(uMax, uSpan / spec.tileU), `u 跨度应为 面宽/tileU（${name} ${cls}）`);
      ok(near(vMax, vSpan / spec.tileV), `v 跨度应为 面高/tileV（${name} ${cls}）`);
      const pxPerM = texW * (uMax / faceW);
      const pyPerM = texH * (vMax / faceH);
      ok(near(pxPerM / pyPerM, 1),
        `各向异性 != 1（${name} ${w}x${h}x${d} cls=${cls}）：${(pxPerM / pyPerM).toFixed(6)}`);
      g.dispose();
    }
  }
}
console.log(`  · 断言累计 ${passed} 通过 / ${failed} 失败`);

// ── ③ snapV：整周期吸附（可选，默认关闭）────────────────────
console.log('[3] snapV 整周期吸附');
{
  const g = boxFacesUV(1.7, 4.4, 1.35, 0, 0, 0, 'B', { tileU: 0.6, tileV: 1.2, snapV: 1 });
  const uv = g.getAttribute('uv') as THREE.BufferAttribute;
  const vMax = Math.max(...Array.from({ length: uv.count }, (_, i) => uv.getY(i)));
  ok(near(vMax, Math.floor(4.4 / 1.2)), `snapV=1 应吸附到 3 个整周期，实际 ${vMax}`);
  g.dispose();
  const g2 = boxFacesUV(1.7, 0.147, 1.35, 0, 0, 0, 'B', { tileU: 0.6, tileV: 1.2, snapV: 1 });
  const uv2 = g2.getAttribute('uv') as THREE.BufferAttribute;
  const v2 = Math.max(...Array.from({ length: uv2.count }, (_, i) => uv2.getY(i)));
  ok(v2 === 1, `不足一周期时应抬到 1（max(1, floor(..))），实际 ${v2}`);
  g2.dispose();
}

// ── ④ 错误纪律：tileU/tileV 非正有限数 ⇒ 抛错 ──────────────────
console.log('[4] 错误纪律：非法 tile 尺寸抛错');
for (const bad of [{ tileU: 0, tileV: 1 }, { tileU: 1, tileV: -1 }, { tileU: NaN, tileV: 1 }]) {
  let threw = false;
  try {
    boxFacesUV(1, 1, 1, 0, 0, 0, 'B', bad);
  } catch {
    threw = true;
  }
  ok(threw, `tileU=${bad.tileU} / tileV=${bad.tileV} 应当抛错（禁止静默退化）`);
}

console.log(`\n结果：${passed} 通过 / ${failed} 失败`);
if (failed > 0) throw new Error(`boxFacesUV 断言测试失败 ${failed} 项`);
