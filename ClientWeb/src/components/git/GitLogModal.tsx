import { useEffect, useMemo, useRef, useState } from 'react';
import { gitLogService } from '@/services/auth.service';
import { useT } from '@/hooks/useT';
import type { CommitDetail, CommitEntry } from '@/types/api';

interface Props {
  open: boolean;
  onClose: () => void;
}

// 弹窗宽度比默认 .modal 宽,容纳分页表格 + 工具条 + 摘要;
// 高度受 max-height + flex 布局约束,避免大数据量撑爆页面。
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const;

// GitLogModal —— 标题栏"提交记录"按钮触发的弹窗。
// 功能:
//   1. 分页大小可切换(10/20/50/100),数据从后端 /api/git/log?skip&limit 拉取。
//   2. 摘要条:本批提交数 / 涉及文件 / +/- 行数合计。
//   3. 搜索框:按 commit message / author 关键字过滤(前端过滤,不影响后端分页)。
//   4. 行点击展开/折叠:展开后展示本提交涉及文件的行数变化。
//   5. 全部展开/全部折叠 / 刷新按钮。
//   6. 分页器:上一页/下一页 + 页码(带省略号)+ 跳页。
export function GitLogModal({ open, onClose }: Props) {
  const t = useT();

  // ---- 列表 / 分页 / 加载状态 ----
  const [pageSize, setPageSize] = useState<number>(10);
  const [page, setPage] = useState(0);
  const [entries, setEntries] = useState<CommitEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // ---- 展开/折叠 ----
  // expanded 用 Set 记录当前页内展开的提交 id,支持"全部展开/全部折叠"批量操作。
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [detailMap, setDetailMap] = useState<Record<string, CommitDetail | undefined>>({});
  const [detailLoading, setDetailLoading] = useState<Set<string>>(new Set());

  // ---- 搜索 ----
  const [search, setSearch] = useState('');

  // 打开弹窗时强制回到第 0 页 + 清空展开状态,避免与上次停留的页码串味。
  useEffect(() => {
    if (!open) return;
    setPage(0);
    setExpanded(new Set());
    setDetailMap({});
    setSearch('');
  }, [open]);

  // Esc 关闭弹窗
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  // 当前页数据拉取。错误时把上一页数据保留 + 顶部展示错误条。
  useEffect(() => {
    if (!open) return;
    let alive = true;
    setLoading(true);
    setError(null);
    gitLogService
      .list(page * pageSize, pageSize)
      .then((data) => {
        if (!alive) return;
        setEntries(data.entries);
        setTotal(data.total);
        // 翻页后清空展开状态,避免展开行 id 不在新页里。
        setExpanded(new Set());
        setDetailMap({});
      })
      .catch((e: Error) => {
        if (!alive) return;
        setError(e.message || 'load failed');
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [open, page, pageSize]);

  // 切换分页大小时回到第 0 页(避免当前页在新 size 下越界)。
  const handlePageSizeChange = (next: number) => {
    setPageSize(next);
    setPage(0);
  };

  // 手动刷新
  const refresh = () => {
    setExpanded(new Set());
    setDetailMap({});
    // 通过 setState 触发同 useEffect 重跑:把 page 当作依赖,这里设一个
    // 临时 state 强制重新拉取最简。
    setPage((p) => p);
  };

  // 展开/折叠单项
  const toggleExpand = (id: string) => {
    setExpanded((cur) => {
      const next = new Set(cur);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  // 全部展开 / 全部折叠
  const expandAll = () => {
    setExpanded(new Set(filtered.map((e) => e.id)));
  };
  const collapseAll = () => {
    setExpanded(new Set());
  };

  // 展开行时按需加载详情,带内存缓存避免重复点击。
  const detailCache = useRef(new Map<string, CommitDetail>());
  useEffect(() => {
    if (!open) return;
    if (expanded.size === 0) return;
    const toLoad: string[] = [];
    expanded.forEach((id) => {
      if (!detailCache.current.has(id) && !detailMap[id]) toLoad.push(id);
    });
    if (toLoad.length === 0) return;
    let alive = true;
    setDetailLoading((cur) => {
      const next = new Set(cur);
      toLoad.forEach((id) => next.add(id));
      return next;
    });
    Promise.all(
      toLoad.map((id) =>
        gitLogService.detail(id).then((d) => {
          detailCache.current.set(d.id, d);
          return d;
        }),
      ),
    )
      .then((details) => {
        if (!alive) return;
        setDetailMap((cur) => {
          const next = { ...cur };
          details.forEach((d) => {
            next[d.id] = d;
          });
          return next;
        });
      })
      .catch((e: Error) => alive && setError(e.message || 'detail failed'))
      .finally(() => {
        if (!alive) return;
        setDetailLoading((cur) => {
          const next = new Set(cur);
          toLoad.forEach((id) => next.delete(id));
          return next;
        });
      });
    return () => {
      alive = false;
    };
  }, [expanded, detailMap, open]);

  // 搜索过滤(对当前页 entries)—— 必须放在 early return 之前,避免 React #310
  // "Rendered more hooks than during the previous render"。hook 顺序需保持稳定。
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter(
      (e) => e.subject.toLowerCase().includes(q) || e.author.toLowerCase().includes(q),
    );
  }, [entries, search]);

  // 本页汇总(基于过滤后的列表)
  const summary = useMemo(() => {
    let files = 0;
    let adds = 0;
    let dels = 0;
    filtered.forEach((e) => {
      files += e.files_changed;
      adds += e.insertions;
      dels += e.deletions;
    });
    return { commits: filtered.length, files, adds, dels };
  }, [filtered]);

  // 早期 return 放在所有 hook 之后,确保 hook 调用顺序稳定。
  if (!open) return null;

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal gitlog-modal" onClick={(e) => e.stopPropagation()}>
        <h2>
          <span>
            {t('gitLog.title')}
            {search && (
              <span
                style={{ marginLeft: 8, fontSize: 12, color: 'var(--muted)', fontWeight: 400 }}
              >
                {t('gitLog.filtered', { n: filtered.length })}
              </span>
            )}
          </span>
          <button className="gitlog-modal__close" onClick={onClose} title="Esc">
            {t('gitLog.close')} ✕
          </button>
        </h2>

        {error && <div className="error">{t('gitLog.loadError', { msg: error })}</div>}

        {/* 工具条:分页大小 + 搜索 + 全部展开/折叠 + 刷新 */}
        <div className="gitlog-toolbar">
          <div className="gitlog-toolbar__group">
            <span className="gitlog-toolbar__label">{t('gitLog.pageSize')}</span>
            <select
              className="gitlog-toolbar__pagesize"
              value={pageSize}
              onChange={(e) => handlePageSizeChange(Number(e.target.value))}
            >
              {PAGE_SIZE_OPTIONS.map((n) => (
                <option key={n} value={n}>
                  {n} {t('gitLog.pageSizeUnit')}
                </option>
              ))}
            </select>
          </div>

          <input
            className="gitlog-toolbar__search"
            type="search"
            value={search}
            placeholder={t('gitLog.searchPlaceholder')}
            onChange={(e) => setSearch(e.target.value)}
            aria-label={t('gitLog.search')}
          />

          <button
            className="gitlog-toolbar__btn"
            onClick={expanded.size > 0 ? collapseAll : expandAll}
            disabled={filtered.length === 0}
            title={expanded.size > 0 ? t('gitLog.collapseAll') : t('gitLog.expandAll')}
          >
            {expanded.size > 0 ? `▼ ${t('gitLog.collapseAll')}` : `▶ ${t('gitLog.expandAll')}`}
          </button>

          <button
            className="gitlog-toolbar__btn"
            onClick={refresh}
            disabled={loading}
            title={t('gitLog.refresh')}
          >
            {loading ? <span className="gitlog-spinner" /> : '↻'} {t('gitLog.refresh')}
          </button>
        </div>

        {/* 摘要条 */}
        {filtered.length > 0 && (
          <div className="gitlog-summary">
            <span className="gitlog-summary__chip">
              <strong>{summary.commits}</strong> {t('gitLog.summary').split('{commits}')[1]?.split('}')[0] || ''}
            </span>
            <span className="gitlog-summary__chip">
              {t('gitLog.fileSummary', {
                n: summary.files,
                adds: summary.adds,
                dels: summary.dels,
              })}
            </span>
          </div>
        )}

        {/* 主体表格 */}
        <div
          className={`gitlog-body ${loading && entries.length === 0 ? 'gitlog-body--empty' : ''}`}
        >
          {loading && entries.length === 0 ? (
            <>
              <span className="gitlog-spinner" /> {t('gitLog.loading')}
            </>
          ) : entries.length === 0 ? (
            <>{t('gitLog.empty')}</>
          ) : (
            <table className="gitlog-table">
              <colgroup>
                <col className="gitlog-col-expand" />
                <col className="gitlog-col-id" />
                <col className="gitlog-col-time" />
                <col />
                <col className="gitlog-col-files" />
                <col className="gitlog-col-add" />
                <col className="gitlog-col-del" />
              </colgroup>
              <thead>
                <tr>
                  <th></th>
                  <th>{t('gitLog.id')}</th>
                  <th>{t('gitLog.time')}</th>
                  <th>{t('gitLog.message')}</th>
                  <th style={{ textAlign: 'right' }}>{t('gitLog.files')}</th>
                  <th style={{ textAlign: 'right' }}>{t('gitLog.add')}</th>
                  <th style={{ textAlign: 'right' }}>{t('gitLog.del')}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.length === 0 ? (
                  <tr>
                    <td
                      colSpan={7}
                      style={{ padding: 24, textAlign: 'center', color: 'var(--muted)' }}
                    >
                      {t('gitLog.empty')}
                    </td>
                  </tr>
                ) : (
                  filtered.map((e) => {
                    const isOpen = expanded.has(e.id);
                    return (
                      <RowGroup
                        key={e.id}
                        entry={e}
                        expanded={isOpen}
                        detail={detailMap[e.id] || null}
                        detailLoading={detailLoading.has(e.id)}
                        onToggle={() => toggleExpand(e.id)}
                        t={t}
                      />
                    );
                  })
                )}
              </tbody>
            </table>
          )}
        </div>

        {/* 分页器 */}
        <div className="gitlog-pagination">
          <span className="gitlog-pagination__info">
            {t('gitLog.pageInfo', { page: page + 1, total: totalPages, count: total })}
          </span>
          <div className="gitlog-pagination__controls">
            <button
              className="gitlog-pagination__pagebtn"
              onClick={() => setPage(0)}
              disabled={page === 0 || loading}
              title="«"
            >
              «
            </button>
            <button
              className="gitlog-pagination__pagebtn"
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={page === 0 || loading}
            >
              ‹
            </button>
            {renderPageNumbers(page, totalPages, setPage, loading, t)}
            <button
              className="gitlog-pagination__pagebtn"
              onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              disabled={page >= totalPages - 1 || loading}
            >
              ›
            </button>
            <button
              className="gitlog-pagination__pagebtn"
              onClick={() => setPage(totalPages - 1)}
              disabled={page >= totalPages - 1 || loading}
              title="»"
            >
              »
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// 渲染分页页码按钮(带省略号)。
// totalPages <= 7 时全部展示;否则保留首页/末页 + 当前页前后 1 个 + 省略号。
function renderPageNumbers(
  current: number,
  total: number,
  setPage: (p: number) => void,
  loading: boolean,
  _t: (k: any) => string,
) {
  if (total <= 1) return null;
  if (total <= 7) {
    return Array.from({ length: total }, (_, i) => pageBtn(i, i, current, setPage, loading));
  }
  const pages: (number | 'ellipsis')[] = [];
  pages.push(0);
  if (current > 2) pages.push('ellipsis');
  for (let i = Math.max(1, current - 1); i <= Math.min(total - 2, current + 1); i++) {
    pages.push(i);
  }
  if (current < total - 3) pages.push('ellipsis');
  pages.push(total - 1);
  return pages.map((p, idx) =>
    typeof p === 'number' ? (
      pageBtn(p, idx, current, setPage, loading)
    ) : (
      <span key={`e-${idx}`} className="gitlog-pagination__ellipsis">
        …
      </span>
    ),
  );
}

function pageBtn(
  pageIdx: number,
  key: number | string,
  current: number,
  setPage: (p: number) => void,
  loading: boolean,
) {
  const isActive = pageIdx === current;
  return (
    <button
      key={key}
      className={`gitlog-pagination__pagebtn ${isActive ? 'is-active' : ''}`}
      onClick={() => !isActive && setPage(pageIdx)}
      disabled={isActive || loading}
    >
      {pageIdx + 1}
    </button>
  );
}

interface RowGroupProps {
  entry: CommitEntry;
  expanded: boolean;
  detail: CommitDetail | null;
  detailLoading: boolean;
  onToggle: () => void;
  t: (key: any, vars?: Record<string, string | number>) => string;
}

// 拆出子组件避免主组件在每次展开切换时重渲染整张表格。
function RowGroup({ entry, expanded, detail, detailLoading, onToggle, t }: RowGroupProps) {
  return (
    <>
      <tr
        onClick={onToggle}
        className={expanded ? 'is-expanded' : ''}
        style={{ cursor: 'pointer' }}
        title={t('gitLog.clickToExpand')}
      >
        <td className="gitlog-cell-expand" onClick={(e) => { e.stopPropagation(); onToggle(); }}>
          <span className={expanded ? 'is-open' : ''}>▶</span>
        </td>
        <td className="gitlog-cell-id">{entry.short_id || entry.id.slice(0, 7)}</td>
        <td className="gitlog-cell-time">{formatTime(entry.time)}</td>
        <td className="gitlog-cell-msg">
          <div className="gitlog-cell-msg__subject">{entry.subject}</div>
          <div className="gitlog-cell-msg__author">{entry.author}</div>
        </td>
        <td className="gitlog-cell-num" style={{ textAlign: 'right' }}>
          {entry.files_changed}
        </td>
        <td
          className={`gitlog-cell-num ${entry.insertions > 0 ? 'gitlog-cell-num--ok' : 'gitlog-cell-num--zero'}`}
          style={{ textAlign: 'right' }}
        >
          {entry.insertions > 0 ? `+${entry.insertions}` : entry.insertions}
        </td>
        <td
          className={`gitlog-cell-num ${entry.deletions > 0 ? 'gitlog-cell-num--danger' : 'gitlog-cell-num--zero'}`}
          style={{ textAlign: 'right' }}
        >
          {entry.deletions > 0 ? `-${entry.deletions}` : entry.deletions}
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={7} style={{ padding: 0 }}>
            <div className="gitlog-detail">
              {detailLoading ? (
                <div className="gitlog-detail__empty">
                  <span className="gitlog-spinner" /> {t('gitLog.loadingDetail')}
                </div>
              ) : detail && detail.files.length > 0 ? (
                <>
                  <div className="gitlog-detail__header">
                    <span>
                      {t('gitLog.fileSummary', {
                        n: detail.files.length,
                        adds: detail.files.reduce((s, f) => s + f.insertions, 0),
                        dels: detail.files.reduce((s, f) => s + f.deletions, 0),
                      })}
                    </span>
                  </div>
                  <table className="gitlog-detail__files">
                    <colgroup>
                      <col />
                      <col style={{ width: 70 }} />
                      <col style={{ width: 70 }} />
                    </colgroup>
                    <thead>
                      <tr>
                        <th>{t('gitLog.file')}</th>
                        <th style={{ textAlign: 'right' }}>{t('gitLog.add')}</th>
                        <th style={{ textAlign: 'right' }}>{t('gitLog.del')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {detail.files.map((f) => {
                        const isNew = f.insertions > 0 && f.deletions === 0;
                        const isDel = f.deletions > 0 && f.insertions === 0;
                        return (
                          <tr key={f.path}>
                            <td
                              className={`gitlog-detail__path ${isNew ? 'gitlog-detail__path-added' : ''} ${isDel ? 'gitlog-detail__path-deleted' : ''}`}
                              title={f.path}
                            >
                              {f.path}
                            </td>
                            <td
                              className={`gitlog-cell-num ${f.insertions > 0 ? 'gitlog-cell-num--ok' : 'gitlog-cell-num--zero'}`}
                            >
                              {f.insertions > 0 ? `+${f.insertions}` : f.insertions}
                            </td>
                            <td
                              className={`gitlog-cell-num ${f.deletions > 0 ? 'gitlog-cell-num--danger' : 'gitlog-cell-num--zero'}`}
                            >
                              {f.deletions > 0 ? `-${f.deletions}` : f.deletions}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </>
              ) : (
                <div className="gitlog-detail__empty">{t('gitLog.noFiles')}</div>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

// formatTime 把 ISO 字符串压缩成 YYYY-MM-DD HH:MM 形式,使用本地时区。
function formatTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
