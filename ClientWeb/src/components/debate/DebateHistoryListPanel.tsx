/**
 * 辩论比赛 — 大厅「历史战绩」列表面板 (2026-08-31 §20260831-08)
 *
 * 对齐后端 GET /api/games/debate/history?page=&page_size= 契约:
 *   - 已结束比赛分页列表:辩题 / 模式 / 胜方 / 最佳辩手 / 结束时间 / 异常标记
 *   - 点行或「查看复盘」按钮 → onOpenDetail(roomId) 由父级打开 DebateReplayModal
 *
 * 交互风格与 DebateLobbyPage「模型胜率统计」面板(§20260831-06)同构:
 * 折叠外壳由父级持有,本组件只负责列表 + 分页。
 *
 * 样式:复用 debate.css 的 .model-stats-table 系列,新增类在
 * styles/debate-history.css(debate.css 已 1673 行,逼近 §4 1800 行上限)。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useEffect, useState } from 'react';
import { debateService } from '@/api/debate';
import { isSessionExpiredError } from '@/services/http';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { DebateHistoryRoom } from '@/types/debate';
import {
  HISTORY_PAGE_SIZE,
  formatUnix,
  modeLabel,
  teamLabel,
} from './historyUtils';

interface Props {
  onOpenDetail: (roomId: string) => void;
}

export default function DebateHistoryListPanel({ onOpenDetail }: Props) {
  const t = useT();
  const [rooms, setRooms] = useState<DebateHistoryRoom[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  // 拉取当前页(切页 / 首挂载)。§7.1:失败就地展示 error bar。
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setErr('');
    debateService
      .listHistory(page, HISTORY_PAGE_SIZE)
      .then((d) => {
        if (cancelled) return;
        setRooms(d?.rooms ?? []);
        setTotal(d?.total ?? 0);
      })
      .catch((e: Error) => {
        if (cancelled || isSessionExpiredError(e)) return;
        setErr(e.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [page]);

  const maxPage = Math.max(1, Math.ceil(total / HISTORY_PAGE_SIZE));

  return (
    <div className="debate-history-list">
      {loading && <p className="model-stats-empty">{t('debate.history.loading' as TKey)}</p>}

      {!loading && err && (
        <div className="history-error" role="alert">
          {t('debate.history.error' as TKey, { msg: err })}
        </div>
      )}

      {!loading && !err && rooms.length === 0 && (
        <p className="model-stats-empty">{t('debate.history.empty' as TKey)}</p>
      )}

      {rooms.length > 0 && (
        <table className="model-stats-table debate-history-table">
          <thead>
            <tr>
              <th>{t('debate.history.topic' as TKey)}</th>
              <th>{t('debate.history.mode' as TKey)}</th>
              <th>{t('debate.history.winner' as TKey)}</th>
              <th>{t('debate.history.bestDebater' as TKey)}</th>
              <th>{t('debate.history.finishedAt' as TKey)}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {rooms.map((r) => (
              <tr
                key={r.room_id}
                className="history-row"
                onClick={() => onOpenDetail(r.room_id)}
              >
                <td className="history-topic-cell">
                  {r.topic_text}
                  {r.is_abnormal && (
                    <span className="history-abnormal-badge">
                      {t('debate.history.abnormal' as TKey)}
                    </span>
                  )}
                </td>
                <td>{modeLabel(r.mode, t)}</td>
                <td>
                  {r.winner_team_id ? (
                    <span className="history-winner-badge">
                      {teamLabel(r, r.winner_team_id)}
                    </span>
                  ) : (
                    <span className="history-muted">{t('debate.replay.noWinner' as TKey)}</span>
                  )}
                </td>
                <td className="history-muted">
                  {r.best_debater_team_id || r.best_debater_seat
                    ? `${teamLabel(r, r.best_debater_team_id)} · ${t('debate.history.seat' as TKey, { n: r.best_debater_seat })}`
                    : '—'}
                </td>
                <td className="history-muted">{formatUnix(r.finished_at)}</td>
                <td>
                  <button
                    type="button"
                    className="btn-secondary btn-sm"
                    onClick={(e) => {
                      e.stopPropagation();
                      onOpenDetail(r.room_id);
                    }}
                  >
                    {t('debate.history.view' as TKey)}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {(rooms.length > 0 || page > 1) && (
        <div className="history-pager">
          <button
            type="button"
            className="btn-secondary btn-sm"
            disabled={page <= 1 || loading}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            {t('debate.history.prev' as TKey)}
          </button>
          <span className="history-pager-info">
            {t('debate.history.pageInfo' as TKey, { page, total })}
          </span>
          <button
            type="button"
            className="btn-secondary btn-sm"
            disabled={page >= maxPage || loading}
            onClick={() => setPage((p) => Math.min(maxPage, p + 1))}
          >
            {t('debate.history.next' as TKey)}
          </button>
        </div>
      )}
    </div>
  );
}
