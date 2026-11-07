/**
 * MetricSpecPanel — 批次 89「口径与异常」子面板。
 *
 * 两块内容：
 *  ① **指标口径卡清单**（按类目分组）：指标名 / 单位 / 口径说明 / 计算式 /
 *     来源 / 刷新档位 / 原文链接。数据源 = `GET /api/virtual_city/realtime_board`
 *     的 `data.metric_specs`（静态目录 ⇒ **不进 WS 帧**，只随 HTTP 快照回读）。
 *  ② **异常告警清单**：kind / severity / value / detail / observed_at。
 *     数据源 = 同一快照的 `data.anomalies`。
 *
 * 空态：口径卡为空 ⇒ 「暂无口径卡」；异常为空 ⇒ 「暂无异常告警」（两块各自空态，
 * 不用一整块空白糊弄）。
 *
 * ⚠ §26 对比度：暗色主题正文 ≥4.5:1、状态徽章 ≥5:1；异常徽章**必带文案**
 *   （禁只靠颜色）；severity 用实色底 + 深/浅字，禁 `opacity` 表状态。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { Anomaly, MetricSpec, RtAnomalyKind } from '@/types/virtualCityRealtime';
import {
  anomalyLabelKey,
  groupSpecsByCategory,
  timeAgoKey,
  timeAgoParts,
  unitLabelKey,
} from './boardUtils';

/** 异常徽章图标（图标 + 文案双通道，禁只靠颜色传达信息）。 */
const ANOMALY_ICON: Record<string, string> = {
  high: '▲',
  low: '▼',
  stale: '⏳',
  jump: '⚡',
};

/** 严重度 → i18n 键（severity 1..3；越界回退 L1）。 */
const SEVERITY_KEYS: Record<number, string> = {
  1: 'virtualCity.rt.anomaly.severity1',
  2: 'virtualCity.rt.anomaly.severity2',
  3: 'virtualCity.rt.anomaly.severity3',
};

/**
 * 类目 → i18n 键（复用既有子 Tab 标签 `virtualCity.rt.sub.*`）。
 *
 * ⚠ 未知类目**回退原始字符串**：translate 对缺 key 返回 key 本身 ⇒ 直接
 *   `t('virtualCity.rt.sub.' + cat)` 会把 "virtualCity.rt.sub.xyz" 显示给用户。
 */
const CATEGORY_LABEL_KEYS: Record<string, string> = {
  a_share: 'virtualCity.rt.sub.a_share',
  hk: 'virtualCity.rt.sub.hk',
  us: 'virtualCity.rt.sub.us',
  metal: 'virtualCity.rt.sub.metal',
  futures: 'virtualCity.rt.sub.futures',
  crypto: 'virtualCity.rt.sub.crypto',
  macro: 'virtualCity.rt.sub.macro',
  news: 'virtualCity.rt.sub.news',
};

/** 异常徽章 class（high 涨异常 / low 跌异常 / stale 陈旧 / jump 跳变）。 */
function anomalyClass(kind: string): string {
  return `virtualCity-rt__anom virtualCity-rt__anom--${kind}`;
}

/** 严重度徽章 class（L1 / L2 / L3 实色底，禁 opacity）。 */
function severityClass(sev: number): string {
  const lvl = sev >= 3 ? 3 : sev === 2 ? 2 : 1;
  return `virtualCity-rt__sev virtualCity-rt__sev--${lvl}`;
}

/** 异常种类文案（i18n；未知 kind 回退原始字符串，不丢信息、不白屏）。 */
function AnomalyKindLabel({ kind }: { kind: RtAnomalyKind | string }) {
  const t = useT();
  const key = anomalyLabelKey(kind);
  return <>{key ? t(key as TKey) : kind}</>;
}

/** 单位文案（i18n；未知单位回退原始 code）。 */
function UnitLabel({ unit }: { unit: string }) {
  const t = useT();
  const key = unitLabelKey(unit);
  return <>{key ? t(key as TKey) : unit || '—'}</>;
}

