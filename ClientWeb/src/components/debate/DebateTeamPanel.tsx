/**
 * 辩论队伍信息面板 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §3.2 队伍信息面板设计。
 */
import { useDebateStore } from '@/store/debate.store';
import type { DebateClientTeam } from '@/types/debate';

export default function DebateTeamPanel() {
  const { currentRoom, currentSpeaker } = useDebateStore();

  if (!currentRoom) return null;

  return (
    <div className="debate-team-panel">
      <h3>🏆 队伍信息</h3>
      {currentRoom.teams.map((team) => (
        <TeamCard
          key={team.team_id}
          team={team}
          currentSpeaker={currentSpeaker}
        />
      ))}
    </div>
  );
}

function TeamCard({
  team,
  currentSpeaker,
}: {
  team: DebateClientTeam;
  currentSpeaker: string;
}) {
  const isPro = team.stance === 'pro' || team.stance === 'gov_upper' || team.stance === 'gov_lower';
  const stanceIcon = isPro ? '🔵' : team.stance === 'con' || team.stance === 'opp_upper' || team.stance === 'opp_lower' ? '🔴' : '🟣';

  return (
    <div className={`team-card team-card--${team.stance}`}>
      <header className="team-card__header">
        {stanceIcon} {team.stance_label}
      </header>
      <ul className="team-card__agents">
        {team.agents.map((ag) => {
          const speakerKey = `${team.team_id}:${ag.seat_id}`;
          const isSpeaking = currentSpeaker === speakerKey;
          return (
            <li
              key={ag.seat_id}
              className={`agent-row${isSpeaking ? ' agent-row--speaking' : ''}`}
            >
              <span className="agent-icon">
                {isSpeaking ? '🎤' : '○'}
              </span>
              <span className="agent-name">{ag.name ?? ag.role_name ?? ag.role}</span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}