// SpectatorCompactBar — 观战者紧凑底栏(解说席 + 观众押注合并)
//
// §20260812-02 v2: 将 CommentaryPanel + SpectatorBetPanel 合并为单一紧凑组件。
// 共享 header 行(左标题 + 右标题),内容区水平并排。
// 空态时高度压缩到单行,消除大面积留白。
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
}

const AMOUNTS = [10, 25, 50, 100];

export function SpectatorCompactBar({ phase, seatCount, myBet, onPlaceBet }: Props) {
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

  return (
    <div className="spectator-compact-bar" data-testid="spectator-compact-bar">
      {/* 共享 header: 左右分列 */}
      <div className="spectator-compact-bar__header">
        <span className="spectator-compact-bar__title-left">
          🎙️ {t('werewolf.commentary.title')}
        </span>
        <span className="spectator-compact-bar__title-right">
          🎯 {betHint}
        </span>
      </div>

      {/* 内容区: 解说 + 押注水平并排 */}
      <div className="spectator-compact-bar__body">
        {/* 左侧: 解说 feed(最近 3 条) */}
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

        {/* 右侧: 押注交互(仅投票阶段展开,或已押注/已结算时显示结果) */}
        {(isVotePhase || myBet) && (
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
