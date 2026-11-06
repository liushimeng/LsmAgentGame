/**
 * cityShowAdapter 单元测试（批次 81 起，批次 84 扩到 14 个显示原语）。
 *
 * 项目当前**没有** vitest / jest（package.json 无 test 脚本，见 CLAUDE.md §4
 * 对测试的要求），因此本文件采用**零依赖断言**写法：
 *   - 自带 `assert` / `eq` / `deepEq` 小 helper（不 import node:assert，
 *     避免 @types/node 缺失导致 tsc 报错）；
 *   - 导出 `runCityShowAdapterTests()`，由 `scripts/verify_city_show_adapter.mjs`
 *     用 esbuild 打包后调用（npm run verify:city-show-adapter）。
 *
 * 若将来引入 vitest，可把底部调用换成 `it('...', ...)` 用例，helper 保留。
 *
 * 批次 84 覆盖：
 *   - 14 个显示原语逐个断言（指令 → 动作序列）；
 *   - `highlight_district` 的 **color** 真的传给下游；
 *   - 未知 type ⇒ 返回「未支持」提示（**不抛错**、不静默）；
 *   - 缺参 / 未接线能力 ⇒ 显式提示。
 */

import {
  applyDisplayCommand,
  type CityShowDeps,
  type CityShowMode,
  type CityShowNotice,
} from '../cityShowAdapter';
import { districtCenter, type VirtualCityDistrictId } from '@/types/virtualCity';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';
// 批次 85：物体级显示控制单测（车辆注册表 / labels DOM / set_filter 类别粒度）
// —— 同一 verify 入口打包执行（模块加载即自跑）。
import './cityVehicleRegistry.test';
import './cityLabelsDom.test';
import './cityFilterCategories.test';
import './cityShowAdapterEntity.test';
import './cityShowCommandDedup.test';
import './cityShowNoticeRow.test';

// ── 零依赖断言 helper ────────────────────────────────────────

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[cityShowAdapter] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function deepEq(actual: unknown, expected: unknown, msg: string): void {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a !== b) {
    throw new Error(`[cityShowAdapter] ${msg}: expected ${b}, got ${a}`);
  }
}

// ── 记录型 deps（把「动作序列」记下来断言）─────────────────────

interface Harness {
  deps: CityShowDeps;
  focusRef: { current: { x: number; z: number } | null };
  selected: (VirtualCityDistrictId | null)[];
  modes: CityShowMode[];
  annotations: ({ text: string; district?: string } | null)[];
  highlights: { district: string; color?: string }[];
  layerCalls: { layer: string; visible: boolean }[];
  filterCalls: { category: string; mode: string }[];
  envCalls: { kind: string; arg: string }[];
  entityCalls: { id: string; kind?: string }[];
  pathCalls: { x: number; z: number }[][];
  presetCalls: string[];
  clears: number;
  notices: CityShowNotice[];
}

