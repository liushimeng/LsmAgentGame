/**
 * 批次 87 —— 虚拟城市「系统对话」错误可见性 / 输入锁 单元测试。
 *
 * 零依赖断言写法（同 virtualCitySystemStore.test.ts）：自带 `eq` / `deepEq`
 * helper，导出 `runVirtualCitySystemErrorHandlingTests()`，由
 * `scripts/verify_virtual_city_system_batch84.mjs` 用 esbuild 打包后调用
 * （npm run verify:virtual-city-system）。
 *
 * 覆盖（方案 §6.2）：
 *   1. `game.error` 白名单码 ⇒ 内联错误消息 + 全局 toast（§4.3）；
 *   2. 非白名单码 ⇒ **忽略**（其它游戏的帧不得打扰用户）；
 *   3. 35059 ⇒ 置**本地停用态**（不只依赖 game.state 字段）；
 *   4. 乐观 busy + 看门狗超时解锁（§4.4，重复发送不叠加计时器）；
 *   5. 停用判定三态（§4.6：true / false / 字段缺失）。
 */

import {
  createBusyGuard,
  BUSY_WATCHDOG_MS,
} from '@/components/virtualCity/citySystemBusyGuard';
import {
  handleCitySystemErrorFrame,
  isCitySystemErrorCode,
  pickCitySystemError,
} from '@/components/virtualCity/citySystemErrorCodes';
import {
  isSystemAgentDisabledByState,
  resolveSystemAgentDisabled,
} from '@/components/virtualCity/citySystemAvailability';
import { applyRoomSwitchReset } from '@/components/virtualCity/citySystemRoomSwitch';
import { useVirtualCitySystemStore } from '@/store/virtualCitySystem.store';
import type { TKey } from '@/i18n';

// ── 零依赖断言 helper ────────────────────────────────────────

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(
      `[virtualCitySystemErrorHandling] ${msg}: expected ${String(expected)}, got ${String(actual)}`,
    );
  }
}

