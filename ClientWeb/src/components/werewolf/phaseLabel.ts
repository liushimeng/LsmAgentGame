/**
 * 狼人杀阶段显示辅助函数。
 *
 * 单纯做一个 i18n key → label 的查找,但把以下逻辑抽到一处,避免三处 UI
 * (WerewolfTable 大横幅 / GameInfoPanel 信息行 / WerewolfGamePage 副标题)
 * 各自重复维护映射。
 *
 * 关键约束:
 *   - i18n key 形如 `werewolf.phase.<enum>`(zh-CN/en/ja 都已登记完整 10 项)
 *   - 缺翻译时不能直接回退成 enum 字符串:`night_witch` 露给玩家没人看得懂
 *   - 用一线 fallback:回退到 zh-CN 的同名 key(translate() 已经做)
 *   - 仍无解时直接回退英文表层名(如 "Night · Witch Awakens"),最后才返回 enum
 *     作为兜底(理论上不应触发)
 */

import type { TKey } from '@/i18n';

type Translate = (key: TKey, vars?: Record<string, string | number>) => string;

const PHASE_OVERRIDES: Record<string, string> = {
  filling: 'Waiting for players…',
  // BUG Round 40 §95: pre_wolves 升级为"首夜强制发言阶段"——每名玩家在
  // 身份确认后必须至少发言 1 轮(可配 1-3 轮)。
  pre_wolves: 'First-Night Forced Speak',
  // 2026-07-29 §134 守卫角色 — 「盲守」阶段(在狼刀之前),守卫无法看到当晚
  // 狼刀目标,必须靠推理预判。
  night_guard: 'Night · Guard Awakens',
  night_wolves: 'Night · Wolves Awakens',
  night_seer: 'Night · Seer Awakens',
  night_witch: 'Night · Witch Awakens',
  dawn: 'Dawn · Death Announced',
  sheriff: 'Sheriff Election',
  speak: 'Day · Discussion',
  vote: 'Day · Vote',
  // 2026-07-10 12 人局 — 白痴翻牌阶段(投票放逐白痴时触发)。
  idiot_reveal: 'Idiot Reveal',
  death_lyric: 'Last Words',
  hunter_shoot: 'Hunter Shooting',
  // §20260830-02 — 自爆带走:自爆狼(已死)选择带走一名存活玩家。
  suicide_take: 'Suicide Take',
  over: 'Game Over',
};

/**
 * 把后端给的阶段 enum 转成玩家可读的本地化字符串。
 * @param t useT() 返回的翻译函数
 * @param phase 服务端下发的阶段值,可能为 undefined / unknown
 * @returns 本地化文案;若 phase 为空则返回 null(UI 应自行决定占位)
 */
export function phaseLabel(t: Translate, phase: string | null | undefined): string | null {
  if (phase == null || phase === '') {
    return null;
  }
  const key = `werewolf.phase.${phase}` as TKey;
  const translated = t(key);
  // translate() 在缺 key 时回退到 key 本身("werewolf.phase.night_witch"),
  // 我们在这里再做一次兜底,只取原始 enum 不理想,转回覆写表。
  if (translated && translated !== key) {
    return translated;
  }
  if (PHASE_OVERRIDES[phase]) {
    return PHASE_OVERRIDES[phase];
  }
  // 阶段字符串没问题,只是 i18n 字典里漏登记 —— 至少别把 "night_witch"
  // 这种后端 enum 显眼地曝给玩家。返回空字符串让上层决定是否占位。
  return '';
}

// 阶段对应的 emoji 图标,纯视觉层;就算 i18n 没登记也不会失控。
export const PHASE_ICONS: Record<string, string> = {
  filling: '🕯',
  pre_wolves: '🕯',
  // 2026-07-29 §134 守卫角色:守卫护盾图标
  night_guard: '🛡️',
  night_wolves: '🐺',
  night_seer: '🔮',
  night_witch: '🧪',
  dawn: '🌅',
  sheriff: '🎖',
  speak: '💬',
  vote: '🗳️',
  // 2026-07-10 12 人局 — 白痴翻牌 + 遗言
  idiot_reveal: '🃏',
  death_lyric: '🪦',
  hunter_shoot: '🔫',
  // §20260830-02 — 自爆带走
  suicide_take: '🧨',
  over: '🏁',
};

export function phaseIcon(phase: string | null | undefined): string {
  if (phase == null || phase === '') {
    return '🕯';
  }
  return PHASE_ICONS[phase] ?? '☀️';
}
