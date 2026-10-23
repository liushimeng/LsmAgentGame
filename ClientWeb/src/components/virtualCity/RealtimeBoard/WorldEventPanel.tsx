/**
 * WorldEventPanel — 批次 80 · 实时看板「事件流」子页。
 *
 * 数据源：**game.state.world_events**（服务端 `WorldEventsJSON` 逐字段下发）——
 *   ⚠ 不走 HTTP /api/virtual_city/realtime_board 快照，也**不进** WS game.realtime_feed 帧；
 *     它随 game.state 帧刷新 ⇒ 事件流子页是「对局内」视图（未开局 / 后端未上线时为空态）。
 *
 * 契约来源：lag_docs/虚拟城市/已实现/80-真实世界事件自动注入引擎/01-方案设计.md §3.6。
 *
 * 卡片结构（每条事件）：
 *   标题 + 强度徽章（severity 1..3）+ 方向徽章（bullish/bearish/neutral）
 *   + 明细（触发依据）+ 标签带（事件类型 / 作用域 / 规则 id / 触发量）
 *   + 注入状态徽章（已注入 / 未注入）+ 观测相对时间 + 来源名
 *   + 即时效果（effects 摘要，服务端中文 text 优先）+ 次月结果快照（outcome 6 指标）
 *   + 原文链接（url 非空时可点开，新窗口 + noopener）。
 *
 * 降级口径（§7.1 全部内联可见，不吞 console）：
 *   - events 缺省（后端未上线 / 未开局）⇒ 提示条 + 空态；
 *   - auto_enabled=false ⇒ 「自动注入已关闭」提示条；
 *   - items 为空 ⇒ 空态（「未触发阈值或数据通道离线」）。
 *
 * §26 对比度：徽章文字 ≥5:1、正文 ≥4.5:1、**禁** opacity 表禁用态（用实色灰底）。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny, formatPct } from '@/types/virtualCity';
import type {
  WorldEventOutcome,
  WorldEventRecord,
  WorldEventsSnapshot,
} from '@/types/virtualCityWorldEvent';
import { pctText, timeAgoParts } from './boardUtils';

/** 事件类型 → i18n 键（未知 kind 回退后端原串）。 */
const KIND_KEYS: Record<string, string> = {
  equity_crash: 'virtualCity.rt.event.kind.equity_crash',
  gold_spike: 'virtualCity.rt.event.kind.gold_spike',
  crypto_crash: 'virtualCity.rt.event.kind.crypto_crash',
  commodity_inflation: 'virtualCity.rt.event.kind.commodity_inflation',
  policy_easing: 'virtualCity.rt.event.kind.policy_easing',
  policy_tightening: 'virtualCity.rt.event.kind.policy_tightening',
  earthquake: 'virtualCity.rt.event.kind.earthquake',
  tsunami: 'virtualCity.rt.event.kind.tsunami',
  extreme_disaster: 'virtualCity.rt.event.kind.extreme_disaster',
  survey_pulse: 'virtualCity.rt.event.kind.survey_pulse',
};

/** 作用域 → i18n 键（未知 scope 回退后端原串）。 */
const SCOPE_KEYS: Record<string, string> = {
  market: 'virtualCity.rt.event.scope.market',
  policy: 'virtualCity.rt.event.scope.policy',
  disaster: 'virtualCity.rt.event.scope.disaster',
  macro: 'virtualCity.rt.event.scope.macro',
  society: 'virtualCity.rt.event.scope.society',
};

/** 相对时间分档 → i18n 键（事件流专用「{n} 分钟前」句式，不复用含「更新」后缀的看板键）。 */
const AGO_KEYS: Record<string, string> = {
  just: 'virtualCity.rt.event.ago.just',
  min: 'virtualCity.rt.event.ago.min',
  hour: 'virtualCity.rt.event.ago.hour',
  day: 'virtualCity.rt.event.ago.day',
};

/** 方向 → i18n 键（未知方向按中性口径，绝不把 key 原文显示给用户）。 */
const DIRECTION_KEYS: Record<string, string> = {
  bullish: 'virtualCity.rt.event.bullish',
  bearish: 'virtualCity.rt.event.bearish',
  neutral: 'virtualCity.rt.event.neutral',
};

/** 翻译函数签名（useT 返回值）。 */
type TFunc = (key: TKey, vars?: Record<string, string | number>) => string;

/** 强度归一（1..3；越界收敛到最近档，徽章 class / 文案共用）。 */
function normalizeSeverity(severity: number): 1 | 2 | 3 {
  if (severity >= 3) return 3;
  if (severity >= 2) return 2;
  return 1;
}

/** 强度徽章 class（1 灰蓝 / 2 黄 / 3 红；三档文字均 ≥5:1）。 */
function severityClass(severity: number): string {
  return `virtualCity-rt__event__badge virtualCity-rt__event__badge--sev${normalizeSeverity(
    severity,
  )}`;
}

