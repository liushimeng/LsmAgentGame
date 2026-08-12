// DraggableModal — modal with header-drag, X close, and a scrollable body.
//
// Visual style is intentionally close to settlement-overlay / settlement-modal
// (used by ConfirmModal / SettlementModal) so the rules viewer feels native
// to the rest of the game UI. Differences from ConfirmModal:
//   - position is fixed + draggable (the whole panel is the drag target only
//     on the header — body interactions stay normal)
//   - body scrolls independently
//   - max size larger (rules can be long)
//
// Used by RulesViewer; also exported for any future "draggable info panel"
// need (e.g. floating dev console). Not a replacement for ConfirmModal —
// confirm dialogs should stay centered & modal-blocking.

import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';

export interface DraggableModalProps {
  /** Modal title (rendered in header). */
  title: ReactNode;
  /** Whether the modal is open. */
  open: boolean;
  /** Called when the user clicks the X or the backdrop. */
  onClose: () => void;
  /** Optional accent color for the header / left border. */
  accent?: string;
  /** Default size: width in px, height in px. Defaults to 720 × 560. */
  defaultWidth?: number;
  defaultHeight?: number;
  children: ReactNode;
  /** Optional footer (rendered in a sticky footer slot). */
  footer?: ReactNode;
}

const HEADER_HEIGHT = 44;

export function DraggableModal({
  title,
  open,
  onClose,
  accent,
  defaultWidth = 720,
  defaultHeight = 560,
  children,
  footer,
}: DraggableModalProps) {
  // Initial position: centered with a small offset so the header is reachable
  // and the user immediately sees the drag affordance.
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);
  const dragRef = useRef<{
    pointerId: number;
    startX: number;
    startY: number;
    origX: number;
    origY: number;
  } | null>(null);

  // Recenter when opening.
  useEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    const cx = Math.max(24, (window.innerWidth - defaultWidth) / 2);
    const cy = Math.max(24, (window.innerHeight - defaultHeight) / 3);
    setPos({ x: cx, y: cy });
  }, [open, defaultWidth, defaultHeight]);

  // Esc to close.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  const onHeaderPointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (!pos) return;
      // Ignore clicks on the close button (it has its own handler).
      if ((e.target as HTMLElement).closest('button')) return;
      dragRef.current = {
        pointerId: e.pointerId,
        startX: e.clientX,
        startY: e.clientY,
        origX: pos.x,
        origY: pos.y,
      };
      (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    },
    [pos],
  );

  const onHeaderPointerMove = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      const d = dragRef.current;
      if (!d || d.pointerId !== e.pointerId || !pos) return;
      const dx = e.clientX - d.startX;
      const dy = e.clientY - d.startY;
      const maxX = window.innerWidth - 80;
      const maxY = window.innerHeight - 80;
      setPos({
        x: Math.max(8, Math.min(maxX, d.origX + dx)),
        y: Math.max(8, Math.min(maxY, d.origY + dy)),
      });
    },
    [pos],
  );

  const onHeaderPointerUp = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (dragRef.current?.pointerId === e.pointerId) {
        dragRef.current = null;
        try {
          (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
        } catch {
          // pointer may already be released
        }
      }
    },
    [],
  );

  if (!open || !pos) return null;

  const style: React.CSSProperties = {
    position: 'fixed',
    top: pos.y,
    left: pos.x,
    width: defaultWidth,
    height: defaultHeight,
    maxWidth: 'calc(100vw - 16px)',
    maxHeight: 'calc(100vh - 16px)',
    background: 'var(--panel)',
    border: '1px solid var(--border)',
    borderRadius: 10,
    boxShadow: '0 16px 48px rgba(0, 0, 0, 0.55)',
    display: 'flex',
    flexDirection: 'column',
    zIndex: 200,
    overflow: 'hidden',
    ...(accent
      ? { borderLeft: `4px solid ${accent}` }
      : {}),
  };

  return (
    <>
      {/* Backdrop — click to close */}
      <div
        className="draggable-modal-backdrop"
        onClick={onClose}
        style={{
          position: 'fixed',
          inset: 0,
          background: 'rgba(0, 0, 0, 0.55)',
          zIndex: 199,
        }}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === 'string' ? title : undefined}
        data-testid="draggable-modal"
        style={style}
      >
        <div
          onPointerDown={onHeaderPointerDown}
          onPointerMove={onHeaderPointerMove}
          onPointerUp={onHeaderPointerUp}
          onPointerCancel={onHeaderPointerUp}
          style={{
            height: HEADER_HEIGHT,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '0 12px 0 16px',
            borderBottom: '1px solid var(--border)',
            background: 'var(--card)',
            cursor: 'grab',
            userSelect: 'none',
            flex: '0 0 auto',
            ...(accent
              ? {
                  background: `linear-gradient(90deg, ${accent}22, var(--card) 60%)`,
                }
              : {}),
          }}
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              fontWeight: 600,
              fontSize: 14,
              color: 'var(--text)',
              overflow: 'hidden',
              whiteSpace: 'nowrap',
              textOverflow: 'ellipsis',
            }}
          >
            {accent && (
              <span
                style={{
                  width: 8,
                  height: 8,
                  borderRadius: '50%',
                  background: accent,
                  display: 'inline-block',
                  flex: '0 0 auto',
                }}
              />
            )}
            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {title}
            </span>
            <span
              style={{
                fontSize: 11,
                color: 'var(--muted)',
                fontWeight: 400,
                marginLeft: 8,
              }}
            >
              ↕ 拖动 · Esc 关闭
            </span>
          </div>
          <button
            type="button"
            aria-label="关闭"
            data-testid="draggable-modal-close"
            onClick={onClose}
            className="btn btn-secondary"
            style={{
              padding: '2px 10px',
              minHeight: 28,
              fontSize: 14,
              lineHeight: 1.2,
            }}
          >
            ✕
          </button>
        </div>

        <div
          style={{
            flex: '1 1 auto',
            overflowY: 'auto',
            overflowX: 'hidden',
            padding: '16px 20px',
            color: 'var(--text)',
            lineHeight: 1.65,
          }}
        >
          {children}
        </div>

        {footer && (
          <div
            style={{
              flex: '0 0 auto',
              borderTop: '1px solid var(--border)',
              padding: '8px 16px',
              background: 'var(--card)',
              display: 'flex',
              justifyContent: 'flex-end',
              gap: 8,
            }}
          >
            {footer}
          </div>
        )}
      </div>
    </>
  );
}
