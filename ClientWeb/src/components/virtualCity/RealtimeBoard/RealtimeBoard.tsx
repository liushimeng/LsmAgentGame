/**
 * RealtimeBoard — 批次 65 实时看板容器。
 *
 * 顶部 = 通道状态徽章（online 绿 / offline 灰底深字 / updating 黄，徽章文字 ≥5:1，
 * 禁 opacity 表状态）+「更新于 N 分钟前」+ 刷新按钮（点击 POST
 * /api/virtual_city/realtime_board/refresh，转圈；失败按 §7.1 在面板内联红条
 * 展示，不吞 console）。
 * 子 Tab 11 个（.virtualCity-subtabs 手写范式，可横向滚动）：总览 / A股 / 港股 /
 * 美股 / 贵金属 / 期货 / 比特币 / 宏观（批次76）/ 新闻 / 事件流（批次80）/ 采集（批次77）。
 *
 * 数据源：store.realtimeFeed（首屏 HTTP 快照 + game.realtime_feed 帧增量，
 * 见 useVirtualCity / VirtualCityGamePage）。
 * ⚠ 批次80「事件流」子页例外：数据源 = store.gameState.world_events
 *   （随 game.state 帧刷新），**不依赖**外部行情通道 ⇒ 通道离线时仍可达。
 */

