/**
 * coverage — 场景物件 id 覆盖自检（批次 28 · 工作线 B4）。
 *
 * `ALL_SCENE_OBJECT_IDS` 是方案 §5.4「全量覆盖清单」的代码固化：场景内每一个
 * 可渲染物件类型的 catalog id。自检要求清单 100% 命中 catalog（缺失/拼错即抛错），
 * 防「新物件忘了配简介」（CLAUDE.md §130）。
 *
 * 运行（无 test runner，走 node 脚本口径）：
 *   cd ClientWeb && node_modules/.bin/esbuild \
 *     src/components/virtualCity/objectInfo/coverage.ts --bundle --platform=node \
 *     --format=cjs --outfile=/tmp/vc-objectinfo-coverage.cjs \
 *     --tsconfig=tsconfig.json && node /tmp/vc-objectinfo-coverage.cjs
 *
 * 反向检查 `unexpected`（catalog 有但清单没有）只告警不抛错 —— catalog 允许
 * 超前于接线新增条目，但清单里的 id 必须全部有文案。
 */

import { allObjectInfoEntries, getObjectInfo } from './catalog';
import { VIRTUAL_CITY_DISTRICTS } from '@/types/virtualCity';

/** 32 城区 id（`district.<id>`）。 */
const DISTRICT_IDS: string[] = VIRTUAL_CITY_DISTRICTS.map((d) => `district.${d.id}`);

/** 方案 §5.4 覆盖清单（场景物件类型 id；district.* 单独并入）。 */
export const SCENE_TYPE_IDS: string[] = [
  // BuildingMesh / building_shapes 5 型
  'building.tower',
  'building.slab',
  'building.house',
  'building.shed',
  'building.pavilion',
  // StreetPropsLayer 街具
  'prop.trash-can',
  'prop.mailbox',
  'prop.vendor-kiosk',
  'prop.bicycle-rack',
  'prop.phone-booth',
  'prop.parking-meter',
  'prop.sign',
  'prop.solar-panel',
  'prop.rooftop-acc',
  'prop.bus-stop',
  // 树（实例化，road/park 按实例区分）
  'tree.road',
  'tree.park',
  // 车辆 4 型
  'vehicle.sedan',
  'vehicle.truck',
  'vehicle.bus',
  'vehicle.taxi',
  // 角色
  'actor.pedestrian',
  'actor.player',
  // 道路 / 交通设施
  'road.street-light',
  'road.traffic-signal',
  // 批次 43：行人过街信号灯（与 road.traffic-signal 反相联动）
  'road.pedestrian-signal',
  // 批次 43：宽路口悬臂式信号灯
  'road.mast-arm-signal',
  // 批次 45：公园设施五件（拆分自 civic.park-extras）
  'park.pavilion',
  'park.playground',
  'park.fitness',
  'park.bench',
  'park.lamp',
  'road.surface',
  'road.ring',
  'road.first-ring',
  'road.gate',
  'road.bridge',
  // 水系
  'water.canal',
  'water.harbour',
  'water.mist',
  // CivicLayer 14 组件
  'civic.city-hall',
  'civic.port-terminal',
  'civic.sports-field',
  'civic.rail-viaduct',
  'civic.heli-pad',
  'civic.gas-station',
  'civic.substation',
  'civic.water-tower',
  'civic.comm-tower',
  'civic.fire-station',
  'civic.police-station',
  'civic.outskirts',
  'civic.park-extras',
  'civic.canal-extras',
  // LandmarksLayer
  'landmark.fountain',
  'landmark.park-grounds',
  'landmark.construction-site',
  'landmark.parking-lot',
  // CityEdgeLayer（四缘 + 灯塔/货轮/吊机/帆船）
  'edge.north-mountains',
  'edge.west-desert',
  'edge.east-forest',
  'edge.south-ocean',
  'edge.lighthouse',
  'edge.cargo-ship',
  'edge.crane',
  'edge.sailboat',
  // 天空 / 地面
  'sky.cloud',
  'ground.city',
];

/** §5.4 全量 id 清单（32 城区 + 场景物件类型）。 */
export const ALL_SCENE_OBJECT_IDS: string[] = [...DISTRICT_IDS, ...SCENE_TYPE_IDS];

export interface CoverageResult {
  total: number;
  hit: number;
  missing: string[];
  duplicate: string[];
  unexpected: string[];
}

/** 清单 → catalog 覆盖自检（缺失/重复即失败；多余条目仅告警）。 */
export function checkCoverage(): CoverageResult {
  const seen = new Set<string>();
  const duplicate: string[] = [];
  for (const id of ALL_SCENE_OBJECT_IDS) {
    if (seen.has(id)) duplicate.push(id);
    seen.add(id);
  }
  const missing = ALL_SCENE_OBJECT_IDS.filter((id) => !getObjectInfo(id));
  const expected = new Set(ALL_SCENE_OBJECT_IDS);
  const unexpected = allObjectInfoEntries()
    .map((e) => e.id)
    .filter((id) => !expected.has(id));
  return {
    total: ALL_SCENE_OBJECT_IDS.length,
    hit: ALL_SCENE_OBJECT_IDS.length - missing.length,
    missing,
    duplicate,
    unexpected,
  };
}

/** CLI 入口：全部命中打印摘要并 exit 0；缺失/重复抛错 exit 1。 */
function main(): void {
  const r = checkCoverage();
  const catCount = allObjectInfoEntries().length;
  console.log(`[objectInfo] catalog entries: ${catCount}`);
  console.log(`[objectInfo] coverage: ${r.hit}/${r.total} ids hit`);
  if (r.unexpected.length > 0) {
    console.log(`[objectInfo] catalog has ${r.unexpected.length} extra id(s) not in checklist (ok): ${r.unexpected.join(', ')}`);
  }
  if (r.missing.length > 0 || r.duplicate.length > 0) {
    if (r.missing.length > 0) console.error(`[objectInfo] MISSING from catalog: ${r.missing.join(', ')}`);
    if (r.duplicate.length > 0) console.error(`[objectInfo] DUPLICATE in checklist: ${r.duplicate.join(', ')}`);
    throw new Error('[objectInfo] coverage check FAILED');
  }
  console.log('[objectInfo] 全部命中 — coverage check PASSED');
}

main();
