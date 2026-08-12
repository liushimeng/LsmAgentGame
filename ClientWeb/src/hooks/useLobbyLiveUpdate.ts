// useLobbyLiveUpdate — subscribe to `room.state` frames so a Lobby page can
// update its room list in real time without waiting for the 5s HTTP poll.
//
// Server fans out a `room.state` envelope to all lobby subscribers whenever
// `Hub.broadcastRoomStateChange` is called (joins, leaves, 15s timeouts,
// spectator joins, room deletions, etc.). The payload carries:
//
//   {
//     room:   { id, game_kind, capacity, current_count, status },
//     action: "player_joined" | "player_left" | "player_disconnecting" |
//             "player_reconnected" | "player_removed" | "spectator_joined" |
//             "spectator_left" | "room_created" | "room_deleted",
//     user_id: <string>,
//   }
//
// On "room_deleted" the entry should be removed from the local cache. On every
// other action the matching room is updated in place. The hook is filter-aware:
// the caller passes the `game_kind` it shows so updates from other games are
// silently dropped at this layer.

import { useEffect } from 'react';
import { wsClient } from '@/services/ws';
import type { RoomInfo } from '@/types/api';

interface RoomStatePayload {
  room: {
    id: string;
    game_kind: string;
    capacity: number;
    current_count: number;
    status: string;
  };
  action: string;
  user_id: string;
}

interface Args {
  /** Game kind this lobby displays — used to filter out unrelated updates. */
  gameKind: string;
  /** Patch a single room in place (current_count / status / spectator_count). */
  updateRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  /** Drop a room from the local cache (status === 'removed'). */
  removeRoom: (roomId: string) => void;
}

export function useLobbyLiveUpdate({ gameKind, updateRoom, removeRoom }: Args) {
  useEffect(() => {
    const off = wsClient.on((env) => {
      if (env.type !== 'room.state') return;
      const payload = env.payload as RoomStatePayload | undefined;
      if (!payload?.room?.id) return;
      if (payload.room.game_kind !== gameKind) return;
      if (payload.room.status === 'removed') {
        removeRoom(payload.room.id);
        return;
      }
      updateRoom({
        id: payload.room.id,
        game_kind: payload.room.game_kind,
        capacity: payload.room.capacity,
        current_count: payload.room.current_count,
        status: payload.room.status,
      });
    });
    return () => {
      off();
    };
  }, [gameKind, updateRoom, removeRoom]);
}