// AppModal — 通用应用弹层组件
//
// 替代 ModelAdminPage / ConfirmModal 中内联的 settlement-overlay 块,
// 提供统一的:
//
//   - ESC 关闭
//   - 点击遮罩关闭
//   - 滚动锁定(打开时锁定 body 滚动)
//   - 自动焦点到第一个输入框 / 按钮
//   - 加载状态旋转图标
//   - 键盘可达性(role="dialog" / aria-modal / aria-label)
//
// §重构:ModelAdminPage 之前在 <style> 块内定义 .settlement-overlay 样式,
// 这导致 modal 出现样式冲突(父组件 <style> 与全局 globals.css 双重定义,
// 渲染顺序导致部分浏览器下 z-index=200 被覆盖)。AppModal 用全局 .app-modal-* 命名
// 空间避免冲突,并通过 createPortal 渲染到 body 顶层(防止父级 overflow/transform 截断)。
//
// §重构 (2026-07-14 R135):
//   - 新增 `blockBackdropClose` / `onBeforeClose` 解决"编辑型弹窗点击外面信息丢失"问题:
//     编辑型弹窗(新增/编辑表单)用户误点遮罩会丢失输入,现在:
//       1. `blockBackdropClose=true` → 遮罩点击 + ESC + × 按钮全部失效,弹窗变成真·模态阻塞;
//       2. `onBeforeClose?: () => boolean | Promise<boolean>` 返回 false 阻止关闭(脏表单提示);
//       3. 触发"阻止关闭"时弹窗整体 shake 动画 + 顶部红条提示"请点击取消/确认按钮关闭"。
//   - 修复 z-index 由 200 升到 300(已在 globals.css),与 GlobalToast(z=1000)共存。

import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

export type AppModalKind = 'info' | 'success' | 'error' | 'warning';

export interface AppModalProps {
  /** 弹层标题 */
  title: React.ReactNode;
  /** 弹层正文(任意 React 节点) */
  children: React.ReactNode;
  /** 标题前的 emoji / 图标字符,例如 "🤖" / "✅" / "❌" */
  icon?: string;
  /** 头部背景语义 — success=绿 / error=红 / warning=黄 / info=默认 */
  kind?: AppModalKind;
  /** 自定义最大宽度(像素),默认 520 */
  maxWidth?: number;
  /** 自定义 aria-label(若不传则使用 title 文本) */
  ariaLabel?: string;
  /** 点击遮罩或 ESC 是否关闭,默认 true。loading 时强制锁定关闭。 */
  dismissible?: boolean;
  /**
   * §R135 — 强制阻止遮罩关闭(以及 ESC)。常用于"编辑型"弹窗(新增/编辑表单),
   * 避免用户误点遮罩时丢失输入。loading 时仍然锁定。
   * 与 `onBeforeClose` 不冲突,本属性优先级最高。
   */
  blockBackdropClose?: boolean;
  /**
   * §R135 — 关闭前钩子(返回 `false` 阻止关闭)。
   * 可与 `blockBackdropClose=false` 共用,实现"脏表单"检测:
   *   onBeforeClose={() => isDirty ? !confirm('放弃编辑?') : true}
   * 注意:本钩子在以下情况**不会**被调用(直接拒绝关闭):
   *   1. `loading === true`(提交流程中)
   *   2. `blockBackdropClose === true`(彻底模态)
   *   3. `dismissible === false`(组件级不可关闭)
   */
  onBeforeClose?: () => boolean | Promise<boolean>;
  /** 关闭被阻止时顶部红条提示文案(默认"请点击取消或确认按钮关闭") */
  blockHint?: string;
  /** 是否处于加载中(禁用关闭 + 显示 spinner) */
  loading?: boolean;
  /** 底部按钮区 */
  footer?: React.ReactNode;
  /** 关闭回调 */
  onClose: () => void;
  /** 测试 ID */
  testId?: string;
}

const ICON_BG: Record<AppModalKind, string> = {
  info: 'linear-gradient(135deg, rgba(10,132,255,0.25), rgba(10,132,255,0.05))',
  success: 'linear-gradient(135deg, rgba(63,185,80,0.25), rgba(63,185,80,0.05))',
  error: 'linear-gradient(135deg, rgba(255,69,58,0.25), rgba(255,69,58,0.05))',
  warning: 'linear-gradient(135deg, rgba(255,159,10,0.25), rgba(255,159,10,0.05))',
};

