// ModelTestResultPanel — 模型测试弹窗组件。从 ModelAdminPage 拆出,使主页
// 维持在 §4 1800 行硬上限之内。承载测试 loading / 成功 / 失败三态,以及
// §134 的完整请求/响应诊断折叠面板。
//
// §20260814-01 — 诊断文案兼容双协议:URL 与 Headers 由后端按协议渲染,
// 本组件仅展示,不再假设 "Anthropic 协议"。
import { createPortal } from 'react-dom';
import { AppModal } from '@/components/ui/AppModal';
import { useT } from '@/hooks/useT';
import type { LlmProvider, ProviderTestResult } from '@/types/model';

export interface TestDialogState {
  provider: LlmProvider;
  status: 'testing' | 'done';
  result?: ProviderTestResult;
  errorMessage?: string;
}

export function TestResultDialog({
  state,
  onClose,
}: {
  state: TestDialogState;
  onClose: () => void;
}) {
  const t = useT();
  const r = state.result;
  const isTesting = state.status === 'testing';
  const chatOK = !!r?.chat_ok;

  return createPortal(
    <AppModal
      title={
        isTesting
          ? `${t('modelAdmin.testDialogTitle')} · ${state.provider.agent_name}`
          : chatOK
            ? `${t('modelAdmin.testDialogTitle')} · ${state.provider.agent_name}`
            : t('modelAdmin.testDialogTitle')
      }
      icon={isTesting ? undefined : (chatOK ? '✅' : '❌')}
      kind={isTesting ? 'info' : (chatOK ? 'success' : 'error')}
      maxWidth={720}
      dismissible={!isTesting}
      loading={isTesting}
      testId="model-admin-test-modal"
      onClose={onClose}
      footer={
        !isTesting && (
          <button
            type="button"
            className="btn btn-primary"
            onClick={onClose}
            data-testid="test-modal-close"
          >
            {t('modelAdmin.testDialogClose')}
          </button>
        )
      }
    >
      {isTesting ? (
        <div className="test-dialog__loading" data-testid="test-modal-loading">
          <div className="test-dialog__loading-spinner" aria-hidden>⏳</div>
          <div className="test-dialog__loading-text">
            正在调用模型 <strong>{state.provider.model}</strong> …
          </div>
          <div className="test-dialog__loading-detail">
            发送测试提示,等待模型回复(具体协议由「协议」列决定,最长 15s)
          </div>
          <div
            className="test-dialog__code-block"
            style={{ marginTop: 14, textAlign: 'left' }}
          >
            {r?.prompt ?? 'Hello，请用中文回答你什么模型？都支持什么功能？200字以内？'}
          </div>
        </div>
      ) : state.errorMessage ? (
        <div className="test-dialog__section" data-testid="test-modal-error-wrap">
          <div className="test-dialog__section-title">调用失败</div>
          <div className="test-dialog__code-block test-dialog__reply--error">
            {state.errorMessage}
          </div>
        </div>
      ) : r ? (
        <>
          <div className="test-dialog__section">
            <div className="test-dialog__section-title">
              {t('modelAdmin.testDialogPrompt')}
            </div>
            <div
              className="test-dialog__code-block"
              data-testid="test-modal-prompt"
            >
              {r.prompt || '-'}
            </div>
          </div>

          <div className="test-dialog__section">
            <div className="test-dialog__section-title">
              {t('modelAdmin.testDialogReply')}
            </div>
            {chatOK ? (
              <div
                className="test-dialog__reply"
                data-testid="test-modal-reply"
              >
                {r.chat_text && r.chat_text.length > 0
                  ? r.chat_text
                  : t('modelAdmin.testDialogEmpty')}
              </div>
            ) : (
              <div
                className="test-dialog__code-block test-dialog__reply--error"
                data-testid="test-modal-error"
              >
                <strong>{t('modelAdmin.testDialogError')}：</strong>
                {r.chat_error ||
                  r.message ||
                  'unknown error'}
              </div>
            )}
          </div>

          <div
            className="test-dialog__meta"
            data-testid="test-modal-meta"
          >
            {t('modelAdmin.testDialogMeta', {
              ms: String(r.chat_latency_ms ?? 0),
              in_tok: String(r.chat_usage_input_tokens ?? 0),
              out_tok: String(r.chat_usage_output_tokens ?? 0),
              reason: r.chat_stop_reason || '-',
            })}
          </div>

          {r.hint && (
            <div
              className="test-dialog__hint"
              data-testid="test-modal-hint"
            >
              💡 {t('modelAdmin.testDialogHint')}：{r.hint}
            </div>
          )}

          {/* §134 — 完整请求 / 响应诊断折叠面板:无论成功失败,都展示
              出站 URL / Headers / Body 与上游 Status / Headers / Body,
              方便定位 placeholder / 401 / 400 / 网络层错误。默认收起
              (减少噪音),点击标题展开。后端已按协议渲染 URL 与 Headers。 */}
          <details
            className="test-dialog__diagnostics"
            data-testid="test-modal-diagnostics"
            style={{ marginTop: 12 }}
          >
            <summary
              className="test-dialog__diagnostics-summary"
              style={{ cursor: 'pointer', userSelect: 'none', fontWeight: 600 }}
            >
              🔍 查看完整请求 / 响应(Request URL · Headers · Body · Response Status · Headers · Body)
            </summary>
            <div style={{ marginTop: 10 }}>
              <div className="test-dialog__section-title">
                Request URL
              </div>
              <div
                className="test-dialog__code-block"
                data-testid="test-modal-request-url"
                style={{ wordBreak: 'break-all' }}
              >
                {r.request_url || '(unknown)'}
              </div>

              <div className="test-dialog__section-title" style={{ marginTop: 12 }}>
                Request Headers
              </div>
              <pre
                className="test-dialog__code-block"
                data-testid="test-modal-request-headers"
                style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}
              >
                {formatHeadersForDisplay(r.request_headers)}
              </pre>

              <div className="test-dialog__section-title" style={{ marginTop: 12 }}>
                Request Body
              </div>
              <pre
                className="test-dialog__code-block"
                data-testid="test-modal-request-body"
                style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all', maxHeight: 240, overflow: 'auto' }}
              >
                {r.request_body || '(empty)'}
              </pre>

              <div className="test-dialog__section-title" style={{ marginTop: 12 }}>
                Response Status
              </div>
              <div
                className="test-dialog__code-block"
                data-testid="test-modal-response-status"
              >
                {typeof r.response_status === 'number'
                  ? (r.response_status === 0
                      ? '0 (未发出去 / 网络层失败 / ctx timeout)'
                      : String(r.response_status))
                  : '(unknown)'}
              </div>

              <div className="test-dialog__section-title" style={{ marginTop: 12 }}>
                Response Headers
              </div>
              <pre
                className="test-dialog__code-block"
                data-testid="test-modal-response-headers"
                style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}
              >
                {formatHeadersForDisplay(r.response_headers)}
              </pre>

              <div className="test-dialog__section-title" style={{ marginTop: 12 }}>
                Response Body
              </div>
              <pre
                className="test-dialog__code-block"
                data-testid="test-modal-response-body"
                style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all', maxHeight: 240, overflow: 'auto' }}
              >
                {r.response_body || '(empty)'}
              </pre>
            </div>
          </details>
        </>
      ) : null}
    </AppModal>,
    document.body,
  );
}

// §134 — 把 Record<string,string> 头字典渲染成 "key: value" 多行文本。
// 与 JSON.stringify 不同,这里只展开一层并按 key 字母排序,运维肉眼
// 快速对比(curl -v 输出风格)。
export function formatHeadersForDisplay(h?: Record<string, string>): string {
  if (!h || Object.keys(h).length === 0) {
    return '(empty)';
  }
  const keys = Object.keys(h).sort();
  const max = Math.max(...keys.map((k) => k.length));
  return keys.map((k) => `${k.padEnd(max)}: ${h[k]}`).join('\n');
}
