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

// HomePage 大厅首页(分类渲染)
// §20260819-02: 6 款游戏按 category 分两组,AGENT 游戏(德州扑克、狼人杀)置顶展示
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

  // §20260819-02: 按 category 分组,未下发 category 字段时按 'traditional' 兜底
  const agents = games.filter((g) => (g.category ?? 'traditional') === 'agent');
  const trads = games.filter((g) => (g.category ?? 'traditional') === 'traditional');

  return (
    <div>
      <h1 style={{ marginTop: 0 }}>{t('home.title')}</h1>
      {err && <div className="error">{err}</div>}
      <div
        style={{
          height: 280,
          marginBottom: 24,
          border: '1px solid var(--border)',
          borderRadius: 8,
          overflow: 'hidden',
        }}
      >
        <ErrorBoundary label="LobbyScene">
          <LobbyScene />
        </ErrorBoundary>
      </div>

      {/* AGENT 游戏(德州扑克 + 狼人杀)置顶 */}
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

      {/* 传统游戏 */}
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

      {/* 兜底:若全部 category 兜底后仍无任何游戏,显示空态 */}
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
