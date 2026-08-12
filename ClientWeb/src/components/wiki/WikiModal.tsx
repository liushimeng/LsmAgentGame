import { useEffect, useMemo, useState } from 'react';
import { wikiService } from '@/services/auth.service';
import { useT } from '@/hooks/useT';
import type { WikiContentPayload, WikiEntry } from '@/types/api';

interface Props {
  open: boolean;
  onClose: () => void;
}

// WikiModal —— 标题栏「📚 Wiki」按钮触发的弹窗。
// 左侧是文档列表（按文件名排序，附 mtime / size / excerpt 摘要）；
// 右侧是当前选中文档的 markdown 文本（轻量渲染：保留原文 + 等宽 pre，
// 不引入 marked/remark 等重型依赖——与 GitLogModal 风格保持一致）。
//
// 交互要点：
//   1. 打开时强制拉一次 list，列表项点击触发按需 content。
//   2. Esc 关闭；点击遮罩关闭；选中项二次点击不切换（在右侧顶部提供"返回列表"按钮）。
//   3. 单文档加载失败 / 太大时给出友好提示，不阻塞其他文档浏览。
export function WikiModal({ open, onClose }: Props) {
  const t = useT();

  // ---- 列表 / 加载状态 ----
  const [entries, setEntries] = useState<WikiEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');

  // ---- 当前选中文档 ----
  const [selected, setSelected] = useState<string | null>(null);
  const [content, setContent] = useState<string>('');
  const [contentLoading, setContentLoading] = useState(false);
  const [contentError, setContentError] = useState<string | null>(null);

  // 打开弹窗时拉一次列表 + 重置状态。
  useEffect(() => {
    if (!open) return;
    setSelected(null);
    setContent('');
    setContentError(null);
    setError(null);
    setSearch('');
    setLoading(true);
    let alive = true;
    wikiService
      .list()
      .then((data) => {
        if (!alive) return;
        setEntries(data.entries);
      })
      .catch((e: Error) => {
        if (!alive) return;
        setError(e.message || 'load failed');
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [open]);

  // Esc 关闭
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  // 选中某条目时按需加载内容
  useEffect(() => {
    if (!open || !selected) return;
    let alive = true;
    setContentLoading(true);
    setContentError(null);
    setContent('');
    wikiService
      .content(selected)
      .then((data: WikiContentPayload) => {
        if (!alive) return;
        setContent(data.content);
      })
      .catch((e: Error) => {
        if (!alive) return;
        setContentError(e.message || 'load content failed');
      })
      .finally(() => alive && setContentLoading(false));
    return () => {
      alive = false;
    };
  }, [open, selected]);

  // 搜索过滤（基于标题 + 文件名 + 摘要）
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter(
      (e) =>
        e.title.toLowerCase().includes(q) ||
        e.name.toLowerCase().includes(q) ||
        e.excerpt.toLowerCase().includes(q),
    );
  }, [entries, search]);

  // 当前选中条目的元信息（用于右侧顶部展示）
  const currentEntry = useMemo(
    () => entries.find((e) => e.name === selected) || null,
    [entries, selected],
  );

  // 早期 return 必须在所有 hook 之后，避免 React #310。
  if (!open) return null;

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal wiki-modal" onClick={(e) => e.stopPropagation()}>
        <h2>
          <span>📚 Wiki</span>
          <button className="wiki-modal__close" onClick={onClose} title="Esc">
            {t('gitLog.close')} ✕
          </button>
        </h2>

        {error && <div className="error">{error}</div>}

        <div className="wiki-modal__body">
          {/* 左侧：文档列表 */}
          <div className="wiki-modal__sidebar">
            <input
              className="wiki-modal__search"
              type="search"
              value={search}
              placeholder={t('gitLog.searchPlaceholder')}
              onChange={(e) => setSearch(e.target.value)}
              aria-label={t('gitLog.search')}
            />
            <div className="wiki-modal__list">
              {loading ? (
                <div className="wiki-modal__empty">
                  <span className="gitlog-spinner" /> {t('gitLog.loading')}
                </div>
              ) : filtered.length === 0 ? (
                <div className="wiki-modal__empty">{t('gitLog.empty')}</div>
              ) : (
                filtered.map((e) => (
                  <button
                    key={e.name}
                    className={
                      'wiki-modal__item' +
                      (selected === e.name ? ' is-active' : '')
                    }
                    onClick={() => setSelected(e.name)}
                    title={e.name}
                  >
                    <div className="wiki-modal__item-title">{e.title}</div>
                    <div className="wiki-modal__item-meta">
                      <span>{formatSize(e.size)}</span>
                      <span>·</span>
                      <span>{formatTime(e.mtime)}</span>
                    </div>
                    {e.excerpt && (
                      <div className="wiki-modal__item-excerpt">{e.excerpt}</div>
                    )}
                  </button>
                ))
              )}
            </div>
            <div className="wiki-modal__sidebar-foot">
              共 <strong>{filtered.length}</strong> 篇
              {search && ` / 全部 ${entries.length} 篇`}
            </div>
          </div>

          {/* 右侧：内容区 */}
          <div className="wiki-modal__content">
            {!selected ? (
              <div className="wiki-modal__placeholder">
                ← 请从左侧选择一个文档
              </div>
            ) : (
              <>
                <div className="wiki-modal__content-head">
                  <button
                    className="wiki-modal__back"
                    onClick={() => setSelected(null)}
                    title="返回列表"
                  >
                    ← 列表
                  </button>
                  <div className="wiki-modal__content-title">
                    {currentEntry ? currentEntry.title : selected}
                    {currentEntry && (
                      <span className="wiki-modal__content-name">
                        {currentEntry.name}
                      </span>
                    )}
                  </div>
                </div>
                <div className="wiki-modal__content-body">
                  {contentLoading ? (
                    <div className="wiki-modal__empty">
                      <span className="gitlog-spinner" />{' '}
                      {t('gitLog.loading')}
                    </div>
                  ) : contentError ? (
                    <div className="error">{contentError}</div>
                  ) : (
                    <pre className="wiki-modal__markdown">{content}</pre>
                  )}
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// formatSize 字节数 → 人类可读。
function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

// formatTime 把 RFC3339 字符串压缩成 YYYY-MM-DD HH:MM,本地时区。
function formatTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}