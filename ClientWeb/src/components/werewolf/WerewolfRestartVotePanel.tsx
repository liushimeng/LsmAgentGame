// WerewolfRestartVotePanel — 2026-07-10 新增的"重开局投票"面板。
//
// 当 game.state.phase === 'restart_vote' 时由 WerewolfGamePage 渲染:
//   - 上一局结果 (winner banner)
//   - 投票三列 (Yes / No / Abstain) 实时刷新
//   - 倒计时 (复用 PhaseClock 风格,5 分钟窗口)
//   - 玩家投票按钮 (3 个,未投票时高亮,已投票后灰)
//   - 决定后 banner: passed / rejected / timeout
//
// 数据来源: gameState.phase_extra.restart_vote(由 view.go::BuildClientState
// 填充);人类投票通过 ws.game.werewolf_restart_vote 帧;增量更新通过
// game.restart_vote_update 帧合并。
import React, { useEffect, useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { RestartVoteExtra, WerewolfGameState, WerewolfPlayerJSON } from '@/types/werewolf';

interface PanelProps {
  gameState: WerewolfGameState;
  onCast: (choice: 'yes' | 'no' | 'abstain') => void;
}

const fmtSeat = (seat: number): string => `${seat + 1}号`;

export const WerewolfRestartVotePanel: React.FC<PanelProps> = ({ gameState, onCast }) => {
  const t = useT();
  const rv: RestartVoteExtra | undefined = gameState.phase_extra?.restart_vote;

  // 本地 setInterval 渲染倒计时(每秒);deadline 来自后端下发
  const [remaining, setRemaining] = useState<number>(rv?.remaining_sec ?? 0);
  useEffect(() => {
    if (!rv?.deadline_at) {
      setRemaining(rv?.remaining_sec ?? 0);
      return;
    }
    const deadline = new Date(rv.deadline_at).getTime();
    const tick = () => {
      const diff = Math.max(0, Math.floor((deadline - Date.now()) / 1000));
      setRemaining(diff);
    };
    tick();
    const id = setInterval(tick, 500);
    return () => clearInterval(id);
  }, [rv?.deadline_at, rv?.remaining_sec]);

  const myChoice = rv?.my_choice;
  const mySeat = gameState.my_seat;
  const isSpectator = mySeat < 0 || mySeat >= gameState.players.length;

  // §20260823-02 P8 — 我已投票后收起为单行摘要(可再展开);仅本局内存态,
  // 不写 localStorage(投票是一次性动作,无跨局偏好)。
  const [stripExpanded, setStripExpanded] = useState(false);

  const decided = !!rv?.decided;
  const result = rv?.result;
  const winner = rv?.winner ?? '';

  const yesSeats = rv?.yes ?? [];
  const noSeats = rv?.no ?? [];
  const abstainSeats = rv?.abstain ?? [];

  // 玩家昵称快查;按 (user_id, seat) → account
  const playerLabel = useMemo(() => {
    const m = new Map<number, string>();
    gameState.players.forEach((p: WerewolfPlayerJSON) => {
      if (p.user_id) {
        const ac = p.user_id.length > 0 ? p.user_id.slice(0, 8) : `seat${p.seat}`;
        m.set(p.seat, ac);
      }
    });
    return (seat: number) => m.get(seat) ?? fmtSeat(seat);
  }, [gameState.players]);

  // 决定 banner 颜色
  const bannerCls = useMemo(() => {
    if (!decided) return 'restart-vote__banner--idle';
    if (result === 'passed') return 'restart-vote__banner--passed';
    if (result === 'rejected') return 'restart-vote__banner--rejected';
    if (result === 'timeout') return 'restart-vote__banner--timeout';
    return 'restart-vote__banner--idle';
  }, [decided, result]);

  const bannerText = decided
    ? result === 'passed'
      ? t('werewolf.restartVote.passed')
      : result === 'rejected'
      ? t('werewolf.restartVote.rejected')
      : result === 'timeout'
      ? t('werewolf.restartVote.timeout')
      : ''
    : t('werewolf.restartVote.subtitle');

  const winnerLabel = useMemo(() => {
    if (winner === 'wolf') return t('werewolf.restartVote.winner.wolf');
    if (winner === 'good') return t('werewolf.restartVote.winner.good');
    return winner;
  }, [winner, t]);

  // §20260823-02 P8 — 我已投票且尚未决定 → 主体(三列票数 + 投票按钮)收起为
  // 单行摘要条「✅ 已投 {choice} · 当前票数」,点击可再展开;决定后恢复全量。
  const collapsedStrip = !!myChoice && !decided && !isSpectator && !stripExpanded;
  const myChoiceLabel =
    myChoice === 'yes'
      ? t('werewolf.restartVote.yesBtn')
      : myChoice === 'no'
        ? t('werewolf.restartVote.noBtn')
        : t('werewolf.restartVote.abstainBtn');

  return (
    <div className="restart-vote" data-testid="werewolf-restart-vote">
      <div className={`restart-vote__banner ${bannerCls}`}>
        <h2 className="restart-vote__title">
          🏆 {t('werewolf.restartVote.title')} · {winnerLabel}
        </h2>
        <p className="restart-vote__subtitle">{bannerText}</p>
        {!decided && (
          <div className="restart-vote__countdown">
            {t('werewolf.restartVote.deadlineHint', { sec: remaining })}
            {' · '}
            {t('werewolf.restartVote.quorum', {
              cur: yesSeats.length,
              quota: rv?.yes_quota ?? 0,
            })}
          </div>
        )}
      </div>

      {collapsedStrip && (
        <div
          className="restart-vote__strip"
          role="button"
          tabIndex={0}
          aria-expanded={false}
          onClick={() => setStripExpanded(true)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              setStripExpanded(true);
            }
          }}
          data-testid="restart-vote-strip"
        >
          <span className="restart-vote__strip-text">
            {t('werewolf.panel.restartVoted', { choice: myChoiceLabel })}
            {' · '}
            {t('werewolf.restartVote.quorum', {
              cur: yesSeats.length,
              quota: rv?.yes_quota ?? 0,
            })}
          </span>
          <button
            type="button"
            className="restart-vote__strip-toggle"
            aria-label={t('werewolf.panel.expand')}
            title={t('werewolf.panel.expand')}
            onClick={(e) => {
              e.stopPropagation();
              setStripExpanded(true);
            }}
          >
            ▶
          </button>
        </div>
      )}

      {!collapsedStrip && !decided && !isSpectator && (
        <div className="restart-vote__actions">
          <button
            type="button"
            className={`restart-vote__btn restart-vote__btn--yes ${myChoice === 'yes' ? 'is-active' : ''}`}
            disabled={myChoice === 'yes'}
            onClick={() => onCast('yes')}
            data-testid="restart-vote-yes"
          >
            ✅ {t('werewolf.restartVote.yesBtn')}
          </button>
          <button
            type="button"
            className={`restart-vote__btn restart-vote__btn--no ${myChoice === 'no' ? 'is-active' : ''}`}
            disabled={myChoice === 'no'}
            onClick={() => onCast('no')}
            data-testid="restart-vote-no"
          >
            ❌ {t('werewolf.restartVote.noBtn')}
          </button>
          <button
            type="button"
            className={`restart-vote__btn restart-vote__btn--abstain ${myChoice === 'abstain' ? 'is-active' : ''}`}
            disabled={myChoice === 'abstain'}
            onClick={() => onCast('abstain')}
            data-testid="restart-vote-abstain"
          >
            ⏸ {t('werewolf.restartVote.abstainBtn')}
          </button>
          {myChoice && (
            <div className="restart-vote__voted">
              {t('werewolf.restartVote.voted', { choice: myChoice })}
            </div>
          )}
        </div>
      )}

      {isSpectator && !decided && !collapsedStrip && (
        <div className="restart-vote__spectator">
          {t('werewolf.restartVote.spectatorHint')}
        </div>
      )}

      {!collapsedStrip && (
      <div className="restart-vote__columns">
        <div className="restart-vote__col restart-vote__col--yes">
          <h3>{t('werewolf.restartVote.yesCol')}</h3>
          <ul>
            {yesSeats.length === 0 && <li className="is-empty">—</li>}
            {yesSeats.map((s) => (
              <li key={`y-${s}`}>{fmtSeat(s)} · {playerLabel(s)}</li>
            ))}
          </ul>
        </div>
        <div className="restart-vote__col restart-vote__col--no">
          <h3>{t('werewolf.restartVote.noCol')}</h3>
          <ul>
            {noSeats.length === 0 && <li className="is-empty">—</li>}
            {noSeats.map((s) => (
              <li key={`n-${s}`}>{fmtSeat(s)} · {playerLabel(s)}</li>
            ))}
          </ul>
        </div>
        <div className="restart-vote__col restart-vote__col--abstain">
          <h3>{t('werewolf.restartVote.abstainCol')}</h3>
          <ul>
            {abstainSeats.length === 0 && <li className="is-empty">—</li>}
            {abstainSeats.map((s) => (
              <li key={`a-${s}`}>{fmtSeat(s)} · {playerLabel(s)}</li>
            ))}
          </ul>
        </div>
      </div>
      )}

      <p className="restart-vote__spec">
        {t('werewolf.restartVote.spec')}
      </p>
    </div>
  );
};

export default WerewolfRestartVotePanel;