/** 方向徽章 class（看多绿 / 看空红 / 中性灰）。 */
function directionClass(direction: string): string {
  if (direction === 'bullish') return 'virtualCity-rt__event__badge virtualCity-rt__event__badge--bull';
  if (direction === 'bearish') return 'virtualCity-rt__event__badge virtualCity-rt__event__badge--bear';
  return 'virtualCity-rt__event__badge virtualCity-rt__event__badge--neutral';
}

/** 注入状态徽章 class。 */
function injectedClass(injected: boolean): string {
  return injected
    ? 'virtualCity-rt__event__badge virtualCity-rt__event__badge--injected'
    : 'virtualCity-rt__event__badge virtualCity-rt__event__badge--not-injected';
}

/** 安全数值格式化（非有限值 ⇒ '—'，绝不显示 NaN）。 */
function num(v: number | undefined | null, digits = 1): string {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '—';
  return v.toFixed(digits);
}

/** 相对时间文本（Unix 秒 → 「{n} 分钟前」；缺时间戳 ⇒ '—'）。 */
function agoText(t: TFunc, tsSec: number | undefined | null): string {
  if (typeof tsSec !== 'number' || !Number.isFinite(tsSec) || tsSec <= 0) return '—';
  const ago = timeAgoParts(tsSec);
  const key = AGO_KEYS[ago.unit];
  if (!key) return '—';
  return t(key as TKey, { n: ago.n });
}

/** 结果快照的 6 项指标（标签键 + 格式化值）。 */
function outcomeRows(
  o: WorldEventOutcome,
): Array<{ key: string; value: string }> {
  return [
    { key: 'virtualCity.rt.event.metric.cpi', value: formatPct(o.cpi_yoy, 2) },
    { key: 'virtualCity.rt.event.metric.unemployment', value: formatPct(o.unemployment_rate, 2) },
    { key: 'virtualCity.rt.event.metric.stockIndex', value: num(o.stock_index, 1) },
    { key: 'virtualCity.rt.event.metric.goldPrice', value: num(o.gold_price, 1) },
    { key: 'virtualCity.rt.event.metric.goodsPrice', value: num(o.goods_price_index, 1) },
    { key: 'virtualCity.rt.event.metric.residentCash', value: formatCny(o.resident_cash_total) },
  ];
}

export interface WorldEventPanelProps {
  /**
   * game.state.world_events 快照。
   * undefined = 后端未上线 / 未开局（缺省键）⇒ 降级提示 + 空态；
   * 非 null 则按 items / auto_enabled 渲染。
   */
  events?: WorldEventsSnapshot | null;
}

