// AdminDisbandButton — 超管专属「⛔ 强制解散房间」按钮 + 二次确认。
//
// 5 个游戏大厅(ChessLobbyPage / DoudizhuLobbyPage / JunqiLobbyPage /
// TexasHoldemLobbyPage / WerewolfLobbyPage / XiangqiLobbyPage)都需要
// 这颗按钮 + 二次确认弹层。把它抽成共享组件后,每个 lobby 只需:
//   1. import { AdminDisbandButton } from '@/components/ui/AdminDisbandButton';
//   2. 把 <button>...room-card__spectate</button> 包进
//      <AdminDisbandButton roomId={room.id} onDisbanded={(id) => removeRoom(id)} />
//      中,或者直接两颗按钮平铺在 .room-card__actions 里。
//
// 服务端:DELETE /api/admin/rooms/:id(super admin only),WS 推 game.removed
// 让所有连接者退出。详见 ServerGo/api/admin_api.go:ForceDisbandRoom。
//
// §13 教训:超管(§13 userType>=3)专属;userType<3 时按钮不渲染,
// 服务端也会再校验一次(ErrPermissionDenied),双保险。

import { useCallback, useState } from 'react';
import { ApiError, isSessionExpiredError } from '@/services/http';
import { useAuthStore } from '@/store/auth.store';
import { forceDisbandRoom } from '@/api/admin';
import { ConfirmModal } from './ConfirmModal';

export interface AdminDisbandButtonProps {
  roomId: string;
  /** room.status === 'over' 时不渲染按钮 —— 不再骚扰管理员。 */
  hideWhenOver?: boolean;
  roomStatus?: string;
  /** 解散成功后回调(默认:从 lobby store 移除该房间)。 */
  onDisbanded?: (roomId: string) => void;
  /** 任意 loading 态(整个 lobby 可能正在创建 / 加入其他房间)。 */
  busy?: boolean;
  /** 二次确认失败时把错误向上抛(默认在按钮旁显示红字)。 */
  onError?: (msg: string) => void;
}

export function AdminDisbandButton({
  roomId,
  roomStatus,
  hideWhenOver = true,
  onDisbanded,
  busy = false,
  onError,
}: AdminDisbandButtonProps) {
  const userType = useAuthStore((s) => s.userType);
  const isSuperAdmin = (userType ?? 1) >= 3;

  const [pending, setPending] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [localErr, setLocalErr] = useState('');

  const handleConfirm = useCallback(async () => {
    setSubmitting(true);
    setLocalErr('');
    try {
      const res = await forceDisbandRoom(roomId, 'admin-force-disband');
      onDisbanded?.(res.room_id);
      setPending(false);
    } catch (e: unknown) {
      if (isSessionExpiredError(e)) {
        setPending(false);
        return;
      }
      const msg =
        e instanceof ApiError
          ? `[${e.code}] ${e.message}`
          : (e as Error)?.message ?? 'disband failed';
      setLocalErr(msg);
      onError?.(msg);
    } finally {
      setSubmitting(false);
    }
  }, [roomId, onDisbanded, onError]);

  if (!isSuperAdmin) return null;
  if (hideWhenOver && roomStatus === 'over') return null;

  return (
    <>
      <button
        type="button"
        className="btn btn-sm btn-danger room-card__admin-disband"
        onClick={(e) => {
          e.stopPropagation();
          setLocalErr('');
          setPending(true);
        }}
        disabled={busy || submitting}
        data-testid={`admin-disband-${roomId}`}
        title="强制解散该房间(所有玩家/观战者会被踢回大厅)"
      >
        ⛔ 强制解散
      </button>
      {pending && (
        <ConfirmModal
          message={`确认强制解散房间 ${roomId.slice(0, 8)}…?\n该房间所有玩家 / 观战者会立即被踢回大厅,且无法恢复。`}
          confirmLabel="强制解散"
          danger
          onConfirm={handleConfirm}
          onCancel={() => { if (!submitting) setPending(false); }}
        />
      )}
      {localErr && onError === undefined && (
        <div className="error" style={{ marginTop: 4 }}>解散失败:{localErr}</div>
      )}
    </>
  );
}
