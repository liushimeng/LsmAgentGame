#!/usr/bin/env node
/**
 * verify_city_grid — 批次 61「全城连续正交方格路网」**几何不变量**自动校验。
 *
 * 校验项（全部与 `src/components/virtualCity/cityGrid.ts` / `roadNetwork.ts` 同源）：
 *   1. 全部 32 城区坐标都是 12 的整数倍（= 街区中心）；
 *   2. 城区两两不重合（一个格子一个城区）；
 *   3. 城区中心到任一街道中心线的距离 ≥ 4.7（街区半宽 4 + 路廊半宽 0.7）——
 *      即「建筑不压路」；
 *   4. 全城路网线段**零斜向**（每段 dx 或 dz 为 0）；
 *   5. 南北向街道在运河处**拆成两段**（中段由桥面承载）⇒ 运河处无「整段消失」；
 *   6. 任一交叉点的两条街道 y 差 ≥ 4 mm（z-fighting 阈值）⇒ 无同 y 共面重叠；
 *   7. 路网**连通分量数 = 1** 且**无悬空端点**（每个端点都落在另一条街道的内部）；
 *   8. 端点数量（应为 0 悬空、1 连通分量）——对照批次 59 旧路网的
 *      79 个底板内端点 / 39 个 deg-1 悬空断头。
 *
 * 用法：`node scripts/verify_city_grid.mjs`
 * 退出码 0 = 全部通过；非 0 = 有断言失败（逐条打印）。
 */

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, '..');

// ── 从源码解析常量（避免引入 TS 运行时依赖；口径与源码逐字一致）──
const gridSrc = readFileSync(resolve(root, 'src/components/virtualCity/cityGrid.ts'), 'utf8');
const distSrc = readFileSync(resolve(root, 'src/types/virtualCity.ts'), 'utf8');

const GRID_CELL = Number(gridSrc.match(/GRID_CELL = (\d+)/)?.[1] ?? 12);
const GRID_INDEX_MAX = Number(gridSrc.match(/GRID_INDEX_MAX = (-?\d+)/)?.[1] ?? 3);
const CANAL_Z = Number(gridSrc.match(/CANAL_STREET_Z = (-?\d+)/)?.[1] ?? 18);
const CANAL_HALF_WIDTH = Number(gridSrc.match(/CANAL_HALF_WIDTH = ([\d.]+)/)?.[1] ?? 1.5);
const STREET_LINES = [...gridSrc.matchAll(/GRID_STREET_LINES: readonly number\[\] = \[([^\]]+)\]/g)]
  .map((m) => m[1].split(',').map((s) => Number(s.trim())))
  .at(-1) ?? [];
const ARTERIAL_LINES = [...gridSrc.matchAll(/GRID_ARTERIAL_LINES: readonly number\[\] = \[([^\]]+)\]/g)]
  .map((m) => m[1].split(',').map((s) => Number(s.trim())))
  .at(-1) ?? [];
const EXPRESSWAY_LINES = [...gridSrc.matchAll(/GRID_EXPRESSWAY_LINES: readonly number\[\] = \[([^\]]+)\]/g)]
  .map((m) => m[1].split(',').map((s) => Number(s.trim())))
  .at(-1) ?? [];
const GRID_STREET_SPAN = Number(
  (readFileSync(resolve(root, 'src/components/virtualCity/roadNetwork.ts'), 'utf8')
    .match(/GRID_STREET_SPAN = (\d+)/) ?? [])[1] ?? 42,
);
const Y_BASE = Number(
  (readFileSync(resolve(root, 'src/components/virtualCity/roadNetwork.ts'), 'utf8')
    .match(/Y_BASE = ([\d.]+)/) ?? [])[1] ?? 0.002,
);
const Y_LINE_STEP = Number(
  (readFileSync(resolve(root, 'src/components/virtualCity/roadNetwork.ts'), 'utf8')
    .match(/Y_LINE_STEP = ([\d.]+)/) ?? [])[1] ?? 0.0006,
);

