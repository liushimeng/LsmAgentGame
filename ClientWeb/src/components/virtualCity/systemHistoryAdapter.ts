/**
 * systemHistoryAdapter —— 后端会话历史（`llmtypes.Message` wire 形状）→
 * 前端 `VirtualCitySystemMessage` 归一化（批次 81 §7.3-6 历史恢复）。
 *
 * 后端实际字段形状（事实来源）：
 *   - 存储：`ServerGo/agent/citysystem/context.go`（[]llmtypes.Message，不含 system）；
 *   - WS：`game.virtual_city_system_history` {room_id, messages[]}（ws 层 sendOK 直出）；
 *   - HTTP：`GET .../system_agent/history` → {code,message,data:{messages:[...]}}。
 *   两端 messages[] 同构，均过 `llm/types/types.go::ContentBlock.MarshalJSON`
 *   （按 Type 收敛）：
 *     text        {type, text}                      —— 空文本被服务端写成 "(empty)"
 *     tool_use    {type, id, name, input}           —— id/name/input 恒有键
 *     tool_result {type, tool_use_id, content, is_error}
 *                                                 —— content 为 text 子块数组
 *     thinking    {type, budget}                    —— 无正文，跳过
 *
 * 一轮对话在存储里拆成多条 raw 消息：
 *   user(text) → [assistant(tool_use±text) → user(tool_result)]* → assistant(text)
 * 归一化到「一条 user 气泡 + 一条 assistant 气泡」：
 *   - user + text 块 → user 气泡（多 text 块以 \n 连接）；
 *   - user + 仅 tool_result（无 text）→ 折叠进**上一条 assistant**：
 *     按 tool_use_id 回填对应 tool_calls[].text / is_err；对不上补一条已完成记录；
 *     无上一条 assistant 时合成一条 assistant 承接（不丢结果）；
 *   - assistant → 与上一条 assistant 合并（同轮）：text 按序以 \n 累加、
 *     tool_use 转 tool_calls 记录（input 统一 stringify）；
 *   - thinking / 未知块 / 空消息 → 跳过。
 *
 * id = `h<rawIdx>-<role>`（raw 数组下标 + role，同一份历史两次归一化逐位稳定，
 * 不依赖时钟 ⇒ React key 不冲突且刷新前后一致）；后端无消息级时间戳，ts 取
 * 归一化时刻（毫秒）、tool 记录 ts 置 0（渲染层 rec.ts falsy 即不显示时间，
 * 不冒充错误时刻）。streaming 恒省略（历史是已落定消息）。
 */

import type {
  VirtualCitySystemMessage,
  VirtualCitySystemWireBlock,
  VirtualCitySystemWireMessage,
  VirtualCityToolCallRecord,
} from '@/types/virtualCitySystem';

function asString(v: unknown): string {
  return typeof v === 'string' ? v : '';
}

/** tool_use.input 归一 stringify（与 store 的 stringifyInput 同口径）。 */
function stringifyInput(raw: unknown): string {
  if (typeof raw === 'string') return raw;
  if (raw === undefined || raw === null) return '';
  try {
    return JSON.stringify(raw);
  } catch {
    return String(raw);
  }
}

/** tool_result.content 取正文（text 子块数组 / 兼容纯字符串）。 */
function collectResultText(content: unknown): string {
  if (typeof content === 'string') return content;
  if (!Array.isArray(content)) return '';
  const parts: string[] = [];
  for (const b of content) {
    const blk = b as VirtualCitySystemWireBlock | null;
    if (blk && typeof blk === 'object' && blk.type === 'text') {
      parts.push(asString(blk.text));
    }
  }
  return parts.join('\n');
}

/** 取 content 里的 text 块正文（顺序拼接）。 */
function collectTexts(content: unknown): string[] {
  if (typeof content === 'string') return content ? [content] : [];
  if (!Array.isArray(content)) return [];
  const parts: string[] = [];
  for (const b of content) {
    const blk = b as VirtualCitySystemWireBlock | null;
    if (blk && typeof blk === 'object' && blk.type === 'text') {
      const t = asString(blk.text);
      if (t) parts.push(t);
    }
  }
  return parts;
}

/**
 * 归一化后端会话历史。raw 为 WS history 帧 / HTTP data.messages 的数组本体；
 * 非数组输入一律返回 []（不抛错——历史是旁路恢复，绝不能打崩对话面板）。
 */
