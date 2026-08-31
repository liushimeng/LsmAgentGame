/**
 * 辩论比赛 历史战绩/复盘 共享纯工具 (2026-08-31 §20260831-08)
 *
 * DebateHistoryListPanel(大厅列表)与 DebateReplayModal(复盘详情)共用,
 * 只做纯函数,不持有状态;保持与 debate 组件同目录(CLAUDE.md §13.1 frontend-dev 工作面)。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import type { TKey } from '@/i18n';
import type {
  DebateHistoryRoom,
  DebateHistorySpeech,
  DebateMode,
  DebateRole,
} from '@/types/debate';

/** 与 useT() 返回签名一致,便于把 t 传入纯函数。 */
export type TFn = (key: TKey, vars?: Record<string, string | number>) => string;

/** 大厅列表每页条数(后端 page_size 上限 20,取 10 保证面板紧凑)。 */
export const HISTORY_PAGE_SIZE = 10;

/** 复盘发言分组的阶段顺序(与 DebateSpeechPanel 的 order 一致)。 */
export const HISTORY_PHASE_ORDER = [
  'preparation',
  'opening_argument',
  'rebuttal',
  'cross_examination',
  'cross_exam_summary',
  'free_debate',
  'closing_argument',
  'judging',
  'result',
  'game_over',
] as const;

/** 5 个评分维度 key(与后端 ScoreDimensions json tag 一致)。 */
export const HISTORY_DIM_KEYS = [
  'argument_quality',
  'logic_rigor',
  'language_expression',
  'team_coordination',
  'rebuttal_effectiveness',
] as const;

/** 模式枚举 → i18n 标签。 */
export function modeLabel(mode: DebateMode | string, t: TFn): string {
  switch (mode) {
    case 'two_team':
      return t('debate.mode.two_team');
    case 'three_team':
      return t('debate.mode.three_team');
    case 'four_team':
      return t('debate.mode.four_team');
    case 'five_team':
      return t('debate.mode.five_team');
    default:
      return mode;
  }
}

/** 辩位枚举 → i18n 标签。 */
export function roleLabel(role: DebateRole | string, t: TFn): string {
  switch (role) {
    case 'first':
      return t('debate.role.first');
    case 'second':
      return t('debate.role.second');
    case 'third':
      return t('debate.role.third');
    case 'fourth':
      return t('debate.role.fourth');
    default:
      return role;
  }
}

/** team_id → 立场标签(team_config 缺失时回退 #id)。 */
export function teamLabel(room: DebateHistoryRoom, teamId: number): string {
  if (!teamId) return '';
  const team = room.team_config?.find((x) => x.team_id === teamId);
  return team?.stance_label || `#${teamId}`;
}

/** "MeiTuan-model" → "MeiTuan"(与 DebateLobbyPage 同口径)。 */
export function shortModelKey(key: string): string {
  const idx = key.lastIndexOf('-');
  return idx > 0 ? key.slice(0, idx) : key;
}

/** 后端时间戳(unix 秒,>1e12 视为毫秒)→ 本地化字符串。 */
export function formatUnix(ts: number): string {
  if (!ts) return '—';
  const ms = ts > 1e12 ? ts : ts * 1000;
  return new Date(ms).toLocaleString();
}

/** 从发言记录反查最佳辩手姓名(找不到回退「座位 N」)。 */
export function bestDebaterName(
  room: DebateHistoryRoom,
  speeches: DebateHistorySpeech[],
  t: TFn,
): string {
  if (!room.best_debater_team_id && !room.best_debater_seat) return '—';
  const hit = speeches.find(
    (sp) =>
      sp.team_id === room.best_debater_team_id && sp.seat === room.best_debater_seat,
  );
  return hit?.speaker_name || t('debate.history.seat', { n: room.best_debater_seat });
}
