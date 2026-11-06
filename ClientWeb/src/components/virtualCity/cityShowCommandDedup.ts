/**
 * cityShowCommandDedup —— 显示指令的「双帧合一」去重（批次 85 C2 修复）。
 *
 * 背景：同一条显示指令会被服务端**双帧**下发
 * （`game.virtual_city_system_event` kind=show_command + `game.virtual_city_show_command`），
 * 前端只应执行一次。批次 85 C2 复盘实证两处旧缺陷：
 *
 *   1. 旧实现把「已见过的 key」永久留在 LRU-128 集合里 —— 模型重试 / 用户重问
 *      产出的**相同**指令（无 trace_id 时 key = 整条 JSON）会被连带吞掉，
 *      「首条必达」不成立；
 *   2. 旧实现在**回调前**记账 —— 若 `onShowCommand` 抛错，双帧两条都被消费
 *      （第一条记账后抛、第二条命中去重），指令**静默**丢失且无任何报错。
 *
 * 语义（本模块钉死，单测见 `__tests__/cityShowCommandDedup.test.ts`）：
 *   - **首条必达**：key 未见过（或已过时间窗）一律放行；
 *   - **只吞邻近重复**：同一 key 在 `DEDUP_WINDOW_MS` 内的再次出现视为双帧孪生，
 *     只执行一次（双帧毫秒级先后到达；窗口取 2s 宽容网络抖动）；
 *   - **失败可重试**：`forgetShowCommand` 在送达回调抛错时撤销记账，孪生帧可重试；
 *   - 容量上限 `CAP` 条（Map 插入序 = 到达序，超限淘汰最旧）。
 *
 * key 口径与旧实现一致：`trace_id` 优先（服务端逐指令补发，见
 * `citysystem.emitCommands`），缺失时整条指令 JSON。
 */

import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

/** 去重时间窗（ms）：双帧孪生毫秒级到达；2s 覆盖网络抖动又不误伤用户重试。 */
export const DEDUP_WINDOW_MS = 2000;

/** 记账容量上限（防长会话无界增长）。 */
const CAP = 128;

/** key → 最近一次放行时刻（Map 插入序 = 到达序，超限淘汰最旧）。 */
const recent = new Map<string, number>();

function keyOf(cmd: VirtualCityDisplayCommand): string {
  return cmd.trace_id || JSON.stringify(cmd);
}

/**
 * 判定一条指令是否为「邻近重复」（双帧孪生）。
 *
 * **注意调用纪律**：本函数在**放行时**记账，调用方必须在回调成功后不再重复
 * 调用；回调抛错时必须调 `forgetShowCommand` 撤账（否则退化为旧缺陷 2）。
 */
export function isDuplicateCommand(cmd: VirtualCityDisplayCommand): boolean {
  const key = keyOf(cmd);
  const now = Date.now();
  const hit = recent.get(key);
  if (hit !== undefined && now - hit < DEDUP_WINDOW_MS) return true;
  // 首见 / 窗口过期 ⇒ 放行并记账（delete+set 把过期 key 挪到队尾）。
  recent.delete(key);
  recent.set(key, now);
  while (recent.size > CAP) {
    const oldest = recent.keys().next().value;
    if (oldest === undefined) break;
    recent.delete(oldest);
  }
  return false;
}

/**
 * 撤销记账（送达回调抛错时调用）：让双帧孪生有机会重试，指令不静默丢失。
 */
export function forgetShowCommand(cmd: VirtualCityDisplayCommand): void {
  recent.delete(keyOf(cmd));
}

/** 测试用：清空记账（生产代码勿调）。 */
export function resetShowCommandDedup(): void {
  recent.clear();
}
