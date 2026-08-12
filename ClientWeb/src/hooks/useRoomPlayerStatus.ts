import { useEffect, useCallback, useState } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';

export interface PlayerStatus {
  roomId: string;
  action: 'disconnecting' | 'reconnected' | 'removed';
  userId: string;
  timestamp: number;
}

interface UseRoomPlayerStatusOptions {
  roomId: string;
  onPlayerDisconnecting?: (userId: string) => void;
  onPlayerReconnected?: (userId: string) => void;
  onPlayerRemoved?: (userId: string) => void;
}

/**
 * Hook that monitors room player status changes (disconnect/reconnect/remove).
 * Automatically subscribes to room.player_status WS frames for the specified room.
 */
export function useRoomPlayerStatus(options: UseRoomPlayerStatusOptions) {
  const { roomId, onPlayerDisconnecting, onPlayerReconnected, onPlayerRemoved } = options;
  const [disconnectedPlayers, setDisconnectedPlayers] = useState<Set<string>>(new Set());

  useEffect(() => {
    const unsub = wsClient.on((env: WsEnvelope) => {
      if (env.type !== 'room.player_status') return;

      const payload = env.payload as {
        room_id: string;
        action: string;
        user_id: string;
      };

      // Only process events for our room
      if (payload.room_id !== roomId) return;

      const { action, user_id } = payload;

      switch (action) {
        case 'disconnecting':
          setDisconnectedPlayers((prev) => {
            const next = new Set(prev);
            next.add(user_id);
            return next;
          });
          onPlayerDisconnecting?.(user_id);
          console.log(`[Room ${roomId}] Player ${user_id} is disconnecting...`);
          break;

        case 'reconnected':
          setDisconnectedPlayers((prev) => {
            const next = new Set(prev);
            next.delete(user_id);
            return next;
          });
          onPlayerReconnected?.(user_id);
          console.log(`[Room ${roomId}] Player ${user_id} has reconnected`);
          break;

        case 'removed':
          setDisconnectedPlayers((prev) => {
            const next = new Set(prev);
            next.delete(user_id);
            return next;
          });
          onPlayerRemoved?.(user_id);
          console.log(`[Room ${roomId}] Player ${user_id} has been removed (timeout)`);
          break;
      }
    });

    return () => unsub();
  }, [roomId, onPlayerDisconnecting, onPlayerReconnected, onPlayerRemoved]);

  const isPlayerDisconnected = useCallback(
    (userId: string) => disconnectedPlayers.has(userId),
    [disconnectedPlayers],
  );

  return {
    disconnectedPlayers,
    isPlayerDisconnected,
  };
}
