/**
 * 虚拟城市 — 对局页（/virtual-city/:roomId 与 /virtual-city/spectate/:roomId）。
 *
 * 布局（产品设计 §6.1 线框）：
 *   顶部信息栏（月 / 年龄 / 周期徽章 / 运行时钟 / 我的座位 / 提前开始 / 离开）
 *   ├─ 左主区：2.5D 城市地图（VirtualCityCityMap）+ 左上角小地图叠加
 *   ├─ 右侧栏：面板 Tab（财务 / 行情 / 流水）+ Agent 思维 + 房间聊天
 *   ├─ 底部动作条（ActionPanel，仅人类座位）
 *   └─ 事件流（MonthTicker）
 *
 * 断线恢复：mount 时 game.join/game.spectate（重试直至 WS OPEN）+ 8s 轮询
 * game.state（仿 DoudizhuGamePage）；观战路由 useSpectatorMode() 隐藏动作条。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { selectSeatedCount, selectSeatCapacity, selectSeatsReady } from '@/store/virtualCity.store';
import { useVirtualCity } from '@/hooks/useVirtualCity';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { wsClient } from '@/services/ws';
import { roomService } from '@/services/auth.service';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { districtCenter, formatPct, type VirtualCityDistrictId } from '@/types/virtualCity';
import {
  VirtualCityCityMap,
  type VirtualCityCameraView,
  type VirtualCityFocusTarget,
} from '@/components/virtualCity/VirtualCityCityMap';
import { VirtualCityMinimap } from '@/components/virtualCity/VirtualCityMinimap';
import { FinancialPanel } from '@/components/virtualCity/FinancialPanel';
import { MarketPanel } from '@/components/virtualCity/MarketPanel';
import { LedgerPanel } from '@/components/virtualCity/LedgerPanel';
import { EconomyPanel } from '@/components/virtualCity/EconomyPanel';
import { SurveyPanel } from '@/components/virtualCity/SurveyPanel';
import { ActionPanel } from '@/components/virtualCity/ActionPanel';
import { MonthTicker } from '@/components/virtualCity/MonthTicker';
import { MarketTradePanel } from '@/components/virtualCity/MarketTradePanel';
import { InsurancePanel } from '@/components/virtualCity/InsurancePanel';
import { GameOverModal } from '@/components/virtualCity/GameOverModal';
import { CityStatsPanel } from '@/components/virtualCity/CityStatsPanel';
import { CivicElectionBanner } from '@/components/virtualCity/CivicElectionBanner';
import { VirtualCityBotPanel } from '@/components/virtualCity/VirtualCityBotPanel';
import { VirtualCityGameChatPanel } from '@/components/virtualCity/VirtualCityGameChatPanel';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

const CYCLE_CLASS: Record<string, string> = {
  recovery: 'virtualCity-cycle--recovery',
  boom: 'virtualCity-cycle--boom',
  recession: 'virtualCity-cycle--recession',
  depression: 'virtualCity-cycle--depression',
};

function fmtElapsed(startedAtSec: number, nowMs: number): string {
  if (!startedAtSec) return '--:--';
  const s = Math.max(0, Math.floor(nowMs / 1000) - startedAtSec);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const p = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${h}:${p(m)}:${p(sec)}` : `${p(m)}:${p(sec)}`;
}

/**
 * 阶段 Q：经济 Tab 内含调研入口 —— EconomyPanel + 顶部「📋 调研」小按钮，
 * 点击触发 SurveyPanel 模态。避免占独立 Tab。
 */
