/**
 * cityLabels —— 名牌 / 气泡 / 标注的**受控渲染闸门**（批次 85 §3.2.2）。
 *
 * 背景：drei `<Html>` 走 DOM 门户，不吃父级 `visible` —— 图层显隐对它无效。
 * 批次 85 把「是否渲染名牌 / 气泡」收敛为**受控渲染条件**：`labels` 图层关闭
 * ⇒ 闸门返回 null ⇒ `<Html>` 不挂载 ⇒ **不进 DOM**（绝不用 `opacity: 0` /
 * `display: none` 糊弄，CLAUDE.md §26 反模式）。
 *
 * 受控面（与批次 84 的 7 图层注册表 `cityShowStore.layers.labels` 同源）：
 *   - `AgentHuman` 名牌 / 发言气泡 / 动作气泡（含 a11y 名牌文本）；
 *   - `CityAnnotationLabel`（Agent `annotate` 的 DOM 浮动标注）；
 *   - 城区悬浮信息卡（DistrictBlock，批次 84 已接线）；
 *   - `CityVoiceBubbleLayer` 市民之声气泡（挂载点套 `LabelsGate`）。
 * 默认 `labels: true` —— 既有视觉零变化。
 *
 * 本文件是**纯 DOM React**（不 import three / drei）：`AgentNameTag` /
 * `SpeechBubble` 把名牌 / 气泡的 DOM 结构抽成可单测组件（react-dom/server
 * 渲染断言「关闭后 DOM 不存在节点」），AgentHuman / CityVoiceBubbleLayer
 * 在各自的 `<Html>` 内复用 —— DOM 结构与批次 23/35 逐字节一致。
 */

import { useEffect, useRef, type ReactNode } from 'react';
import { useCityShowStore } from './cityShowStore';

/**
 * 名牌 / 气泡受控渲染闸门：`labels` 图层开 ⇒ 渲染 children；关 ⇒ null（不进 DOM）。
 * 供 drei `<Html>` 外层与纯 DOM 节点共用（机制唯一，单测钉死）。
 */
export function LabelsGate({ children }: { children: ReactNode }) {
  const on = useCityShowStore((s) => s.layers.labels);
  return on ? <>{children}</> : null;
}

/** `AgentNameTag` 的 props（全部原语，便于 memo / 单测构造）。 */
export interface AgentNameTagProps {
  /** 座位号（1 起）。 */
  seat: number;
  /** 昵称（缺省兜底 account 由调用方拼好）。 */
  name: string;
  /** 职业 emoji。 */
  emoji: string;
  /** 状态图标（可空串）。 */
  statusIcon: string;
  /** 净资产文案（已格式化）。 */
  netWorthText: string;
  /** 是否「我」（金环 + 紧凑态也显示昵称）。 */
  isMe: boolean;
  /** 是否 AI 居民（🤖 徽章）。 */
  isBot: boolean;
  /** AI 模型名（🤖 title）。 */
  modelDisplay?: string;
  /** 拥挤时紧凑形态（隐藏昵称，除非 isMe）。 */
  compact: boolean;
}

/**
 * 座位居民名牌 DOM（批次 35 §4.3 从 AgentHuman / AgentToken 原样抽出）：
 * 座位号 + 昵称 + 职业图标 + 状态图标 + 净资产 + 🤖。
 */
export function AgentNameTag({
  seat,
  name,
  emoji,
  statusIcon,
  netWorthText,
  isMe,
  isBot,
  modelDisplay,
  compact,
}: AgentNameTagProps) {
  return (
    <div
      className={
        'virtualCity-token-tag' +
        (isMe ? ' virtualCity-token-tag--me' : '') +
        (isBot ? ' virtualCity-token-tag--bot' : '') +
        (compact ? ' virtualCity-token-tag--compact' : '')
      }
    >
      <span className="virtualCity-token-tag__seat">{seat}</span>
      {(!compact || isMe) && (
        <span className="virtualCity-token-tag__name" title={name}>
          {name}
        </span>
      )}
      <span className="virtualCity-token-tag__emoji">{emoji}</span>
      <span className="virtualCity-token-tag__status">{statusIcon}</span>
      <span className="virtualCity-token-tag__nw">{netWorthText}</span>
      {isBot && (
        <span className="virtualCity-token-tag__bot" title={modelDisplay}>
          🤖
        </span>
      )}
    </div>
  );
}

/** 气泡变体（CSS modifier 后缀；'' = 普通发言气泡）。 */
export type SpeechBubbleVariant = '' | 'action' | 'voice';

/** `SpeechBubble` 的 props。 */
export interface SpeechBubbleProps {
  /** 气泡正文（调用方负责截断）。 */
  text: string;
  /** 变体修饰（action 动作气泡 / voice 市民之声 / '' 发言气泡）。 */
  variant?: SpeechBubbleVariant;
  /** 前置图标（action 气泡的 💭/🚶/👀；不传不渲染图标位）。 */
  icon?: string;
  /** 署名（voice 气泡的「🗣 名字」，调用方拼好前缀；空串不渲染署名位）。 */
  name?: string;
}

/**
 * 发言 / 动作 / 市民之声气泡 DOM（批次 23/35 原样抽出，三处共用）。
 */
export function SpeechBubble({ text, variant = '', icon, name }: SpeechBubbleProps) {
  return (
    <div
      className={
        'virtualCity-speech-bubble' + (variant ? ` virtualCity-speech-bubble--${variant}` : '')
      }
      role="status"
    >
      {icon && (
        <span className="virtualCity-speech-bubble__icon" aria-hidden="true">
          {icon}
        </span>
      )}
      {name && <span className="virtualCity-speech-bubble__name">{name}</span>}
      <span className="virtualCity-speech-bubble__text">{text}</span>
    </div>
  );
}

/** 标注浮动标签自动消隐时长（方案 §4.4：8s）。 */
export const ANNOTATION_TTL_MS = 8000;

/**
 * Agent `annotate` 指令的 DOM 浮动标签（8s 自动消隐）。
 * 独立成组件：窗口关闭 / 最小化后标注仍要可见（视角已被拉动是既成事实）。
 * 批次 85：整卡受 `labels` 图层闸门控制（关闭 ⇒ 不进 DOM）。
 */
export function CityAnnotationLabel() {
  const annotation = useCityShowStore((s) => s.annotation);
  const clearAnnotation = useCityShowStore((s) => s.clearAnnotation);
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    if (!annotation) return undefined;
    const ts = annotation.ts;
    timerRef.current = window.setTimeout(() => {
      // 仅当标注未被更新（ts 未变）才消除，避免新标注被旧定时器误清。
      if (useCityShowStore.getState().annotation?.ts === ts) clearAnnotation();
    }, ANNOTATION_TTL_MS);
    return () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [annotation, clearAnnotation]);

  if (!annotation) return null;
  return (
    <LabelsGate>
      <div
        className="virtualCity-sysdialog-annotation"
        role="status"
        data-testid="virtualCity-system-annotation"
      >
        📍 {annotation.text}
      </div>
    </LabelsGate>
  );
}
