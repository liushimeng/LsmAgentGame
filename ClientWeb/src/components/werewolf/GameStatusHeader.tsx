/**
 * GameStatusHeader.tsx — 2026-08-07 §狼人杀13人局游戏状态重构
 *
 * 合并原 werewolf-main-header + JudgeActionBar + WerewolfStatusBar 三块独立
 * 组件,统一到一个紧凑、可折叠的粘性头部,顶部总高度 ≤ 60px(展开)/ ≤ 32px(折叠)。
 *
 * 设计要点:
 *  - 字段保留率 100%:所有原 chip 信息(阶段倒计时 / Agent 数 / API/Token /
 *    法官状态 / 房间信息 / 历史按钮)全部保留。
 *  - 可折叠:localStorage 键 `werewolf.gameStatusHeader.collapsed` 持久化。
 *  - 折叠态仅保留关键 chip,展开态显示完整 chip 矩阵。
 *  - 法官跑马灯全文不再持续滚动,改为 ⚙️announce chip + tooltip 摘要;
 *    完整宣告历史由 HistoryDrawer ⚖️ 法官 tab 承载(原 JudgePanel)。
 *  - 复用原 WerewolfStatusBar 的聚合函数(统计 active/calling/streaming/
 *    retrying/quarantined)和 PhaseClock 的 deadline 计算逻辑,字段语义不变。
 */

import React, { useEffect, useMemo, useRef, useState } from 'react';
import type { WerewolfGameState } from '@/types/werewolf';
import { useT } from '@/hooks/useT';
import { formatK } from '@/shared/utils/format';
import { phaseLabel } from './phaseLabel';

interface GameStatusHeaderProps {
  gameState: WerewolfGameState;
  /** 是否当前有真人玩家在房间(影响 deadline 紧度 chip) */
  isHumanInRoom: boolean;
  /** 观众模式标志 */
  spectator: boolean;
  /** URL 上的房间 ID(gameState 未下发时兜底) */
  roomId: string;
  /** 父级 1s setInterval 提供的 nowMs */
  nowMs: number;
  /** 触发历史抽屉 */
  onOpenHistory: () => void;
}

const LS_COLLAPSED = 'werewolf.gameStatusHeader.collapsed';

function readCollapsedPref(): boolean {
  try {
    return typeof window !== 'undefined' && window.localStorage.getItem(LS_COLLAPSED) === '1';
  } catch {
    return false;
  }
}

function writeCollapsedPref(collapsed: boolean): void {
  try {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(LS_COLLAPSED, collapsed ? '1' : '0');
    }
  } catch {
    /* 隐身模式降级 */
  }
}

