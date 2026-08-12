/**
 * 狼人杀 emotion metadata 共享模块 (2026-08-04 §重构)
 *
 * 原先 emotion metadata 表写在 WerewolfTable.tsx 第 69-90 行,FactionDrawer 也
 * 需要同样的 emoji + label 数据。本模块统一管理,供:
 *   - WerewolfTable emotion badge / compact emoji (model-name 行)
 *   - FactionDrawer agent emotion 行
 *   - HistoryDrawer 情绪变化曲线
 *
 * emoji 数组与 §优化-20260730-01 保持一致(深底浅字配色);
 * label 通过 i18n translate() 取(避免在 React 组件外调用 hook)。
 */
import { translate, type Lang } from '@/i18n';
import { useI18nStore } from '@/store/i18n.store';

export type WerewolfEmotionKey =
  | 'confident'
  | 'excited'
  | 'calm'
  | 'panic'
  | 'wary'
  | 'irritated'
  | 'grievance'
  | 'confused'
  | 'guilty'
  | 'tired';

export interface EmotionMeta {
  key: WerewolfEmotionKey;
  /** 中文简写 label(从 i18n 取) */
  label: string;
  emoji: string;
  /** §优化-20260730-01 — 深底浅字配色(WCAG ≥ 4.5:1) */
  bg: string;
  fg: string;
  bgDark: string;
  fgDark: string;
}

/** 固定的 bg/fg 配色(不参与 i18n) */
const COLOR: Record<WerewolfEmotionKey, Omit<EmotionMeta, 'key' | 'label' | 'emoji'>> = {
  confident: { bg: '#cce5ff', fg: '#0d2845', bgDark: '#1e3a5f', fgDark: '#cce5ff' },
  excited:   { bg: '#ffd9b3', fg: '#5a2b00', bgDark: '#5f3a1e', fgDark: '#ffd9b3' },
  calm:      { bg: '#e6e6e6', fg: '#1a1a1a', bgDark: '#3a3a3a', fgDark: '#e6e6e6' },
  panic:     { bg: '#ffcccc', fg: '#5a0d0d', bgDark: '#5f1e1e', fgDark: '#ffcccc' },
  wary:      { bg: '#fff2b3', fg: '#5a4400', bgDark: '#5f4e1e', fgDark: '#fff2b3' },
  irritated: { bg: '#ffb3b3', fg: '#5a0d0d', bgDark: '#5f2828', fgDark: '#ffb3b3' },
  grievance: { bg: '#ffd1dc', fg: '#5a0d2a', bgDark: '#5f2e3a', fgDark: '#ffd1dc' },
  confused:  { bg: '#d9d9d9', fg: '#1a1a1a', bgDark: '#3a3a3a', fgDark: '#d9d9d9' },
  guilty:    { bg: '#d6c4e0', fg: '#2a1a4a', bgDark: '#3a2e4e', fgDark: '#d6c4e0' },
  tired:     { bg: '#c9d6e0', fg: '#1a2a3a', bgDark: '#2e3e4e', fgDark: '#c9d6e0' },
};

const EMOJI: Record<WerewolfEmotionKey, string> = {
  confident: '😌', excited: '🤩', calm: '😐', panic: '😨', wary: '🤔',
  irritated: '😤', grievance: '🥺', confused: '😵', guilty: '😬', tired: '😴',
};

const KEY_SET: ReadonlySet<string> = new Set(Object.keys(EMOJI));

/** 是否合法的 emotion key */
export function isWerewolfEmotion(key?: string | null): key is WerewolfEmotionKey {
  return typeof key === 'string' && KEY_SET.has(key);
}

/**
 * 取 emotion 完整 meta(emoji + label + bg/fg)。
 * 返回 null 表示 key 缺失或非法 — 调用方应降级为不渲染。
 * lang 缺省 = 当前 i18n store 语言。
 */
export function getEmotionMeta(key?: string | null, lang?: Lang): EmotionMeta | null {
  if (!isWerewolfEmotion(key)) return null;
  const l = lang ?? useI18nStore.getState().lang;
  const c = COLOR[key];
  return {
    key,
    // 2026-08-04 — emotion label i18n 键,缺失时 fallback 到 key(不抛错)
    label: translate(l, `werewolf.emotion.${key}` as any) || key,
    emoji: EMOJI[key],
    ...c,
  };
}

/**
 * 取 emotion 短显示(emoji + label)。
 * 给 FactionDrawer 这类只用 emoji+label 的位置用。
 */
export function getEmotionDisplay(key?: string | null, lang?: Lang): { emoji: string; label: string } | null {
  const m = getEmotionMeta(key, lang);
  return m ? { emoji: m.emoji, label: m.label } : null;
}