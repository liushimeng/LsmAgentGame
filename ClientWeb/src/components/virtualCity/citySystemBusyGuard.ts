/**
 * 批次 87 P1-3 / P1-4 —— 「系统对话」输入锁（busy）的**乐观锁 + 看门狗**。
 *
 * 背景（方案 §3.2）：
 *   - P1-3：`busy` 过去只在收到 `agent_start` 才置 true ⇒ 发送后到 Agent 首个事件
 *     之间（首 token 延迟可达数秒）输入框仍可点 ⇒ 连点撞服务端并发限流 35060。
 *   - P1-4：`busy` 只由 `agent_end` / `error` 清 ⇒ 服务端重启 / WS 断连时**没有**
 *     这两个事件 ⇒ `busy` 永久为 true ⇒ 输入框永久禁用（死锁）。
 *
 * 本模块提供**四路解锁**所需的纯逻辑（无 React / 无 store 依赖 ⇒ 可零依赖单测）：
 *   1. `arm()`          —— 发送即置 busy（乐观锁）+ 启动看门狗计时（重复发送**重置**
 *                          计时，不叠加失控）；
 *   2. `settle()`       —— 收到 `agent_end` / `error` / `cleared` ⇒ 停表（busy 由
 *                          store 的 `applyEvent` 清）；
 *   3. 看门狗超时        —— 到点仍未收到结束事件 ⇒ 解锁 + 内联提示 + 全局 toast；
 *   4. `releaseOnDisconnect()` —— WS 断连 ⇒ 解锁 + 「正在重连」提示。
 */

/**
 * 看门狗超时阈值（ms）。
 *
 * 与服务端 `[city_system] request_timeout_ms`（默认 180000ms = 180s）**对齐并留
 * 20s 余量**：服务端超时后会回 `error` 事件，正常路径下看门狗先于它触发属异常；
 * 取 180s + 20s 保证「服务端已放弃但事件未送达」的窗口内前端一定解锁。
 */
export const BUSY_WATCHDOG_MS = 200_000;

/** 看门狗 / 输入锁的依赖（由调用方注入，便于单测）。 */
export interface BusyGuardDeps {
  /** 设置 busy（true = 锁定输入）。 */
  setBusy: (busy: boolean) => void;
  /** 读当前 busy（超时回调里判定「是否仍在等」）。 */
  isBusy: () => boolean;
  /** 内联提示：向会话流追加一条提示（渲染为红条 / 提示行）。 */
  appendInline: (text: string) => void;
  /** 全局 toast 上报（§7.1）。 */
  reportGlobal: (message: string) => void;
  /** 超时文案（**函数**：触发时才取值 ⇒ 语言切换后文案不陈旧）。 */
  timeoutText: () => string;
  /** 断连文案（同上，函数取值）。 */
  disconnectedText: () => string;
  /** 超时阈值（ms）；缺省 `BUSY_WATCHDOG_MS`（单测注入小值）。 */
  timeoutMs?: number;
}

/** 输入锁句柄。 */
export interface BusyGuard {
  /** 发送即调用：置 busy + 重置看门狗计时（重复调用不叠加计时器）。 */
  arm(): void;
  /** 正常结束（`agent_end` / `error` / `cleared`）⇒ 停表。 */
  settle(): void;
  /** 传输层断连 ⇒ 解锁 + 提示（未在等待时静默忽略，不打扰用户）。 */
  releaseOnDisconnect(): void;
  /** 卸载时清理定时器。 */
  dispose(): void;
  /** 当前是否处于「已上锁」状态。 */
  isArmed(): boolean;
}

/**
 * 创建输入锁（乐观 busy + 看门狗）。
 *
 * ⚠ `arm()` 会**重置**计时器：连续发送只保留最后一个计时器，避免 N 个定时器
 * 叠加导致「解锁时机不可预期」。
 */
export function createBusyGuard(deps: BusyGuardDeps): BusyGuard {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let armed = false;

  const clearTimer = () => {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  };

  const arm = () => {
    // 重置（不叠加）：先停掉旧计时器，再起新的。
    clearTimer();
    armed = true;
    // 乐观锁：发送即 busy，不等 `agent_start`（首 token 延迟可达数秒）。
    deps.setBusy(true);
    timer = setTimeout(() => {
      timer = null;
      if (!armed) return;
      armed = false;
      // 期间已被 `settle()` / 其它路径解锁 ⇒ 不再重复提示。
      if (!deps.isBusy()) return;
      deps.setBusy(false);
      const text = deps.timeoutText();
      deps.appendInline(text);
      deps.reportGlobal(text);
    }, deps.timeoutMs ?? BUSY_WATCHDOG_MS);
  };

  const settle = () => {
    armed = false;
    clearTimer();
  };

  const releaseOnDisconnect = () => {
    // 未在等待（没有在途轮次）⇒ 断连是常态，不打扰用户。
    if (!armed) return;
    settle();
    if (!deps.isBusy()) return;
    deps.setBusy(false);
    const text = deps.disconnectedText();
    deps.appendInline(text);
    deps.reportGlobal(text);
  };

  const dispose = () => {
    armed = false;
    clearTimer();
  };

  return {
    arm,
    settle,
    releaseOnDisconnect,
    dispose,
    isArmed: () => armed,
  };
}
