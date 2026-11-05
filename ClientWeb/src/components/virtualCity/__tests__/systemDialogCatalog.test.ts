/**
 * systemDialogCatalog 单元测试（批次 84 §5.1）。
 *
 * 零依赖断言写法（同 cityShowAdapter.test.ts）：自带 `eq` / `deepEq` helper，
 * 由 `scripts/verify_virtual_city_system_batch84.mjs` 用 esbuild 打包后调用
 * （npm run verify:virtual-city-system）；模块加载即自跑。
 *
 * 覆盖：
 *   - 点击目录条目 ⇒ **只回填输入框**，**绝不自动发送**（安全约束）；
 *   - 回填文本格式：`注入 <名称>（L<默认强度>，<kind>）`；
 *   - 强度归一（缺省 / 越界 → 3）；
 *   - 检索参数构建（group / q / limit）。
 */

import {
  applyCatalogPick,
  buildCatalogPickText,
  catalogQuery,
  clampMagnitude,
  DEFAULT_MAGNITUDE,
} from '../systemDialogCatalog';
import type { VirtualCityScenarioCatalogItem } from '@/types/virtualCitySystem';

// ── 零依赖断言 helper ────────────────────────────────────────

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[systemDialogCatalog] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function deepEq(actual: unknown, expected: unknown, msg: string): void {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a !== b) {
    throw new Error(`[systemDialogCatalog] ${msg}: expected ${b}, got ${a}`);
  }
}

const ITEM: VirtualCityScenarioCatalogItem = {
  kind: 'earthquake__magnitude_5',
  name: '5 级地震',
  group: '自然灾害',
  description: '强震襲击市区',
  default_magnitude: 5,
  params: [],
};

export function runSystemDialogCatalogTests(): void {
  // 1. 回填文本格式：注入 <名称>（L<默认强度>，<kind>）。
  {
    eq(
      buildCatalogPickText(ITEM, '注入'),
      '注入 5 级地震（L5，earthquake__magnitude_5）',
      '回填文本格式',
    );
    eq(
      buildCatalogPickText(ITEM, 'Inject'),
      'Inject 5 级地震（L5，earthquake__magnitude_5）',
      '回填文本动词可本地化',
    );
  }

  // 2. 强度归一：缺省 / 越界 → 3（L1–L5）。
  {
    eq(clampMagnitude(undefined), DEFAULT_MAGNITUDE, '缺省强度 → 3');
    eq(clampMagnitude(0), 1, '越低下限 → 1');
    eq(clampMagnitude(9), 5, '越上限 → 5');
    eq(clampMagnitude(3), 3, '正常值原样');
  }

  // 3. **点条目只回填输入框，不发送消息**（安全约束的核心断言）。
  {
    let draft = '';
    let sent: string[] = [];
    // 模拟面板行为：onPick 只写 draft；submit 才会发送。
    const fillDraft = (text: string) => { draft = text; };
    const sendMessage = (text: string) => { sent.push(text); };
    const submit = () => {
      if (draft.trim()) {
        sendMessage(draft);
        draft = '';
      }
    };

    applyCatalogPick(ITEM, { fillDraft });
    eq(sent.length, 0, '点击条目不得触发发送');
    eq(draft, '注入 5 级地震（L5，earthquake__magnitude_5）', '点击条目应回填输入框');

    // 人工确认（点发送）后才真正发出。
    submit();
    eq(sent.length, 1, '人工确认后应发送一次');
    eq(sent[0], '注入 5 级地震（L5，earthquake__magnitude_5）', '发送内容 = 回填内容');
    eq(draft, '', '发送后输入框清空');
  }

  // 4. 检索参数构建（空 group / q 不下发；limit 夹取）。
  {
    deepEq(catalogQuery('', '', 30), { limit: '30' }, '空 group/q 不下发');
    deepEq(catalogQuery('公共卫生', '地震', 500), { group: '公共卫生', q: '地震', limit: '200' }, '参数构建');
    deepEq(catalogQuery('  ', ' x ', 0), { q: 'x', limit: '30' }, '空白裁剪 + limit=0 走缺省 30');
  }

  // eslint-disable-next-line no-console
  console.log('[systemDialogCatalog] 4 组断言全部通过');
}

// 直接执行（被打包器当作入口调用）时自动运行。
runSystemDialogCatalogTests();
