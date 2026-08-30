/**
 * DayNightOverlay.tsx — 2026-07-10 天黑请闭眼 / 天亮了 视觉特效
 *
 * 监听 gameState.phase 变化,在以下场景展示全屏覆盖层:
 *   - 夜间阶段进入 → 「🌙 天黑请闭眼」
 *   - dawn 进入 → 「🌅 天亮了」
 *   - 白天阶段进入 → 「☀️ 白天开始」(柔和)
 *
 * 用 useEffect + setTimeout 自动消失,不阻塞 UI 交互(pointer-events: none)。
 *
 * 配色 + 动画通过 globals.css .ww-daynight-overlay-* 类实现;本文件只负责触发。
 *
 * ⚠️ §20260811-08 U4 契约 —— NIGHT_PHASES 必须与后端引擎
 * `ServerGo/game/werewolf/engine.go` 的 `Phase.IsNight()` **逐项一致**。
 * 本 Set 是该函数的一份手工副本,历史上曾连续落后三个版本:
 *   §134 守卫(night_guard)/ §猎魔人(night_demon_hunter)/
 *   §20260810-09 警长定序(sheriff_order) 三次新增 phase 都漏改了这里,
 * 导致 13 人局带守卫时 night_guard 阶段没有夜晚遮罩,遮罩整整晚一个阶段才出现。
 *
 * 新增 phase 时的同步清单已由「五处」扩充为「六处」(见 CLAUDE.md §13 lesson 134):
 *   SkipPhaseAction / watchdogActingSeat / dispatchQuarantinedSkipLocked /
 *   isActingPhase / defaultPhaseDeadlineSec / **本文件的 NIGHT_PHASES|DAY_PHASES**
 */

import { useEffect, useState } from 'react';
import type { WerewolfPhase } from '@/types/werewolf';

interface Props {
  phase: WerewolfPhase | string;
}

type OverlayKind = 'night' | 'dawn' | 'day' | null;

// 与 engine.go `Phase.IsNight()` 逐项对齐(§20260811-08 U4)。
const NIGHT_PHASES = new Set<string>([
  'pre_wolves',
  'night_guard', // §134 守卫盲守(在狼刀之前)—— 曾遗漏
  'night_wolves',
  'night_seer',
  'night_witch',
  'night_demon_hunter', // §猎魔人 —— 曾遗漏
]);

const DAWN_PHASE = 'dawn';

const DAY_PHASES = new Set<string>([
  'speak',
  'vote',
  'sheriff',
  'sheriff_order', // §20260810-09 警长定序 —— 曾遗漏
  'death_lyric',
  'hunter_shoot',
  'idiot_reveal',
  'suicide_take', // §20260830-02 自爆带走 —— 白天结算阶段
]);

function pickOverlay(phase: string): OverlayKind {
  if (NIGHT_PHASES.has(phase)) return 'night';
  if (phase === DAWN_PHASE) return 'dawn';
  if (DAY_PHASES.has(phase)) return 'day';
  return null;
}

interface OverlayConfig {
  emoji: string;
  title: string;
  subtitle: string;
  durationMs: number;
  klass: string;
}

const OVERLAY_CONFIG: Record<Exclude<OverlayKind, null>, OverlayConfig> = {
  night: {
    emoji: '🌙',
    title: '天黑请闭眼',
    subtitle: '夜晚降临,请勿暴露身份信息',
    durationMs: 2500,
    klass: 'ww-daynight-overlay--night',
  },
  dawn: {
    emoji: '🌅',
    title: '天亮了',
    subtitle: '公布昨夜伤亡',
    durationMs: 1800,
    klass: 'ww-daynight-overlay--dawn',
  },
  day: {
    emoji: '☀️',
    title: '白天开始',
    subtitle: '请依次发言 / 投票',
    durationMs: 1200,
    klass: 'ww-daynight-overlay--day',
  },
};

export function DayNightOverlay({ phase }: Props) {
  const [active, setActive] = useState<OverlayKind>(null);

  useEffect(() => {
    const next = pickOverlay(String(phase));
    if (next === null) {
      setActive(null);
      return;
    }
    setActive(next);
    const t = setTimeout(() => setActive(null), OVERLAY_CONFIG[next].durationMs);
    return () => clearTimeout(t);
  }, [phase]);

  if (!active) return null;
  const cfg = OVERLAY_CONFIG[active];

  return (
    <div
      className={`ww-daynight-overlay ${cfg.klass}`}
      role="presentation"
      data-testid={`daynight-overlay-${active}`}
    >
      <div className="ww-daynight-overlay__inner">
        <div className="ww-daynight-overlay__emoji" aria-hidden="true">
          {cfg.emoji}
        </div>
        <div className="ww-daynight-overlay__title">{cfg.title}</div>
        <div className="ww-daynight-overlay__subtitle">{cfg.subtitle}</div>
      </div>
    </div>
  );
}

export default DayNightOverlay;