export function normalizeSystemHistory(raw: unknown): VirtualCitySystemMessage[] {
  if (!Array.isArray(raw)) return [];
  const out: VirtualCitySystemMessage[] = [];
  // tool_use_id → tool 记录（跨 raw 消息折叠；新 user 文本轮次时清空）。
  let pendingTools = new Map<string, VirtualCityToolCallRecord>();
  const now = Date.now();

  const lastAssistant = (): VirtualCitySystemMessage | null => {
    const last = out[out.length - 1];
    return last && last.role === 'assistant' ? last : null;
  };

  raw.forEach((item, idx) => {
    const m = item as (VirtualCitySystemWireMessage & { id?: unknown; text?: unknown }) | null;
    if (!m || typeof m !== 'object') return;

    // 防御：后端若已直接下发前端形状（id + text），原样透传（role 校验）。
    if (typeof m.text === 'string' && typeof m.id === 'string') {
      const role = m.role === 'user' ? 'user' : m.role === 'assistant' ? 'assistant' : null;
      if (!role) return;
      const extra = item as Record<string, unknown>;
      pendingTools = new Map();
      out.push({
        id: m.id,
        role,
        text: m.text,
        ts: typeof extra.ts === 'number' ? extra.ts : now,
        ...(Array.isArray(extra.tool_calls)
          ? { tool_calls: extra.tool_calls as VirtualCityToolCallRecord[] }
          : {}),
      });
      return;
    }

    const role = typeof m.role === 'string' ? m.role : '';
    const texts = collectTexts(m.content);
    const toolUses: { id: string; name: string; input: unknown }[] = [];
    const toolResults: { tool_use_id: string; text: string; is_err: boolean }[] = [];
    if (Array.isArray(m.content)) {
      for (const b of m.content) {
        const blk = b as VirtualCitySystemWireBlock | null;
        if (!blk || typeof blk !== 'object') continue;
        if (blk.type === 'tool_use') {
          toolUses.push({
            id: asString(blk.id),
            name: asString(blk.name),
            input: blk.input,
          });
        } else if (blk.type === 'tool_result') {
          toolResults.push({
            tool_use_id: asString(blk.tool_use_id),
            text: collectResultText(blk.content),
            is_err: blk.is_error === true,
          });
        }
        // thinking（{type,budget} 无正文）/ 未知块：跳过。
      }
    }

    // 折叠 tool_result 进上一条 assistant（先折叠，user 文本轮次尚未入列）。
    if (toolResults.length > 0) {
      const la = lastAssistant();
      for (const r of toolResults) {
        const rec = r.tool_use_id ? pendingTools.get(r.tool_use_id) : undefined;
        if (rec) {
          rec.text = r.text;
          rec.is_err = r.is_err;
          continue;
        }
        const orphan: VirtualCityToolCallRecord = {
          tool: '',
          input: '',
          text: r.text,
          is_err: r.is_err,
          ts: 0,
        };
        if (la) {
          la.tool_calls = [...(la.tool_calls ?? []), orphan];
        } else {
          // 无上一条 assistant（裁剪孤儿 / 异常序）⇒ 合成一条承接，不丢结果。
          out.push({
            id: `h${idx}-assistant`,
            role: 'assistant',
            text: '',
            ts: now,
            tool_calls: [orphan],
          });
        }
      }
    }

    if (role === 'assistant') {
      const recs: VirtualCityToolCallRecord[] = toolUses.map((tu) => {
        const rec: VirtualCityToolCallRecord = {
          tool: tu.name,
          input: stringifyInput(tu.input),
          text: '',
          is_err: false,
          ts: 0,
        };
        if (tu.id) pendingTools.set(tu.id, rec);
        return rec;
      });
      const text = texts.join('\n');
      if (!text && recs.length === 0 && toolResults.length === 0) return; // 空消息（如纯 thinking）
      const la = lastAssistant();
      if (la) {
        // 同轮合并（user 文本必开新气泡，故上一条 assistant 同属本轮）。
        if (text) la.text = la.text ? `${la.text}\n${text}` : text;
        if (recs.length > 0) la.tool_calls = [...(la.tool_calls ?? []), ...recs];
        return;
      }
      out.push({
        id: `h${idx}-assistant`,
        role: 'assistant',
        text,
        ts: now,
        ...(recs.length > 0 ? { tool_calls: recs } : {}),
      });
      return;
    }

    // user（或异常 role）：有 text 块才算一条用户气泡；纯 tool_result 已折叠。
    if (texts.length === 0) return;
    pendingTools = new Map(); // 新一轮，旧轮 tool_use_id 不再跨轮匹配。
    out.push({
      id: `h${idx}-user`,
      role: 'user',
      text: texts.join('\n'),
      ts: now,
    });
  });

  return out;
}
