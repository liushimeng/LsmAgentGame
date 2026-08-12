// Hook that exposes the latest WS envelope. The connection lifecycle itself is
// owned by AppLayout (single source of truth); this hook only observes traffic
// and must NOT connect or close the shared socket.

import { useEffect, useState } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';

export function useWebSocket() {
  const [last, setLast] = useState<WsEnvelope | null>(null);

  useEffect(() => {
    // Observe only — do not connect/close. AppLayout manages the lifecycle.
    const off = wsClient.on(setLast);
    return off;
  }, []);

  return last;
}
