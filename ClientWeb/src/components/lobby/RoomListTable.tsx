/**
 * RoomListTable — 共享房间列表表格,5+1 款游戏大厅共用。
 *
 * 取代原先每个 LobbyPage 各自渲染的 `.room-grid` / `.room-card` 卡片网格,
 * 改为统一的「列表/表格」布局,并补齐:
 *   - 房间编号(ID)列 + 房间名称列 + 创建时间列
 *   - 表头点击排序:房间编号 / 房间名称 / 创建时间(asc/desc 切换)
 *   - 分页:默认每页 20,可选 10 / 20 / 50 / 100
 *
 * 排序与分页均为**客户端**计算(基于传入的完整 `rooms` 数组)。这是因为大厅
 * 房间数量受 `max_room` 配置 + Janitor 清理约束,本身有上界;而 5s HTTP 轮询
 * + `room.state` WS 实时更新(patchRoom / removeRoom)操作的是 store 里的完整
 * 缓存——客户端分页能让实时增删与当前分页保持一致,避免服务端分页下「WS 删一
 * 条 → 当前页出现空洞」的体验割裂。
 *
 * 组件本身是纯展示 + 自管排序/分页状态;加入 / 观战 / 解散的业务逻辑(导航、
 * 错误处理)仍由各 LobbyPage 通过回调持有。
 */

import { useEffect, useMemo, useState } from 'react';
import type { RoomInfo } from '@/types/api';
import { useT } from '@/hooks/useT';
import { AdminDisbandButton } from '@/components/ui/AdminDisbandButton';

type SortKey = 'id' | 'name' | 'created_at';
type SortDir = 'asc' | 'desc';

const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];
const DEFAULT_PAGE_SIZE = 20;

export interface RoomListTableProps {
  rooms: RoomInfo[];
  /** 点击加入房间(业务侧负责导航 / 错误处理)。 */
  onJoin: (roomId: string) => void;
  /** 点击观战。 */
  onSpectate: (roomId: string) => void;
  /**
   * 2026-07-30 §R210-05: 我已是该房间的 player / spectator 时点击主按钮
   * 走直接路由,不再调用 onJoin / onSpectate (后者会发 HTTP,在 playing
   * 房间会返 30001 ErrRoomFull)。fallback: 该 prop 未提供时仍走 onJoin/onSpectate。
   */
  onNavigate?: (roomId: string, role: 'player' | 'spectator') => void;
  /** 强制解散成功后回调(从 store 移除该房间)。 */
  onRemove: (roomId: string) => void;
  /** 任意 loading 态(创建 / 加入中),禁用操作按钮。 */
  busy: boolean;
  /** 空列表主文案(各游戏 `<game>.noRooms`)。 */
  emptyText: string;
  /** 空列表副文案(各游戏 `<game>.createFirst`)。 */
  emptySub: string;
  /**
   * BUG-R200-P2-05 (2026-07-30): 当前用户在各房间的角色映射。
   * Key=roomId, Value=player/spectator。空映射表示未知(用 joinable() 兜底)。
   * 有值时:
   *   - player → 主按钮显示"进入房间"(走玩家路由)
   *   - spectator → 主按钮显示"👁 观战"(走观众路由,幂等)
   * 避免刷新后 playing 状态房间只剩"已满"按钮、用户无法重新进入。
   */
  myRoles?: Record<string, 'player' | 'spectator'>;
}

