import { useEffect } from 'react';
import { Routes, Route } from 'react-router-dom';
import { useAuth } from './hooks/useAuth';
import { AuthModal } from './components/auth/AuthModal';
import { AppLayout } from './components/layout/AppLayout';
import { HomePage } from './pages/HomePage';
import { GamePage } from './pages/GamePage';
import { GamesPage } from './pages/GamesPage';
import { ProfilePage } from './pages/ProfilePage';
import { AboutPage } from './pages/AboutPage';
import { XiangqiLobbyPage } from './pages/XiangqiLobbyPage';
import { XiangqiGamePage } from './pages/XiangqiGamePage';
import { ChessLobbyPage } from './pages/ChessLobbyPage';
import { ChessGamePage } from './pages/ChessGamePage';
import { JunqiLobbyPage } from './pages/JunqiLobbyPage';
import { JunqiGamePage } from './pages/JunqiGamePage';
import { DoudizhuLobbyPage } from './pages/DoudizhuLobbyPage';
import { DoudizhuGamePage } from './pages/DoudizhuGamePage';
import { WerewolfLobbyPage } from './pages/WerewolfLobbyPage';
import { WerewolfGamePage } from './pages/WerewolfGamePage';
import { DebateLobbyPage } from './pages/DebateLobbyPage';
import { DebateGamePage } from './pages/DebateGamePage';
import { TexasHoldemLobbyPage } from './pages/TexasHoldemLobbyPage';
import { TexasHoldemGamePage } from './pages/TexasHoldemGamePage';
import { VirtualCityLobbyPage } from './pages/VirtualCityLobbyPage';
import { VirtualCityGamePage } from './pages/VirtualCityGamePage';
import { AdminUsersPage } from './pages/AdminUsersPage';
import { ModelAdminPage } from './pages/ModelAdminPage';
import { ModelDetailPage } from './pages/ModelDetailPage';
import { ModelGameLogPage } from './pages/ModelGameLogPage';

export default function App() {
  const isAuthenticated = useAuth((s) => s.isAuthenticated);
  const hasHydrated = useAuth((s) => s.hasHydrated);

  // lsm.auth 损坏（非 JSON，如 '[object Object]'）超时兜底防黑屏 —— 主修复：
  // zustand 4.5.7 persist 在 lsm.auth 非 JSON 时链尾 .catch 吞错且 hasHydrated 永不
  // 置位（node 实测真实 4.5.7：即便 deserialize 返回 null 走成功路径，后置回调虽
  // 执行、setHasHydrated(true) 虽被调用，deprecated options shim 双重包装下该 set
  // 写入仍被覆盖，闸门照旧卡死）。此处 mount 后 ~1.5s 检查一次：正常路径 persist
  // 同步 rehydrate 在模块加载时即完成，远早于此，不会误触发；仅当闸门被卡死时才
  // 清坏 key 并强制抬闸，保证应用按未登录正常启动。必须放在下方 early return
  // 之前（hooks 规则：无条件执行，否则闸门关闭的渲染会少调 hook 直接崩）。
  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (useAuth.getState().hasHydrated) return; // 正常路径：persist 早已抬闸
      try {
        JSON.parse(localStorage.getItem('lsm.auth') || '');
      } catch {
        console.warn('[auth] lsm.auth 损坏且 hydration 卡死，清除坏 key 并强制抬闸');
        localStorage.removeItem('lsm.auth');
      }
      useAuth.getState().setHasHydrated(true);
    }, 1500);
    return () => window.clearTimeout(timer);
  }, []);

  // Avoid rendering the auth-modal/layout mismatch during Zustand persist
  // rehydration. Without this guard, the first paint after F5 can briefly
  // show AuthModal on top of an authenticated layout (or vice-versa),
  // producing the "flash then black" symptom on /profile and every page.
  if (!hasHydrated) {
    return <div className="app-shell" />;
  }

  return (
    <>
      <AppLayout>
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/games" element={<GamesPage />} />
          <Route path="/game/:id" element={<GamePage />} />
          <Route path="/xiangqi" element={<XiangqiLobbyPage />} />
          <Route path="/xiangqi/:roomId" element={<XiangqiGamePage />} />
          <Route path="/xiangqi/spectate/:roomId" element={<XiangqiGamePage />} />
          <Route path="/chess" element={<ChessLobbyPage />} />
          <Route path="/chess/:roomId" element={<ChessGamePage />} />
          <Route path="/chess/spectate/:roomId" element={<ChessGamePage />} />
          <Route path="/junqi" element={<JunqiLobbyPage />} />
          <Route path="/junqi/:roomId" element={<JunqiGamePage />} />
          <Route path="/junqi/spectate/:roomId" element={<JunqiGamePage />} />
          <Route path="/doudizhu" element={<DoudizhuLobbyPage />} />
          <Route path="/doudizhu/:roomId" element={<DoudizhuGamePage />} />
          <Route path="/doudizhu/spectate/:roomId" element={<DoudizhuGamePage />} />
          <Route path="/texasholdem" element={<TexasHoldemLobbyPage />} />
          <Route path="/texasholdem/:roomId" element={<TexasHoldemGamePage />} />
          <Route path="/texasholdem/spectate/:roomId" element={<TexasHoldemGamePage />} />
          {/* 2026-09-24 §虚拟城市定位 v2.0 — 全 Agent 真实城市模拟器（财富游戏改名）。 */}
          <Route path="/virtual-city" element={<VirtualCityLobbyPage />} />
          <Route path="/virtual-city/:roomId" element={<VirtualCityGamePage />} />
          <Route path="/virtual-city/spectate/:roomId" element={<VirtualCityGamePage />} />
          <Route path="/werewolf" element={<WerewolfLobbyPage />} />
          <Route path="/werewolf/:roomId" element={<WerewolfGamePage />} />
          <Route path="/werewolf/spectate/:roomId" element={<WerewolfGamePage />} />
          <Route path="/debate" element={<DebateLobbyPage />} />
          <Route path="/debate/:roomId" element={<DebateGamePage />} />
          <Route path="/debate/spectate/:roomId" element={<DebateGamePage />} />
          <Route path="/profile" element={<ProfilePage />} />
          <Route path="/about" element={<AboutPage />} />
          <Route path="/admin/users" element={<AdminUsersPage />} />
          <Route path="/admin/models" element={<ModelAdminPage />} />
          <Route path="/admin/models/:providerId" element={<ModelDetailPage />} />
          <Route path="/admin/models/:providerId/games/:gameLogId" element={<ModelGameLogPage />} />
        </Routes>
      </AppLayout>
      {!isAuthenticated && <AuthModal />}
    </>
  );
}