function makeHarness(): Harness {
  const focusRef: { current: { x: number; z: number } | null } = { current: null };
  const selected: (VirtualCityDistrictId | null)[] = [];
  const modes: CityShowMode[] = [];
  const annotations: ({ text: string; district?: string } | null)[] = [];
  const highlights: { district: string; color?: string }[] = [];
  const layerCalls: { layer: string; visible: boolean }[] = [];
  const filterCalls: { category: string; mode: string }[] = [];
  const envCalls: { kind: string; arg: string }[] = [];
  const entityCalls: { id: string; kind?: string }[] = [];
  const pathCalls: { x: number; z: number }[][] = [];
  const presetCalls: string[] = [];
  const notices: CityShowNotice[] = [];
  const h = {
    focusRef,
    selected,
    modes,
    annotations,
    highlights,
    layerCalls,
    filterCalls,
    envCalls,
    entityCalls,
    pathCalls,
    presetCalls,
    notices,
    clears: 0,
  } as Harness;
  const deps: CityShowDeps = {
    focusRef,
    setSelectedDistrict: (id) => { selected.push(id); },
    setMode: (m) => { modes.push(m); },
    setAnnotation: (text, district) => { annotations.push({ text, district }); },
    clearAnnotation: () => { annotations.push(null); },
    setHighlight: (district, color) => { highlights.push({ district, color }); },
    layerApi: {
      setLayerVisibility: (layer, visible) => {
        if (!['buildings', 'trees', 'vehicles', 'pedestrians', 'props', 'labels', 'water'].includes(layer)) {
          return false;
        }
        layerCalls.push({ layer, visible });
        return true;
      },
      setFilter: (category, mode) => {
        if (category && !['building', 'tree', 'vehicle', 'pedestrian', 'props', 'water', 'labels'].includes(category)) {
          return false;
        }
        filterCalls.push({ category, mode });
        return true;
      },
    },
    envApi: {
      setTimeOfDay: (phase, hour) => {
        if (!phase && hour === undefined) return false;
        envCalls.push({ kind: 'timeOfDay', arg: phase ?? String(hour) });
        return true;
      },
      setWeather: (weather) => {
        if (!['clear', 'rain', 'snow', 'fog', 'haze', 'sandstorm', 'storm'].includes(weather)) return false;
        envCalls.push({ kind: 'weather', arg: weather });
        return true;
      },
      setTimeSpeed: (speed) => {
        if (!['pause', 'slow', 'normal', 'fast', 'xfast'].includes(speed)) return false;
        envCalls.push({ kind: 'speed', arg: speed });
        return true;
      },
      clear: () => { h.clears += 1; },
    },
    entityApi: {
      resolve: (id, kind) => {
        entityCalls.push({ id, kind });
        if (id === 'resident:7') return { x: 3, z: 4, label: 'resident:7' };
        return null;
      },
    },
    pathApi: {
      flyPath: (points) => {
        pathCalls.push(points);
        return points.length >= 2;
      },
    },
    cameraApi: {
      applyPreset: (preset) => {
        if (!['bird', 'eye', 'street', 'top', 'free'].includes(preset)) return false;
        presetCalls.push(preset);
        return true;
      },
    },
    clearOverrides: () => { h.clears += 1; },
    onNotice: (n) => { notices.push(n); },
  };
  h.deps = deps;
  return h;
}

function cmd(p: Partial<VirtualCityDisplayCommand> & { type: VirtualCityDisplayCommand['type'] }): VirtualCityDisplayCommand {
  return p;
}

// ── 用例 ─────────────────────────────────────────────────────

