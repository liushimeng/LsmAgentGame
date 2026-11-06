/**
 * 批次 87 P1-2 —— 「系统对话」相关 WS 错误帧（`game.error`）的**纯逻辑**层。
 *
 * 背景（方案 §3.2 P1-2）：后端拒绝时发 `game.error` 帧（载荷 `{code, message}`），
 * 而 `useVirtualCitySystemAgent` 的帧 switch **没有该分支** ⇒ 用户消息本地已回显，
 * 但**任何地方都看不到错误**（违反 CLAUDE.md §7.1「错误必须最高层级展示」）。
 *
 * 本模块职责（纯函数，无 React / 无 store 依赖 ⇒ 可零依赖单测）：
 *   1. **白名单过滤**：`game.error` 是全游戏共用的帧类型，本模块只认
 *      City-System 相关错误码；其余码（其它游戏的帧）一律忽略（P2-8：载荷无
 *      `room_id` 时无法按房间过滤 ⇒ 用错误码白名单兜底）。
 *   2. **人类可读文案**：错误码 → i18n 键（区分「服务端停用 / 上一轮还在执行 /
 *      房间不存在」等语义，而不是把后端英文码原样抛给用户）。
 *   3. **双通道派发**：内联红条（会话流）+ 全局 toast（`reportGlobalError`）。
 *   4. **35059 特判**：服务端明确说「已停用」⇒ 前端把停用态**写进本地 store**，
 *      面板立即红条 + 禁发，不再只依赖 `game.state` 字段（后端漏盖 / 旧后端时
 *      也能正确表现）。
 *
 * 错误码事实来源：`ServerGo/errcode/errcode.go`（35001 / 35059–35065）。
 */

import type { TKey } from '@/i18n';

/**
 * City-System 相关错误码白名单。
 *
 * - 35001 房间不存在（虚拟城市专用段起点）
 * - 35059 系统 Agent 未启用（city_system.enabled=false）
 * - 35060 该用户上一轮尚未结束（并发限流 1）
 * - 35061 目标 Agent 未注册
 * - 35062 目标 Agent 在时限内未回应
 * - 35063 目标 Agent 回包超长
 * - 35064 场景分组未知
 * - 35065 显示指令参数非法
 */
export const CITY_SYSTEM_ERROR_CODES = [
  35001, 35059, 35060, 35061, 35062, 35063, 35064, 35065,
] as const;

/** 白名单中的错误码（联合类型）。 */
export type CitySystemErrorCode = (typeof CITY_SYSTEM_ERROR_CODES)[number];

/** 错误码 → 人类可读文案的 i18n 键（三语同步，§29.8 缺键即白屏）。 */
const ERROR_MESSAGE_KEYS: Record<CitySystemErrorCode, TKey> = {
  35001: 'virtualCity.systemDialog.errorRoomNotFound',
  35059: 'virtualCity.systemDialog.errorDisabled',
  35060: 'virtualCity.systemDialog.errorBusy',
  35061: 'virtualCity.systemDialog.errorAgentNotFound',
  35062: 'virtualCity.systemDialog.errorNoReply',
  35063: 'virtualCity.systemDialog.errorReplyTooLong',
  35064: 'virtualCity.systemDialog.errorGroupUnknown',
  35065: 'virtualCity.systemDialog.errorDisplayInvalid',
};

/** 归一化错误码：接受 number / 纯数字字符串；其它一律 NaN（不误判）。 */
function normalizeCode(raw: unknown): number {
  if (typeof raw === 'number') return Number.isFinite(raw) ? raw : NaN;
  if (typeof raw === 'string') {
    const s = raw.trim();
    if (/^\d+$/.test(s)) return Number(s);
  }
  return NaN;
}

/** 该错误码是否属于「系统对话」域（白名单）。 */
export function isCitySystemErrorCode(code: unknown): boolean {
  const n = normalizeCode(code);
  return (CITY_SYSTEM_ERROR_CODES as readonly number[]).includes(n);
}

/** `game.error` 帧载荷的最小形状（宽松：字段可能缺失 / 类型不定）。 */
export interface CitySystemErrorFrameLike {
  code?: unknown;
  message?: unknown;
  room_id?: unknown;
}

/** 白名单命中的错误帧（已归一化）。 */
export interface CitySystemErrorPick {
  /** 归一化后的错误码。 */
  code: number;
  /** 服务端原文（可能为空串）。 */
  serverMessage: string;
  /** 人类可读文案的 i18n 键。 */
  messageKey: TKey;
  /** 是否意味着「服务端已停用系统对话」⇒ 前端置本地停用态。 */
  disablesAgent: boolean;
}

/**
 * 从一帧 `game.error` 里挑出属于本房间的 City-System 错误。
 *
 * 过滤规则（§4.3）：
 *   1. 错误码必须在白名单内；
 *   2. 帧带 `room_id` 时必须与当前房间一致（不带 `room_id` 的历史帧放过，
 *      靠白名单兜底）。
 *
 * @returns 命中的错误；不命中返回 null（调用方**一律忽略**，不得打扰用户）。
 */
export function pickCitySystemError(
  frame: CitySystemErrorFrameLike | null | undefined,
  currentRoomId: string | null | undefined,
): CitySystemErrorPick | null {
  if (!frame || typeof frame !== 'object') return null;
  const code = normalizeCode(frame.code);
  if (!isCitySystemErrorCode(code)) return null;
  const room = typeof frame.room_id === 'string' ? frame.room_id : '';
  if (room && currentRoomId && room !== currentRoomId) return null;
  const n = code as CitySystemErrorCode;
  return {
    code,
    serverMessage: typeof frame.message === 'string' ? frame.message : '',
    messageKey: ERROR_MESSAGE_KEYS[n],
    disablesAgent: n === 35059,
  };
}

/** 处理一帧 `game.error` 所需的依赖（由调用方注入，便于单测）。 */
export interface CitySystemErrorHandlerDeps {
  /** 内联红条：向会话流追加一条错误消息（复用 `SystemDialogMessage` 红条渲染）。 */
  appendInlineError: (text: string) => void;
  /** 全局 toast（§7.1 第二层）。 */
  reportGlobal: (message: string) => void;
  /** 置本地停用态（仅 35059 触发）。 */
  setAgentDisabled: (disabled: boolean) => void;
  /** 文案解析：按 i18n 键取当前语言的文本。 */
  text: (key: TKey) => string;
}

/**
 * 处理一帧 `game.error`：命中白名单 ⇒ 双通道展示 + 35059 置本地停用态。
 *
 * @returns 是否处理了该帧（false = 非本域错误，调用方应忽略）。
 */
export function handleCitySystemErrorFrame(
  frame: CitySystemErrorFrameLike | null | undefined,
  currentRoomId: string | null | undefined,
  deps: CitySystemErrorHandlerDeps,
): boolean {
  const picked = pickCitySystemError(frame, currentRoomId);
  if (!picked) return false;
  // 文案优先级：i18n 人类可读文案 → 服务端原文 → 通用兜底。
  const text =
    deps.text(picked.messageKey) || picked.serverMessage || '系统对话请求失败，请稍后重试';
  deps.appendInlineError(text);
  deps.reportGlobal(text);
  if (picked.disablesAgent) deps.setAgentDisabled(true);
  return true;
}
