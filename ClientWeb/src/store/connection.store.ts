import { create } from 'zustand';
import { wsClient, type WsStatus } from '@/services/ws';

// 连接状态 store —— 由 wsClient.onStatus 驱动，供全局 Loading 遮罩订阅。
// 不持久化：状态总是从实际连接重新推导。
export interface ConnectionState {
  status: WsStatus;
  /** 是否应展示 Loading 遮罩（连接中 / 重连中）。 */
  overlayVisible: boolean;
  setStatus: (s: WsStatus) => void;
}

export const useConnectionStore = create<ConnectionState>((set) => ({
  status: 'idle',
  overlayVisible: false,
  setStatus: (s) =>
    set({
      status: s,
      overlayVisible: s === 'connecting' || s === 'reconnecting',
    }),
}));

// 进程级订阅：把 wsClient 的状态变化同步进 store。模块首次 import 时建立一次，
// 之后所有连接/重连都会更新 store，无需组件手动订阅 wsClient。
wsClient.onStatus((s) => {
  useConnectionStore.getState().setStatus(s);
});
