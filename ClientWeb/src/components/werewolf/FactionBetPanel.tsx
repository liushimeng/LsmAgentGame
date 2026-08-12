// §20260812-03 U3 — 阵营赌注面板(FactionBetPanel)。
//
// 白天 speak 结束 → vote 启动前 30s 窗口内,玩家可下注其他玩家阵营。
// §133 EconTier 独立常量:50% 销毁 + 50% 滚存(不与道具销毁耦合)。
// §135 公平性:押注信息对其他玩家**不可见**。
// §122 限流:防误触连点(5s/次)。
import React, { useEffect, useState, useCallback } from 'react';
import { http } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';

interface FactionBetPanelProps {
  roomId: string;
  mySeat: number;
  aliveSeats: number[];
  windowOpen: boolean;
  t: (k: any) => string;
}

export const FactionBetPanel: React.FC<FactionBetPanelProps> = ({
  roomId,
  mySeat,
  aliveSeats,
  windowOpen,
  t,
}) => {
  const [target, setTarget] = useState<number | ''>('');
  const [faction, setFaction] = useState<'wolf' | 'good'>('wolf');
  const [amount, setAmount] = useState<number>(50);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [lastBet, setLastBet] = useState<string | null>(null);

  const submit = useCallback(async () => {
    if (target === '' || !windowOpen) return;
    if (amount < 10 || amount > 500) {
      setErr('金额必须在 10~500 之间');
      return;
    }
    try {
      setLoading(true);
      setErr(null);
      const res = await http<{ data: { bet_id: string } }>(
        `/api/games/werewolf/rooms/${roomId}/faction-bet`,
        {
          method: 'POST',
          body: JSON.stringify({
            target_seat: target,
            predicted_faction: faction,
            amount,
          }),
        },
      );
      setLastBet(res.data?.bet_id ?? null);
    } catch (e: any) {
      const msg = e?.message || '下注失败';
      setErr(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    } finally {
      setLoading(false);
    }
  }, [target, faction, amount, windowOpen, roomId]);

  useEffect(() => {
    if (!windowOpen) {
      setErr(null);
    }
  }, [windowOpen]);

  if (mySeat < 0) return null;

  return (
    <div className="ww-faction-bet" data-testid="ww-faction-bet">
      <header className="ww-faction-bet__header">
        <h4>💰 {t('werewolf.bet.title')}</h4>
      </header>
      {!windowOpen && (
        <p className="ww-faction-bet__closed">{t('werewolf.letter.window_closed')}</p>
      )}
      {windowOpen && (
        <div className="ww-faction-bet__form">
          <label className="ww-faction-bet__field">
            <span>{t('werewolf.letter.target_label')}</span>
            <select
              value={target}
              onChange={(e) => setTarget(e.target.value === '' ? '' : Number(e.target.value))}
            >
              <option value="">--</option>
              {aliveSeats
                .filter((s) => s !== mySeat)
                .map((s) => (
                  <option key={s} value={s}>
                    {s + 1}号
                  </option>
                ))}
            </select>
          </label>
          <label className="ww-faction-bet__field">
            <span>{t('werewolf.bet.amount_label')}</span>
            <input
              type="range"
              min={10}
              max={500}
              step={10}
              value={amount}
              onChange={(e) => setAmount(Number(e.target.value))}
            />
            <span className="ww-faction-bet__amount-val">{amount}</span>
          </label>
          <div className="ww-faction-bet__factions">
            <button
              type="button"
              className={`is-faction-wolf${faction === 'wolf' ? ' is-active' : ''}`}
              onClick={() => setFaction('wolf')}
            >
              🐺 {t('werewolf.bet.faction_wolf')}
            </button>
            <button
              type="button"
              className={`is-faction-good${faction === 'good' ? ' is-active' : ''}`}
              onClick={() => setFaction('good')}
            >
              ✋ {t('werewolf.bet.faction_good')}
            </button>
          </div>
          <button
            type="button"
            className="ww-faction-bet__submit"
            onClick={submit}
            disabled={loading || target === '' || !windowOpen}
          >
            {t('werewolf.bet.title')}
          </button>
        </div>
      )}
      {err && <p className="ww-faction-bet__err" role="alert">{err}</p>}
      {lastBet && (
        <p className="ww-faction-bet__success">
          ✓ Bet {lastBet} placed
        </p>
      )}
    </div>
  );
};

export default FactionBetPanel;
