/**
 * CityOverviewPanel — 批次 69 看板「城市总览」维度（新增）。
 *
 * 定位：把分散在 8 个业务面板里的关键 KPI **聚合到一屏**，作为看板首屏入口。
 * 数据源**全部**是既有的 `VirtualCityGameState` 字段 —— **不新增任何后端字段**。
 *
 * 六个 KPI 组：
 *   ① 时间与周期（month / age / phase / cycle / LPR / 央行政策）
 *   ② 民生与社会（society：基尼 / 圈层 / 总财富 / 中位数）
 *   ③ 消费与劳动力（consumer_market / labor_market）
 *   ④ 资产与金融（market 行情 + my 本人资产）
 *   ⑤ 宏观与风险（bubble / liquidity_spreads / minsky_overview / family / prediction）
 *   ⑥ 外部联动（批次 66 外盘情绪锚 market.exo_anchor）
 *
 * 口径纪律：
 *   - 数值一律走既有格式化工具（formatCny / formatPct，见 types/virtualCityFormat.ts），
 *     **不自己写格式化逻辑**；
 *   - 字段缺失 / 观战态无 `my` ⇒ 显示「—」，绝不渲染 undefined、绝不崩溃。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { VirtualCityGameState } from '@/types/virtualCity';
import { formatCny, formatPct } from '@/types/virtualCityFormat';

/** 单条 KPI（标签 : 数值）。 */
interface KpiRow {
  /** React key。 */
  key: string;
  /** 标签文案。 */
  label: string;
  /** 已格式化数值（缺失时统一为「—」）。 */
  value: string;
}

/** 一个 KPI 分组。 */
interface KpiGroup {
  key: string;
  title: string;
  rows: KpiRow[];
}

/** 数值 → 定点字符串；非有限数一律「—」。 */
function num(v: number | undefined | null, digits = 2): string {
  if (v === undefined || v === null || !Number.isFinite(v)) return '—';
  return v.toFixed(digits);
}

/** 人数（整数）。 */
function cnt(v: number | undefined | null): string {
  if (v === undefined || v === null || !Number.isFinite(v)) return '—';
  return String(Math.round(v));
}

export interface CityOverviewPanelProps {
  /** game.state 快照（null = 对局未就绪）。 */
  gameState: VirtualCityGameState | null;
}