function SpecCard({ spec }: { spec: MetricSpec }) {
  const t = useT();
  const link = spec.source_url || '';
  return (
    <div className="virtualCity-rt__spec" data-testid="virtualCity-rt-spec-card">
      <div className="virtualCity-rt__spec-head">
        <span className="virtualCity-rt__spec-name">{spec.indicator || spec.id}</span>
        <span className="virtualCity-rt__spec-unit">
          <UnitLabel unit={spec.unit} />
        </span>
      </div>
      <dl className="virtualCity-rt__spec-list">
        <dt>{t('virtualCity.rt.caliber.definition' as TKey)}</dt>
        <dd>{spec.caliber || '—'}</dd>
        <dt>{t('virtualCity.rt.caliber.formula' as TKey)}</dt>
        <dd>
          {spec.formula ? (
            <code className="virtualCity-rt__caliber-code">{spec.formula}</code>
          ) : (
            '—'
          )}
        </dd>
        <dt>{t('virtualCity.rt.caliber.source' as TKey)}</dt>
        <dd>{spec.source || '—'}</dd>
        <dt>{t('virtualCity.rt.caliber.refresh' as TKey)}</dt>
        <dd>{spec.refresh || '—'}</dd>
        <dt>{t('virtualCity.rt.caliber.link' as TKey)}</dt>
        <dd>
          {link ? (
            <a
              className="virtualCity-rt__caliber-link"
              href={link}
              target="_blank"
              rel="noreferrer"
              title={link}
            >
              {/* ⚠ D5（活体验收结论，**无需改动**）：下方 `—` 分支表示
                  **原文链接缺失是后端刻意留空**（采集层不带 per-quote URL，
                  后端不编造链接），**非渲染缺陷** ⇒ 勿改文案、勿伪造链接。 */}
              {t('virtualCity.rt.caliber.linkOpen' as TKey)}
            </a>
          ) : (
            '—'
          )}
        </dd>
      </dl>
      <div className="virtualCity-rt__spec-id" title={spec.id}>
        {spec.id}
      </div>
    </div>
  );
}

function AnomalyRow({ anomaly, nowSec }: { anomaly: Anomaly; nowSec: number }) {
  const t = useT();
  const sevKey = SEVERITY_KEYS[anomaly.severity] ?? SEVERITY_KEYS[1];
  const value = Number.isFinite(anomaly.value) ? anomaly.value : null;
  // ⚠ 后端口径（主 Agent 收口裁定）：`Anomaly.value` 的**单位随 kind 变** ——
  //   high / low / jump = **幅度百分比**（%）；stale = **陈旧秒数**（秒）。
  //   ⇒ stale 走「秒」文案（`virtualCity.rt.anomaly.seconds`），其余走「%」。
  //   绝不给 stale 追加 `%` 后缀（否则 14400 秒会显示成「14400.00%」）。
  const isStale = anomaly.kind === 'stale';
  // ⚠ D4（活体验收）：high / low 的后端 `value` 是**带符号**的 change_pct
  //   （low 为负，如 -3.96），而 `detail` 是后端拼的**绝对幅度**文案
  //   （「跌幅 3.96% > 阈值 3.00%」）⇒ 同屏出现「-3.96%」与「3.96%」像两个数。
  //   ⇒ high / low 渲染**绝对值**，与 detail 口径一致；方向由徽章图标
  //     （▲/▼）与 kind 文案承载，不靠数值符号表达。
  //   jump 后端已是 `math.Abs(...)`（恒正），保持原值即可。
  const pctText = isStale
    ? ''
    : `${(anomaly.kind === 'high' || anomaly.kind === 'low' ? Math.abs(value ?? 0) : (value ?? 0)).toFixed(2)}%`;
  // ⚠ D3（活体验收）：`observed_at` 是裸 Unix 时间戳（实测 1793956279），
  //   与看板头部的「刚刚更新」两种时间口径同屏割裂 ⇒ 改用既有
  //   `timeAgoParts` + `timeAgoKey`（boardUtils.ts，与看板头部同一实现）
  //   渲染相对时间。i18n 复用既有 `virtualCity.rt.updated.*`，不新增键。
  const ago = timeAgoParts(anomaly.observed_at, nowSec);
  return (
    <div className="virtualCity-rt__anomrow" data-testid="virtualCity-rt-anomaly-row">
      <div className="virtualCity-rt__anomrow-head">
        <span className={anomalyClass(anomaly.kind)}>
          <span className="virtualCity-rt__anom-icon" aria-hidden="true">
            {ANOMALY_ICON[anomaly.kind] ?? '⚠'}
          </span>
          <span className="virtualCity-rt__anom-text">
            <AnomalyKindLabel kind={anomaly.kind} />
          </span>
        </span>
        <span className={severityClass(anomaly.severity)}>
          {t(sevKey as TKey)}
        </span>
        <span className="virtualCity-rt__anomrow-name">{anomaly.name || anomaly.symbol}</span>
      </div>
      {value !== null && (
        <div className="virtualCity-rt__anomrow-value">
          {t('virtualCity.rt.anomaly.value' as TKey)}:{' '}
          {isStale
            ? t('virtualCity.rt.anomaly.seconds' as TKey, { n: Math.round(value) })
            : pctText}
        </div>
      )}
      {anomaly.detail && (
        <p className="virtualCity-rt__anomrow-detail">{anomaly.detail}</p>
      )}
      <div className="virtualCity-rt__anomrow-meta">
        {anomaly.symbol ? `${anomaly.symbol} · ` : ''}
        {t('virtualCity.rt.anomaly.observedAt' as TKey)}:{' '}
        {t(timeAgoKey(ago) as TKey, { n: ago.n })}
      </div>
    </div>
  );
}

