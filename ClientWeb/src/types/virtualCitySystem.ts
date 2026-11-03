/**
 * 虚拟城市 批次 81「City-System 主 Agent 与系统对话」—— WS 帧与消息类型。
 *
 * 契约唯一事实来源：lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/
 * 01-方案设计.md §6（数据契约）。改这里必须同步后端 ws 层与方案文档。
 *
 * 帧（S→C）：
 *   game.virtual_city_system_event   {room_id, kind, data}
 *   game.virtual_city_system_history {room_id, messages[]}
 *   game.virtual_city_show_command   {room_id, command}
 * 帧（C→S）：
 *   game.virtual_city_system_message {room_id, text}
 *   game.virtual_city_system_reset   {room_id}
 */

/** `game.virtual_city_system_event` 的 kind 枚举（方案 §3.5）。 */
export type VirtualCitySystemEventKind =
  | 'agent_start'
  | 'text'
  | 'thinking'
  | 'tool_start'
  | 'tool_result'
  | 'show_command'
  | 'agent_end'
  | 'error'
  | 'report'
  | 'cleared';

/** 一次工具调用的前端留痕（tool_start 建记录，tool_result 回填结果）。 */
export interface VirtualCityToolCallRecord {
  /** 工具名（如 inject_scenario / query_city_state）。 */
  tool: string;
  /** 调用参数（后端给对象时前端 stringify，展示层再摘要）。 */
  input: string;
  /** 工具输出文本（tool_result 回填；空串 = 进行中）。 */
  text: string;
  /** 工具是否失败。 */
  is_err: boolean;
  /** 时间戳（unix 毫秒，取后端下发值，缺省用本地 Date.now）。 */
  ts: number;
}

/**
 * 结构化显示指令（City-WebGL-Show 产出，前端既有 3D 引擎执行）。
 * 服务端只做「看哪里 / 怎么显示」的翻译，渲染仍在前端（方案 §2 决策 3）。
 */
export interface VirtualCityDisplayCommand {
  type: 'focus_district' | 'focus_point' | 'set_mode' | 'highlight_district' | 'annotate' | 'clear';
  district?: string;
  x?: number;
  z?: number;
  /** 'orbit' | 'fly'。 */
  mode?: string;
  text?: string;
  color?: string;
  /** 全链路追踪 id（前端去重 / 排查用，不参与渲染决策）。 */
  trace_id?: string;
}

/** 一条对话消息（用户提问 / Agent 回答及其工具留痕）。 */
export interface VirtualCitySystemMessage {
  id: string;
  role: 'user' | 'assistant';
  /** 增量追加（assistant 流式）；agent_end 时以后端最终文本落定。 */
  text: string;
  ts: number;
  /** assistant 专属：本轮工具调用序列。 */
  tool_calls?: VirtualCityToolCallRecord[];
  /** assistant 专属：本轮产生的显示指令（仅用于徽章提示，执行走 show_command 帧）。 */
  show_commands?: VirtualCityDisplayCommand[];
  /** `query_prediction_report` 命中时的报告负载（PredictionReportView 消费）。 */
  report?: unknown;
  /** `error` 事件的下发错误文案（面板内联红条 + 全局 toast 双通道）。 */
  error?: string;
  /** 流式进行中（agent_start → agent_end/error 之间）。 */
  streaming?: boolean;
}

/** S→C：`game.virtual_city_system_event` 负载。 */
export interface VirtualCitySystemEventFrame {
  room_id?: string;
  kind: VirtualCitySystemEventKind;
  data: Record<string, unknown>;
}

/**
 * 后端会话历史的 wire 形状（`llmtypes.Message` JSON）——WS history 帧与
 * HTTP `GET .../system_agent/history` 的 `data.messages[]` 同构。
 * 字段按 `ServerGo/llm/types/types.go::ContentBlock.MarshalJSON` 的
 * per-Type 收敛形状（text / tool_use / tool_result / thinking），
 * 前端展示形状经 `systemHistoryAdapter.normalizeSystemHistory` 归一化得到。
 */
export interface VirtualCitySystemWireBlock {
  /** "text" | "tool_use" | "tool_result" | "thinking"（thinking 仅 {type,budget}，无正文）。 */
  type: string;
  /** text 块正文（服务端 MarshalJSON 对空文本写 "(empty)"）。 */
  text?: string;
  /** tool_use：id / name / input（wire 恒有键，input 为对象）。 */
  id?: string;
  name?: string;
  input?: unknown;
  /** tool_result：tool_use_id / content（text 子块数组）/ is_error。 */
  tool_use_id?: string;
  content?: VirtualCitySystemWireBlock[] | string;
  is_error?: boolean;
  /** thinking 块的 token 预算（wire 仅 {type,budget}，无正文）。 */
  budget?: number;
}

/** 后端会话历史的一条消息（`llmtypes.Message` JSON：role + content 块数组）。 */
export interface VirtualCitySystemWireMessage {
  /** "user" | "assistant"（tool_result 以 user 消息承载，见 context.go）。 */
  role: string;
  content: VirtualCitySystemWireBlock[] | string;
}

/** S→C：`game.virtual_city_system_history` 负载（首屏 / 重连恢复）。 */
export interface VirtualCitySystemHistoryFrame {
  room_id?: string;
  /** wire 形状（llmtypes.Message JSON），非前端消息形状——消费前必须归一化。 */
  messages: VirtualCitySystemWireMessage[];
}

/** S→C：`game.virtual_city_show_command` 负载（驱动 3D 视角 / 高亮 / 标注）。 */
export interface VirtualCityShowCommandFrame {
  room_id?: string;
  command: VirtualCityDisplayCommand;
}
