/**
 * 「显示指令提示留痕行」渲染单测（批次 85 C2 · 关窗不丢提示）。
 *
 * 契约：关窗 / 最小化期间产生的显示指令提示转入消息流（role='notice'），
 * 复开后在历史里**永久可见** —— 用 react-dom/server 直渲真实
 * `SystemDialogMessage`，断言留痕行节点与文案（含 vehicle 专属文案分支）。
 *
 * 由 `scripts/verify_city_show_adapter.mjs` 经入口 import 打包执行。
 */

import { renderToStaticMarkup } from 'react-dom/server.browser';
import { SystemDialogMessage } from '../SystemDialogMessage';
import { useI18nStore } from '@/store/i18n.store';
import type { VirtualCitySystemMessage } from '@/types/virtualCitySystem';

function ok(cond: boolean, msg: string): void {
  if (!cond) throw new Error(`[cityShowNoticeRow] ${msg}`);
}

// zustand SSR 快照陷阱（见 cityLabelsDom.test.tsx 夹具注释）：夹具原地改字段。
useI18nStore.getState().lang = 'zh-CN';

function renderNotice(notice: VirtualCitySystemMessage['notice']): string {
  return renderToStaticMarkup(
    <SystemDialogMessage
      message={{
        id: 'n-test',
        role: 'notice',
        text: '',
        ts: 1700000000000,
        ...(notice ? { notice } : {}),
      }}
    />,
  );
}

// 1. 车辆未找到（C2 本体）：留痕行存在 + vehicle 专属文案 + 无关闭按钮（历史行）。
{
  const html = renderNotice({ code: 'entity_not_found', type: 'focus_entity', detail: 'vehicle:nope-b85' });
  ok(html.includes('virtualCity-system-notice-row'), '留痕行节点必须渲染');
  ok(html.includes('virtualCity-sysdialog__notice-text'), '文案容器必须复用横幅同款 class');
  ok(html.includes('未找到车辆：nope-b85'), '车辆未找到文案（去 vehicle: 前缀）');
  ok(!html.includes('notice-close'), '历史留痕行不得带关闭按钮（横幅才有）');
}

// 2. 非车辆实体未找到：走通用「未找到实体」文案分支。
{
  const html = renderNotice({ code: 'entity_not_found', type: 'focus_entity', detail: 'resident:42' });
  ok(html.includes('未找到实体：resident:42'), '通用实体未找到文案');
}

// 3. 未识别类别（C6 契约）同款文案落留痕行。
{
  const html = renderNotice({ code: 'unknown_category', type: 'set_filter', detail: 'hospital' });
  ok(html.includes('未识别的类别：hospital'), '未识别类别文案');
}

// 4. 缺 notice 负载时回落 text 字段（历史兼容，不白屏）。
{
  const html = renderNotice(undefined);
  ok(html.includes('virtualCity-system-notice-row'), '缺负载也必须渲染留痕行');
  ok(!html.includes('undefined'), '缺负载不得渲染 undefined 字样');
}

// eslint-disable-next-line no-console
console.log('[cityShowNoticeRow] all assertions passed');
