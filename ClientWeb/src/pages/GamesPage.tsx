import { useEffect, useState } from 'react';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { gameService } from '@/services/auth.service';
import { useGameStore } from '@/store/game.store';
import { GameCard } from '@/components/lobby/GameCard';
import { useT } from '@/hooks/useT';
import type { GameInfo } from '@/types/api';

// 全部游戏列表页 —— 复用大厅的游戏数据，纯列表呈现。
export function GamesPage() {
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
      <h1 style={{ marginTop: 0 }}>{t('games.title')}</h1>
      {err && <div className="error">{err}</div>}
      <div className="game-grid">
        {games.map((g) => <GameCard key={g.id} game={g} />)}
      </div>
    </div>
  );
}
