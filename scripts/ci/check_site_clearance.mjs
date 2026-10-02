/**
 * check_site_clearance — **落地构筑物与道路的净距**数值护栏。
 *
 * 覆盖两处已实证的「几何没错、落位错了」事故：
 *   · 批次 49 高架铁路：13 根桥墩有一根离 arterial-z-26 路缘仅 1.2 m；
 *   · 批次 50 施工工地：围挡西侧**压在** arterial-z26 的沥青路面上 4.0 m。
 *
 * 为什么需要它（§130 类缺陷的通用形态）：
 *   `RailViaduct.tsx` 里的 `SPAN_M` / `PIER_X0` 是**裸字面量**，tsc / 构建 / 出图
 *   全部无感 —— 把 30.5 改成 30.0，编译照过、模型照渲染，只是有一根桥墩**静静压在
 *   arterial-z±26 的沥青路面上**。批次 43~48 的多个事故（体育场压路、码头伸出城界、
 *   桥墩上路）都是这一类：几何没错，是**落位**错了，而没有任何判据把「几何」和
 *   「它与全城其它所有消费者的关系」放进同一个断言。
 *
 * 判据：把 `roadNetwork` 的真实路段与消费端的落位常量放进同一份数据里，
 * 逐点算 `净距 = 点到路段中心线距离 − 路半宽 − 自身半宽`，任一点 < MIN_CLEAR 即失败。
 *
 * 两条被检查的落位（常量一律**从组件源码文本里取**，不复制第二份）：
 *   ① `civic/RailViaduct.tsx` 的 `SPAN_M` / `PIER_X0` / `PIER_COUNT`（逐墩）
 *   ② `props/ConstructionSite.tsx` 的 `SITE_X` / `SITE_Z` / `SITE_W` / `SITE_D`
 *      （场坪按 25×25 网格采样；塔吊臂的回转扫掠**不检查** —— 臂根 41.8 m
 *        远高于路面与沿线建筑，规范允许越路回转）
 *
 * 用法（仓库根目录）：
 *   npx esbuild ClientWeb/src/components/virtualCity/roadNetwork.ts --bundle \
 *       --format=esm --outfile=/tmp/_rn.mjs --loader:.ts=ts --external:three
 *   npx esbuild ClientWeb/src/types/virtualCity.ts --bundle \
 *       --format=esm --outfile=/tmp/_vc.mjs --loader:.ts=ts --external:three --external:react
 *   node scripts/ci/check_site_clearance.mjs
 *
 * ⚠ 本脚本**读组件源码文本**取落位常量，而不是 import tsx（它 import three / r3f，
 *   node 侧跑不起来）。变量改名时脚本会取不到值 —— 已在 `num()` 里 `exit(2)`，
 *   不会默默通过。
 */
import { readFileSync } from 'node:fs';
import { buildRoadNetwork } from '/tmp/_rn.mjs';
import { VIRTUAL_CITY_DISTRICTS } from '/tmp/_vc.mjs';

const RAIL_Z = 46;              // three 世界 z
const PIER_HZ = 0.18;           // 墩横向半宽（墩 2.4×1.8 m ⇒ 沿 z 是 1.8 m）
const MIN_CLEAR_M = 3.0;        // 桥墩离路缘的最小净距（米）
const CORRIDOR_PAD = 4;         // 只检查走廊附近 ±4 u 的路段

// ── 从 RailViaduct.tsx 源码取走廊参数 ──────────────────────────────────
const src = readFileSync(
  new URL('../../ClientWeb/src/components/virtualCity/civic/RailViaduct.tsx', import.meta.url),
  'utf8',
);
function num(re) {
  const m = src.match(re);
  if (!m) {
    console.error(`✗ 无法从 RailViaduct.tsx 解析 ${re} —— 变量可能已改名，请同步本脚本`);
    process.exit(2);
  }
  return Number(m[1]);
}
const SPAN_M = num(/const SPAN_M = ([\d.]+);/);
const PIER_COUNT = num(/const PIER_COUNT = (\d+);/);
const PIER_X0 = num(/const PIER_X0 = (-?[\d.]+);/);
const SPAN = SPAN_M / 10;

const piers = Array.from({ length: PIER_COUNT }, (_, k) => PIER_X0 + SPAN * k);
const corridor = [piers[0], piers[piers.length - 1] + SPAN];

// ── 真实路段 ────────────────────────────────────────────────────────────
const net = buildRoadNetwork(VIRTUAL_CITY_DISTRICTS, 0);
const segments = Object.values(net).flat().filter((s) => s && s.key && s.from);
const near = segments.filter((s) => {
  const zs = [s.from[1], s.to[1]];
  const xs = [s.from[0], s.to[0]];
  return Math.min(...zs) <= RAIL_Z + CORRIDOR_PAD
    && Math.max(...zs) >= RAIL_Z - CORRIDOR_PAD
    && Math.max(...xs) > corridor[0] - 10
    && Math.min(...xs) < corridor[1] + 10;
});

