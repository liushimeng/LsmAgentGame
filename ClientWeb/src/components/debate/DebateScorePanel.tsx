/**
 * 辩论评分结果面板 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §3.5 评分面板设计。
 */
import { useDebateStore } from '@/store/debate.store';

export default function DebateScorePanel() {
  const { result, phase } = useDebateStore();

  // 仅在 result 阶段或之后显示
  if (phase !== 'result' && phase !== 'game_over') {
    return (
      <div className="debate-score-panel debate-score-panel--pending">
        <h3>🏆 评审结果</h3>
        <p>评审结束后公布</p>
      </div>
    );
  }

  if (!result) {
    return (
      <div className="debate-score-panel">
        <h3>🏆 评审结果</h3>
        <p>评审中...</p>
      </div>
    );
  }

  return (
    <div className="debate-score-panel">
      <h3>🏆 评审结果</h3>

      {/* 胜方高亮 */}
      <div className="winner-banner">
        🥇 胜方:{result.winner_team_name}
      </div>

      {/* 各队伍分数 */}
      {result.team_scores.map((ts) => {
        const isWinner = ts.team_id === result.winner_team_id;
        return (
          <div
            key={ts.team_id}
            className={`team-score${isWinner ? ' team-score--winner' : ''}`}
          >
            <header className="team-score__header">
              {isWinner ? '🥇' : '🥈'} {ts.team_name} #{ts.rank}
              <span className="total">{ts.total_score.toFixed(1)}</span>
            </header>
            <div className="team-score__bars">
              {Object.entries(ts.dimension_scores).map(([dim, score]) => (
                <div key={dim} className="score-bar">
                  <span className="score-bar__label">{dim}</span>
                  <div className="score-bar__track">
                    <div
                      className="score-bar__fill"
                      style={{ width: `${(score / 10) * 100}%` }}
                    />
                  </div>
                  <span className="score-bar__value">{score.toFixed(1)}</span>
                </div>
              ))}
            </div>
          </div>
        );
      })}

      {/* 最佳辩手 */}
      <div className="best-debater">
        🌟 最佳辩手:{result.best_debater.name} (座位 {result.best_debater.seat})
      </div>

      {/* 整体评语 */}
      {result.judge_details && result.judge_details.length > 0 && (
        <details className="judge-comments">
          <summary>📝 裁判点评</summary>
          <ul>
            {result.judge_details.map((js) => (
              <li key={js.judge_id}>
                <strong>裁判 {js.judge_id + 1}({js.model_key}):</strong>
                <p>{js.overall_comment}</p>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}