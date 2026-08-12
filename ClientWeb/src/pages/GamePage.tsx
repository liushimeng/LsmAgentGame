import { useParams, Link } from 'react-router-dom';
import { GameScene } from '@/scenes/GameScene';

export function GamePage() {
  const { id } = useParams();
  return (
    <div>
      <div style={{ marginBottom: 12 }}>
        <Link to="/">← back to lobby</Link>
        <span style={{ marginLeft: 12, color: 'var(--muted)' }}>game: {id}</span>
      </div>
      <GameScene />
    </div>
  );
}
