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
import { TexasHoldemLobbyPage } from './pages/TexasHoldemLobbyPage';
import { TexasHoldemGamePage } from './pages/TexasHoldemGamePage';
import { AdminUsersPage } from './pages/AdminUsersPage';
import { ModelAdminPage } from './pages/ModelAdminPage';
import { ModelDetailPage } from './pages/ModelDetailPage';
import { ModelGameLogPage } from './pages/ModelGameLogPage';

export default function App() {
  const isAuthenticated = useAuth((s) => s.isAuthenticated);
  const hasHydrated = useAuth((s) => s.hasHydrated);

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
          <Route path="/werewolf" element={<WerewolfLobbyPage />} />
          <Route path="/werewolf/:roomId" element={<WerewolfGamePage />} />
          <Route path="/werewolf/spectate/:roomId" element={<WerewolfGamePage />} />
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
