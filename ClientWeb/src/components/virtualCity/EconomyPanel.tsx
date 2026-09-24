/**
 * EconomyPanel — 真实经济循环引擎仪表盘（P1 第二期，tab `economy`）。
 *
 * 契约：lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-真实经济循环引擎-v1.md §8。
 * 数据源 game.state.consumer_market / labor_market / society（economy_enabled=false
 * 时后端零值/缺失下发）。
 *
 * 展示分四区：
 *   ① CPI 同比 / 环比徽章
 *   ② 八大类价格环比发散横条图（涨红跌绿 A 股习惯；图标 goodsIcon(id)，PNG 缺失
 *      回落 emoji；方向 + 带符号标签双通道，色盲不依赖颜色单独判读）
 *   ③ 劳动力市场：失业率仪表（自然率 5% 阈值线）+ 就业率 + 工资年增长 +
 *      企业营收 + 裁员潮徽章（状态 = 颜色 + 文字标签，永不单靠颜色）
 *   ④ 社会结构：基尼系数仪表 + 收入五等份单色条 + 三圈层堆叠条（直接标注人数）
 *
 * 空态兜底照 MinskyPanel.tsx 模式（缺字段渲染 virtualCity-panel__empty）。
 * 对比度（CLAUDE.md §26）：正文/数值 ≥4.5:1（--virtualCity-text 系 token），徽章 ≥5:1。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { goodsIcon } from '@/assets/images/virtualCity';
import {
  formatCny,
  formatPct,
  virtualCityGoodsMeta,
  type VirtualCityGameState,
  type VirtualCityGoodsItem,
} from '@/types/virtualCity';
import { LorenzCurvePanel } from './LorenzCurvePanel';
import { VirtualCityPyramidPanel } from './VirtualCityPyramidPanel';
import { FundFlowSankeyPanel } from './FundFlowSankeyPanel';

interface Props {
  gameState: VirtualCityGameState | null;
}

/** 带符号百分比（+0.8% / −0.4%；0 → ±0.0%）。 */
function fmtSignedPct(v: number, digits = 1): string {
  const s = formatPct(Math.abs(v), digits);
  if (v > 0) return `+${s}`;
  if (v < 0) return `−${s}`;
  return s;
}

/** 失业率状态色（暗色面板底 ≥3:1 的标记色；文本仍走文本 token）。
 *  阈值：≥12% 裁员潮红 / ≥10% 橙 / ≥6% 琥珀 / <6% 绿（引擎 LayoffWave 同源语义）。 */
function unemploymentColor(rate: number): string {
  if (rate >= 0.12) return '#f87171';
  if (rate >= 0.10) return '#fb923c';
  if (rate >= 0.06) return '#fbbf24';
  return '#4ade80';
}

/** 八大类单行：图标 + 名称 + 环比发散条（中心线为 0，右涨左跌）。 */
function GoodsRow({ item, scale }: { item: VirtualCityGoodsItem; scale: number }) {
  const t = useT();
  const meta = virtualCityGoodsMeta(item.id);
  const icon = goodsIcon(item.id);
  const up = item.mom_change > 0;
  const down = item.mom_change < 0;
  // 发散条宽度：|mom| / 全场最大 |mom| × 50%（半轨），0 时不渲染填充。
  const halfPct = scale > 0 ? Math.min(50, (Math.abs(item.mom_change) / scale) * 50) : 0;
  const dirClass = up ? 'virtualCity-economy__goods-fill--up' : 'virtualCity-economy__goods-fill--down';
  const numClass = up
    ? 'virtualCity-num--up virtualCity-economy__goods-mom'
    : down
      ? 'virtualCity-num--down virtualCity-economy__goods-mom'
      : 'virtualCity-economy__goods-mom';
  return (
    <div
      className="virtualCity-economy__goods-row"
      title={`${meta?.nameZh ?? item.id} · ${t('virtualCity.economy.weight' as TKey)} ${(item.weight * 100).toFixed(0)}% · ${t('virtualCity.economy.priceIdx' as TKey)} ${item.price_idx.toFixed(1)}`}
    >
      <span className="virtualCity-economy__goods-icon" aria-hidden="true">
        {icon ? <img src={icon} alt="" /> : (meta?.emoji ?? '🛍')}
      </span>
      <span className="virtualCity-economy__goods-name">
        {t(`virtualCity.goods.${item.id}` as TKey)}
      </span>
      <span className="virtualCity-economy__goods-bar" role="img" aria-label={`${item.id} ${fmtSignedPct(item.mom_change)}`}>
        <span className="virtualCity-economy__goods-center" />
        {(up || down) && (
          <span
            className={`virtualCity-economy__goods-fill ${dirClass}`}
            style={up ? { left: '50%', width: `${halfPct}%` } : { right: '50%', width: `${halfPct}%` }}
          />
        )}
      </span>
      <span className={numClass}>{fmtSignedPct(item.mom_change)}</span>
    </div>
  );
}

