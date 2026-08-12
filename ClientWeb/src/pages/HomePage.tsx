import { useEffect, useState } from 'react';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { gameService } from '@/services/auth.service';
import { useGameStore } from '@/store/game.store';
import { GameCard } from '@/components/lobby/GameCard';
import { LobbyScene } from '@/scenes/LobbyScene';
import { ErrorBoundary } from '@/components/common/ErrorBoundary';
import { useT } from '@/hooks/useT';
import type { GameInfo } from '@/types/api';

export function HomePage() {
  const games = useGameStore((s) => s.games);
  const setGames = useGameStore((s) => s.setGames);
  const [err, setErr] = useState('');
  const t = useT();

  useEffect(() => {
    gameService.list()
      .then((g: GameInfo[]) => setGames(g))
      .catch((e: Error) => {
        if (!isSessionExpiredError(e)) {
          setErr(e.message);
          reportGlobalError({ message: e.message, severity: 'error' });
        }
      });
  }, [setGames]);

  return (
    <div>
      <h1 style={{ marginTop: 0 }}>{t('home.title')}</h1>
      {err && <div className="error">{err}</div>}
      <div style={{ height: 280, marginBottom: 24, border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
        <ErrorBoundary label="LobbyScene">
          <LobbyScene />
        </ErrorBoundary>
      </div>
      <div className="game-grid">
        {games.map((g) => <GameCard key={g.id} game={g} />)}
      </div>
    </div>
  );
}
