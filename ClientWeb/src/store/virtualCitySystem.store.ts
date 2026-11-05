/**
 * 虚拟城市 批次 81 —— 「系统对话」会话 store（zustand）。
 *
 * 单向数据流：`useVirtualCitySystemAgent` 收 WS 帧 → 写本 store →
 * `<SystemDialogPanel>` / `<SystemDialogMessage>` 订阅渲染。
 *
 * 幂等纪律（方案 §3.5 / 任务 §2）：
 *   - `agent_start` 若末尾已是 streaming 的 assistant 消息 ⇒ 复用，不新建
 *     （重连补发 / 连续两轮快速点击都不会叠出空气泡）。
 *   - `text` / `thinking` 增量只追加到「当前 streaming 的 assistant」。
 *   - `tool_result` 按工具名**从后往前**匹配最近一条同名调用回填（无状态，
 *     不依赖模块级游标；并发同工具时匹配到的仍是最新的那条）。
 *   - `cleared` / `reset` 清空消息。
 *
 * 写入频率：只在 WS 事件到达时写，无定时器（与 virtualCity.store 同口径）。
 */

import { create } from 'zustand';
import type {
  VirtualCityDisplayCommand,
  VirtualCitySystemEventFrame,
  VirtualCitySystemMessage,
  VirtualCityToolCallRecord,
} from '@/types/virtualCitySystem';

/** 会话消息上限（任务 §2 cap 200）。 */
const MAX_MESSAGES = 200;

/** 浮窗拖拽位置持久化键（批次83 §3.1；纯客户端 UI 偏好，换房不清）。 */
export const DIALOG_POS_STORAGE_KEY = 'vc-sysdialog-pos-v1';

/** 浮窗左上角位置（px，视口坐标；null = 默认位，渲染时不写 inline style）。 */
export interface VirtualCitySystemDialogPos {
  x: number;
  y: number;
}

/**
 * 从 localStorage 恢复浮窗位置（批次83 §3.1）：
 * JSON 解析 + 有限数字校验，任何非法形态（缺键 / NaN / JSON 坏）静默丢弃返回 null；
 * 视口越界不在读取时钳制（无元素宽度），由面板挂载 / resize 时统一重钳制。
 */
function loadDialogPos(): VirtualCitySystemDialogPos | null {
  try {
    const raw = window.localStorage.getItem(DIALOG_POS_STORAGE_KEY);
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return null;
    const { x, y } = parsed as { x?: unknown; y?: unknown };
    if (typeof x !== 'number' || typeof y !== 'number') return null;
    if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
    return { x, y };
  } catch {
    return null;
  }
}

let idSeq = 0;

/** 生成前端消息 id（仅用于 React key 与 upsert 定位，不参与后端语义）。 */
function nextId(prefix: string): string {
  idSeq += 1;
  return `${prefix}-${Date.now().toString(36)}-${idSeq}`;
}

/** 后端 ts 可能是秒 / 毫秒 / 缺失；统一成毫秒，非法回落本地时钟。 */
function numTs(raw: unknown): number {
  const n = typeof raw === 'number' ? raw : Number(raw);
  if (!Number.isFinite(n) || n <= 0) return Date.now();
  // 秒级（< 1e12）→ 毫秒
  return n < 1e12 ? n * 1000 : n;
}

/** tool_start 的 input 后端可能给对象（方案 §6.1），统一 stringify 成字符串。 */
function stringifyInput(raw: unknown): string {
  if (typeof raw === 'string') return raw;
  if (raw === undefined || raw === null) return '';
  try {
    return JSON.stringify(raw);
  } catch {
    return String(raw);
  }
}

/** 构造一条 assistant 消息（显式标注 role，避免数组字面量经 .slice() 后类型拓宽）。 */
function assistantMsg(p: Partial<VirtualCitySystemMessage> & { id: string }): VirtualCitySystemMessage {
  return { role: 'assistant', text: '', ts: Date.now(), ...p };
}

/**
 * 取「当前 streaming 的 assistant 消息」；没有则新建一条。
 * 返回的数组**始终是新引用**（调用方随后 map 追加内容），未新建时与原数组同内容。
 */
function ensureStreamingAssistant(
  messages: VirtualCitySystemMessage[],
): { messages: VirtualCitySystemMessage[]; id: string } {
  const last = messages[messages.length - 1];
  if (last && last.role === 'assistant' && last.streaming) {
    return { messages, id: last.id };
  }
  const id = nextId('a');
  return {
    messages: [
      ...messages,
      assistantMsg({ id, streaming: true }),
    ].slice(-MAX_MESSAGES),
    id,
  };
}

