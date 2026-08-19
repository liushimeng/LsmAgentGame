import { useEffect, useState } from 'react';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { gameService } from '@/services/auth.service';
import { useGameStore } from '@/store/game.store';
import { GameCard } from '@/components/lobby/GameCard';
import { useT } from '@/hooks/useT';
import type { GameInfo } from '@/types/api';

// GamesPage 全部游戏列表页(分类渲染)
// §20260819-02: HomePage 同款分类,但不加 3D 场景,纯列表呈现。
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

  // §20260819-02: 按 category 分组
  const agents = games.filter((g) => (g.category ?? 'traditional') === 'agent');
  const trads = games.filter((g) => (g.category ?? 'traditional') === 'traditional');

  return (
    <div>
      <h1 style={{ marginTop: 0 }}>{t('games.title' as any)}</h1>
      {err && <div className="error">{err}</div>}

      {agents.length > 0 && (
        <section className="game-section">
          <h2 className="game-section-title game-section-title--agent">
            🤖 {t('games.categories.agent' as any)}
          </h2>
          <div className="game-grid">
            {agents.map((g) => (
              <GameCard key={g.id} game={g} />
            ))}
          </div>
        </section>
      )}

      {trads.length > 0 && (
        <section className="game-section">
          <h2 className="game-section-title game-section-title--traditional">
            🎮 {t('games.categories.traditional' as any)}
          </h2>
          <div className="game-grid">
            {trads.map((g) => (
              <GameCard key={g.id} game={g} />
            ))}
          </div>
        </section>
      )}

      {agents.length === 0 && trads.length === 0 && (
        <div className="game-grid">
          {games.map((g) => (
            <GameCard key={g.id} game={g} />
          ))}
        </div>
      )}
    </div>
  );
}
