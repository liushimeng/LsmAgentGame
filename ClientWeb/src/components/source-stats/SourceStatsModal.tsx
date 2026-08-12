import { useEffect, useMemo, useState } from 'react';
import { sourceStatsService } from '@/services/auth.service';
import { useT } from '@/hooks/useT';
import { reportGlobalError } from '@/services/globalError';
import type { SourceStatsPayload } from '@/types/api';

interface Props {
  open: boolean;
  onClose: () => void;
}

// SourceStatsModal —— 标题栏「📊 源码统计」按钮触发的弹窗。
// 数据源:GET /api/source-stats(公开接口,服务端在启动期扫描目录)。
//
// 展示:
//   1. 顶部 4 张统计卡片 —— 前端/后端/总计 + 总文件/总行数/总字节数。
//   2. 主体表格 —— 按扩展名分组的文件数 / 行数 / 字节数(按字节数倒序)。
//   3. 错误兜底 —— 单组扫描失败不阻塞其他组,在卡片上展示 ⚠。
//
// 交互:
//   - Esc 关闭 + 点击遮罩关闭(与 Wiki / GitLog 弹窗保持一致)。
//   - 手动刷新按钮:重新拉取最新数据(开发期常用,源文件持续修改)。
export function SourceStatsModal({ open, onClose }: Props) {
  const t = useT();

  // ---- 数据 / 加载 / 错误 ----
  const [data, setData] = useState<SourceStatsPayload | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // 用于「刷新」按钮强行触发重拉
  const [refreshTick, setRefreshTick] = useState(0);

  // ---- 打开弹窗时拉取一次 + 重置状态 ----
  useEffect(() => {
    if (!open) return;
    setLoading(true);
    setError(null);
    let alive = true;
    sourceStatsService
      .get()
      .then((payload) => {
        if (!alive) return;
        setData(payload);
      })
      .catch((e: Error) => {
        if (!alive) return;
        const msg = e.message || 'load failed';
        setError(msg);
        reportGlobalError({ message: msg, severity: 'error' });
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [open, refreshTick]);

  // Esc 关闭
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  // 显示用的衍生数据 —— 必须在 early return 之前(避免 React #310)。
  const totalCard = useMemo(() => {
    if (!data) return null;
    return data.total;
  }, [data]);

  // 早期 return —— 必须在所有 hook 之后
  if (!open) return null;

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal sourcestats-modal" onClick={(e) => e.stopPropagation()}>
        <h2>
          <span>📊 {t('sourceStats.title')}</span>
          <button
            className="sourcestats-modal__close"
            onClick={onClose}
            title="Esc"
            disabled={loading}
          >
            {t('gitLog.close')} ✕
          </button>
        </h2>

        {/* 顶部工具栏:刷新按钮(开发期 / 频繁改源码时使用) */}
        <div className="sourcestats-toolbar">
          <span className="sourcestats-toolbar__hint">
            {t('sourceStats.builtAt', {
              time: data?.built_at ? formatTime(data.built_at) : '—',
            })}
          </span>
          <button
            className="sourcestats-toolbar__btn"
            onClick={() => setRefreshTick((n) => n + 1)}
            disabled={loading}
            title={t('gitLog.refresh')}
          >
            {loading ? <span className="gitlog-spinner" /> : '↻'} {t('gitLog.refresh')}
          </button>
        </div>

        {error && <div className="error">{t('gitLog.loadError', { msg: error })}</div>}

        {/* 加载态:显示骨架占位 */}
        {loading && !data && (
          <div className="sourcestats-loading">
            <span className="gitlog-spinner" /> {t('gitLog.loading')}
          </div>
        )}

        {data && (
          <>
            {/* 顶部卡片:前端 / 后端 / 总计 */}
            <div className="sourcestats-cards">
              {data.groups.map((g) => (
                <GroupCard key={g.name} group={g} t={t} />
              ))}
              {totalCard && <GroupCard group={totalCard} t={t} highlight />}
            </div>

            {/* 主体:扩展名分布表 */}
            <div className="sourcestats-body">
              <table className="sourcestats-table">
                <colgroup>
                  <col className="sourcestats-col-ext" />
                  <col className="sourcestats-col-num" />
                  <col className="sourcestats-col-num" />
                  <col className="sourcestats-col-num" />
                  <col className="sourcestats-col-bar" />
                </colgroup>
                <thead>
                  <tr>
                    <th>{t('sourceStats.ext')}</th>
                    <th style={{ textAlign: 'right' }}>{t('sourceStats.files')}</th>
                    <th style={{ textAlign: 'right' }}>{t('sourceStats.lines')}</th>
                    <th style={{ textAlign: 'right' }}>{t('sourceStats.bytes')}</th>
                    <th>{t('sourceStats.bar')}</th>
                  </tr>
                </thead>
                <tbody>
                  {data.extensions.length === 0 ? (
                    <tr>
                      <td
                        colSpan={5}
                        style={{ padding: 24, textAlign: 'center', color: 'var(--muted)' }}
                      >
                        {t('gitLog.empty')}
                      </td>
                    </tr>
                  ) : (
                    data.extensions.map((e) => (
                      <ExtRow
                        key={e.ext}
                        ext={e.ext}
                        files={e.files}
                        lines={e.lines}
                        bytes={e.bytes}
                        maxBytes={data.extensions[0]?.bytes || 0}
                      />
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// 单组卡片(前端/后端/总计)
function GroupCard({
  group,
  t,
  highlight,
}: {
  group: { name: string; files: number; lines: number; bytes: number; error?: string };
  t: (k: any, vars?: Record<string, string | number>) => string;
  highlight?: boolean;
}) {
  return (
    <div
      className={
        'sourcestats-card' + (highlight ? ' sourcestats-card--total' : '')
      }
    >
      <div className="sourcestats-card__title">{group.name}</div>
      {group.error ? (
        <div className="sourcestats-card__error" title={group.error}>
          ⚠ {t('sourceStats.error')}
        </div>
      ) : (
        <>
          <div className="sourcestats-card__row">
            <span className="sourcestats-card__num">{group.files.toLocaleString()}</span>
            <span className="sourcestats-card__label">{t('sourceStats.files')}</span>
          </div>
          <div className="sourcestats-card__row">
            <span className="sourcestats-card__num">{group.lines.toLocaleString()}</span>
            <span className="sourcestats-card__label">{t('sourceStats.lines')}</span>
          </div>
          <div className="sourcestats-card__row">
            <span className="sourcestats-card__num">{formatBytes(group.bytes)}</span>
            <span className="sourcestats-card__label">{t('sourceStats.bytes')}</span>
          </div>
        </>
      )}
    </div>
  );
}

// 单行扩展名 + 文件数/行数/字节数 + 相对柱状条
function ExtRow({
  ext,
  files,
  lines,
  bytes,
  maxBytes,
}: {
  ext: string;
  files: number;
  lines: number;
  bytes: number;
  maxBytes: number;
}) {
  const pct = maxBytes > 0 ? Math.round((bytes / maxBytes) * 100) : 0;
  return (
    <tr>
      <td className="sourcestats-cell-ext">
        <span className="sourcestats-ext-badge">{ext}</span>
      </td>
      <td className="sourcestats-cell-num" style={{ textAlign: 'right' }}>
        {files.toLocaleString()}
      </td>
      <td className="sourcestats-cell-num" style={{ textAlign: 'right' }}>
        {lines.toLocaleString()}
      </td>
      <td className="sourcestats-cell-num" style={{ textAlign: 'right' }}>
        {formatBytes(bytes)}
      </td>
      <td className="sourcestats-cell-bar">
        <div className="sourcestats-bar">
          <div
            className="sourcestats-bar__fill"
            style={{ width: pct + '%' }}
            aria-label={pct + '%'}
          />
        </div>
      </td>
    </tr>
  );
}

// 字节数 → 人类可读
function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

// formatTime 把 ISO/RFC3339 字符串压缩成 YYYY-MM-DD HH:MM,本地时区。
function formatTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}