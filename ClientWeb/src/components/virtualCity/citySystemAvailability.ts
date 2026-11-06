/**
 * 批次 87 §4.6 —— 「系统对话是否停用」的**前端判定契约**（纯函数，可零依赖单测）。
 *
 * 契约表（方案 §4.6）：
 *
 * | `game.state.system_agent_enabled` | 面板      | 发送 |
 * |----------------------------------|-----------|------|
 * | `true`                            | 正常      | 允许 |
 * | `false`                           | 内联红条  | 禁止 |
 * | **字段缺失**（旧后端）            | 正常      | 允许（向后兼容） |
 *
 * ⇒ 判定必须是 **`=== false`** 的严格比较**：字段缺失（`undefined`）与 `null`
 * 一律视为「可用」。批次 87 的 P0-1（后端 `game.state` 漏盖该字段 ⇒ 前端每 8s
 * 轮询把状态刷成 `false` ⇒ 假停用）正是在这条契约上翻车 ⇒ 判定逻辑锁进单测。
 *
 * 另有一路**本地停用态**（`localDisabled`）：服务端明确回 35059「系统对话已停用」
 * 时由前端写入 store（不依赖 `game.state` 字段 ⇒ 后端漏盖 / 旧后端时也能正确表现）。
 */

/**
 * 仅当 `game.state.system_agent_enabled` **明确为 false** 时才判定为停用。
 *
 * @param systemAgentEnabled `game.state` 里的原始值（可能 undefined / null / 非布尔）。
 */
export function isSystemAgentDisabledByState(systemAgentEnabled: unknown): boolean {
  return systemAgentEnabled === false;
}

/**
 * 合并两路停用信号 ⇒ 最终「是否停用」。
 *
 * @param systemAgentEnabled `game.state.system_agent_enabled`（原始值）。
 * @param localDisabled 本地停用态（35059 ⇒ true）。
 */
export function resolveSystemAgentDisabled(
  systemAgentEnabled: unknown,
  localDisabled: boolean | undefined,
): boolean {
  return localDisabled === true || isSystemAgentDisabledByState(systemAgentEnabled);
}
