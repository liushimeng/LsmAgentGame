/**
 * 裁判实时打分看板面板 (2026-08-31 §20260831-09)
 *
 * 对齐 docs/辩论比赛/07 §3.3 — 每个裁判一张卡:
 *   - 卡片内按队伍分块展示累计 5 维度 + 总分 + 最近评语;
 *   - 下方「📜 阶段历史」折叠区:按提交时间倒序展示最近 10 条 stage_score。
 *
 * 数据来源:store.scoreboards(Record<number, DebateJudgeScoreboard>)。
 * 后端通过 debate.judge_scoreboard 帧增量更新。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useDebateStore } from '@/store/debate.store';
import { DIM_LABELS } from './DebateScorePanel';
import type { DebateJudgeScoreboard } from '@/types/debate';

export default function DebateJudgeScoreboardPanel() {
  const scoreboards = useDebateStore((s) => s.scoreboards);
  const judges = useDebateStore((s) => s.currentRoom?.judges ?? []);

  const entries = Object.values(scoreboards);
  if (entries.length === 0) {
    return (
      <div className="debate-judge-scoreboard-panel debate-judge-scoreboard-panel--empty">
        <h3>⚖️ 裁判实时打分</h3>
        <p>等待裁判提交阶段打分...</p>
      </div>
    );
  }

  return (
    <div className="debate-judge-scoreboard-panel">
      <h3>⚖️ 裁判实时打分</h3>
      {entries.map((board) => {
        const judge = judges.find((j) => j.judge_id === board.judge_id);
        return (
          <JudgeScoreboardCard
            key={board.judge_id}
            board={board}
            judgeName={judge?.name ?? `裁判 ${board.judge_id + 1}`}
          />
        );
      })}
    </div>
  );
}

function JudgeScoreboardCard({
  board,
  judgeName,
}: {
  board: DebateJudgeScoreboard;
  judgeName: string;
}) {
  const teamEntries = Object.values(board.team_scores);

  return (
    <div className={`judge-scoreboard-card${board.is_final ? ' judge-scoreboard-card--final' : ''}`}>
      <header className="judge-scoreboard-card__header">
        <span className="judge-scoreboard-card__name">
          {board.is_final ? '✅' : '⏳'} {judgeName}
        </span>
        {board.model_key && (
          <span className="judge-scoreboard-card__model">{board.model_key.replace(/-model$/, '')}</span>
        )}
      </header>

      <div className="judge-scoreboard-card__teams">
        {teamEntries.map((ts) => (
          <div key={ts.team_id} className="judge-scoreboard-card__team">
            <div className="judge-scoreboard-card__team-header">
              队伍 {ts.team_id + 1}
              <span className="judge-scoreboard-card__total">{ts.total_score.toFixed(1)}</span>
            </div>
            <div className="judge-scoreboard-card__dims">
              {(['argument_quality', 'logic_rigor', 'language_expression', 'team_coordination', 'rebuttal_effectiveness'] as const).map((dim) => (
                <div key={dim} className="judge-scoreboard-card__dim">
                  <span className="judge-scoreboard-card__dim-label">{DIM_LABELS[dim] ?? dim}</span>
                  <div className="judge-scoreboard-card__dim-track">
                    <div
                      className="judge-scoreboard-card__dim-fill"
                      style={{ width: `${(ts[dim] / 10) * 100}%` }}
                    />
                  </div>
                  <span className="judge-scoreboard-card__dim-value">{ts[dim].toFixed(1)}</span>
                </div>
              ))}
            </div>
            {ts.latest_comment && (
              <div className="judge-scoreboard-card__comment" title={ts.latest_comment}>
                💬 {ts.latest_comment.slice(0, 40)}{ts.latest_comment.length > 40 ? '...' : ''}
                <span className="judge-scoreboard-card__phase">({ts.latest_phase_cn})</span>
              </div>
            )}
          </div>
        ))}
      </div>

      {board.stage_history.length > 0 && (
        <details className="judge-scoreboard-card__history">
          <summary>📜 阶段历史({board.stage_history.length})</summary>
          <ul>
            {board.stage_history
              .slice()
              .reverse()
              .map((h, idx) => (
                <li key={`${h.phase}-${h.submitted_at_ms}-${idx}`} className="judge-scoreboard-card__history-item">
                  <span className="judge-scoreboard-card__history-phase">
                    {h.phase_cn}
                    {h.is_final && <span className="judge-scoreboard-card__final-badge">最终</span>}
                  </span>
                  <span className="judge-scoreboard-card__history-comment">
                    {h.overall_comment?.slice(0, 60) ?? '(无评语)'}
                  </span>
                </li>
              ))}
          </ul>
        </details>
      )}
    </div>
  );
}
