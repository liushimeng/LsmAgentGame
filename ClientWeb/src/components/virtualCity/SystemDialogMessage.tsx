/**
 * SystemDialogMessage —— 「系统对话」单条消息（用户 / Agent）。
 *
 * 批次 81 方案 §4.3：
 *   - user      → 右侧气泡
 *   - assistant → 左侧气泡；streaming 带光标动画；tool_calls 渲染**默认折叠**的
 *     工具卡（🔧 工具名 · 参数摘要 · ✓/✗ · 时间，展开看完整 input/output）；
 *     show_commands 渲染「已调整视角」徽章；report 渲染 PredictionReportView；
 *     error 渲染内联红条（§7.1 内联层，全局 toast 由 hook 上报）。
 *
 * 批次 84 §4.3：新增 role='agent' —— 其它 Agent 的**上行来信**，渲染成
 * 「📨 来自 <Agent>」独立卡片（与用户 / 助手气泡明显区分，含来源 Agent、
 * 正文与时间）。
 *
 * 全部文案走 i18n `t()`（三语同步，缺键只降级为空串，绝不白屏）。
 */

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { VirtualCityPredictionReport } from '@/api/virtualCity';
import { PredictionReportView } from '@/components/virtualCity/PredictionReportView';
import type {
  VirtualCitySystemMessage,
  VirtualCityToolCallRecord,
} from '@/types/virtualCitySystem';

/** 参数摘要长度上限（超出截断，完整内容在展开区）。 */
const SUMMARY_MAX = 48;

