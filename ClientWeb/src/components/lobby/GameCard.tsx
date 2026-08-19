import { useNavigate } from 'react-router-dom';
import type { GameInfo } from '@/types/api';
import { useT } from '@/hooks/useT';

// GameCard 大厅游戏卡片。
//
// §20260819-02: 增加 category 字段支持的徽章渲染。
//  - "agent" 类别显示紫红 AGENT 徽章 + 边框高亮(德州扑克、狼人杀)
//  - "traditional" 类别显示灰色「传统」徽章(象棋、军棋、斗地主)
export function GameCard({ game }: { game: GameInfo }) {
  const nav = useNavigate();
  const t = useT();

  // 兼容旧 backend:未下发 category 字段时按 "traditional" 兜底
  const category = game.category ?? 'traditional';

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

  const badgeKey =
    category === 'agent'
      ? 'games.categories.agent'
      : 'games.categories.traditional';

  return (
    <div
      className={`game-card game-card--${category}`}
      onClick={handleClick}
      role="button"
      tabIndex={0}
    >
      <span className={`game-card__badge game-card__badge--${category}`}>
        {t(badgeKey as any)}
      </span>
      <h3>{game.name}</h3>
      <div className="meta">
        {game.kind} · {game.online} online
      </div>
    </div>
  );
}