/** 通用水平仪表（label + value + track + fill + 可选阈值线）；fill 色由调用方给。 */
function Meter({
  label,
  valueText,
  pct,
  fill,
  thresholdPct,
  thresholdTitle,
}: {
  label: string;
  valueText: string;
  /** 填充比例 0-100。 */
  pct: number;
  fill: string;
  /** 阈值线位置 0-100（可选）。 */
  thresholdPct?: number;
  thresholdTitle?: string;
}) {
  return (
    <div className="virtualCity-economy__meter">
      <div className="virtualCity-economy__meter-head">
        <span className="virtualCity-economy__meter-label">{label}</span>
        <span className="virtualCity-economy__meter-value">{valueText}</span>
      </div>
      <div className="virtualCity-economy__meter-track">
        <div
          className="virtualCity-economy__meter-fill"
          style={{ width: `${Math.max(0, Math.min(100, pct))}%`, background: fill }}
        />
        {thresholdPct !== undefined && (
          <span
            className="virtualCity-economy__meter-threshold"
            style={{ left: `${Math.max(0, Math.min(100, thresholdPct))}%` }}
            title={thresholdTitle}
          />
        )}
      </div>
    </div>
  );
}

export function EconomyPanel({ gameState }: Props) {
  const t = useT();
  if (!gameState) {
    return (
      <div className="virtualCity-panel__empty">
        <p>{t('virtualCity.panel.waiting' as TKey)}</p>
      </div>
    );
  }

  const cm = gameState.consumer_market;
  const lm = gameState.labor_market;
  const soc = gameState.society;

  // 空态兜底（照 MinskyPanel：旧房间 / economy_enabled=false 时后端零值下发）。
  const hasGoods = !!cm && Array.isArray(cm.goods) && cm.goods.length > 0 && cm.goods.some((g) => g.price_idx !== 0);
  const hasLabor = !!lm && (lm.firm_revenue_cny !== 0 || lm.unemployment_rate !== 0);
  const hasSociety = !!soc && Array.isArray(soc.quintiles) && soc.quintiles.length > 0;
  if (!hasGoods && !hasLabor && !hasSociety) {
    return (
      <div className="virtualCity-economy">
        <div className="virtualCity-economy__head">
          <span className="virtualCity-economy__title">{t('virtualCity.economy.title' as TKey)}</span>
        </div>
        <div className="virtualCity-panel__empty">
          <p>{t('virtualCity.economy.unavailable' as TKey)}</p>
        </div>
      </div>
    );
  }

  // 八大类发散条全场标定尺度（最大 |环比|）；不足时用静态表兜底顺序。
  const goods = hasGoods ? cm!.goods : [];
  const goodsScale = Math.max(...goods.map((g) => Math.abs(g.mom_change)), 0.0001);

  const unemployment = lm?.unemployment_rate ?? 0;
  const employment = lm?.employment_ratio ?? 1 - unemployment;
  const wave = Math.max(0, Math.min(3, lm?.layoff_wave ?? 0));
  const waveClass = wave >= 2 ? 'virtualCity-economy__wave--hot' : wave >= 1 ? 'virtualCity-economy__wave--watch' : 'virtualCity-economy__wave--calm';

  const circles = soc?.circles;
  const circleTotal = circles
    ? Math.max(1, circles.survival + circles.accumulate + circles.freedom)
    : 1;

  return (
    <div className="virtualCity-economy">
      <div className="virtualCity-economy__head">
        <span className="virtualCity-economy__title">{t('virtualCity.economy.title' as TKey)}</span>
        <div className="virtualCity-economy__badges">
          {cm && (
            <>
              <span className="virtualCity-badge">{t('virtualCity.economy.cpiYoy' as TKey)} {formatPct(cm.cpi_yoy)}</span>
              <span
                className={
                  cm.cpi_mom > 0
                    ? 'virtualCity-badge virtualCity-economy__badge--up'
                    : cm.cpi_mom < 0
                      ? 'virtualCity-badge virtualCity-economy__badge--down'
                      : 'virtualCity-badge'
                }
              >
                {t('virtualCity.economy.cpiMom' as TKey)} {fmtSignedPct(cm.cpi_mom)}
              </span>
            </>
          )}
        </div>
      </div>

      {/* ① 八大类价格环比（涨红跌绿；顺序 = 后端 §2.1 权重表序） */}
      {hasGoods && (
        <div className="virtualCity-economy__section">
          <div className="virtualCity-economy__section-title">{t('virtualCity.economy.goodsTitle' as TKey)}</div>
          {goods.map((g) => (
            <GoodsRow key={g.id} item={g} scale={goodsScale} />
          ))}
          {/* 图例：涨/跌色对（CVD 地板带 → 方向 + 符号双通道兜底） */}
          <div className="virtualCity-economy__legend">
            <span className="virtualCity-economy__legend-item">
              <i className="virtualCity-economy__legend-dot virtualCity-economy__legend-dot--up" />
              {t('virtualCity.economy.up' as TKey)}
            </span>
            <span className="virtualCity-economy__legend-item">
              <i className="virtualCity-economy__legend-dot virtualCity-economy__legend-dot--down" />
              {t('virtualCity.economy.down' as TKey)}
            </span>
            <span className="virtualCity-economy__legend-note">{t('virtualCity.economy.legendNote' as TKey)}</span>
          </div>
        </div>
      )}

      {/* ② 劳动力市场（零值结构 = economy_enabled=false，不渲染） */}
      {hasLabor && lm && (
        <div className="virtualCity-economy__section">
          <div className="virtualCity-economy__section-title">
            {t('virtualCity.economy.laborTitle' as TKey)}
            <span className={`virtualCity-badge virtualCity-economy__wave ${waveClass}`}>
              {t('virtualCity.economy.layoff' as TKey)} · {t(`virtualCity.economy.layoff.${wave}` as TKey)}
            </span>
          </div>
          <Meter
            label={t('virtualCity.economy.unemployment' as TKey)}
            valueText={formatPct(unemployment)}
            pct={(unemployment / 0.35) * 100}
            fill={unemploymentColor(unemployment)}
            thresholdPct={(0.05 / 0.35) * 100}
            thresholdTitle={t('virtualCity.economy.naturalRate' as TKey)}
          />
          <div className="virtualCity-economy__stats">
            <div className="virtualCity-economy__stat">
              <span className="virtualCity-economy__stat-label">{t('virtualCity.economy.employment' as TKey)}</span>
              <span className="virtualCity-economy__stat-value">{formatPct(employment)}</span>
            </div>
            <div className="virtualCity-economy__stat">
              <span className="virtualCity-economy__stat-label">{t('virtualCity.economy.wageGrowth' as TKey)}</span>
              <span className={`virtualCity-economy__stat-value ${(lm.avg_wage_growth_yoy ?? 0) >= 0 ? 'virtualCity-num--pos' : 'virtualCity-num--neg'}`}>
                {fmtSignedPct(lm.avg_wage_growth_yoy)}
              </span>
            </div>
            <div className="virtualCity-economy__stat">
              <span className="virtualCity-economy__stat-label">{t('virtualCity.economy.firmRevenue' as TKey)}</span>
              <span className="virtualCity-economy__stat-value">¥{formatCny(lm.firm_revenue_cny)}</span>
            </div>
          </div>
        </div>
      )}

      {/* ③ 社会结构（零值结构 = economy_enabled=false，不渲染） */}
      {hasSociety && soc && (
        <div className="virtualCity-economy__section">
          <div className="virtualCity-economy__section-title">{t('virtualCity.economy.societyTitle' as TKey)}</div>
          <Meter
            label={t('virtualCity.economy.gini' as TKey)}
            valueText={soc.gini.toFixed(3)}
            pct={soc.gini * 100}
            fill="#a78bfa"
            thresholdPct={40}
            thresholdTitle={t('virtualCity.economy.giniWarn' as TKey)}
          />
          {soc.quintiles.length > 0 && (
            <div className="virtualCity-economy__quintiles">
              <div className="virtualCity-economy__quintiles-title">{t('virtualCity.economy.quintiles' as TKey)}</div>
              {soc.quintiles.map((q, i) => (
                <div key={i} className="virtualCity-economy__quintile-row">
                  <span className="virtualCity-economy__quintile-label">Q{i + 1}</span>
                  <span className="virtualCity-economy__quintile-track">
                    <span className="virtualCity-economy__quintile-fill" style={{ width: `${Math.max(0.5, q * 100)}%` }} />
                  </span>
                  <span className="virtualCity-economy__quintile-value">{formatPct(q, 0)}</span>
                </div>
              ))}
            </div>
          )}
          {circles && (
            <div className="virtualCity-economy__circles">
              <div className="virtualCity-economy__circles-title">{t('virtualCity.economy.circles' as TKey)}</div>
              {/* 堆叠条：分段间 2px 间隙；直接标注人数（不单靠颜色） */}
              <div className="virtualCity-economy__circles-stack">
                {circles.survival > 0 && (
                  <span
                    className="virtualCity-economy__circles-fill virtualCity-economy__circles-fill--survival"
                    style={{ width: `${(circles.survival / circleTotal) * 100}%` }}
                    title={`${t('virtualCity.economy.circle.survival' as TKey)} ${circles.survival}`}
                  />
                )}
                {circles.accumulate > 0 && (
                  <span
                    className="virtualCity-economy__circles-fill virtualCity-economy__circles-fill--accumulate"
                    style={{ width: `${(circles.accumulate / circleTotal) * 100}%` }}
                    title={`${t('virtualCity.economy.circle.accumulate' as TKey)} ${circles.accumulate}`}
                  />
                )}
                {circles.freedom > 0 && (
                  <span
                    className="virtualCity-economy__circles-fill virtualCity-economy__circles-fill--freedom"
                    style={{ width: `${(circles.freedom / circleTotal) * 100}%` }}
                    title={`${t('virtualCity.economy.circle.freedom' as TKey)} ${circles.freedom}`}
                  />
                )}
              </div>
              <div className="virtualCity-economy__circles-legend">
                <span className="virtualCity-economy__legend-item">
                  <i className="virtualCity-economy__legend-dot virtualCity-economy__circles-dot--survival" />
                  {t('virtualCity.economy.circle.survival' as TKey)} {circles.survival}
                </span>
                <span className="virtualCity-economy__legend-item">
                  <i className="virtualCity-economy__legend-dot virtualCity-economy__circles-dot--accumulate" />
                  {t('virtualCity.economy.circle.accumulate' as TKey)} {circles.accumulate}
                </span>
                <span className="virtualCity-economy__legend-item">
                  <i className="virtualCity-economy__legend-dot virtualCity-economy__circles-dot--freedom" />
                  {t('virtualCity.economy.circle.freedom' as TKey)} {circles.freedom}
                </span>
              </div>
            </div>
          )}
        </div>
      )}

      {/* ④ P2 v2 财富结构(2026-09-19 §P2-可视化):洛伦兹/金字塔/资金流向。
         * 折叠块:条件渲染;空字段时各组件内部走 virtualCity-panel__empty 兜底。
         * 顺序固定:Lorenz → Pyramid → Flow(由宏观 → 微观 → 闭环)。 */}
      {(soc?.lorenz_points || soc?.pyramid_layers || gameState?.flow_stat) && (
        <div className="virtualCity-dashboard">
          <div className="virtualCity-dashboard__title">
            {t('virtualCity.dashboard.structure' as TKey)}
          </div>
          {soc?.lorenz_points && <LorenzCurvePanel society={soc} my={gameState?.my ?? null} />}
          {soc?.pyramid_layers && <VirtualCityPyramidPanel layers={soc.pyramid_layers} />}
          {gameState?.flow_stat && <FundFlowSankeyPanel flowStat={gameState.flow_stat} />}
        </div>
      )}

      {/* 教育向说明（可折叠；恩格尔定律 + 经济循环链路） */}
      <details className="virtualCity-economy__edu">
        <summary>{t('virtualCity.economy.eduToggle' as TKey)}</summary>
        <div className="virtualCity-economy__edu-body">
          <p>{t('virtualCity.economy.eduP1' as TKey)}</p>
          <p>{t('virtualCity.economy.eduP2' as TKey)}</p>
          <p className="virtualCity-economy__edu-note">
            {t('virtualCity.economy.eduEnergy' as TKey, { up: 2, down: -1 })}
            {' · '}
            {t('virtualCity.economy.eduNote' as TKey)}
          </p>
        </div>
      </details>
    </div>
  );
}