function deepEq(actual: unknown, expected: unknown, msg: string): void {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a !== b) {
    throw new Error(`[virtualCitySystemErrorHandling] ${msg}: expected ${b}, got ${a}`);
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** 记录一次「全局 toast 上报」的假实现。 */
interface Recorder {
  inline: string[];
  toast: string[];
  disabled: boolean[];
}

function makeRecorder(): Recorder {
  return { inline: [], toast: [], disabled: [] };
}

/** 假 i18n：返回 `T:<key>`，便于断言「用了哪条文案」。 */
const fakeText = (key: TKey): string => `T:${key}`;

export async function runVirtualCitySystemErrorHandlingTests(): Promise<void> {
  // ── 1. 白名单过滤（§4.3）──────────────────────────────────────────────
  {
    const whitelist = [35001, 35059, 35060, 35061, 35062, 35063, 35064, 35065];
    for (const code of whitelist) {
      eq(isCitySystemErrorCode(code), true, `${code} 应属于系统对话错误白名单`);
    }
    // 非白名单（其它游戏的帧）必须被忽略。
    for (const code of [35002, 35055, 35066, 30011, 40001, 0]) {
      eq(isCitySystemErrorCode(code), false, `${code} 不应属于系统对话错误白名单`);
    }
    eq(isCitySystemErrorCode(undefined), false, 'undefined 码不应命中');
    eq(isCitySystemErrorCode(null), false, 'null 码不应命中');
    eq(isCitySystemErrorCode('abc'), false, '非数字字符串码不应命中');
    eq(isCitySystemErrorCode('35059'), true, '数字字符串码应命中（归一化）');
  }

  // ── 2. 帧挑取：房间过滤 + 服务端原文 ────────────────────────────────
  {
    const picked = pickCitySystemError({ code: 35060, message: 'busy' }, 'room-1');
    eq(picked?.code, 35060, '白名单码应被挑出');
    eq(picked?.serverMessage, 'busy', '服务端原文应保留');
    eq(picked?.disablesAgent, false, '35060 不应置本地停用态');
    eq(picked?.messageKey, 'virtualCity.systemDialog.errorBusy', '35060 文案键');

    eq(
      pickCitySystemError({ code: 35060, room_id: 'room-2' }, 'room-1'),
      null,
      'room_id 不匹配的帧应被忽略',
    );
    eq(
      pickCitySystemError({ code: 35060, room_id: 'room-1' }, 'room-1')?.code,
      35060,
      'room_id 匹配的帧应被挑出',
    );
    eq(
      pickCitySystemError({ code: 99999 }, 'room-1'),
      null,
      '非白名单码应被忽略',
    );
    eq(
      pickCitySystemError(null, 'room-1'),
      null,
      '空帧应被忽略',
    );
    eq(
      pickCitySystemError({ code: 35059 }, 'room-1')?.disablesAgent,
      true,
      '35059 应置本地停用态',
    );
  }

  // ── 3. 双通道：内联红条 + 全局 toast（§4.3）─────────────────────────
  {
    const rec = makeRecorder();
    const handled = handleCitySystemErrorFrame(
      { code: 35060, message: '服务器忙' },
      'room-1',
      {
        appendInlineError: (text) => rec.inline.push(text),
        reportGlobal: (message) => rec.toast.push(message),
        setAgentDisabled: (d) => rec.disabled.push(d),
        text: fakeText,
      },
    );
    eq(handled, true, '白名单码应被处理');
    eq(rec.inline.length, 1, '应产生 1 条内联错误消息');
    eq(rec.toast.length, 1, '应产生 1 个全局 toast');
    eq(rec.inline[0], 'T:virtualCity.systemDialog.errorBusy', '内联文案应取 i18n 键');
    eq(rec.toast[0], rec.inline[0], '内联与 toast 文案应一致');
    eq(rec.disabled.length, 0, '35060 不应置本地停用态');
  }

  // ── 4. 非白名单码 ⇒ 完全忽略（不打扰用户）──────────────────────────
  {
    const rec = makeRecorder();
    const handled = handleCitySystemErrorFrame(
      { code: 30011, message: '观战者禁止输入' },
      'room-1',
      {
        appendInlineError: (text) => rec.inline.push(text),
        reportGlobal: (message) => rec.toast.push(message),
        setAgentDisabled: (d) => rec.disabled.push(d),
        text: fakeText,
      },
    );
    eq(handled, false, '非白名单码不应被处理');
    eq(rec.inline.length, 0, '非白名单码不应产生内联消息');
    eq(rec.toast.length, 0, '非白名单码不应产生 toast');
  }

  // ── 5. 35059 ⇒ 本地停用态 ───────────────────────────────────────────
  {
    const rec = makeRecorder();
    handleCitySystemErrorFrame(
      { code: 35059, message: '系统 Agent 未启用' },
      'room-1',
      {
        appendInlineError: (text) => rec.inline.push(text),
        reportGlobal: (message) => rec.toast.push(message),
        setAgentDisabled: (d) => rec.disabled.push(d),
        text: fakeText,
      },
    );
    deepEq(rec.disabled, [true], '35059 应置本地停用态 true');
    eq(rec.inline[0], 'T:virtualCity.systemDialog.errorDisabled', '35059 文案键');
    eq(rec.toast.length, 1, '35059 也应有全局 toast');
  }

  // ── 6. 停用判定三态（§4.6）─────────────────────────────────────────
  {
    eq(isSystemAgentDisabledByState(true), false, 'true ⇒ 不停用');
    eq(isSystemAgentDisabledByState(false), true, 'false ⇒ 停用');
    eq(isSystemAgentDisabledByState(undefined), false, '字段缺失 ⇒ 不停用（向后兼容）');
    eq(isSystemAgentDisabledByState(null), false, 'null ⇒ 不停用');

    eq(resolveSystemAgentDisabled(true, false), false, 'true + 无本地停用 ⇒ 可用');
    eq(resolveSystemAgentDisabled(false, false), true, 'false ⇒ 停用');
    eq(resolveSystemAgentDisabled(undefined, false), false, '字段缺失 ⇒ 可用');
    eq(resolveSystemAgentDisabled(undefined, true), true, '本地停用态 ⇒ 停用');
    eq(resolveSystemAgentDisabled(true, true), true, '本地停用态优先于 true');
  }

  // ── 7. store：setBusy / appendError / setAgentDisabled ──────────────
  {
    const store = useVirtualCitySystemStore.getState();
    store.reset();
    store.setAgentDisabled(false);
    store.setBusy(true);
    eq(useVirtualCitySystemStore.getState().busy, true, 'setBusy(true) 应生效');
    useVirtualCitySystemStore.getState().setBusy(false);
    eq(useVirtualCitySystemStore.getState().busy, false, 'setBusy(false) 应生效');

    useVirtualCitySystemStore.getState().setAgentDisabled(true);
    eq(
      useVirtualCitySystemStore.getState().agentDisabled,
      true,
      'setAgentDisabled(true) 应生效（本地停用态）',
    );
    useVirtualCitySystemStore.getState().setAgentDisabled(false);
    eq(
      useVirtualCitySystemStore.getState().agentDisabled,
      false,
      'setAgentDisabled(false) 应生效',
    );

    // appendError ⇒ 独立一条带 error 字段的 assistant 消息（内联红条渲染路径）。
    useVirtualCitySystemStore.getState().reset();
    useVirtualCitySystemStore.getState().appendError('服务端已停用');
    const msgs = useVirtualCitySystemStore.getState().messages;
    eq(msgs.length, 1, 'appendError 应产生 1 条消息');
    eq(msgs[0].role, 'assistant', '错误消息 role 应为 assistant');
    eq(msgs[0].error, '服务端已停用', '错误消息应带 error 文案');
    eq(useVirtualCitySystemStore.getState().busy, false, 'appendError 应解锁 busy');

    // 流式气泡不被错误消息打断（收尾为 streaming=false）。
    useVirtualCitySystemStore.getState().reset();
    useVirtualCitySystemStore.getState().applyEvent({ kind: 'agent_start', data: {} });
    useVirtualCitySystemStore.getState().applyEvent({ kind: 'text', data: { delta: '分析中' } });
    useVirtualCitySystemStore.getState().appendError('本轮超时');
    const msgs2 = useVirtualCitySystemStore.getState().messages;
    eq(msgs2.length, 2, '错误消息应独立追加，不并入助手气泡');
    eq(msgs2[0].text, '分析中', '助手气泡文本保留');
    eq(msgs2[0].streaming, false, '助手气泡应被收尾（streaming=false）');
    eq(msgs2[1].error, '本轮超时', '第 2 条是错误消息');
  }

  // ── 8. 乐观 busy + 看门狗超时解锁（§4.4）──────────────────────────
  {
    eq(BUSY_WATCHDOG_MS, 200_000, '看门狗阈值应为 200s（服务端 180s + 20s 余量）');

    const store = useVirtualCitySystemStore.getState();
    store.reset();
    store.setBusy(false);

    const rec = makeRecorder();
    const guard = createBusyGuard({
      setBusy: (b) => useVirtualCitySystemStore.getState().setBusy(b),
      isBusy: () => useVirtualCitySystemStore.getState().busy,
      appendInline: (text) => rec.inline.push(text),
      reportGlobal: (message) => rec.toast.push(message),
      timeoutText: () => '本轮超时未返回，请重试或稍后再问。',
      disconnectedText: () => '连接已断开，正在重连…',
      timeoutMs: 20,
    });

    // 发送即置 busy（乐观锁，不等 agent_start）。
    guard.arm();
    eq(useVirtualCitySystemStore.getState().busy, true, 'arm() 应立即置 busy（乐观锁）');
    eq(guard.isArmed(), true, 'arm() 后应处于上锁态');

    await sleep(80);
    eq(useVirtualCitySystemStore.getState().busy, false, '看门狗超时后应解锁 busy');
    eq(rec.inline.length, 1, '看门狗超时应追加 1 条内联提示');
    eq(rec.toast.length, 1, '看门狗超时应上报 1 次全局 toast');
    eq(rec.inline[0], '本轮超时未返回，请重试或稍后再问。', '内联提示文案');
    eq(guard.isArmed(), false, '超时后应解除上锁态');
    guard.dispose();
  }

  // ── 9. 重复发送：看门狗**重置**而非叠加（§4.4）─────────────────────
  {
    const store = useVirtualCitySystemStore.getState();
    store.reset();
    store.setBusy(false);

    const rec = makeRecorder();
    const guard = createBusyGuard({
      setBusy: (b) => useVirtualCitySystemStore.getState().setBusy(b),
      isBusy: () => useVirtualCitySystemStore.getState().busy,
      appendInline: (text) => rec.inline.push(text),
      reportGlobal: (message) => rec.toast.push(message),
      timeoutText: () => '超时',
      disconnectedText: () => '断连',
      timeoutMs: 40,
    });

    guard.arm();
    await sleep(25); // 距首次超时还剩 15ms
    guard.arm(); // 重复发送 ⇒ 重置计时（不叠加第二个定时器）
    await sleep(25); // 距首次计时点已过，但第二次计时才走了 25ms
    eq(rec.inline.length, 0, '重置后不应提前触发超时');
    await sleep(40);
    eq(rec.inline.length, 1, '两次 arm() 只应触发一次超时（不叠加失控）');
    eq(rec.toast.length, 1, '两次 arm() 只应上报一次 toast');
    guard.dispose();
  }

  // ── 10. settle（agent_end / error / cleared）⇒ 停表，不再超时 ───────
  {
    const store = useVirtualCitySystemStore.getState();
    store.reset();
    store.setBusy(false);

    const rec = makeRecorder();
    const guard = createBusyGuard({
      setBusy: (b) => useVirtualCitySystemStore.getState().setBusy(b),
      isBusy: () => useVirtualCitySystemStore.getState().busy,
      appendInline: (text) => rec.inline.push(text),
      reportGlobal: (message) => rec.toast.push(message),
      timeoutText: () => '超时',
      disconnectedText: () => '断连',
      timeoutMs: 20,
    });

    guard.arm();
    guard.settle();
    await sleep(70);
    eq(rec.inline.length, 0, 'settle() 后不应触发超时提示');
    eq(rec.toast.length, 0, 'settle() 后不应触发 toast');
    guard.dispose();
  }

  // ── 11. 断连解锁（第四路）──────────────────────────────────────────
  {
    const store = useVirtualCitySystemStore.getState();
    store.reset();
    store.setBusy(false);

    const rec = makeRecorder();
    const guard = createBusyGuard({
      setBusy: (b) => useVirtualCitySystemStore.getState().setBusy(b),
      isBusy: () => useVirtualCitySystemStore.getState().busy,
      appendInline: (text) => rec.inline.push(text),
      reportGlobal: (message) => rec.toast.push(message),
      timeoutText: () => '超时',
      disconnectedText: () => '连接已断开，正在重连…',
      timeoutMs: 1000,
    });

    // 未在等待时断连 ⇒ 静默忽略（不打扰用户）。
    guard.releaseOnDisconnect();
    eq(rec.toast.length, 0, '未在等待时断连不应上报');

    guard.arm();
    guard.releaseOnDisconnect();
    eq(useVirtualCitySystemStore.getState().busy, false, '断连后应解锁 busy');
    eq(rec.inline.length, 1, '断连应追加 1 条内联提示');
    eq(rec.toast.length, 1, '断连应上报 1 次全局 toast');
    eq(rec.inline[0], '连接已断开，正在重连…', '断连提示文案');
    guard.dispose();
  }

  // ── 12. 换房 ⇒ 清空会话（跨房残留收口，批次 87 收尾）─────────────────
  {
    const store = useVirtualCitySystemStore.getState();
    // 先造出「上一房」的残留：消息 + 系统信息 + busy + 本地停用态。
    store.reset();
    useVirtualCitySystemStore.getState().appendUser('上一房的问题');
    useVirtualCitySystemStore.getState().applyEvent({ kind: 'agent_start', data: {} });
    useVirtualCitySystemStore.getState().applyEvent({
      kind: 'tool_result',
      data: { tool: 'query_city_state', text: '上一房的城市状态', is_err: false, ts: 1 },
    });
    useVirtualCitySystemStore.getState().setBusy(true);
    useVirtualCitySystemStore.getState().setAgentDisabled(true);
    eq(
      useVirtualCitySystemStore.getState().messages.length > 0,
      true,
      '前置：上一房应残留消息',
    );

    // 换房 ⇒ 走统一的换房重置（hook 的 [roomId] effect 调用的同一入口）。
    applyRoomSwitchReset({
      reset: () => useVirtualCitySystemStore.getState().reset(),
      setAgentDisabled: (d) => useVirtualCitySystemStore.getState().setAgentDisabled(d),
    });

    const after = useVirtualCitySystemStore.getState();
    eq(after.messages.length, 0, '换房后旧房消息必须被清空（不得串台）');
    eq(after.systemInfo, null, '换房后 systemInfo 必须被清空');
    eq(after.busy, false, '换房后 busy 必须被解锁');
    eq(after.agentDisabled, false, '换房后本地停用态必须清零（由新房重新判定）');
  }

  // eslint-disable-next-line no-console
  console.log('[virtualCitySystemErrorHandling] 12 组断言全部通过');
}

// 直接执行（被打包器当作入口调用）时自动运行。
await runVirtualCitySystemErrorHandlingTests();
