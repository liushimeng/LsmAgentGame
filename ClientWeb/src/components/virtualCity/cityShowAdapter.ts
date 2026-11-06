/**
 * cityShowAdapter —— 结构化显示指令 → 既有 3D 控制动作（纯函数 + 副作用适配层）。
 *
 * 批次 81「系统对话」：City-WebGL-Show Agent 只产出「看哪里 / 怎么显示」的
 * 结构化指令（方案 §3.3），渲染与相机控制仍由前端既有引擎执行。本文件是
 * **游戏侧薄适配层**（§2.1 约束 5：`engine3d/` 不 import 游戏私有模块，
 * 反之游戏侧可以依赖引擎），把指令翻译成既有控制面：
 *
 *   focus_district    → 选中区 + 相机聚焦该区中心（与 handleSelectDistrict 同一 ref）
 *   focus_point       → 相机聚焦任意世界坐标
 *   set_mode          → freeViewApi.setMode（orbit ↔ fly）
 *   highlight_district→ 选中态高亮（**批次 84 起颜色真正生效**）
 *   annotate          → cityShowStore 的 DOM 浮动标签（8s 自动消）
 *   clear             → 清选中 + 清标注 + 清全部显示覆盖
 *
 * 批次 84「全维度事件体系与 Agent 双向对话」把显示原语扩到 **14 个**
 * （wire 契约见 lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/
 * 01-方案设计.md §4.5.2，字段名与后端 json tag 逐字一致）：
 *
 *   set_time_of_day    → 覆盖昼夜（cityTimeStore 采样器真实现）
 *   set_weather        → 覆盖天气（同上）
 *   set_time_speed     → 覆盖时间流速（同上）
 *   focus_entity       → 实体聚焦（cityEntityResolver 解析 + focusRef）
 *   set_layer_visibility→ 图层显隐（cityShowStore 图层注册表）
 *   set_filter         → 类别筛选（按图层隔离 / 隐藏；批次 85 起 category 支持
 *                        物件类别 / 车辆类型的「类别 → 实例」粒度，退化为按类别
 *                        显隐并以 `filter_showhide` 提示明示口径）
 *   camera_preset      → 相机预设（引擎一次性轨道位姿请求）
 *   fly_path           → 折线飞行（focusRef 节奏写入）
 *
 * **未知 type / 缺参 / 能力未接线一律显式提示**（返回 notice 并回调
 * `deps.onNotice`），绝不静默吞掉 —— 批次 84 §11 的头号风险就是
 * 「前端显示指令类型不全导致点了没反应」。
 */

import type { MutableRefObject } from 'react';
import { districtCenter, type VirtualCityDistrictId } from '@/types/virtualCity';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';
import { resolveCityFilterCategory } from './cityFilterCategories';

/** 视角模式（与 engine3d/controls/freeViewStore 的 FreeViewMode 同集）。 */
export type CityShowMode = 'orbit' | 'fly';

/** 显示指令提示码（UI 侧据此取 i18n 文案；adapter 不写死语言）。 */
export type CityShowNoticeCode =
  /** 后端下发了前端不认识的指令类型。 */
  | 'unsupported_type'
  /** 指令合法，但本批未接引擎（能力缺失）。 */
  | 'unwired'
  /** 指令缺参 / 参数非法。 */
  | 'invalid_params'
  /** focus_entity 未解析到实体。 */
  | 'entity_not_found'
  /** set_filter 的 category 无法映射到已知图层 / 物件类别。 */
  | 'unknown_category'
  /**
   * 批次 85 §3.2.3：实例级筛选（物件类别 / 车辆类型）走「按类别显隐」——
   * 引擎无安全的逐实例调色入口，退化实现**明示不静默**（信息性提示，指令已生效）。
   */
  | 'filter_showhide';

/** 一条显示指令提示。 */
export interface CityShowNotice {
  code: CityShowNoticeCode;
  /** 指令类型（未支持时即未知类型名）。 */
  type: string;
  /** 补充信息（如实体 id、图层名）。 */
  detail?: string;
  /** 产生时间（unix ms；由落 store 的一方补写）。 */
  ts?: number;
}

/** 执行结果（UI 据此提示；`applied=false` 表示未生效）。 */
export interface CityShowResult {
  /** 指令类型（原样回传；未知类型即后端下发的字符串）。 */
  type: string;
  /** 是否已生效。 */
  applied: boolean;
  /** 未生效时的提示（若有）。 */
  notice?: CityShowNotice;
}

/** 图层 / 筛选命令式接口（由 cityShowStore 提供）。 */
export interface CityShowLayerApi {
  /** 设置单个图层显隐；非法图层名返回 false。 */
  setLayerVisibility: (layer: string, visible: boolean) => boolean;
  /** 设置类别筛选；无法映射类别返回 false。 */
  setFilter: (category: string, mode: 'highlight' | 'dim') => boolean;
}

/** 昼夜 / 天气 / 时间流速命令式接口（由 cityTimeStore 提供）。 */
export interface CityShowEnvApi {
  setTimeOfDay: (phase?: string, hour?: number) => boolean;
  setWeather: (weather: string) => boolean;
  setTimeSpeed: (speed: string) => boolean;
  /** 清除全部环境覆盖。 */
  clear: () => void;
}