const ICON_COLOR: Record<AppModalKind, string> = {
  info: '#0a84ff',
  success: '#3fb950',
  error: '#ff453a',
  warning: '#ff9f0a',
};

export function AppModal({
  title,
  children,
  icon,
  kind = 'info',
  maxWidth = 520,
  ariaLabel,
  dismissible = true,
  blockBackdropClose = false,
  onBeforeClose,
  blockHint,
  loading = false,
  footer,
  onClose,
  testId,
}: AppModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const modalRef = useRef<HTMLDivElement>(null);
  // §R135 — 关闭被阻止时,显示顶部红条 + shake 动画。3s 自动消失。
  const [blockedShake, setBlockedShake] = useState(0);
  const blockedTimerRef = useRef<number | null>(null);

  const showBlockedHint = useCallback(() => {
    setBlockedShake((n) => n + 1);
    if (blockedTimerRef.current) {
      window.clearTimeout(blockedTimerRef.current);
    }
    blockedTimerRef.current = window.setTimeout(() => {
      setBlockedShake(0);
      blockedTimerRef.current = null;
    }, 3000);
  }, []);

  useEffect(() => {
    return () => {
      if (blockedTimerRef.current) window.clearTimeout(blockedTimerRef.current);
    };
  }, []);

  const handleClose = useCallback(() => {
    if (loading) return;
    if (!dismissible) {
      showBlockedHint();
      return;
    }
    if (blockBackdropClose) {
      showBlockedHint();
      return;
    }
    if (onBeforeClose) {
      // onBeforeClose 可能是异步;若返回 false 则阻止关闭
      let allowed = true;
      try {
        const ret = onBeforeClose();
        if (ret instanceof Promise) {
          // 异步:等待 promise;false 时阻止
          ret.then((ok) => {
            if (ok) onClose();
            else showBlockedHint();
          }).catch(() => {
            showBlockedHint();
          });
          return;
        }
        allowed = ret;
      } catch {
        allowed = false;
      }
      if (!allowed) {
        showBlockedHint();
        return;
      }
    }
    onClose();
  }, [dismissible, blockBackdropClose, loading, onBeforeClose, onClose, showBlockedHint]);

  // ESC 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        handleClose();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [handleClose]);

  // 锁定 body 滚动
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = prev;
    };
  }, []);

  // 打开时焦点移到 modal
  useEffect(() => {
    modalRef.current?.focus();
  }, []);

  const label = typeof ariaLabel === 'string' && ariaLabel ? ariaLabel : (typeof title === 'string' ? title : undefined);
  const hintText = blockHint ?? '请点击取消或确认按钮关闭弹窗';
  const isDismissibleX = dismissible && !loading && !blockBackdropClose;

  return createPortal(
    <div
      ref={overlayRef}
      className="app-modal-overlay"
      onClick={handleClose}
      data-testid={testId}
    >
      <div
        ref={modalRef}
        className={
          'app-modal' +
          (blockedShake > 0 ? ' app-modal--shake' : '')
        }
        role="dialog"
        aria-modal="true"
        aria-label={label}
        tabIndex={-1}
        style={{ maxWidth }}
        onClick={(e) => e.stopPropagation()}
      >
        {blockedShake > 0 && (
          <div className="app-modal__block-hint" role="status" data-testid={testId ? `${testId}-block-hint` : undefined}>
            <span aria-hidden>⚠️</span>
            <span>{hintText}</span>
          </div>
        )}
        <div
          className="app-modal__header"
          style={{ background: ICON_BG[kind] }}
        >
          <div className="app-modal__icon" style={{ color: ICON_COLOR[kind] }}>
            {loading ? (
              <span className="app-modal__spinner" aria-hidden>⏳</span>
            ) : (
              <span>{icon ?? (kind === 'success' ? '✅' : kind === 'error' ? '❌' : kind === 'warning' ? '⚠️' : 'ℹ️')}</span>
            )}
          </div>
          <h2 className="app-modal__title">{title}</h2>
          {isDismissibleX && (
            <button
              type="button"
              className="app-modal__close"
              onClick={handleClose}
              aria-label="关闭"
              data-testid={testId ? `${testId}-close-x` : undefined}
            >
              ✕
            </button>
          )}
        </div>
        <div className="app-modal__body">{children}</div>
        {footer && <div className="app-modal__footer">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

export default AppModal;