/** 工具参数摘要：能 parse 成对象就取前两个键值对，否则原样截断。 */
function summarizeInput(input: string): string {
  const raw = (input ?? '').trim();
  if (!raw) return '—';
  if (raw.length <= SUMMARY_MAX) return raw;
  try {
    const obj = JSON.parse(raw) as Record<string, unknown>;
    if (obj && typeof obj === 'object') {
      const parts = Object.entries(obj)
        .slice(0, 2)
        .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`);
      const tail = Object.keys(obj).length > 2 ? '…' : '';
      const s = parts.join(' ');
      return s.length > SUMMARY_MAX ? `${s.slice(0, SUMMARY_MAX)}…` : `${s}${tail}`;
    }
  } catch {
    // 非 JSON：走下面的原始截断
  }
  return `${raw.slice(0, SUMMARY_MAX)}…`;
}

/**
 * report 负载守卫：`VirtualCitySystemMessage.report` 是 `unknown`（后端负载
 * 形状由 ws 层透传），渲染前收窄成 PredictionReportView 的入参类型。
 */
function isPredictionReport(v: unknown): v is VirtualCityPredictionReport {
  return typeof v === 'object' && v !== null;
}

/** unix 毫秒 → `HH:mm:ss`（24h，三语通用，避免 toLocaleTimeString 的 locale 差异）。 */
function fmtTime(ts: number): string {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return '';
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/**
 * 批次 84：显示指令提示文案（按提示码取 i18n；缺 key 回落 safeKey 串，
 * 绝不返回 undefined ⇒ 不白屏）。批次 85 起从 SystemDialogPanel 移入本文件
 * （顶栏横幅与消息流留痕共用一份文案逻辑，避免两处口径漂移）。
 */
export function noticeText(
  t: (key: TKey, vars?: Record<string, string | number>) => string,
  notice: { code: string; type: string; detail?: string },
): string {
  switch (notice.code) {
    case 'unsupported_type':
      return t('virtualCity.systemDialog.noticeUnsupportedType' as TKey, { type: notice.type });
    case 'unwired':
      return t('virtualCity.systemDialog.noticeUnwired' as TKey, { type: notice.type });
    case 'invalid_params':
      return t('virtualCity.systemDialog.noticeInvalidParams' as TKey, { type: notice.type });
    case 'entity_not_found': {
      // 批次 85：车辆 id 形如 `vehicle:<id>`，给出「车辆未找到」专属文案。
      const detail = notice.detail ?? notice.type;
      return detail.startsWith('vehicle:')
        ? t('virtualCity.systemDialog.noticeVehicleNotFound' as TKey, {
            detail: detail.replace(/^vehicle:/, ''),
          })
        : t('virtualCity.systemDialog.noticeEntityNotFound' as TKey, { detail });
    }
    case 'unknown_category':
      return t('virtualCity.systemDialog.noticeUnknownCategory' as TKey, {
        detail: notice.detail ?? notice.type,
      });
    case 'filter_showhide':
      // 批次 85 §3.2.3：实例级筛选退化为「按类别显隐」——明示口径，不静默。
      return t('virtualCity.systemDialog.noticeFilterShowhide' as TKey, {
        detail: notice.detail ?? notice.type,
      });
    default:
      return notice.type;
  }
}

/** 单条工具调用卡（默认折叠；⏳ = 已 start 未 result）。 */
function ToolCallCard({ rec }: { rec: VirtualCityToolCallRecord }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const pending = rec.text === '' && !rec.is_err;
  const stateIcon = pending ? '⏳' : rec.is_err ? '✗' : '✓';
  const stateKey: TKey = pending
    ? ('virtualCity.systemDialog.toolPending' as TKey)
    : rec.is_err
      ? ('virtualCity.systemDialog.toolFailed' as TKey)
      : ('virtualCity.systemDialog.toolOk' as TKey);
  return (
    <div
      className={
        'virtualCity-sysmsg__tool' + (rec.is_err ? ' virtualCity-sysmsg__tool--err' : '')
      }
      data-testid={`virtualCity-system-tool-${rec.tool}`}
    >
      <button
        type="button"
        className="virtualCity-sysmsg__tool-head"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-label={`${t('virtualCity.systemDialog.toolPrefix')}: ${rec.tool}`}
      >
        <span className="virtualCity-sysmsg__tool-summary">
          🔧 {rec.tool} · {summarizeInput(rec.input)} · {stateIcon} {t(stateKey)}
          {rec.ts ? ` · ${fmtTime(rec.ts)}` : ''}
        </span>
        <span className="virtualCity-sysmsg__tool-caret">{open ? '▴' : '▾'}</span>
      </button>
      {open && (
        <div className="virtualCity-sysmsg__tool-body">
          <div className="virtualCity-sysmsg__tool-row">
            <span className="virtualCity-sysmsg__tool-label">
              {t('virtualCity.systemDialog.toolInput')}
            </span>
            <pre className="virtualCity-sysmsg__tool-pre">{rec.input || '—'}</pre>
          </div>
          <div className="virtualCity-sysmsg__tool-row">
            <span className="virtualCity-sysmsg__tool-label">
              {t('virtualCity.systemDialog.toolOutput')}
            </span>
            <pre className="virtualCity-sysmsg__tool-pre">
              {rec.text || (pending ? t('virtualCity.systemDialog.toolRunning') : '—')}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
}

interface Props {
  message: VirtualCitySystemMessage;
}

export function SystemDialogMessage({ message }: Props) {
  const t = useT();
  const isUser = message.role === 'user';
  // 批次 84：其它 Agent 的上行来信（role='agent'）—— 独立卡片，不混进助手气泡。
  const isAgentLetter = message.role === 'agent';

  if (isAgentLetter) {
    return (
      <div
        className="virtualCity-sysmsg virtualCity-sysmsg--agent"
        data-testid="virtualCity-system-agent-letter"
      >
        <div className="virtualCity-sysmsg__letter-head">
          <span className="virtualCity-sysmsg__letter-icon" aria-hidden="true">📨</span>
          <span className="virtualCity-sysmsg__letter-from">
            {t('virtualCity.systemDialog.agentMessageFrom' as TKey)}{' '}
            {message.agent_from || t('virtualCity.systemDialog.agentMessageUnknown' as TKey)}
          </span>
          {message.ts ? (
            <span className="virtualCity-sysmsg__letter-time">{fmtTime(message.ts)}</span>
          ) : null}
        </div>
        <div className="virtualCity-sysmsg__letter-text">{message.text}</div>
      </div>
    );
  }

  // 批次 85 C2：显示指令提示**留痕行**（关窗 / 最小化期间产生的提示转入消息流，
  // 历史里永久可见；文案按当前语言实时渲染，复用横幅同一套 notice 文案逻辑）。
  if (message.role === 'notice') {
    return (
      <div className="virtualCity-sysmsg" data-testid="virtualCity-system-notice-row">
        <div className="virtualCity-sysdialog__notice" role="status">
          <span className="virtualCity-sysdialog__notice-text">
            {message.notice ? noticeText(t, message.notice) : message.text}
          </span>
          {message.ts ? (
            <span className="virtualCity-sysmsg__letter-time">{fmtTime(message.ts)}</span>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div
      className={
        'virtualCity-sysmsg' + (isUser ? ' virtualCity-sysmsg--user' : ' virtualCity-sysmsg--assistant')
      }
      data-testid="virtualCity-system-message"
    >
      <div className="virtualCity-sysmsg__bubble">
        {isUser ? (
          <span className="virtualCity-sysmsg__text">{message.text}</span>
        ) : (
          <>
            {message.text && (
              <span className="virtualCity-sysmsg__text">
                {message.text}
                {message.streaming && <span className="virtualCity-sysmsg__cursor" aria-hidden="true" />}
              </span>
            )}
            {!message.text && message.streaming && (
              <span className="virtualCity-sysmsg__text virtualCity-sysmsg__text--thinking">
                {t('virtualCity.systemDialog.busy')}
                <span className="virtualCity-sysmsg__cursor" aria-hidden="true" />
              </span>
            )}
          </>
        )}
      </div>

      {!isUser && message.tool_calls && message.tool_calls.length > 0 && (
        <div className="virtualCity-sysmsg__tools">
          {message.tool_calls.map((rec, i) => (
            <ToolCallCard rec={rec} key={`${rec.tool}-${i}`} />
          ))}
        </div>
      )}

      {!isUser && message.show_commands && message.show_commands.length > 0 && (
        <div className="virtualCity-sysmsg__badges">
          {message.show_commands.map((c, i) => (
            <span className="virtualCity-sysmsg__badge" key={`${c.type}-${i}`}>
              🎥 {t('virtualCity.systemDialog.showCommandBadge')}
            </span>
          ))}
        </div>
      )}

      {!isUser && isPredictionReport(message.report) && (
        <div className="virtualCity-sysmsg__report">
          <div className="virtualCity-sysmsg__report-badge">
            📊 {t('virtualCity.systemDialog.reportBadge')}
          </div>
          <PredictionReportView report={message.report as VirtualCityPredictionReport} />
        </div>
      )}

      {!isUser && message.error && (
        <div className="virtualCity-sysmsg__error" role="alert">
          {t('virtualCity.systemDialog.errorPrefix')}
          {message.error}
        </div>
      )}
    </div>
  );
}

export default SystemDialogMessage;