/** 实体解析接口（由 cityEntityResolver 提供）。 */
export interface CityShowEntityApi {
  resolve: (
    entityId: string,
    kind?: string,
  ) => { x: number; z: number; label: string } | null;
}

/** 路径飞行接口（由 cityPathFlight 提供）。 */
export interface CityShowPathApi {
  flyPath: (points: { x: number; z: number }[]) => boolean;
}

/** 相机预设接口（由 cityCameraPreset 提供）。 */
export interface CityShowCameraApi {
  applyPreset: (preset: string) => boolean;
}

export interface CityShowDeps {
  /** 与 `VirtualCityGamePage::handleSelectDistrict` 共用的相机聚焦 ref。 */
  focusRef: MutableRefObject<{ x: number; z: number } | null>;
  /** `useVirtualCityStore.setSelectedDistrict`（选区高亮）。 */
  setSelectedDistrict: (id: VirtualCityDistrictId | null) => void;
  /** `freeViewApi.setMode`。 */
  setMode: (mode: CityShowMode) => void;
  /** `cityShowApi.setAnnotation`。 */
  setAnnotation: (text: string, district?: string) => void;
  /** `cityShowApi.clearAnnotation`。 */
  clearAnnotation: () => void;
  /** 批次 84：图层显隐 + 类别筛选。 */
  layerApi?: CityShowLayerApi;
  /** 批次 84：昼夜 / 天气 / 时间流速覆盖。 */
  envApi?: CityShowEnvApi;
  /** 批次 84：实体定位。 */
  entityApi?: CityShowEntityApi;
  /** 批次 84：折线飞行。 */
  pathApi?: CityShowPathApi;
  /** 批次 84：相机预设。 */
  cameraApi?: CityShowCameraApi;
  /** 批次 84：高亮（含颜色；`highlight_district` 的 color 字段）。 */
  setHighlight?: (district: string, color?: string) => void;
  /** 批次 84：清除全部显示覆盖（`clear` 指令）。 */
  clearOverrides?: () => void;
  /** 批次 84：提示回调（未支持 / 未接引擎）。 */
  onNotice?: (notice: CityShowNotice) => void;
}

/** 有限数判定（focus_point 参数校验）。 */
function isFiniteNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v);
}

/** 非空字符串判定。 */
function nonEmptyString(v: unknown): v is string {
  return typeof v === 'string' && v.trim().length > 0;
}

/**
 * 执行一条显示指令。
 *
 * 返回执行结果：未知 type / 缺参 / 能力未接线 ⇒ `applied=false` 且带
 * `notice`（并回调 `deps.onNotice`）—— **绝不静默吞掉**。
 */
