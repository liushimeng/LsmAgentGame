/**
 * SnapshotPanel — 批次 86 看板第 14 维度「存档管理」。
 *
 * 定位：把「存档 / 读档 / 删除 / 回滚最近一次注入」变成面板内可操作的高风险
 * 入口（方案 §3.2 / §5）。四项操作全部走 HTTP（§4.1）：
 *   ① 存档  —— POST /snapshot（reason=manual）
 *   ② 读档  —— POST /snapshot/restore（confirm → 覆盖当前城市状态）
 *   ③ 删除  —— DELETE /snapshot/:id（confirm → 不可恢复）
 *   ④ 回滚  —— 找最近一条 `result_outcome=success` 且带 `snapshot_id` 的留痕，
 *              用其关联快照调 restore（与后端 RollbackLastScenario 同口径；
 *              快照是注入**之前**拍的 ⇒ 精确回到注入前状态）。
 *
 * 口径纪律：
 *   - **空态 / 缺失绝不白屏**：无快照 ⇒ 「暂无快照」；无回滚目标 ⇒ 回滚按钮禁用 + 原因；
 *   - **未知用途回落原字符串**（reason 枚举）；
 *   - **§7.1**：所有失败一律面板内联红条 + reportGlobalError 上报；
 *   - **高风险操作二次确认**（ConfirmModal，不用 window.confirm —— 会阻塞 CDP）；
 *   - **观战态只读**：`readOnly` ⇒ 全部写操作禁用（title 明示原因，不用 opacity）。
 */

import { useCallback, useEffect, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  deleteVirtualCitySnapshot,
  fetchVirtualCityEventLifecycle,
  fetchVirtualCitySnapshots,
  restoreVirtualCitySnapshot,
  snapshotVirtualCityRoom,
} from '@/api/virtualCity';
import { errorMessage, reportGlobalError } from '@/services/globalError';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import type { VirtualCitySnapshotMeta } from '@/types/virtualCity';

/** 快照用途 → i18n 键（未知回落原字符串）。 */
const REASON_KEYS: Record<string, TKey> = {
  manual: 'virtualCity.dash.snapshot.reason.manual' as TKey,
  auto_rollback: 'virtualCity.dash.snapshot.reason.auto_rollback' as TKey,
  recovery: 'virtualCity.dash.snapshot.reason.recovery' as TKey,
  replay: 'virtualCity.dash.snapshot.reason.replay' as TKey,
};

/** 二次确认的动作。 */
type ConfirmAction = 'restore' | 'delete' | 'rollback';

/** 二次确认状态（null = 未确认）。 */
interface ConfirmState {
  action: ConfirmAction;
  snapshotId: string;
  message: string;
}

