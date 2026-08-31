/**
 * 辩论裁判面板 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §3.3 裁判面板设计。
 */
import { useDebateStore } from '@/store/debate.store';
import type { DebateJudgeScore } from '@/types/debate';

export default function DebateJudgePanel() {
  const { currentRoom, judgeScores } = useDebateStore();

  if (!currentRoom) return null;

  return (
    <div className="debate-judge-panel">
      <h3>⚖️ 裁判评审</h3>
      <ul className="judge-list">
        {currentRoom.judges.map((j) => {
          const score = judgeScores.find((s) => s.judge_id === j.judge_id);
          return <JudgeCard key={j.judge_id} judge={j} score={score} />;
        })}
      </ul>
    </div>
  );
}

function JudgeCard({
  judge,
  score,
}: {
  judge: { judge_id: number; name?: string; model_key?: string };
  score?: DebateJudgeScore;
}) {
  const submitted = !!score;
  return (
    <li className={`judge-card${submitted ? ' judge-card--done' : ''}`}>
      <div className="judge-card__name">
        {submitted ? '✅' : '⏳'} {judge.name ?? `裁判 ${judge.judge_id + 1}`}
      </div>
      {score && score.overall_comment && (
        <div className="judge-card__comment">{score.overall_comment}</div>
      )}
    </li>
  );
}