function formatHMS(totalSec: number): string {
  const s = Math.max(0, Math.floor(totalSec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = s % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`;
  return `${m}:${String(ss).padStart(2, '0')}`;
}

export const GameStatusHeader: React.FC<GameStatusHeaderProps> = ({
  gameState,
  isHumanInRoom,
  spectator,
  roomId,
  nowMs,
  onOpenHistory,
}) => {
  const t = useT();

  // 折叠状态(localStorage 持久化)。
  const [collapsed, setCollapsed] = useState<boolean>(readCollapsedPref);
  useEffect(() => {
    writeCollapsedPref(collapsed);
  }, [collapsed]);

  // 法官新宣告 2s pulse(对齐 §130 法官顶部 banner 设计期望)。
  const [judgePulse, setJudgePulse] = useState(false);
  const prevAnnouncement = useRef<string | undefined>(undefined);
  useEffect(() => {
    const cur = gameState.judge_context?.last_announcement;
    if (cur && cur !== prevAnnouncement.current) {
      prevAnnouncement.current = cur;
      setJudgePulse(true);
      const id = setTimeout(() => setJudgePulse(false), 2000);
      return () => clearTimeout(id);
    }
    prevAnnouncement.current = cur;
    return undefined;
  }, [gameState.judge_context?.last_announcement]);

  const phaseExtra = (gameState as any).phase_extra;
  const phaseDeadlineAt: string | undefined = phaseExtra?.phase_deadline_at;
  const [remaining, setRemaining] = useState<number | undefined>(undefined);
  const isOverdue = remaining === 0 && !!phaseDeadlineAt;

  useEffect(() => {
    if (!phaseDeadlineAt) {
      setRemaining(undefined);
      return;
    }
    const deadline = new Date(phaseDeadlineAt).getTime();
    const tick = () => {
      const diff = Math.max(0, Math.floor((deadline - nowMs) / 1000));
      setRemaining(diff);
    };
    tick();
  }, [phaseDeadlineAt, nowMs]);

  const bots = gameState.bot_contexts ?? [];
  const isOver = gameState.status === 'over';

  // 复用原 §127 Agent 聚合函数。
  const stats = useMemo(() => {
    let active = 0, calling = 0, retrying = 0, streaming = 0, quarantined = 0;
    let lastBroadcast = '';
    if (!isOver) {
      for (const b of bots) {
        const phase = b.llm_call_phase ?? 'idle';
        if (phase === 'calling') calling++;
        else if (phase === 'streaming') streaming++;
        else if (phase === 'retrying') retrying++;
        if (phase !== 'idle' && phase !== 'quarantined') active++;
        if (b.quarantined || phase === 'quarantined') quarantined++;
        if (b.quarantine_broadcast) lastBroadcast = b.quarantine_broadcast;
      }
    }
    return { active, calling, retrying, streaming, quarantined, lastBroadcast };
  }, [bots, isOver]);

  const agentStats = gameState.agent_stats;
  const hasAgentStats = !!agentStats && (agentStats.agent_count > 0 || agentStats.judge_enabled);
  const judgeStats = agentStats?.judge_enabled ? agentStats : null;

  const jc = gameState.judge_context;
  const judgeEnabled = !!gameState.judge_enabled;
  const judgeQuarantined = !!jc?.quarantined;
  const judgePending = gameState.judge_pending_announce;
  const judgeModel = jc?.model;
  const judgeTool = jc?.last_tool;
  const judgeAnnouncement = jc?.last_announcement;
  const judgeSummaryReady = !!(jc?.last_summary_sections && (
    jc.last_summary_sections.outcome ||
    jc.last_summary_sections.turning_point ||
    jc.last_summary_sections.role_timeline ||
    jc.last_summary_sections.mvp ||
    jc.last_summary_sections.wolf_deception
  ));

  // 房间运行时间(沿用 RoomRunningClock 字段语义)。
  const gameStartedAt = gameState.game_started_at;
  const hasClock = !!gameStartedAt && gameStartedAt > 0;
  const elapsedSec = hasClock
    ? Math.max(0, Math.floor((nowMs - gameStartedAt * 1000) / 1000))
    : 0;
  const clockLabel = !hasClock
    ? t('werewolf.history.clock.idle')
    : isOver
      ? t('werewolf.history.clock.ended').replace('{h}:{m}:{s}', formatHMS(elapsedSec))
      : t('werewolf.history.clock.running').replace('{h}:{m}:{s}', formatHMS(elapsedSec));

  // §20260817-03 U3 — 每小时平均 Token 消耗(派生展示:累计 ÷ 已运行小时)。
  // 与统计组 token chip 同源(agent_stats.total_api_tokens,不含法官统计);
  // formatK 自动 K/M 缩放。守卫:开局不足 60s 外推误差过大不显示;
  // token 为 0 不显示。status=over 后分子分母都不再增长,数值自然冻结为整局均值。
  const totalApiTokens = agentStats?.total_api_tokens ?? 0;
  const showTokenRate = hasClock && elapsedSec >= 60 && totalApiTokens > 0;
  const tokenRatePerHour = showTokenRate
    ? Math.round(totalApiTokens / (elapsedSec / 3600))
    : 0;

  const phaseText = phaseLabel(t, gameState.phase) ?? '—';
  const winnerText = isOver
    ? t('werewolf.header.gameOverWinner', { winner: gameState.winner || '?' })
    : phaseText;
  const roomIdText = gameState.room_id ?? roomId;

  return (
    <header
      className={`ww-game-status-header ${collapsed ? 'is-collapsed' : 'is-expanded'} ${judgePulse ? 'is-pulse' : ''}`}
      data-testid="ww-game-status-header"
      role="region"
      aria-label={t('werewolf.gameStatusHeader.title')}
    >
      {/* 标题行 — 始终可见,28px */}
      <div className="ww-game-status-header__row ww-game-status-header__row--title">
        <span className="ww-game-status-header__title">
          🐺 {t('werewolf.title')}
        </span>
        <span className="ww-game-status-header__sub">
          {spectator ? `👁 ${t('werewolf.gameStatusHeader.spectator')}` : `🎮 ${t('werewolf.gameStatusHeader.playing')}`}
          {' · '}
          <span className="ww-game-status-header__roomid" title={roomIdText}>
            房间 {roomIdText}
          </span>
          {' · '}
          <span className={`ww-game-status-header__phase ${isOver ? 'is-over' : ''}`}>
            {winnerText}
          </span>
        </span>
        <div className="ww-game-status-header__actions">
          {hasClock && (
            <span
              className={`ww-game-status-header__chip ww-game-status-header__chip--clock ${isOver ? 'is-ended' : ''}`}
              data-testid="ww-room-clock"
            >
              ⏱ {clockLabel}
            </span>
          )}
          {/* §20260817-03 U3 — 每小时 Token 消耗参考值:紧跟运行时长 chip,
              给玩家"继续玩下去每小时烧多少 Token"的心理预期。 */}
          {showTokenRate && (
            <span
              className="ww-game-status-header__chip ww-game-status-header__chip--tokenrate"
              data-testid="ww-token-rate"
              title={t('werewolf.statusBar.tokensPerHourTip', {
                total: formatK(totalApiTokens),
                hours: (elapsedSec / 3600).toFixed(2),
              })}
            >
              {t('werewolf.statusBar.tokensPerHour', { rate: formatK(tokenRatePerHour) })}
            </span>
          )}
          <button
            type="button"
            className="ww-header-btn ww-header-btn--history"
            onClick={onOpenHistory}
            data-testid="ww-history-button"
            aria-label={t('werewolf.history.headerButton')}
            title={t('werewolf.history.headerButton')}
          >
            📜 {t('werewolf.history.headerButton')}
          </button>
          <button
            type="button"
            className="ww-game-status-header__fold"
            onClick={() => setCollapsed((v) => !v)}
            data-testid="ww-status-header-fold"
            aria-label={collapsed ? t('werewolf.gameStatusHeader.expand') : t('werewolf.gameStatusHeader.fold')}
            title={collapsed ? t('werewolf.gameStatusHeader.expand') : t('werewolf.gameStatusHeader.fold')}
          >
            {collapsed ? '▼' : '▲'}
          </button>
        </div>
      </div>

      {/* 主信息行 — 折叠时隐藏(只露标题行,28px);展开时显示(再 28px,共 ≤ 60px)
       * §20260816-02 U2 — 信息分两组:
       *   group-game  = 游戏状态(阶段/法官/真人混合/响应中)
       *   group-stats = 实时统计(API/Token)
       *   蓝色 vs 绿色色相区分,1280 视口下自动 stack 双行。 */}
      {!collapsed && (
        <div className="ww-game-status-header__row ww-game-status-header__row--main">
          <div className="ww-game-status-header__group ww-game-status-header__group--game">
            {isOver && (
              <span
                className="ww-game-status-header__chip ww-game-status-header__chip--gameover"
                data-testid="ww-status-gameover"
              >
                {t('werewolf.statusBar.gameOverReview')}
              </span>
            )}
            {!isOver && typeof remaining === 'number' && (
              <span
                className={`ww-game-status-header__chip ww-game-status-header__chip--deadline ${isOverdue ? 'is-overdue' : ''}`}
                data-testid="ww-status-deadline"
                title={t('werewolf.history.clock.running')}
              >
                {isOverdue
                  ? t('werewolf.statusBar.clockOverdue')
                  : t('werewolf.statusBar.clockRemaining', { seconds: remaining })}
              </span>
            )}
            {!isOver && stats.active > 0 && (
              <span className="ww-game-status-header__chip ww-game-status-header__chip--thinking">
                {t('werewolf.statusBar.agentsResponding', { count: stats.active })}
                {stats.calling > 0 && (
                  <span className="ww-game-status-header__sub">
                    {t('werewolf.statusBar.subCalls', { count: stats.calling })}
                  </span>
                )}
                {stats.streaming > 0 && (
                  <span className="ww-game-status-header__sub">
                    {t('werewolf.statusBar.subStreaming', { count: stats.streaming })}
                  </span>
                )}
                {stats.retrying > 0 && (
                  <span className="ww-game-status-header__sub">
                    {t('werewolf.statusBar.subRetrying', { count: stats.retrying })}
                  </span>
                )}
              </span>
            )}
            {!isOver && stats.quarantined > 0 && (
              <span className="ww-game-status-header__chip ww-game-status-header__chip--quarantined">
                {t('werewolf.statusBar.agentsAutoPlay', { count: stats.quarantined })}
              </span>
            )}
            {isHumanInRoom && (
              <span className="ww-game-status-header__chip ww-game-status-header__chip--human">
                {t('werewolf.statusBar.mixedRoom')}
              </span>
            )}
            {judgeEnabled && judgeModel && (
              <span
                className={`ww-game-status-header__chip ww-game-status-header__chip--judge ${judgeQuarantined ? 'is-quarantined' : ''}`}
                title={judgeAnnouncement || judgeModel}
              >
                🎙 {judgeModel}
                {judgeTool && (
                  <span className="ww-game-status-header__sub">⚙️ {judgeTool}</span>
                )}
                {judgePending && judgePending !== '' && (
                  <span className="ww-game-status-header__badge ww-game-status-header__badge--pending">
                    {t('werewolf.judge.pending')}
                  </span>
                )}
                {judgeQuarantined && (
                  <span className="ww-game-status-header__badge ww-game-status-header__badge--quarantined">
                    {t('werewolf.judge.quarantined')}
                  </span>
                )}
                {judgeSummaryReady && (
                  <span className="ww-game-status-header__badge ww-game-status-header__badge--summary">
                    {t('werewolf.judge.summaryReady')}
                  </span>
                )}
              </span>
            )}
          </div>
          <div className="ww-game-status-header__group ww-game-status-header__group--stats">
            {hasAgentStats && (
              <span className="ww-game-status-header__chip ww-game-status-header__chip--stats">
                {t('werewolf.statusBar.apiStats', {
                  callCount: agentStats!.api_call_count,
                  successCount: agentStats!.api_success_count,
                  failCount: agentStats!.api_fail_count,
                })}
              </span>
            )}
            {hasAgentStats && (
              <span className="ww-game-status-header__chip ww-game-status-header__chip--tokens">
                {t('werewolf.statusBar.tokenStats', {
                  input: formatK(agentStats!.total_input_tokens),
                  output: formatK(agentStats!.total_output_tokens),
                  total: formatK(agentStats!.total_api_tokens),
                })}
              </span>
            )}
            {judgeStats && judgeStats.judge_api_call_count > 0 && (
              <span className="ww-game-status-header__chip ww-game-status-header__chip--judge-stats">
                {t('werewolf.statusBar.judgeTokenStats', {
                  callCount: judgeStats.judge_api_call_count,
                  successCount: judgeStats.judge_api_success_count,
                  failCount: judgeStats.judge_api_fail_count,
                  input: formatK(judgeStats.judge_total_input_tokens),
                  output: formatK(judgeStats.judge_total_output_tokens),
                  total: formatK(judgeStats.judge_total_api_tokens),
                })}
              </span>
            )}
          </div>
        </div>
      )}

      {/* quarantine 全局红字广播 — 折叠态也保留(顶到折叠之上 1 行) */}
      {stats.lastBroadcast && (
        <div className="ww-game-status-header__broadcast" data-testid="ww-status-broadcast">
          {stats.lastBroadcast}
        </div>
      )}
    </header>
  );
};

export default GameStatusHeader;