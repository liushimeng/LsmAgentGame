// HistoryDrawer — 2026-07-18 §UX-运行时;2026-07-22 §UX-法官布局新增「⚖️ 法官」tab;
// 2026-08-10 §20260810-08 — 新增「🗂 信息流」tab(spectator only,接 InfoFlowPanel)。
// 独立于 FactionDrawer 的"对局历史"抽屉(右上角 380px,与 FactionDrawer 同宽同位)。
// 2026-08-11 §20260811-02 U2 — 新增「🔮 假说」+「🧠 决策」两个 tab(均 spectator only)。
//   这两个字段后端早已下发(bot_hypotheses / bot_contexts[].decision_trail),
//   注释也写明了应渲染在本抽屉,但对应 tab 从未存在 —— §130「声明了却从不接线」
//   的反向复现(后端认真填了、前端从没读)。本次补齐消费者。
// 8 个 sub-tab(spectator=true 时)或 5 个 sub-tab(spectator=false 时):
//   ⏱ 时间轴 — 当前对局已发生事件(phase 切换 / 死亡 / 投票 / 警徽流 / 白痴翻牌)
//   🤖 独白 — 遍历 bot_contexts[spectator 才能看到 heart_thought]
//   ⚰ 死亡 — 遍历 all_dead_list_verbose
//   🏆 总结 — judge_summary + judge_model_memories
//   ⚖️ 法官 — 法官宣告历史 + 一举一动活动流(承接原 WerewolfTable 内嵌 JudgePanel,
//             避免法官 UI 挤压 13 人座位格与游戏界面重合)
//   🗂 信息流(spectator only)— InfoEntryJSON 时序图 + 疑似说漏嘴告警区
//             (§20260810-08 二期;见 InfoFlowPanel.tsx)
//   🔮 假说(spectator only)— 各 bot 的身份猜测 + 置信度条形图
//             (§20260811-02 U2;见 HypothesisPanel.tsx)
//   🧠 决策(spectator only)— 全场决策流回放(轮/阶段/工具/耗时)
//             (§20260811-02 U2;见 DecisionTrailPanel.tsx)
//
// 数据来源:game.state(全在 ClientGameState 已下发字段),无需新 API。
//
// i18n:werewolf.history.* + werewolf.infoflow.*,ESC 关闭,nowMs 自维护 1s tick。

import React, { useEffect, useMemo, useState } from 'react';
import type {
  WerewolfGameState,
  BotContextJSON,
  DeadPlayerJSON,
} from '@/types/werewolf';
import { useT } from '@/hooks/useT';
import { getEmotionDisplay } from './emotion';
import { phaseLabel } from '@/components/werewolf/phaseLabel';
import { RoomRunningClock } from '@/components/werewolf/RoomRunningClock';
import { JudgePanel } from '@/components/werewolf/JudgePanel';
import { GuessAccuracyCard } from '@/components/werewolf/GuessAccuracyCard';
import InfoFlowPanel from '@/components/werewolf/InfoFlowPanel';
// §20260811-02 U2 — 接线修复:bot_hypotheses / decision_trail 的首个前端消费者。
import HypothesisPanel from '@/components/werewolf/HypothesisPanel';
import DecisionTrailPanel from '@/components/werewolf/DecisionTrailPanel';
// §20260811-06 U3 — 公开推理链(spectator only)。
import ReasoningChainsPanel from '@/components/werewolf/ReasoningChainsPanel';
// §20260811-08 U1/U3 — 上帝视角面板。该组件自 §20260810-11 V1 落地起
// **从未被任何文件 import**(§126 教训:组件存在但未被 import 等于不存在),
// 本批次补齐 PerSeatPOV 数据后一并接线为第 10 个 sub-tab。
import GodModeView from '@/components/werewolf/GodModeView';
// §20260812-03 U1 — 阵营胜率热力图面板。仅观战者可见,§132 隐私隔离。
import { WinRateHeatmapPanel } from '@/components/werewolf/WinRateHeatmap';
// §20260813-02 U4 — 夜间血迹图(S2)。spectator-only 夜间行动空间可视化。
import { NightBloodMap } from '@/components/werewolf/NightBloodMap';
import { useAuthStore } from '@/store/auth.store';

