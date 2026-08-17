// SpectatorCompactBar — 观战者紧凑底栏(解说席 + 观众押注合并)
//
// §20260812-02 v2: 将 CommentaryPanel + SpectatorBetPanel 合并为单一紧凑组件。
// 共享 header 行(左标题 + 右标题),内容区水平并排。
// 空态时高度压缩到单行,消除大面积留白。
//
// §20260817-03 U1: AI 解说未开启时(commentaryEnabled=false)隐藏解说席:
//   - 解说区与押注区都不需要时整个底栏返回 null,grid auto 行塌缩为 0,
//     中栏 1fr 行吸收释放的垂直空间,座位网格获得更多显示高度;
//   - 仅投票/押注交互仍保留时,押注区独占整行。
//
// §136: 本组件放 components/werewolf/(狼人杀私有),仅观战者路由渲染。

import { useCallback, useState } from 'react';
import { useWerewolfStore } from '@/store/werewolf.store';
import { useT } from '@/hooks/useT';
import { reportGlobalError } from '@/services/globalError';

interface Props {
  roomId: string;
  phase: string;
  seatCount: number;
  playerNames: Record<number, string>;
  myBet?: { target_seat: number; amount: number; settled: boolean; result: string; payout: number };
  onPlaceBet: (targetSeat: number, amount: number) => Promise<void>;
  /**
   * §20260817-03 U1 — 房间是否开启了 AI 解说(game.state.commentary_enabled)。
   * false 时隐藏解说席(未投票/未押注则整个底栏不渲染)。
   */
  commentaryEnabled: boolean;
}

const AMOUNTS = [10, 25, 50, 100];

export function SpectatorCompactBar({ phase, seatCount, myBet, onPlaceBet, commentaryEnabled }: Props) {
  const t = useT();
  const feed = useWerewolfStore((s) => s.commentaryFeed);
  const [targetSeat, setTargetSeat] = useState<number>(-1);
  const [amount, setAmount] = useState<number>(50);
  const [submitting, setSubmitting] = useState(false);

  const isVotePhase = phase === 'vote';

  const handleBet = useCallback(async () => {
    if (targetSeat < 0 || submitting) return;
    setSubmitting(true);
    try {
      await onPlaceBet(targetSeat, amount);
    } catch (e: any) {
      reportGlobalError({ message: e?.message ?? '押注失败', severity: 'error' });
    } finally {
      setSubmitting(false);
    }
  }, [targetSeat, amount, submitting, onPlaceBet]);

  // 右侧标题文案
  const betHint = myBet?.settled
    ? (myBet.result === 'win' ? `🎉 +${myBet.payout}` : myBet.result === 'refund' ? '↩️ 退款' : '💨 未中')
    : myBet
      ? '⏳ 等待结果'
      : isVotePhase
        ? '🎯 观众押注 · 猜谁会被放逐'
        : '投票阶段开始后可押注';

  // §20260817-03 U1 — 两个子区的可见性:
  //   解说区 = 房间开启了 AI 解说;押注区 = 投票阶段或已押注。
  //   两者都不可见 → 整个底栏返回 null(grid auto 行塌缩,空间让给座位网格)。
  const showCommentary = commentaryEnabled;
  const showBet = isVotePhase || !!myBet;
  if (!showCommentary && !showBet) {
    return null;
  }

  return (
    <div className="spectator-compact-bar" data-testid="spectator-compact-bar">
      {/* 共享 header: 左右分列(各区按需渲染) */}
      <div className="spectator-compact-bar__header">
        {showCommentary && (
          <span className="spectator-compact-bar__title-left">
            {t('werewolf.commentary.title')}
          </span>
        )}
        {showBet && (
          <span className="spectator-compact-bar__title-right">
            🎯 {betHint}
          </span>
        )}
      </div>

      {/* 内容区: 解说 + 押注水平并排(各区按需渲染) */}
      <div className="spectator-compact-bar__body">
        {/* 左侧: 解说 feed(最近 3 条) — §20260817-03 U1: 未开启解说时不渲染 */}
        {showCommentary && (
          <div className="spectator-compact-bar__commentary">
            {feed.length === 0 ? (
              <span className="spectator-compact-bar__empty">
                {t('werewolf.commentary.empty')}
              </span>
            ) : (
              feed.slice(-3).map((line) => (
                <div
                  key={line.seq}
                  className={`spectator-compact-bar__line spectator-compact-bar__line--${line.style}`}
                >
                  <span className="spectator-compact-bar__style-tag">
                    {line.style === 'pro'
                      ? t('werewolf.commentary.stylePro')
                      : t('werewolf.commentary.styleFun')}
                  </span>
                  <span className="spectator-compact-bar__text">{line.text}</span>
                </div>
              ))
            )}
          </div>
        )}

        {/* 右侧: 押注交互(仅投票阶段展开,或已押注/已结算时显示结果) */}
        {showBet && (
          <div className="spectator-compact-bar__bet">
            {myBet?.settled ? (
              <span className="spectator-compact-bar__bet-result">
                {(myBet.target_seat + 1)}号 · {myBet.amount}💰
                {myBet.result === 'win' ? (
                  <span className="spectator-compact-bar__win">🎉 +{myBet.payout}</span>
                ) : myBet.result === 'refund' ? (
                  <span className="spectator-compact-bar__refund">↩️</span>
                ) : (
                  <span className="spectator-compact-bar__lose">💨</span>
                )}
              </span>
            ) : myBet ? (
              <span className="spectator-compact-bar__bet-placed">
                {(myBet.target_seat + 1)}号 · {myBet.amount}💰 · ⏳
              </span>
            ) : (
              <>
                {/* 座位选择 */}
                <div className="spectator-compact-bar__seats">
                  {Array.from({ length: seatCount }, (_, i) => (
                    <button
                      key={i}
                      type="button"
                      className={`spectator-compact-bar__seat${targetSeat === i ? ' is-selected' : ''}`}
                      onClick={() => setTargetSeat(i)}
                      disabled={submitting}
                    >
                      {i + 1}
                    </button>
                  ))}
                </div>
                {/* 金额选择 */}
                <div className="spectator-compact-bar__amounts">
                  {AMOUNTS.map((a) => (
                    <button
                      key={a}
                      type="button"
                      className={`spectator-compact-bar__amount${amount === a ? ' is-selected' : ''}`}
                      onClick={() => setAmount(a)}
                      disabled={submitting}
                    >
                      {a}
                    </button>
                  ))}
                </div>
                {/* 确认 */}
                <button
                  type="button"
                  className="btn btn-primary spectator-compact-bar__confirm"
                  onClick={handleBet}
                  disabled={targetSeat < 0 || submitting}
                >
                  {submitting ? '…' : `${amount}💰→${targetSeat >= 0 ? targetSeat + 1 : '?'}`}
                </button>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