export function applyDisplayCommand(
  cmd: VirtualCityDisplayCommand,
  deps: CityShowDeps,
): CityShowResult {
  const fail = (
    type: string,
    code: CityShowNoticeCode,
    detail?: string,
  ): CityShowResult => {
    const notice: CityShowNotice = { code, type, ...(detail ? { detail } : {}) };
    deps.onNotice?.(notice);
    return { type, applied: false, notice };
  };

  if (!cmd || typeof cmd.type !== 'string') {
    return fail(String(cmd?.type ?? ''), 'unsupported_type');
  }

  switch (cmd.type) {
    // ── 批次 81 的 6 个既有原语 ────────────────────────────────────────
    case 'focus_district': {
      const d = typeof cmd.district === 'string' ? cmd.district : '';
      if (!d) return fail(cmd.type, 'invalid_params', 'district');
      const id = d as VirtualCityDistrictId;
      deps.setSelectedDistrict(id);
      const c = districtCenter(d);
      deps.focusRef.current = { x: c.x, z: c.z };
      return { type: cmd.type, applied: true };
    }

    case 'focus_point': {
      if (!isFiniteNumber(cmd.x) || !isFiniteNumber(cmd.z)) {
        return fail(cmd.type, 'invalid_params', 'x/z');
      }
      deps.focusRef.current = { x: cmd.x, z: cmd.z };
      return { type: cmd.type, applied: true };
    }

    case 'set_mode': {
      if (cmd.mode !== 'orbit' && cmd.mode !== 'fly') {
        return fail(cmd.type, 'invalid_params', 'mode');
      }
      deps.setMode(cmd.mode);
      return { type: cmd.type, applied: true };
    }

    case 'highlight_district': {
      if (!nonEmptyString(cmd.district)) {
        return fail(cmd.type, 'invalid_params', 'district');
      }
      deps.setSelectedDistrict(cmd.district as VirtualCityDistrictId);
      // 批次 84：color 字段真正生效（此前被忽略）。
      if (deps.setHighlight) {
        deps.setHighlight(cmd.district, cmd.color);
      } else {
        return fail(cmd.type, 'unwired', 'highlight_color');
      }
      return { type: cmd.type, applied: true };
    }

    case 'annotate': {
      const text = typeof cmd.text === 'string' ? cmd.text : '';
      if (!text) return fail(cmd.type, 'invalid_params', 'text');
      deps.setAnnotation(text, cmd.district);
      return { type: cmd.type, applied: true };
    }

    case 'clear': {
      deps.setSelectedDistrict(null);
      deps.clearAnnotation();
      // 批次 84：clear 同时清掉全部显示覆盖（时刻 / 天气 / 流速 / 图层 / 筛选）。
      deps.clearOverrides?.();
      return { type: cmd.type, applied: true };
    }

    // ── 批次 84 新增的 8 个原语 ────────────────────────────────────────
    case 'set_time_of_day': {
      if (!deps.envApi) return fail(cmd.type, 'unwired', 'env');
      const ok = deps.envApi.setTimeOfDay(cmd.phase, cmd.hour);
      if (!ok) return fail(cmd.type, 'invalid_params', 'phase/hour');
      return { type: cmd.type, applied: true };
    }

    case 'set_weather': {
      if (!deps.envApi) return fail(cmd.type, 'unwired', 'env');
      const ok = deps.envApi.setWeather(String(cmd.weather ?? ''));
      if (!ok) return fail(cmd.type, 'invalid_params', 'weather');
      return { type: cmd.type, applied: true };
    }

    case 'set_time_speed': {
      if (!deps.envApi) return fail(cmd.type, 'unwired', 'env');
      const ok = deps.envApi.setTimeSpeed(String(cmd.speed ?? ''));
      if (!ok) return fail(cmd.type, 'invalid_params', 'speed');
      return { type: cmd.type, applied: true };
    }

    case 'focus_entity': {
      if (!deps.entityApi) return fail(cmd.type, 'unwired', 'entity');
      const entityId = typeof cmd.entity_id === 'string' ? cmd.entity_id : '';
      if (!entityId) return fail(cmd.type, 'invalid_params', 'entity_id');
      const pos = deps.entityApi.resolve(entityId, cmd.kind);
      if (!pos) return fail(cmd.type, 'entity_not_found', entityId);
      deps.focusRef.current = { x: pos.x, z: pos.z };
      return { type: cmd.type, applied: true };
    }

    case 'set_layer_visibility': {
      if (!deps.layerApi) return fail(cmd.type, 'unwired', 'layer');
      const layer = typeof cmd.layer === 'string' ? cmd.layer : '';
      if (!layer) return fail(cmd.type, 'invalid_params', 'layer');
      const visible = cmd.visible !== false;
      const ok = deps.layerApi.setLayerVisibility(layer, visible);
      if (!ok) return fail(cmd.type, 'invalid_params', layer);
      return { type: cmd.type, applied: true };
    }

    case 'set_filter': {
      if (!deps.layerApi) return fail(cmd.type, 'unwired', 'filter');
      const category = typeof cmd.category === 'string' ? cmd.category.trim() : '';
      if (!category) {
        // category 空 = 取消筛选（后端约定）。
        deps.layerApi.setFilter('', 'highlight');
        return { type: cmd.type, applied: true };
      }
      const mode = cmd.mode === 'dim' ? 'dim' : 'highlight';
      const ok = deps.layerApi.setFilter(category, mode);
      if (!ok) return fail(cmd.type, 'unknown_category', category);
      // 批次 85 §3.2.3：物件类别 / 车辆类型的实例级筛选 —— 引擎无安全的逐实例
      // 调色入口（GLB clone(true) 跨实例共享材质引用；逐实例调色须新建材质实例，
      // 违反 draw call 纪律）⇒ 退化为「按类别显隐」。**明示不静默**：生效同时
      // 回调 onNotice 说明口径（信息性提示，不影响 applied=true）。
      if (resolveCityFilterCategory(category).scope === 'instance') {
        deps.onNotice?.({
          code: 'filter_showhide',
          type: cmd.type,
          detail: category,
        });
      }
      return { type: cmd.type, applied: true };
    }

    case 'camera_preset': {
      if (!deps.cameraApi) return fail(cmd.type, 'unwired', 'camera');
      const preset = typeof cmd.preset === 'string' ? cmd.preset : '';
      const ok = deps.cameraApi.applyPreset(preset);
      if (!ok) return fail(cmd.type, 'invalid_params', 'preset');
      return { type: cmd.type, applied: true };
    }

    case 'fly_path': {
      if (!deps.pathApi) return fail(cmd.type, 'unwired', 'path');
      const points = Array.isArray(cmd.points) ? cmd.points : [];
      if (points.length < 2) return fail(cmd.type, 'invalid_params', 'points');
      // 折线飞行只在 orbit 模式有语义（focusRef 通道）⇒ 先切模式。
      deps.setMode('orbit');
      const ok = deps.pathApi.flyPath(
        points.filter((p) => p && isFiniteNumber(p.x) && isFiniteNumber(p.z)),
      );
      if (!ok) return fail(cmd.type, 'invalid_params', 'points');
      return { type: cmd.type, applied: true };
    }

    default:
      // 未知类型：**显式提示**，不静默吞掉。
      return fail(cmd.type, 'unsupported_type');
  }
}
