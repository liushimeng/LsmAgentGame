/**
 * useVirtualCitySystemAgent —— 订阅「系统对话」WS 帧 → store，并提供发送 / 清空。
 *
 * 帧协议（批次 81 方案 §3.5 / §6.1）：
 *   S→C game.virtual_city_system_event    {room_id, kind, data}   → store.applyEvent
 *   S→C game.virtual_city_system_history  {room_id, messages[]}  → 归一化 → store.setMessages
 *   S→C game.virtual_city_show_command    {room_id, command}     → onShowCommand（驱动 3D）
 *   C→S game.virtual_city_system_message  {room_id, text}
 *   C→S game.virtual_city_system_reset    {room_id}
 *   C→S game.virtual_city_system_history  {room_id}              → 请求历史（§7.3-6）
 *
 * 历史恢复（§7.3-6，WS 为主 + HTTP 兜底）：
 *   - 挂载（含换房）时若 WS 已 open 立即请求一次；未 open 则挂 `wsClient.onOpen`
 *     连上后补发——`wsClient.send` 自带 CONNECTING 缓冲，但只经 onOpen 单点触发，
 *     避免与缓冲 flush 双发；
 *   - WS 请求 2s 未回（或 WS 不可用）⇒ HTTP `GET .../system_agent/history` 兜底；
 *   - WS history 帧与 HTTP 兜底**先到先得**（historyResolvedRef 只一路生效）；
 *   - 重连不无脑重发：已恢复且本地有消息 ⇒ 跳过；仅空会话补拉（防重连风暴）。
 *
 * WS 连接由 `AppLayout` 唯一持有（§16）；本 hook 只按帧名订阅并过滤 room_id
 * （规则同 `useVirtualCity`：无 room_id 的帧不过滤）。
 *
 * 错误展示（§7.1 双通道）：`error` 事件 → 消息内联红条（store 落 error 字段）
 * + `reportGlobalError` 全局 toast；两者都做，绝不吞进 console。
 */

import { useCallback, useEffect, useRef } from 'react';
import type { MutableRefObject } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { reportGlobalError } from '@/services/globalError';
import { useVirtualCitySystemStore } from '@/store/virtualCitySystem.store';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { fetchVirtualCitySystemAgentHistory } from '@/api/virtualCity';
import { normalizeSystemHistory } from '@/components/virtualCity/systemHistoryAdapter';
import {
  forgetShowCommand,
  isDuplicateCommand,
} from '@/components/virtualCity/cityShowCommandDedup';
import type {
  VirtualCityDisplayCommand,
  VirtualCityShowCommandFrame,
  VirtualCitySystemEventFrame,
  VirtualCitySystemHistoryFrame,
} from '@/types/virtualCitySystem';

/** 相机聚焦目标（与 VirtualCityCityMap 的 VirtualCityFocusTarget 同形）。 */
type SystemFocusRef = MutableRefObject<{ x: number; z: number } | null>;

export interface VirtualCitySystemAgentDeps {
  /** 与页面 `handleSelectDistrict` 共用的聚焦 ref（由 onShowCommand 消费）。 */
  focusRef: SystemFocusRef;
  /** 显示指令出口（由调用方注入 cityShowAdapter 动作）。 */
  onShowCommand: (cmd: VirtualCityDisplayCommand) => void;
}

/** 会话历史上限（与 store 的 cap 同步，防服务端下发超长历史撑爆内存）。 */
const MAX_HISTORY = 200;

/** WS history 请求未回包的兜底等待（ms）：超时改走 HTTP `system_agent/history`。 */
const WS_HISTORY_TIMEOUT_MS = 2000;

/**
 * 显示指令去重（`game.virtual_city_show_command` 与 `system_event.kind=show_command`
 * 描述同一条指令；focus / set_mode / annotate 虽幂等，重复 annotate 会重启
 * 浮动标签的 8s 消隐计时）。批次 85 C2 修复：语义收窄为「首条必达、只吞邻近
 * 重复」，实现抽到纯叶模块 `cityShowCommandDedup`（可单测）。
 */