function distToSeg(px, pz, a, b) {
  const dx = b[0] - a[0];
  const dz = b[1] - a[1];
  const A = dx * dx + dz * dz;
  if (A < 1e-9) return Math.hypot(px - a[0], pz - a[1]);
  const t = Math.max(0, Math.min(1, ((px - a[0]) * dx + (pz - a[1]) * dz) / A));
  return Math.hypot(px - (a[0] + t * dx), pz - (a[1] + t * dz));
}
const halfWidth = (s) => (s.cls === 'arterial' ? 0.7 : 0.45);   // 主路 14 m / 次路 9 m

let worst = { clear: Infinity };
const bad = [];
for (const x of piers) {
  for (const s of near) {
    const clear = distToSeg(x, RAIL_Z, s.from, s.to) - halfWidth(s) - PIER_HZ;
    if (clear < worst.clear) worst = { clear, x, key: s.key };
    if (clear * 10 < MIN_CLEAR_M) bad.push({ x, key: s.key, clear: clear * 10 });
  }
}

console.log(`走廊 z=${RAIL_Z}｜${PIER_COUNT} 跨 × ${SPAN_M} m（${SPAN} u）｜`
  + `桥墩 x ∈ [${corridor[0].toFixed(3)}, ${corridor[1].toFixed(3)}] u`
  + `（长 ${((corridor[1] - corridor[0]) * 10).toFixed(1)} m）`);
console.log(`邻近路段 ${near.length} 条：${near.map((s) => s.key).join(', ')}`);
console.log(`最紧的一根墩：x=${worst.x.toFixed(2)} u 距 ${worst.key} 净距 ${(worst.clear * 10).toFixed(2)} m`
  + `（阈值 ${MIN_CLEAR_M} m）`);
if (bad.length) {
  console.error(`✗ ${bad.length} 根桥墩离路缘不足 ${MIN_CLEAR_M} m：`);
  for (const b of bad) console.error(`    x=${b.x.toFixed(2)} u  ${b.key}  ${b.clear.toFixed(2)} m`);
  process.exit(1);
}
console.log('✓ 全部桥墩离路缘 ≥ ' + MIN_CLEAR_M + ' m');

// ── ② 施工工地场坪（批次 50）────────────────────────────────────────────
const SITE_MIN_CLEAR_M = 3.0;
const csSrc = readFileSync(
  new URL('../../ClientWeb/src/components/virtualCity/props/ConstructionSite.tsx', import.meta.url),
  'utf8',
);
function cnum(re) {
  const m = csSrc.match(re);
  if (!m) {
    console.error(`✗ 无法从 ConstructionSite.tsx 解析 ${re} —— 变量可能已改名，请同步本脚本`);
    process.exit(2);
  }
  return Number(m[1]);
}
const SX = cnum(/const SITE_X = (-?[\d.]+);/);
const SZ = cnum(/const SITE_Z = (-?[\d.]+);/);
const SW = cnum(/const SITE_W = ([\d.]+);/);
const SD = cnum(/const SITE_D = ([\d.]+);/);

let csWorst = { clear: Infinity };
for (let i = 0; i <= 24; i++) {
  for (let j = 0; j <= 24; j++) {
    const px = SX - SW / 2 + (SW * i) / 24;
    const pz = SZ - SD / 2 + (SD * j) / 24;
    for (const s of segments) {
      const clear = distToSeg(px, pz, s.from, s.to) - halfWidth(s);
      if (clear < csWorst.clear) csWorst = { clear, x: px, z: pz, key: s.key };
    }
  }
}
console.log(`\n工地场坪 (${SX}, ${SZ}) ${(SW * 10).toFixed(0)}×${(SD * 10).toFixed(0)} m`
  + `｜最小路净距 ${(csWorst.clear * 10).toFixed(1)} m @(${csWorst.x.toFixed(2)}, ${csWorst.z.toFixed(2)})`
  + ` 距 ${csWorst.key}（阈值 ${SITE_MIN_CLEAR_M} m）`);
if (csWorst.clear * 10 < SITE_MIN_CLEAR_M) {
  console.error(`✗ 施工围挡离 ${csWorst.key} 仅 ${(csWorst.clear * 10).toFixed(1)} m —— 围挡会压在路面上`);
  process.exit(1);
}
console.log('✓ 工地围挡离全部路段 ≥ ' + SITE_MIN_CLEAR_M + ' m');