export function WorldEventPanel({ events }: WorldEventPanelProps) {
  const t = useT();
  // ⚠ 后端 nil slice 序列化成 null ⇒ 一律兜底空数组。
  const items = events?.items ?? [];

  // ── 降级态 ①：后端未上线 / 未开局（整个 world_events 键缺省）────────────
  if (!events) {
    return (
      <div className="virtualCity-rt__event" data-testid="virtualCity-rt-event">
        <div className="virtualCity-rt__event__notice" role="status">
          {t('virtualCity.rt.event.noRoom' as TKey)}
        </div>
        <div className="virtualCity-panel__empty virtualCity-rt__empty">
          <span className="virtualCity-rt__empty-icon" aria-hidden="true">🌍</span>
          <p>{t('virtualCity.rt.event.empty' as TKey)}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="virtualCity-rt__event" data-testid="virtualCity-rt-event">
      {/* ── 小节标题（子页级，非看板维度标题，避免双标题）── */}
      <div className="virtualCity-rt__event__heading">
        <span className="virtualCity-rt__section-title">
          {t('virtualCity.rt.event.title' as TKey)}
        </span>
        <span className="virtualCity-rt__event__heading-count">
          {t('virtualCity.rt.event.count' as TKey, { n: items.length })}
        </span>
      </div>

      {/* ── 顶部状态条：自动注入开关 + 本月注入数 + 更新时间 ── */}
      <div className="virtualCity-rt__event__bar">
        <span
          className={
            events.auto_enabled
              ? 'virtualCity-rt__event__badge virtualCity-rt__event__badge--auto'
              : 'virtualCity-rt__event__badge virtualCity-rt__event__badge--auto-off'
          }
        >
          {events.auto_enabled
            ? t('virtualCity.rt.event.autoOn' as TKey)
            : t('virtualCity.rt.event.autoOffBadge' as TKey)}
        </span>
        <span className="virtualCity-rt__event__meta">
          {t('virtualCity.rt.event.injectedThisMonth' as TKey, {
            n: events.injected_this_month ?? 0,
          })}
        </span>
        {events.updated_at > 0 && (
          <span className="virtualCity-rt__event__meta">
            {t('virtualCity.rt.event.updated' as TKey, { n: agoText(t, events.updated_at) })}
          </span>
        )}
      </div>

      {/* ── 降级态 ②：自动注入已关闭（只观测不注入）── */}
      {!events.auto_enabled && (
        <div className="virtualCity-rt__event__notice" role="status">
          {t('virtualCity.rt.event.autoOff' as TKey)}
        </div>
      )}

      {/* ── 空态：未触发阈值 / 数据通道离线 ── */}
      {items.length === 0 ? (
        <div className="virtualCity-panel__empty virtualCity-rt__empty">
          <span className="virtualCity-rt__empty-icon" aria-hidden="true">🌍</span>
          <p>{t('virtualCity.rt.event.empty' as TKey)}</p>
        </div>
      ) : (
        <div className="virtualCity-rt__event__list">
          {items.map((ev) => (
            <EventCard ev={ev} key={ev.key || `${ev.rule}-${ev.observed_at}`} />
          ))}
        </div>
      )}
    </div>
  );
}

/** 单条事件卡片（内部组件，便于拆小、避免主组件超长）。 */
function EventCard({ ev }: { ev: WorldEventRecord }) {
  const t = useT();
  const kindKey = KIND_KEYS[ev.kind];
  const scopeKey = SCOPE_KEYS[ev.scope];
  // ⚠ 后端 nil slice ⇒ null；effects 允许空数组。
  const effects = ev.effects ?? [];
  const severityText = t(`virtualCity.rt.event.severity${normalizeSeverity(ev.severity)}` as TKey);
  const directionText = t(
    (DIRECTION_KEYS[ev.direction] ?? DIRECTION_KEYS.neutral) as TKey,
  );
  const observedText = ev.observed_at > 0 ? agoText(t, ev.observed_at) : '—';

  return (
    <article className="virtualCity-rt__event__card" data-testid="virtualCity-rt-event-card">
      {/* 标题 + 强度徽章 + 方向徽章 */}
      <div className="virtualCity-rt__event__head">
        <span className="virtualCity-rt__event__title">{ev.title || ev.kind}</span>
        <span
          className={severityClass(ev.severity)}
          title={t('virtualCity.rt.event.severity' as TKey)}
        >
          {severityText}
        </span>
        <span className={directionClass(ev.direction)}>{directionText}</span>
      </div>

      {/* 明细（触发依据） */}
      {ev.detail && <p className="virtualCity-rt__event__detail">{ev.detail}</p>}

      {/* 标签带：类型 / 作用域 / 规则 / 触发量 */}
      <div className="virtualCity-rt__event__tags">
        <span className="virtualCity-rt__event__tag">
          {kindKey ? t(kindKey as TKey) : ev.kind}
        </span>
        {scopeKey && (
          <span className="virtualCity-rt__event__tag">{t(scopeKey as TKey)}</span>
        )}
        {ev.rule && (
          <span className="virtualCity-rt__event__tag">
            {t('virtualCity.rt.event.rule' as TKey, { rule: ev.rule })}
          </span>
        )}
        {typeof ev.magnitude === 'number' && Number.isFinite(ev.magnitude) && (
          <span className="virtualCity-rt__event__tag">
            {t('virtualCity.rt.event.magnitude' as TKey, { v: pctText(ev.magnitude, 2) })}
          </span>
        )}
      </div>

      {/* 元信息：观测时间 / 来源 / 注入状态 */}
      <div className="virtualCity-rt__event__meta-row">
        <span className="virtualCity-rt__event__meta">
          {t('virtualCity.rt.event.observedAt' as TKey)} {observedText}
        </span>
        {ev.source && (
          <span className="virtualCity-rt__event__meta">
            {t('virtualCity.rt.event.source' as TKey)} {ev.source}
          </span>
        )}
        <span className={injectedClass(!!ev.injected)}>
          {ev.injected
            ? t('virtualCity.rt.event.injected' as TKey)
            : t('virtualCity.rt.event.notInjected' as TKey)}
        </span>
      </div>

      {/* 即时效果（effects 摘要） */}
      {effects.length > 0 && (
        <div className="virtualCity-rt__event__section">
          <span className="virtualCity-rt__event__section-label">
            {t('virtualCity.rt.event.effects' as TKey)}
          </span>
          <div className="virtualCity-rt__event__effects">
            {effects.map((e, i) => (
              <span className="virtualCity-rt__event__effect" key={`${e.metric}-${i}`}>
                {e.text || `${e.metric} ${num(e.delta, 2)}`}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* 次月结果快照（outcome；待回填 ⇒ 不渲染该段） */}
      {ev.outcome && (
        <div className="virtualCity-rt__event__section">
          <span className="virtualCity-rt__event__section-label">
            {t('virtualCity.rt.event.outcome' as TKey, { m: ev.outcome.month })}
          </span>
          <div className="virtualCity-rt__event__outcome">
            {outcomeRows(ev.outcome).map((row) => (
              <span className="virtualCity-rt__event__outcome-item" key={row.key}>
                <span className="virtualCity-rt__event__outcome-label">
                  {t(row.key as TKey)}
                </span>
                <span className="virtualCity-rt__event__outcome-value">{row.value}</span>
              </span>
            ))}
          </div>
        </div>
      )}

      {/* 原文链接（url 为空串 ⇒ 不渲染） */}
      {ev.url && (
        <a
          className="virtualCity-rt__event__link"
          href={ev.url}
          target="_blank"
          rel="noopener noreferrer"
        >
          {t('virtualCity.rt.event.link' as TKey)} ↗
        </a>
      )}
    </article>
  );
}

export default WorldEventPanel;
