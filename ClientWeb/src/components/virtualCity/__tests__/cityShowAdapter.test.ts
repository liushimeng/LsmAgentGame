/**
 * cityShowAdapter 单元测试（批次 81）。
 *
 * 项目当前**没有** vitest / jest（package.json 无 test 脚本，见 CLAUDE.md §4
 * 对测试的要求），因此本文件采用**零依赖断言**写法：
 *   - 自带 `assert` / `eq` / `deepEq` 小 helper（不 import node:assert，
 *     避免 @types/node 缺失导致 tsc 报错）；
 *   - 导出 `runCityShowAdapterTests()`，由 `scripts/verify_city_show_adapter.mjs`
 *     用 esbuild 打包后调用（npm run verify:city-show-adapter）。
 *
 * 若将来引入 vitest，可把底部调用换成 `it('...', ...)` 用例，helper 保留。
 */

import {
  applyDisplayCommand,
  type CityShowDeps,
  type CityShowMode,
} from '../cityShowAdapter';
import { districtCenter, type VirtualCityDistrictId } from '@/types/virtualCity';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

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
}

function makeHarness(): Harness {
  const focusRef: { current: { x: number; z: number } | null } = { current: null };
  const selected: (VirtualCityDistrictId | null)[] = [];
  const modes: CityShowMode[] = [];
  const annotations: ({ text: string; district?: string } | null)[] = [];
  const deps: CityShowDeps = {
    focusRef,
    setSelectedDistrict: (id) => { selected.push(id); },
    setMode: (m) => { modes.push(m); },
    setAnnotation: (text, district) => { annotations.push({ text, district }); },
    clearAnnotation: () => { annotations.push(null); },
  };
  return { deps, focusRef, selected, modes, annotations };
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

  // 3. set_mode：合法模式生效，非法值静默忽略。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'set_mode', mode: 'fly' }), h.deps);
    deepEq(h.modes, ['fly'], 'set_mode=fly 应生效');
    applyDisplayCommand(cmd({ type: 'set_mode', mode: 'walk' }), h.deps);
    deepEq(h.modes, ['fly'], '非法 mode 应被忽略');
    eq(h.focusRef.current, null, 'set_mode 不应动聚焦');
  }

  // 4. highlight_district：只改选中态，不动相机。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'highlight_district', district: 'tech' }), h.deps);
    deepEq(h.selected, ['tech'], 'highlight_district 应选中 tech');
    eq(h.focusRef.current, null, 'highlight_district 不应动相机');
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

  // 6. clear：清选中 + 清标注。
  {
    const h = makeHarness();
    applyDisplayCommand(cmd({ type: 'annotate', text: 'x' }), h.deps);
    applyDisplayCommand(cmd({ type: 'highlight_district', district: 'commerce' }), h.deps);
    applyDisplayCommand(cmd({ type: 'clear' }), h.deps);
    deepEq(h.selected, ['commerce', null], 'clear 应清空选区');
    deepEq(h.annotations, [{ text: 'x', district: undefined }, null], 'clear 应清空标注');
  }

  // 7. 缺参 / 非法指令：一律静默忽略（显示指令是旁路，绝不能打崩场景）。
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
  }

  // 8. 未知 type：静默忽略（后端新增指令时前端不崩）。
  {
    const h = makeHarness();
    applyDisplayCommand({ type: 'hologram' } as unknown as VirtualCityDisplayCommand, h.deps);
    eq(h.selected.length, 0, '未知指令不应有副作用');
    eq(h.focusRef.current, null, '未知指令不应有副作用');
  }

  // eslint-disable-next-line no-console
  console.log('[cityShowAdapter] 8 组断言全部通过');
}

// 直接执行（被打包器当作入口调用）时自动运行。
runCityShowAdapterTests();
