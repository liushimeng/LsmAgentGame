import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/store/auth.store';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useT } from '@/hooks/useT';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import { reportGlobalError } from '@/services/globalError';

// 用户列表项。普通用户只会收到 id/nickname/online；管理员及以上才有详细字段。
interface UserItem {
  id: string;
  nickname: string;
  online: boolean;
  account?: string;
  phone?: string;
  email?: string;
  user_type?: number;
  my_invite_code?: string;
  referral_count?: number;
  referrer_user_id?: string;
  created_at?: number;
  last_login_at?: number;
  can_delete?: boolean;
}

// 支持的每页条数（与服务端硬上限 100 对齐）。
const PAGE_SIZE_OPTIONS = [10, 20, 40, 60, 100] as const;
const DEFAULT_PAGE_SIZE = 20;
const BATCH_DELETE_LIMIT = 100; // 单次批量删除上限

// 解析 localStorage 中持久化的 page size。非法值回退到默认值。
function loadPageSize(): number {
  try {
    const raw = localStorage.getItem('lsm.adminUsers.pageSize');
    const n = raw ? parseInt(raw, 10) : NaN;
    if ((PAGE_SIZE_OPTIONS as readonly number[]).includes(n)) return n;
  } catch {
    // localStorage 不可用（隐私模式）静默回退。
  }
  return DEFAULT_PAGE_SIZE;
}

