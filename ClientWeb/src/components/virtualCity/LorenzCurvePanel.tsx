/**
 * LorenzCurvePanel — 洛伦兹曲线 + 基尼系数 + 我的位置（2026-09-19 §P2 v2）
 *
 * 契约: lag_docs/虚拟城市/已实现/07-P2财富可视化/.../§13.3。
 * 数据源: game.state.society.lorenz_points / gini / 我的净资产(net_worth from my)。
 * 渲染: 纯 SVG 折线(13 点折线 + 完美平等对角线 + 我的位置点)。
 *
 * - 对比度(CLAUDE.md §26): 正文 ≥4.5:1;数值/徽章 ≥5:1。
 * - 暗色主题;空态走 virtualCity-panel__empty。
 * - 观战者视角(my_seat = -1): 不渲染我的位置点。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny } from '@/types/virtualCity';
import type { VirtualCitySociety, VirtualCityMyState } from '@/types/virtualCity';

interface Props {
  society: VirtualCitySociety | null | undefined;
  my: VirtualCityMyState | null | undefined;
}

const W = 320;
const H = 160;
const PAD_X = 24;
const PAD_Y = 16;

export function LorenzCurvePanel({ society, my }: Props) {
  const t = useT();

  if (!society || !society.lorenz_points || society.lorenz_points.length < 2) {
    return (
      <div className="virtualCity-lorenz">
        <div className="virtualCity-lorenz__title">
          <span>{t('virtualCity.dashboard.lorenz.title' as TKey)}</span>
        </div>
        <div className="virtualCity-lorenz__empty">{t('virtualCity.dashboard.lorenz.empty' as TKey)}</div>
      </div>
    );
  }

  const pts = society.lorenz_points; // [(popFrac, virtualCityFrac)]
  // 坐标转换: (0..1) → (PAD_X..W-PAD_X, H-PAD_Y..PAD_Y)
  const sx = (v: number) => PAD_X + v * (W - PAD_X * 2);
  const sy = (v: number) => H - PAD_Y - v * (H - PAD_Y * 2);

  // 折线路径
  const pathD = pts
    .map((p, i) => `${i === 0 ? 'M' : 'L'} ${sx(p[0]).toFixed(2)} ${sy(p[1]).toFixed(2)}`)
    .join(' ');

  // 完全平等对角线 (0,0) → (1,1)
  const equalX1 = sx(0);
  const equalY1 = sy(0);
  const equalX2 = sx(1);
  const equalY2 = sy(1);

  // 我的排名:按 net_worth 在所有 alive 玩家中的位置 — 此处无 alive[] 直接数据,
  // 仅显示 my.net_worth 配合 society.median_wealth / mean_wealth 估算相对位置。
  // 精确排名需 alive 列表(留 v3)。当前显示「我的净资产 vs 中位数/均值」即可。
  const myNet = my?.net_worth ?? 0;
  const median = society.median_wealth ?? 0;
  const mean = society.mean_wealth ?? 0;
  const rankDesc: 'top' | 'upper' | 'below' | null =
    myNet > 0 && median > 0
      ? myNet >= mean
        ? myNet >= median * 2
          ? 'top'
          : 'upper'
        : 'below'
      : null;

  return (
    <div className="virtualCity-lorenz">
      <div className="virtualCity-lorenz__title">
        <span>{t('virtualCity.dashboard.lorenz.title' as TKey)}</span>
        <span className="virtualCity-lorenz__gini">
          {t('virtualCity.dashboard.lorenz.giniBadge' as TKey, {
            value: society.gini.toFixed(3),
          })}
        </span>
      </div>
      {/* 18/04 AB-3：与桑基图同款收缩链 —— 外层可横向滚动，SVG width:100%/height:auto。 */}
      <div className="virtualCity-sankey-scroll">
      <svg
        className="virtualCity-lorenz__svg"
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="xMidYMid meet"
        role="img"
        aria-label={t('virtualCity.dashboard.lorenz.title' as TKey)}
      >
        {/* 坐标轴 */}
        <line
          x1={PAD_X}
          y1={H - PAD_Y}
          x2={W - PAD_X}
          y2={H - PAD_Y}
          stroke="#475569"
          strokeWidth={1}
        />
        <line
          x1={PAD_X}
          y1={PAD_Y}
          x2={PAD_X}
          y2={H - PAD_Y}
          stroke="#475569"
          strokeWidth={1}
        />
        {/* 完全平等对角线 */}
        <line
          x1={equalX1}
          y1={equalY1}
          x2={equalX2}
          y2={equalY2}
          stroke="#94a3b8"
          strokeWidth={1.2}
          strokeDasharray="4 3"
        />
        {/* 实际洛伦兹曲线 */}
        <path d={pathD} fill="none" stroke="#a78bfa" strokeWidth={2} />
        {/* 我的位置点(简化:按净值/总财富映射到 LorenzPoints 上) */}
        {myNet > 0 && society.total_wealth && society.total_wealth > 0 && rankDesc && (
          <MyPositionDot
            sx={sx}
            sy={sy}
            myFrac={Math.min(1, myNet / society.total_wealth)}
            label={t('virtualCity.dashboard.lorenz.myPos' as TKey)}
          />
        )}
      </svg>
      </div>
      <div className="virtualCity-lorenz__legend">
        <span>
          <i className="virtualCity-lorenz__legend-dot virtualCity-lorenz__legend-dot--equal" />
          {t('virtualCity.dashboard.lorenz.equalLine' as TKey)}
        </span>
        <span>
          <i className="virtualCity-lorenz__legend-dot virtualCity-lorenz__legend-dot--curve" />
          {t('virtualCity.dashboard.lorenz.lorenzCurve' as TKey)}
        </span>
        {myNet > 0 && (
          <span style={{ color: '#f472b6' }}>
            {t('virtualCity.dashboard.lorenz.myPos' as TKey)}: ¥{formatCny(myNet)}
          </span>
        )}
      </div>
    </div>
  );
}

function MyPositionDot({
  sx,
  sy,
  myFrac,
  label,
}: {
  sx: (v: number) => number;
  sy: (v: number) => number;
  myFrac: number;
  label: string;
}) {
  // 用 myFrac 在对角线上的交点作为可视化位置(简化)
  const x = sx(myFrac);
  const y = sy(myFrac);
  return (
    <g>
      <circle cx={x} cy={y} r={5} className="virtualCity-lorenz__me" />
      <text x={x + 8} y={y - 6} className="virtualCity-lorenz__me-label">
        {label}
      </text>
    </g>
  );
}