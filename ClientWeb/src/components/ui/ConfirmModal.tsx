import { createPortal } from 'react-dom';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

export interface ConfirmModalProps {
  /** 提示文案（i18n key）。与 `message` 互斥，优先级低于 `message`。 */
  messageKey?: TKey;
  /** 已插值的提示文案（字符串）。优先级高于 `messageKey`，用于动态变量（如 {count}）场景。 */
  message?: string;
  /** 取消按钮文案（i18n key）。默认 common.cancel。 */
  cancelKey?: TKey;
  /** 已插值的取消按钮文案（字符串）。优先级高于 `cancelKey`。 */
  cancelLabel?: string;
  /** 确认按钮文案（i18n key）。默认 common.confirm。 */
  confirmKey?: TKey;
  /** 已插值的确认按钮文案（字符串）。优先级高于 `confirmKey`。 */
  confirmLabel?: string;
  /** 危险操作（红色确认按钮）。默认 false。 */
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * ConfirmModal —— 替代 window.confirm 的可挂载确认弹层。
 *
 * 5 个 GamePage（xiangqi/chess/junqi/doudizhu/texasholdem）的认输按钮
 * 原本调用 window.confirm，会阻塞 CDP 自动化测试且无法自定义样式。
 * 该组件通过 React 渲染，纯展示、无副作用，匹配现有 SettlementModal 的样式约定。
 *
 * BUG-FORCEDISBAND-LAYOUT FIX (Round 24): 弹层现在通过 React Portal
 * 渲染到 document.body,而不是父组件所在的 DOM 节点。当父组件是
 * `<td>` 或 `<RoomListTable>` 等不允许 fixed-position 子节点的容器时,
 * 这层解耦让 modal 永远能正确居中显示,不会撑爆父容器布局。
 */
export function ConfirmModal({
  messageKey,
  message,
  cancelKey,
  cancelLabel,
  confirmKey,
  confirmLabel,
  danger = false,
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  const t = useT();

  // 字符串优先；否则走 i18n key 翻译（messageKey 为可选；缺省则用 common.cancel 作为兜底）。
  const messageText = message ?? (messageKey ? t(messageKey) : t('common.cancel' as TKey));
  const cancelText = cancelLabel ?? t(cancelKey ?? ('common.cancel' as TKey));
  const confirmText = confirmLabel ?? t(confirmKey ?? ('common.confirm' as TKey));

  return createPortal(
    <div className="settlement-overlay" onClick={onCancel}>
      <div
        className="settlement-modal"
        onClick={(e) => e.stopPropagation()}
        role="alertdialog"
        aria-label={messageText}
      >
        <div className="settlement-modal__header">
          <span className="settlement-result settlement-result--lose">
            {danger ? '⚠️' : '❓'}
          </span>
        </div>

        <div className="settlement-modal__body">
          <p style={{ margin: 0, textAlign: 'center', lineHeight: 1.5 }}>
            {messageText}
          </p>
        </div>

        <div
          style={{
            display: 'flex',
            gap: 12,
            justifyContent: 'center',
            marginTop: 16,
          }}
        >
          <button
            type="button"
            className="btn btn-secondary"
            onClick={onCancel}
            data-testid="confirm-cancel"
          >
            {cancelText}
          </button>
          <button
            type="button"
            className={danger ? 'btn btn-danger' : 'btn btn-primary'}
            onClick={onConfirm}
            data-testid="confirm-ok"
            autoFocus
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}