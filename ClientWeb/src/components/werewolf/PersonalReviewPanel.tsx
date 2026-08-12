// §20260812-01 U1 — 个人复盘 4 维面板(纯前端,依赖后端 GET /api/games/werewolf/rooms/:id/review)。
//
// 4 维卡片栅格:投票准确率 / 发言暴露度 / 道具效率 / Agent 互动质量。
// 数据形状对齐后端 werewolf.PersonalReviewResponse(§121 数据形状契约)。
//
// 错误展示(§7.1):面板内联红条(最高可见位置) + reportGlobalError 全局兜底。
import { useEffect, useState } from 'react';
import { http, ApiError, isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

/** ReviewDimension 对齐后端 werewolf.ReviewDimension。 */
export interface ReviewDimension {
  score: number; // 0.0~1.0
  raw: number;
  hit_count: number;
  total_count: number;
  description: string;
}

/** PersonalReview 对齐后端 werewolf.PersonalReview。 */
export interface PersonalReview {
  user_id: string;
  vote_accuracy: ReviewDimension;
  speak_exposure: ReviewDimension;
  prop_efficiency: ReviewDimension;
  agent_interaction: ReviewDimension;
  overall_score: number;
  role?: string;
  winner?: string;
  highlights?: string[];
}

/** PersonalReviewResponse 对齐后端 werewolf.PersonalReviewResponse。 */
export interface PersonalReviewResponse {
  review: PersonalReview;
  computed_at: number;
  from_cache: boolean;
}

export interface PersonalReviewPanelProps {
  roomId: string;
  userId: string;
  open: boolean;
  onClose: () => void;
}

const SCORE_COLORS: Array<{ min: number; bg: string; label: string }> = [
  { min: 0.8, bg: 'rgba(46, 204, 113, 0.7)', label: '优秀' },
  { min: 0.5, bg: 'rgba(241, 196, 15, 0.7)', label: '良好' },
  { min: 0.0, bg: 'rgba(231, 76, 60, 0.7)', label: '待提升' },
];

/** 颜色阈值遵循 §26 — 文字与背景对比度 ≥ 4.5(白字 + ≥0.45 透明度 + 700 字重)。 */
function scoreColor(score: number): string {
  for (const c of SCORE_COLORS) {
    if (score >= c.min) return c.bg;
  }
  return SCORE_COLORS[SCORE_COLORS.length - 1].bg;
}

export function PersonalReviewPanel({
  roomId,
  userId,
  open,
  onClose,
}: PersonalReviewPanelProps) {
  const t = useT();
  const [data, setData] = useState<PersonalReview | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    setErr(null);
    (async () => {
      try {
        const url = `/api/games/werewolf/rooms/${encodeURIComponent(roomId)}/review/${encodeURIComponent(userId)}`;
        const res = await http<PersonalReviewResponse>(url, { method: 'GET' });
        if (cancelled) return;
        setData(res.review);
      } catch (e: any) {
        if (cancelled) return;
        if (isSessionExpiredError(e)) {
          onClose();
          return;
        }
        const msg = e instanceof ApiError ? e.message : String(e?.message ?? e);
        setErr(msg);
        reportGlobalError({ message: msg, severity: 'error' });
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [open, roomId, userId, onClose]);

  if (!open) return null;
  return (
    <div className="personal-review-panel" role="dialog" aria-modal="true" aria-label={t('werewolf.review.title' as TKey)}>
      <div className="personal-review-panel__overlay" onClick={onClose} />
      <aside className="personal-review-panel__body">
        <header className="personal-review-panel__header">
          <h3 className="personal-review-panel__title">{t('werewolf.review.title' as TKey)}</h3>
          <button
            type="button"
            className="personal-review-panel__close"
            onClick={onClose}
            aria-label={t('common.close' as TKey)}
          >×</button>
        </header>
        {err && (
          <div className="personal-review-panel__error" role="alert">
            ⚠ {err}
          </div>
        )}
        {loading && <div className="personal-review-panel__loading">⏳ {t('common.loading' as TKey)}</div>}
        {data && (
          <div className="personal-review-panel__grid">
            <ReviewCard
              titleKey={'werewolf.review.voteAccuracy' as TKey}
              dim={data.vote_accuracy}
              data-testid="personal-review-vote"
            />
            <ReviewCard
              titleKey={'werewolf.review.speakExposure' as TKey}
              dim={data.speak_exposure}
              data-testid="personal-review-speak"
            />
            <ReviewCard
              titleKey={'werewolf.review.propEfficiency' as TKey}
              dim={data.prop_efficiency}
              data-testid="personal-review-prop"
            />
            <ReviewCard
              titleKey={'werewolf.review.agentInteraction' as TKey}
              dim={data.agent_interaction}
              data-testid="personal-review-agent"
            />
            <div className="personal-review-panel__overall" data-testid="personal-review-overall">
              <span className="personal-review-panel__overall-label">
                {t('werewolf.review.overall' as TKey)}
              </span>
              <span className="personal-review-panel__overall-score">
                {(data.overall_score * 100).toFixed(0)}%
              </span>
            </div>
            {data.highlights && data.highlights.length > 0 && (
              <div className="personal-review-panel__highlights">
                <h4>✨ {t('werewolf.review.highlights' as TKey)}</h4>
                <ul>
                  {data.highlights.map((h, i) => (
                    <li key={i}>{h}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </aside>
    </div>
  );
}

function ReviewCard({
  titleKey,
  dim,
  'data-testid': dataTestid,
}: {
  titleKey: TKey;
  dim: ReviewDimension;
  'data-testid'?: string;
}) {
  const t = useT();
  const pct = (dim.score * 100).toFixed(0);
  const bg = scoreColor(dim.score);
  return (
    <div className="personal-review-panel__card" data-testid={dataTestid}>
      <div className="personal-review-panel__card-label">{t(titleKey)}</div>
      <div
        className="personal-review-panel__card-score"
        style={{ background: bg, color: '#fff', fontWeight: 700 }}
      >
        {pct}%
      </div>
      <div className="personal-review-panel__card-meta">
        {dim.total_count > 0
          ? `${dim.hit_count}/${dim.total_count}`
          : t('werewolf.review.noData' as TKey)}
      </div>
    </div>
  );
}
