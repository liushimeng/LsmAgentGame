/**
 * GameOverModal — 终局「人生结算」：三维评分条（财务自由度 / 人生满意度 / 社会贡献）
 * + 结局徽章（6 结局）+ 净资产 SVG 折线（game.month 全帧推导）+ 人生报告 +
 * [查看完整 Ledger] / [返回大厅]。
 *
 * 数据源：game.over{scores[], report} + store.monthFrames（净资产曲线）。
 */

import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { AppModal } from '@/components/ui/AppModal';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  formatCny,
  type VirtualCityEndingId,
  type VirtualCityGameState,
  type VirtualCityMonthFrame,
  type VirtualCityOverFrame,
} from '@/types/virtualCity';

const ENDING_CLASS: Record<string, string> = {
  winner: 'virtualCity-ending--winner',
  affluent: 'virtualCity-ending--affluent',
  ordinary: 'virtualCity-ending--ordinary',
  indebted: 'virtualCity-ending--indebted',
  bankrupt: 'virtualCity-ending--bankrupt',
  lonely_rich: 'virtualCity-ending--lonely_rich',
  accident_death: 'virtualCity-ending--accident_death', // P1-4 意外身故（§5.3）
};

function endingKey(ending: string): TKey {
  const known: VirtualCityEndingId[] = [
    'winner', 'affluent', 'ordinary', 'indebted', 'bankrupt', 'lonely_rich', 'accident_death',
  ];
  return (known.includes(ending as VirtualCityEndingId)
    ? `virtualCity.ending.${ending}`
    : 'virtualCity.ending.ordinary') as TKey;
}

/** 三维评分条（0–100）。 */
function ScoreBar({ label, weight, score }: { label: string; weight: string; score: number }) {
  const pct = Math.max(0, Math.min(100, score));
  return (
    <div className="virtualCity-score-row">
      <span className="virtualCity-score-row__label">
        {label} <small>({weight})</small>
      </span>
      <span className="virtualCity-score-row__track">
        <span className="virtualCity-score-row__fill" style={{ width: `${pct}%` }} />
      </span>
      <span className="virtualCity-score-row__value">{Math.round(score)}</span>
    </div>
  );
}

