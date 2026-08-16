// ModelAdminPage — /admin/models
// Admin LLM Provider CRUD page. Mirrors the style of AdminUsersPage (table
// inside a card, gradient title, action buttons in the right column).
// All API calls go through `api/modelAdmin.ts`; row state lives in
// `store/modelAdmin.store.ts`. Backend may not be implemented yet — UI is
// built per plan §2.5 contract and will work once the Go endpoints land.
//
// §重构 (2026-07-12):
//   1. 「新增模型」按钮 — 之前位于工具栏,但因样式与按钮尺寸不够显眼,用户在
//      长列表(8 个默认模型)下经常找不到。现在升级为「卡片 + 大号主按钮」,
//      顶部 toolbar 改为 flex 布局并加上明显的渐变背景,显眼度提升。
//   2. 测试按钮弹窗 — 之前点击「测试」会同时显示 toast(右上角浮动提示)
//      和模态弹窗,但 toast 3s 后消失而弹窗**没有 loading 状态**,用户在
//      15s LLM 调用等待期间完全没反馈,以为"测试按钮没反应"。现在新增
//      测试中 → 测试完成 二态过渡,弹窗立即弹出,显示调用进度。
//   3. 弹窗细节 — 引入统一的 AppModal 组件,提供 ESC 关闭、滚动锁定、
//      自动焦点、加载 spinner、关闭按钮 × 等细节优化。
//
// §重构 (2026-07-13 R133):
//   1. **仅超级管理员可见** — 前端 isAdmin 判定从 `userType >= 2` 升级为
//      `userType === 3`,与后端 requireSuper 中间件保持一致(见
//      `ServerGo/api/model_admin_api.go:131`)。普通用户/管理员访问路由
//      也会展示「仅超级管理员可访问」提示。
//   2. **表格列完整显示** — 给 `api_key_hint` / `endpoint` / `model` /
//      `remark` 列加 `word-break:break-all` + 取消 `text-overflow:ellipsis`
//      截断,确保长字符串完整展示。新增「API Key 切换显示」按钮,默认显示
//      hint 完整内容,可一键切换为「脱敏视图」(前端四级 re-mask 是兜底
//      防御,后端永远只返回 hint,不返回明文)。
//   3. **新增「备注」列** — 之前只在弹窗内部可见,现在表格独立列
//      `min-width:160px; white-space:pre-wrap`,完整保留多行内容。
//   4. **编辑弹窗加宽 + 双列** — maxWidth 620 → 760,FormField 引入
//      `half` / `full` 宽度变体;长字段(agent_name / endpoint / remark)
//      升级为 textarea;确定按钮依然支持只修改单字段(后端 PUT 接受
//      Partial<LlmProviderCreate>,未提供的字段保持不变)。

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/store/auth.store';
import { useModelAdminStore } from '@/store/modelAdmin.store';
import { grantDailyToAll, type GrantDailyResponse } from '@/api/modelAdmin';
import { getRadarStats, type ModelRadarStats } from '@/api/llm';
import { reportGlobalError } from '@/services/globalError';
import { useT } from '@/hooks/useT';
import { useI18nStore } from '@/store/i18n.store';
import { formatBalance } from '@/shared/utils/balance';
import { AppModal } from '@/components/ui/AppModal';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import { ModelRadarChart } from '@/components/common/ModelRadarChart';
// §20260813-02 U1/U2 — 胜率趋势 + 道具经济分析(自包含组件,页内仅 1 行接线,
// 避免本页突破 §4 1800 行上限)。
import { ModelAnalyticsSection } from '@/components/common/ModelAnalyticsPanels';
import type { LlmProvider, LlmProviderCreate } from '@/types/model';
// §20260814-01 — 双协议常量:规范值 + 下拉选项 + 归一化(兼容存量旧值)。
import {
  PROVIDER_PROTOCOL_OPTIONS,
  normalizeProviderProtocol,
  type ProviderProtocol,
} from '@/types/model';
// §20260814-01 — 测试弹窗组件(从本页拆出以维持 §4 1800 行上限)。
import {
  TestResultDialog,
  type TestDialogState,
} from '@/components/common/ModelTestResultPanel';

// 后端永远只下发 api_key_hint("sk-XXXX…YYYY" / "API-KEY-PLACEHOLDER")
// 形式,明文 api_key 不会回客户端(见
// `ServerGo/api/model_admin_api.go:19` 注释 + `registry.go:334`)。
// 这里把 hint 完整展示,只在用户主动勾选「脱敏」时才截断。
function maskApiKey(hint: string): string {
  if (!hint) return '-';
  if (hint.length <= 12) return hint;
  return hint.slice(0, 4) + '…' + hint.slice(-4);
}

function formatDate(iso: string): string {
  if (!iso) return '-';
  try {
    return new Date(iso).toLocaleString('zh-CN');
  } catch {
    return iso;
  }
}

interface FormState {
  agent_name: string;
  model: string;
  provider_type: string;
  api_key: string;
  endpoint: string;
  // §R224 (2026-08-01) — 重新引入 §128 误删的 extended thinking 配置。
  // 8 家代理必须打开，否则 400 "missing messages.content.thinking"。
  thinking_enabled: boolean;
  thinking_budget_tokens: number;
  enabled: boolean;
  remark: string;
}

const EMPTY_FORM: FormState = {
  agent_name: '',
  model: '',
  provider_type: 'anthropic',
  api_key: '',
  endpoint: '',
  // §R224 — 默认 thinking 关闭，operator 可在表单中勾选开启。
  thinking_enabled: false,
  thinking_budget_tokens: 4096,
  enabled: true,
  remark: '',
};

function toForm(p: LlmProvider): FormState {
  return {
    agent_name: p.agent_name,
    model: p.model,
    provider_type: p.provider_type,
    api_key: '', // never echo back
    endpoint: p.endpoint ?? '',
    // §R224 — 从已存在 provider 回填；旧行(§128 后入库)两列 0/false，
    // 回填到 EMPTY_FORM 默认值(关 + 4096)，语义与新建一致。
    thinking_enabled: p.thinking_enabled ?? false,
    thinking_budget_tokens: p.thinking_budget_tokens ?? 4096,
    enabled: p.enabled,
    remark: p.remark ?? '',
  };
}