// 用户列表页 —— 所有登录用户均可访问：
//   普通用户：仅看 昵称 + 在线状态
//   管理员：完整字段（无密码）
//   超管：完整字段 + 单删/批量删除（带级联删除）
// 所有数据交互走 WebSocket（user.* 帧），不再使用 HTTP /api/admin/users。
// 分页：user.list 携带 { skip, limit, sort, online }，服务端返回 { total, skip, limit, online? }。
//   - 在线 Tab: sort = "created_at"，online = true（DB 端按在线 ID 集合过滤）
//   - 离线 Tab: sort = "last_login_at"，online = false（按最后登录倒序）
//   重要：online 过滤必须在 service 层做，否则 tab 计数永远是当前 page size 上限。
// Tab / page size 状态持久化到 localStorage。
export function AdminUsersPage() {
  const navigate = useNavigate();
  const { isAuthenticated } = useAuthStore();
  const myUserId = useAuthStore((s) => s.userId);
  const t = useT();

  const [users, setUsers] = useState<UserItem[]>([]);
  // 调用者自身权限由服务端在 user.list_resp 中返回，决定渲染粒度。
  const [myUserType, setMyUserType] = useState<number>(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(0); // 0 基页码
  const [total, setTotal] = useState(0);
  // 各 tab 的真实总人数（来自服务端过滤后的 total），用于 tab 角标显示。
  // 0 表示未拉取过；切换 tab 后会被对应请求的响应覆盖。
  const [onlineTotal, setOnlineTotal] = useState<number>(0);
  const [offlineTotal, setOfflineTotal] = useState<number>(0);
  // 每页条数（持久化）。
  const [pageSize, setPageSize] = useState<number>(loadPageSize);

  // Tab 状态：'online' 或 'offline'，从 localStorage 恢复
  const [activeTab, setActiveTab] = useState<'online' | 'offline'>(() => {
    try {
      const cached = localStorage.getItem('lsm.adminUsers.activeTab');
      return cached === 'offline' ? 'offline' : 'online';
    } catch {
      return 'online';
    }
  });

  // 批量选择 / 弹层 / toast
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [batchConfirmOpen, setBatchConfirmOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<{ id: string; nick: string } | null>(null);
  const [pendingRevoke, setPendingRevoke] = useState<{ id: string; nick: string } | null>(null);
  const [toastMsg, setToastMsg] = useState<string>('');

  // page 在闭包里会失效（onOpen/broadcast 回调持有旧值），用 ref 追踪最新值。
  const pageRef = useRef(page);
  pageRef.current = page;
  const activeTabRef = useRef(activeTab);
  activeTabRef.current = activeTab;
  const pageSizeRef = useRef(pageSize);
  pageSizeRef.current = pageSize;

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  // 未登录则回首页。
  useEffect(() => {
    if (!isAuthenticated) navigate('/');
  }, [isAuthenticated, navigate]);

  // Tab 切换：清空选中 + 写 localStorage + 回到第 0 页（不同集合的分页坐标无意义）。
  useEffect(() => {
    setSelectedIds(new Set());
    setPage(0);
    try {
      localStorage.setItem('lsm.adminUsers.activeTab', activeTab);
    } catch {
      // localStorage 写入失败（隐私模式 / 配额）静默忽略。
    }
  }, [activeTab]);

  // 拉取当前页（重连、翻页、删除广播后均可复用）。
  // sort: 在线 → created_at，离线 → last_login_at（让服务端先按对应字段排序再分页）。
  // online: 由 tab 决定，传给 service 层在 DB 端做 IN/NOT IN 过滤（避免 tab 计数被 page size 截断）。
  // 直接读取 page/activeTab/pageSize ref 的最新值，避免 useCallback 闭包陷阱导致翻页时 skip 计算错误。
  const requestList = useCallback(
    (overridePage?: number, overrideTab?: 'online' | 'offline', overrideSize?: number) => {
      setLoading(true);
      const p = overridePage ?? pageRef.current;
      const tab = overrideTab ?? activeTabRef.current;
      const size = overrideSize ?? pageSizeRef.current;
      wsClient.send('user.list', {
        skip: p * size,
        limit: size,
        sort: tab === 'offline' ? 'last_login_at' : 'created_at',
        online: tab === 'online',
      });
    },
    [],
  );

  // 订阅 user.* 响应帧；重连后自动重发当前页。
  useEffect(() => {
    if (!isAuthenticated) return;

    const offMsg = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('user.')) return;
      const p = (env.payload ?? {}) as Record<string, unknown>;
      switch (env.type) {
        case 'user.list_resp': {
          setUsers((p.users as UserItem[]) ?? []);
          setMyUserType((p.my_user_type as number) ?? 1);
          const newTotal = (p.total as number) ?? 0;
          setTotal(newTotal);
          // 同步 tab 角标：response 透传 online 时,本 tab 计数 = newTotal;
          // 透传 false 时,offline 计数 = newTotal。否则只更新当前 tab,
          // 另一个 tab 保持上次值。
          if (p.online === true) {
            setOnlineTotal(newTotal);
          } else if (p.online === false) {
            setOfflineTotal(newTotal);
          } else {
            // 无 online 过滤的旧路径,同时更新两 tab 计数(等价全量)。
            setOnlineTotal(newTotal);
            setOfflineTotal(newTotal);
          }
          setLoading(false);
          setError(null);
          break;
        }
        case 'user.deleted': {
          const id = p.id as string;
          setUsers((prev) => prev.filter((u) => u.id !== id));
          setTotal((prev) => Math.max(0, prev - 1));
          // 同步从 selectedIds 中移除，避免 ghost 选中。
          setSelectedIds((prev) => {
            if (!prev.has(id)) return prev;
            const next = new Set(prev);
            next.delete(id);
            return next;
          });
          // 另一个管理员删除了用户 —— 重新拉取当前页保持一致（补齐缺行）。
          requestList();
          break;
        }
        case 'user.delete_resp': {
          // 发起者收到的回执（删除成功）。列表移除由广播的 user.deleted 统一处理。
          break;
        }
        case 'user.batch_delete_resp': {
          const success = (p.success_ids as string[]) ?? [];
          const failed =
            (p.failed as Array<{ id: string; reason_code: number; reason: string }>) ?? [];
          // 清空选中（服务端会广播 user.deleted 自动刷新列表）。
          setSelectedIds(new Set());
          // 显式 reload 以兜底（部分失败场景下广播可能不完整）。
          requestList();
          if (failed.length > 0 && success.length > 0) {
            setToastMsg(
              t('adminUsers.errorBatchPartial', {
                success: success.length,
                failed: failed.length,
              }),
            );
          } else if (failed.length > 0 && success.length === 0) {
            setToastMsg(
              t('adminUsers.errorBatchAllFailed', { count: failed.length }),
            );
          }
          if (failed.length > 0) {
            setTimeout(() => setToastMsg(''), 4000);
          }
          break;
        }
        case 'user.revoke_super_resp': {
          // 发起者收到的回执:撤销超管成功。广播 user.role_changed 负责更新行,
          // 这里给成功 toast + 显式 reload 兜底。
          setToastMsg(t('adminUsers.revokeSuperOk'));
          setTimeout(() => setToastMsg(''), 4000);
          requestList();
          break;
        }
        case 'user.role_changed': {
          // 广播:某用户角色变更(撤销超管后 new_user_type=1)。就地更新对应行,
          // 使删除/撤销按钮的可见性随之重新计算。
          const id = p.id as string;
          const nt = (p.new_user_type as number) ?? 1;
          setUsers((prev) =>
            prev.map((u) => (u.id === id ? { ...u, user_type: nt } : u)),
          );
          break;
        }
        case 'user.error': {
          const errMsg = (p.message as string) || t('adminUsers.errorGeneric');
          setError(errMsg);
          // §7.1 — WS 驱动的错误也上报到最高层级,确保用户切到别的 tab 时仍能看到。
          reportGlobalError({ message: errMsg, severity: 'error' });
          setLoading(false);
          break;
        }
      }
    });

    // 连接就绪时（含重连）请求列表。
    const offOpen = wsClient.onOpen(requestList);
    if (wsClient.connected) requestList();

    return () => {
      offMsg();
      offOpen();
    };
  }, [isAuthenticated, requestList, t]);

  // page / tab / pageSize 变化时重新拉取。
  // 直接传入当前值，避免闭包延迟导致请求使用旧的 skip。
  useEffect(() => {
    if (!isAuthenticated) return;
    if (wsClient.connected) requestList(page, activeTab, pageSize);
  }, [page, activeTab, pageSize, isAuthenticated, requestList]);

  // pageSize 变化时把 page 钳制到有效范围，并写 localStorage。
  useEffect(() => {
    try {
      localStorage.setItem('lsm.adminUsers.pageSize', String(pageSize));
    } catch {
      // 静默忽略。
    }
    const maxPage = Math.max(0, Math.ceil(total / pageSize) - 1);
    if (page > maxPage) setPage(maxPage);
  }, [pageSize, total, page]);

  // 触发单删确认弹层。
  const handleDeleteUser = useCallback((id: string, nick: string) => {
    setPendingDelete({ id, nick });
  }, []);

  const getUserTypeLabel = (type?: number): string => {
    switch (type) {
      case 1:
        return t('adminUsers.userTypeNormal');
      case 2:
        return t('adminUsers.userTypeAdmin');
      case 3:
        return t('adminUsers.userTypeSuper');
      default:
        return '-';
    }
  };

  const formatDate = (timestamp?: number) => {
    if (!timestamp) return '-';
    return new Date(timestamp * 1000).toLocaleString('zh-CN');
  };

  if (!isAuthenticated) return null;

  const isAdmin = myUserType >= 2; // 管理员及以上看详细列
  const isSuper = myUserType >= 3; // 超管看删除列

  const subtitle = isSuper
    ? t('adminUsers.subtitleSuper')
    : isAdmin
      ? t('adminUsers.subtitleAdmin')
      : t('adminUsers.subtitleUser');

  // 客户端不再做 online/offline 分桶 —— service 层已按 online filter 过滤过。
  // currentList 直接取 users。
  const currentList = users;

  return (
    <div className="admin-users-page">
      <h1>👥 {t('adminUsers.title')}</h1>
      <p className="admin-users-page__subtitle">{subtitle}</p>

      {toastMsg && <div className="admin-users-toast" data-testid="admin-users-toast">{toastMsg}</div>}

      {error && <div className="error">{error}</div>}

      {!error && (
        <>
          {/* Tab 切换器 */}
          <div className="admin-users-tabs" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={activeTab === 'online'}
              className={
                'admin-users-tab' + (activeTab === 'online' ? ' admin-users-tab--active' : '')
              }
              onClick={() => setActiveTab('online')}
              data-testid="tab-online"
            >
              {t('adminUsers.tabOnline')} ({onlineTotal})
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={activeTab === 'offline'}
              className={
                'admin-users-tab' + (activeTab === 'offline' ? ' admin-users-tab--active' : '')
              }
              onClick={() => setActiveTab('offline')}
              data-testid="tab-offline"
            >
              {t('adminUsers.tabOffline')} ({offlineTotal})
            </button>
          </div>

          {/* 吸顶批量工具栏（仅超管 + 至少选中 1 项时显示） */}
          {isSuper && selectedIds.size > 0 && (
            <div className="admin-users-batch-toolbar" data-testid="batch-toolbar">
              <span data-testid="batch-selected-count">
                {selectedIds.size === 1
                  ? t('adminUsers.toolbarSelectedOne')
                  : t('adminUsers.toolbarSelected', { count: selectedIds.size })}
              </span>
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                onClick={() => setSelectedIds(new Set())}
                data-testid="batch-clear"
              >
                {t('adminUsers.toolbarClear')}
              </button>
              <button
                type="button"
                className="btn btn-danger btn-sm"
                onClick={() => {
                  if (selectedIds.size === 0) return;
                  if (selectedIds.size > BATCH_DELETE_LIMIT) {
                    setToastMsg(t('adminUsers.errorBatchSizeLimit'));
                    setTimeout(() => setToastMsg(''), 3000);
                    return;
                  }
                  setBatchConfirmOpen(true);
                }}
                data-testid="batch-delete"
              >
                {t('adminUsers.batchDelete')}
              </button>
            </div>
          )}

          <div className="admin-users-table-wrapper">
            {loading && <div className="admin-users-loading-overlay">{t('common.loading')}</div>}
            <table className="admin-users-table">
              <thead>
                <tr>
                  {isSuper && (
                    <th style={{ width: 40 }}>
                      <input
                        type="checkbox"
                        checked={
                          currentList.length > 0 &&
                          currentList.filter((u) => u.can_delete).every((u) => selectedIds.has(u.id))
                        }
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedIds(
                              new Set(
                                currentList.filter((u) => u.can_delete).map((u) => u.id),
                              ),
                            );
                          } else {
                            setSelectedIds(new Set());
                          }
                        }}
                        data-testid="select-all"
                        aria-label={t('adminUsers.selectAll')}
                      />
                    </th>
                  )}
                  <th>{t('adminUsers.colNickname')}</th>
                  <th>{t('adminUsers.colOnline')}</th>
                  {isAdmin && <th>{t('adminUsers.colAccount')}</th>}
                  {isAdmin && <th>{t('adminUsers.colUserType')}</th>}
                  {isAdmin && <th>{t('adminUsers.colPhone')}</th>}
                  {isAdmin && <th>{t('adminUsers.colEmail')}</th>}
                  {isAdmin && <th>{t('adminUsers.colInviteCode')}</th>}
                  {isAdmin && <th>{t('adminUsers.colReferralCount')}</th>}
                  {isAdmin && <th>{t('adminUsers.colCreatedAt')}</th>}
                  {isAdmin && <th>{t('adminUsers.colLastLoginAt')}</th>}
                  {isSuper && <th>{t('adminUsers.colAction')}</th>}
                </tr>
              </thead>
              <tbody>
                {currentList.map((user) => (
                  <tr
                    key={user.id}
                    className={selectedIds.has(user.id) ? 'selected' : undefined}
                  >
                    {isSuper && (
                      <td>
                        {user.can_delete ? (
                          <input
                            type="checkbox"
                            checked={selectedIds.has(user.id)}
                            onChange={(e) => {
                              setSelectedIds((prev) => {
                                const next = new Set(prev);
                                if (e.target.checked) {
                                  next.add(user.id);
                                } else {
                                  next.delete(user.id);
                                }
                                return next;
                              });
                            }}
                            data-testid={`row-checkbox-${user.id}`}
                          />
                        ) : null}
                      </td>
                    )}
                    <td className="admin-users-table__nick">{user.nickname}</td>
                    <td>
                      <span className={'online-dot ' + (user.online ? 'online-dot--on' : 'online-dot--off')}>
                        {user.online
                          ? `🟢 ${t('adminUsers.statusOnline')}`
                          : `⚪ ${t('adminUsers.statusOffline')}`}
                      </span>
                    </td>
                    {isAdmin && <td>{user.account || '-'}</td>}
                    {isAdmin && (
                      <td>
                        <span className={`user-type-badge user-type-${user.user_type}`}>
                          {getUserTypeLabel(user.user_type)}
                        </span>
                      </td>
                    )}
                    {isAdmin && <td>{user.phone || '-'}</td>}
                    {isAdmin && <td>{user.email || '-'}</td>}
                    {isAdmin && <td><code>{user.my_invite_code || '-'}</code></td>}
                    {isAdmin && <td>{user.referral_count ?? 0}</td>}
                    {isAdmin && <td>{formatDate(user.created_at)}</td>}
                    {isAdmin && <td>{formatDate(user.last_login_at)}</td>}
                    {isSuper && (
                      <td>
                        {user.can_delete && (
                          <button
                            type="button"
                            className="btn btn-danger btn-sm"
                            onClick={() => handleDeleteUser(user.id, user.nickname)}
                            data-testid={`row-delete-${user.id}`}
                          >
                            {t('adminUsers.delete')}
                          </button>
                        )}
                        {/* 撤销超管:仅对「离线的、非自己的、超管」用户显示 */}
                        {isSuper &&
                          user.user_type === 3 &&
                          !user.online &&
                          user.id !== myUserId && (
                            <button
                              type="button"
                              className="btn btn-secondary btn-sm"
                              onClick={() =>
                                setPendingRevoke({ id: user.id, nick: user.nickname })
                              }
                              data-testid={`row-revoke-super-${user.id}`}
                            >
                              {t('adminUsers.revokeSuper')}
                            </button>
                          )}
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>

            {!loading && currentList.length === 0 && (
              <div className="empty-state">{t('adminUsers.errorEmpty')}</div>
            )}
          </div>

          <div className="admin-users-pagination">
            <span className="admin-users-pagination__info">
              {t('adminUsers.totalCount', { total })}
              {' · '}
              第 {page + 1} / {totalPages} 页
            </span>
            <div className="admin-users-pagination__actions">
              <label className="admin-users-pagesize">
                <span className="admin-users-pagesize__label">
                  {t('adminUsers.pageSize')}
                </span>
                <select
                  className="admin-users-pagesize__select"
                  value={pageSize}
                  onChange={(e) => {
                    const n = parseInt(e.target.value, 10);
                    if ((PAGE_SIZE_OPTIONS as readonly number[]).includes(n)) {
                      setPage(0);
                      setPageSize(n);
                    }
                  }}
                  disabled={loading}
                  data-testid="page-size-select"
                >
                  {PAGE_SIZE_OPTIONS.map((n) => (
                    <option key={n} value={n}>
                      {n} {t('adminUsers.pageSizeUnit')}
                    </option>
                  ))}
                </select>
              </label>
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={page === 0 || loading}
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                data-testid="page-prev"
              >
                上一页
              </button>
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={page >= totalPages - 1 || loading}
                onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                data-testid="page-next"
              >
                下一页
              </button>
            </div>
          </div>
        </>
      )}

      {/* 单删确认弹层 */}
      {pendingDelete && (
        <ConfirmModal
          messageKey="adminUsers.confirmDeleteTitle"
          confirmLabel={t('adminUsers.confirmDeleteOk')}
          danger
          onConfirm={() => {
            wsClient.send('user.delete', { id: pendingDelete.id });
            setPendingDelete(null);
          }}
          onCancel={() => setPendingDelete(null)}
        />
      )}

      {/* 撤销超管确认弹层 */}
      {pendingRevoke && (
        <ConfirmModal
          messageKey="adminUsers.confirmRevokeSuperTitle"
          confirmLabel={t('adminUsers.revokeSuper')}
          danger
          onConfirm={() => {
            wsClient.send('user.revoke_super', { id: pendingRevoke.id });
            setPendingRevoke(null);
          }}
          onCancel={() => setPendingRevoke(null)}
        />
      )}

      {/* 批量删除确认弹层（用已插值的 message / confirmLabel 传 {count}） */}
      {batchConfirmOpen && (
        <ConfirmModal
          message={t('adminUsers.confirmBatchDeleteTitle', { count: selectedIds.size })}
          confirmLabel={t('adminUsers.confirmBatchDeleteOk')}
          danger
          onConfirm={() => {
            wsClient.send('user.batch_delete', { ids: Array.from(selectedIds) });
            setBatchConfirmOpen(false);
          }}
          onCancel={() => setBatchConfirmOpen(false)}
        />
      )}

      <style>{`
        .admin-users-page {
          position: relative;
          padding: 28px 24px 40px;
          width: 100%;
          max-width: 1800px;
          margin: 0 auto;
          border-radius: 12px;
          color: var(--text);
          box-sizing: border-box;
          /* 柔和 radial 渐变叠加暗色底 —— 与 game-card / room-card 同色系。 */
          background:
            radial-gradient(1200px 400px at 18% 0%, rgba(10,132,255,0.10), transparent 60%),
            radial-gradient(800px 300px at 92% 8%, rgba(63,185,80,0.06), transparent 60%),
            var(--bg);
        }
        /* 大屏 / 侧栏折叠时让整个页面跟随视口拉宽 */
        @media (min-width: 1600px) {
          .admin-users-page {
            max-width: none;
            padding: 32px 36px 48px;
          }
        }
        @media (min-width: 2200px) {
          .admin-users-page {
            padding: 36px 48px 56px;
          }
        }

        .admin-users-page h1 {
          margin: 0 0 6px 0;
          font-size: 26px;
          font-weight: 700;
          /* accent 渐变标题文字。 */
          background: linear-gradient(135deg, var(--text) 0%, var(--accent) 100%);
          -webkit-background-clip: text;
          background-clip: text;
          color: transparent;
        }

        .admin-users-page__subtitle {
          color: var(--muted);
          margin: 0 0 20px 0;
          font-size: 13px;
        }

        .error, .empty-state {
          padding: 20px;
          text-align: center;
          color: var(--muted);
        }
        .error { color: var(--danger); }
        .empty-state { padding: 48px 16px; }

        .admin-users-tabs {
          display: flex;
          gap: 4px;
          border-bottom: 1px solid var(--border);
          margin: 12px 0;
        }
        .admin-users-tab {
          padding: 8px 16px;
          background: transparent;
          border: none;
          border-bottom: 2px solid transparent;
          color: var(--text);
          cursor: pointer;
          font-size: 14px;
          transition: all 0.15s;
        }
        .admin-users-tab:hover { color: var(--accent); }
        .admin-users-tab--active {
          color: var(--accent);
          border-bottom-color: var(--accent);
          font-weight: 600;
        }

        .admin-users-batch-toolbar {
          display: flex;
          align-items: center;
          gap: 12px;
          padding: 10px 16px;
          background: var(--panel);
          border: 1px solid var(--border);
          border-radius: 6px;
          margin-bottom: 8px;
          position: sticky;
          top: 0;
          z-index: 10;
          color: var(--text);
          font-size: 13px;
        }

        .admin-users-table-wrapper {
          position: relative;
          overflow-x: auto;
          border: 1px solid var(--border);
          border-radius: 10px;
          background: linear-gradient(180deg, var(--panel) 0%, rgba(22,27,34,0.85) 100%);
          box-shadow: 0 8px 24px rgba(0,0,0,0.35);
        }

        .admin-users-loading-overlay {
          position: absolute;
          inset: 0;
          display: flex;
          align-items: center;
          justify-content: center;
          background: rgba(14,17,23,0.55);
          color: var(--muted);
          font-size: 14px;
          z-index: 2;
          border-radius: 10px;
        }

        .admin-users-table {
          width: 100%;
          border-collapse: separate;
          border-spacing: 0;
          background: transparent;
        }

        .admin-users-table th,
        .admin-users-table td {
          padding: 12px 16px;
          text-align: left;
          border-bottom: 1px solid var(--border);
          color: var(--text);
          font-size: 13px;
          white-space: nowrap;
        }

        .admin-users-table thead th {
          position: sticky;
          top: 0;
          background: linear-gradient(180deg, rgba(10,132,255,0.18) 0%, rgba(10,132,255,0.06) 100%);
          font-weight: 600;
          font-size: 12px;
          letter-spacing: 0.04em;
          color: var(--text);
          border-bottom: 1px solid var(--accent);
        }

        .admin-users-table tbody tr {
          transition: background 0.15s ease;
        }
        .admin-users-table tbody tr:nth-child(even) {
          background: rgba(255,255,255,0.015);
        }
        .admin-users-table tbody tr:hover {
          background: rgba(10,132,255,0.10);
        }
        .admin-users-table tbody tr.selected {
          background: rgba(80, 140, 255, 0.08);
        }
        .admin-users-table tbody tr:last-child td {
          border-bottom: none;
        }
        .admin-users-table__nick {
          font-weight: 600;
        }

        .online-dot {
          font-size: 13px;
          white-space: nowrap;
        }
        .online-dot--on { color: var(--ok); }
        .online-dot--off { color: var(--muted); }

        .user-type-badge {
          display: inline-block;
          padding: 3px 10px;
          border-radius: 999px;
          font-size: 12px;
          font-weight: 500;
          border: 1px solid transparent;
        }
        .user-type-1 { background: rgba(10,132,255,0.12); color: var(--accent); border-color: rgba(10,132,255,0.35); }
        .user-type-2 { background: rgba(63,185,80,0.12); color: var(--ok); border-color: rgba(63,185,80,0.35); }
        .user-type-3 { background: rgba(248,81,73,0.12); color: var(--danger); border-color: rgba(248,81,73,0.35); }

        .admin-users-pagination {
          display: flex;
          align-items: center;
          justify-content: space-between;
          gap: 12px;
          margin-top: 16px;
          padding: 10px 14px;
          border: 1px solid var(--border);
          border-radius: 8px;
          background: var(--panel);
        }
        .admin-users-pagination__info { color: var(--muted); font-size: 12px; }
        .admin-users-pagination__actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }

        .admin-users-pagesize {
          display: inline-flex;
          align-items: center;
          gap: 6px;
          font-size: 12px;
          color: var(--muted);
          margin-right: 4px;
        }
        .admin-users-pagesize__label { white-space: nowrap; }
        .admin-users-pagesize__select {
          background: var(--bg);
          color: var(--text);
          border: 1px solid var(--border);
          border-radius: 6px;
          padding: 4px 8px;
          font-size: 12px;
          cursor: pointer;
          transition: border-color 0.15s, color 0.15s;
        }
        .admin-users-pagesize__select:hover:not(:disabled) {
          border-color: var(--accent);
          color: var(--accent);
        }
        .admin-users-pagesize__select:disabled { opacity: 0.5; cursor: not-allowed; }

        .admin-users-toast {
          position: fixed;
          top: 16px;
          right: 16px;
          padding: 10px 16px;
          background: var(--danger);
          color: white;
          border-radius: 6px;
          z-index: 100;
          box-shadow: 0 4px 12px rgba(0,0,0,0.2);
          font-size: 13px;
          max-width: 360px;
        }

        .btn {
          border: none;
          border-radius: 6px;
          cursor: pointer;
          font-size: 13px;
          transition: background 0.15s, border-color 0.15s, color 0.15s;
        }
        .btn:disabled { opacity: 0.45; cursor: not-allowed; }

        .btn-secondary {
          background: var(--bg);
          color: var(--text);
          border: 1px solid var(--border);
          padding: 6px 14px;
        }
        .btn-secondary:hover:not(:disabled) {
          border-color: var(--accent);
          color: var(--accent);
        }

        .btn-danger {
          background: var(--danger);
          color: #fff;
          padding: 6px 12px;
        }
        .btn-danger:hover:not(:disabled) { background: #d83d36; }

        .btn-sm { padding: 4px 10px; font-size: 12px; }

        code {
          background: var(--bg);
          border: 1px solid var(--border);
          padding: 2px 6px;
          border-radius: 4px;
          font-size: 12px;
          color: var(--accent);
        }
      `}</style>
    </div>
  );
}