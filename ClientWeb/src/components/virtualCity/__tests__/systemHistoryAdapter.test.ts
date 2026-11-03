/**
 * systemHistoryAdapter 单元测试（批次 81 §7.3-6 历史恢复）。
 *
 * 零依赖断言写法（同 cityShowAdapter.test.ts）：自带 `eq` / `deepEq` helper，
 * 导出 `runSystemHistoryAdapterTests()`，由 `scripts/verify_system_history_adapter.mjs`
 * 用 esbuild 打包后调用（npm run verify:system-history）；模块加载即自跑。
 *
 * 覆盖：wire → VirtualCitySystemMessage 的 role 映射、tool_result 折叠、
 * 同轮 assistant 合并、thinking 跳过、孤儿/未匹配 tool_result、id 稳定性、
 * 非法输入不抛错。
 */

import { normalizeSystemHistory } from '../systemHistoryAdapter';
import type { VirtualCitySystemWireMessage } from '@/types/virtualCitySystem';

// ── 零依赖断言 helper ────────────────────────────────────────

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[systemHistoryAdapter] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function deepEq(actual: unknown, expected: unknown, msg: string): void {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a !== b) {
    throw new Error(`[systemHistoryAdapter] ${msg}: expected ${b}, got ${a}`);
  }
}

