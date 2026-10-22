/**
 * MissionLogPanel — 批次 77 实时看板「采集」子页。
 *
 * 数据 = GET /api/virtual_city/realtime_board 的 data.missions（服务端 MissionLog
 * 环形记录新→旧 ≤20 条）。⚠️ WS game.realtime_feed 帧**不携带** missions —— 只有
 * HTTP 快照 / 手动刷新回读更新（store.realtimeMissions），本组件被动渲染。
 *
 * 行 = 时间（timeAgo 复用 boardUtils）/ 类目（复用 virtualCity.rt.sub.* 子 Tab
 * 标签，未知类目回退原始字符串）/ 状态徽章（running 黄 / ok 绿 / fail 红，
 * 徽章文字 ≥5:1）/ 来源 / 步数 used/max / 数据（📈quotes 📰news）/ 规划（空串
 * 显示占位文案，非空用等宽样式）；失败行内联红字 error。
 * 空态沿用 BehaviorPanel 范式（图标 + 说明占位块，同 NewsList）。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { MissionRecord } from '@/types/virtualCityRealtime';
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

/** 状态徽章 class（running 黄 / ok 绿 / fail 红；未知按 fail 口径）。 */
function badgeClass(status: string): string {
  if (status === 'running') {
    return 'virtualCity-rt-mlog__badge virtualCity-rt-mlog__badge--running';
  }
  if (status === 'ok') return 'virtualCity-rt-mlog__badge virtualCity-rt-mlog__badge--ok';
  return 'virtualCity-rt-mlog__badge virtualCity-rt-mlog__badge--fail';
}

export interface MissionLogPanelProps {
  /** 新→旧 ≤20 条；null = 快照未回读，[] = 无记录 —— 两者都显示空态。 */
  missions: MissionRecord[] | null;
}

export function MissionLogPanel({ missions }: MissionLogPanelProps) {
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
      <div className="virtualCity-rt-mlog__row virtualCity-rt-mlog__row--head">
        <span>{t('virtualCity.rt.missions.col.time' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.category' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.status' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.source' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.steps' as TKey)}</span>
        <span>{t('virtualCity.rt.missions.col.data' as TKey)}</span>
      </div>
      {missions.map((m) => {
        const ago = timeAgoParts(m.started_at);
        const catKey = CATEGORY_LABEL_KEYS[m.category];
        const statusKey = STATUS_KEYS[m.status] ?? STATUS_KEYS.fail;
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