export function useVirtualCitySystemAgent(
  roomId: string,
  deps: VirtualCitySystemAgentDeps,
) {
  const messages = useVirtualCitySystemStore((s) => s.messages);
  const busy = useVirtualCitySystemStore((s) => s.busy);
  const connected = useVirtualCitySystemStore((s) => s.connected);
  const minimized = useVirtualCitySystemStore((s) => s.minimized);
  const setMinimized = useVirtualCitySystemStore((s) => s.setMinimized);

  const roomIdRef = useRef(roomId);
  roomIdRef.current = roomId;
  // deps 每次渲染都是新对象 ⇒ 用 ref 承载，订阅 effect 只挂一次（不随父组件重渲染反复解绑）。
  const depsRef = useRef(deps);
  depsRef.current = deps;

  // ── 历史恢复控制（§7.3-6）────────────────────────────────────────────
  // WS history 帧与 HTTP 兜底**先到先得**：resolved 后另一路丢弃。
  const historyResolvedRef = useRef(false);
  // 历史请求发出后已有实时事件 / 发送 ⇒ 不拿旧历史覆盖新对话（极端竞态兜底）。
  const sawLiveRef = useRef(false);
  // 订阅 effect（[]）→ 历史 effect（[roomId]）的 applyHistory 通道。
  const historyApplyRef = useRef<((raw: unknown) => void) | null>(null);

  useEffect(() => {
    /**
     * 显示指令送达（批次 85 C2 修复）：去重 → 回调 → 抛错不吞。
     *  - `isDuplicateCommand` 放行时记账（首条必达、只吞邻近重复）；
     *  - 回调抛错 ⇒ `forgetShowCommand` 撤账（双帧孪生可重试）+ 全局 toast
     *    （§7.1：错误绝不吞进 console / 空 catch）。
     */
    const deliver = (cmd: VirtualCityDisplayCommand | null) => {
      if (!cmd || typeof cmd.type !== 'string') return;
      if (isDuplicateCommand(cmd)) return;
      try {
        depsRef.current.onShowCommand(cmd);
      } catch (e) {
        forgetShowCommand(cmd);
        reportGlobalError({
          message: `显示指令执行失败（${cmd.type}）：${e instanceof Error ? e.message : String(e)}`,
          severity: 'error',
        });
      }
    };

    const unsub = wsClient.on((env: WsEnvelope) => {
      const p = (env.payload || {}) as Record<string, unknown>;
      // 只处理当前房间的帧（无 room_id 的帧不过滤，与 useVirtualCity 同口径）。
      if (p.room_id && p.room_id !== roomIdRef.current) return;

      switch (env.type) {
        case 'game.virtual_city_system_event': {
          const f = p as unknown as VirtualCitySystemEventFrame;
          if (!f || typeof f.kind !== 'string') return;
          // 实时事件（cleared 除外）= 会话有新内容 ⇒ 迟到的历史不得覆盖（§7.3-6）。
          if (f.kind !== 'cleared') sawLiveRef.current = true;
          useVirtualCitySystemStore.getState().applyEvent(f);
          if (f.kind === 'error') {
            const d = f.data || {};
            const msg =
              (typeof d.message === 'string' && d.message) ||
              (typeof d.error === 'string' && d.error) ||
              (typeof d.text === 'string' && d.text) ||
              '';
            // §7.1 双通道之二：全局 toast（内联红条由消息自身渲染）。
            reportGlobalError({
              message: msg || '虚拟城市系统 Agent 执行失败',
              severity: 'error',
            });
          }
          // 批次 84：`agent_message`（其它 Agent 的来信）**不是错误** ⇒
          // 不触发 reportGlobalError；它由 store 落成一条独立的来信消息。
          if (f.kind === 'show_command') {
            deliver(d_command(f.data));
          }
          break;
        }

        case 'game.virtual_city_system_history': {
          const h = p as unknown as VirtualCitySystemHistoryFrame;
          // wire 形状（llmtypes.Message JSON）⇒ 归一化后落库（§7.3-6）。
          const apply = historyApplyRef.current;
          if (apply) {
            apply(h?.messages);
          } else {
            // 兜底：历史 effect 未挂载（极端时序）时仍归一化落库，不丢帧。
            useVirtualCitySystemStore.getState().setMessages(
              normalizeSystemHistory(h?.messages).slice(-MAX_HISTORY),
            );
          }
          break;
        }

        case 'game.virtual_city_show_command': {
          const f = p as unknown as VirtualCityShowCommandFrame;
          deliver(f?.command ?? null);
          break;
        }

        default:
          break;
      }
    });
    // 批次 85 C2：断线重连后**重新入房** —— 房间成员集合随连接生命周期丢失
    // （Hub 按连接登记观战者），重连后不补 `game.spectate` / `game.join` 的话，
    // `BroadcastRoomIncludingSpectators` 的 system_event / show_command 帧对本页
    // **永久失聪**（C2「无提示无报错」的送达侧根因）。首连的入房由
    // VirtualCityGamePage 的 join effect 负责（避免双发），此后每次重连在此补发。
    let openedOnce = false;
    const offRejoin = wsClient.onOpen(() => {
      if (!openedOnce) {
        openedOnce = true;
        return;
      }
      const seat = useVirtualCityStore.getState().mySeat;
      wsClient.send(typeof seat === 'number' && seat >= 0 ? 'game.join' : 'game.spectate', {
        room_id: roomIdRef.current,
        game_kind: 'virtual_city',
      });
    });
    return () => {
      unsub();
      offRejoin();
    };
  }, []);

  /**
   * 历史恢复（方案 §7.3-6「断线重连 → 历史经 WS system_history 恢复」）：
   *   1. 挂载 / 换房时 WS 已 open ⇒ 立即发一次 `game.virtual_city_system_history`；
   *   2. WS 尚未 open（连接中 / 重连中）⇒ 挂 `wsClient.onOpen` 连上后补发
   *      （只经 onOpen 单点触发，避免与 wsClient 的 CONNECTING 缓冲 flush 双发）；
   *   3. WS 请求 2s 未回（或 WS 不可用）⇒ HTTP `GET .../system_agent/history` 兜底；
   *   4. WS history 帧与 HTTP 兜底先到先得（historyResolvedRef 只一路生效）；
   *   5. 重连不无脑重发：已恢复且本地有消息 ⇒ 跳过，仅空会话补拉（防重连风暴）。
   */
  useEffect(() => {
    historyResolvedRef.current = false;
    sawLiveRef.current = false;
    let cancelled = false;
    let httpTimer: ReturnType<typeof setTimeout> | null = null;

    const applyHistory = (raw: unknown) => {
      // 迟到回包（换房 / 卸载后）一律丢弃，不得污染下一轮恢复状态。
      if (cancelled) return;
      if (historyResolvedRef.current) return; // 先到先得：只一路生效
      historyResolvedRef.current = true;
      if (httpTimer !== null) {
        clearTimeout(httpTimer);
        httpTimer = null;
      }
      const st = useVirtualCitySystemStore.getState();
      // 极端竞态兜底：请求在途时用户已开始新对话 ⇒ 不拿旧历史覆盖新消息。
      if (sawLiveRef.current && st.messages.length > 0) return;
      st.setMessages(normalizeSystemHistory(raw).slice(-MAX_HISTORY));
    };
    historyApplyRef.current = applyHistory;

    const httpFallback = async () => {
      try {
        const data = await fetchVirtualCitySystemAgentHistory(roomIdRef.current);
        applyHistory(data.messages);
      } catch {
        if (cancelled || historyResolvedRef.current) return;
        // §7.1：WS 超时且 HTTP 兜底也失败 ⇒ 全局 toast，禁止吞进 console。
        reportGlobalError({
          message: '系统对话历史恢复失败，请刷新页面重试',
          severity: 'error',
        });
      }
    };

    const armHttpFallback = () => {
      if (cancelled || httpTimer !== null) return;
      httpTimer = setTimeout(() => {
        httpTimer = null;
        void httpFallback();
      }, WS_HISTORY_TIMEOUT_MS);
    };

    const request = () => {
      if (historyResolvedRef.current) return;
      wsClient.send('game.virtual_city_system_history', { room_id: roomIdRef.current });
      // 重拉（空会话补拉）时重启兜底计时；首发时计时已随 effect 启动。
      armHttpFallback();
    };

    // 兜底计时无条件启动：WS 不可用 / 迟迟不 open 时 HTTP 是唯一通道（§7.3-6）。
    armHttpFallback();

    if (wsClient.connected) request();
    const offOpen = wsClient.onOpen(() => {
      if (historyResolvedRef.current) {
        // 已恢复过：仅本地空会话（服务端重启 / 误清）时补拉一次，防重连风暴。
        if (useVirtualCitySystemStore.getState().messages.length > 0) return;
        historyResolvedRef.current = false;
      }
      request();
    });

    return () => {
      cancelled = true;
      historyApplyRef.current = null;
      if (httpTimer !== null) clearTimeout(httpTimer);
      offOpen();
    };
  }, [roomId]);

  /**
   * 发送一句自然语言指令。先本地落一条 user 消息（立即回显），再发 WS；
   * 空串直接忽略（发送按钮同样禁用，双保险）。
   */
  const sendMessage = useCallback((text: string) => {
    const trimmed = text.trim();
    if (!trimmed) return;
    sawLiveRef.current = true; // 新对话已在途 ⇒ 迟到历史不得覆盖（§7.3-6）
    useVirtualCitySystemStore.getState().appendUser(trimmed);
    wsClient.send('game.virtual_city_system_message', {
      room_id: roomId,
      text: trimmed,
    });
  }, [roomId]);

  /** 清空对话：发 WS reset + 本地立即清空（服务端会回 cleared 事件，幂等）。 */
  const reset = useCallback(() => {
    wsClient.send('game.virtual_city_system_reset', { room_id: roomId });
    useVirtualCitySystemStore.getState().reset();
  }, [roomId]);

  return {
    messages,
    busy,
    connected,
    minimized,
    setMinimized,
    sendMessage,
    reset,
  };
}

export type VirtualCitySystemAgentApi = ReturnType<typeof useVirtualCitySystemAgent>;

/**
 * 取显示指令（system_event kind=show_command 的负载）。批次 85 C2：兼容两种
 * wire 形状 —— 嵌套 `data.command` 与**扁平** `data` 即指令本体（实证
 * `citysystem.emitCommands` 下发扁平形状，`displayCommand` 无 command 字段）。
 * 旧实现只认嵌套 ⇒ system_event 通道整条死代码（双帧去重形同虚设、
 * `show_commands` 徽章恒空、丢一条帧即整条指令丢失）。
 */
function d_command(data: Record<string, unknown> | undefined): VirtualCityDisplayCommand | null {
  if (!data || typeof data !== 'object') return null;
  const nested = data.command;
  if (nested && typeof nested === 'object') {
    const c = nested as VirtualCityDisplayCommand;
    if (typeof c.type === 'string') return c;
  }
  if (typeof data.type === 'string') return data as unknown as VirtualCityDisplayCommand;
  return null;
}
