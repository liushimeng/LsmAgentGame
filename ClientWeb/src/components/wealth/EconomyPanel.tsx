/**
 * EconomyPanel — 真实经济循环引擎仪表盘（P1 第二期，tab `economy`）。
 *
 * 契约：lag_docs/财商流游戏/已实现/05-P1扩展/财商流游戏-P1-真实经济循环引擎-v1.md §8。
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
 * 空态兜底照 MinskyPanel.tsx 模式（缺字段渲染 wealth-panel__empty）。
 * 对比度（CLAUDE.md §26）：正文/数值 ≥4.5:1（--wealth-text 系 token），徽章 ≥5:1。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { goodsIcon } from '@/assets/images/wealth';
import {
  formatCny,
  formatPct,
  wealthGoodsMeta,
  type WealthGameState,
  type WealthGoodsItem,
} from '@/types/wealth';

interface Props {
  gameState: WealthGameState | null;
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
function GoodsRow({ item, scale }: { item: WealthGoodsItem; scale: number }) {
  const t = useT();
  const meta = wealthGoodsMeta(item.id);
  const icon = goodsIcon(item.id);
  const up = item.mom_change > 0;
  const down = item.mom_change < 0;
  // 发散条宽度：|mom| / 全场最大 |mom| × 50%（半轨），0 时不渲染填充。
  const halfPct = scale > 0 ? Math.min(50, (Math.abs(item.mom_change) / scale) * 50) : 0;
  const dirClass = up ? 'wealth-economy__goods-fill--up' : 'wealth-economy__goods-fill--down';
  const numClass = up
    ? 'wealth-num--up wealth-economy__goods-mom'
    : down
      ? 'wealth-num--down wealth-economy__goods-mom'
      : 'wealth-economy__goods-mom';
  return (
    <div
      className="wealth-economy__goods-row"
      title={`${meta?.nameZh ?? item.id} · ${t('wealth.economy.weight' as TKey)} ${(item.weight * 100).toFixed(0)}% · ${t('wealth.economy.priceIdx' as TKey)} ${item.price_idx.toFixed(1)}`}
    >
      <span className="wealth-economy__goods-icon" aria-hidden="true">
        {icon ? <img src={icon} alt="" /> : (meta?.emoji ?? '🛍')}
      </span>
      <span className="wealth-economy__goods-name">
        {t(`wealth.goods.${item.id}` as TKey)}
      </span>
      <span className="wealth-economy__goods-bar" role="img" aria-label={`${item.id} ${fmtSignedPct(item.mom_change)}`}>
        <span className="wealth-economy__goods-center" />
        {(up || down) && (
          <span
            className={`wealth-economy__goods-fill ${dirClass}`}
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
    <div className="wealth-economy__meter">
      <div className="wealth-economy__meter-head">
        <span className="wealth-economy__meter-label">{label}</span>
        <span className="wealth-economy__meter-value">{valueText}</span>
      </div>
      <div className="wealth-economy__meter-track">
        <div
          className="wealth-economy__meter-fill"
          style={{ width: `${Math.max(0, Math.min(100, pct))}%`, background: fill }}
        />
        {thresholdPct !== undefined && (
          <span
            className="wealth-economy__meter-threshold"
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
      <div className="wealth-panel__empty">
        <p>{t('wealth.panel.waiting' as TKey)}</p>
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
      <div className="wealth-economy">
        <div className="wealth-economy__head">
          <span className="wealth-economy__title">{t('wealth.economy.title' as TKey)}</span>
        </div>
        <div className="wealth-panel__empty">
          <p>{t('wealth.economy.unavailable' as TKey)}</p>
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
  const waveClass = wave >= 2 ? 'wealth-economy__wave--hot' : wave >= 1 ? 'wealth-economy__wave--watch' : 'wealth-economy__wave--calm';

  const circles = soc?.circles;
  const circleTotal = circles
    ? Math.max(1, circles.survival + circles.accumulate + circles.freedom)
    : 1;

  return (
    <div className="wealth-economy">
      <div className="wealth-economy__head">
        <span className="wealth-economy__title">{t('wealth.economy.title' as TKey)}</span>
        <div className="wealth-economy__badges">
          {cm && (
            <>
              <span className="wealth-badge">{t('wealth.economy.cpiYoy' as TKey)} {formatPct(cm.cpi_yoy)}</span>
              <span
                className={
                  cm.cpi_mom > 0
                    ? 'wealth-badge wealth-economy__badge--up'
                    : cm.cpi_mom < 0
                      ? 'wealth-badge wealth-economy__badge--down'
                      : 'wealth-badge'
                }
              >
                {t('wealth.economy.cpiMom' as TKey)} {fmtSignedPct(cm.cpi_mom)}
              </span>
            </>
          )}
        </div>
      </div>

      {/* ① 八大类价格环比（涨红跌绿；顺序 = 后端 §2.1 权重表序） */}
      {hasGoods && (
        <div className="wealth-economy__section">
          <div className="wealth-economy__section-title">{t('wealth.economy.goodsTitle' as TKey)}</div>
          {goods.map((g) => (
            <GoodsRow key={g.id} item={g} scale={goodsScale} />
          ))}
          {/* 图例：涨/跌色对（CVD 地板带 → 方向 + 符号双通道兜底） */}
          <div className="wealth-economy__legend">
            <span className="wealth-economy__legend-item">
              <i className="wealth-economy__legend-dot wealth-economy__legend-dot--up" />
              {t('wealth.economy.up' as TKey)}
            </span>
            <span className="wealth-economy__legend-item">
              <i className="wealth-economy__legend-dot wealth-economy__legend-dot--down" />
              {t('wealth.economy.down' as TKey)}
            </span>
            <span className="wealth-economy__legend-note">{t('wealth.economy.legendNote' as TKey)}</span>
          </div>
        </div>
      )}

      {/* ② 劳动力市场（零值结构 = economy_enabled=false，不渲染） */}
      {hasLabor && lm && (
        <div className="wealth-economy__section">
          <div className="wealth-economy__section-title">
            {t('wealth.economy.laborTitle' as TKey)}
            <span className={`wealth-badge wealth-economy__wave ${waveClass}`}>
              {t('wealth.economy.layoff' as TKey)} · {t(`wealth.economy.layoff.${wave}` as TKey)}
            </span>
          </div>
          <Meter
            label={t('wealth.economy.unemployment' as TKey)}
            valueText={formatPct(unemployment)}
            pct={(unemployment / 0.35) * 100}
            fill={unemploymentColor(unemployment)}
            thresholdPct={(0.05 / 0.35) * 100}
            thresholdTitle={t('wealth.economy.naturalRate' as TKey)}
          />
          <div className="wealth-economy__stats">
            <div className="wealth-economy__stat">
              <span className="wealth-economy__stat-label">{t('wealth.economy.employment' as TKey)}</span>
              <span className="wealth-economy__stat-value">{formatPct(employment)}</span>
            </div>
            <div className="wealth-economy__stat">
              <span className="wealth-economy__stat-label">{t('wealth.economy.wageGrowth' as TKey)}</span>
              <span className={`wealth-economy__stat-value ${(lm.avg_wage_growth_yoy ?? 0) >= 0 ? 'wealth-num--pos' : 'wealth-num--neg'}`}>
                {fmtSignedPct(lm.avg_wage_growth_yoy)}
              </span>
            </div>
            <div className="wealth-economy__stat">
              <span className="wealth-economy__stat-label">{t('wealth.economy.firmRevenue' as TKey)}</span>
              <span className="wealth-economy__stat-value">¥{formatCny(lm.firm_revenue_cny)}</span>
            </div>
          </div>
        </div>
      )}

      {/* ③ 社会结构（零值结构 = economy_enabled=false，不渲染） */}
      {hasSociety && soc && (
        <div className="wealth-economy__section">
          <div className="wealth-economy__section-title">{t('wealth.economy.societyTitle' as TKey)}</div>
          <Meter
            label={t('wealth.economy.gini' as TKey)}
            valueText={soc.gini.toFixed(3)}
            pct={soc.gini * 100}
            fill="#a78bfa"
            thresholdPct={40}
            thresholdTitle={t('wealth.economy.giniWarn' as TKey)}
          />
          {soc.quintiles.length > 0 && (
            <div className="wealth-economy__quintiles">
              <div className="wealth-economy__quintiles-title">{t('wealth.economy.quintiles' as TKey)}</div>
              {soc.quintiles.map((q, i) => (
                <div key={i} className="wealth-economy__quintile-row">
                  <span className="wealth-economy__quintile-label">Q{i + 1}</span>
                  <span className="wealth-economy__quintile-track">
                    <span className="wealth-economy__quintile-fill" style={{ width: `${Math.max(0.5, q * 100)}%` }} />
                  </span>
                  <span className="wealth-economy__quintile-value">{formatPct(q, 0)}</span>
                </div>
              ))}
            </div>
          )}
          {circles && (
            <div className="wealth-economy__circles">
              <div className="wealth-economy__circles-title">{t('wealth.economy.circles' as TKey)}</div>
              {/* 堆叠条：分段间 2px 间隙；直接标注人数（不单靠颜色） */}
              <div className="wealth-economy__circles-stack">
                {circles.survival > 0 && (
                  <span
                    className="wealth-economy__circles-fill wealth-economy__circles-fill--survival"
                    style={{ width: `${(circles.survival / circleTotal) * 100}%` }}
                    title={`${t('wealth.economy.circle.survival' as TKey)} ${circles.survival}`}
                  />
                )}
                {circles.accumulate > 0 && (
                  <span
                    className="wealth-economy__circles-fill wealth-economy__circles-fill--accumulate"
                    style={{ width: `${(circles.accumulate / circleTotal) * 100}%` }}
                    title={`${t('wealth.economy.circle.accumulate' as TKey)} ${circles.accumulate}`}
                  />
                )}
                {circles.freedom > 0 && (
                  <span
                    className="wealth-economy__circles-fill wealth-economy__circles-fill--freedom"
                    style={{ width: `${(circles.freedom / circleTotal) * 100}%` }}
                    title={`${t('wealth.economy.circle.freedom' as TKey)} ${circles.freedom}`}
                  />
                )}
              </div>
              <div className="wealth-economy__circles-legend">
                <span className="wealth-economy__legend-item">
                  <i className="wealth-economy__legend-dot wealth-economy__circles-dot--survival" />
                  {t('wealth.economy.circle.survival' as TKey)} {circles.survival}
                </span>
                <span className="wealth-economy__legend-item">
                  <i className="wealth-economy__legend-dot wealth-economy__circles-dot--accumulate" />
                  {t('wealth.economy.circle.accumulate' as TKey)} {circles.accumulate}
                </span>
                <span className="wealth-economy__legend-item">
                  <i className="wealth-economy__legend-dot wealth-economy__circles-dot--freedom" />
                  {t('wealth.economy.circle.freedom' as TKey)} {circles.freedom}
                </span>
              </div>
            </div>
          )}
        </div>
      )}

      {/* 教育向说明（可折叠；恩格尔定律 + 经济循环链路） */}
      <details className="wealth-economy__edu">
        <summary>{t('wealth.economy.eduToggle' as TKey)}</summary>
        <div className="wealth-economy__edu-body">
          <p>{t('wealth.economy.eduP1' as TKey)}</p>
          <p>{t('wealth.economy.eduP2' as TKey)}</p>
          <p className="wealth-economy__edu-note">
            {t('wealth.economy.eduEnergy' as TKey, { up: 2, down: -1 })}
            {' · '}
            {t('wealth.economy.eduNote' as TKey)}
          </p>
        </div>
      </details>
    </div>
  );
}
