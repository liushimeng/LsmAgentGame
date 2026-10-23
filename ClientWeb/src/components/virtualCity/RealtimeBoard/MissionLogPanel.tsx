/**
 * MissionLogPanel — 批次 77 实时看板「采集」子页（批次 78 增统计条 + 站点尝试列）。
 *
 * 数据 = GET /api/virtual_city/realtime_board 的 data.missions（服务端 MissionLog
 * 环形记录新→旧 ≤20 条）。⚠️ WS game.realtime_feed 帧**不携带** missions / stats ——
 * 只有 HTTP 快照 / 手动刷新回读更新（store.realtimeMissions / store.realtimeStats），
 * 本组件被动渲染。
 *
 * 行 = 时间（timeAgo 复用 boardUtils）/ 类目（复用 virtualCity.rt.sub.* 子 Tab
 * 标签，未知类目回退原始字符串）/ 状态徽章（running 黄 / ok 绿 / fail 红，
 * 徽章文字 ≥5:1）/ 来源 / 步数 used/max / 数据（📈quotes 📰news）/ 站点尝试
 * （批次78：visited 折叠为 host+pathname，最多 3 条 + "+N"）/ 规划（空串显示占位
 * 文案，非空用等宽样式）；失败行内联红字 error。
 *
 * ⚠ 布局口径（批次78）：侧栏仅 340~380px，6 列并排已把「来源」列压到 ~50px ——
 *   再加第 7 列会让「来源」彻底消失 ⇒ 「站点尝试」按**整行带**渲染（与既有
 *   「规划」带同范式：grid-column 1/-1 + 行内标签），表头 6 列保持逐字不变。
 * 空态沿用 BehaviorPanel 范式（图标 + 说明占位块，同 NewsList）。
 *
 * 批次 78 统计条（.virtualCity-rt-mlog__stats__*）：总次数 / 成功率 / 平均步数 /
 * 平均耗时 / 失败次数 —— stats 为 null（快照未回读）⇒ 整条不渲染，行数据照常。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { MissionRecord, MissionStats } from '@/types/virtualCityRealtime';
import { timeAgoKey, timeAgoParts } from './boardUtils';

/** 类目 → 子 Tab 标签键复用（virtualCity.rt.sub.*）；未知类目回退原始 category 字符串。 */
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

/** 状态 → i18n 键（未知状态按 fail 口径；translate 恒返回字符串，§17）。 */
const STATUS_KEYS: Record<string, string> = {
  running: 'virtualCity.rt.missions.status.running',
  ok: 'virtualCity.rt.missions.status.ok',
  fail: 'virtualCity.rt.missions.status.fail',
};

/** 站点尝试最多展示的条数（其余折叠为 "+N"）。 */
const VISITED_MAX = 3;

/** 状态徽章 class（running 黄 / ok 绿 / fail 红；未知按 fail 口径）。 */
function badgeClass(status: string): string {
  if (status === 'running') {
    return 'virtualCity-rt-mlog__badge virtualCity-rt-mlog__badge--running';
  }
  if (status === 'ok') return 'virtualCity-rt-mlog__badge virtualCity-rt-mlog__badge--ok';
  return 'virtualCity-rt-mlog__badge virtualCity-rt-mlog__badge--fail';
}

/**
 * URL → 「host + pathname」短标签（不泄露 query / hash）。
 *
 * 解析失败（非绝对 URL）⇒ 原样返回，避免把整条长串塞进窄列。
 */
function visitedLabel(raw: string): string {
  try {
    const u = new URL(raw);
    return `${u.host}${u.pathname}`;
  } catch {
    return raw;
  }
}

/** 安全数值格式化（非有限值 ⇒ '—'，绝不显示 NaN）。 */
function num(v: number | undefined | null, digits: number): string {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '—';
  return v.toFixed(digits);
}

/** 成功率 0..1 ⇒ 百分比文本（1 位小数；缺字段 ⇒ '—'，不拼出「—%」）。 */
function rateText(rate: number | undefined | null): string {
  if (typeof rate !== 'number' || !Number.isFinite(rate)) return '—';
  return `${(rate * 100).toFixed(1)}%`;
}

export interface MissionLogPanelProps {
  /** 新→旧 ≤20 条；null = 快照未回读，[] = 无记录 —— 两者都显示空态。 */
  missions: MissionRecord[] | null;
  /**
   * 批次 78 · 采集健康度聚合（HTTP 快照回读；WS 帧不携带）。
   * null = 未回读 ⇒ 不渲染统计条（行数据照常）。
   */
  stats?: MissionStats | null;
}