import { useEffect, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { fetchRealtimeBoard, refreshRealtimeBoard } from '@/api/virtualCity';
import { getCategory, timeAgoKey, timeAgoParts } from './boardUtils';
import { BoardOverview } from './BoardOverview';
import { QuoteTable } from './QuoteTable';
import { NewsList } from './NewsList';
import { MissionLogPanel } from './MissionLogPanel';
import { WorldEventPanel } from './WorldEventPanel';

type RtSubTab =
  | 'overview' | 'a_share' | 'hk' | 'us' | 'metal' | 'futures' | 'crypto'
  | 'macro' | 'news' | 'events' | 'missions';

/** 行情子 Tab → feed.categories 键（overview / news 无类目数据）。 */
const QUOTE_SUBTABS: RtSubTab[] = ['a_share', 'hk', 'us', 'metal', 'futures', 'crypto', 'macro'];

/** 1 分钟粒度重渲染「更新于 N 分钟前」。 */
function useMinuteTick(): number {
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));
  useEffect(() => {
    const timer = setInterval(() => setNow(Math.floor(Date.now() / 1000)), 30_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

export function RealtimeBoard() {
  const t = useT();
  const feed = useVirtualCityStore((s) => s.realtimeFeed);
  const missions = useVirtualCityStore((s) => s.realtimeMissions);
  // 批次78：采集健康度聚合（同样只随 HTTP 快照回读，WS 帧不携带）。
  const stats = useVirtualCityStore((s) => s.realtimeStats);
  // 批次80：真实世界事件流（数据源 = game.state.world_events，随 game.state 帧刷新；
  // 不走 HTTP 快照、也不进 WS game.realtime_feed 帧 ⇒ 后端未上线时为 undefined）。
  const worldEvents = useVirtualCityStore((s) => s.gameState?.world_events);
  const [sub, setSub] = useState<RtSubTab>('overview');
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const nowSec = useMinuteTick();

  const status = refreshing ? 'updating' : (feed?.channel.status ?? 'offline');
  const ago = feed ? timeAgoParts(feed.updated_at, nowSec) : null;

  const handleRefresh = async (category?: string) => {
    if (refreshing) return;
    setRefreshing(true);
    setError(null);
    try {
      await refreshRealtimeBoard(category);
      // 快照不回读：mission 是异步的，数据经 game.realtime_feed 帧落库；
      // 拉一次快照让 accepted 后的通道状态即时可见（失败静默，等 WS 帧）。
      // 批次77：missions 不进 WS 帧，一并随快照回读（无记录为 []）。
      // 批次78：stats 同理（无记录为 null ⇒ 不渲染统计条）。
      try {
        const r = await fetchRealtimeBoard();
        useVirtualCityStore.getState().setRealtimeFeed(r.feed ?? null);
        useVirtualCityStore.getState().setRealtimeMissions(r.missions ?? []);
        useVirtualCityStore.getState().setRealtimeStats(r.stats ?? null);
      } catch {
        // best-effort：WS 帧会补齐
      }
    } catch (e) {
      // §7.1：面板内联红条展示，不吞进 console。
      const msg = e instanceof Error ? e.message : String(e);
      setError(t('virtualCity.rt.refreshFail' as TKey, { msg }));
    } finally {
      setRefreshing(false);
    }
  };

  const badgeClass =
    status === 'online'
      ? 'virtualCity-rt__badge virtualCity-rt__badge--online'
      : status === 'updating'
        ? 'virtualCity-rt__badge virtualCity-rt__badge--updating'
        : 'virtualCity-rt__badge virtualCity-rt__badge--offline';
  const badgeKey =
    status === 'online'
      ? 'virtualCity.rt.channel.online'
      : status === 'updating'
        ? 'virtualCity.rt.channel.updating'
        : 'virtualCity.rt.channel.offline';

  const subs: Array<{ key: RtSubTab; labelKey: string }> = [
    { key: 'overview', labelKey: 'virtualCity.rt.sub.overview' },
    { key: 'a_share', labelKey: 'virtualCity.rt.sub.a_share' },
    { key: 'hk', labelKey: 'virtualCity.rt.sub.hk' },
    { key: 'us', labelKey: 'virtualCity.rt.sub.us' },
    { key: 'metal', labelKey: 'virtualCity.rt.sub.metal' },
    { key: 'futures', labelKey: 'virtualCity.rt.sub.futures' },
    { key: 'crypto', labelKey: 'virtualCity.rt.sub.crypto' },
    // 批次76:宏观指标类目（LPR/CPI/GDP/PMI/外汇储备）。
    { key: 'macro', labelKey: 'virtualCity.rt.sub.macro' },
    { key: 'news', labelKey: 'virtualCity.rt.sub.news' },
    // 批次80:真实世界事件流（game.state.world_events；置于「采集」前）。
    { key: 'events', labelKey: 'virtualCity.rt.sub.events' },
    // 批次77:采集任务遥测（HTTP 快照回读，WS 帧不携带 missions）。
    { key: 'missions', labelKey: 'virtualCity.rt.sub.missions' },
  ];

  return (
    <div className="virtualCity-rt">
      <div className="virtualCity-rt__head">
        <span className={badgeClass} data-testid="virtualCity-rt-channel">
          {t(badgeKey as TKey)}
        </span>
        {ago && (
          <span className="virtualCity-rt__time">
            {t(timeAgoKey(ago) as TKey, { n: ago.n })}
          </span>
        )}
        <button
          type="button"
          className="virtualCity-rt__refresh"
          onClick={() => void handleRefresh()}
          disabled={refreshing}
          title={t('virtualCity.rt.refresh' as TKey)}
          data-testid="virtualCity-rt-refresh"
        >
          <span className={refreshing ? 'virtualCity-rt__spin' : ''} aria-hidden="true">⟳</span>
          {' '}{t('virtualCity.rt.refresh' as TKey)}
        </button>
      </div>

      {/* 通道离线原因（服务端权威文案，有则显示）。 */}
      {feed && feed.channel.status === 'offline' && feed.channel.detail && (
        <div className="virtualCity-rt__offline" role="status">
          {feed.channel.detail}
        </div>
      )}

      {/* §7.1：刷新失败面板内联红条。 */}
      {error && (
        <div className="virtualCity-rt__error" role="alert">
          <span>⚠️ {error}</span>
          <button type="button" onClick={() => setError(null)} aria-label="dismiss">×</button>
        </div>
      )}

      <div className="virtualCity-subtabs">
        {subs.map((s) => (
          <button
            key={s.key}
            type="button"
            className={
              'virtualCity-subtabs__btn' + (sub === s.key ? ' virtualCity-subtabs__btn--active' : '')
            }
            onClick={() => setSub(s.key)}
          >
            {t(s.labelKey as TKey)}
          </button>
        ))}
      </div>

      {/* 首屏快照未到且无 feed：整板空态。
          ⚠ 批次80 例外：「事件流」子页的数据源是 game.state.world_events，
            与外部行情通道无关 ⇒ 即使 feed 未到也照常渲染（否则通道离线时
            事件流不可达，而通道离线恰恰是「无事件」的常见成因）。 */}
      {sub === 'events' ? (
        <WorldEventPanel events={worldEvents} />
      ) : !feed ? (
        <div className="virtualCity-panel__empty virtualCity-rt__empty">
          <span className="virtualCity-rt__empty-icon" aria-hidden="true">📡</span>
          <p>{t('virtualCity.rt.empty.feed' as TKey)}</p>
        </div>
      ) : (
        <>
          {sub === 'overview' && <BoardOverview feed={feed} />}
          {sub === 'news' && <NewsList news={feed.news} />}
          {sub === 'missions' && <MissionLogPanel missions={missions} stats={stats} />}
          {QUOTE_SUBTABS.includes(sub) && (() => {
            const cat = getCategory(feed.categories, sub);
            if (!cat || !cat.quotes.length) {
              return (
                <div className="virtualCity-panel__empty virtualCity-rt__empty">
                  <span className="virtualCity-rt__empty-icon" aria-hidden="true">📈</span>
                  <p>{t('virtualCity.rt.empty.category' as TKey)}</p>
                </div>
              );
            }
            return <QuoteTable category={cat} />;
          })()}
        </>
      )}
    </div>
  );
}

export default RealtimeBoard;
