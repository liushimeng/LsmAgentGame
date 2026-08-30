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
// §20260830-01 — 死亡身份公开:⚰ 死亡页 role chip 的阵营三档配色。
import { roleTierOf } from '@/components/werewolf/roleFaction';
// §20260813-02 U4 — 夜间血迹图(S2)。spectator-only 夜间行动空间可视化。
import { NightBloodMap } from '@/components/werewolf/NightBloodMap';
// §20260814-01 U1 — 三个「写好了却从未被 import」的面板接线(§126/§130 清算)。
// 这三个组件自 §20260812-01 落地起零 import:MindMirror 与 TrustTrace 缺挂载点,
// PersonalReview 还缺后端路由(本批次一并补齐 GET .../review/:userId)。
import { MindMirrorPanel, type AgentHypothesisEntry } from '@/components/werewolf/MindMirrorPanel';
import { TrustTraceChart } from '@/components/werewolf/TrustTraceChart';
import { PersonalReviewPanel } from '@/components/werewolf/PersonalReviewPanel';
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
  | 'bloodmap'
  // §20260814-01 U1 — 三个接线修复的 tab。三者都**不是** spectator-only:
  //   mindmirror  — 人类直觉 vs Agent 逻辑,本来就是给「玩家本人」用的
  //   trusttrace  — TrustTraceEntry 只含 {seat, day, score},无身份字段(§135)
  //   review      — 个人复盘,后端已校验只能看自己的
  | 'mindmirror' | 'trusttrace' | 'review';

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
  // BUG-R233-P2-01 语义对齐(与上方 buildTimeline 同源):观战者仅终局
  // (status==='over')放行角色名,对局中保持 R204 纵深防御。原实现无条件
  // 遮蔽,终局复盘时 ⚱ 页全是「未知」与时间轴不一致 —— 随 §20260830-01
  // 死亡身份公开一并对齐。
  const isGameOver = gs.status === 'over';
  const hideRole = spectator && !isGameOver;
  return (
    <div className="ww-history-deaths">
      <h4>{t('werewolf.history.deaths.title')}</h4>
      <ul className="ww-history-deaths__list">
        {dead.map((d) => {
          // §20260830-01 — d.role 非空(服务端脱敏字段,RolePubliclyRevealed
          // 单点判定)= 身份已对全场公开 → 阵营三档 chip;空 → 「身份未公开」
          // 灰 chip 占位,避免空白被误读为缺数据。前端不得用
          // reveal_role_on_death 布尔自行推导可见性(§1.2 不变式 1)。
          // 未知角色枚举(已退役角色的历史回放)时 translate 回落为 key 原文,
          // 改显原始枚举值。
          const roleI18nKey = `werewolf.role.${d.role}` as any;
          const translated = d.role ? t(roleI18nKey) : '';
          const roleLabel = d.role
            ? (translated !== `werewolf.role.${d.role}` ? translated : d.role)
            : '';
          const tier = roleTierOf(d.role);
          const showRoleChip = !hideRole && !!roleLabel;
          return (
            <li key={`${d.seat}-${d.day}`} className={`ww-history-deaths__row verdict-${d.verdict || 'death'}`}>
              <span className="ww-history-deaths__day">D{d.day}</span>
              <span className="ww-history-deaths__seat">#{d.seat + 1}</span>
              {showRoleChip ? (
                <span
                  className={
                    'ww-history-deaths__role werewolf-seat__role-badge' +
                    (tier ? ` werewolf-seat__role-badge--${tier}` : '')
                  }
                  title={t('werewolf.dead_list.role_revealed' as any)}
                >
                  {roleLabel}
                </span>
              ) : (
                <span className="ww-history-deaths__role werewolf-seat__role-badge werewolf-seat__role-badge--hidden">
                  {t('werewolf.dead_list.role_hidden' as any)}
                </span>
              )}
              <span className="ww-history-deaths__cause">{d.cause}</span>
              {d.verdict && (
                <span className={`ww-history-deaths__verdict verdict-${d.verdict}`}>{d.verdict === 'execution' ? t('werewolf.verdict.execution' as any) : t('werewolf.verdict.death' as any)}</span>
              )}
            </li>
          );
        })}
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

  // 2026-08-14 §20260814-01 U1 — 修复既有缺陷:sub 状态从不随 spectator 重置。
  //
  // 原注释(下方 allTabs 上面)声称「sub 状态若玩家切换前是 infoflow,会落到
  // 第一个可见 tab」—— 但代码里**从来没有那个 fallback**。实际行为:
  // 非观战者若停留在某个 spectator-only tab 上(观战 → 入座、或 spectator
  // prop 因重连翻转),tab 条里没有 is-active 项,而 body 里每个渲染分支都带
  // `&& spectator` 守卫 ⇒ **整片空白**,用户以为抽屉坏了。
  //
  // 这是「注释描述了一个不存在的行为」的又一例(§20260812-04 教训 2:
  // 注释与实现不符应当像编译错误一样对待)。现在真正实现它。
  useEffect(() => {
    if (!spectator && SPECTATOR_ONLY_TABS.includes(sub)) {
      setSub('timeline');
    }
  }, [spectator, sub]);

  // §20260814-01 U1 — MindMirror 的 Agent 侧输入:把 bot_hypotheses 折叠成
  // 「每座位一个阵营倾向 + 置信度」。
  //
  // §135 硬约束:**只**导出 faction 与 confidence,绝不透传 role_guess 明文
  // (那是具体身份猜测,等同于把 Agent 的验人结论摊给所有人)。
  // 同一目标座位有多条假说时取置信度最高的那条。
  //
  // 后端只在 viewer<0 分支填 bot_hypotheses,故玩家侧恒为 [](面板降级为
  // 「只显示我的直觉」)—— 这是刻意的:MindMirror 的价值在于事后对照,
  // 而非在局中给玩家 Agent 的推理结果。
  const mindMirrorHypothesis = useMemo<AgentHypothesisEntry[]>(() => {
    const best = new Map<number, AgentHypothesisEntry>();
    for (const bh of gameState.bot_hypotheses ?? []) {
      for (const e of bh.entries ?? []) {
        const faction: AgentHypothesisEntry['faction'] =
          e.role_guess === 'werewolf' ? 'wolf'
            : e.role_guess === 'unknown' || !e.role_guess ? 'unknown'
              : 'good';
        const confidence = Math.max(0, Math.min(1, (e.confidence ?? 0) / 100));
        const prev = best.get(e.target_seat);
        if (!prev || confidence > prev.confidence) {
          best.set(e.target_seat, { seat: e.target_seat, faction, confidence });
        }
      }
    }
    return Array.from(best.values()).sort((a, b) => a.seat - b.seat);
  }, [gameState.bot_hypotheses]);

  // 本地玩家是否存活(MindMirror 只在人类存活时开放录入)。
  const isLocalPlayerAlive = useMemo(() => {
    const seat = gameState.my_seat;
    if (seat < 0) return false;
    return gameState.players?.find((p) => p.seat === seat)?.alive ?? false;
  }, [gameState.my_seat, gameState.players]);

  if (!open) return null;

  // 2026-08-10 §20260810-08 — 信息流 tab 仅 spectator 可见(与 §135 身份隔离一致)。
  // sub 落在 spectator-only tab 而当前非观战者时,由上方 useEffect 回退到 timeline。
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
    // §20260814-01 U1 — 接线修复:三个组件写好后从未被 import(§126)。
    { key: 'mindmirror', label: t('werewolf.history.subtab.mindmirror') },
    { key: 'trusttrace', label: t('werewolf.history.subtab.trusttrace') },
    { key: 'review',     label: t('werewolf.history.subtab.review') },
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
          {/* §20260814-01 U1 — 心镜:人类直觉 vs Agent 逻辑。
              agentHypothesis 由 bot_hypotheses 派生,而后端只在 viewer<0
              分支填充该字段(§135),故玩家侧恒为空数组 —— 面板会渲染
              「仅有我的直觉、Agent 侧暂无数据」的对照,这正是它的降级形态。
              §135 关键:只传 faction + confidence,**绝不**传 role_guess 明文。 */}
          {sub === 'mindmirror' && (
            <MindMirrorPanel
              roomId={gameState.room_id}
              mySeat={gameState.my_seat}
              agentHypothesis={mindMirrorHypothesis}
              isHumanInRoom={!spectator && gameState.my_seat >= 0}
              isLocalPlayerAlive={isLocalPlayerAlive}
            />
          )}
          {/* §20260814-01 U1 — 信任度轨迹。后端 trust_trace 由法官整局总结解析
              (judge_summary_bridge.go),终局前为空 → 组件自渲染空态。
              **不传 factionBySeat**:那会把阵营染色暴露给所有人(§135),
              组件对缺省值走 'unknown' 灰色单色,信息量不减。 */}
          {sub === 'trusttrace' && (
            <TrustTraceChart entries={gameState.trust_trace ?? []} />
          )}
          {/* §20260814-01 U1 — 个人复盘 4 维。走本批次新增的
              GET /api/games/werewolf/rooms/:roomId/review/:userId;
              后端校验「只能看自己的」(§135),故 userId 传当前登录者。
              observer(my_seat<0)没有可复盘的对局数据,渲染空态提示。 */}
          {sub === 'review' && (
            <PersonalReviewPanel
              roomId={gameState.room_id}
              userId={currentUserId ?? ''}
              open
              onClose={onClose}
            />
          )}
        </div>
      </aside>
    </div>
  );
};

export default HistoryDrawer;