/** 从后往前找最后一条 streaming 的 assistant 下标（ES2022 无 findLastIndex）。 */
function lastStreamingIndex(messages: VirtualCitySystemMessage[]): number {
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    const m = messages[i];
    if (m.role === 'assistant' && m.streaming) return i;
  }
  return -1;
}

export interface VirtualCitySystemStore {
  /** 会话消息（新→旧追加，cap 200）。 */
  messages: VirtualCitySystemMessage[];
  /** 是否收到过 agent_start / agent_end（通道可达信号，任务 §2）。 */
  connected: boolean;
  /** agent_start → agent_end / error / cleared 之间为 true（输入禁用）。 */
  busy: boolean;
  /** 窗口是否最小化（收起为浮动条，不断线）。 */
  minimized: boolean;
  /** 浮窗拖拽位置（批次83 §3.1；null = 默认位，渲染走 CSS 基础规则）。 */
  dialogPos: VirtualCitySystemDialogPos | null;
  /**
   * 批次 84：系统信息面板的 Agent 回灌文本（`query_city_state` 工具结果，
   * 最新一条）。null = 尚未回灌（面板走 WS game.state 的字段摘要兜底）。
   */
  systemInfo: string | null;

  appendUser: (text: string) => void;
  upsertAssistant: (id: string, patch: Partial<VirtualCitySystemMessage>) => void;
  applyEvent: (frame: VirtualCitySystemEventFrame) => void;
  setMinimized: (minimized: boolean) => void;
  /** 设置浮窗位置；null = 复位默认位（localStorage 写 / 清由面板拖拽收尾负责）。 */
  setDialogPos: (pos: VirtualCitySystemDialogPos | null) => void;
  setConnected: (connected: boolean) => void;
  /** 历史整表替换（`game.virtual_city_system_history`，首屏 / 重连恢复）。 */
  setMessages: (messages: VirtualCitySystemMessage[]) => void;
  reset: () => void;
}