interface HistoryDrawerProps {
  open: boolean;
  onClose: () => void;
  gameState: WerewolfGameState;
  spectator: boolean;
}

type SubTab =
  | 'timeline' | 'monologue' | 'deaths' | 'summary' | 'judge' | 'infoflow'
  // §20260811-02 U2 — 两个新 sub-tab,均 spectator only。
  | 'hypothesis' | 'decision'
  // §20260811-06 U3 — 公开推理链。
  | 'reasoning'
  // §20260811-08 U1/U3 — 上帝视角(全视角读心 + 公开技能行动)。
  | 'godmode'
  // §20260812-03 U1 — 阵营胜率热力图(仅观战者,§132 隐私隔离)。
  | 'heatmap'
  // §20260813-02 U4 — 夜间血迹图(S2,仅观战者)。
  | 'bloodmap';

/** §20260811-02 U2 + §20260811-06 U3 + §20260812-03 U1 + §20260813-02 U4 — spectator 专属 sub-tab 白名单。 */
const SPECTATOR_ONLY_TABS: SubTab[] = ['infoflow', 'hypothesis', 'decision', 'reasoning', 'godmode', 'heatmap', 'bloodmap'];

interface TimelineEntry {
  /** 内部 key */
  key: string;
  /** 渲染图标 */
  emoji: string;
  /** 类型文案 */
  type: string;
  /** 详情文本(已格式) */
  detail: string;
  /** 天数;0 = 入夜前 */
  day: number;
}