/** 时间戳 → 本地时间字符串；非法 ⇒ 「—」。 */
function fmtTime(ts: number | undefined | null): string {
  if (typeof ts !== 'number' || !Number.isFinite(ts) || ts <= 0) return '—';
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

/** 字节 → 人读（B / KB / MB）；非法 ⇒ 「—」。 */
function fmtBytes(n: number | undefined | null): string {
  if (typeof n !== 'number' || !Number.isFinite(n) || n < 0) return '—';
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(2)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${n} B`;
}

/** 截断长哈希 / id。 */
function shortHash(s: string | undefined | null): string {
  if (!s) return '—';
  if (s.length <= 18) return s;
  return `${s.slice(0, 9)}…${s.slice(-7)}`;
}

export interface SnapshotPanelProps {
  /** 房间 id。 */
  roomId: string;
  /** 只读态（观战 / 服务端视角无本人座位）⇒ 禁用全部写操作。 */
  readOnly: boolean;
}

export function SnapshotPanel({ roomId, readOnly }: SnapshotPanelProps) {
  const t = useT();

  const [snapshots, setSnapshots] = useState<VirtualCitySnapshotMeta[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [rollingBack, setRollingBack] = useState(false);
  const [confirm, setConfirm] = useState<ConfirmState | null>(null);

  /** 回滚目标（最近一条 success + 带快照的留痕）。 */
  const [rollbackTarget, setRollbackTarget] = useState<{
    traceId: string;
    snapshotId: string;
  } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const r = await fetchVirtualCitySnapshots(roomId);
      setSnapshots(Array.isArray(r?.snapshots) ? r.snapshots : []);
    } catch (e) {
      setError(
        t('virtualCity.dash.snapshot.listFailed' as TKey, {
          msg: errorMessage(e, 'list failed'),
        }),
      );
      reportGlobalError(e);
    } finally {
      setLoading(false);
    }
  }, [roomId, t]);

  /** 回滚目标探测（最近一条 success 留痕的关联快照）。 */
  const probeRollback = useCallback(async () => {
    try {
      const r = await fetchVirtualCityEventLifecycle(roomId);
      const traces = Array.isArray(r?.traces) ? r.traces : [];
      const hit = traces.find(
        (tr) => tr.result_outcome === 'success' && !!tr.snapshot_id,
      );
      setRollbackTarget(
        hit && hit.snapshot_id
          ? { traceId: hit.trace_id, snapshotId: hit.snapshot_id }
          : null,
      );
    } catch {
      // best-effort：探测失败不弹错（用户点回滚时再如实报错）。
      setRollbackTarget(null);
    }
  }, [roomId]);

  useEffect(() => {
    void load();
    void probeRollback();
  }, [load, probeRollback]);

  /** 存档。 */
  const handleSave = useCallback(async () => {
    if (readOnly || saving) return;
    setSaving(true);
    setError(null);
    try {
      await snapshotVirtualCityRoom(roomId, 'manual');
      await load();
    } catch (e) {
      setError(
        t('virtualCity.dash.snapshot.saveFailed' as TKey, {
          msg: errorMessage(e, 'save failed'),
        }),
      );
      reportGlobalError(e);
    } finally {
      setSaving(false);
    }
  }, [roomId, readOnly, saving, t, load]);

  /** 确认后的执行。 */
  const handleConfirm = useCallback(async () => {
    if (!confirm) return;
    const { action, snapshotId } = confirm;
    setConfirm(null);
    setError(null);
    try {
      if (action === 'restore') {
        setBusyId(snapshotId);
        await restoreVirtualCitySnapshot(roomId, snapshotId);
        await load();
      } else if (action === 'delete') {
        setBusyId(snapshotId);
        await deleteVirtualCitySnapshot(roomId, snapshotId);
        await load();
      } else {
        setRollingBack(true);
        await restoreVirtualCitySnapshot(roomId, snapshotId);
        await load();
        await probeRollback();
      }
    } catch (e) {
      const key =
        action === 'restore'
          ? 'virtualCity.dash.snapshot.restoreFailed'
          : action === 'delete'
            ? 'virtualCity.dash.snapshot.deleteFailed'
            : 'virtualCity.dash.snapshot.rollbackFailed';
      setError(t(key as TKey, { msg: errorMessage(e, 'operation failed') }));
      reportGlobalError(e);
    } finally {
      setBusyId(null);
      setRollingBack(false);
    }
  }, [confirm, roomId, t, load, probeRollback]);

  /** 读档二次确认。 */
  const askRestore = useCallback(
    (snapshotId: string) => {
      if (readOnly) return;
      setConfirm({
        action: 'restore',
        snapshotId,
        message: t('virtualCity.dash.snapshot.confirmRestore' as TKey, {
          id: shortHash(snapshotId),
        }),
      });
    },
    [readOnly, t],
  );

  /** 删除二次确认。 */
  const askDelete = useCallback(
    (snapshotId: string) => {
      if (readOnly) return;
      setConfirm({
        action: 'delete',
        snapshotId,
        message: t('virtualCity.dash.snapshot.confirmDelete' as TKey, {
          id: shortHash(snapshotId),
        }),
      });
    },
    [readOnly, t],
  );

  /** 回滚二次确认。 */
  const askRollback = useCallback(() => {
    if (readOnly || !rollbackTarget) return;
    setConfirm({
      action: 'rollback',
      snapshotId: rollbackTarget.snapshotId,
      message: t('virtualCity.dash.snapshot.confirmRollback' as TKey, {
        id: shortHash(rollbackTarget.snapshotId),
      }),
    });
  }, [readOnly, rollbackTarget, t]);

  const reasonLabel = (reason: string): string => {
    const key = REASON_KEYS[reason];
    return key ? t(key) : reason || '—';
  };

  const list = snapshots ?? [];

  return (
    <div
      className="vc-snap"
      data-testid="virtualCity-snapshots"
      data-vc-snap-count={String(list.length)}
      data-vc-snap-loading={loading ? '1' : '0'}
    >
      {/* 工具条：存档 / 回滚 / 刷新 */}
      <div className="vc-snap__toolbar">
        <button
          type="button"
          className="vc-snap__btn vc-snap__btn--primary"
          onClick={() => void handleSave()}
          disabled={readOnly || saving}
          title={
            readOnly
              ? t('virtualCity.dash.snapshot.readOnly' as TKey)
              : t('virtualCity.dash.snapshot.save' as TKey)
          }
          data-testid="virtualCity-snap-save"
        >
          {saving
            ? t('virtualCity.dash.snapshot.saving' as TKey)
            : t('virtualCity.dash.snapshot.save' as TKey)}
        </button>
        <button
          type="button"
          className="vc-snap__btn"
          onClick={askRollback}
          disabled={readOnly || rollingBack || !rollbackTarget}
          title={
            readOnly
              ? t('virtualCity.dash.snapshot.readOnly' as TKey)
              : !rollbackTarget
                ? t('virtualCity.dash.snapshot.noRollback' as TKey)
                : t('virtualCity.dash.snapshot.rollback' as TKey)
          }
          data-testid="virtualCity-snap-rollback"
        >
          {rollingBack
            ? t('virtualCity.dash.snapshot.rollingBack' as TKey)
            : t('virtualCity.dash.snapshot.rollback' as TKey)}
        </button>
        <button
          type="button"
          className="vc-snap__btn"
          onClick={() => void load()}
          disabled={loading}
          title={t('virtualCity.dash.snapshot.refresh' as TKey)}
        >
          {loading
            ? t('virtualCity.dash.snapshot.loading' as TKey)
            : t('virtualCity.dash.snapshot.refresh' as TKey)}
        </button>
      </div>

      {/* §7.1：失败内联红条 */}
      {error && (
        <div className="vc-snap__error" role="alert" data-testid="virtualCity-snap-error">
          <span>⚠️ {error}</span>
          <button type="button" onClick={() => setError(null)} aria-label="dismiss">
            ×
          </button>
        </div>
      )}

      {/* 空态 */}
      {list.length === 0 && !loading && (
        <div className="vc-snap__empty">
          {t('virtualCity.dash.snapshot.empty' as TKey)}
        </div>
      )}

      {/* 快照列表 */}
      <div className="vc-snap__list">
        {list.map((snap) => (
          <article
            className="vc-snap__item"
            key={snap.snapshot_id}
            data-testid="virtualCity-snap-row"
            data-vc-snapshot-id={snap.snapshot_id}
            data-vc-reason={snap.reason || ''}
          >
            <header className="vc-snap__item-head">
              <span className="vc-snap__item-id" title={snap.snapshot_id}>
                {shortHash(snap.snapshot_id)}
              </span>
              <span className="vc-snap__badge">{reasonLabel(snap.reason)}</span>
            </header>
            <div className="vc-snap__meta">
              <span className="vc-snap__meta-cell">
                <span className="vc-snap__meta-label">
                  {t('virtualCity.dash.snapshot.col.createdAt' as TKey)}
                </span>
                <span className="vc-snap__meta-value">{fmtTime(snap.created_at)}</span>
              </span>
              <span className="vc-snap__meta-cell">
                <span className="vc-snap__meta-label">
                  {t('virtualCity.dash.snapshot.col.month' as TKey)}
                </span>
                <span className="vc-snap__meta-value">
                  {t('virtualCity.month' as TKey, { m: snap.month })}
                </span>
              </span>
              <span className="vc-snap__meta-cell">
                <span className="vc-snap__meta-label">
                  {t('virtualCity.dash.snapshot.col.size' as TKey)}
                </span>
                <span className="vc-snap__meta-value">{fmtBytes(snap.size_bytes)}</span>
              </span>
            </div>
            <div className="vc-snap__hash" title={snap.state_hash}>
              <span className="vc-snap__meta-label">
                {t('virtualCity.dash.snapshot.col.stateHash' as TKey)}
              </span>
              <code className="vc-snap__hash-value">{shortHash(snap.state_hash)}</code>
            </div>
            <footer className="vc-snap__actions">
              <button
                type="button"
                className="vc-snap__btn"
                onClick={() => askRestore(snap.snapshot_id)}
                disabled={readOnly || busyId === snap.snapshot_id}
                title={
                  readOnly
                    ? t('virtualCity.dash.snapshot.readOnly' as TKey)
                    : t('virtualCity.dash.snapshot.restore' as TKey)
                }
                data-testid="virtualCity-snap-restore"
                data-vc-snapshot-id={snap.snapshot_id}
              >
                {t('virtualCity.dash.snapshot.restore' as TKey)}
              </button>
              <button
                type="button"
                className="vc-snap__btn vc-snap__btn--danger"
                onClick={() => askDelete(snap.snapshot_id)}
                disabled={readOnly || busyId === snap.snapshot_id}
                title={
                  readOnly
                    ? t('virtualCity.dash.snapshot.readOnly' as TKey)
                    : t('virtualCity.dash.snapshot.delete' as TKey)
                }
                data-testid="virtualCity-snap-delete"
                data-vc-snapshot-id={snap.snapshot_id}
              >
                {t('virtualCity.dash.snapshot.delete' as TKey)}
              </button>
            </footer>
          </article>
        ))}
      </div>

      {/* 高风险二次确认（Portal；不用 window.confirm —— 会阻塞 CDP 自动化） */}
      {confirm && (
        <ConfirmModal
          message={confirm.message}
          danger={confirm.action !== 'restore'}
          onConfirm={() => void handleConfirm()}
          onCancel={() => setConfirm(null)}
        />
      )}
    </div>
  );
}

export default SnapshotPanel;