export const useVirtualCitySystemStore = create<VirtualCitySystemStore>((set) => ({
  messages: [],
  connected: false,
  busy: false,
  minimized: false,
  dialogPos: loadDialogPos(),
  systemInfo: null,

  appendUser: (text) =>
    set((s) => ({
      messages: [
        ...s.messages,
        { id: nextId('u'), role: 'user' as const, text, ts: Date.now() },
      ].slice(-MAX_MESSAGES),
    })),

  upsertAssistant: (id, patch) =>
    set((s) => {
      const idx = s.messages.findIndex((m) => m.id === id);
      if (idx === -1) {
        return {
          messages: [
            ...s.messages,
            assistantMsg({ id, ...patch }),
          ].slice(-MAX_MESSAGES),
        };
      }
      const arr = s.messages.slice();
      arr[idx] = { ...arr[idx], ...patch };
      return { messages: arr };
    }),

  applyEvent: (frame) => {
    const d = (frame && frame.data) || {};
    switch (frame.kind) {
      case 'agent_start': {
        // 幂等：已有 streaming 消息 ⇒ 复用（不新建空气泡），仅推进状态位。
        set((s) => ({
          busy: true,
          connected: true,
          messages: ensureStreamingAssistant(s.messages).messages,
        }));
        break;
      }

      case 'text': {
        const delta = typeof d.delta === 'string' ? d.delta : '';
        if (!delta) break;
        set((s) => {
          const { messages, id } = ensureStreamingAssistant(s.messages);
          return {
            messages: messages.map((m) =>
              m.id === id ? { ...m, text: m.text + delta } : m,
            ),
          };
        });
        break;
      }

      case 'thinking': {
        const delta = typeof d.delta === 'string' ? d.delta : '';
        if (!delta) break;
        set((s) => {
          const { messages, id } = ensureStreamingAssistant(s.messages);
          return {
            messages: messages.map((m) =>
              m.id === id ? { ...m, text: m.text + delta } : m,
            ),
          };
        });
        break;
      }

      case 'tool_start': {
        const tool = typeof d.tool === 'string' ? d.tool : '';
        if (!tool) break;
        const rec: VirtualCityToolCallRecord = {
          tool,
          input: stringifyInput(d.input),
          text: '',
          is_err: false,
          ts: numTs(d.ts),
        };
        set((s) => {
          const { messages, id } = ensureStreamingAssistant(s.messages);
          return {
            messages: messages.map((m) =>
              m.id === id
                ? { ...m, tool_calls: [...(m.tool_calls ?? []), rec] }
                : m,
            ),
          };
        });
        break;
      }

      case 'tool_result': {
        const tool = typeof d.tool === 'string' ? d.tool : '';
        const text = typeof d.text === 'string' ? d.text : '';
        const isErr = d.is_err === true;
        const ts = numTs(d.ts);
        if (!tool) break;
        set((s) => {
          const { messages, id } = ensureStreamingAssistant(s.messages);
          const nextMessages = messages.map((m) => {
            if (m.id !== id) return m;
            const calls = [...(m.tool_calls ?? [])];
            let idx = -1;
            for (let i = calls.length - 1; i >= 0; i -= 1) {
              if (calls[i].tool === tool) { idx = i; break; }
            }
            if (idx === -1) {
              // 后端只给了 result（无对应 start）⇒ 补一条已完成记录，不丢结果。
              return {
                ...m,
                tool_calls: [...calls, { tool, input: '', text, is_err: isErr, ts }],
              };
            }
            calls[idx] = { ...calls[idx], text, is_err: isErr, ts };
            return { ...m, tool_calls: calls };
          });
          // 批次 84：query_city_state 的结果落 store ⇒ 系统信息面板显示
          // Agent 回灌的城市状态摘要（不新造后端接口）。
          if (tool === 'query_city_state' && text && !isErr) {
            return { messages: nextMessages, systemInfo: text };
          }
          return { messages: nextMessages };
        });
        break;
      }

      // 批次 84：其它 Agent 的上行来信（§7.1 新增 kind，不属于错误 ⇒ 不上报
      // 全局 toast）。渲染成**独立的一条来信消息**（role='agent'），
      // 不并入助手气泡，也不打断当前流式回复。
      case 'agent_message': {
        const from = typeof d.from === 'string' ? d.from : '';
        const text = typeof d.text === 'string' ? d.text : '';
        if (!text) break;
        set((s) => ({
          messages: [
            ...s.messages,
            {
              id: nextId('am'),
              role: 'agent' as const,
              text,
              ts: numTs(d.ts),
              ...(from ? { agent_from: from } : {}),
            },
          ].slice(-MAX_MESSAGES),
        }));
        break;
      }

      case 'show_command': {
        const cmd = d.command as VirtualCityDisplayCommand | undefined;
        if (!cmd || typeof cmd.type !== 'string') break;
        set((s) => {
          const { messages, id } = ensureStreamingAssistant(s.messages);
          return {
            messages: messages.map((m) =>
              m.id === id
                ? { ...m, show_commands: [...(m.show_commands ?? []), cmd] }
                : m,
            ),
          };
        });
        break;
      }

      case 'report': {
        // 兼容两种负载：{report:{...}} 与 data 即报告本体。
        const rep = Object.prototype.hasOwnProperty.call(d, 'report') ? d.report : d;
        set((s) => {
          const { messages, id } = ensureStreamingAssistant(s.messages);
          return {
            messages: messages.map((m) => (m.id === id ? { ...m, report: rep } : m)),
          };
        });
        break;
      }

      case 'agent_end': {
        const finalText = typeof d.text === 'string' ? d.text : '';
        set((s) => {
          const idx = lastStreamingIndex(s.messages);
          if (idx === -1) return { busy: false, connected: true };
          const arr = s.messages.slice();
          arr[idx] = {
            ...arr[idx],
            // 后端最终文本为权威；缺失时保留增量累积（旧服务端兜底）。
            text: finalText || arr[idx].text,
            streaming: false,
          };
          return { messages: arr, busy: false, connected: true };
        });
        break;
      }

      case 'error': {
        const msg =
          typeof d.message === 'string' && d.message
            ? d.message
            : typeof d.error === 'string' && d.error
              ? d.error
              : typeof d.text === 'string'
                ? d.text
                : '';
        set((s) => {
          const idx = lastStreamingIndex(s.messages);
          if (idx === -1) {
            // 没有进行中的回复 ⇒ 补一条错误消息，避免错误被吞。
            return {
              busy: false,
              messages: [
                ...s.messages,
                {
                  ...assistantMsg({ id: nextId('a'), error: msg }),
                },
              ].slice(-MAX_MESSAGES),
            };
          }
          const arr = s.messages.slice();
          arr[idx] = { ...arr[idx], error: msg, streaming: false };
          return { messages: arr, busy: false };
        });
        break;
      }

      case 'cleared': {
        set({ messages: [], busy: false, systemInfo: null });
        break;
      }

      default:
        break;
    }
  },

  setMinimized: (minimized) => set({ minimized }),
  setDialogPos: (pos) => set({ dialogPos: pos }),
  setConnected: (connected) => set({ connected }),

  setMessages: (messages) => set({ messages: messages.slice(-MAX_MESSAGES) }),

  reset: () => set({ messages: [], busy: false, systemInfo: null }),
}));
