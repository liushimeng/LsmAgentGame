/**
 * ResidentProfileDrawer — 城市居民人物卡档案抽屉（档案锚定设计 §8.3）。
 *
 * 右侧滑出抽屉（FactionDrawer 同款骨架：380px/30vw + overlay），数据源为
 * REST GET /api/games/virtual-city/rooms/:id/city/residents(/:cardId)（§7 契约）：
 *   - 搜索框（q，300ms 防抖；姓名/职业/卡号包含匹配）
 *   - 分页列表（limit 50，上一页/下一页；行内展示姓名/职业/年龄/城区/收入/储蓄/就业徽章）
 *   - 点击行展开详情卡（opening_hook 引言块 / 人格词 chips / 5 年目标 /
 *     婚姻·健康档 / source_file 审计路径）
 *   - hydrating 中：顶部进度条 + 禁用列表（后端异步锚定 100K ≤ 30s，期间合成兜底）；
 *     ready：列表可用；failed/idle：灰字说明。
 *   - focusCardId（城市之声条目点击进入）：先拉单卡详情置顶展示「定位该卡」。
 *
 * 观战者/玩家同可见（档案是公开合成人格，无隐私）。错误按 §7.1 就地内联红条
 * （不吞进 console；会话过期类错误交还全局弹层，不重复展示）。
 *
 * 样式在 styles/virtualCity-residents.css（globals.css 链尾 @import；virtualCity.css 已
 * 1738 行，追加会突破 §4 单文件 ≤1800 行硬上限，故按 virtualCity-p2/insurance 先例拆分）。
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { isSessionExpiredError } from '@/services/http';
import { fetchCityResident, fetchCityResidents } from '@/api/virtualCity';
import { formatCny } from '@/types/virtualCity';
import type { VirtualCityCityProfileProgress, VirtualCityCityResidentProfile } from '@/types/virtualCity';

/** 每页条数（§8.3 契约固定 50）。 */
const PAGE_LIMIT = 50;
/** 搜索防抖毫秒数（§8.3 契约 300ms）。 */
const SEARCH_DEBOUNCE_MS = 300;
/** hydrating 期间进度轮询间隔（快照另有 8s 全量轮询，此处更快感知终态）。 */
const HYDRATE_POLL_MS = 2500;

interface Props {
  open: boolean;
  roomId: string;
  /** 打开时定位展示的居民卡号（城市之声 resident_id 点击进入）；可空。 */
  focusCardId?: string | null;
  onClose: () => void;
}

/** 婚姻字段 → i18n（single/married 复用 virtualCity.family.*；未知值原样展示）。 */
function maritalLabel(marital: string, t: (k: TKey, v?: Record<string, string | number>) => string): string {
  if (marital === 'married') return t('virtualCity.family.married');
  if (marital === 'single') return t('virtualCity.family.single');
  return marital || '—';
}

/** 人格词 chips 数据源：「、」分隔 → 非空词数组。 */
function personalityChips(personality: string): string[] {
  return (personality || '')
    .split(/[、,，;；]/)
    .map((s) => s.trim())
    .filter(Boolean);
}

