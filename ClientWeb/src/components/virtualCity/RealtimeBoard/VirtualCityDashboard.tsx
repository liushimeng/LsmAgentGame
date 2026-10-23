/**
 * VirtualCityDashboard — 批次 69「虚拟城市 - 实时看板」外壳。
 *
 * 把批次 62/65 之前散落的 9 个平级业务 Tab 统一重构为**单一看板**：
 *   3 组 12 个一级 Tab（维度）：
 *     城市运行 city    → city-overview / finance / ledger / economy / behavior / macro / calibration
 *     市场金融 market  → market / market-trade / insurance
 *     外部实时 external → realtime / datasources
 *
 * 壳层结构（自上而下）：
 *   ① 标题条：标题「虚拟城市 - 实时看板」+ 副行「数据版本 v{n} · {n} 个维度」
 *   ② 实时通道徽章 + 「更新于 N 分钟前」+ 刷新按钮（**只反映外部行情通道**；
 *      城市内维度的新鲜度由各自面板负责，看板不重复展示）
 *      —— ①② 合并到同一视觉带（左侧标题/副行，右侧通道区），380px 侧栏省一行高度。
 *   ③ **两级 Tab 导航**（.virtualCity-dash__nav，sticky）：
 *      第一级 = 分类选择器（城市运行 / 市场金融 / 外部实时，3 等分单行）；
 *      第二级 = 当前分类下的维度按钮（横排 wrap）。
 *      切换分类时若当前维度不在新分类下 ⇒ 自动切到该分类的第一个维度。
 *   ④ 当前维度面板（外层包 BoardSection，可折叠；切换维度后自动把面板体滚入视野）
 *
 * ⚠ 布局契约（批次69 二轮实测回归修复）：
 *   本组件必须是 `.virtualCity-sidebar` 的**直接子元素** —— 侧栏自己就是滚动容器
 *   （overflow-y:auto，见 virtualCity-city3d.css）。若中间夹一层
 *   `.virtualCity-sidebar__panels`（独立滚动窗，min-height 仅 160px），Tab 条与
 *   面板体会被关进小窗 ⇒ 屏上只剩 Tab 条、看不到任何数据；且中间那层 overflow:auto
 *   还会让本组件内的 sticky 导航失效。
 *
 * 关键契约：
 *   - **Tab 的权威来源是静态目录**（dashboardCatalog.ts）⇒ 首屏不依赖网络；
 *     后端 dimensions[] 只做校验/补充（按 id 覆盖 title_key/group，未知 id 忽略）。
 *   - 当前 Tab 读写 store.panelTab（VirtualCityPanelTab）⇒ ActionPanel 的
 *     onTradeTab(→'market-trade') / GameOverModal 的 onViewLedger(→'ledger')
 *     等跨面板跳转继续有效。
 *   - §7.1：刷新 / 快照失败一律**看板内联红条**展示，不吞进 console。
 *   - 标题去重：看板已有统一标题条 ⇒ 面板自带的纯标题元素（behavior / macro /
 *     insurance 的 __title）由 CSS 精确隐藏（只隐藏标题，保留 __monthly 等信息行）。
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import type { VirtualCityMarketPoint, VirtualCityPanelTab } from '@/store/virtualCity.store';
import {
  fetchRealtimeBoard,
  fetchVirtualCityDashboard,
  refreshRealtimeBoard,
  type VirtualCityDashboardSnapshot,
} from '@/api/virtualCity';
import type {
  VirtualCityAction,
  VirtualCityDistrictId,
  VirtualCityGameState,
  VirtualCityTradeAction,
} from '@/types/virtualCity';
import { FinancialPanel } from '@/components/virtualCity/FinancialPanel';
import { MarketPanel } from '@/components/virtualCity/MarketPanel';
import { LedgerPanel } from '@/components/virtualCity/LedgerPanel';
import { EconomyPanelWithSurvey } from '@/components/virtualCity/EconomyPanelWithSurvey';
import { BehaviorPanel } from '@/components/virtualCity/BehaviorPanel';
import { MacroPanel } from '@/components/virtualCity/MacroPanel';
import { InsurancePanel } from '@/components/virtualCity/InsurancePanel';
import { MarketTradePanel } from '@/components/virtualCity/MarketTradePanel';
import { RealtimeBoard } from './RealtimeBoard';
import { BoardSection } from './BoardSection';
import { CityOverviewPanel } from './CityOverviewPanel';
import { DataSourcesPanel } from './DataSourcesPanel';
import { CalibrationPanel } from './CalibrationPanel';
import {
  DASHBOARD_CATALOG,
  DASHBOARD_GROUP_IDS,
  DASHBOARD_GROUPS,
  type DashboardDimension,
  type DashboardGroupId,
} from './dashboardCatalog';
import { timeAgoKey, timeAgoParts } from './boardUtils';

export interface VirtualCityDashboardProps {
  /** 房间 id（MarketTradePanel / 调研面板需要）。 */
  roomId: string;
  /** game.state 快照。 */
  gameState: VirtualCityGameState | null;
  /** 每月一条的市场快照（MarketPanel 走势图用）。 */
  marketHistory: VirtualCityMarketPoint[];
  /** 本人座位号（-1 = 观战）。 */
  mySeat: number;
  /** 观战态（隐藏全部写操作入口）。 */
  spectator: boolean;
  /** 发送 game.virtual_city_action。 */
  sendAction: (action: VirtualCityAction) => void;
  /** 发送 game.virtual_city_trade。 */
  sendTrade: (action: VirtualCityTradeAction) => void;
  /** 选区回调（MarketPanel 房价条）。 */
  onSelectDistrict: (id: VirtualCityDistrictId) => void;
}

