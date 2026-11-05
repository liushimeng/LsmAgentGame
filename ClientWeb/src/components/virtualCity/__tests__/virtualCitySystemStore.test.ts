/**
 * virtualCitySystemStore 单元测试（批次 84）。
 *
 * 零依赖断言写法（同 cityShowAdapter.test.ts）：自带 `eq` / `deepEq` helper，
 * 导出 `runVirtualCitySystemStoreTests()`，由
 * `scripts/verify_virtual_city_system_batch84.mjs` 用 esbuild 打包后调用
 * （npm run verify:virtual-city-system）；模块加载即自跑。
 *
 * 覆盖：
 *   - `agent_message` 事件 ⇒ 产生一条**独立的来信消息**（role='agent'），
 *     不并入助手气泡、不打断流式回复；
 *   - `query_city_state` 的工具结果 ⇒ 落 store 的 systemInfo（信息面板用）；
 *   - cleared / reset ⇒ 清空 systemInfo。
 */

import { useVirtualCitySystemStore } from '@/store/virtualCitySystem.store';

// ── 零依赖断言 helper ────────────────────────────────────────

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[virtualCitySystemStore] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

export function runVirtualCitySystemStoreTests(): void {
  const store = useVirtualCitySystemStore.getState();

  // 1. agent_message ⇒ 一条独立来信消息。
  {
    store.reset();
    store.applyEvent({
      kind: 'agent_message',
      data: {
        from: 'LsmAgentGame-City-Chrome',
        text: '今日 A 股收盘：沪指 3120.4，涨 0.8%。',
        ts: 1730000000000,
      },
    });
    const msgs = useVirtualCitySystemStore.getState().messages;
    eq(msgs.length, 1, 'agent_message 应产生 1 条消息');
    eq(msgs[0].role, 'agent', '来信消息 role 应为 agent');
    eq(msgs[0].agent_from, 'LsmAgentGame-City-Chrome', '来信消息应带来源 Agent');
    eq(msgs[0].text, '今日 A 股收盘：沪指 3120.4，涨 0.8%。', '来信消息应带正文');
    eq(msgs[0].ts, 1730000000000, '来信消息应带时间戳');
  }

  // 2. 来信**不并入**助手气泡（助手流式消息保持独立）。
  {
    store.reset();
    store.applyEvent({ kind: 'agent_start', data: {} });
    store.applyEvent({ kind: 'text', data: { delta: '正在分析…' } });
    store.applyEvent({
      kind: 'agent_message',
      data: { from: 'LsmAgentGame-City-Human', text: '居民们感到不安。', ts: 1730000001000 },
    });
    const msgs = useVirtualCitySystemStore.getState().messages;
    eq(msgs.length, 2, '助手消息 + 来信消息共 2 条');
    eq(msgs[0].role, 'assistant', '第 1 条仍是助手消息');
    eq(msgs[0].text, '正在分析…', '助手消息文本不受来信影响');
    eq(msgs[1].role, 'agent', '第 2 条是来信');
  }

  // 3. 空正文来信 ⇒ 不产生消息（不刷屏）。
  {
    store.reset();
    store.applyEvent({ kind: 'agent_message', data: { from: 'X', text: '' } });
    eq(useVirtualCitySystemStore.getState().messages.length, 0, '空来信不产生消息');
  }

  // 4. query_city_state 的工具结果 ⇒ systemInfo（系统信息面板）。
  {
    store.reset();
    store.applyEvent({ kind: 'tool_start', data: { tool: 'query_city_state', input: '{}' } });
    store.applyEvent({
      kind: 'tool_result',
      data: { tool: 'query_city_state', text: '人口 120000，CPI 1.02', is_err: false, ts: 1 },
    });
    eq(
      useVirtualCitySystemStore.getState().systemInfo,
      '人口 120000，CPI 1.02',
      'query_city_state 结果应落 systemInfo',
    );
  }

  // 5. 非 query_city_state 的工具结果 ⇒ 不污染 systemInfo。
  {
    store.reset();
    store.applyEvent({ kind: 'tool_start', data: { tool: 'inject_scenario', input: '{}' } });
    store.applyEvent({
      kind: 'tool_result',
      data: { tool: 'inject_scenario', text: '已注入', is_err: false, ts: 1 },
    });
    eq(useVirtualCitySystemStore.getState().systemInfo, null, '其它工具不应写 systemInfo');
  }

  // 6. cleared / reset ⇒ 清空 systemInfo。
  {
    store.reset();
    store.applyEvent({ kind: 'tool_start', data: { tool: 'query_city_state', input: '{}' } });
    store.applyEvent({
      kind: 'tool_result',
      data: { tool: 'query_city_state', text: 'x', is_err: false, ts: 1 },
    });
    store.applyEvent({ kind: 'cleared', data: {} });
    eq(useVirtualCitySystemStore.getState().systemInfo, null, 'cleared 应清空 systemInfo');

    store.applyEvent({ kind: 'tool_start', data: { tool: 'query_city_state', input: '{}' } });
    store.applyEvent({
      kind: 'tool_result',
      data: { tool: 'query_city_state', text: 'y', is_err: false, ts: 1 },
    });
    store.reset();
    eq(useVirtualCitySystemStore.getState().systemInfo, null, 'reset 应清空 systemInfo');
  }

  // eslint-disable-next-line no-console
  console.log('[virtualCitySystemStore] 6 组断言全部通过');
}

// 直接执行（被打包器当作入口调用）时自动运行。
runVirtualCitySystemStoreTests();
