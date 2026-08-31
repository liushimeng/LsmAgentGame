/**
 * 辩论比赛 AI 解说面板 (2026-08-31 §20260831-04)
 *
 * 对齐 docs/辩论比赛/04 §3.3 观众交互 + §20260831-03 AI 实时解说 Agent。
 *
 * 设计:
 *   - 顶部样式切换(pro 严谨 / fun 吐槽,仅展示当前最新一条的样式)
 *   - 解说按时间倒序展示,最新一条高亮(金色左边框)
 *   - 空状态时显示「AI 解说正在旁听...」
 *   - 仅当房间启用解说时填充内容(§20260831-04 由 DebateManager.CommentatorModelKey 控制,
 *     后端 broadcast 即使空 model_key 也会跳过)
 */
import { useDebateStore } from '@/store/debate.store';

export default function DebateCommentaryPanel() {
  const commentaries = useDebateStore((s) => s.commentaries);

  if (commentaries.length === 0) {
    return (
      <div className="debate-commentary debate-commentary--empty">
        <header className="commentary-header">
          <span className="commentary-icon">🎙️</span>
          <h3>AI 实时解说</h3>
        </header>
        <p className="commentary-empty-text">解说员正在旁听,精彩处会主动点评...</p>
      </div>
    );
  }

  // 最新一条高亮(数组末尾是最新,因为 pushCommentary 是追加)
  const latest = commentaries[commentaries.length - 1];
  const history = commentaries.slice(0, -1).reverse();

  return (
    <div className="debate-commentary">
      <header className="commentary-header">
        <span className="commentary-icon">🎙️</span>
        <h3>AI 实时解说</h3>
        <span className={`commentary-style-tag commentary-style-tag--${latest.style}`}>
          {latest.style === 'fun' ? '吐槽' : '严谨'}
        </span>
      </header>

      <div className="commentary-latest">
        <p className="commentary-text">{latest.text}</p>
        <span className="commentary-time">{formatTime(latest.timestamp)}</span>
      </div>

      {history.length > 0 && (
        <details className="commentary-history">
          <summary>历史解说({history.length})</summary>
          <ul>
            {history.map((c, idx) => (
              <li key={`${c.timestamp}-${idx}`} className="commentary-history-item">
                <p>{c.text}</p>
                <span className="commentary-time">{formatTime(c.timestamp)}</span>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}

function formatTime(ts: number): string {
  const d = new Date(ts);
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  const ss = String(d.getSeconds()).padStart(2, '0');
  return `${hh}:${mm}:${ss}`;
}
