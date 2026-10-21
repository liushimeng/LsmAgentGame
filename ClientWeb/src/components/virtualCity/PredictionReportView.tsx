/**
 * PredictionReportView — 预测报告渲染（批次 68）。
 *
 * 契约：tmpPlan/虚拟城市-预测运行报告_20261021_113136.md §4（JSON 形状）/
 * §7（前端交付清单）。渲染章节严格按 §7.2：
 *   元数据条 / 首月直接冲击 chips / 峰值与恢复表 / 逐月轨迹简表（月 × 核心 4 指标，
 *   ≤36 行可滚动）/ 外推 3·6·12 月 / 边界与限制（R1 声明醒目）；
 *   resilience 仅 applicable:true 时渲染韧性画像。
 * 传输链（transmission）与分配结构（distribution）类型已在 api/virtualCity.ts
 * 对齐 §4，但不在 §7 交付清单内，本组件暂不渲染。
 *
 * 暗色对比度（CLAUDE.md §26）：正文 var(--virtualCity-text) ≥12:1；徽章
 * #111827 on #d4a017 ≈7.5:1（≥5:1）；已恢复 #34d399 / 未恢复 #f87171 on 深底
 * 均 ≥4.5:1；边界警示框琥珀边框 + 实色深底（禁 25% 透明浅字反模式）。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type {
  VirtualCityPredictionExtrapolation,
  VirtualCityPredictionReport,
} from '@/api/virtualCity';

/** 轨迹简表的核心 4 指标（§7.2「月×核心 4 指标」；完整指标在 values 全量字典里）。 */
const TRAJECTORY_CORE = ['goods_price_idx', 'unemployment', 'stock_index', 'satisfaction'] as const;

/** 外推视野列（§7.2「外推 3·6·12 月」）。 */
const HORIZONS = [3, 6, 12];

/** 指标名 → i18n 标签键（未知指标原样展示，不吞后端新指标）。 */
const METRIC_KEY: Record<string, TKey> = {
  goods_price_idx: 'virtualCity.prediction.metric.goodsPriceIdx' as TKey,
  cpi_ann: 'virtualCity.prediction.metric.cpiAnn' as TKey,
  unemployment: 'virtualCity.prediction.metric.unemployment' as TKey,
  stock_index: 'virtualCity.prediction.metric.stockIndex' as TKey,
  gold_price: 'virtualCity.prediction.metric.goldPrice' as TKey,
  house_idx: 'virtualCity.prediction.metric.houseIdx' as TKey,
  treasury_cash: 'virtualCity.prediction.metric.treasuryCash' as TKey,
  avg_resident_cash: 'virtualCity.prediction.metric.avgResidentCash' as TKey,
  gini_wealth: 'virtualCity.prediction.metric.giniWealth' as TKey,
  gini_income: 'virtualCity.prediction.metric.giniIncome' as TKey,
  top10_pct: 'virtualCity.prediction.metric.top10Pct' as TKey,
  bottom50_pct: 'virtualCity.prediction.metric.bottom50Pct' as TKey,
  satisfaction: 'virtualCity.prediction.metric.satisfaction' as TKey,
  bubble_leverage: 'virtualCity.prediction.metric.bubbleLeverage' as TKey,
};

/** 数值展示：整数原样；≥1000 千分位整数；≥1 两位小数；小比率四位小数。 */
function fmtNum(v: number | null | undefined): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '-';
  if (Number.isInteger(v)) return v.toLocaleString('en-US');
  const abs = Math.abs(v);
  if (abs >= 1000) return Math.round(v).toLocaleString('en-US');
  if (abs >= 1) return v.toFixed(2);
  return v.toFixed(4);
}

/** 偏离百分比：带符号一位小数（24.0 → "+24.0%"）。 */
function fmtPct(v: number | null | undefined): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '-';
  const s = Math.abs(v).toFixed(1);
  return `${v > 0 ? '+' : v < 0 ? '-' : ''}${s}%`;
}