export function ResidentProfileDrawer({ open, roomId, focusCardId, onClose }: Props) {
  const t = useT();
  const [progress, setProgress] = useState<VirtualCityCityProfileProgress | null>(null);
  const [residents, setResidents] = useState<VirtualCityCityResidentProfile[]>([]);
  const [matched, setMatched] = useState(0);
  const [offset, setOffset] = useState(0);
  const [qInput, setQInput] = useState('');
  const [qApplied, setQApplied] = useState('');
  const [expanded, setExpanded] = useState<string | null>(null);
  /** 城市之声定位卡（单卡详情；独立于分页列表置顶展示）。 */
  const [focusDetail, setFocusDetail] = useState<VirtualCityCityResidentProfile | null>(null);
  const [focusMissing, setFocusMissing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  /** hydrating 轮询心跳（变更触发列表重拉）。 */
  const [reloadTick, setReloadTick] = useState(0);
  /** 竞态守卫：仅采纳最后一次请求的响应。 */
  const reqSeq = useRef(0);

  // 注：状态重置（换房间 / 换定位卡 / 重新打开）由父级以 key={focusCardId ?? 'all'}
  // 整体重挂载实现 —— 首帧即全新 state，避免 effect 重置带来的瞬态旧页请求。

  // 搜索 300ms 防抖：qInput 稳定后写入 qApplied 并回到第一页。
  useEffect(() => {
    if (!open) return;
    const id = window.setTimeout(() => {
      setQApplied(qInput.trim());
      setOffset(0);
    }, SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(id);
  }, [qInput, open]);

  // 列表加载（open / 翻页 / 搜索 / 定位卡变更 / 轮询心跳 共用；seq 防乱序覆盖）。
  useEffect(() => {
    if (!open) return;
    const seq = ++reqSeq.current;
    setLoading(true);
    fetchCityResidents(roomId, offset, PAGE_LIMIT, qApplied || undefined)
      .then((page) => {
        if (seq !== reqSeq.current) return;
        setProgress(page.progress ?? null);
        setResidents(Array.isArray(page.residents) ? page.residents : []);
        setMatched(page.matched ?? 0);
        setError(null);
      })
      .catch((e: unknown) => {
        if (seq !== reqSeq.current || isSessionExpiredError(e)) return;
        setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (seq === reqSeq.current) setLoading(false);
      });
  }, [open, roomId, offset, qApplied, focusCardId, reloadTick]);

  // 定位卡（城市之声点击进入）：拉单卡详情。
  useEffect(() => {
    if (!open || !focusCardId) return;
    let cancelled = false;
    setFocusMissing(false);
    fetchCityResident(roomId, focusCardId)
      .then((p) => {
        if (cancelled) return;
        if (p) setFocusDetail(p);
        else setFocusMissing(true);
      })
      .catch((e: unknown) => {
        if (cancelled || isSessionExpiredError(e)) return;
        setFocusMissing(true);
        setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [open, roomId, focusCardId]);

  // hydrating 期间轮询进度直到终态（每次心跳自增 reloadTick 触发上面列表重拉）。
  const hydrating = progress?.status === 'hydrating';
  useEffect(() => {
    if (!open || !hydrating) return;
    const id = window.setInterval(() => setReloadTick((n) => n + 1), HYDRATE_POLL_MS);
    return () => window.clearInterval(id);
  }, [open, hydrating]);

  // ESC 关闭（FactionDrawer 同款）。
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  const onPrev = useCallback(() => setOffset((o) => Math.max(0, o - PAGE_LIMIT)), []);
  const onNext = useCallback(() => setOffset((o) => o + PAGE_LIMIT), []);

  const page = Math.floor(offset / PAGE_LIMIT) + 1;
  const pages = Math.max(1, Math.ceil(matched / PAGE_LIMIT));
  const hasPrev = offset > 0;
  const hasNext = offset + PAGE_LIMIT < matched;

  /** hydrating 进度百分比（done/total；total=0 时 0%）。 */
  const hydratePct = useMemo(() => {
    if (!progress || progress.total <= 0) return 0;
    return Math.min(100, Math.round((progress.done / progress.total) * 100));
  }, [progress]);

  if (!open) return null;

  const listDisabled = hydrating;

  const renderDetail = (p: VirtualCityCityResidentProfile) => (
    <div className="virtualCity-resident-drawer__detail" data-testid="resident-detail">
      {p.opening_hook && (
        <div className="virtualCity-resident-drawer__field">
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.openingHook' as TKey)}
          </span>
          <blockquote className="virtualCity-resident-drawer__hook">「{p.opening_hook}」</blockquote>
        </div>
      )}
      {personalityChips(p.personality).length > 0 && (
        <div className="virtualCity-resident-drawer__field">
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.personality' as TKey)}
          </span>
          <span className="virtualCity-resident-drawer__chips">
            {personalityChips(p.personality).map((chip) => (
              <span key={chip} className="virtualCity-resident-drawer__chip">{chip}</span>
            ))}
          </span>
        </div>
      )}
      {p.goal && (
        <div className="virtualCity-resident-drawer__field">
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.goal' as TKey)}
          </span>
          <span className="virtualCity-resident-drawer__field-v">🎯 {p.goal}</span>
        </div>
      )}
      <div className="virtualCity-resident-drawer__field-row">
        <span className="virtualCity-resident-drawer__field">
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.marital' as TKey)}
          </span>
          <span className="virtualCity-resident-drawer__field-v">{maritalLabel(p.marital, t)}</span>
        </span>
        <span className="virtualCity-resident-drawer__field">
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.healthGrade' as TKey)}
          </span>
          <span className="virtualCity-resident-drawer__field-v">
            {p.health_grade || '—'}
          </span>
        </span>
        <span className="virtualCity-resident-drawer__field">
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.expense' as TKey)}
          </span>
          <span className="virtualCity-resident-drawer__field-v">{formatCny(p.expense)}</span>
        </span>
      </div>
      <div className="virtualCity-resident-drawer__field-row">
        <span className="virtualCity-resident-drawer__field">
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.domain' as TKey)}
          </span>
          <span className="virtualCity-resident-drawer__field-v">{p.domain_name || '—'}</span>
        </span>
        <span className="virtualCity-resident-drawer__field">
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.cardId' as TKey)}
          </span>
          <span className="virtualCity-resident-drawer__field-v">{p.card_id}</span>
        </span>
      </div>
      {p.source_file && (
        <div className="virtualCity-resident-drawer__source" title={p.source_file}>
          <span className="virtualCity-resident-drawer__field-k">
            {t('virtualCity.residentDrawer.sourceFile' as TKey)}
          </span>
          <code className="virtualCity-resident-drawer__source-path">{p.source_file}</code>
        </div>
      )}
    </div>
  );

  return (
    <div
      className="virtualCity-resident-drawer"
      data-testid="resident-profile-drawer"
      role="dialog"
      aria-modal="true"
      aria-label={t('virtualCity.residentDrawer.title' as TKey)}
    >
      <div className="virtualCity-resident-drawer__overlay" onClick={onClose} />
      <aside className="virtualCity-resident-drawer__panel">
        <header className="virtualCity-resident-drawer__header">
          <h3 className="virtualCity-resident-drawer__title">
            👥 {t('virtualCity.residentDrawer.title' as TKey)}
          </h3>
          <button
            type="button"
            className="virtualCity-resident-drawer__close"
            onClick={onClose}
            aria-label={t('virtualCity.residentDrawer.close' as TKey)}
            data-testid="resident-drawer-close"
          >
            ×
          </button>
        </header>

        {/* 锚定进度 / 状态条（hydrating：进度条；failed/idle：灰字说明） */}
        {progress && progress.status !== 'ready' && (
          <div
            className={`virtualCity-resident-drawer__status virtualCity-resident-drawer__status--${progress.status}`}
            data-testid="resident-drawer-status"
          >
            {progress.status === 'hydrating' ? (
              <>
                <span className="virtualCity-resident-drawer__status-text">
                  {t('virtualCity.cityProfiles.anchorProgress' as TKey, {
                    done: progress.done,
                    total: progress.total,
                    pool: progress.pool_size,
                  })}
                </span>
                <span className="virtualCity-resident-drawer__status-bar" role="progressbar" aria-valuenow={hydratePct} aria-valuemin={0} aria-valuemax={100}>
                  <span
                    className="virtualCity-resident-drawer__status-fill"
                    style={{ width: `${hydratePct}%` }}
                  />
                </span>
                <span className="virtualCity-resident-drawer__status-pct">{hydratePct}%</span>
              </>
            ) : progress.status === 'failed' ? (
              <span className="virtualCity-resident-drawer__status-text">
                {t('virtualCity.cityProfiles.anchorFailed' as TKey)}
              </span>
            ) : (
              <span className="virtualCity-resident-drawer__status-text">
                {t('virtualCity.cityProfiles.anchorIdle' as TKey)}
              </span>
            )}
          </div>
        )}
        {progress?.status === 'ready' && (
          <div
            className="virtualCity-resident-drawer__status virtualCity-resident-drawer__status--ready"
            data-testid="resident-drawer-status"
          >
            <span className="virtualCity-resident-drawer__status-text">
              ✓ {t('virtualCity.cityProfiles.anchorReady' as TKey, { n: progress.anchored })}
            </span>
          </div>
        )}

        {/* 搜索框（hydrating 中禁用：禁用态用独立灰底 + not-allowed，不用 opacity） */}
        <div className="virtualCity-resident-drawer__search">
          <input
            type="search"
            value={qInput}
            placeholder={t('virtualCity.residentDrawer.searchPlaceholder' as TKey)}
            onChange={(e) => setQInput(e.target.value)}
            disabled={listDisabled}
            aria-label={t('virtualCity.residentDrawer.searchPlaceholder' as TKey)}
            data-testid="resident-drawer-search"
          />
        </div>

        {error && (
          <div className="virtualCity-resident-drawer__error" role="alert" data-testid="resident-drawer-error">
            ⚠️ {error}
          </div>
        )}

        {/* 城市之声定位卡（focusCardId 命中时置顶展示） */}
        {focusDetail && (
          <div className="virtualCity-resident-drawer__focus">
            <div className="virtualCity-resident-drawer__detail-head">
              <b className="virtualCity-resident-drawer__name">{focusDetail.name}</b>
              <span className="virtualCity-resident-drawer__occ">{focusDetail.occupation}</span>
            </div>
            {renderDetail(focusDetail)}
          </div>
        )}
        {focusMissing && (
          <div className="virtualCity-resident-drawer__notice">
            {t('virtualCity.residentDrawer.notFound' as TKey)}
          </div>
        )}

        {/* 列表主体：hydrating → 进度占位禁用；ready → 分页列表 */}
        <div className="virtualCity-resident-drawer__body">
          {listDisabled ? (
            <div className="virtualCity-resident-drawer__placeholder">
              ⏳ {t('virtualCity.cityProfiles.anchorProgress' as TKey, {
                done: progress?.done ?? 0,
                total: progress?.total ?? 0,
                pool: progress?.pool_size ?? 0,
              })}
            </div>
          ) : residents.length === 0 && !loading ? (
            <div className="virtualCity-resident-drawer__placeholder">
              {t('virtualCity.residentDrawer.empty' as TKey)}
            </div>
          ) : (
            <ul className="virtualCity-resident-drawer__list">
              {residents.map((p) => {
                const isOpen = expanded === p.card_id;
                return (
                  <li
                    key={p.card_id}
                    className={'virtualCity-resident-drawer__item' + (isOpen ? ' is-open' : '')}
                  >
                    <button
                      type="button"
                      className={
                        'virtualCity-resident-drawer__row' + (isOpen ? ' is-open' : '')
                      }
                      onClick={() => setExpanded(isOpen ? null : p.card_id)}
                      aria-expanded={isOpen}
                      data-testid={`resident-row-${p.card_id}`}
                    >
                      <span className="virtualCity-resident-drawer__row-main">
                        <b className="virtualCity-resident-drawer__name">{p.name}</b>
                        <span className="virtualCity-resident-drawer__occ">{p.occupation}</span>
                      </span>
                      <span className="virtualCity-resident-drawer__row-meta">
                        <span className="virtualCity-resident-drawer__meta-item">
                          {t('virtualCity.residentDrawer.age' as TKey)} {p.age}
                        </span>
                        <span className="virtualCity-resident-drawer__meta-item">{p.district}</span>
                        <span className="virtualCity-resident-drawer__meta-item">
                          {t('virtualCity.residentDrawer.income' as TKey)} {formatCny(p.income)}
                        </span>
                        <span className="virtualCity-resident-drawer__meta-item">
                          {t('virtualCity.residentDrawer.savings' as TKey)} {formatCny(p.savings)}
                        </span>
                        <span
                          className={
                            p.employed
                              ? 'virtualCity-resident-drawer__badge virtualCity-resident-drawer__badge--employed'
                              : 'virtualCity-resident-drawer__badge virtualCity-resident-drawer__badge--unemployed'
                          }
                        >
                          {p.employed
                            ? t('virtualCity.residentDrawer.employed' as TKey)
                            : t('virtualCity.residentDrawer.unemployed' as TKey)}
                        </span>
                        {p.stressed && (
                          <span className="virtualCity-resident-drawer__badge virtualCity-resident-drawer__badge--stressed">
                            {t('virtualCity.residentDrawer.stressed' as TKey)}
                          </span>
                        )}
                      </span>
                    </button>
                    {isOpen && renderDetail(p)}
                  </li>
                );
              })}
            </ul>
          )}
          {loading && (
            <div className="virtualCity-resident-drawer__notice">
              {t('common.loading' as TKey)}
            </div>
          )}
        </div>

        {/* 分页脚条 */}
        <footer className="virtualCity-resident-drawer__footer">
          <span className="virtualCity-resident-drawer__matched">
            {t('virtualCity.residentDrawer.matched' as TKey, { n: matched })}
          </span>
          <span className="virtualCity-resident-drawer__pager">
            <button
              type="button"
              className="virtualCity-resident-drawer__btn"
              onClick={onPrev}
              disabled={!hasPrev || listDisabled}
              data-testid="resident-drawer-prev"
            >
              {t('virtualCity.residentDrawer.prev' as TKey)}
            </button>
            <span className="virtualCity-resident-drawer__pageinfo">
              {t('virtualCity.residentDrawer.pageInfo' as TKey, { page, pages })}
            </span>
            <button
              type="button"
              className="virtualCity-resident-drawer__btn"
              onClick={onNext}
              disabled={!hasNext || listDisabled}
              data-testid="resident-drawer-next"
            >
              {t('virtualCity.residentDrawer.next' as TKey)}
            </button>
          </span>
        </footer>
      </aside>
    </div>
  );
}

export default ResidentProfileDrawer;
