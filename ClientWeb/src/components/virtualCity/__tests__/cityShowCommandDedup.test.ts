/**
 * cityShowCommandDedup 单测（批次 85 C2 修复）——「首条必达、只吞邻近重复」。
 *
 * 契约（见模块头注释）：
 *   1. key 未见过 ⇒ 放行（**首条必达**）；
 *   2. 同 key 在时间窗内的再次出现 = 双帧孪生 ⇒ 吞掉（**只达一次**）；
 *   3. 不同 trace_id 的同体指令各自放行（服务端逐指令补 trace_id）；
 *   4. 无 trace_id 时 key = 整条 JSON：首条放行、邻近重复吞掉；
 *   5. `forgetShowCommand` 撤账后同 key 可重试（送达回调抛错不丢指令）；
 *   6. 时间窗过期后同 key **重新放行**（用户重试 / 模型重调不被永久吞掉）；
 *   7. 容量上限淘汰最旧记账（长会话无界增长）。
 *
 * 零依赖断言写法与 cityShowAdapter.test.ts 同款（项目无 vitest）。
 */

import {
  DEDUP_WINDOW_MS,
  forgetShowCommand,
  isDuplicateCommand,
  resetShowCommandDedup,
} from '../cityShowCommandDedup';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[cityShowCommandDedup] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function cmd(partial: Partial<VirtualCityDisplayCommand>): VirtualCityDisplayCommand {
  return { type: 'focus_entity', ...partial } as VirtualCityDisplayCommand;
}

/** 固定时钟 + 恢复（时间窗过期用例）。 */
function withFakeNow<T>(start: number, fn: (advance: (ms: number) => void) => T): T {
  const orig = Date.now;
  let now = start;
  Date.now = () => now;
  try {
    return fn((ms) => { now += ms; });
  } finally {
    Date.now = orig;
  }
}

resetShowCommandDedup();

// 1+2. 首条必达；邻近重复（双帧孪生）只吞一次。
withFakeNow(1_000_000, (advance) => {
  const a = cmd({ entity_id: 'vehicle:nope-b85', trace_id: 'tr-a' });
  eq(isDuplicateCommand(a), false, '首条必须放行');
  eq(isDuplicateCommand(a), true, '同 trace_id 的邻近重复必须吞掉');
  advance(DEDUP_WINDOW_MS + 1);
  eq(isDuplicateCommand(a), false, '时间窗过期后同 key 必须重新放行（用户重试不被吞）');
});

// 3. 不同 trace_id 的同体指令各自放行（服务端逐指令补 trace_id）。
withFakeNow(2_000_000, () => {
  const b1 = cmd({ entity_id: 'vehicle:nope-b85', trace_id: 'tr-b1' });
  const b2 = cmd({ entity_id: 'vehicle:nope-b85', trace_id: 'tr-b2' });
  eq(isDuplicateCommand(b1), false, 'trace_id 不同必须各自放行（第 1 条）');
  eq(isDuplicateCommand(b2), false, 'trace_id 不同必须各自放行（第 2 条）');
});

// 4. 无 trace_id：key = 整条 JSON —— 首条放行、邻近重复吞掉。
withFakeNow(3_000_000, () => {
  const c = cmd({ entity_id: 'vehicle:nope-b85', kind: 'vehicle' });
  eq(isDuplicateCommand(c), false, '无 trace_id 首条必须放行');
  eq(isDuplicateCommand(c), true, '无 trace_id 的邻近重复必须吞掉');
});

// 5. forgetShowCommand 撤账 ⇒ 同 key 可重试（送达抛错不丢指令）。
withFakeNow(4_000_000, () => {
  const d = cmd({ entity_id: 'vehicle:x', trace_id: 'tr-d' });
  eq(isDuplicateCommand(d), false, 'forget 用例：首条放行');
  forgetShowCommand(d);
  eq(isDuplicateCommand(d), false, 'forgetShowCommand 后必须可重试');
});

// 6. 容量上限：灌满 CAP+ 后最旧记账被淘汰，可再放行。
withFakeNow(5_000_000, () => {
  const first = cmd({ entity_id: 'e-0', trace_id: 'tr-cap-0' });
  eq(isDuplicateCommand(first), false, 'cap 用例：首条放行');
  for (let i = 1; i < 140; i += 1) {
    isDuplicateCommand(cmd({ entity_id: `e-${i}`, trace_id: `tr-cap-${i}` }));
  }
  eq(isDuplicateCommand(first), false, '超容量后最旧记账必须被淘汰（可再放行）');
});

resetShowCommandDedup();

// eslint-disable-next-line no-console
console.log('[cityShowCommandDedup] all assertions passed');