export function MissionLogPanel({ missions, stats }: MissionLogPanelProps) {
  const t = useT();
  if (!missions || !missions.length) {
    return (
      <div className="virtualCity-panel__empty virtualCity-rt__empty">
        <span className="virtualCity-rt__empty-icon" aria-hidden="true">🛰️</span>
        <p>{t('virtualCity.rt.missions.empty' as TKey)}</p>
      </div>
    );
  }
  return (
    <div className="virtualCity-rt-mlog" data-testid="virtualCity-rt-mlog">
      {/* 批次 78 · 采集健康度统计条（stats 为 null ⇒ 整条不渲染）。 */}
      {stats && (
        <div
          className="virtualCity-rt-mlog__stats"
          data-testid="virtualCity-rt-mlog-stats"
        >
          <div className="virtualCity-rt-mlog__stats__item">
            <span className="virtualCity-rt-mlog__stats__label">
              {t('virtualCity.rt.missions.stats.total' as TKey)}
            </span>
            <span className="virtualCity-rt-mlog__stats__value">
              {num(stats.total, 0)}
            </span>
          </div>
          <div className="virtualCity-rt-mlog__stats__item">
            <span className="virtualCity-rt-mlog__stats__label">
              {t('virtualCity.rt.missions.stats.successRate' as TKey)}
            </span>
            <span className="virtualCity-rt-mlog__stats__value">
              {rateText(stats.success_rate)}
            </span>
          </div>
          <div className="virtualCity-rt-mlog__stats__item">
            <span className="virtualCity-rt-mlog__stats__label">
              {t('virtualCity.rt.missions.stats.avgSteps' as TKey)}
            </span>
            <span className="virtualCity-rt-mlog__stats__value">
              {num(stats.avg_steps, 1)}
            </span>
          </div>
          <div className="virtualCity-rt-mlog__stats__item">
            <span className="virtualCity-rt-mlog__stats__label">
              {t('virtualCity.rt.missions.stats.avgDuration' as TKey)}
            </span>
            <span className="virtualCity-rt-mlog__stats__value">
              {t('virtualCity.rt.missions.stats.seconds' as TKey, {
                n: num(stats.avg_duration_ms / 1000, 1),
              })}
            </span>
          </div>
          <div className="virtualCity-rt-mlog__stats__item">
            <span className="virtualCity-rt-mlog__stats__label">
              {t('virtualCity.rt.missions.stats.fail' as TKey)}
            </span>
            {/* 失败数复用既有 fail 徽章色板（§26.1 文字 ≥5:1），不自造状态色。 */}
            <span className="virtualCity-rt-mlog__badge virtualCity-rt-mlog__badge--fail">
              {num(stats.fail, 0)}
            </span>
          </div>
        </div>
      )}
      <div className="virtualCity-rt-mlog__row virtualCity-rt-mlog__row--head">
        <span>{t('virtualCity.rt.missions.col.time' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.category' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.status' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.source' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.steps' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.data' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.visited' as TKey)}</span>
      </div>
      {missions.map((m) => {
        const ago = timeAgoParts(m.started_at);
        const catKey = CATEGORY_LABEL_KEYS[m.category];
        const statusKey = STATUS_KEYS[m.status] ?? STATUS_KEYS.fail;
        // ⚠ 服务端 nil slice 序列化成 null ⇒ 一律兜底空数组。
        const visited = m.visited ?? [];
        return (
          <div className="virtualCity-rt-mlog__row" key={m.id} data-testid="virtualCity-rt-mlog-row">
            <span className="virtualCity-rt-mlog__cell-time">
              {t(timeAgoKey(ago) as TKey, { n: ago.n })}
            </span>
            <span className="virtualCity-rt-mlog__cell-cat">
              {catKey ? t(catKey as TKey) : m.category}
            </span>
            <span>
              <span className={badgeClass(m.status)}>{t(statusKey as TKey)}</span>
            </span>
            <span className="virtualCity-rt-mlog__cell-src">{m.source || '—'}</span>
            <span className="virtualCity-rt-mlog__cell-steps">
              {m.steps_used}/{m.max_steps}
            </span>
            <span className="virtualCity-rt-mlog__cell-data">
              <span title="quotes">📈 {m.quotes}</span>
              <span title="news">📰 {m.news}</span>
            </span>
            {/* 批次 78 · 站点尝试带（整行铺开，与「规划」带同范式）——
                侧栏仅 340~380px，7 列并排会把「来源」列挤没 ⇒ 走整行带。 */}
            <div className="virtualCity-rt-mlog__visited">
              <span className="virtualCity-rt-mlog__visited__label">
                {t('virtualCity.rt.missions.col.visited' as TKey)}
              </span>
              {visited.length === 0 ? (
                <span className="virtualCity-rt-mlog__visited__empty">—</span>
              ) : (
                <>
                  {visited.slice(0, VISITED_MAX).map((u, i) => (
                    <span
                      className="virtualCity-rt-mlog__visited__item"
                      key={`${u}-${i}`}
                      title={u}
                    >
                      {visitedLabel(u)}
                    </span>
                  ))}
                  {visited.length > VISITED_MAX && (
                    <span className="virtualCity-rt-mlog__visited__more">
                      +{visited.length - VISITED_MAX}
                    </span>
                  )}
                </>
              )}
            </div>
            <div className="virtualCity-rt-mlog__plan">
              <span className="virtualCity-rt-mlog__plan-label">
                {t('virtualCity.rt.missions.col.plan' as TKey)}
              </span>
              {m.plan ? (
                <code className="virtualCity-rt-mlog__plan-code">{m.plan}</code>
              ) : (
                <span className="virtualCity-rt-mlog__plan-none">
                  {t('virtualCity.rt.missions.planNone' as TKey)}
                </span>
              )}
            </div>
            {m.status === 'fail' && !!m.error && (
              <div className="virtualCity-rt-mlog__error">{m.error}</div>
            )}
          </div>
        );
      })}
    </div>
  );
}

export default MissionLogPanel;
