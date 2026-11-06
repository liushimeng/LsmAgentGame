/**
 * 批次 87 收尾 —— **换房时的会话状态重置**（纯逻辑，可零依赖单测）。
 *
 * 背景：`useVirtualCitySystemStore` 是**单例**（跨房间共享），而会话消息
 * `messages` 只在新房的 history 帧回来时才被 `setMessages` 覆盖 ⇒
 *   - 新房**有**历史 ⇒ 旧房消息被覆盖（看不出问题）；
 *   - 新房**无**历史 ⇒ **旧房的对话内容直接显示在新房面板里**（跨房串台，
 *     且旧房内容会随 history 请求被回灌/清空，时序上也不自洽）。
 *
 * ⇒ 换房时**先清空会话**，再走历史恢复链路（`historyApplyRef` /
 * `historyResolvedRef` / `sawLiveRef` 的先到先得与「迟到历史不覆盖新对话」
 * 语义保持不变）：
 *
 *   1. `reset()` 清 messages / busy / systemInfo（跨房不该残留）；
 *   2. `setAgentDisabled(false)` 清本地停用态（停用与否由**新房**的
 *      `game.state.system_agent_enabled` / 35059 重新判定）。
 *
 * ⚠ 只清「会话内容」，**不动** UI 偏好（`dialogPos` / `minimized`）。
 */

/** 换房重置的目标（由调用方注入 store 动作，便于单测）。 */
export interface SystemRoomSwitchTarget {
  /** store 的 `reset()`：清 messages / busy / systemInfo。 */
  reset: () => void;
  /** 置本地停用态。 */
  setAgentDisabled: (disabled: boolean) => void;
}

/**
 * 换房 ⇒ 清空会话 + 清本地停用态。
 *
 * @param target store 动作集合。
 */
export function applyRoomSwitchReset(target: SystemRoomSwitchTarget): void {
  target.reset();
  target.setAgentDisabled(false);
}
