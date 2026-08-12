import { Component, ErrorInfo, ReactNode } from 'react';

interface Props {
  children: ReactNode;
  /** Custom fallback UI; receives the thrown error. */
  fallback?: (error: Error, reset: () => void) => ReactNode;
  /** Optional label used in the default fallback card. */
  label?: string;
}

interface State {
  error: Error | null;
}

/**
 * Minimal error boundary used to isolate WebGL / 3D scenes from the rest of
 * the page so that a render-time crash in one widget doesn't blank the whole
 * route. Inspired by the test-report Bug 4.1: a headless Chrome with no GPU
 * could crash the <LobbyScene /> Three.js canvas and unmount the entire HomePage.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // eslint-disable-next-line no-console
    console.error('[ErrorBoundary]', this.props.label ?? 'unlabeled', error, info.componentStack);
  }

  reset = (): void => {
    this.setState({ error: null });
  };

  render(): ReactNode {
    const { error } = this.state;
    if (!error) return this.props.children;
    if (this.props.fallback) return this.props.fallback(error, this.reset);
    return (
      <div
        role="alert"
        style={{
          padding: 16,
          color: 'var(--text-muted, #888)',
          fontSize: 13,
          background: 'var(--surface-muted, rgba(255,255,255,0.04))',
          border: '1px dashed var(--border, #444)',
          borderRadius: 8,
        }}
      >
        <div style={{ marginBottom: 8 }}>
          {this.props.label ? `${this.props.label} 加载失败` : '组件渲染失败'}
        </div>
        <div style={{ fontFamily: 'monospace', opacity: 0.8, whiteSpace: 'pre-wrap' }}>
          {error.message}
        </div>
      </div>
    );
  }
}