export function ModelAdminPage() {
  const navigate = useNavigate();
  const t = useT();
  const lang = useI18nStore((s) => s.lang);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const userType = useAuthStore((s) => s.userType);

  const {
    providers,
    loading,
    error,
    toast,
    toastKind,
    lastError,
    lastTestResult,
    lastTestProviderId,
    defaultEndpoint,
    showDisabled,
    disabledCount,
    setShowDisabled,
    loadProviders,
    createProvider,
    updateProvider,
    deleteProvider,
    testProvider,
    reloadProviders,
    clearToast,
    clearLastError,
    clearTestResult,
  } = useModelAdminStore();

  const [editing, setEditing] = useState<{ id: string | null; form: FormState } | null>(null);
  // §20260816-03 — enabled 决定确认弹窗提供「停用」还是「彻底删除」。
  const [pendingDelete, setPendingDelete] = useState<
    { id: string; name: string; enabled: boolean } | null
  >(null);
  const [submitting, setSubmitting] = useState(false);
  // §7.1 — 弹窗内联错误:API 失败 / 校验失败时显示在弹窗内(最高层级,用户正在操作的
  // 位置),不依赖背景 toast(会被 modal 遮挡或 3s 后消失)。
  const [formError, setFormError] = useState<string | null>(null);
  // §重构 R133 — API Key 全局显示策略:true=显示完整 hint,false=脱敏。
  // 默认 true(超级管理员应该看到完整 hint),右上角「👁 显示 API Key」按钮切换。
  const [showApiKey, setShowApiKey] = useState(true);
  // §20260812-02 U1 — 模型能力雷达图数据。
  const [radarData, setRadarData] = useState<Record<string, ModelRadarStats> | null>(null);
  const [showRadar, setShowRadar] = useState(false);
  // §135 — 超级管理员每日 grant 弹窗(批量对所有 enabled 模型发金币)。
  // 阶段:`form`(输入)→ `submitting`(提交中)→ `result`(展示 granted/skipped)。
  const [grantDialog, setGrantDialog] = useState<
    | { stage: 'form' }
    | { stage: 'submitting' }
    | { stage: 'result'; data: GrantDailyResponse; amount: number; remark: string }
    | null
  >(null);
  const [grantAmount, setGrantAmount] = useState<string>('500');
  const [grantRemark, setGrantRemark] = useState<string>('每日金币发放');
  // §重构 R133 — 编辑弹窗里 API Key 输入框是否展示「当前 hint」只读预览,
  // 帮管理员一目了然知道这条记录当前的 key hint 是什么样(不暴露明文)。
  // §重构 — 测试弹窗独立状态,点击测试按钮立即弹窗(loading),API 返回后
  // 切换到 done 并填充结果。lastTestResult 由 store 提供 fallback,
  // 但点击瞬间就要看到 modal,避免 15s LLM 调用期间的「无反馈」错觉。
  const [testDialog, setTestDialog] = useState<TestDialogState | null>(null);

  // 未登录回首页;非超级管理员给出明确提示(后端 requireSuper 同样限定 userType === 3)
  useEffect(() => {
    if (!isAuthenticated) {
      navigate('/');
      return;
    }
  }, [isAuthenticated, navigate]);

  const isAdmin = userType === 3; // 3 = super admin (UserTypeSuper)

  useEffect(() => {
    if (isAuthenticated && isAdmin) {
      void loadProviders();
    }
  }, [isAuthenticated, isAdmin, loadProviders]);

  // §20260812-02 U1 — 加载雷达图数据（一次性，不随 providers 刷新）。
  useEffect(() => {
    if (!isAuthenticated || !isAdmin) return;
    getRadarStats()
      .then(setRadarData)
      .catch(() => { /* best-effort, radar is optional */ });
  }, [isAuthenticated, isAdmin]);

  // 关闭 toast 提示
  useEffect(() => {
    if (!toast) return;
    const tm = setTimeout(clearToast, 3000);
    return () => clearTimeout(tm);
  }, [toast, clearToast]);

  // §7.1 — store 写入 lastError(CRUD 失败)时,若弹窗正在打开,同步到弹窗内联错误,
  // 让用户直接在弹窗内看到失败原因(不依赖背景 toast)。
  useEffect(() => {
    if (lastError && editing) setFormError(lastError);
  }, [lastError, editing]);

  const openCreate = useCallback(() => {
    setFormError(null);
    clearLastError();
    setEditing({ id: null, form: { ...EMPTY_FORM } });
  }, [clearLastError]);

  const openEdit = useCallback((p: LlmProvider) => {
    setFormError(null);
    clearLastError();
    setEditing({ id: p.id, form: toForm(p) });
  }, [clearLastError]);

  const closeForm = useCallback(() => {
    if (submitting) return;
    setFormError(null);
    setEditing(null);
  }, [submitting]);

  const updateForm = useCallback(<K extends keyof FormState>(k: K, v: FormState[K]) => {
    setEditing((cur) => (cur ? { ...cur, form: { ...cur.form, [k]: v } } : cur));
    // 用户修改字段时清空旧的错误提示,避免「已经改对了但红字还在」的困惑。
    setFormError((cur) => (cur ? null : cur));
  }, []);

  /**
   * 把「用户改过的字段」挑出来,构造 Partial<LlmProviderCreate>。
   *
   * 编辑时,原始值由 toForm(p) 捕获(进入弹窗时的快照)。提交时逐字段对比:
   *   - 字符串字段:trim 后与原值相同 → 不传(后端保持不变);
   *   - api_key:空串 = 不修改(后端保留旧 key),非空 = 整体覆盖;
   *   - enabled:boolean 直接对比。
   * 这样管理员只改「Agent 名称」一个字段时,body 里只有 agent_name,其余字段
   * 后端一律保持原值(见 model_admin_api.go UpdateProviderRequest 指针语义)。
   */
  function buildUpdateBody(f: FormState, original: LlmProvider): Partial<LlmProviderCreate> {
    const body: Partial<LlmProviderCreate> = {};
    if (f.agent_name.trim() !== original.agent_name) {
      body.agent_name = f.agent_name.trim();
    }
    if (f.model.trim() !== original.model) {
      body.model = f.model.trim();
    }
    if (f.provider_type !== original.provider_type) {
      body.provider_type = f.provider_type;
    }
    if (f.api_key) {
      // 非空 = 用户想覆盖 API Key(后端加密存储,空串 = 保留旧值)。
      body.api_key = f.api_key;
    }
    const ep = f.endpoint.trim();
    if (ep !== (original.endpoint ?? '')) {
      body.endpoint = ep;
    }
    if (f.enabled !== original.enabled) {
      body.enabled = f.enabled;
    }
    const rm = f.remark.trim();
    if (rm !== (original.remark ?? '')) {
      body.remark = rm;
    }
    // §R224 (2026-08-01) — 重新引入 thinking 配置的 update 字段。
    // 仅当值"改变"才写 body,以让后端靠 nil 判定"未改/保留"。
    if (f.thinking_enabled !== (original.thinking_enabled ?? false)) {
      body.thinking_enabled = f.thinking_enabled;
    }
    if (f.thinking_budget_tokens !== (original.thinking_budget_tokens ?? 0)) {
      body.thinking_budget_tokens = f.thinking_budget_tokens;
    }
    return body;
  }

  const submitForm = useCallback(async () => {
    if (!editing) return;
    const f = editing.form;
    setFormError(null);

    // 校验:agent_name / model 必填。错误直接显示在弹窗内,不关闭弹窗。
    if (!f.agent_name.trim()) {
      setFormError('Agent 名称不能为空');
      return;
    }
    if (!f.model.trim()) {
      setFormError('Model 不能为空');
      return;
    }

    // §20260814-01 — OpenAI 协议必须填写 endpoint(无全局默认可回退)。
    if (
      normalizeProviderProtocol(f.provider_type) === 'openai-completions' &&
      !f.endpoint.trim()
    ) {
      setFormError('openai-completions 协议必须填写 Endpoint 基础地址');
      return;
    }

    setSubmitting(true);
    try {
      if (editing.id == null) {
        // Create — api_key 必填。
        if (!f.api_key) {
          setFormError('创建时 API Key 必填');
          setSubmitting(false);
          return;
        }
        const body: LlmProviderCreate = {
          agent_name: f.agent_name.trim(),
          model: f.model.trim(),
          provider_type: f.provider_type,
          api_key: f.api_key,
          endpoint: f.endpoint.trim() || undefined,
          // §R224 (2026-08-01) — 把 thinking 配置一并提交。新建场景下
          // 始终把两个字段都写出来,避免后端 nil → false/0 的隐式转换。
          enabled: f.enabled,
          remark: f.remark.trim() || undefined,
          thinking_enabled: f.thinking_enabled,
          thinking_budget_tokens: f.thinking_budget_tokens,
        };
        const ok = await createProvider(body);
        if (ok) {
          setFormError(null);
          setEditing(null);
        }
      } else {
        // Update — 仅提交用户改过的字段。
        const original = providers.find((p) => p.id === editing.id);
        if (!original) {
          setFormError('该 provider 已不存在,请刷新列表');
          return;
        }
        const body = buildUpdateBody(f, original);
        const ok = await updateProvider(editing.id, body);
        if (ok) {
          setFormError(null);
          setEditing(null);
        }
        // updateProvider 失败时 store 已 reportGlobalError + 设置 toast;
        // 这里再补一个弹窗内联错误,确保在弹窗打开时用户也能看到。
      }
    } catch (e) {
      // 兜底:store 的 catch 走不到时(理论上不会),在这里显示错误。
      const msg = e instanceof Error ? e.message : '操作失败';
      setFormError(msg);
    } finally {
      setSubmitting(false);
    }
  }, [editing, providers, createProvider, updateProvider]);

  // §重构 — 测试按钮:立即弹出「测试中」模态,API 返回后切到「完成」状态。
  // 用户在 15s LLM 调用期间可清楚看到 spinner + 「正在调用...」反馈。
  const onTest = useCallback(
    async (p: LlmProvider) => {
      // 立即弹窗(loading 态)
      setTestDialog({ provider: p, status: 'testing' });
      try {
        await testProvider(p.id);
        // store 已经更新了 lastTestResult,我们从 store 拉出来填入 dialog
        const latest = useModelAdminStore.getState().lastTestResult;
        setTestDialog({ provider: p, status: 'done', result: latest ?? undefined });
      } catch (e) {
        const msg = e instanceof Error ? e.message : '测试失败';
        setTestDialog({ provider: p, status: 'done', errorMessage: msg });
      }
    },
    [testProvider],
  );

  const closeTestDialog = useCallback(() => {
    setTestDialog(null);
    clearTestResult();
  }, [clearTestResult]);

  // §135 — 超级管理员每日 grant 弹窗开关 + 提交。
  // grant 后端是单一 RPC + 后端逐 provider 调用 `walletSvc.Credit`,失败
  // 不会中断其它 provider;前端拿到 granted/skipped 两段分别展示。
  const openGrantDialog = useCallback(() => {
    setGrantDialog({ stage: 'form' });
  }, []);
  const closeGrantDialog = useCallback(() => {
    if (grantDialog?.stage === 'submitting') return; // 提交流程中锁定
    setGrantDialog(null);
  }, [grantDialog]);
  const submitGrant = useCallback(async () => {
    if (grantDialog?.stage !== 'form') return;
    const amount = Number.parseInt(grantAmount, 10);
    if (!Number.isFinite(amount) || amount <= 0 || amount > 1000000) {
      const msg = t('modelAdmin.detail.grantAmountInvalid');
      reportGlobalError({ message: msg, severity: 'error' });
      return;
    }
    if (!grantRemark.trim()) {
      reportGlobalError({ message: t('modelAdmin.detail.grantRemarkRequired'), severity: 'error' });
      return;
    }
    setGrantDialog({ stage: 'submitting' });
    try {
      const data = await grantDailyToAll({ amount, remark: grantRemark.trim() });
      setGrantDialog({ stage: 'result', data, amount, remark: grantRemark.trim() });
      // 成功后立即刷新本列表页(model wallet 余额变了,列表上展示给运营)
      void reloadProviders();
    } catch (e: any) {
      const msg = e?.message ?? 'grant failed';
      reportGlobalError({ message: msg, severity: 'error' });
      setGrantDialog({ stage: 'form' });
    }
  }, [grantDialog, grantAmount, grantRemark, t, reloadProviders]);

  // 注:store 中保留 lastTestResult / lastTestProviderId 是为兼容其它监听器
  // 可能的 fallback 读取;本页面使用独立的 testDialog 状态。
  // eslint-disable-next-line @typescript-eslint/no-unused-expressions
  lastTestResult;
  // eslint-disable-next-line @typescript-eslint/no-unused-expressions
  lastTestProviderId;

  // R133 — 编辑弹窗内嵌「当前 key hint」只读预览,需要在 render 时拿到对应 provider
  const currentEditing = useMemo(
    () => (editing?.id ? providers.find((p) => p.id === editing.id) ?? null : null),
    [editing, providers],
  );

  if (!isAuthenticated) return null;
  if (!isAdmin) {
    return (
      <div className="model-admin-page" data-testid="model-admin-no-permission">
        <h1>🤖 {t('modelAdmin.title')}</h1>
        <div className="model-admin-no-permission">
          <span className="model-admin-no-permission__icon">🔒</span>
          <div className="model-admin-no-permission__title">
            仅超级管理员可访问
          </div>
          <div className="model-admin-no-permission__hint">
            当前账号权限等级 <code>{userType ?? '-'}</code>,访问「LLM 模型管理」需要 <code>userType = 3</code>(超级管理员)。请联系超级管理员升级账号或登录超级管理员账号。
          </div>
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => navigate('/')}
            data-testid="back-home"
          >
            ← 返回首页
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="model-admin-page">
      <h1>🤖 {t('modelAdmin.title')}</h1>
      <p className="model-admin-page__subtitle">{t('modelAdmin.subtitle')}</p>

      {toast && (
        <div
          className={
            'model-admin-toast' +
            (toastKind === 'error' ? ' model-admin-toast--error' : ' model-admin-toast--ok')
          }
          data-testid="model-admin-toast"
        >
          {toast}
        </div>
      )}

      {error && <div className="error">{error}</div>}

      <div className="model-admin-toolbar">
        <button
          type="button"
          className="btn btn-primary model-admin-btn-new"
          onClick={openCreate}
          data-testid="btn-new"
        >
          <span className="model-admin-btn-icon">＋</span>
          <span>{t('modelAdmin.newProvider')}</span>
        </button>
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() => void reloadProviders()}
          disabled={loading}
          data-testid="btn-reload"
        >
          🔄 {t('modelAdmin.actionReload')}
        </button>
        {/* §135 — 超级管理员每日 grant 入口;仅 `userType === 3` 可见,
            后端 requireSuper 双重保护。*/}
        {isAdmin && (
          <button
            type="button"
            className="btn btn-secondary model-admin-btn-grant"
            onClick={openGrantDialog}
            data-testid="btn-grant-daily"
            title={t('modelAdmin.detail.grantDailyDesc')}
          >
            🎁 {t('modelAdmin.detail.grantDaily')}
          </button>
        )}
        {/* R133 — 切换 API Key 完整显示 / 脱敏两种视图 */}
        <button
          type="button"
          className={
            'btn btn-secondary model-admin-toggle-key' +
            (showApiKey ? ' model-admin-toggle-key--on' : '')
          }
          onClick={() => setShowApiKey((v) => !v)}
          data-testid="toggle-api-key"
          title={showApiKey ? '点击切换为脱敏视图' : '点击切换为完整显示'}
        >
          {showApiKey ? '👁 完整 API Key' : '🫣 脱敏视图'}
        </button>
        {/* §20260816-03 — 显示/隐藏已停用(软删除)的模型行。
            后端默认只返回 enabled=true;不给这个开关,被软删除的行就变成
            「看不见也删不掉」的幽灵记录。 */}
        <button
          type="button"
          className={'btn btn-secondary' + (showDisabled ? ' btn-primary' : '')}
          onClick={() => void setShowDisabled(!showDisabled)}
          data-testid="toggle-show-disabled"
          title={showDisabled ? '只看启用中的模型' : '连已停用的模型一起显示'}
        >
          {showDisabled ? '🚫 隐藏已停用' : `♻ 显示已停用${disabledCount > 0 ? ` (${disabledCount})` : ''}`}
        </button>
        {/* §20260812-02 U1 — 雷达图 toggle */}
        <button
          type="button"
          className={'btn btn-secondary' + (showRadar ? ' btn-primary' : '')}
          onClick={() => setShowRadar((v) => !v)}
          data-testid="toggle-radar"
        >
          📊 {showRadar ? '隐藏雷达图' : '能力雷达图'}
        </button>
        <div className="model-admin-toolbar__hint">
          💡 {t('modelAdmin.subtitle')}
        </div>
      </div>

      {/* §20260812-02 U1 — 模型能力雷达图（可折叠） */}
      {showRadar && radarData && Object.keys(radarData).length > 0 && (
        <div className="model-admin-radar-section">
          <ModelRadarChart data={radarData} width={420} />
        </div>
      )}

      {/* §20260813-02 U1/U2 — 胜率趋势 + 道具经济(自包含:toggle + fetch) */}
      <ModelAnalyticsSection show={isAdmin} />

      <div className="model-admin-table-wrapper">
        {loading && <div className="admin-users-loading-overlay">{t('common.loading')}</div>}
        <table className="admin-users-table admin-users-table--wide">
          <thead>
            <tr>
              <th>{t('modelAdmin.colAgentName')}</th>
              <th>{t('modelAdmin.colModel')}</th>
              <th>{t('modelAdmin.colBalance')}</th>
              <th>{t('modelAdmin.colProviderType')}</th>
              <th>{t('modelAdmin.colApiKeyHint')}</th>
              <th>{t('modelAdmin.colEndpoint')}</th>
              {/* §R224 (2026-08-01) — 重新引入 thinking 列表列。 */}
              <th>{t('modelAdmin.colThinking')}</th>
              <th>{t('modelAdmin.colEnabled')}</th>
              {/* R133 — 新增备注列,完整展示多行文本 */}
              <th>备注</th>
              <th>{t('modelAdmin.colUpdatedAt')}</th>
              <th>{t('modelAdmin.colAction')}</th>
            </tr>
          </thead>
          <tbody>
            {providers.map((p) => (
              <tr key={p.id}>
                <td className="admin-users-table__nick">
                  <button
                    type="button"
                    className="link-button"
                    onClick={() => navigate(`/admin/models/${encodeURIComponent(p.id)}`)}
                    data-testid={`row-detail-${p.id}`}
                  >
                    {p.agent_name}
                  </button>
                </td>
                <td><code className="cell-wrap">{p.model}</code></td>
                <td className="cell-wrap">
                  {p.balance === undefined ? '—' : formatBalance(p.balance, lang)}
                </td>
                <td>{p.provider_type}</td>
                {/* R133 — API Key 列:默认完整 hint,工具栏一键脱敏 */}
                <td>
                  <code className="cell-wrap cell-wrap--code">
                    {showApiKey ? (p.api_key_hint || '-') : maskApiKey(p.api_key_hint)}
                  </code>
                </td>
                {/* §133 — Endpoint 完整显示 + 「← 全局」标识(DB 行为空回退到全局) */}
                <td>
                  <div className="endpoint-cell">
                    <code
                      className="cell-wrap cell-wrap--code"
                      title={p.effective_endpoint || ''}
                    >
                      {p.effective_endpoint || defaultEndpoint || '-'}
                    </code>
                    {p.endpoint_inherited && (
                      <span
                        className="endpoint-inherited-badge"
                        title={`DB 行为空,使用全局默认 endpoint:${defaultEndpoint ?? '-'}`}
                      >
                        ← 全局
                      </span>
                    )}
                  </div>
                </td>
                <td>
                  {/* §R224 (2026-08-01) — 重新引入列表列。 */}
                  {p.thinking_enabled ? (
                    <span title={`budget=${p.thinking_budget_tokens ?? 0}`}>
                      ✓ {p.thinking_budget_tokens ?? 0}
                    </span>
                  ) : (
                    <span style={{ opacity: 0.5 }}>—</span>
                  )}
                </td>
                <td>
                  {p.enabled ? (
                    <span className="online-dot online-dot--on">✓</span>
                  ) : (
                    <span className="online-dot online-dot--off">✗</span>
                  )}
                </td>
                <td className="cell-wrap cell-remark" title={p.remark || ''}>
                  {p.remark || <span style={{ opacity: 0.5 }}>-</span>}
                </td>
                <td className="cell-wrap">{formatDate(p.updated_at)}</td>
                <td>
                  <div className="row-actions">
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      onClick={() => openEdit(p)}
                      data-testid={`row-edit-${p.id}`}
                    >
                      {t('modelAdmin.actionEdit')}
                    </button>
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      onClick={() => void onTest(p)}
                      data-testid={`row-test-${p.id}`}
                    >
                      {t('modelAdmin.actionTest')}
                    </button>
                    <button
                      type="button"
                      className="btn btn-danger btn-sm"
                      onClick={() =>
                        setPendingDelete({ id: p.id, name: p.agent_name, enabled: p.enabled })
                      }
                      data-testid={`row-delete-${p.id}`}
                    >
                      {t('modelAdmin.actionDelete')}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!loading && providers.length === 0 && (
          <div className="empty-state">
            <p>{t('modelAdmin.empty')}</p>
            <button
              type="button"
              className="btn btn-primary model-admin-btn-new model-admin-btn-new--empty"
              onClick={openCreate}
              data-testid="btn-new-empty"
            >
              <span className="model-admin-btn-icon">＋</span>
              <span>{t('modelAdmin.newProvider')}</span>
            </button>
          </div>
        )}
      </div>

      {/* 新增/编辑弹窗 — 用 AppModal 替换内联 div,获得 ESC + 滚动锁定 + × 按钮等细节
          §R135 — blockBackdropClose 阻止遮罩/ESC/× 关闭;误点外面触发 shake + 红条提示,
          引导用户走"取消"或"确认"按钮关闭,避免误丢已填字段。 */}
      {editing && (
        <AppModal
          title={editing.id ? t('modelAdmin.editProvider') : t('modelAdmin.newProvider')}
          icon="🤖"
          kind="info"
          maxWidth={760}
          dismissible={!submitting}
          blockBackdropClose={!submitting}
          loading={submitting}
          blockHint="请点击「取消」或「确认」按钮关闭,误点外面不会丢失已填字段"
          testId="model-admin-modal"
          footer={
            <>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={closeForm}
                disabled={submitting}
                data-testid="form-cancel"
              >
                {t('common.cancel')}
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => void submitForm()}
                disabled={submitting}
                data-testid="form-submit"
              >
                {submitting
                  ? t('common.loading')
                  : editing.id
                    ? t('common.confirm')
                    : t('modelAdmin.actionAdd')}
              </button>
            </>
          }
          onClose={closeForm}
        >
          {formError && (
            <div className="model-admin-form__error" data-testid="form-error" role="alert">
              <span className="model-admin-form__error-icon" aria-hidden>⚠️</span>
              {formError}
            </div>
          )}
          <div className="model-admin-form">
            {/* R133 — 双列布局:核心身份字段 (agent_name/model) 同行,
                长文本字段 (endpoint/remark) 跨整行 textarea,
                api_key + provider_type + enabled 单独一行。 */}
            <div className="model-admin-form__row">
              <FormField label={t('modelAdmin.fieldAgentName')} required>
                <textarea
                  className="form-input form-textarea"
                  value={editing.form.agent_name}
                  onChange={(e) => updateForm('agent_name', e.target.value)}
                  placeholder="例如:美团 LongCat-2.0"
                  rows={1}
                  data-testid="form-agent-name"
                  autoFocus
                />
              </FormField>
              <FormField label={t('modelAdmin.fieldModel')} required>
                <textarea
                  className="form-input form-textarea"
                  value={editing.form.model}
                  onChange={(e) => updateForm('model', e.target.value)}
                  placeholder="例如:MeiTuan-model"
                  rows={1}
                  data-testid="form-model"
                />
              </FormField>
            </div>

            <div className="model-admin-form__row">
              <FormField label={t('modelAdmin.fieldProviderType')}>
                <select
                  className="form-input"
                  value={normalizeProviderProtocol(editing.form.provider_type) as ProviderProtocol}
                  onChange={(e) => updateForm('provider_type', e.target.value)}
                  data-testid="form-provider-type"
                >
                  {PROVIDER_PROTOCOL_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {t(opt.i18nKey)}
                    </option>
                  ))}
                </select>
              </FormField>
              <FormField label={t('modelAdmin.fieldEnabled')}>
                <label style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  <input
                    type="checkbox"
                    checked={editing.form.enabled}
                    onChange={(e) => updateForm('enabled', e.target.checked)}
                    data-testid="form-enabled"
                  />
                  <span>{t('modelAdmin.fieldEnabled')}</span>
                </label>
              </FormField>
            </div>

            <FormField
              label={t('modelAdmin.fieldApiKey')}
              hint={
                editing.id
                  ? t('modelAdmin.apiKeyPlaceholder')
                  : '必填 — 创建后明文不再回显,只能写入新值覆盖'
              }
              required={editing.id == null}
            >
              {/* R133 — 编辑时显示当前 hint 只读,提示框提醒「填则覆盖,空则保留」 */}
              {editing.id != null && currentEditing && (
                <div className="model-admin-current-hint">
                  当前 API Key 提示:<code>{currentEditing.api_key_hint || '-'}</code>
                  <span className="model-admin-current-hint__note">(明文不存在于前端,只能整体覆盖)</span>
                </div>
              )}
              <textarea
                className="form-input form-textarea"
                value={editing.form.api_key}
                onChange={(e) => updateForm('api_key', e.target.value)}
                placeholder={editing.id ? '填入新 key 覆盖现有,留空保留旧值' : 'sk-...'}
                rows={2}
                data-testid="form-api-key"
                spellCheck={false}
              />
            </FormField>

            <FormField
              label={t('modelAdmin.fieldEndpoint')}
              hint={
                editing.id != null && currentEditing?.endpoint_inherited
                  ? `当前实际生效:${currentEditing.effective_endpoint || defaultEndpoint || '-'} ← 全局默认(本行 DB endpoint 为空)`
                  : editing.id != null
                    ? '支持覆盖全局默认;留空 = 使用全局默认;填值 = 单独覆盖这条 provider'
                    : '留空 = 使用全局默认;填值 = 单独覆盖这条 provider'
              }
            >
              <textarea
                className="form-input form-textarea"
                value={editing.form.endpoint}
                onChange={(e) => updateForm('endpoint', e.target.value)}
                placeholder={`https://api.example.com/v1(留空使用全局${defaultEndpoint ? ':' + defaultEndpoint : ''})`}
                rows={2}
                data-testid="form-endpoint"
                spellCheck={false}
              />
              {/* §133 — 管理员能立刻看到 DB 端覆盖值 + 实际生效值 */}
              {editing.id != null && currentEditing && (
                <div className="model-admin-current-hint" style={{ marginTop: 6 }}>
                  当前 DB 覆盖值:<code>{currentEditing.endpoint || '(空 → 回退全局)'}</code>
                  <span className="model-admin-current-hint__note">
                    实际生效:<code style={{ color: 'var(--ok)' }}>{currentEditing.effective_endpoint || defaultEndpoint || '-'}</code>
                  </span>
                </div>
              )}
              {/* §20260814-01 — OpenAI 协议:endpoint 为基础地址,自动追加 /chat/completions */}
              {normalizeProviderProtocol(editing.form.provider_type) ===
                'openai-completions' && (
                <div className="model-admin-current-hint" style={{ marginTop: 6 }}>
                  <span style={{ color: 'var(--info)' }}>
                    💡 {t('modelAdmin.protocolEndpointHint')}
                  </span>
                </div>
              )}
            </FormField>

            {/* §R224 (2026-08-01) — 重新引入 thinking 字段控件。
              8 家代理(美团/豆包/DeepSeek/GLM/Kimi/MiniMax/Qwen/小米)若不开启,
              会收到上游 400 "missing messages.content.thinking"。 */}
            <FormField
              label={t('modelAdmin.fieldThinkingRequired')}
              hint="打开后,LLM 请求的每条 message 头部会注入 `{type:thinking, budget:N}` 块;8 家代理(美团/豆包/DeepSeek/GLM/Kimi/MiniMax/Qwen/小米)必须开启,否则 400。"
            >
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6 }}>
                <input
                  type="checkbox"
                  checked={editing.form.thinking_enabled}
                  onChange={(e) => updateForm('thinking_enabled', e.target.checked)}
                  data-testid="form-thinking-enabled"
                />
                <span>{t('modelAdmin.fieldThinkingRequired')}</span>
              </label>
              <input
                type="number"
                className="form-input"
                value={editing.form.thinking_budget_tokens}
                onChange={(e) => updateForm('thinking_budget_tokens', Number(e.target.value) || 0)}
                min={0}
                step={512}
                disabled={!editing.form.thinking_enabled}
                data-testid="form-thinking-budget"
                aria-label={t('modelAdmin.fieldThinkingBudget')}
              />
            </FormField>

            <FormField
              label={t('modelAdmin.fieldRemark')}
              hint="支持多行文本,可写使用场景/限速/注意事项等"
            >
              <textarea
                className="form-input form-textarea"
                value={editing.form.remark}
                onChange={(e) => updateForm('remark', e.target.value)}
                placeholder="例如:主用于狼人杀发言&#10;QPS 上限 5,晚高峰避免使用"
                rows={3}
                data-testid="form-remark"
              />
            </FormField>

            <div className="model-admin-form__footnote">
              💡 提示:后端 PUT 接口接受 <code>Partial</code>,只提交你修改过的字段(API Key 不填空就保留旧值)。
            </div>
          </div>
        </AppModal>
      )}

      {/* §20260816-03 — 删除确认按当前 enabled 状态分流:
          · 启用中的行 → 停用(软删除),保留审计链,可通过「显示已停用」找回;
          · 已停用的行 → 提供「彻底删除」(物理删除,需超级管理员且无对局引用),
            这是清理测试脏数据等「本就不该存在」的行的唯一正规通道。 */}
      {pendingDelete && (
        <ConfirmModal
          message={
            pendingDelete.enabled
              ? `确定停用「${pendingDelete.name}」吗?该模型会从列表隐藏(可通过「显示已停用」找回),历史对局记录保留。`
              : `「${pendingDelete.name}」已处于停用状态。是否**彻底删除**该行?此操作不可恢复;若该模型有历史对局记录,服务端会拒绝并提示改用停用。`
          }
          confirmLabel={pendingDelete.enabled ? '停用' : '彻底删除'}
          danger
          onConfirm={async () => {
            // enabled=true → 软删除;已停用 → 硬删除。
            await deleteProvider(pendingDelete.id, !pendingDelete.enabled);
            setPendingDelete(null);
          }}
          onCancel={() => setPendingDelete(null)}
        />
      )}

      {/* 测试按钮弹窗 — loading + done 两态过渡 */}
      {testDialog && (
        <TestResultDialog state={testDialog} onClose={closeTestDialog} />
      )}

      {/* §135 — 超级管理员每日 grant 批量发放弹窗 */}
      {grantDialog && (
        <GrantDialog
          dialog={grantDialog}
          amount={grantAmount}
          remark={grantRemark}
          onAmountChange={setGrantAmount}
          onRemarkChange={setGrantRemark}
          onSubmit={submitGrant}
          onClose={closeGrantDialog}
        />
      )}

      <style>{`
        .model-admin-page {
          position: relative;
          padding: 28px 24px 40px;
          width: 100%;
          max-width: 1800px;
          margin: 0 auto;
          border-radius: 12px;
          color: var(--text);
          background:
            radial-gradient(1200px 400px at 18% 0%, rgba(10,132,255,0.10), transparent 60%),
            radial-gradient(800px 300px at 92% 8%, rgba(63,185,80,0.06), transparent 60%),
            var(--bg);
          box-sizing: border-box;
        }
        /* 大屏 / 侧栏折叠时让整个页面跟随视口拉宽 */
        @media (min-width: 1600px) {
          .model-admin-page {
            max-width: none;
            padding: 32px 36px 48px;
          }
        }
        @media (min-width: 2200px) {
          .model-admin-page {
            padding: 36px 48px 56px;
          }
        }
        .model-admin-page h1 {
          margin: 0 0 6px 0;
          font-size: 26px;
          font-weight: 700;
          background: linear-gradient(135deg, var(--text) 0%, var(--accent) 100%);
          -webkit-background-clip: text;
          background-clip: text;
          color: transparent;
        }
        .model-admin-page__subtitle {
          color: var(--muted);
          margin: 0 0 20px 0;
          font-size: 13px;
        }
        .model-admin-toolbar {
          display: flex;
          gap: 10px;
          margin-bottom: 14px;
          align-items: center;
          flex-wrap: wrap;
          padding: 10px 14px;
          border: 1px solid var(--border);
          border-radius: 10px;
          background: linear-gradient(180deg, rgba(10,132,255,0.06), rgba(63,185,80,0.03));
        }
        .model-admin-toolbar__hint {
          margin-left: auto;
          color: var(--muted);
          font-size: 12px;
        }
        .model-admin-btn-new {
          display: inline-flex;
          align-items: center;
          gap: 6px;
          padding: 8px 18px !important;
          font-size: 14px !important;
          font-weight: 600;
          box-shadow: 0 2px 8px rgba(10,132,255,0.35);
        }
        .model-admin-btn-new:hover:not(:disabled) {
          box-shadow: 0 4px 14px rgba(10,132,255,0.55);
        }
        .model-admin-btn-icon {
          font-size: 16px;
          line-height: 1;
        }
        .model-admin-btn-new--empty {
          margin-top: 14px;
          padding: 10px 22px !important;
          font-size: 15px !important;
        }
        .model-admin-btn-new--empty .model-admin-btn-icon {
          font-size: 18px;
        }
        .model-admin-table-wrapper {
          position: relative;
          overflow-x: auto;
          border: 1px solid var(--border);
          border-radius: 10px;
          background: linear-gradient(180deg, var(--panel) 0%, rgba(22,27,34,0.85) 100%);
          box-shadow: 0 8px 24px rgba(0,0,0,0.35);
        }
        .model-admin-form {
          display: flex;
          flex-direction: column;
          gap: 4px;
        }
        .model-admin-form__error {
          display: flex;
          align-items: flex-start;
          gap: 8px;
          padding: 10px 12px;
          margin-bottom: 12px;
          border: 1px solid var(--danger, #e5484d);
          border-radius: 6px;
          background: color-mix(in srgb, var(--danger, #e5484d) 14%, var(--bg, #161618));
          color: var(--text, #eee);
          font-size: 13px;
          line-height: 1.45;
        }
        .model-admin-form__error-icon {
          font-size: 14px;
          line-height: 1.3;
        }
        .row-actions {
          display: flex;
          gap: 6px;
          flex-wrap: wrap;
        }
        .row-actions .btn { font-size: 12px; }
        .link-button {
          background: none;
          border: none;
          color: var(--accent);
          cursor: pointer;
          padding: 0;
          font: inherit;
          font-weight: 600;
        }
        .link-button:hover { text-decoration: underline; }
        .model-admin-toast {
          position: fixed;
          top: 16px;
          right: 16px;
          padding: 10px 16px;
          border-radius: 6px;
          z-index: 100;
          box-shadow: 0 4px 12px rgba(0,0,0,0.2);
          font-size: 13px;
          max-width: 360px;
        }
        .model-admin-toast--ok { background: var(--ok); color: #fff; }
        .model-admin-toast--error { background: var(--danger); color: #fff; }
        .form-input {
          width: 100%;
          background: var(--bg);
          color: var(--text);
          border: 1px solid var(--border);
          border-radius: 6px;
          padding: 7px 10px;
          font-size: 13px;
          font-family: inherit;
          box-sizing: border-box;
        }
        .form-input:focus {
          outline: none;
          border-color: var(--accent);
          box-shadow: 0 0 0 2px rgba(10,132,255,0.18);
        }
        .form-input::placeholder { color: var(--muted); opacity: 0.6; }
        .form-field {
          display: flex;
          flex-direction: column;
          gap: 4px;
          margin-bottom: 12px;
        }
        .form-field__label {
          font-size: 12px;
          color: var(--muted);
          font-weight: 600;
        }
        .form-field__label--required::after {
          content: ' *';
          color: var(--danger);
        }
        .form-field__hint {
          font-size: 11px;
          color: var(--muted);
          font-style: italic;
        }
        .btn-primary {
          background: var(--accent);
          color: #fff;
          padding: 6px 14px;
          border: none;
          border-radius: 6px;
          cursor: pointer;
          font: inherit;
        }
        .btn-primary:hover:not(:disabled) { filter: brightness(1.1); }
        .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
        .btn-secondary {
          background: rgba(127,127,127,0.18);
          color: var(--text);
          padding: 6px 14px;
          border: 1px solid var(--border);
          border-radius: 6px;
          cursor: pointer;
          font: inherit;
        }
        .btn-secondary:hover:not(:disabled) { background: rgba(127,127,127,0.28); }
        .btn-secondary:disabled { opacity: 0.5; cursor: not-allowed; }
        .btn-danger {
          background: var(--danger);
          color: #fff;
          padding: 6px 14px;
          border: none;
          border-radius: 6px;
          cursor: pointer;
          font: inherit;
        }
        .btn-danger:hover:not(:disabled) { filter: brightness(1.1); }
        .btn-sm { padding: 4px 10px !important; font-size: 12px !important; }

        .test-dialog__section {
          margin-top: 12px;
        }
        .test-dialog__section:first-child { margin-top: 0; }
        .test-dialog__section-title {
          font-size: 12px;
          opacity: 0.7;
          margin-bottom: 4px;
          font-weight: 600;
        }
        .test-dialog__code-block {
          background: rgba(127,127,127,0.10);
          padding: 8px 10px;
          border-radius: 6px;
          font-family: ui-monospace, 'SF Mono', Consolas, monospace;
          font-size: 12.5px;
          white-space: pre-wrap;
          word-break: break-word;
          max-height: 180px;
          overflow-y: auto;
        }
        .test-dialog__reply {
          background: rgba(63,185,80,0.10);
          padding: 10px 12px;
          border-radius: 6px;
          font-size: 14px;
          white-space: pre-wrap;
          word-break: break-word;
          max-height: 320px;
          overflow-y: auto;
          line-height: 1.6;
        }
        .test-dialog__reply--error {
          background: rgba(255,69,58,0.10);
          font-family: ui-monospace, 'SF Mono', Consolas, monospace;
          font-size: 13px;
        }
        .test-dialog__meta {
          margin-top: 12px;
          font-size: 12px;
          opacity: 0.7;
          font-family: ui-monospace, 'SF Mono', Consolas, monospace;
        }
        .test-dialog__hint {
          margin-top: 10px;
          font-size: 12px;
          color: var(--text-muted, #888);
        }
        .test-dialog__loading {
          text-align: center;
          padding: 30px 20px;
          color: var(--muted);
        }
        .test-dialog__loading-spinner {
          display: inline-block;
          font-size: 42px;
          animation: modelAdminSpin 1.2s linear infinite;
          margin-bottom: 12px;
        }
        .test-dialog__loading-text {
          font-size: 14px;
          line-height: 1.6;
        }
        .test-dialog__loading-detail {
          font-size: 12px;
          color: var(--muted);
          margin-top: 4px;
        }
        @keyframes modelAdminSpin {
          to { transform: rotate(360deg); }
        }

        /* R133 — 表格加宽 + 完整展示长字段(API Key / endpoint / 备注) */
        .admin-users-table--wide {
          min-width: 1280px;
          table-layout: auto;
        }
        .admin-users-table--wide th,
        .admin-users-table--wide td {
          vertical-align: top;
          padding: 10px 12px;
        }
        .cell-wrap {
          white-space: pre-wrap;
          word-break: break-all;
          overflow-wrap: anywhere;
          max-width: 320px;
        }
        .cell-wrap--code {
          font-family: ui-monospace, 'SF Mono', Consolas, monospace;
          font-size: 12.5px;
          background: rgba(127,127,127,0.08);
          padding: 2px 6px;
          border-radius: 4px;
          display: inline-block;
        }
        .cell-remark {
          font-size: 12.5px;
          color: var(--text);
          background: rgba(127,127,127,0.04);
          border-radius: 4px;
          padding: 6px 8px;
          max-width: 220px;
        }
        /* §133 — Endpoint 完整显示 + 「← 全局」标识 */
        .endpoint-cell {
          display: flex;
          flex-direction: column;
          gap: 4px;
          align-items: flex-start;
        }
        .endpoint-inherited-badge {
          display: inline-block;
          font-size: 10.5px;
          padding: 1px 6px;
          background: rgba(255,159,10,0.18);
          color: rgb(255,159,10);
          border: 1px solid rgba(255,159,10,0.35);
          border-radius: 10px;
          font-weight: 600;
          white-space: nowrap;
          cursor: help;
        }
        .model-admin-toggle-key {
          font-weight: 600;
        }
        .model-admin-toggle-key--on {
          background: rgba(63,185,80,0.20) !important;
          border-color: rgba(63,185,80,0.45) !important;
        }

        /* §135 — 每日 grant 弹窗 + 入口按钮 */
        .model-admin-btn-grant {
          background: linear-gradient(135deg, rgba(255,196,0,0.18), rgba(255,159,10,0.10));
          border-color: rgba(255,196,0,0.45);
          font-weight: 600;
        }
        .model-admin-btn-grant:hover:not(:disabled) {
          background: linear-gradient(135deg, rgba(255,196,0,0.28), rgba(255,159,10,0.16));
        }
        .grant-dialog-form {
          display: grid;
          gap: 12px;
        }
        .grant-dialog-desc {
          color: var(--muted);
          font-size: 12px;
          margin: 4px 0 0;
        }
        .grant-dialog-status {
          display: flex;
          align-items: center;
          justify-content: center;
          padding: 20px;
          color: var(--muted);
        }
        .grant-dialog-result p {
          margin: 0 0 8px;
          color: var(--text);
        }

        /* R133 — 编辑弹窗:双列布局 + textarea + 提示框 */
        .model-admin-form__row {
          display: grid;
          grid-template-columns: 1fr 1fr;
          gap: 12px;
        }
        .model-admin-form__row > .form-field { margin-bottom: 12px; }
        .form-textarea {
          font-family: ui-monospace, 'SF Mono', Consolas, monospace;
          resize: vertical;
          min-height: 32px;
          line-height: 1.5;
        }
        .model-admin-current-hint {
          background: rgba(10,132,255,0.10);
          border: 1px solid rgba(10,132,255,0.30);
          padding: 6px 10px;
          border-radius: 6px;
          font-size: 12.5px;
          margin-bottom: 6px;
        }
        .model-admin-current-hint code {
          background: rgba(0,0,0,0.18);
          padding: 1px 6px;
          border-radius: 4px;
          font-family: ui-monospace, 'SF Mono', Consolas, monospace;
          margin-left: 4px;
        }
        .model-admin-current-hint__note {
          color: var(--muted);
          font-size: 11.5px;
          margin-left: 8px;
          font-style: italic;
        }
        .model-admin-form__footnote {
          margin-top: 4px;
          padding: 8px 12px;
          background: rgba(10,132,255,0.06);
          border-radius: 6px;
          font-size: 12px;
          color: var(--muted);
        }
        .model-admin-form__footnote code {
          background: rgba(0,0,0,0.18);
          padding: 1px 5px;
          border-radius: 4px;
          font-family: ui-monospace, 'SF Mono', Consolas, monospace;
        }

        /* R133 — 权限不足提示 */
        .model-admin-no-permission {
          text-align: center;
          padding: 60px 24px;
          background: rgba(255,69,58,0.06);
          border: 1px solid rgba(255,69,58,0.18);
          border-radius: 12px;
          max-width: 560px;
          margin: 30px auto;
        }
        .model-admin-no-permission__icon {
          font-size: 56px;
          display: block;
          margin-bottom: 12px;
        }
        .model-admin-no-permission__title {
          font-size: 20px;
          font-weight: 700;
          color: var(--danger);
          margin-bottom: 8px;
        }
        .model-admin-no-permission__hint {
          font-size: 13px;
          color: var(--muted);
          line-height: 1.6;
          margin-bottom: 16px;
        }
        .model-admin-no-permission__hint code {
          background: rgba(0,0,0,0.18);
          padding: 1px 6px;
          border-radius: 4px;
          font-family: ui-monospace, 'SF Mono', Consolas, monospace;
        }
      `}</style>
    </div>
  );
}

