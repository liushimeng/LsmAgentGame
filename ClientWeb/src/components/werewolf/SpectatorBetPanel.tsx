// SpectatorBetPanel — 观众押注竞猜面板 (§20260812-02 U3)
//
// 仅观战者可见。PhaseVote 阶段 30 秒窗口内,观众可选择一个座位押注 10~100 金币。
// 押注信息对玩家不可见(§119 协议层隔离)。
//
// §136:本组件放 components/werewolf/(狼人杀私有),因为仅狼人杀有此功能。

import { useCallback, useState } from 'react';
import { reportGlobalError } from '@/services/globalError';

interface Props {
  roomId: string;
  phase: string;
  seatCount: number;
  playerNames: Record<number, string>; // seat → display name
  myBet?: { target_seat: number; amount: number; settled: boolean; result: string; payout: number };
  onPlaceBet: (targetSeat: number, amount: number) => Promise<void>;
}

const AMOUNTS = [10, 25, 50, 100];

export function SpectatorBetPanel({ phase, seatCount, myBet, onPlaceBet }: Props) {
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

  // 已结算:显示结果
  if (myBet?.settled) {
    return (
      <div className="spectator-bet-panel spectator-bet-panel--settled" data-testid="spectator-bet-settled">
        <div className="spectator-bet-panel__title">🎯 押注结果</div>
        <div className="spectator-bet-panel__result">
          <span>押注 {(myBet.target_seat + 1)} 号被放逐 · {myBet.amount} 金币</span>
          {myBet.result === 'win' ? (
            <span className="spectator-bet-panel__win">🎉 押中! +{myBet.payout} 金币</span>
          ) : myBet.result === 'refund' ? (
            <span className="spectator-bet-panel__refund">↩️ 平票退款 {myBet.amount} 金币</span>
          ) : (
            <span className="spectator-bet-panel__lose">💨 未押中</span>
          )}
        </div>
      </div>
    );
  }

  // 已押注但未结算
  if (myBet) {
    return (
      <div className="spectator-bet-panel" data-testid="spectator-bet-placed">
        <div className="spectator-bet-panel__title">🎯 已押注</div>
        <div className="spectator-bet-panel__placed">
          {(myBet.target_seat + 1)} 号被放逐 · {myBet.amount} 金币 · 等待结果…
        </div>
      </div>
    );
  }

  // 不在投票阶段
  if (!isVotePhase) {
    return (
      <div className="spectator-bet-panel spectator-bet-panel--disabled" data-testid="spectator-bet-disabled">
        <div className="spectator-bet-panel__title">🎯 观众押注</div>
        <div className="spectator-bet-panel__hint">投票阶段开始后可押注</div>
      </div>
    );
  }

  // 可押注
  return (
    <div className="spectator-bet-panel" data-testid="spectator-bet-panel">
      <div className="spectator-bet-panel__title">🎯 观众押注 · 猜谁会被放逐</div>

      {/* 选择目标座位 */}
      <div className="spectator-bet-panel__seats">
        {Array.from({ length: seatCount }, (_, i) => (
          <button
            key={i}
            type="button"
            className={`spectator-bet-panel__seat${targetSeat === i ? ' is-selected' : ''}`}
            onClick={() => setTargetSeat(i)}
            disabled={submitting}
          >
            {i + 1}号
          </button>
        ))}
      </div>

      {/* 选择金额 */}
      <div className="spectator-bet-panel__amounts">
        {AMOUNTS.map((a) => (
          <button
            key={a}
            type="button"
            className={`spectator-bet-panel__amount${amount === a ? ' is-selected' : ''}`}
            onClick={() => setAmount(a)}
            disabled={submitting}
          >
            💰{a}
          </button>
        ))}
      </div>

      {/* 确认押注 */}
      <button
        type="button"
        className="btn btn-primary spectator-bet-panel__confirm"
        onClick={handleBet}
        disabled={targetSeat < 0 || submitting}
        data-testid="spectator-bet-confirm"
      >
        {submitting ? '押注中…' : `押注 ${amount} 金币 → ${(targetSeat >= 0 ? targetSeat + 1 : '?')} 号`}
      </button>
    </div>
  );
}