export function runSystemHistoryAdapterTests(): void {
  // 1. 非法输入：一律 []，不抛错（历史是旁路恢复，绝不能打崩对话面板）。
  {
    deepEq(normalizeSystemHistory(null), [], 'null → []');
    deepEq(normalizeSystemHistory(undefined), [], 'undefined → []');
    deepEq(normalizeSystemHistory({}), [], '对象 → []');
    deepEq(normalizeSystemHistory('x'), [], '字符串 → []');
    deepEq(normalizeSystemHistory([null, 42, 'x']), [], '垃圾元素被跳过');
  }

  // 2. 单轮文本：user + assistant 各成一条；id = h<idx>-<role> 稳定。
  {
    const raw: VirtualCitySystemWireMessage[] = [
      { role: 'user', content: [{ type: 'text', text: '城市现在怎么样？' }] },
      { role: 'assistant', content: [{ type: 'text', text: '一切正常。' }] },
    ];
    const out = normalizeSystemHistory(raw);
    eq(out.length, 2, '单轮 2 条');
    eq(out[0].role, 'user', '第 1 条 user');
    eq(out[0].text, '城市现在怎么样？', 'user 文本');
    eq(out[0].id, 'h0-user', 'user id 稳定');
    eq(out[1].role, 'assistant', '第 2 条 assistant');
    eq(out[1].text, '一切正常。', 'assistant 文本');
    eq(out[1].id, 'h1-assistant', 'assistant id 稳定');
    eq(out[1].streaming, undefined, '历史消息非 streaming');
    eq(out[1].tool_calls, undefined, '无工具调用不带 tool_calls');
  }

  // 3. 工具轮：assistant(tool_use) + user(tool_result) 折叠 + 尾部 assistant 合并
  //    ⇒ 仍是一条 user 气泡 + 一条 assistant 气泡；tool_calls 回填 text/is_err。
  {
    const raw: VirtualCitySystemWireMessage[] = [
      { role: 'user', content: [{ type: 'text', text: '注入暴雨' }] },
      {
        role: 'assistant',
        content: [
          { type: 'thinking', budget: 1024 },
          { type: 'text', text: '我先看看城市状态。' },
          { type: 'tool_use', id: 'tu1', name: 'query_city_state', input: { area: 'all' } },
          { type: 'tool_use', id: 'tu2', name: 'inject_scenario', input: { kind: 'rainstorm' } },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'tool_result',
            tool_use_id: 'tu1',
            content: [{ type: 'text', text: '[step 1/8] 城市状态 OK' }],
            is_error: false,
          },
          {
            type: 'tool_result',
            tool_use_id: 'tu2',
            content: [{ type: 'text', text: '[step 2/8] 注入失败' }],
            is_error: true,
          },
        ],
      },
      { role: 'assistant', content: [{ type: 'text', text: '已注入暴雨场景。' }] },
    ];
    const out = normalizeSystemHistory(raw);
    eq(out.length, 2, '工具轮折叠成 2 条');
    eq(out[0].role, 'user', 'user 气泡保留');
    const a = out[1];
    eq(a.role, 'assistant', 'assistant 单气泡');
    eq(a.text, '我先看看城市状态。\n已注入暴雨场景。', '同轮 assistant 文本累加');
    eq(a.tool_calls?.length, 2, '还原 2 条 tool_calls');
    eq(a.tool_calls?.[0].tool, 'query_city_state', 'tool 名');
    eq(a.tool_calls?.[0].input, '{"area":"all"}', 'input stringify');
    eq(a.tool_calls?.[0].text, '[step 1/8] 城市状态 OK', 'tool_result 折叠回填');
    eq(a.tool_calls?.[0].is_err, false, '成功标记');
    eq(a.tool_calls?.[1].tool, 'inject_scenario', 'tool 名 2');
    eq(a.tool_calls?.[1].is_err, true, '失败标记');
    eq(a.tool_calls?.[1].ts, 0, '历史无时间戳 ⇒ ts=0（渲染隐藏）');
  }

  // 4. 孤儿 tool_result（无上一条 assistant）⇒ 合成 assistant 承接，不丢结果。
  {
    const raw: VirtualCitySystemWireMessage[] = [
      {
        role: 'user',
        content: [
          { type: 'tool_result', tool_use_id: 'x', content: [{ type: 'text', text: 'R' }] },
        ],
      },
    ];
    const out = normalizeSystemHistory(raw);
    eq(out.length, 1, '孤儿 tool_result 合成 1 条');
    eq(out[0].role, 'assistant', '合成 assistant');
    eq(out[0].tool_calls?.length, 1, '承接 1 条记录');
    eq(out[0].tool_calls?.[0].text, 'R', '结果文本不丢');
  }

  // 5. 未匹配 tool_use_id 的 tool_result ⇒ 补一条已完成记录（不覆盖、不丢）。
  {
    const raw: VirtualCitySystemWireMessage[] = [
      {
        role: 'assistant',
        content: [{ type: 'tool_use', id: 'a1', name: 't1', input: {} }],
      },
      {
        role: 'user',
        content: [
          { type: 'tool_result', tool_use_id: 'zzz', content: [{ type: 'text', text: 'X' }] },
        ],
      },
    ];
    const out = normalizeSystemHistory(raw);
    eq(out.length, 1, '折叠进同轮 assistant');
    eq(out[0].tool_calls?.length, 2, '未匹配结果追加记录');
    eq(out[0].tool_calls?.[0].text, '', '原记录未被误填');
    eq(out[0].tool_calls?.[1].text, 'X', '追加记录带结果');
  }

  // 6. thinking / 空消息跳过：纯 thinking assistant 不产生空气泡。
  {
    const raw: VirtualCitySystemWireMessage[] = [
      { role: 'user', content: [{ type: 'text', text: 'q' }] },
      { role: 'assistant', content: [{ type: 'thinking', budget: 512 }] },
      { role: 'user', content: [] },
    ];
    const out = normalizeSystemHistory(raw);
    eq(out.length, 1, '纯 thinking / 空消息被跳过');
    eq(out[0].role, 'user', '只剩 user');
  }

  // 7. 已归一化形状（id + text）防御性透传：role 校验、id 保留。
  {
    const out = normalizeSystemHistory([
      { id: 'x1', role: 'user', text: 'hi', ts: 123 },
      { id: 'x2', role: 'alien', text: 'zz' },
    ]);
    eq(out.length, 1, '非法 role 透传被拒');
    eq(out[0].id, 'x1', 'id 保留');
    eq(out[0].ts, 123, 'ts 保留');
    eq(out[0].role, 'user', 'role 保留');
  }

  // 8. id 稳定性：同一份历史两次归一化逐位相等（React key 刷新前后一致）。
  {
    const raw: VirtualCitySystemWireMessage[] = [
      { role: 'user', content: [{ type: 'text', text: 'a' }] },
      { role: 'assistant', content: [{ type: 'tool_use', id: 't', name: 'n', input: null }] },
      {
        role: 'user',
        content: [{ type: 'tool_result', tool_use_id: 't', content: [{ type: 'text', text: 'r' }] }],
      },
      { role: 'assistant', content: [{ type: 'text', text: 'done' }] },
    ];
    const a = normalizeSystemHistory(raw);
    const b = normalizeSystemHistory(raw);
    deepEq(
      a.map((m) => m.id),
      b.map((m) => m.id),
      '两次归一化 id 一致',
    );
  }

  // 9. tool_result.content 兼容纯字符串（防御）。
  {
    const out = normalizeSystemHistory([
      { role: 'assistant', content: [{ type: 'tool_use', id: 'i', name: 'f', input: {} }] },
      { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'i', content: 'str-result' }] },
    ]);
    eq(out[0].tool_calls?.[0].text, 'str-result', '字符串 content 被收集');
  }

  // eslint-disable-next-line no-console
  console.log('[systemHistoryAdapter] 9 组断言全部通过');
}

// 直接执行（被打包器当作入口调用）时自动运行。
runSystemHistoryAdapterTests();