function FormField({
  label,
  hint,
  required,
  children,
}: {
  label: string;
  hint?: string;
  required?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="form-field">
      <span className={'form-field__label' + (required ? ' form-field__label--required' : '')}>
        {label}
      </span>
      {children}
      {hint && <span className="form-field__hint">{hint}</span>}
    </div>
  );
}

// §135 — 超级管理员每日 grant 批量发放弹窗。两段 UI:
//   1. form: 输入 amount / remark,主按钮「发放」submit
//   2. result: 展示后端返回 granted/skipped 两个明细列表
//
// 弹窗加载中(后端 RPC in-flight)锁定关闭,避免重复提交。
function GrantDialog({
  dialog,
  amount,
  remark,
  onAmountChange,
  onRemarkChange,
  onSubmit,
  onClose,
}: {
  dialog: NonNullable<
    | { stage: 'form' }
    | { stage: 'submitting' }
    | { stage: 'result'; data: import('@/api/modelAdmin').GrantDailyResponse; amount: number; remark: string }
  >;
  amount: string;
  remark: string;
  onAmountChange: (v: string) => void;
  onRemarkChange: (v: string) => void;
  onSubmit: () => void;
  onClose: () => void;
}) {
  const t = useT();
  const submitting = dialog.stage === 'submitting';
  const closeLocked = submitting;

  return (
    <AppModal
      title={t('modelAdmin.detail.grantDaily')}
      icon="🎁"
      kind="info"
      maxWidth={620}
      blockBackdropClose={closeLocked}
      loading={submitting}
      onClose={onClose}
      testId="grant-daily-dialog"
      footer={
        <>
          {dialog.stage === 'form' && (
            <>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={onClose}
                data-testid="grant-dialog-cancel"
              >
                {t('common.cancel') ?? '取消'}
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={onSubmit}
                data-testid="grant-dialog-confirm"
              >
                {t('modelAdmin.detail.grantAction')}
              </button>
            </>
          )}
          {dialog.stage === 'submitting' && (
            <button type="button" className="btn btn-secondary" disabled>
              ⏳ ...
            </button>
          )}
          {dialog.stage === 'result' && (
            <button
              type="button"
              className="btn btn-primary"
              onClick={onClose}
              data-testid="grant-dialog-close"
            >
              {t('common.cancel')}
            </button>
          )}
        </>
      }
    >
      {dialog.stage === 'form' && (
        <div className="grant-dialog-form">
          <FormField
            label={t('modelAdmin.detail.grantAmount')}
            required
            hint={t('modelAdmin.detail.grantAmountHint')}
          >
            <input
              type="number"
              min={1}
              max={1000000}
              value={amount}
              onChange={(e) => onAmountChange(e.target.value)}
              className="form-input"
              data-testid="grant-dialog-amount"
            />
          </FormField>
          <FormField
            label={t('modelAdmin.detail.grantRemark')}
            required
            hint={t('modelAdmin.detail.grantRemarkHint')}
          >
            <input
              type="text"
              maxLength={255}
              value={remark}
              onChange={(e) => onRemarkChange(e.target.value)}
              className="form-input"
              data-testid="grant-dialog-remark"
            />
          </FormField>
          <p className="grant-dialog-desc">⚠️ {t('modelAdmin.detail.grantDailyDesc')}</p>
        </div>
      )}

      {dialog.stage === 'submitting' && (
        <div className="grant-dialog-status">
          <p>⏳ {t('common.loading')}</p>
        </div>
      )}

      {dialog.stage === 'result' && (
        <div className="grant-dialog-result">
          <p>
            ✅ {t('modelAdmin.detail.grantResult')} · {t('modelAdmin.detail.colStartedAt')}
            : {dialog.data.date}
          </p>
          <div className="model-admin-table-wrapper">
            <table className="admin-users-table">
              <thead>
                <tr>
                  <th>{t('modelAdmin.colAgentName')}</th>
                  <th style={{ textAlign: 'right' }}>{t('modelAdmin.detail.grantAmount')}</th>
                  <th style={{ textAlign: 'right' }}>{t('modelAdmin.detail.balance')}</th>
                </tr>
              </thead>
              <tbody>
                {dialog.data.granted.map((g) => (
                  <tr key={g.provider_id}>
                    <td>{g.provider_name}</td>
                    <td style={{ textAlign: 'right', color: 'var(--ok)' }}>+{g.amount}</td>
                    <td style={{ textAlign: 'right' }}>{g.balance_after}</td>
                  </tr>
                ))}
                {dialog.data.skipped.map((s) => (
                  <tr key={s.provider_id}>
                    <td>{s.provider_name}</td>
                    <td style={{ textAlign: 'right', color: 'var(--muted)' }}>
                      {t('modelAdmin.detail.grantSkippedHint')}
                    </td>
                    <td style={{ textAlign: 'right', color: 'var(--muted)' }}>—</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p>
            🎁 {t('modelAdmin.detail.grantResult')}: {dialog.data.granted.length} · 🔁{' '}
            {t('modelAdmin.detail.grantSkipped')}: {dialog.data.skipped.length}
          </p>
        </div>
      )}
    </AppModal>
  );
}

export default ModelAdminPage;