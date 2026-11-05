/**
 * cityEntityResolver —— 显示指令 `focus_entity` 的实体定位（游戏侧薄适配层）。
 *
 * 批次 84 的 `focus_entity` 指令带 `entity_id` + `kind`
 * （building / resident / vehicle）。后端只给**字符串 id**，前端需要把它
 * 解析成世界坐标才能拉相机。本文件提供统一的解析入口：
 *
 *   1. **注册表命中** —— 场景组件（居民 / 车辆 / 建筑）把自己的实时位置
 *      注册进来（`registerEntity`），id 形如 `resident:seat:2`；
 *   2. `resident:<n>` —— 批次 35 `crowdRegistry` 的上街居民 live 位置；
 *   3. `<districtId>` —— 城区中心（`districtCenter`）；
 *   4. `building:<districtId>:<idx>` —— 批次 30 街墙布局的第 idx 栋楼
 *      （相对区中心的偏移 + 区中心 = 世界坐标，真实可算）。
 *
 * 解析不到 ⇒ 返回 null ⇒ 调用方**显式提示**「未找到实体」，绝不静默吞掉
 * （批次 84 §11：前端显示指令类型不全导致「点了没反应」是主要风险）。
 */

import {
  VIRTUAL_CITY_DISTRICTS,
  districtCenter,
  type VirtualCityDistrictId,
} from '@/types/virtualCity';
import { buildingsFor } from './building_layout';
import { getCrowdPosition } from './crowdRegistry';

/** 实体类型（与后端 `kind` 字段逐字一致）。 */
export type CityEntityKind = 'building' | 'resident' | 'vehicle';

/** 解析结果（世界坐标 + 可读标签）。 */
export interface CityEntityPosition {
  x: number;
  z: number;
  /** 供 UI 展示的实体名（未知名 → id 本身）。 */
  label: string;
}

/** 注册表（id → 世界坐标）；场景组件写入，解析器读取。 */
const registry = new Map<string, { x: number; z: number; label?: string }>();

/**
 * 注册 / 更新实体位置（场景组件每帧或低频调用）。
 * 稳态零分配：命中即原地改字段。
 */
export function setEntityPosition(id: string, x: number, z: number, label?: string): void {
  const hit = registry.get(id);
  if (hit) {
    hit.x = x;
    hit.z = z;
    if (label !== undefined) hit.label = label;
  } else {
    registry.set(id, { x, z, ...(label ? { label } : {}) });
  }
}

/** 注销实体（场景卸载时调用，防残留坐标）。 */
export function removeEntity(id: string): void {
  registry.delete(id);
}

/** 清空注册表（换房 / 卸载）。 */
export function clearEntityRegistry(): void {
  registry.clear();
}

const DISTRICT_IDS = new Set<string>(VIRTUAL_CITY_DISTRICTS.map((d) => d.id));

/**
 * 解析实体 id → 世界坐标。
 *
 * @param entityId 实体 id（注册表 id / `resident:<n>` / 城区 id / `building:<区>:<序>`）
 * @param _kind 实体类型（预留：当前解析按 id 形态即可判定类型；不认识的类型同样尝试通用解析）
 */
export function resolveEntity(entityId: string, _kind?: string): CityEntityPosition | null {
  const id = (entityId ?? '').trim();
  if (!id) return null;

  // 1. 注册表
  const reg = registry.get(id);
  if (reg) return { x: reg.x, z: reg.z, label: reg.label ?? id };

  // 2. 上街居民（crowdRegistry）
  const crowdMatch = /^resident:(\d+)$/.exec(id);
  if (crowdMatch) {
    const pos = getCrowdPosition(Number(crowdMatch[1]));
    if (pos) return { x: pos.x, z: pos.z, label: id };
    return null;
  }

  // 3. 城区 id
  if (DISTRICT_IDS.has(id)) {
    const c = districtCenter(id as VirtualCityDistrictId);
    return { x: c.x, z: c.z, label: id };
  }

  // 4. building:<districtId>:<idx>
  const bMatch = /^building:([A-Za-z0-9_]+):(\d+)$/.exec(id);
  if (bMatch) {
    const def = VIRTUAL_CITY_DISTRICTS.find((d) => d.id === bMatch[1]);
    if (def) {
      const specs = buildingsFor(def);
      const idx = Number(bMatch[2]);
      const spec = specs[idx];
      if (spec) {
        const c = districtCenter(def.id as VirtualCityDistrictId);
        return { x: c.x + spec.x, z: c.z + spec.z, label: `${def.id}#${idx}` };
      }
    }
    return null;
  }

  return null;
}

/** 供非 React 代码（adapter）直接调用。 */
export const cityEntityApi = {
  resolve: resolveEntity,
  register: setEntityPosition,
  remove: removeEntity,
  clear: clearEntityRegistry,
};