export function MetricSpecPanel({
  metricSpecs,
  anomalies,
  nowSec,
}: {
  /** 口径卡目录（null = 快照未回读；[] = 无口径卡）。 */
  metricSpecs: MetricSpec[] | null;
  /** 异常告警清单（null = 快照未回读；[] = 无异常）。 */
  anomalies: Anomaly[] | null;
  /**
   * 当前时刻（Unix 秒）—— 供异常行渲染**相对时间**（D3）。
   *
   * ⚠ 由**调用方**注入（`RealtimeBoard.tsx` 的 `useMinuteTick()`），
   *   本组件**不另建定时器**（避免多个 tick 各自跑、且便于测试传确定性 now）。
   */
  nowSec: number;
}) {
  const t = useT();
  const groups = groupSpecsByCategory(metricSpecs);
  const list = anomalies ?? [];

  return (
    <div className="virtualCity-rt__specpanel" data-testid="virtualCity-rt-specpanel">
      {/* ① 指标口径卡（按类目分组） */}
      <section className="virtualCity-rt__specpanel-sec">
        <h4 className="virtualCity-rt__specpanel-title">
          {t('virtualCity.rt.specpanel.specsTitle' as TKey)}
          {metricSpecs ? (
            <span className="virtualCity-rt__specpanel-count">
              {t('virtualCity.rt.specpanel.specsCount' as TKey, { n: metricSpecs.length })}
            </span>
          ) : null}
        </h4>
        {!metricSpecs || !groups.length ? (
          <div className="virtualCity-rt__specpanel-empty">
            {t('virtualCity.rt.specpanel.specsEmpty' as TKey)}
          </div>
        ) : (
          groups.map((g) => (
            <div className="virtualCity-rt__specgroup" key={g.category}>
              <div className="virtualCity-rt__specgroup-name">
                {(() => {
                  const key = CATEGORY_LABEL_KEYS[g.category];
                  return key ? t(key as TKey) : g.category;
                })()}
              </div>
              {g.items.map((s) => (
                <SpecCard key={s.id} spec={s} />
              ))}
            </div>
          ))
        )}
      </section>

      {/* ② 异常告警清单 */}
      <section className="virtualCity-rt__specpanel-sec">
        <h4 className="virtualCity-rt__specpanel-title">
          {t('virtualCity.rt.specpanel.anomTitle' as TKey)}
          {anomalies ? (
            <span className="virtualCity-rt__specpanel-count">
              {t('virtualCity.rt.specpanel.anomCount' as TKey, { n: list.length })}
            </span>
          ) : null}
        </h4>
        {!anomalies || !list.length ? (
          <div className="virtualCity-rt__specpanel-empty">
            {t('virtualCity.rt.specpanel.anomEmpty' as TKey)}
          </div>
        ) : (
          list.map((a) => (
            <AnomalyRow
              key={`${a.category}:${a.symbol}:${a.kind}`}
              anomaly={a}
              nowSec={nowSec}
            />
          ))
        )}
      </section>
    </div>
  );
}

export default MetricSpecPanel;