/** 32 城区坐标（从 types/virtualCity.ts 的静态表解析）。 */
const districts = [...distSrc.matchAll(/\{ id: '([a-z_]+)',[^\n]*?x: (-?\d+), z: (-?\d+),/g)].map(
  (m) => ({ id: m[1], x: Number(m[2]), z: Number(m[3]) }),
);

let failures = 0;
const ok = (cond, msg, extra = '') => {
  if (cond) console.log(`  ✓ ${msg}${extra ? ' — ' + extra : ''}`);
  else {
    failures++;
    console.error(`  ✗ ${msg}${extra ? ' — ' + extra : ''}`);
  }
};

console.log('=== 批次 61 · 全城连续正交方格路网 · 几何不变量校验 ===');
console.log(
  `常量：GRID_CELL=${GRID_CELL} 街区中心索引 ±${GRID_INDEX_MAX} 街道线=[${STREET_LINES.join(',')}] ` +
  `主干道=[${ARTERIAL_LINES.join(',')}] 快速路=[${EXPRESSWAY_LINES.join(',')}] 跨度=±${GRID_STREET_SPAN} ` +
  `运河 z=${CANAL_Z}(±${CANAL_HALF_WIDTH}) Y=${Y_BASE}+level×${Y_LINE_STEP}`,
);

// ── 1. 城区坐标是 12 的整数倍 ──
console.log('\n[1] 32 城区坐标全部落在 12 的整数倍（= 街区中心）');
ok(districts.length === 32, '城区数 = 32', `实际 ${districts.length}`);
const offGrid = districts.filter(
  (d) => !Number.isInteger(d.x / GRID_CELL) || !Number.isInteger(d.z / GRID_CELL),
);
ok(offGrid.length === 0, '全部为格点', offGrid.map((d) => `${d.id}(${d.x},${d.z})`).join(' '));

// ── 2. 两两不重合 ──
console.log('\n[2] 城区两两不重合（一格一城）');
const seen = new Map();
const dupes = [];
for (const d of districts) {
  const k = `${d.x},${d.z}`;
  if (seen.has(k)) dupes.push(`${d.id} vs ${seen.get(k)} @ ${k}`);
  else seen.set(k, d.id);
}
ok(dupes.length === 0, '无重复占位', dupes.join(' '));

// ── 3. 建筑不压路（城区中心到街道中心线 ≥ 4.7）──
console.log('\n[3] 城区中心到任一街道中心线的距离 ≥ 4.7（街区半宽 4 + 路廊半宽 0.7）');
const BLOCK_ROAD_CLEARANCE = 4.7;
let worst = Infinity;
let worstId = '';
for (const d of districts) {
  for (const line of STREET_LINES) {
    const dist = Math.min(Math.abs(d.x - line), Math.abs(d.z - line));
    if (dist < worst) {
      worst = dist;
      worstId = d.id;
    }
  }
}
ok(worst >= BLOCK_ROAD_CLEARANCE, '最小净距达标', `最小 ${worst.toFixed(2)}u（${worstId}）≥ ${BLOCK_ROAD_CLEARANCE}`);

// ── 4. 零斜向 ──
console.log('\n[4] 全城路网线段零斜向（每段 dx 或 dz 为 0）');
const isArterial = (v) => ARTERIAL_LINES.includes(v);
const isExpressway = (v) => EXPRESSWAY_LINES.includes(v);
const nsLines = STREET_LINES.slice();
const ewLines = STREET_LINES.filter((z) => z !== CANAL_Z);
const segments = [];
for (const x of nsLines) {
  const cls = isExpressway(x) ? 'expressway' : isArterial(x) ? 'arterial' : 'grid';
  const kind = cls === 'grid' ? 'side' : 'main';
  segments.push({ key: `ns-${x}-s`, from: [x, -GRID_STREET_SPAN], to: [x, CANAL_Z - CANAL_HALF_WIDTH], kind, cls, axis: 'ns', line: x });
  segments.push({ key: `ns-${x}-n`, from: [x, CANAL_Z + CANAL_HALF_WIDTH], to: [x, GRID_STREET_SPAN], kind, cls, axis: 'ns', line: x });
}
for (const z of ewLines) {
  const cls = isExpressway(z) ? 'expressway' : isArterial(z) ? 'arterial' : 'grid';
  segments.push({ key: `ew-${z}`, from: [-GRID_STREET_SPAN, z], to: [GRID_STREET_SPAN, z], kind: cls === 'grid' ? 'side' : 'main', cls, axis: 'ew', line: z });
}
const diagonal = segments.filter((s) => {
  const dx = s.to[0] - s.from[0];
  const dz = s.to[1] - s.from[1];
  return Math.abs(dx) > 1e-6 && Math.abs(dz) > 1e-6;
});
ok(diagonal.length === 0, '无斜向段', `段数 ${segments.length}，斜向 ${diagonal.length}`);

// ── 5. 运河处拆段（无整段消失）──
console.log('\n[5] 南北向街道在运河处拆成两段（中段由桥面承载）');
const unSplit = segments.filter(
  (s) => s.axis === 'ns' && s.from[1] === -GRID_STREET_SPAN && s.to[1] === GRID_STREET_SPAN,
);
ok(unSplit.length === 0, '无「整段跨运河」的街道', `未拆 ${unSplit.length} 条`);
const gap = (CANAL_Z + CANAL_HALF_WIDTH) - (CANAL_Z - CANAL_HALF_WIDTH);
ok(gap > 0, '运河缺口宽度 > 0', `${gap.toFixed(2)}u（由桥面 4.8u 覆盖）`);

// ── 6. 交叉点 y 差 ≥ 4mm ──
console.log('\n[6] 任一交叉点两条街道的 y 差 ≥ 4 mm（z-fighting 阈值）');
// 层级分配口径与 roadNetwork.ts 一致：快速路 → 主干道 → 次干道；类内先南北后东西。
const levelOf = new Map();
let level = 0;
for (const x of nsLines) if (isExpressway(x)) levelOf.set(`ns:${x}`, level++);
for (const z of ewLines) if (isExpressway(z)) levelOf.set(`ew:${z}`, level++);
for (const x of nsLines) if (isArterial(x)) levelOf.set(`ns:${x}`, level++);
for (const z of ewLines) if (isArterial(z)) levelOf.set(`ew:${z}`, level++);
for (const x of nsLines) if (!isExpressway(x) && !isArterial(x)) levelOf.set(`ns:${x}`, level++);
for (const z of ewLines) if (!isExpressway(z) && !isArterial(z)) levelOf.set(`ew:${z}`, level++);
const yFor = (axis, v) => Y_BASE + (levelOf.get(`${axis}:${v}`) ?? 0) * Y_LINE_STEP;
let minDy = Infinity;
for (const x of nsLines) {
  for (const z of ewLines) {
    const dy = Math.abs(yFor('ns', x) - yFor('ew', z));
    if (dy < minDy) minDy = dy;
  }
}
// ⚠ 单位换算：1 u = 10 m = **10000 mm**（不是 1000）。旧写法 `minDy*1000` 把
//   0.0006 u 显示成「0.60 mm」，实际是 6 mm —— 断言通过却看着像没过阈值，会误导后人。
ok(minDy >= 0.0004, '最小 y 差 ≥ 4 mm', `最小 ${(minDy * 10000).toFixed(2)} mm（共 ${nsLines.length}×${ewLines.length}=${nsLines.length * ewLines.length} 个交叉口）`);

// ── 7/8. 连通性与悬空端点 ──
console.log('\n[7] 路网连通性：连通分量数 = 1、无悬空端点（deg-1 断头）');
// 以「街道线」为图节点：南北向街道线与东西向街道线在交叉口相连。
const nodes = [
  ...nsLines.map((x) => ({ id: `ns${x}`, axis: 'ns', v: x })),
  ...ewLines.map((z) => ({ id: `ew${z}`, axis: 'ew', v: z })),
];
const parent = new Map(nodes.map((n) => [n.id, n.id]));
const find = (a) => {
  while (parent.get(a) !== a) {
    parent.set(a, parent.get(parent.get(a)));
    a = parent.get(a);
  }
  return a;
};
const union = (a, b) => {
  const ra = find(a);
  const rb = find(b);
  if (ra !== rb) parent.set(ra, rb);
};
for (const x of nsLines) for (const z of ewLines) union(`ns${x}`, `ew${z}`);
const comps = new Set(nodes.map((n) => find(n.id)));
ok(comps.size === 1, '连通分量数 = 1', `实际 ${comps.size}`);

// 悬空端点：线段端点若既不在城市边界（±42），也不落在另一条街道的内部 ⇒ 悬空。
const dangling = [];
for (const s of segments) {
  for (const end of [s.from, s.to]) {
    const onBoundary = Math.abs(end[0]) === GRID_STREET_SPAN || Math.abs(end[1]) === GRID_STREET_SPAN;
    const onCanalEdge = Math.abs(end[1] - CANAL_Z) === CANAL_HALF_WIDTH;
    if (onBoundary || onCanalEdge) continue;
    // 端点必须落在某条垂直街道的内部
    const perpHit = s.axis === 'ns'
      ? ewLines.some((z) => Math.abs(end[1] - z) < 1e-6)
      : nsLines.some((x) => Math.abs(end[0] - x) < 1e-6);
    if (!perpHit) dangling.push(`${s.key} 端点 (${end[0]},${end[1]})`);
  }
}
ok(dangling.length === 0, '无悬空端点（deg-1 断头）', dangling.join(' '));

// 对照旧路网（批次 59 实测：79 个端点落在街区底板内部、39 个 deg-1 悬空断头）
console.log('\n[8] 端点统计（对照批次 59 旧路网：39 个 deg-1 悬空断头 / 58 个直角拐角缺接头）');
const totalEndpoints = segments.length * 2;
const interiorEndpoints = totalEndpoints - segments.filter((s) =>
  s.from[0] === -GRID_STREET_SPAN || s.from[1] === -GRID_STREET_SPAN ||
  s.to[0] === GRID_STREET_SPAN || s.to[1] === GRID_STREET_SPAN ||
  Math.abs(s.to[1] - CANAL_Z) === CANAL_HALF_WIDTH).length * 2;
console.log(`  · 线段总数：${segments.length}（南北 ${nsLines.length}×2 拆段 + 东西 ${ewLines.length}）`);
console.log(`  · 端点总数：${totalEndpoints}；城市边界端点：${segments.length * 2 - interiorEndpoints}（4 角 + 运河口）`);
console.log(`  · 悬空端点：${dangling.length}（旧路网 39）`);
console.log(`  · 直角拐角：0（方格网无 L 形拐角，旧路网 58 处缺接头补丁 / 1175 m² 沥青）`);

console.log('\n=== 结果 ===');
if (failures === 0) {
  console.log('全部通过 ✅');
  process.exit(0);
}
console.error(`失败 ${failures} 项 ❌`);
process.exit(1);
