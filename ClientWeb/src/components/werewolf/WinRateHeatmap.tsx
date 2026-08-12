// §20260812-03 U1 — 阵营胜率热力图面板(仅观战者可见,§132 隐私隔离)。
//
// 数据源:game.state.win_rate_probability (number[13],下标 0..12 对应 1..13 号位)
// 由服务端 view.go BuildClientStateWithRoom 仅在 viewer<0 分支填充,玩家
// 永远拿不到此字段(omitempty,§130 接线单点保证)。
//
// 渲染:13 座位方格网格 + 6 阶色阶(蓝→青→黄→橙→红→深红)。
// 悬浮显示「座位 N · 狼人概率 X% · 当前存活/总存活 Y/Z」。
//
// §13 跨职责约束:本组件是纯渲染层,概率计算全在 ServerGo/game/werewolf/win_predict.go。
// §120 公平性:启发式公式不含 LLM 决策信号,纯客观行为数据。
// §135:概率值不含身份明文,仅数值。
import React, { useMemo } from 'react';
import type { WerewolfPlayerJSON } from '@/types/werewolf';

// 6 阶色阶(0~16% / 16~33% / 33~50% / 50~66% / 66~83% / 83~100%)
const HEAT_COLORS = [
  '#3b82f6', // 蓝: < 16%
  '#06b6d4', // 青: 16~33%
  '#eab308', // 黄: 33~50%
  '#f97316', // 橙: 50~66%
  '#ef4444', // 红: 66~83%
  '#991b1b', // 深红: ≥ 83%
];

function colorForProb(prob: number): string {
  if (prob < 0.16) return HEAT_COLORS[0];
  if (prob < 0.33) return HEAT_COLORS[1];
  if (prob < 0.50) return HEAT_COLORS[2];
  if (prob < 0.66) return HEAT_COLORS[3];
  if (prob < 0.83) return HEAT_COLORS[4];
  return HEAT_COLORS[5];
}

interface WinRateHeatmapPanelProps {
  probabilities: number[] | undefined;
  players: WerewolfPlayerJSON[];
  t: (k: any) => string;
}

export const WinRateHeatmapPanel: React.FC<WinRateHeatmapPanelProps> = ({
  probabilities,
  players,
  t,
}) => {
  const aliveCount = useMemo(
    () => players.filter((p) => p.alive).length,
    [players],
  );
  const totalSeated = useMemo(
    () => players.filter((p) => p.user_id !== '').length,
    [players],
  );

  if (!probabilities || probabilities.length !== 13) {
    return (
      <div className="ww-heatmap-empty" data-testid="ww-heatmap-empty">
        {t('werewolf.heatmap.empty')}
      </div>
    );
  }

  return (
    <div className="ww-heatmap" data-testid="ww-heatmap">
      <header className="ww-heatmap__header">
        <h4 className="ww-heatmap__title">{t('werewolf.heatmap.title')}</h4>
        <p className="ww-heatmap__subtitle">{t('werewolf.heatmap.subtitle')}</p>
      </header>
      <div className="ww-heatmap__grid" role="grid">
        {probabilities.map((prob, seat) => {
          const player = players[seat];
          const isAlive = player?.alive ?? false;
          const isSeated = player?.user_id !== '';
          const pct = Math.round(prob * 100);
          const color = colorForProb(prob);
          return (
            <div
              key={seat}
              role="gridcell"
              className={`ww-heatmap__cell${isAlive ? '' : ' is-dead'}${
                isSeated ? '' : ' is-empty'
              }`}
              style={{ background: color, opacity: isSeated ? 1 : 0.3 }}
              title={`${seat + 1}号 · 狼人概率 ${pct}% · 存活 ${aliveCount}/${totalSeated}`}
              data-seat={seat}
              data-prob={prob.toFixed(2)}
            >
              <span className="ww-heatmap__seat-num">{seat + 1}</span>
              <span className="ww-heatmap__pct">{pct}%</span>
              {!isAlive && isSeated && (
                <span className="ww-heatmap__dead-tag">💀</span>
              )}
            </div>
          );
        })}
      </div>
      <footer className="ww-heatmap__legend">
        {[0, 1, 2, 3, 4, 5].map((idx) => (
          <span key={idx} className="ww-heatmap__legend-item">
            <span
              className="ww-heatmap__legend-swatch"
              style={{ background: HEAT_COLORS[idx] }}
            />
            <span className="ww-heatmap__legend-label">
              {idx === 0 && '< 16%'}
              {idx === 1 && '16-33%'}
              {idx === 2 && '33-50%'}
              {idx === 3 && '50-66%'}
              {idx === 4 && '66-83%'}
              {idx === 5 && '≥ 83%'}
            </span>
          </span>
        ))}
      </footer>
    </div>
  );
};

export default WinRateHeatmapPanel;