/** 将 ISO 时间字符串格式化为 `YYYY-MM-DD HH:mm:ss`(本地时区)。 */
function formatTime(iso: string): string {
  if (!iso) return '-';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ` +
    `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/** 判断房间是否可加入:open 且未满。playing / over / 已满 → 只能观战。 */
function joinable(r: RoomInfo): boolean {
  return r.status === 'open' && (r.current_count ?? 0) < r.capacity;
}

export function RoomListTable({
  rooms,
  onJoin,
  onSpectate,
  onNavigate,
  onRemove,
  busy,
  emptyText,
  emptySub,
  myRoles,
}: RoomListTableProps) {
  const t = useT();
  const [sortKey, setSortKey] = useState<SortKey>('created_at');
  const [sortDir, setSortDir] = useState<SortDir>('desc'); // 默认最新在前
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  // 排序后的完整列表(拷贝,不改动 store)。
  const sorted = useMemo(() => {
    const arr = rooms.slice();
    const dir = sortDir === 'asc' ? 1 : -1;
    arr.sort((a, b) => {
      let cmp = 0;
      if (sortKey === 'created_at') {
        cmp = new Date(a.created_at ?? 0).getTime() - new Date(b.created_at ?? 0).getTime();
        if (Number.isNaN(cmp)) cmp = 0;
      } else if (sortKey === 'name') {
        cmp = String(a.name ?? '').localeCompare(String(b.name ?? ''), undefined, { numeric: true, sensitivity: 'base' });
      } else {
        // id
        cmp = String(a.id).localeCompare(String(b.id));
      }
      return cmp * dir;
    });
    return arr;
  }, [rooms, sortKey, sortDir]);

  const total = sorted.length;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  // rooms 增删后,当前页可能越界 → 夹紧到 [1, totalPages]。
  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);

  // 切换排序键:同键再点切换方向;换键默认 desc(时间)或 asc(其余)。
  const toggleSort = (key: SortKey) => {
    if (key === sortKey) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortKey(key);
      // 时间默认倒序(最新在前),编号/名称默认升序——符合直觉。
      setSortDir(key === 'created_at' ? 'desc' : 'asc');
    }
    setPage(1);
  };

  const pageRooms = sorted.slice((page - 1) * pageSize, page * pageSize);

  const handleCopyId = async (id: string) => {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(id);
      } else {
        // 兜底:textarea + execCommand(旧浏览器 / 非 HTTPS)。
        const ta = document.createElement('textarea');
        ta.value = id;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      setCopiedId(id);
      window.setTimeout(() => setCopiedId((cur) => (cur === id ? null : cur)), 1500);
    } catch {
      /* 剪贴板失败不影响主流程 */
    }
  };

  const sortIndicator = (key: SortKey) => {
    if (key !== sortKey) return ' ↕';
    return sortDir === 'asc' ? ' ↑' : ' ↓';
  };

  if (total === 0) {
    return (
      <div className="room-list-empty">
        <p>{emptyText}</p>
        <p className="sub">{emptySub}</p>
      </div>
    );
  }

  return (
    <div className="room-list-wrap">
      <table className="room-list-table">
        <thead>
          <tr>
            <th
              className="room-list-th room-list-th--sortable"
              onClick={() => toggleSort('id')}
              title={t('lobby.copyId')}
            >
              {t('lobby.colId')}{sortIndicator('id')}
            </th>
            <th
              className="room-list-th room-list-th--sortable"
              onClick={() => toggleSort('name')}
            >
              {t('lobby.colName')}{sortIndicator('name')}
            </th>
            <th
              className="room-list-th room-list-th--sortable"
              onClick={() => toggleSort('created_at')}
            >
              {t('lobby.colCreated')}{sortIndicator('created_at')}
            </th>
            <th className="room-list-th">{t('lobby.colPlayers')}</th>
            <th className="room-list-th">{t('lobby.colStatus')}</th>
            <th className="room-list-th room-list-th--actions">{t('lobby.colActions')}</th>
          </tr>
        </thead>
        <tbody>
          {pageRooms.map((room) => {
            // 2026-07-30 §R210-05: 已知角色时跳过 joinable() 兜底 —
            // 自己就是 player 房间的 canJoin 永远 false,反而让"进入房间"
            // 按钮被禁用,用户体验退化。已知角色时主按钮直接走路由,
            // joinable 仅在未知角色时参与兜底。
            const myRole = room.id && myRoles ? myRoles[room.id] : undefined;
            const knownRole = myRole !== undefined;
            const canJoin = !knownRole && joinable(room);
            const idShort = room.id.length > 10 ? `${room.id.slice(0, 8)}…` : room.id;
            return (
              <tr
                key={room.id}
                className="room-list-row"
                onClick={() => canJoin && onJoin(room.id)}
                role={canJoin ? 'button' : undefined}
                tabIndex={canJoin ? 0 : undefined}
                onKeyDown={(e) => {
                  if (canJoin && (e.key === 'Enter' || e.key === ' ')) {
                    e.preventDefault();
                    onJoin(room.id);
                  }
                }}
              >
                <td className="room-list-td room-list-td--id" title={room.id}>
                  <span className="room-list-id-mono">{idShort}</span>
                  <button
                    type="button"
                    className="room-list-copy-btn"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleCopyId(room.id);
                    }}
                    title={copiedId === room.id ? t('lobby.copied') : t('lobby.copyId')}
                  >
                    {copiedId === room.id ? '✓' : '⧉'}
                  </button>
                </td>
                <td className="room-list-td room-list-td--name">
                  {room.name?.trim() || <span className="room-list-muted">-</span>}
                </td>
                <td className="room-list-td room-list-td--created">
                  {formatTime(room.created_at)}
                </td>
                <td className="room-list-td room-list-td--players">
                  {/* 防御性夹紧:历史脏数据可能出现 8/7 之类的幽灵行(§BUG-WEREWOLF-CAPACITY-COUNT)。 */}
                  {Math.min(room.current_count ?? 0, room.capacity)} / {room.capacity}
                  {room.spectator_count != null && room.spectator_count > 0 && (
                    <span className="room-list-spec-count"> · 👁 {room.spectator_count}</span>
                  )}
                </td>
                <td className="room-list-td room-list-td--status">
                  {/* §R180-P3-OBS1:把 raw status 翻成 i18n,避免 'playing' 显示为英文且易与
                      Join 按钮的「已满」混淆。仅翻译已知三态(其它生值保留原值)。 */}
                  <span className={`room-status ${room.status}`}>
                    {room.status === 'open'
                      ? t('lobby.status.open')
                      : room.status === 'playing'
                      ? t('lobby.status.playing')
                      : room.status === 'over'
                      ? t('lobby.status.over')
                      : room.status}
                  </span>
                </td>
                <td className="room-list-td room-list-td--actions" onClick={(e) => e.stopPropagation()}>
                  {/* BUG-FORCEDISBAND-LAYOUT FIX (Round 24): wrap the action
                   * buttons in `.room-list-actions` (inline-flex) so the
                   * <td> no longer carries `display: flex` (which is
                   * non-conforming and previously broke row height whenever
                   * a ConfirmModal rendered inside the cell). The modal
                   * itself is now portalled to <body> in ConfirmModal.tsx,
                   * so the table layout stays clean either way. */}
                  <div className="room-list-actions">
                    {/* BUG-R200-P2-05 (2026-07-30): 主按钮根据 myRoles 分流。
                        - myRoles[roomId] === 'player' → "进入房间"(走玩家路由)
                        - myRoles[roomId] === 'spectator' → "👁 观战"(走观众路由,幂等)
                        - 未知 + joinable → "加入"
                        - 未知 + 已满/playing → "已满"(灰色,但观战按钮始终可用)
                        避免刷新后 playing 状态房间只剩"已满"按钮、用户无法重新进入。 */}
                    {(() => {
                      const myRole = room.id && myRoles ? myRoles[room.id] : undefined;
                      if (myRole === 'player') {
                        // 2026-07-30 §R210-05: 已是 player 的房间主按钮直接走
                        // onNavigate(玩家路由),不再调用 onJoin — 后者会发
                        // HTTP /api/rooms/:id/join,在 playing 房间永远返 30001。
                        return (
                          <button
                            type="button"
                            className="btn btn-sm btn-primary room-list-join"
                            onClick={() => (onNavigate ? onNavigate(room.id, 'player') : onJoin(room.id))}
                            disabled={busy}
                            title={t('lobby.reenter')}
                          >
                            {t('lobby.reenter')}
                          </button>
                        );
                      }
                      if (myRole === 'spectator') {
                        // 同上,spectator 角色走观众路由,不再调用 onSpectate。
                        return (
                          <button
                            type="button"
                            className="btn btn-sm btn-secondary room-list-spectate"
                            onClick={() => (onNavigate ? onNavigate(room.id, 'spectator') : onSpectate(room.id))}
                            disabled={busy}
                            title={t('lobby.watchButton')}
                          >
                            👁 {t('lobby.watchButton')}
                          </button>
                        );
                      }
                      // 未知角色:用 joinable() 兜底(历史行为)。
                      return (
                        <button
                          type="button"
                          className="btn btn-sm btn-primary room-list-join"
                          onClick={() => onJoin(room.id)}
                          disabled={busy || !canJoin}
                          title={canJoin ? t('lobby.join') : t('lobby.full')}
                        >
                          {canJoin ? t('lobby.join') : t('lobby.full')}
                        </button>
                      );
                    })()}
                    {/* 观战按钮:非 spectator 角色时作为第二按钮保留。
                        2026-07-30 §R210-05: 已知 player 角色时也保留观战按钮
                        (免得用户被锁死在玩家路由);spectator 角色已被主按钮覆盖。 */}
                    {!(room.id && myRoles && myRoles[room.id] === 'spectator') && (
                      <button
                        type="button"
                        className="btn btn-sm btn-secondary room-list-spectate"
                        onClick={() => onSpectate(room.id)}
                        disabled={busy}
                        title={t('lobby.watchButton')}
                      >
                        👁 {t('lobby.watchButton')}
                      </button>
                    )}
                    <AdminDisbandButton
                      roomId={room.id}
                      roomStatus={room.status}
                      onDisbanded={(id) => onRemove(id)}
                      busy={busy}
                    />
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>

      <div className="room-list-pagination">
        <label className="room-list-pagesize">
          {t('lobby.pageSize')}
          <select
            value={pageSize}
            onChange={(e) => {
              setPageSize(Number(e.target.value));
              setPage(1);
            }}
          >
            {PAGE_SIZE_OPTIONS.map((n) => (
              <option key={n} value={n}>{n}</option>
            ))}
          </select>
        </label>
        <div className="room-list-nav">
          <button
            type="button"
            className="btn btn-sm btn-secondary"
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            disabled={page <= 1}
          >
            {t('lobby.prev')}
          </button>
          <span className="room-list-pageinfo">
            {t('lobby.pageInfo', { page, total: totalPages })}
          </span>
          <button
            type="button"
            className="btn btn-sm btn-secondary"
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            disabled={page >= totalPages}
          >
            {t('lobby.next')}
          </button>
        </div>
      </div>
    </div>
  );
}

export default RoomListTable;