/** 净资产曲线（25→60 岁；monthFrames 内该座位逐月 net_worth）。 */
function NetWorthCurve({ frames, seat }: { frames: VirtualCityMonthFrame[]; seat: number }) {
  const t = useT();
  const points = useMemo(() => {
    const rows = frames
      .map((f) => ({ month: f.month, nw: f.summaries.find((s) => s.seat === seat)?.net_worth }))
      .filter((x): x is { month: number; nw: number } => typeof x.nw === 'number');
    if (rows.length < 2) return null;
    const min = Math.min(...rows.map((r) => r.nw));
    const max = Math.max(...rows.map((r) => r.nw));
    const span = max - min || 1;
    const w = 260;
    const h = 64;
    const path = rows
      .map((r, i) => {
        const x = (i / (rows.length - 1)) * (w - 4) + 2;
        const y = h - 4 - ((r.nw - min) / span) * (h - 10);
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`;
      })
      .join(' ');
    return { path, min, max, w, h };
  }, [frames, seat]);

  if (!points) {
    return <p className="virtualCity-gameover__curve-empty">{t('virtualCity.panel.empty' as TKey)}</p>;
  }
  return (
    <svg
      className="virtualCity-gameover__curve"
      viewBox={`0 0 ${points.w} ${points.h}`}
      width="100%"
      height={points.h}
      role="img"
      aria-label={t('virtualCity.gameOver.netWorthCurve' as TKey)}
    >
      <path d={points.path} fill="none" stroke="#d4a017" strokeWidth="2" />
    </svg>
  );
}

interface Props {
  over: VirtualCityOverFrame;
  gameState: VirtualCityGameState | null;
  monthFrames: VirtualCityMonthFrame[];
  mySeat: number;
  onViewLedger: () => void;
}

export function GameOverModal({ over, gameState, monthFrames, mySeat, onViewLedger }: Props) {
  const t = useT();
  const nav = useNavigate();
  const mine = over.scores.find((s) => s.seat === mySeat) ?? over.scores[0];
  const me = gameState?.players.find((p) => p.seat === mine?.seat);

  return (
    <AppModal
      title={`🏁 ${t('virtualCity.gameOver.title')}`}
      icon="🏁"
      kind="success"
      maxWidth={640}
      dismissible
      onClose={() => nav('/virtual-city')}
      footer={
        <>
          <button type="button" className="btn btn-secondary" onClick={onViewLedger}>
            📜 {t('virtualCity.gameOver.viewLedger' as TKey)}
          </button>
          <button type="button" className="btn btn-primary" onClick={() => nav('/virtual-city')}>
            {t('virtualCity.gameOver.backToLobby' as TKey)}
          </button>
        </>
      }
    >
      {/* 18/04 AB-2：挂 virtualCity-modal__body 标记 —— 收缩链（min-height:0）与
          86vh 上限由 virtualCity-city3d.css 按此标记对 .app-modal 外壳统一补齐。 */}
      <div className="virtualCity-gameover virtualCity-modal__body">
        {mine && (
          <div className="virtualCity-gameover__head">
            <div className="virtualCity-gameover__total">
              <span className="virtualCity-gameover__total-label">{t('virtualCity.gameOver.total' as TKey)}</span>
              <b>{Math.round(mine.total)}</b>
              <small>/100</small>
            </div>
            <span className={`virtualCity-ending-badge ${ENDING_CLASS[mine.ending] ?? ''}`}>
              🏆 {t(endingKey(mine.ending))}
            </span>
            <span className="virtualCity-gameover__seat">
              {t('virtualCity.gameOver.seat' as TKey, { n: mine.seat + 1 })}
              {me ? ` · ${me.nickname}` : ''}
            </span>
          </div>
        )}

        {mine && (
          <div className="virtualCity-gameover__scores">
            <ScoreBar label={t('virtualCity.gameOver.fiScore' as TKey)} weight="50%" score={mine.fi_score} />
            <ScoreBar label={t('virtualCity.gameOver.lifeScore' as TKey)} weight="30%" score={mine.life_score} />
            <ScoreBar label={t('virtualCity.gameOver.socialScore' as TKey)} weight="20%" score={mine.social_score} />
          </div>
        )}

        <div className="virtualCity-gameover__curve-block">
          <div className="virtualCity-gameover__curve-title">
            📈 {t('virtualCity.gameOver.netWorthCurve' as TKey)}
          </div>
          {mine && <NetWorthCurve frames={monthFrames} seat={mine.seat} />}
        </div>

        <details className="virtualCity-gameover__report" open>
          <summary>📜 {t('virtualCity.gameOver.report' as TKey)}</summary>
          <pre className="virtualCity-gameover__report-text">{over.reports[mine?.seat ?? mySeat] ?? ''}</pre>
        </details>

        <details className="virtualCity-gameover__board">
          <summary>{t('virtualCity.gameOver.board' as TKey)}</summary>
          <table className="virtualCity-table">
            <thead>
              <tr>
                <th>#</th>
                <th>{t('virtualCity.gameOver.total' as TKey)}</th>
                <th>FI</th>
                <th>{t('virtualCity.gameOver.lifeScore' as TKey)}</th>
                <th>{t('virtualCity.gameOver.socialScore' as TKey)}</th>
                <th>{t('virtualCity.netWorth' as TKey)}</th>
                <th>{t('virtualCity.gameOver.endingCol' as TKey)}</th>
              </tr>
            </thead>
            <tbody>
              {[...over.scores]
                .sort((a, b) => b.total - a.total)
                .map((s) => {
                  const p = gameState?.players.find((x) => x.seat === s.seat);
                  return (
                    <tr key={s.seat}>
                      <td>{s.seat + 1}{p ? ` ${p.nickname}` : ''}</td>
                      <td><b>{Math.round(s.total)}</b></td>
                      <td>{Math.round(s.fi_score)}</td>
                      <td>{Math.round(s.life_score)}</td>
                      <td>{Math.round(s.social_score)}</td>
                      <td>{formatCny(p?.net_worth)}</td>
                      <td>{t(endingKey(s.ending))}</td>
                    </tr>
                  );
                })}
            </tbody>
          </table>
        </details>
      </div>
    </AppModal>
  );
}
