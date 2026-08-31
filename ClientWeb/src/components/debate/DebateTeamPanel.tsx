/**
 * 辩论队伍信息面板 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §3.2 队伍信息面板设计。
 * §20260831-04 — 多队模式:按 teams 数组渲染(2-5 队),
 * angle_1~angle_5(五队发散模式)立场有专属图标与配色。
 */
import { useDebateStore } from '@/store/debate.store';
import type { DebateClientTeam } from '@/types/debate';

/** 立场 → 图标(多角度立场各自专属,§20260831-04)
 *  §20260831-08 — 导出供 DebateReplayModal(复盘详情)复用。 */
export const STANCE_ICONS: Record<string, string> = {
  pro: '🔵',
  con: '🔴',
  neutral: '🟣',
  gov_upper: '🔷',
  gov_lower: '🔹',
  opp_upper: '🔶',
  opp_lower: '🔸',
  angle_1: '🟦',
  angle_2: '🟨',
  angle_3: '🟩',
  angle_4: '🟧',
  angle_5: '🟪',
};

export default function DebateTeamPanel() {
  const { currentRoom, currentSpeaker } = useDebateStore();

  if (!currentRoom) return null;

  return (
    <div className="debate-team-panel">
      <h3>🏆 队伍信息({currentRoom.teams.length} 队)</h3>
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
  const stanceIcon = STANCE_ICONS[team.stance] ?? '⚪';

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