function buildTimeline(gs: WerewolfGameState, t: (k: any) => string, spectator = false): TimelineEntry[] {
  const out: TimelineEntry[] = [];
  // BUG-R204-SEC-01: 观战者统一用「未知」占位角色名。
  //
  // BUG-R233-P2-01 (2026-08-02): 终局例外 — 与 WerewolfTable.tsx 同源。Status==='over'
  // 时 §135 RolePubliclyRevealed() clause ① 服务端对全员亮牌,允许观战者复盘。
  // 对局进行中保持 R204 纵深防御不变。
  const unknownRole = t('werewolf.role.unknown' as any);
  const isGameOver = gs.status === 'over';
  // spectator=true + 未终局 → 全程占位; spectator=true + 已终局 → 放行角色。
  const hideRoleForSpectator = spectator && !isGameOver;

  // 阶段切换在前:`game.started` 后 day 推进。
  out.push({
    key: `phase-${gs.phase}-${gs.day}`,
    emoji: '🌗',
    type: t('werewolf.history.timeline.phase'),
    detail: phaseLabel(t, gs.phase) ?? String(gs.phase),
    day: gs.day,
  });

  // 投票 — 用 votes map 拼一次"已投票数 vs 总存活"
  if (gs.votes && Object.keys(gs.votes).length > 0) {
    const entries = Object.entries(gs.votes)
      .filter(([k]) => Number(k) >= 0)
      .map(([seat, count]) => `→ #${Number(seat) + 1} × ${count}`);
    if (entries.length > 0) {
      out.push({
        key: `vote-${Object.keys(gs.votes).join(',')}`,
        emoji: '🗳',
        type: t('werewolf.history.timeline.vote'),
        detail: entries.join('  '),
        day: gs.day,
      });
    }
  }

  // 警徽流
  if (Array.isArray(gs.sheriff_streams)) {
    const slots = (gs.sheriff_streams as number[])
      .map((s, i) => (s >= 0 ? ` slot${i + 1}=#${s + 1}` : null))
      .filter(Boolean) as string[];
    if (slots.length > 0) {
      out.push({
        key: `sheriff-${gs.sheriff_streams?.join(',')}`,
        emoji: '🎖',
        type: t('werewolf.history.timeline.sheriffStream'),
        detail: slots.join(' / '),
        day: gs.day,
      });
    }
  }

  // 白痴翻牌
  if (Array.isArray(gs.idiot_revealed_seats) && gs.idiot_revealed_seats.length > 0) {
    out.push({
      key: `idiot-${gs.idiot_revealed_seats.join(',')}`,
      emoji: '🃏',
      type: t('werewolf.history.timeline.idiotReveal'),
      detail: gs.idiot_revealed_seats.map((s) => `#${s + 1}`).join('  '),
      day: gs.day,
    });
  }

  // 全部死亡(已稳固列表,非仅昨夜)
  // BUG-R204-SEC-01: 观战者不显示死亡玩家角色名,仅显示「未知」。
  const deadList = (gs.all_dead_list_verbose ?? []) as DeadPlayerJSON[];
  for (const d of deadList) {
    out.push({
      key: `dead-${d.seat}-${d.day}`,
      emoji: d.verdict === 'execution' ? '⚱' : '☠',
      type: t('werewolf.history.timeline.death'),
      detail: `#${d.seat + 1} ${d.account} · ${hideRoleForSpectator ? unknownRole : (d.role ? t(`werewolf.role.${d.role}` as any) : '?')} · ${d.cause}${d.verdict ? ` [${d.verdict === 'execution' ? t('werewolf.verdict.execution' as any) : t('werewolf.verdict.death' as any)}]` : ''}`,
      day: d.day,
    });
  }

  // 2026-08-08 §20260808-02 — 时间轴补遗言条目(缺陷 D)。
  // 进行中一轮:phase_extra.dead_list;历史轮:all_dead_list_verbose。
  // 仅 last_words_status 为 spoken/skipped 的项各推一条「事件发生了」的事实,
  // 不放遗言全文(全文在聊天流与 500K 队列,避免与 🤖 独白/聊天记录重复)。
  // day 取条目 day;spoken=💀 已发表 / skipped=⏭ 已放弃。
  const seenLyricSeats = new Set<number>();
  const pushLyricEntry = (d: DeadPlayerJSON) => {
    if (d.last_words_status !== 'spoken' && d.last_words_status !== 'skipped') return;
    if (seenLyricSeats.has(d.seat)) return;
    seenLyricSeats.add(d.seat);
    const spoken = d.last_words_status === 'spoken';
    out.push({
      key: `lastwords-${d.seat}-${d.day}`,
      emoji: spoken ? '💀' : '⏭',
      type: t('werewolf.lastWords.title'),
      detail: `#${d.seat + 1} ${d.account} · ${spoken ? t('werewolf.lastWords.statusSpoken') : t('werewolf.lastWords.statusSkipped')}`,
      day: d.day,
    });
  };
  for (const d of deadList) pushLyricEntry(d);
  const extraDead = (gs.phase_extra?.dead_list ?? []) as DeadPlayerJSON[];
  for (const d of extraDead) pushLyricEntry(d);

  // 按 day 升序,稳定排序
  out.sort((a, b) => a.day - b.day);
  return out;
}

interface SubPanelProps {
  gs: WerewolfGameState;
  t: (k: any) => string;
  spectator: boolean;
}

function TimelinePanel({ gs, t, spectator }: { gs: WerewolfGameState; t: (k: any) => string; spectator: boolean }) {
  const entries = useMemo(() => buildTimeline(gs, t, spectator), [gs, t, spectator]);
  if (entries.length === 0) {
    return <div className="ww-history-panel__empty">{t('werewolf.history.timeline.empty')}</div>;
  }
  return (
    <ul className="ww-history-timeline">
      {entries.map((e) => (
        <li key={e.key} className="ww-history-timeline__row">
          <span className="ww-history-timeline__emoji" aria-hidden>{e.emoji}</span>
          <span className="ww-history-timeline__day">[D{e.day}]</span>
          <span className="ww-history-timeline__type">{e.type}</span>
          <span className="ww-history-timeline__detail">{e.detail}</span>
        </li>
      ))}
    </ul>
  );
}

