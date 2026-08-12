import { useNavigate } from 'react-router-dom';
import type { GameInfo } from '@/types/api';

export function GameCard({ game }: { game: GameInfo }) {
  const nav = useNavigate();

  const handleClick = () => {
    if (game.kind === 'xiangqi') {
      nav('/xiangqi');
      return;
    }
    if (game.kind === 'chess') {
      nav('/chess');
      return;
    }
    if (game.kind === 'junqi') {
      nav('/junqi');
      return;
    }
    if (game.kind === 'doudizhu') {
      nav('/doudizhu');
      return;
    }
    if (game.kind === 'texasholdem') {
      nav('/texasholdem');
      return;
    }
    if (game.kind === 'werewolf') {
      nav('/werewolf');
      return;
    }
    nav(`/game/${encodeURIComponent(game.id)}`);
  };

  return (
    <div className="game-card" onClick={handleClick} role="button" tabIndex={0}>
      <h3>{game.name}</h3>
      <div className="meta">{game.kind} · {game.online} online</div>
    </div>
  );
}