function EconomyPanelWithSurvey({
  gameState,
  roomId,
}: {
  gameState: import('@/types/virtualCity').VirtualCityGameState | null;
  roomId: string;
}) {
  const [surveyOpen, setSurveyOpen] = useState(false);
  return (
    <div className="virtualCity-economy-wrap">
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', marginBottom: 4 }}>
        <button
          type="button"
          className="virtualCity-tab-extra-btn"
          onClick={() => setSurveyOpen(true)}
          aria-label="打开调研"
        >
          📋 调研
        </button>
      </div>
      <EconomyPanel gameState={gameState} />
      {surveyOpen && (
        <div
          className="virtualCity-modal-overlay"
          role="dialog"
          aria-modal="true"
          onClick={() => setSurveyOpen(false)}
        >
          {/* 18/04 AB-2：改统一 virtualCity-modal 三段结构（head sticky / body 内滚），
              去掉内联 maxWidth/maxHeight —— 尺寸契约收口到 virtualCity-city3d.css。 */}
          <div
            className="virtualCity-modal"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="virtualCity-modal__head">
              <strong>📋 调研</strong>
              <button
                type="button"
                className="virtualCity-tab-extra-btn"
                onClick={() => setSurveyOpen(false)}
              >
                ✕ 关闭
              </button>
            </div>
            <div className="virtualCity-modal__body">
              <SurveyPanel roomId={roomId} gameState={gameState} />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export function VirtualCityGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const t = useT();
  const spectator = useSpectatorMode();

  const gameState = useVirtualCityStore((s) => s.gameState);
  const mySeat = useVirtualCityStore((s) => s.mySeat);
  const eventFeed = useVirtualCityStore((s) => s.eventFeed);
  const monthFrames = useVirtualCityStore((s) => s.monthFrames);
  const marketHistory = useVirtualCityStore((s) => s.marketHistory);
  const gameOver = useVirtualCityStore((s) => s.gameOver);
  const lastError = useVirtualCityStore((s) => s.lastError);
  const setLastError = useVirtualCityStore((s) => s.setLastError);
  const panelTab = useVirtualCityStore((s) => s.panelTab);
  const setPanelTab = useVirtualCityStore((s) => s.setPanelTab);
  const selectedDistrict = useVirtualCityStore((s) => s.selectedDistrict);
  const setSelectedDistrict = useVirtualCityStore((s) => s.setSelectedDistrict);
  const reset = useVirtualCityStore((s) => s.reset);
  // 2026-09-16 §财商流10–12座位 — 座位占用三件套（返回原语 → 不引发多余重渲染；
  // 实现见 store/virtualCity.store.ts 对应的 selectSeatedCount / selectSeatCapacity /
  // selectSeatsReady）。必须在任何 early-return 之前调用以满足 React Rules of Hooks。
  const seatedCount = useVirtualCityStore(selectSeatedCount);
  const seatCapacity = useVirtualCityStore(selectSeatCapacity);
  const seatsReady = useVirtualCityStore(selectSeatsReady);

  const {
    spectate, unspectate, leaveGame, requestState, sendAction, sendTrade, startEarly,
  } = useVirtualCity(roomId ?? '');

  const [leavePromptOpen, setLeavePromptOpen] = useState(false);
  const [now, setNow] = useState(Date.now());
  const viewRef = useRef<VirtualCityCameraView>({ x: 0, z: 0, dist: 34 });
  const focusRef = useRef<VirtualCityFocusTarget | null>(null);

  // 运行时钟（game_started_at，RoomRunningClock 同源语义）。
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  // 入场：join / spectate（WS 未 OPEN 时 500ms 重试）+ 8s 轮询全量快照。
  useEffect(() => {
    if (!roomId) return;
    reset();
    let retries = 0;
    const tryHook = () => {
      if (retries++ > 10) return;
      const frame = spectator ? 'game.spectate' : 'game.join';
      const sent = wsClient.send(frame, { room_id: roomId, game_kind: 'virtual_city' });
      if (sent === false) setTimeout(tryHook, 500);
    };
    const timer = setTimeout(tryHook, 300);
    const stateTimer = setInterval(() => requestState(), 8000);
    return () => {
      clearTimeout(timer);
      clearInterval(stateTimer);
      if (spectator) unspectate();
    };
  }, [roomId, spectator, spectate, unspectate, requestState, reset]);

  const handleSelectDistrict = useCallback(
    (id: VirtualCityDistrictId) => {
      setSelectedDistrict(id);
      // 聚焦该区中心（DistrictDefs 坐标）。
      const c = districtCenter(id);
      focusRef.current = { x: c.x, z: c.z };
    },
    [setSelectedDistrict],
  );

  const handleLeave = useCallback(async () => {
    if (!roomId) return;
    if (spectator) {
      unspectate();
      try {
        await roomService.leaveSpectate(roomId);
      } catch {
        // best-effort
      }
    } else {
      leaveGame();
      try {
        await roomService.leave(roomId);
      } catch {
        // best-effort
      }
    }
    reset();
    nav('/virtual-city');
  }, [roomId, spectator, unspectate, leaveGame, reset, nav]);

  if (!roomId) {
    return <div className="error">Missing room ID</div>;
  }

  const effectiveSeat = spectator ? -1 : mySeat;
  const canStartEarly =
    !spectator &&
    gameState?.status === 'open' &&
    effectiveSeat >= 0 &&
    seatsReady;
  const waitingForSeats =
    gameState?.status === 'open' && !seatsReady;

  // 13-3D城市渲染优化 · 阶段 E：9 Tab 按语义分两组（数据/交易），组内 wrap 防溢出。
  // 15-3D城市全面真实感深化 · 阶段 Q：合并 Tab 9 → 6（economy + survey → economy 含调研入口；
  // listing + loan + infomarket → market-trade 含子导航）。
  type TabGroup = 'data' | 'trade';
  type PanelTab = 'finance' | 'market' | 'ledger' | 'economy' | 'market-trade' | 'insurance';
  // 同步 store 类型（局部重新声明避免扩大 store.ts 类型）
  const tabs: { key: PanelTab; label: string; group: TabGroup }[] = [
    { key: 'finance', label: t('virtualCity.tab.finance' as TKey), group: 'data' },
    { key: 'market', label: t('virtualCity.tab.market' as TKey), group: 'data' },
    { key: 'ledger', label: t('virtualCity.tab.ledger' as TKey), group: 'data' },
    { key: 'economy', label: `📊 ${t('virtualCity.tab.economy' as TKey)}`, group: 'data' },
    { key: 'market-trade', label: `💼 市场`, group: 'trade' },
    { key: 'insurance', label: `🛡 ${t('virtualCity.tab.insurance' as TKey)}`, group: 'trade' },
  ];
  const tabGroups: { key: TabGroup; labelKey: TKey }[] = [
    { key: 'data', labelKey: 'virtualCity.tabgroup.data' as TKey },
    { key: 'trade', labelKey: 'virtualCity.tabgroup.trade' as TKey },
  ];

  // ── 批次 20 文档 3 A4：市长选举横幅 / 政务数据（纯派生值，不走 hook —— 位于
  //    上方 if (!roomId) 早退之后，加 hook 会违反 Rules of Hooks）。──
  const election = gameState?.public_services ?? null;
  const mayorSeat = election?.election_enabled ? (election.mayor_seat ?? -1) : -1;
  const mayorNickname = mayorSeat >= 0
    ? gameState?.players.find((p) => p.seat === mayorSeat)?.nickname
    : undefined;
  // 津贴停发徽标：已改由 CivicElectionBanner 直接消费 public_services.stipend_stopped
  // （旧「当月 policy 事件中文文本探测」best-effort 逻辑已删除）。

  return (
    <div className="virtualCity-root virtualCity-game">
      {/* 顶部信息栏 */}
      <header className="virtualCity-topbar">
        <div className="virtualCity-topbar__title">
          🏙 {t('virtualCity.title' as TKey)}
          <small className="virtualCity-topbar__room">#{roomId.slice(0, 8)}</small>
          {spectator && <span className="virtualCity-badge virtualCity-badge--spectator">👁 {t('virtualCity.spectate' as TKey)}</span>}
        </div>
        {gameState && (
          <>
            <span className="virtualCity-topbar__item">
              {t('virtualCity.month' as TKey, { m: gameState.month })}
            </span>
            <span className="virtualCity-topbar__item">
              {t('virtualCity.age' as TKey, { a: gameState.age })}
            </span>
            <span className={`virtualCity-badge virtualCity-cycle ${CYCLE_CLASS[gameState.cycle.phase] ?? ''}`}>
              {t(`virtualCity.cycle.${gameState.cycle.phase}` as TKey)} · LPR {formatPct(gameState.cycle.lpr)}
            </span>
            {gameState.phase === 'settling' && (
              <span className="virtualCity-badge virtualCity-phase-badge--settling">
                {t('virtualCity.phase.settling' as TKey)}
              </span>
            )}
            <span className="virtualCity-topbar__item" title={t('virtualCity.runningTime' as TKey)}>
              ⏱ {fmtElapsed(gameState.game_started_at, now)}
            </span>
            <span
              className="virtualCity-topbar__item"
              title={t('virtualCity.seatsCount' as TKey, { n: seatedCount, max: seatCapacity })}
              data-testid="virtualCity-seats-count"
            >
              🤖 {t('virtualCity.seatsCount' as TKey, { n: seatedCount, max: seatCapacity })}
            </span>
            {effectiveSeat >= 0 && (
              <span className="virtualCity-topbar__item">
                {t('virtualCity.mySeat' as TKey, { n: effectiveSeat + 1 })}
              </span>
            )}
          </>
        )}
        <div className="virtualCity-topbar__actions">
          {canStartEarly && (
            <button
              type="button"
              className="btn btn-secondary"
              onClick={startEarly}
              title={t('virtualCity.startHint' as TKey)}
              data-testid="virtualCity-start-early"
            >
              ▶ {t('virtualCity.startEarly' as TKey)}
            </button>
          )}
          <button
            type="button"
            className="btn btn-danger"
            onClick={() => setLeavePromptOpen(true)}
          >
            ⏏ {t('virtualCity.leaveRoom' as TKey)}
          </button>
        </div>
      </header>

      {/* 批次 20 文档 3 A4：当选横幅（顶栏下方；未启用/无市长整条不渲染，可关闭按届重现） */}
      <CivicElectionBanner
        election={election}
        month={gameState?.month ?? 0}
        mayorNickname={mayorNickname}
      />

      {/* 就地错误 banner（§7.1 双通道之二；全局 toast 由 useVirtualCity 上报） */}
      {lastError && (
        <div className="virtualCity-error-banner" role="alert">
          <span>⚠️ [{lastError.code}] {lastError.message}</span>
          <button type="button" onClick={() => setLastError(null)} aria-label="dismiss">×</button>
        </div>
      )}

      {/* 抽样展示位未就绪提示（MinSeats=10）：等待 12 抽样居民 Agent 注册完成 */}
      {waitingForSeats && (
        <div className="virtualCity-seats-hint" role="status" data-testid="virtualCity-seats-hint">
          {t('virtualCity.seatsWaiting' as TKey)}
        </div>
      )}

      {/* 主体：地图 + 侧栏 */}
      <div className="virtualCity-main">
        <div className="virtualCity-map-area">
          {gameState ? (
            <>
              <VirtualCityCityMap
                gameState={gameState}
                viewRef={viewRef}
                focusRef={focusRef}
                selectedDistrict={selectedDistrict}
                onSelectDistrict={handleSelectDistrict}
              />
              <VirtualCityMinimap
                gameState={gameState}
                viewRef={viewRef}
                selectedDistrict={selectedDistrict}
                onSelectDistrict={handleSelectDistrict}
              />
            </>
          ) : (
            <div className="waiting-board">
              <p>{t(spectator ? ('virtualCity.spectating' as TKey) : ('virtualCity.waiting' as TKey))}</p>
              <div className="spinner" />
            </div>
          )}
        </div>

        <aside className="virtualCity-sidebar">
          <div className="virtualCity-tabs virtualCity-tabs--panel">
            {tabGroups.map((g) => (
              <div className="virtualCity-tabs__group" key={g.key}>
                <span className="virtualCity-tabs__group-label">{t(g.labelKey)}</span>
                <div className="virtualCity-tabs__group-btns">
                  {tabs
                    .filter((x) => x.group === g.key)
                    .map((x) => (
                      <button
                        key={x.key}
                        type="button"
                        className={'virtualCity-tabs__btn' + (panelTab === x.key ? ' virtualCity-tabs__btn--active' : '')}
                        onClick={() => setPanelTab(x.key)}
                      >
                        {x.label}
                      </button>
                    ))}
                </div>
              </div>
            ))}
          </div>
          <div className="virtualCity-sidebar__panels">
            {panelTab === 'finance' && <FinancialPanel gameState={gameState} />}
            {panelTab === 'market' && (
              <MarketPanel
                gameState={gameState}
                marketHistory={marketHistory}
                onSelectDistrict={handleSelectDistrict}
              />
            )}
            {panelTab === 'ledger' && <LedgerPanel gameState={gameState} />}
            {/* 阶段 Q：经济 Tab 内含调研入口（EconomyPanel 顶部小按钮触发模态） */}
            {panelTab === 'economy' && (
              <EconomyPanelWithSurvey
                gameState={gameState}
                roomId={roomId}
              />
            )}
            {/* 阶段 Q：市场 Tab 聚合 listing / loan / infomarket 三个子面板 */}
            {panelTab === 'market-trade' && (
              <MarketTradePanel
                roomId={roomId}
                gameState={gameState}
                mySeat={effectiveSeat}
                my={gameState?.my ?? null}
                sendTrade={sendTrade}
              />
            )}
            {/* P1 第四期：商业保险（my.insurance 驱动；观战 / 全 Agent 模式只读）。 */}
            {panelTab === 'insurance' && (
              <InsurancePanel
                insurance={gameState?.my?.insurance ?? null}
                myCash={gameState?.my?.cash ?? 0}
                spectator={spectator || (gameState?.my_seat ?? -1) < 0}
                onAction={sendAction}
              />
            )}
          </div>
          {gameState && !spectator && gameState.my_seat < 0 && (
            <div className="virtualCity-join-hint">{t('virtualCity.joinHint' as TKey)}</div>
          )}
          {/* §20260921 城市背景层 — city 缺省（旧房）时整面板不渲染；
              roomId 供居民档案抽屉（档案锚定设计 §8.3）拉取 REST。 */}
          <CityStatsPanel
            city={gameState?.city}
            roomId={roomId}
            election={election}
            players={gameState?.players}
          />
          <VirtualCityBotPanel
            botContexts={gameState?.bot_contexts ?? []}
            players={gameState?.players ?? []}
          />
          <VirtualCityGameChatPanel roomId={roomId} gameState={gameState} currentMonth={gameState?.month ?? null} />
        </aside>
      </div>

      {/* 底部：动作条（人类座位）+ 事件流 */}
      {!spectator && (
        <ActionPanel
          roomId={roomId}
          gameState={gameState}
          mySeat={effectiveSeat}
          sendAction={sendAction}
          onTradeTab={(tab) => setPanelTab(tab)}
        />
      )}
      <MonthTicker
        gameState={gameState}
        eventFeed={eventFeed}
        lastMonth={monthFrames.length > 0 ? monthFrames[monthFrames.length - 1] : null}
      />

      {/* 终局结算 */}
      {gameOver && (
        <GameOverModal
          over={gameOver}
          gameState={gameState}
          monthFrames={monthFrames}
          mySeat={effectiveSeat}
          onViewLedger={() => setPanelTab('ledger')}
        />
      )}

      {leavePromptOpen && (
        <ConfirmModal
          messageKey={'virtualCity.confirmLeave' as TKey}
          danger
          onConfirm={() => {
            setLeavePromptOpen(false);
            void handleLeave();
          }}
          onCancel={() => setLeavePromptOpen(false)}
        />
      )}
    </div>
  );
}