/** 生成时间戳（unix 毫秒）→ `YYYY-MM-DD HH:mm`。 */
function fmtWhen(raw: number | string | undefined): string {
  if (raw === undefined || raw === null || raw === '') return '';
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return String(raw);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

interface Props {
  report: VirtualCityPredictionReport;
}

export function PredictionReportView({ report }: Props) {
  const t = useT();

  const metricLabel = (metric: string): string => {
    const key = METRIC_KEY[metric];
    return key ? t(key) : metric;
  };

  const meta = report.meta;
  const tierLabel = meta.tier === 'R1'
    ? t('virtualCity.prediction.tierR1' as TKey)
    : meta.tier;

  // 外推：按指标聚合各视野点，便于一张表（行=指标，列=3/6/12）。
  const extrapByMetric = new Map<string, Map<number, VirtualCityPredictionExtrapolation>>();
  for (const e of report.extrapolation ?? []) {
    const byH = extrapByMetric.get(e.metric) ?? new Map<number, VirtualCityPredictionExtrapolation>();
    byH.set(e.horizon_months, e);
    extrapByMetric.set(e.metric, byH);
  }

  // 轨迹简表：核心指标在任一观测点出现才渲染该列。
  const trajectory = report.trajectory ?? [];
  const coreCols = TRAJECTORY_CORE.filter((m) =>
    trajectory.some((p) => p.values && p.values[m] !== undefined),
  );
  // 表头展示基线（平直基线 = T0 值，恒定）。
  const baselineOf = (metric: string): number | undefined => {
    for (const p of trajectory) {
      const b = p.baseline?.[metric];
      if (b !== undefined) return b;
    }
    return undefined;
  };

  return (
    <div className="virtualCity-prediction__report" data-testid="virtualCity-prediction-report">
      {/* ── 元数据条 ─────────────────────────────────────── */}
      <div className="virtualCity-prediction__meta" data-testid="virtualCity-prediction-meta">
        <span className="virtualCity-prediction__badge virtualCity-prediction__badge--tier">
          {tierLabel}
        </span>
        <span className="virtualCity-prediction__meta-item">
          <b>{meta.run_id}</b>
        </span>
        <span className="virtualCity-prediction__meta-item">
          {t('virtualCity.prediction.startMonth' as TKey)} {meta.start_month}
        </span>
        <span className="virtualCity-prediction__meta-item">
          {t('virtualCity.prediction.monthsObserved' as TKey)} {meta.months_observed}
        </span>
        <span className="virtualCity-prediction__meta-item">
          {t('virtualCity.prediction.seed' as TKey)} {meta.seed}
        </span>
        <span
          className="virtualCity-prediction__meta-item virtualCity-prediction__meta-item--dim"
          title={meta.baseline_snapshot_id}
        >
          {t('virtualCity.prediction.baseline' as TKey)} {meta.baseline_snapshot_id}
        </span>
        <span className="virtualCity-prediction__meta-item virtualCity-prediction__meta-item--dim">
          {t('virtualCity.prediction.generatedAt' as TKey)} {fmtWhen(meta.generated_at)}
        </span>
        {meta.injections && meta.injections.length > 0 && (
          <span className="virtualCity-prediction__meta-item virtualCity-prediction__meta-item--dim">
            {t('virtualCity.prediction.injections' as TKey)}
            {' '}
            {meta.injections.map((inj) => `${inj.id}/${inj.kind}`).join(', ')}
          </span>
        )}
      </div>

      {/* ── 首月直接冲击（chips）───────────────────────────── */}
      <section className="virtualCity-prediction__section">
        <div className="virtualCity-prediction__section-title">
          {t('virtualCity.prediction.firstImpact' as TKey)}
        </div>
        <p className="virtualCity-prediction__note">
          {t('virtualCity.prediction.firstImpactNote' as TKey)}
        </p>
        {(report.first_month_impact ?? []).length === 0 ? (
          <p className="virtualCity-prediction__note virtualCity-prediction__note--dim">
            {t('virtualCity.prediction.none' as TKey)}
          </p>
        ) : (
          <div className="virtualCity-prediction__chips">
            {(report.first_month_impact ?? []).map((imp) => (
              <span
                className={
                  'virtualCity-prediction__chip'
                  + (imp.delta_pct > 0
                    ? ' virtualCity-prediction__chip--up'
                    : imp.delta_pct < 0
                      ? ' virtualCity-prediction__chip--down'
                      : '')
                }
                key={imp.metric}
                title={`${metricLabel(imp.metric)}: ${fmtNum(imp.baseline)} → ${fmtNum(imp.value)} (${imp.text || ''})`}
                data-testid={`virtualCity-prediction-impact-${imp.metric}`}
              >
                <b>{metricLabel(imp.metric)}</b>
                <span className="virtualCity-prediction__chip-delta">{fmtPct(imp.delta_pct)}</span>
              </span>
            ))}
          </div>
        )}
      </section>

      {/* ── 峰值与恢复表 ─────────────────────────────────── */}
      <section className="virtualCity-prediction__section">
        <div className="virtualCity-prediction__section-title">
          {t('virtualCity.prediction.peakRecovery' as TKey)}
        </div>
        {(report.peak_recovery ?? []).length === 0 ? (
          <p className="virtualCity-prediction__note virtualCity-prediction__note--dim">
            {t('virtualCity.prediction.none' as TKey)}
          </p>
        ) : (
          <table className="virtualCity-prediction__table">
            <thead>
              <tr>
                <th>{t('virtualCity.prediction.metricCol' as TKey)}</th>
                <th>{t('virtualCity.prediction.peakDev' as TKey)}</th>
                <th>{t('virtualCity.prediction.troughDev' as TKey)}</th>
                <th>{t('virtualCity.prediction.recoveryMonth' as TKey)}</th>
                <th>{t('virtualCity.prediction.recoveryState' as TKey)}</th>
              </tr>
            </thead>
            <tbody>
              {(report.peak_recovery ?? []).map((pr) => (
                <tr key={pr.metric}>
                  <td>{metricLabel(pr.metric)}</td>
                  <td className="virtualCity-prediction__num">
                    {fmtPct(pr.peak_dev_pct)}
                    <span className="virtualCity-prediction__sub">
                      {' '}
                      {t('virtualCity.prediction.monthAt' as TKey, { m: pr.peak_month })}
                    </span>
                  </td>
                  <td className="virtualCity-prediction__num">
                    {fmtPct(pr.trough_dev_pct)}
                    <span className="virtualCity-prediction__sub">
                      {' '}
                      {t('virtualCity.prediction.monthAt' as TKey, { m: pr.trough_month })}
                    </span>
                  </td>
                  <td className="virtualCity-prediction__num">
                    {pr.recovery_month === null || pr.recovery_month === undefined
                      ? '—'
                      : t('virtualCity.prediction.monthAt' as TKey, { m: pr.recovery_month })}
                  </td>
                  <td>
                    <span
                      className={
                        'virtualCity-prediction__badge'
                        + (pr.recovered
                          ? ' virtualCity-prediction__badge--ok'
                          : ' virtualCity-prediction__badge--warn')
                      }
                    >
                      {pr.recovered
                        ? t('virtualCity.prediction.recovered' as TKey)
                        : t('virtualCity.prediction.unrecovered' as TKey)}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {/* ── 逐月轨迹简表（月 × 核心 4 指标，≤36 行可滚动）──── */}
      <section className="virtualCity-prediction__section">
        <div className="virtualCity-prediction__section-title">
          {t('virtualCity.prediction.trajectory' as TKey)}
        </div>
        <p className="virtualCity-prediction__note">
          {t('virtualCity.prediction.trajectoryNote' as TKey)}
        </p>
        {trajectory.length === 0 ? (
          <p className="virtualCity-prediction__note virtualCity-prediction__note--dim">
            {t('virtualCity.prediction.none' as TKey)}
          </p>
        ) : (
          <div className="virtualCity-prediction__scroll">
            <table className="virtualCity-prediction__table">
              <thead>
                <tr>
                  <th>{t('virtualCity.prediction.month' as TKey)}</th>
                  {coreCols.map((m) => {
                    const base = baselineOf(m);
                    return (
                      <th key={m}>
                        {metricLabel(m)}
                        {base !== undefined && (
                          <span className="virtualCity-prediction__sub">
                            {' '}
                            ({t('virtualCity.prediction.baselineShort' as TKey)} {fmtNum(base)})
                          </span>
                        )}
                      </th>
                    );
                  })}
                </tr>
              </thead>
              <tbody>
                {trajectory.map((p) => (
                  <tr key={p.month}>
                    <td className="virtualCity-prediction__num">{p.month}</td>
                    {coreCols.map((m) => (
                      <td className="virtualCity-prediction__num" key={m}>
                        {fmtNum(p.values?.[m])}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* ── 外推 3·6·12 月 ───────────────────────────────── */}
      <section className="virtualCity-prediction__section">
        <div className="virtualCity-prediction__section-title">
          {t('virtualCity.prediction.extrapolation' as TKey)}
        </div>
        <p className="virtualCity-prediction__note">
          {t('virtualCity.prediction.extrapolationNote' as TKey)}
        </p>
        {extrapByMetric.size === 0 ? (
          <p className="virtualCity-prediction__note virtualCity-prediction__note--dim">
            {t('virtualCity.prediction.none' as TKey)}
          </p>
        ) : (
          <table className="virtualCity-prediction__table">
            <thead>
              <tr>
                <th>{t('virtualCity.prediction.metricCol' as TKey)}</th>
                {HORIZONS.map((h) => (
                  <th key={h}>
                    {t('virtualCity.prediction.horizon' as TKey, { n: h })}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {[...extrapByMetric.entries()].map(([metric, byH]) => (
                <tr key={metric}>
                  <td>{metricLabel(metric)}</td>
                  {HORIZONS.map((h) => {
                    const e = byH.get(h);
                    return (
                      <td className="virtualCity-prediction__num" key={h}>
                        {e ? (
                          <>
                            {fmtNum(e.point)}
                            <span className="virtualCity-prediction__sub">
                              {' '}
                              [{fmtNum(e.lo)} ~ {fmtNum(e.hi)}]
                            </span>
                          </>
                        ) : (
                          '—'
                        )}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {/* ── 韧性画像（仅 applicable）───────────────────────── */}
      {report.resilience?.applicable && (
        <section className="virtualCity-prediction__section">
          <div className="virtualCity-prediction__section-title">
            {t('virtualCity.prediction.resilience' as TKey)}
          </div>
          <div className="virtualCity-prediction__scores">
            <span className="virtualCity-prediction__score">
              <i>{t('virtualCity.prediction.resilienceRecovery' as TKey)}</i>
              <b>{report.resilience.recovery_score}</b>
            </span>
            <span className="virtualCity-prediction__score">
              <i>{t('virtualCity.prediction.resilienceAdaptation' as TKey)}</i>
              <b>{report.resilience.adaptation_score}</b>
            </span>
            <span className="virtualCity-prediction__score">
              <i>{t('virtualCity.prediction.resilienceTransform' as TKey)}</i>
              <b>{report.resilience.transform_score}</b>
            </span>
          </div>
          {(report.resilience.basis ?? []).length > 0 && (
            <>
              <p className="virtualCity-prediction__note">
                {t('virtualCity.prediction.resilienceBasis' as TKey)}
              </p>
              <ul className="virtualCity-prediction__list">
                {(report.resilience.basis ?? []).map((b, i) => (
                  <li key={i}>{b}</li>
                ))}
              </ul>
            </>
          )}
        </section>
      )}

      {/* ── 边界与限制（R1 声明醒目）──────────────────────── */}
      <section
        className="virtualCity-prediction__section virtualCity-prediction__boundaries"
        data-testid="virtualCity-prediction-boundaries"
      >
        <div className="virtualCity-prediction__section-title">
          {t('virtualCity.prediction.boundaries' as TKey)}
        </div>
        <p className="virtualCity-prediction__note">
          {t('virtualCity.prediction.boundariesNote' as TKey)}
        </p>
        <ul className="virtualCity-prediction__list">
          {(report.boundaries ?? []).map((b, i) => (
            <li
              className={
                b.includes('R1')
                  ? 'virtualCity-prediction__boundary virtualCity-prediction__boundary--r1'
                  : 'virtualCity-prediction__boundary'
              }
              key={i}
            >
              {b}
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}

export default PredictionReportView;