export function runCityShowAdapterTests(): void {
  // 1. focus_district：选中区 + 相机聚焦该区中心（与 handleSelectDistrict 同口径）。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'focus_district', district: 'finance' }), h.deps);
    deepEq(h.selected, ['finance'], 'focus_district 应选中 finance');
    const c = districtCenter('finance');
    deepEq(h.focusRef.current, { x: c.x, z: c.z }, 'focus_district 应聚焦区中心');
    eq(h.modes.length, 0, 'focus_district 不应切换视角模式');
    eq(h.annotations.length, 0, 'focus_district 不应产生标注');
  }

  // 2. focus_point：任意世界坐标聚焦，不动选区。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'focus_point', x: 12.5, z: -7 }), h.deps);
    deepEq(h.focusRef.current, { x: 12.5, z: -7 }, 'focus_point 应写入坐标');
    eq(h.selected.length, 0, 'focus_point 不应改选区');
  }

  // 3. set_mode：合法模式生效，非法值显式提示（不静默）。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'set_mode', mode: 'fly' }), h.deps);
    deepEq(h.modes, ['fly'], 'set_mode=fly 应生效');
    const bad = applyDisplayCommand(cmd({ type: 'set_mode', mode: 'walk' }), h.deps);
    eq(bad.applied, false, '非法 mode 不应生效');
    eq(bad.notice?.code, 'invalid_params', '非法 mode 应给出提示');
    deepEq(h.modes, ['fly'], '非法 mode 不应改模式');
    eq(h.focusRef.current, null, 'set_mode 不应动聚焦');
  }

  // 4. highlight_district：改选中态 + **颜色真的传给下游**（批次 84）。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'highlight_district', district: 'tech' }), h.deps);
    deepEq(h.selected, ['tech'], 'highlight_district 应选中 tech');
    eq(h.focusRef.current, null, 'highlight_district 不应动相机');
    deepEq(h.highlights, [{ district: 'tech', color: undefined }], 'highlight 应下发高亮');

    const h2 = makeHarness();
    applyDisplayCommand(
      cmd({ type: 'highlight_district', district: 'tech', color: '#ff3366' }),
      h2.deps,
    );
    deepEq(
      h2.highlights,
      [{ district: 'tech', color: '#ff3366' }],
      'highlight_district 的 color 必须传给下游',
    );
  }

  // 5. annotate：写标注（带 / 不带 district）。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'annotate', text: '地震震中', district: 'oldtown' }), h.deps);
    deepEq(h.annotations, [{ text: '地震震中', district: 'oldtown' }], 'annotate 应带 district');
    applyDisplayCommand(cmd({ type: 'annotate', text: '全城广播' }), h.deps);
    deepEq(
      h.annotations,
      [{ text: '地震震中', district: 'oldtown' }, { text: '全城广播', district: undefined }],
      'annotate 可不带 district',
    );
  }

  // 6. clear：清选中 + 清标注 + 清全部显示覆盖。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'annotate', text: 'x' }), h.deps);
    applyDisplayCommand(cmd({ type: 'highlight_district', district: 'commerce' }), h.deps);
    applyDisplayCommand(cmd({ type: 'clear' }), h.deps);
    deepEq(h.selected, ['commerce', null], 'clear 应清空选区');
    deepEq(h.annotations, [{ text: 'x', district: undefined }, null], 'clear 应清空标注');
    eq(h.clears, 1, 'clear 应清掉全部显示覆盖');
  }

  // 7. 缺参 / 非法指令：显式提示且**无副作用**（显示指令是旁路，绝不能打崩场景）。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'focus_district' }), h.deps);
    applyDisplayCommand(cmd({ type: 'focus_point', x: 1 }), h.deps);
    applyDisplayCommand(cmd({ type: 'focus_point', x: Number.NaN, z: 2 }), h.deps);
    applyDisplayCommand(cmd({ type: 'highlight_district' }), h.deps);
    applyDisplayCommand(cmd({ type: 'annotate' }), h.deps);
    applyDisplayCommand(cmd({ type: 'annotate', text: '' }), h.deps);
    eq(h.selected.length, 0, '缺参指令不应改选区');
    eq(h.focusRef.current, null, '缺参指令不应动相机');
    eq(h.annotations.length, 0, '缺参指令不应产生标注');
    eq(h.modes.length, 0, '缺参指令不应切模式');
    eq(h.notices.length, 6, '每个缺参指令都应给出提示');
  }

  // 8. 未知 type：返回「未支持」提示（**不抛错、不静默**）。
  {
    const h = makeHarness();
    const r = applyDisplayCommand(
      { type: 'hologram' } as unknown as VirtualCityDisplayCommand,
      h.deps,
    );
    eq(r.applied, false, '未知指令不应生效');
    eq(r.notice?.code, 'unsupported_type', '未知指令应给出「未支持」提示');
    eq(r.notice?.type, 'hologram', '提示应带指令类型');
    eq(h.notices.length, 1, '未知指令应回调 onNotice');
    eq(h.selected.length, 0, '未知指令不应有副作用');
    eq(h.focusRef.current, null, '未知指令不应有副作用');
  }

  // ── 批次 84：新增 8 个原语 ────────────────────────────────────

  // 9. set_time_of_day：覆盖昼夜（phase / hour）。
  {
    const h = makeHarness();
    const r = applyDisplayCommand(cmd({ type: 'set_time_of_day', phase: 'night' }), h.deps);
    eq(r.applied, true, 'set_time_of_day 应生效');
    deepEq(h.envCalls, [{ kind: 'timeOfDay', arg: 'night' }], 'set_time_of_day 应下发相位');
  }

  // 10. set_weather：覆盖天气。
  {
    const h = makeHarness();
    const r = applyDisplayCommand(cmd({ type: 'set_weather', weather: 'sandstorm' }), h.deps);
    eq(r.applied, true, 'set_weather 应生效');
    deepEq(h.envCalls, [{ kind: 'weather', arg: 'sandstorm' }], 'set_weather 应下发天气');
  }

  // 11. set_time_speed：覆盖时间流速。
  {
    const h = makeHarness();
    const r = applyDisplayCommand(cmd({ type: 'set_time_speed', speed: 'xfast' }), h.deps);
    eq(r.applied, true, 'set_time_speed 应生效');
    deepEq(h.envCalls, [{ kind: 'speed', arg: 'xfast' }], 'set_time_speed 应下发流速');
  }

  // 12. focus_entity：解析到实体 ⇒ 聚焦；解析不到 ⇒ 显式提示。
  {
    const h = makeHarness();
    const r = applyDisplayCommand(
      cmd({ type: 'focus_entity', entity_id: 'resident:7', kind: 'resident' }),
      h.deps,
    );
    eq(r.applied, true, 'focus_entity 命中时应生效');
    deepEq(h.focusRef.current, { x: 3, z: 4 }, 'focus_entity 应聚焦实体坐标');
    deepEq(h.entityCalls, [{ id: 'resident:7', kind: 'resident' }], 'focus_entity 应走实体解析');

    const h2 = makeHarness();
    const r2 = applyDisplayCommand(
      cmd({ type: 'focus_entity', entity_id: 'resident:999', kind: 'resident' }),
      h2.deps,
    );
    eq(r2.applied, false, '实体不存在时不应生效');
    eq(r2.notice?.code, 'entity_not_found', '实体不存在应给出提示');
    eq(h2.focusRef.current, null, '实体不存在时不应动相机');
  }

  // 13. set_layer_visibility：图层显隐。
  {
    const h = makeHarness();
    const r = applyDisplayCommand(
      cmd({ type: 'set_layer_visibility', layer: 'vehicles', visible: false }),
      h.deps,
    );
    eq(r.applied, true, 'set_layer_visibility 应生效');
    deepEq(h.layerCalls, [{ layer: 'vehicles', visible: false }], '应下发图层开关');

    const h2 = makeHarness();
    const r2 = applyDisplayCommand(
      cmd({
        type: 'set_layer_visibility',
        layer: 'nope' as unknown as VirtualCityDisplayCommand['layer'],
        visible: true,
      }),
      h2.deps,
    );
    eq(r2.applied, false, '未知图层不应生效');
    eq(r2.notice?.code, 'invalid_params', '未知图层应给出提示');
  }

  // 14. set_filter：类别筛选（highlight / dim / 取消）。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'set_filter', category: 'building', mode: 'highlight' }), h.deps);
    deepEq(h.filterCalls, [{ category: 'building', mode: 'highlight' }], 'set_filter 应下发筛选');

    const h2 = makeHarness();
    applyDisplayCommand(cmd({ type: 'set_filter', category: 'building', mode: 'dim' }), h2.deps);
    deepEq(h2.filterCalls, [{ category: 'building', mode: 'dim' }], 'set_filter=dim 应下发');

    const h3 = makeHarness();
    applyDisplayCommand(cmd({ type: 'set_filter', category: '' }), h3.deps);
    deepEq(h3.filterCalls, [{ category: '', mode: 'highlight' }], 'category 空 = 取消筛选');

    const h4 = makeHarness();
    const r4 = applyDisplayCommand(cmd({ type: 'set_filter', category: 'nope' }), h4.deps);
    eq(r4.applied, false, '未知类别不应生效');
    eq(r4.notice?.code, 'unknown_category', '未知类别应给出提示');
  }

  // 15. camera_preset：相机预设。
  {
    const h = makeHarness();
    const r = applyDisplayCommand(cmd({ type: 'camera_preset', preset: 'bird' }), h.deps);
    eq(r.applied, true, 'camera_preset 应生效');
    deepEq(h.presetCalls, ['bird'], 'camera_preset 应下发预设');

    const h2 = makeHarness();
    const r2 = applyDisplayCommand(
      cmd({ type: 'camera_preset', preset: 'ufo' as unknown as VirtualCityDisplayCommand['preset'] }),
      h2.deps,
    );
    eq(r2.applied, false, '未知预设不应生效');
    eq(r2.notice?.code, 'invalid_params', '未知预设应给出提示');
  }

  // 16. fly_path：折线飞行（切 orbit + 下发航点）。
  {
    const h = makeHarness();
    const r = applyDisplayCommand(
      cmd({ type: 'fly_path', points: [{ x: 0, z: 0 }, { x: 10, z: 10 }] }),
      h.deps,
    );
    eq(r.applied, true, 'fly_path 应生效');
    deepEq(h.modes, ['orbit'], 'fly_path 应切到 orbit');
    deepEq(h.pathCalls, [[{ x: 0, z: 0 }, { x: 10, z: 10 }]], 'fly_path 应下发航点');

    const h2 = makeHarness();
    const r2 = applyDisplayCommand(cmd({ type: 'fly_path', points: [{ x: 1, z: 1 }] }), h2.deps);
    eq(r2.applied, false, '点数不足不应生效');
    eq(r2.notice?.code, 'invalid_params', '点数不足应给出提示');
  }

  // 17. 未接线能力 ⇒ 显式提示「本批未接引擎」（不静默吞掉）。
  {
    const bare: CityShowDeps = {
      focusRef: { current: null },
      setSelectedDistrict: () => {},
      setMode: () => {},
      setAnnotation: () => {},
      clearAnnotation: () => {},
    };
    const r1 = applyDisplayCommand(cmd({ type: 'set_weather', weather: 'rain' }), bare);
    eq(r1.applied, false, '未接线的 set_weather 不应生效');
    eq(r1.notice?.code, 'unwired', '未接线应给出「未接引擎」提示');
    const r2 = applyDisplayCommand(cmd({ type: 'focus_entity', entity_id: 'x' }), bare);
    eq(r2.notice?.code, 'unwired', '未接线的 focus_entity 应给出提示');
    const r3 = applyDisplayCommand(cmd({ type: 'set_layer_visibility', layer: 'trees', visible: true }), bare);
    eq(r3.notice?.code, 'unwired', '未接线的 set_layer_visibility 应给出提示');
    const r4 = applyDisplayCommand(cmd({ type: 'camera_preset', preset: 'top' }), bare);
    eq(r4.notice?.code, 'unwired', '未接线的 camera_preset 应给出提示');
    const r5 = applyDisplayCommand(cmd({ type: 'fly_path', points: [{ x: 0, z: 0 }, { x: 1, z: 1 }] }), bare);
    eq(r5.notice?.code, 'unwired', '未接线的 fly_path 应给出提示');
    const r6 = applyDisplayCommand(cmd({ type: 'highlight_district', district: 'tech' }), bare);
    eq(r6.notice?.code, 'unwired', '未接线的 highlight 色应给出提示');
  }

  // eslint-disable-next-line no-console
  console.log('[cityShowAdapter] 17 组断言全部通过');
}

// 直接执行（被打包器当作入口调用）时自动运行。
runCityShowAdapterTests();
