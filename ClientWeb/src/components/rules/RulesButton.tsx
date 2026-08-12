// RulesButton + embedded RulesViewer — a one-liner "规则说明" button that
// owns its own open state and renders the DraggableModal as a sibling.
//
// Why a self-contained button (rather than a hook + manual state in the page):
//   - 5 GameInfoPanel files would each need a useState + 2 render branches
//     if we put the state at the page level. A self-contained button drops
//     the integration cost to a single <RulesButton kind="xiangqi" /> line.
//   - The modal is a fixed-position overlay so it doesn't conflict with the
//     panel-section flex layout around it.
//   - The viewer is keyed on `kind` so 5 buttons can never accidentally share
//     state across games on the same page (defensive; not currently used, but
//     future "all-games cheatsheet" page could host several at once).
//
// The viewer uses the same module-level cache as RulesViewer, so re-opens
// are instant after the first fetch.

import { useState, useCallback } from 'react';
import { RulesViewer } from '@/components/rules/RulesViewer';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { GameKind } from '@/rules/gameRules';

export interface RulesButtonProps {
  kind: GameKind;
  /** Optional extra className appended to the trigger button. */
  className?: string;
  /** Test id for the trigger button (CDP-friendly). */
  testId?: string;
}

export function RulesButton({ kind, className, testId }: RulesButtonProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const onOpen = useCallback(() => setOpen(true), []);
  const onClose = useCallback(() => setOpen(false), []);
  return (
    <>
      <button
        type="button"
        className={`btn btn-secondary rules-button ${className ?? ''}`.trim()}
        onClick={onOpen}
        data-testid={testId ?? `rules-button-${kind}`}
        aria-haspopup="dialog"
      >
        {t('rules.button' as TKey)}
      </button>
      {open && <RulesViewer kind={kind} open={open} onClose={onClose} />}
    </>
  );
}