function MonologuePanel({ gs, t, spectator }: SubPanelProps) {
  const bots = (gs.bot_contexts ?? []) as BotContextJSON[];
  if (bots.length === 0) {
    return <div className="ww-history-panel__empty">{t('werewolf.history.monologue.empty')}</div>;
  }
  const shown = bots.filter((b) =>
    spectator ? !!b.heart_thought || !!b.last_decision_summary : !!b.last_decision_summary,
  );
  if (shown.length === 0) {
    return <div className="ww-history-panel__empty">{t('werewolf.history.monologue.empty')}</div>;
  }
  return (
    <div className="ww-history-monologue">
      {spectator && (
        <div className="ww-history-monologue__hint">{t('werewolf.history.monologue.heartOnly')}</div>
      )}
      {shown.map((b) => (
        <details key={b.seat} className="ww-history-monologue__agent">
          <summary>
            <span className="ww-history-monologue__seat">#{b.seat + 1}</span>
            <span className="ww-history-monologue__model">{b.model ?? 'unknown'}</span>
            {b.llm_call_phase && b.llm_call_phase !== 'idle' && (
              <span className="ww-history-monologue__phase">{b.llm_call_phase}</span>
            )}
          </summary>
          {spectator && b.heart_thought && (
            <blockquote className="ww-history-monologue__heart">💭 {b.heart_thought}</blockquote>
          )}
          {b.last_decision_summary && (
            <div className="ww-history-monologue__summary">📋 {b.last_decision_summary}</div>
          )}
          {/* 2026-08-04 §重构 — 情绪变化曲线(§重构 em 共享 metadata)。仅 spectator 可见,
              沿用 §119 HeartThought 协议层隔离原则:真人玩家不应看到其它 Agent 内心状态,
              emotion_history 仅 spectator 可查。 */}
          {spectator && Array.isArray(b.emotion_history) && b.emotion_history.length > 0 && (
            <details className="ww-history-monologue__emotion-history">
              <summary>
                🎭 {t('werewolf.history.emotionHistory.title')} ({b.emotion_history.length})
              </summary>
              <ul className="ww-history-monologue__emotion-list">
                {b.emotion_history.map((h, idx) => {
                  const emo = getEmotionDisplay(h.emotion);
                  const time = new Date(h.at_ms).toLocaleTimeString();
                  return (
                    <li key={idx} className="ww-history-monologue__emotion-row">
                      <span className="time">[{time}]</span>{' '}
                      <span className="emoji" aria-hidden>{emo?.emoji ?? '🎭'}</span>{' '}
                      <span className="label">{emo?.label ?? h.emotion}</span>
                      {h.reason && <span className="reason"> — {h.reason}</span>}
                    </li>
                  );
                })}
              </ul>
            </details>
          )}
        </details>
      ))}
    </div>
  );
}

