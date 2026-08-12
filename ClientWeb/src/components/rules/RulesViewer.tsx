// RulesViewer — fetches the markdown rules for a given game_kind and renders
// it inside a DraggableModal. Used by all 5 GamePages (xiangqi/chess/junqi/
// doudizhu/texasholdem) to expose a "规则说明" button.
//
// Lifecycle:
//   1. open === false → render nothing
//   2. open === true → fetch /rules/<kind>.md on first open
//   3. cache the result in module state so subsequent opens are instant
//   4. on fetch error, show a small "加载失败" message; on success, render
//      via the built-in renderMarkdown() (no external dep).
//
// Why a separate component rather than inlining in each GamePage:
//   - 5 GamePages already diverge on board/lifecycle — centralizing the
//     "fetch + render + drag" pipeline keeps each page's edit surface tiny.
//   - Future games (e.g. 狼人杀) can drop in a single <RulesViewer kind=...
//     /> without rewriting any fetch/parsing code.
//
// Markdown source files live in ClientWeb/public/rules/<kind>.md (Vite serves
// /public/ at the site root in both dev and prod).

import { useEffect, useState, type ReactNode } from 'react';
import { DraggableModal } from '@/components/common/DraggableModal';
import { renderMarkdown } from '@/rules/renderMarkdown';
import { GAME_ACCENT, type GameKind, rulesUrl } from '@/rules/gameRules';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

export interface RulesViewerProps {
  kind: GameKind;
  open: boolean;
  onClose: () => void;
  /** Optional custom title override. Defaults to a per-kind i18n key. */
  titleKey?: TKey;
  /** Default modal size — rules are long, so width is generous by default. */
  width?: number;
  height?: number;
}

// Module-level cache: fetching the same rules twice in a session is wasteful.
// Keyed by the URL the RulesViewer would fetch.
const cache = new Map<string, string>();

export function RulesViewer({
  kind,
  open,
  onClose,
  titleKey,
  width = 760,
  height = 600,
}: RulesViewerProps) {
  const t = useT();
  const [md, setMd] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const url = rulesUrl(kind);
  const accent = GAME_ACCENT[kind];

  useEffect(() => {
    if (!open) return;
    const cached = cache.get(url);
    if (cached !== undefined) {
      setMd(cached);
      setError(null);
      return;
    }
    let cancelled = false;
    setError(null);
    setMd(null);
    fetch(url, { cache: 'no-cache' })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.text();
      })
      .then((text) => {
        if (cancelled) return;
        cache.set(url, text);
        setMd(text);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [open, url]);

  const titleNode: ReactNode = titleKey
    ? t(titleKey)
    : t(`rules.title.${kind}` as TKey);

  return (
    <DraggableModal
      open={open}
      onClose={onClose}
      title={titleNode}
      accent={accent}
      defaultWidth={width}
      defaultHeight={height}
    >
      {error ? (
        <div
          style={{
            color: 'var(--danger)',
            padding: 12,
            border: '1px solid var(--danger)',
            borderRadius: 6,
            background: 'rgba(248, 81, 73, 0.08)',
          }}
        >
          {t('rules.loadError' as TKey)}: {error}
          <div style={{ marginTop: 8, fontSize: 12, color: 'var(--muted)' }}>
            {t('rules.loadErrorHint' as TKey)}
          </div>
        </div>
      ) : md === null ? (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--muted)',
            padding: 24,
          }}
        >
          {t('common.loading')}
        </div>
      ) : (
        renderMarkdown(md)
      )}
    </DraggableModal>
  );
}