export function CityOverviewPanel({ gameState }: CityOverviewPanelProps) {
  const t = useT();

  if (!gameState) {
    return (
      <div className="virtualCity-dash__overview" data-testid="virtualCity-dash-overview">
        <div className="virtualCity-panel__empty">
          <p>{t('virtualCity.panel.waiting' as TKey)}</p>
        </div>
      </div>
    );
  }

  const { cycle, market, central_bank, my } = gameState;
  const society = gameState.society ?? null;
  const cm = gameState.consumer_market ?? null;
  const lm = gameState.labor_market ?? null;
  const bubble = gameState.bubble ?? null;
  const spreads = gameState.liquidity_spreads ?? null;
  const minsky = gameState.minsky_overview ?? null;
  const family = gameState.family ?? null;
  const prediction = gameState.prediction ?? null;
  const exo = market.exo_anchor ?? null;

  // 房价指数 = 各区 price_index 均值（MarketPanel 同口径的聚合简化）。
  const houseIdx =
    market.districts.length > 0
      ? market.districts.reduce((s, d) => s + d.price_index, 0) / market.districts.length
      : undefined;

  // 泡沫状态 → 既有 i18n 键（与 MacroPanel 同一套文案）。
  const bubbleStateKey = bubble
    ? ({
        none: 'virtualCity.macro.bubbleNone',
        overheat: 'virtualCity.macro.bubbleOverheat',
        burst: 'virtualCity.macro.bubbleBurst',
        recession: 'virtualCity.macro.bubbleRecession',
      } as const)[bubble.state] ?? 'virtualCity.macro.bubbleNone'
    : null;

  // 外盘情绪锚方向（与 MarketPanel 的 ExoAnchorSection 同口径）。
  const exoSentimentKey = !exo || !exo.active
    ? 'virtualCity.rt.exo.inactive'
    : exo.sentiment > 0.02
      ? 'virtualCity.rt.exo.sentimentUp'
      : exo.sentiment < -0.02
        ? 'virtualCity.rt.exo.sentimentDown'
        : 'virtualCity.rt.exo.sentimentFlat';

  const groups: KpiGroup[] = [
    {
      key: 'time',
      title: t('virtualCity.dash.overview.time' as TKey),
      rows: [
        {
          key: 'month',
          label: t('virtualCity.month' as TKey, { m: gameState.month }),
          value: cnt(gameState.month),
        },
        {
          key: 'age',
          label: t('virtualCity.age' as TKey, { a: gameState.age }),
          value: cnt(gameState.age),
        },
        {
          key: 'phase',
          label: t('virtualCity.dash.overview.phase' as TKey),
          value: t(`virtualCity.phase.${gameState.phase}` as TKey),
        },
        {
          key: 'cycle',
          label: t('virtualCity.dash.overview.cycle' as TKey),
          value: t(`virtualCity.cycle.${cycle.phase}` as TKey),
        },
        {
          key: 'lpr',
          label: t('virtualCity.lpr' as TKey),
          value: formatPct(cycle.lpr),
        },
        {
          key: 'policyRate',
          label: t('virtualCity.cb.policyRate' as TKey),
          value: formatPct(central_bank?.policy_rate),
        },
        {
          key: 'creditTightness',
          label: t('virtualCity.cb.creditTightness' as TKey),
          value: formatPct(central_bank?.credit_tightness),
        },
      ],
    },
    {
      key: 'society',
      title: t('virtualCity.dash.overview.society' as TKey),
      rows: [
        {
          key: 'gini',
          label: t('virtualCity.economy.gini' as TKey),
          value: society ? society.gini.toFixed(3) : '—',
        },
        {
          key: 'survival',
          label: t('virtualCity.economy.circle.survival' as TKey),
          value: cnt(society?.circles?.survival),
        },
        {
          key: 'accumulate',
          label: t('virtualCity.economy.circle.accumulate' as TKey),
          value: cnt(society?.circles?.accumulate),
        },
        {
          key: 'freedom',
          label: t('virtualCity.economy.circle.freedom' as TKey),
          value: cnt(society?.circles?.freedom),
        },
        {
          key: 'totalWealth',
          label: t('virtualCity.dash.overview.totalWealth' as TKey),
          value: formatCny(society?.total_wealth),
        },
        {
          key: 'medianWealth',
          label: t('virtualCity.dash.overview.medianWealth' as TKey),
          value: formatCny(society?.median_wealth),
        },
      ],
    },
    {
      key: 'consume',
      title: t('virtualCity.dash.overview.consume' as TKey),
      rows: [
        {
          key: 'cpiYoy',
          label: t('virtualCity.economy.cpiYoy' as TKey),
          value: formatPct(cm?.cpi_yoy),
        },
        {
          key: 'cpiMom',
          label: t('virtualCity.economy.cpiMom' as TKey),
          value: formatPct(cm?.cpi_mom),
        },
        {
          key: 'unemployment',
          label: t('virtualCity.economy.unemployment' as TKey),
          value: formatPct(lm?.unemployment_rate),
        },
        {
          key: 'employment',
          label: t('virtualCity.economy.employment' as TKey),
          value: formatPct(lm?.employment_ratio),
        },
        {
          key: 'wageGrowth',
          label: t('virtualCity.economy.wageGrowth' as TKey),
          value: formatPct(lm?.avg_wage_growth_yoy),
        },
        {
          key: 'firmRevenue',
          label: t('virtualCity.economy.firmRevenue' as TKey),
          value: formatCny(lm?.firm_revenue_cny),
        },
      ],
    },
    {
      key: 'assets',
      title: t('virtualCity.dash.overview.assets' as TKey),
      rows: [
        {
          key: 'stock',
          label: t('virtualCity.stockIndex' as TKey),
          value: num(market.stock_index),
        },
        {
          key: 'gold',
          label: t('virtualCity.goldPrice' as TKey),
          value: formatCny(market.gold_price),
        },
        {
          key: 'bond',
          label: t('virtualCity.bondYield' as TKey),
          value: formatPct(market.bond_yield),
        },
        {
          key: 'house',
          label: t('virtualCity.housePrice' as TKey),
          value: num(houseIdx),
        },
        {
          key: 'cash',
          label: t('virtualCity.cash' as TKey),
          value: formatCny(my?.cash),
        },
        {
          key: 'netWorth',
          label: t('virtualCity.netWorth' as TKey),
          value: formatCny(my?.net_worth),
        },
        {
          key: 'fi',
          label: t('virtualCity.fiIndex' as TKey),
          value: num(my?.fi_index),
        },
      ],
    },
    {
      key: 'risk',
      title: t('virtualCity.dash.overview.risk' as TKey),
      rows: [
        {
          key: 'bubbleState',
          label: t('virtualCity.macro.bubbleState' as TKey),
          value: bubbleStateKey ? t(bubbleStateKey as TKey) : '—',
        },
        {
          key: 'leverage',
          label: t('virtualCity.macro.metricLeverage' as TKey),
          value: formatPct(bubble?.leverage_ratio),
        },
        {
          key: 'turnover',
          label: t('virtualCity.macro.metricTurnover' as TKey),
          value: formatPct(bubble?.turnover_ratio),
        },
        {
          key: 'cashRatio',
          label: t('virtualCity.macro.metricCash' as TKey),
          value: formatPct(bubble?.cash_ratio),
        },
        {
          key: 'spreadStock',
          label: t('virtualCity.macro.spreadStock' as TKey),
          value: formatPct(spreads?.stock),
        },
        {
          key: 'spreadHouse',
          label: t('virtualCity.macro.spreadHouse' as TKey),
          value: formatPct(spreads?.house),
        },
        {
          key: 'ponziRatio',
          label: t('minsky.ponziRatio' as TKey),
          value: formatPct(minsky?.ponzi_ratio),
        },
        {
          key: 'family',
          label: t('virtualCity.dash.overview.family' as TKey),
          value: family
            ? `${cnt(family.parents_alive_count)} / ${cnt(family.children_count)}`
            : '—',
        },
        {
          key: 'prediction',
          label: t('virtualCity.dash.overview.prediction' as TKey),
          value: prediction ? prediction.run_id : '—',
        },
      ],
    },
    {
      key: 'external',
      title: t('virtualCity.dash.overview.external' as TKey),
      rows: [
        {
          key: 'exoState',
          label: t('virtualCity.rt.exo.title' as TKey),
          value: t(exoSentimentKey as TKey),
        },
        {
          key: 'exoSentiment',
          label: t('virtualCity.dash.overview.exoSentiment' as TKey),
          value: exo ? num(exo.sentiment, 3) : '—',
        },
        {
          key: 'exoStockBias',
          label: t('virtualCity.rt.exo.stockBias' as TKey),
          value: exo ? num(exo.stock_bias, 4) : '—',
        },
        {
          key: 'exoGoldBias',
          label: t('virtualCity.rt.exo.goldBias' as TKey),
          value: exo ? num(exo.gold_bias, 4) : '—',
        },
      ],
    },
  ];

  return (
    <div className="virtualCity-dash__overview" data-testid="virtualCity-dash-overview">
      {groups.map((g) => (
        <div className="virtualCity-dash__ov-group" key={g.key}>
          <div className="virtualCity-dash__ov-group-title">{g.title}</div>
          <div className="virtualCity-dash__ov-grid">
            {g.rows.map((r) => (
              <div className="virtualCity-dash__ov-item" key={r.key}>
                <span className="virtualCity-dash__ov-label">{r.label}</span>
                <span className="virtualCity-dash__ov-value">{r.value}</span>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

export default CityOverviewPanel;
