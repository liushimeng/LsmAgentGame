/**
 * 财商流游戏 — 对局页（/wealth/:roomId 与 /wealth/spectate/:roomId）。
 *
 * 布局（产品设计 §6.1 线框）：
 *   顶部信息栏（月 / 年龄 / 周期徽章 / 运行时钟 / 我的座位 / 提前开始 / 离开）
 *   ├─ 左主区：2.5D 城市地图（WealthCityMap）+ 左上角小地图叠加
 *   ├─ 右侧栏：面板 Tab（财务 / 行情 / 流水）+ Agent 思维 + 房间聊天
 *   ├─ 底部动作条（ActionPanel，仅人类座位）
 *   └─ 事件流（MonthTicker）
 *
 * 断线恢复：mount 时 game.join/game.spectate（重试直至 WS OPEN）+ 8s 轮询
 * game.state（仿 DoudizhuGamePage）；观战路由 useSpectatorMode() 隐藏动作条。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useWealthStore } from '@/store/wealth.store';
import { useWealth } from '@/hooks/useWealth';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { wsClient } from '@/services/ws';
import { roomService } from '@/services/auth.service';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { districtCenter, formatPct, type WealthDistrictId } from '@/types/wealth';
import {
  WealthCityMap,
  type WealthCameraView,
  type WealthFocusTarget,
} from '@/components/wealth/WealthCityMap';
import { WealthMinimap } from '@/components/wealth/WealthMinimap';
import { FinancialPanel } from '@/components/wealth/FinancialPanel';
import { MarketPanel } from '@/components/wealth/MarketPanel';
import { LedgerPanel } from '@/components/wealth/LedgerPanel';
import { ActionPanel } from '@/components/wealth/ActionPanel';
import { MonthTicker } from '@/components/wealth/MonthTicker';
import { GameOverModal } from '@/components/wealth/GameOverModal';
import { WealthBotPanel } from '@/components/wealth/WealthBotPanel';
import { WealthGameChatPanel } from '@/components/wealth/WealthGameChatPanel';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

const CYCLE_CLASS: Record<string, string> = {
  recovery: 'wealth-cycle--recovery',
  boom: 'wealth-cycle--boom',
  recession: 'wealth-cycle--recession',
  depression: 'wealth-cycle--depression',
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

export function WealthGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const t = useT();
  const spectator = useSpectatorMode();

  const gameState = useWealthStore((s) => s.gameState);
  const mySeat = useWealthStore((s) => s.mySeat);
  const eventFeed = useWealthStore((s) => s.eventFeed);
  const monthFrames = useWealthStore((s) => s.monthFrames);
  const marketHistory = useWealthStore((s) => s.marketHistory);
  const gameOver = useWealthStore((s) => s.gameOver);
  const lastError = useWealthStore((s) => s.lastError);
  const setLastError = useWealthStore((s) => s.setLastError);
  const panelTab = useWealthStore((s) => s.panelTab);
  const setPanelTab = useWealthStore((s) => s.setPanelTab);
  const selectedDistrict = useWealthStore((s) => s.selectedDistrict);
  const setSelectedDistrict = useWealthStore((s) => s.setSelectedDistrict);
  const reset = useWealthStore((s) => s.reset);

  const {
    spectate, unspectate, leaveGame, requestState, sendAction, startEarly,
  } = useWealth(roomId ?? '');

  const [leavePromptOpen, setLeavePromptOpen] = useState(false);
  const [now, setNow] = useState(Date.now());
  const viewRef = useRef<WealthCameraView>({ x: 0, z: 0, dist: 34 });
  const focusRef = useRef<WealthFocusTarget | null>(null);

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
      const sent = wsClient.send(frame, { room_id: roomId, game_kind: 'wealth' });
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
    (id: WealthDistrictId) => {
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
    nav('/wealth');
  }, [roomId, spectator, unspectate, leaveGame, reset, nav]);

  if (!roomId) {
    return <div className="error">Missing room ID</div>;
  }

  const effectiveSeat = spectator ? -1 : mySeat;
  const seatedCount = gameState?.players.length ?? 0;
  const canStartEarly =
    !spectator && gameState?.status === 'open' && effectiveSeat >= 0 && seatedCount >= 3;

  const tabs: { key: typeof panelTab; label: string }[] = [
    { key: 'finance', label: t('wealth.tab.finance' as TKey) },
    { key: 'market', label: t('wealth.tab.market' as TKey) },
    { key: 'ledger', label: t('wealth.tab.ledger' as TKey) },
  ];

  return (
    <div className="wealth-root wealth-game">
      {/* 顶部信息栏 */}
      <header className="wealth-topbar">
        <div className="wealth-topbar__title">
          💰 {t('wealth.title' as TKey)}
          <small className="wealth-topbar__room">#{roomId.slice(0, 8)}</small>
          {spectator && <span className="wealth-badge wealth-badge--spectator">👁 {t('wealth.spectate' as TKey)}</span>}
        </div>
        {gameState && (
          <>
            <span className="wealth-topbar__item">
              {t('wealth.month' as TKey, { m: gameState.month })}
            </span>
            <span className="wealth-topbar__item">
              {t('wealth.age' as TKey, { a: gameState.age })}
            </span>
            <span className={`wealth-badge wealth-cycle ${CYCLE_CLASS[gameState.cycle.phase] ?? ''}`}>
              {t(`wealth.cycle.${gameState.cycle.phase}` as TKey)} · LPR {formatPct(gameState.cycle.lpr)}
            </span>
            {gameState.phase === 'settling' && (
              <span className="wealth-badge wealth-phase-badge--settling">
                {t('wealth.phase.settling' as TKey)}
              </span>
            )}
            <span className="wealth-topbar__item" title={t('wealth.runningTime' as TKey)}>
              ⏱ {fmtElapsed(gameState.game_started_at, now)}
            </span>
            {effectiveSeat >= 0 && (
              <span className="wealth-topbar__item">
                {t('wealth.mySeat' as TKey, { n: effectiveSeat + 1 })}
              </span>
            )}
          </>
        )}
        <div className="wealth-topbar__actions">
          {canStartEarly && (
            <button
              type="button"
              className="btn btn-secondary"
              onClick={startEarly}
              title={t('wealth.startHint' as TKey)}
              data-testid="wealth-start-early"
            >
              ▶ {t('wealth.startEarly' as TKey)}
            </button>
          )}
          <button
            type="button"
            className="btn btn-danger"
            onClick={() => setLeavePromptOpen(true)}
          >
            ⏏ {t('wealth.leaveRoom' as TKey)}
          </button>
        </div>
      </header>

      {/* 就地错误 banner（§7.1 双通道之二；全局 toast 由 useWealth 上报） */}
      {lastError && (
        <div className="wealth-error-banner" role="alert">
          <span>⚠️ [{lastError.code}] {lastError.message}</span>
          <button type="button" onClick={() => setLastError(null)} aria-label="dismiss">×</button>
        </div>
      )}

      {/* 主体：地图 + 侧栏 */}
      <div className="wealth-main">
        <div className="wealth-map-area">
          {gameState ? (
            <>
              <WealthCityMap
                gameState={gameState}
                viewRef={viewRef}
                focusRef={focusRef}
                selectedDistrict={selectedDistrict}
                onSelectDistrict={handleSelectDistrict}
              />
              <WealthMinimap
                gameState={gameState}
                viewRef={viewRef}
                selectedDistrict={selectedDistrict}
                onSelectDistrict={handleSelectDistrict}
              />
            </>
          ) : (
            <div className="waiting-board">
              <p>{t(spectator ? ('wealth.spectating' as TKey) : ('wealth.waiting' as TKey))}</p>
              <div className="spinner" />
            </div>
          )}
        </div>

        <aside className="wealth-sidebar">
          <div className="wealth-tabs wealth-tabs--panel">
            {tabs.map((x) => (
              <button
                key={x.key}
                type="button"
                className={'wealth-tabs__btn' + (panelTab === x.key ? ' wealth-tabs__btn--active' : '')}
                onClick={() => setPanelTab(x.key)}
              >
                {x.label}
              </button>
            ))}
          </div>
          <div className="wealth-sidebar__panels">
            {panelTab === 'finance' && <FinancialPanel gameState={gameState} />}
            {panelTab === 'market' && (
              <MarketPanel
                gameState={gameState}
                marketHistory={marketHistory}
                onSelectDistrict={handleSelectDistrict}
              />
            )}
            {panelTab === 'ledger' && <LedgerPanel gameState={gameState} />}
          </div>
          {gameState && !spectator && gameState.my_seat < 0 && (
            <div className="wealth-join-hint">{t('wealth.joinHint' as TKey)}</div>
          )}
          <WealthBotPanel
            botContexts={gameState?.bot_contexts ?? []}
            players={gameState?.players ?? []}
          />
          <WealthGameChatPanel roomId={roomId} gameState={gameState} currentMonth={gameState?.month ?? null} />
        </aside>
      </div>

      {/* 底部：动作条（人类座位）+ 事件流 */}
      {!spectator && (
        <ActionPanel
          roomId={roomId}
          gameState={gameState}
          mySeat={effectiveSeat}
          sendAction={sendAction}
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
          messageKey={'wealth.confirmLeave' as TKey}
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