/** 1 分钟粒度重渲染「更新于 N 分钟前」。 */
function useMinuteTick(): number {
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));
  useEffect(() => {
    const timer = setInterval(() => setNow(Math.floor(Date.now() / 1000)), 30_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

export function VirtualCityDashboard({
  roomId,
  gameState,
  marketHistory,
  mySeat,
  spectator,
  sendAction,
  sendTrade,
  onSelectDistrict,
}: VirtualCityDashboardProps) {
  const t = useT();
  const panelTab = useVirtualCityStore((s) => s.panelTab);
  const setPanelTab = useVirtualCityStore((s) => s.setPanelTab);
  const feed = useVirtualCityStore((s) => s.realtimeFeed);
  const setRealtimeFeed = useVirtualCityStore((s) => s.setRealtimeFeed);
  const setRealtimeMissions = useVirtualCityStore((s) => s.setRealtimeMissions);
  // 批次78：采集健康度聚合（stats 与 missions 一样只随 HTTP 快照回读）。
  const setRealtimeStats = useVirtualCityStore((s) => s.setRealtimeStats);

  const [snapshot, setSnapshot] = useState<VirtualCityDashboardSnapshot | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  /** 折叠状态：按维度 id 记忆（切换 Tab 后回来不丢）。 */
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const nowSec = useMinuteTick();

  // ── 首屏快照：看板目录 + 实时看板 feed ────────────────────────────────
  useEffect(() => {
    let cancelled = false;
    // ① 看板目录（dimensions / data_sources）。失败不致命 —— 静态目录兜底。
    fetchVirtualCityDashboard()
      .then((r) => {
        if (cancelled) return;
        setSnapshot(r?.dashboard ?? null);
        const rt = r?.dashboard?.realtime ?? null;
        // ⚠️ store 已有 feed（WS 帧带来的）时**不要**用快照覆盖 ——
        //    离线快照会把 WS 数据冲掉。
        if (rt && !useVirtualCityStore.getState().realtimeFeed) {
          setRealtimeFeed(rt);
        }
      })
      .catch(() => {
        // 目录不可达 → 静态目录兜底（Tab 仍全量渲染）；不弹错（首屏拉取非操作错误）。
        if (!cancelled) setSnapshot(null);
      });
    // ② 实时看板快照（late-joiner 补齐 game.realtime_feed 帧间隙）。
    //    批次77：missions 不进 WS 帧，快照拿到就整表替换（无记录为 []）。
    //    批次78：stats 同理（无记录为 null）。
    fetchRealtimeBoard()
      .then((r) => {
        if (cancelled) return;
        const f = r?.feed ?? null;
        // 只在拿到数据时写入 —— 失败**不置空**，避免冲掉已有（WS / 看板快照）数据。
        if (f) setRealtimeFeed(f);
        setRealtimeMissions(r.missions ?? []);
        setRealtimeStats(r.stats ?? null);
      })
      .catch(() => {
        // best-effort：WS 帧会补齐；不置空以免冲掉已有数据。
      });
    return () => {
      cancelled = true;
    };
  }, [setRealtimeFeed, setRealtimeMissions, setRealtimeStats]);

  // ── 目录合并：静态目录为权威，后端目录按 id 覆盖 title_key / group ──────
  const dimensions = useMemo<DashboardDimension[]>(() => {
    const backend = new Map(
      (snapshot?.dimensions ?? []).map((d) => [d.id, d] as const),
    );
    return DASHBOARD_CATALOG.map((d) => {
      const b = backend.get(d.id);
      if (!b) return d;
      // 只接受已知分组 id（后端目录是外部输入 ⇒ 必须收敛到联合类型）。
      const g = b.group as DashboardGroupId;
      return {
        ...d,
        titleKey: b.title_key || d.titleKey,
        group: DASHBOARD_GROUP_IDS.includes(g) ? g : d.group,
      };
    });
  }, [snapshot]);

  const activeDim = dimensions.find((d) => d.id === panelTab) ?? dimensions[0];
  /** 当前维度所属分类（派生，不另存 state ⇒ 与 store.panelTab 永远一致）。 */
  const activeGroup: DashboardGroupId = activeDim?.group ?? 'city';
  /** 当前分类下的维度（第二级 Tab 行）。 */
  const groupDims = useMemo(
    () => dimensions.filter((d) => d.group === activeGroup),
    [dimensions, activeGroup],
  );

  /** 切换分类：若当前维度不在新分类下，自动切到该分类的第一个维度。 */
  const handleGroupClick = useCallback(
    (g: DashboardGroupId) => {
      const first = dimensions.find((d) => d.group === g);
      if (!first) return;
      if (activeDim?.group !== g) setPanelTab(first.id);
    },
    [activeDim, dimensions, setPanelTab],
  );

  const status = refreshing ? 'updating' : (feed?.channel.status ?? 'offline');
  const ago = feed ? timeAgoParts(feed.updated_at, nowSec) : null;
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

  const handleRefresh = useCallback(async () => {
    if (refreshing) return;
    setRefreshing(true);
    setError(null);
    try {
      await refreshRealtimeBoard();
      // 快照不回读：mission 是异步的，数据经 game.realtime_feed 帧落库；
      // 拉一次快照让 accepted 后的通道状态即时可见（失败静默，等 WS 帧）。
      // 批次77：missions 不进 WS 帧，一并随快照回读（无记录为 []）。
      // 批次78：stats 同理（无记录为 null）。
      try {
        const r = await fetchRealtimeBoard();
        const f = r?.feed ?? null;
        if (f) setRealtimeFeed(f);
        setRealtimeMissions(r.missions ?? []);
        setRealtimeStats(r.stats ?? null);
      } catch {
        // best-effort：WS 帧会补齐
      }
    } catch (e) {
      // §7.1：看板内联红条展示，不吞进 console。
      const msg = e instanceof Error ? e.message : String(e);
      setError(t('virtualCity.dash.refreshFail' as TKey, { msg }));
    } finally {
      setRefreshing(false);
    }
  }, [refreshing, setRealtimeFeed, setRealtimeMissions, setRealtimeStats, t]);

  const toggleCollapsed = useCallback((id: string) => {
    setCollapsed((prev) => ({ ...prev, [id]: !prev[id] }));
  }, []);

  // ── 切换维度后把面板体滚到可视区 ─────────────────────────────────────
  // 看板整体位于侧栏滚动容器内；Tab 条在顶部、面板体在下方，切换维度时若
  // 停留在原滚动位置，屏上看到的可能还是上一维度的内容。⇒ 切维度后把
  // 维度标题条滚到滚动视口顶部（首帧不滚动，避免一进房就把标题顶掉）。
  const sectionRef = useRef<HTMLDivElement | null>(null);
  const firstRenderRef = useRef(true);
  useEffect(() => {
    if (firstRenderRef.current) {
      firstRenderRef.current = false;
      return;
    }
    sectionRef.current?.scrollIntoView({ block: 'start', behavior: 'smooth' });
  }, [panelTab]);

  // 只读态口径与改造前逐字一致：观战 **或** 服务端视角无本人座位（gameState.my_seat < 0）。
  const readOnly = spectator || (gameState?.my_seat ?? -1) < 0;

  // ── 维度面板体 ────────────────────────────────────────────────────────
  const renderPanel = (id: VirtualCityPanelTab) => {
    switch (id) {
      case 'city-overview':
        return <CityOverviewPanel gameState={gameState} />;
      case 'finance':
        return <FinancialPanel gameState={gameState} />;
      case 'ledger':
        return <LedgerPanel gameState={gameState} />;
      case 'economy':
        return (
          <EconomyPanelWithSurvey
            gameState={gameState}
            roomId={roomId}
            onFamilyAction={sendAction}
            spectator={readOnly}
          />
        );
      case 'behavior':
        return (
          <BehaviorPanel
            behavior={gameState?.my?.behavior ?? null}
            spectator={readOnly}
            onAction={sendAction}
          />
        );
      case 'macro':
        return (
          <MacroPanel
            bubble={gameState?.bubble ?? null}
            spreads={gameState?.liquidity_spreads ?? null}
          />
        );
      case 'market':
        return (
          <MarketPanel
            gameState={gameState}
            marketHistory={marketHistory}
            onSelectDistrict={onSelectDistrict}
          />
        );
      case 'market-trade':
        return (
          <MarketTradePanel
            roomId={roomId}
            gameState={gameState}
            mySeat={mySeat}
            my={gameState?.my ?? null}
            sendTrade={sendTrade}
          />
        );
      case 'insurance':
        return (
          <InsurancePanel
            insurance={gameState?.my?.insurance ?? null}
            myCash={gameState?.my?.cash ?? 0}
            spectator={readOnly}
            onAction={sendAction}
          />
        );
      case 'realtime':
        return <RealtimeBoard />;
      case 'datasources':
        return <DataSourcesPanel sources={snapshot?.data_sources ?? []} />;
      case 'calibration':
        return <CalibrationPanel gameState={gameState} />;
      default:
        return null;
    }
  };

  const schemaVersion = snapshot?.schema_version ?? 1;

  return (
    <div className="virtualCity-dash" data-testid="virtualCity-dash">
      {/* ① 看板标题条 + ② 实时通道徽章/更新时间/刷新
          （两条合并到同一视觉带：左侧标题 + 副行，右侧通道区 —— 380px 侧栏省一行高度） */}
      <div className="virtualCity-dash__head">
        <div className="virtualCity-dash__head-left">
          <div className="virtualCity-dash__title">{t('virtualCity.dash.title' as TKey)}</div>
          <div className="virtualCity-dash__meta">
            {t('virtualCity.dash.meta' as TKey, {
              v: schemaVersion,
              n: dimensions.length,
            })}
          </div>
        </div>
        <div className="virtualCity-dash__channel">
          <span className={badgeClass} data-testid="virtualCity-dash-channel">
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
            data-testid="virtualCity-dash-refresh"
          >
            <span className={refreshing ? 'virtualCity-rt__spin' : ''} aria-hidden="true">⟳</span>
            {' '}{t('virtualCity.rt.refresh' as TKey)}
          </button>
        </div>
      </div>

      {/* §7.1：刷新失败 / 目录不可用 —— 看板内联红条。 */}
      {error && (
        <div className="virtualCity-dash__error" role="alert">
          <span>⚠️ {error}</span>
          <button type="button" onClick={() => setError(null)} aria-label="dismiss">×</button>
        </div>
      )}

      {/* ③ 两级 Tab 导航（sticky 容器）：
          第一级 = 分类选择器（城市运行 / 市场金融 / 外部实时，3 等分单行）；
          第二级 = 当前分类下的维度按钮（横向 wrap）。
          粘性定位的滚动容器是 .virtualCity-sidebar（它已有 overflow-y:auto）——
          看板必须是它的**直接子元素**（中间夹一层 overflow:auto 会让 sticky 失效）。 */}
      <div className="virtualCity-dash__nav">
        <div className="virtualCity-dash__groups" role="tablist">
          {DASHBOARD_GROUPS.map((g) => (
            <button
              key={g.id}
              type="button"
              className={
                'virtualCity-dash__group-btn' +
                (activeGroup === g.id ? ' virtualCity-dash__group-btn--active' : '')
              }
              onClick={() => handleGroupClick(g.id)}
              aria-pressed={activeGroup === g.id}
              title={t(g.labelKey as TKey)}
              data-testid={`virtualCity-dash-group-${g.id}`}
            >
              {t(g.labelKey as TKey)}
            </button>
          ))}
        </div>
        <div className="virtualCity-dash__dims">
          {groupDims.map((d) => (
            <button
              key={d.id}
              type="button"
              className={
                'virtualCity-tabs__btn' +
                (panelTab === d.id ? ' virtualCity-tabs__btn--active' : '')
              }
              onClick={() => setPanelTab(d.id)}
              title={t(d.titleKey as TKey)}
              aria-current={panelTab === d.id}
              data-testid={`virtualCity-dash-tab-${d.id}`}
              /* CDP 实机验收钩子：稳定 id（不随 i18n 语言变化）。 */
              data-vc-tab={d.id}
            >
              {t(d.titleKey as TKey)}
            </button>
          ))}
        </div>
      </div>

      {/* ④ 当前维度面板（外层 BoardSection：统一标题条 + 折叠） */}
      {activeDim && (
        <div className="virtualCity-dash__body" ref={sectionRef}>
          <BoardSection
            title={t(activeDim.titleKey as TKey)}
            collapsed={!!collapsed[activeDim.id]}
            onToggle={() => toggleCollapsed(activeDim.id)}
          >
            {renderPanel(activeDim.id)}
          </BoardSection>
        </div>
      )}
    </div>
  );
}

export default VirtualCityDashboard;
