import { useEffect, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { AppHeader } from './AppHeader';
import { AppSidebar } from './AppSidebar';
import { ChatPanel } from '@/components/chat/ChatPanel';
import { ReconnectingOverlay } from '@/components/common/ReconnectingOverlay';
import { ErrorBoundary } from '@/components/common/ErrorBoundary';
import { SessionExpiredToast } from '@/components/auth/SessionExpiredToast';
import { GlobalToast } from '@/components/common/GlobalToast';
import { useAuth } from '@/hooks/useAuth';
import { useAuthStore } from '@/store/auth.store';
import { useUiStore } from '@/store/ui.store';
import { useSessionRestore } from '@/hooks/useSessionRestore';
import { wsClient } from '@/services/ws';
// Importing the connection store wires wsClient.onStatus → store (side effect).
import '@/store/connection.store';

// AppLayout —— 全站统一布局：顶部标题栏/工具栏 + 左侧菜单栏 + 中间内容区 + 右侧聊天面板。
// 登录页（AuthModal）以模态层叠加在此布局之上，无需单独布局。
// 聊天面板仅在已登录时显示，避免匿名用户看到一个永远连不上的面板。
//
// AppLayout 是唯一的 WS 生命周期持有者：登录后建立连接、登出后关闭。页面/路由切换
// 不再各自 connect/close，避免误关唯一连接。断线重连与状态恢复由 useSessionRestore
// 统一编排，连接中/重连中由 ReconnectingOverlay 展示全屏 Loading。
export function AppLayout({ children }: { children: ReactNode }) {
  const isAuth = useAuth((s) => s.isAuthenticated);
  const fullscreen = useUiStore((s) => s.fullscreen);
  const setBreakpoint = useUiStore((s) => s.setBreakpoint);
  const hydrateFromMedia = useUiStore((s) => s.hydrateFromMedia);
  const navigate = useNavigate();

  // 在 AppLayout 挂载时立即把 persisted token 同步到 http.ts 的模块级缓存。
  // zustand `persist` 已经在 rehydration 阶段把 lsm.auth 写回了 store，但 http.ts
  // 的 `getAuthToken()` 第一行只读自己的模块级 cache——它在本次页面加载内可能
  // 从未被 setAuthToken 触碰过，导致 F5 后首次 wsClient.connect() 看到 token
  // 为空直接早返回。AppLayout 是最长寿的组件，让它承担一次性 bootstrap 责任。
  useEffect(() => {
    useAuthStore.getState().hydrateFromStorage();
  }, []);

  // 全站唯一的 WS 生命周期：登录后连接，登出后关闭。
  useEffect(() => {
    if (isAuth) {
      // 复用一个微任务：等上一行 hydrateFromStorage 把模块级 token 缓存好。
      // React 18 自动批处理足以让两次 setState 都生效后再 connect。
      wsClient.connect();
      return () => wsClient.close();
    }
  }, [isAuth]);

  // 浏览器从后台切回前台时，浏览器/网络栈可能已经把 socket 杀掉。强制
  // 重新评估连接状态——如果已经断开，会走标准重连流程；如果还活着则
  // 早返回。这是 headless Chrome / 移动端 Safari 上防止「页面静默失活」
  // 的最后一公里保险。
  useEffect(() => {
    if (!isAuth) return;
    const onVisibility = () => {
      if (document.visibilityState === 'visible' && !wsClient.connected) {
        wsClient.connect();
      }
    };
    document.addEventListener('visibilitychange', onVisibility);
    return () => document.removeEventListener('visibilitychange', onVisibility);
  }, [isAuth]);

  // 断线重连/刷新后的房间与对局状态恢复。
  useSessionRestore();

  // 全局"去领取每日奖励"事件：大厅/房间创建对话框的「余额不足提示」按钮触发此事件。
  // 在 AppLayout 全局挂载监听，统一跳转到个人中心（ProfilePage）执行领取，
  // 避免在多处散落重复的事件监听/导航代码，也避免点击后无响应（即用户口中的
  // "黑屏 / 没反应"）。ProfilePage 已有完整领取 UI 与成功 toast。
  useEffect(() => {
    const onClaimDaily = () => {
      navigate('/profile', { state: { focusClaim: true } });
    };
    window.addEventListener('wallet:claimDaily', onClaimDaily);
    return () => window.removeEventListener('wallet:claimDaily', onClaimDaily);
  }, [navigate]);

  // 监听视口尺寸,实时同步断点
  useEffect(() => {
    const detect = () => {
      const w = window.innerWidth;
      if (w <= 640) setBreakpoint('mobile');
      else if (w <= 1024) setBreakpoint('pad');
      else setBreakpoint('desktop');
    };
    detect();
    hydrateFromMedia();
    window.addEventListener('resize', detect);
    return () => window.removeEventListener('resize', detect);
  }, [setBreakpoint, hydrateFromMedia]);

  return (
    <div className={'app-shell' + (fullscreen ? ' app-shell--fullscreen' : '')}>
      <SessionExpiredToast />
      <GlobalToast />
      {!fullscreen && (
        <ErrorBoundary label="AppHeader">
          <AppHeader />
        </ErrorBoundary>
      )}
      <div className="app-body">
        {!fullscreen && <AppSidebar />}
        <ErrorBoundary
          label="页面内容"
          fallback={(_error, reset) => (
            <div className="page-fatal-error">
              <h2>页面渲染异常</h2>
              <p>该页面出现错误,已自动隔离。下方按钮可重试或刷新。</p>
              <div className="page-fatal-error__actions">
                <button type="button" onClick={reset}>重试渲染</button>
                <button type="button" onClick={() => window.location.reload()}>刷新页面</button>
              </div>
            </div>
          )}
        >
          <main className="content">{children}</main>
        </ErrorBoundary>
        {isAuth && !fullscreen && <ChatPanel />}
        {isAuth && fullscreen && <ChatPanel />}
      </div>
      {isAuth && <ReconnectingOverlay />}
    </div>
  );
}