function DeathsPanel({ gs, t, spectator }: SubPanelProps) {
  const dead = (gs.all_dead_list_verbose ?? []) as DeadPlayerJSON[];
  if (dead.length === 0) {
    return <div className="ww-history-panel__empty">{t('werewolf.history.deaths.empty')}</div>;
  }
  return (
    <div className="ww-history-deaths">
      <h4>{t('werewolf.history.deaths.title')}</h4>
      <ul className="ww-history-deaths__list">
        {dead.map((d) => (
          <li key={`${d.seat}-${d.day}`} className={`ww-history-deaths__row verdict-${d.verdict || 'death'}`}>
            <span className="ww-history-deaths__day">D{d.day}</span>
            <span className="ww-history-deaths__seat">#{d.seat + 1}</span>
            {/* BUG-R204-SEC-01: 观战者不显示死亡玩家角色名。 */}
            <span className="ww-history-deaths__role">{spectator ? t('werewolf.role.unknown' as any) : (d.role ? t(`werewolf.role.${d.role}` as any) : '?')}</span>
            <span className="ww-history-deaths__cause">{d.cause}</span>
            {d.verdict && (
              <span className={`ww-history-deaths__verdict verdict-${d.verdict}`}>{d.verdict === 'execution' ? t('werewolf.verdict.execution' as any) : t('werewolf.verdict.death' as any)}</span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function SummaryPanel({ gs, t, spectator }: SubPanelProps) {
  const mem = gs.judge_model_memories ?? {};
  const memKeys = Object.keys(mem);
  // §20260809-02 U3:从 gs.players 收集真实角色,供 GuessAccuracyCard 渲染。
  // 仅在终局 status==='over' 时渲染(终局前 §135 身份不公开)。
  const isOver = gs.status === 'over';
  const actualRoles: Record<number, string> = {};
  if (isOver && Array.isArray(gs.players)) {
    for (const p of gs.players) {
      const seat = (p as any).seat;
      const role = (p as any).role;
      if (typeof seat === 'number' && typeof role === 'string' && role) {
        actualRoles[seat] = role;
      }
    }
  }
  if (!gs.judge_summary && memKeys.length === 0) {
    return <div className="ww-history-panel__empty">{t('werewolf.history.summary.empty')}</div>;
  }
  return (
    <div className="ww-history-summary">
      {gs.judge_summary && (
        <pre className="ww-history-summary__text">{gs.judge_summary}</pre>
      )}
      {memKeys.length > 0 && (
        <>
          <h4>{t('werewolf.history.summary.modelMemory')}</h4>
          {memKeys.map((k) => (
            <details key={k} className="ww-history-summary__model">
              <summary>{k}</summary>
              <pre>{(mem[k] ?? []).join('\n')}</pre>
            </details>
          ))}
        </>
      )}
      {/* §20260809-02 U3:人类身份猜测准确率卡(仅终局 + 非 spectator)。 */}
      {isOver && !spectator && (
        <GuessAccuracyCard
          roomId={gs.room_id ?? ''}
          account={(gs as any).my_user_id ? `uid_${(gs as any).my_user_id}` : 'anon_unknown'}
          actualRoles={actualRoles}
        />
      )}
    </div>
  );
}

function JudgeSubPanel({ gs }: { gs: WerewolfGameState }) {
  return (
    <JudgePanel
      enabled={!!gs.judge_enabled}
      context={gs.judge_context}
      pending={gs.judge_pending_announce}
    />
  );
}

export const HistoryDrawer: React.FC<HistoryDrawerProps> = ({
  open, onClose, gameState, spectator,
}) => {
  const t = useT();
  const [sub, setSub] = useState<SubTab>('timeline');
  // §20260811-08 U1/U3 — GodModeView 的双重身份校验需要当前 user_id。
  const currentUserId = useAuthStore((s) => s.userId);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  // 2026-08-10 §20260810-08 — 信息流 tab 仅 spectator 可见(与 §135 身份隔离一致);
  // 这里按 spectator 过滤 tabs,sub 状态若玩家切换前是 infoflow,会落到第一个可见 tab。
  const allTabs: { key: SubTab; label: string }[] = [
    { key: 'timeline',  label: t('werewolf.history.subtab.timeline') },
    { key: 'monologue', label: t('werewolf.history.subtab.monologue') },
    { key: 'deaths',    label: t('werewolf.history.subtab.deaths') },
    { key: 'summary',   label: t('werewolf.history.subtab.summary') },
    { key: 'judge',     label: t('werewolf.history.subtab.judge') },
    { key: 'infoflow',  label: t('werewolf.history.subtab.infoflow') },
    // §20260811-02 U2 — 接线修复:后端早已下发,前端首次渲染。
    { key: 'hypothesis', label: t('werewolf.history.subtab.hypothesis') },
    { key: 'decision',   label: t('werewolf.history.subtab.decision') },
    // §20260811-06 U3 — 公开推理链(spectator only,§135 隔离)。
    { key: 'reasoning',  label: t('werewolf.history.subtab.reasoning') },
    // §20260811-08 U1/U3 — 上帝视角(spectator only)。
    { key: 'godmode',    label: t('werewolf.history.subtab.godmode') },
    // §20260812-03 U1 — 阵营胜率热力图(spectator only,§132 隐私隔离)。
    { key: 'heatmap',    label: t('werewolf.history.subtab.heatmap') },
    // §20260813-02 U4 — 夜间血迹图(S2,spectator only)。
    { key: 'bloodmap',   label: t('werewolf.history.subtab.bloodmap') },
  ];
  const tabs = spectator
    ? allTabs
    : allTabs.filter((tb) => !SPECTATOR_ONLY_TABS.includes(tb.key));

  return (
    <div
      className="ww-history-drawer"
      data-testid="ww-history-drawer"
      role="dialog"
      aria-modal="true"
      aria-label={t('werewolf.history.drawerTitle')}
    >
      <div className="ww-history-drawer__overlay" onClick={onClose} />
      <aside className="ww-history-drawer__panel">
        <header className="ww-history-drawer__header">
          <h3 className="ww-history-drawer__title">{t('werewolf.history.drawerTitle')}</h3>
          <RoomRunningClock
            gameStartedAt={gameState.game_started_at}
            status={gameState.status}
          />
          <button
            type="button"
            className="ww-history-drawer__close"
            onClick={onClose}
            aria-label={t('werewolf.drawer.close')}
          >×</button>
        </header>
        <nav className="ww-history-drawer__tabs">
          {tabs.map((tb) => (
            <button
              key={tb.key}
              type="button"
              className={`ww-history-drawer__tab${sub === tb.key ? ' is-active' : ''}`}
              onClick={() => setSub(tb.key)}
              data-testid={`ww-history-tab-${tb.key}`}
            >
              {tb.label}
            </button>
          ))}
        </nav>
        <div className="ww-history-drawer__body">
          {sub === 'timeline'  && <TimelinePanel gs={gameState} t={t} spectator={spectator} />}
          {sub === 'monologue' && <MonologuePanel gs={gameState} t={t} spectator={spectator} />}
          {sub === 'deaths'    && <DeathsPanel gs={gameState} t={t} spectator={spectator} />}
          {sub === 'summary'   && <SummaryPanel gs={gameState} t={t} spectator={spectator} />}
          {sub === 'judge'     && <JudgeSubPanel gs={gameState} />}
          {sub === 'infoflow'  && spectator && <InfoFlowPanel gameState={gameState} />}
          {/* §20260811-02 U2 — spectator 守卫是纵深防御第二道:后端已分别在
              view.go:1039(假说)与 room_state.go:557(决策)对玩家清空(§135)。 */}
          {sub === 'hypothesis' && spectator && <HypothesisPanel gameState={gameState} />}
          {sub === 'decision'   && spectator && <DecisionTrailPanel gameState={gameState} />}
          {/* §20260811-06 U3 — 公开推理链(spectator only,后端 room_state.go 已清空玩家分支)。 */}
          {sub === 'reasoning'  && spectator && <ReasoningChainsPanel gameState={gameState} />}
          {/* §20260811-08 U1/U3 — 上帝视角。后端仅在 viewer<0 分支填充 cs.god_mode
              (view.go:1271),此处 spectator 守卫是纵深防御第二道(§135)。 */}
          {sub === 'godmode'    && spectator && gameState.god_mode && (
            <GodModeView
              snapshot={gameState.god_mode}
              players={gameState.players}
              currentUserId={currentUserId ?? ''}
              isSpectator={spectator}
            />
          )}
          {/* §20260812-03 U1 — 阵营胜率热力图(spectator only,后端 view.go:1379 仅在
              viewer<0 分支填充 cs.win_rate_probability,§132 隐私隔离)。 */}
          {sub === 'heatmap'    && spectator && (
            <WinRateHeatmapPanel
              probabilities={gameState.win_rate_probability}
              players={gameState.players}
              t={t}
            />
          )}
          {/* §20260813-02 U4 — 夜间血迹图(S2)。后端仅在 viewer<0 分支填充
              god_mode(含 wolf_kills / guard_protect_entries 增量字段),
              此处 spectator 守卫是纵深防御第二道(§135)。 */}
          {sub === 'bloodmap'   && spectator && gameState.god_mode && (
            <NightBloodMap
              snapshot={gameState.god_mode}
              players={gameState.players}
            />
          )}
        </div>
      </aside>
    </div>
  );
};

export default HistoryDrawer;
