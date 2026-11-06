/**
 * cityVehicleRegistry + `vehicle:<id>` 实体解析单测（批次 85 §3.2.1）。
 *
 * 零依赖断言写法（同 cityShowAdapter.test.ts）：由
 * `scripts/verify_city_show_adapter.mjs` 经入口 import 打包执行
 * （npm run verify:city-show-adapter）；模块加载即自跑，失败抛错 ⇒ 非零退出。
 *
 * 覆盖（方案 §7.1 硬性断言）：
 *   1. 注册表 register / unregister / lookup / nearest / ids；
 *   2. `vehicle:<id>` 可解析；无效 id ⇒ 解析 null（adapter 层转
 *      `entity_not_found`，**不抛错、不瞎聚**）；
 *   3. 既有解析链（注册表 / 城区 / building）零回归；
 *   4. focus_entity + 真实 cityEntityApi：命中聚焦 / 未命中 entity_not_found。
 */

import {
  vehicleRegistry,
  clearVehicleRegistry,
} from '../cityVehicleRegistry';
import { resolveEntity, setEntityPosition, clearEntityRegistry } from '../cityEntityResolver';
import { applyDisplayCommand, type CityShowDeps } from '../cityShowAdapter';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[cityVehicleRegistry] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function deepEq(actual: unknown, expected: unknown, msg: string): void {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a !== b) {
    throw new Error(`[cityVehicleRegistry] ${msg}: expected ${b}, got ${a}`);
  }
}

export function runCityVehicleRegistryTests(): void {
  // ── 1. 注册表基础行为 ───────────────────────────────────────────
  clearVehicleRegistry();
  eq(vehicleRegistry.ids().length, 0, '初始注册表应为空');

  vehicleRegistry.register('sedan-0', { x: 1, z: 2 }, 'sedan');
  vehicleRegistry.register('truck-0', { x: -4, z: 8 }, 'truck');
  deepEq(vehicleRegistry.lookup('sedan-0'), { x: 1, z: 2 }, 'lookup 应返回登记坐标');
  deepEq(vehicleRegistry.lookup('truck-0'), { x: -4, z: 8 }, 'lookup 应返回另一车坐标');
  eq(vehicleRegistry.lookup('nope'), null, '未登记 id 的 lookup 应为 null');
  eq(vehicleRegistry.lookup(''), null, '空 id 的 lookup 应为 null');

  // 同 id 重复 register = 原地更新（车辆逐帧上报的稳态路径）。
  vehicleRegistry.register('sedan-0', { x: 3, z: 4 });
  deepEq(vehicleRegistry.lookup('sedan-0'), { x: 3, z: 4 }, '重复 register 应更新坐标');

  // nearest：离 (3,4) 最近的是 sedan-0。
  eq(vehicleRegistry.nearest(3.1, 4.1), 'sedan-0', 'nearest 应命中最近车辆');
  eq(vehicleRegistry.nearest(-4, 8), 'truck-0', 'nearest 在另一侧应命中 truck-0');
  deepEq(vehicleRegistry.ids().sort(), ['sedan-0', 'truck-0'], 'ids 应列出全部车辆');

  vehicleRegistry.unregister('sedan-0');
  eq(vehicleRegistry.lookup('sedan-0'), null, 'unregister 后 lookup 应为 null');
  eq(vehicleRegistry.nearest(3, 4), 'truck-0', 'unregister 后 nearest 应跳过已注销车');
  vehicleRegistry.unregister('sedan-0'); // 重复注销幂等，不抛错

  vehicleRegistry.register('', { x: 0, z: 0 }); // 空 id 拒绝
  eq(vehicleRegistry.ids().length, 1, '空 id 不应入表');

  clearVehicleRegistry();
  eq(vehicleRegistry.nearest(0, 0), null, '空表 nearest 应为 null');
  eq(vehicleRegistry.ids().length, 0, 'clear 后应为空');

  // ── 2. `vehicle:<id>` 解析（cityEntityResolver 第 5 级）──────────
  clearEntityRegistry();
  clearVehicleRegistry();
  vehicleRegistry.register('bus-2', { x: 10, z: -6 }, 'bus');
  const hit = resolveEntity('vehicle:bus-2');
  deepEq(hit, { x: 10, z: -6, label: 'vehicle:bus-2' }, 'vehicle:<id> 应解析到注册坐标');
  eq(resolveEntity('vehicle:bus-9'), null, '无效车辆 id 应解析 null（不抛错）');
  eq(resolveEntity('vehicle:'), null, '空车辆 id 应解析 null（不抛错）');

  // ── 3. 既有解析链零回归（注册表 / 城区 / building）────────────────
  setEntityPosition('resident:seat:2', 7, 8, '小明');
  deepEq(
    resolveEntity('resident:seat:2'),
    { x: 7, z: 8, label: '小明' },
    '既有注册表级解析应零回归',
  );
  const districtPos = resolveEntity('finance');
  eq(districtPos?.x !== undefined && districtPos?.z !== undefined, true, '城区 id 应解析到区中心');
  const bPos = resolveEntity('finance:0'); // 注意：既有形态 building:<区>:<序>
  eq(bPos, null, 'building 级只认 building:<区>:<序> 形态（旧形态不误命中）');
  const bPos2 = resolveEntity('building:finance:0');
  eq(bPos2 !== null, true, 'building:<区>:<序> 应解析（零回归）');
  eq(resolveEntity('whatever-unknown'), null, '未知 id 应解析 null（不静默瞎聚）');

  // ── 4. focus_entity + 真实 entityApi（adapter → resolver 闭环）────
  clearEntityRegistry();
  clearVehicleRegistry();
  vehicleRegistry.register('truck-1', { x: 5, z: 5 }, 'truck');
  const focusRef: { current: { x: number; z: number } | null } = { current: null };
  const notices: unknown[] = [];
  const deps: CityShowDeps = {
    focusRef,
    setSelectedDistrict: () => {},
    setMode: () => {},
    setAnnotation: () => {},
    clearAnnotation: () => {},
    entityApi: { resolve: resolveEntity },
    onNotice: (n) => notices.push(n),
  };

  const ok = applyDisplayCommand(
    { type: 'focus_entity', entity_id: 'vehicle:truck-1', kind: 'vehicle' } as VirtualCityDisplayCommand,
    deps,
  );
  eq(ok.applied, true, 'focus_entity 命中车辆应生效');
  deepEq(focusRef.current, { x: 5, z: 5 }, 'focus_entity 应聚焦车辆坐标');
  eq(notices.length, 0, '命中不应产生提示');

  const miss = applyDisplayCommand(
    { type: 'focus_entity', entity_id: 'vehicle:ghost', kind: 'vehicle' } as VirtualCityDisplayCommand,
    deps,
  );
  eq(miss.applied, false, '无效车辆 id 不应生效');
  eq(miss.notice?.code, 'entity_not_found', '无效车辆 id 应回 entity_not_found（不抛错）');
  deepEq(focusRef.current, { x: 5, z: 5 }, '未命中不应改相机');

  // eslint-disable-next-line no-console
  console.log('[cityVehicleRegistry] 全部断言通过');
}

// 直接执行（被打包器当作入口 import）时自动运行。
runCityVehicleRegistryTests();
