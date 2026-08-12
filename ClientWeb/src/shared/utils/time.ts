// 时间格式化工具函数。

/**
 * 把"距今多少毫秒"格式化为人类可读的相对时间字符串(中文习惯)。
 * 输出粒度: < 60s → "Ns"; < 60min → "Nmin"; 否则 "Nh"。
 *
 * 注意:单位后缀(前/ago)由调用方按 i18n 追加,本函数只返回"数字 + 单位"的中性
 * 片段,便于三语灵活拼接。
 *
 * @param ms 过去的某个 unix 毫秒时间戳(0/undefined/NaN → 返回 "")
 * @param nowMs 当前 unix 毫秒时间戳(默认 Date.now())
 */
export function formatRelativeTime(ms: number, nowMs: number = Date.now()): string {
  if (!ms || ms <= 0) return '';
  const diff = Math.max(0, nowMs - ms);
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}min`;
  const h = Math.floor(m / 60);
  return `${h}h`;
}

/**
 * 把相对时间片段 + i18n 后缀拼成完整文案("12s 前" / "12s ago" / "12秒前")。
 * 直接在组件内用 t() 拼更灵活,此函数仅作兜底便利。
 */
export function formatRelativeTimeWithSuffix(
  ms: number,
  suffix: string,
  nowMs: number = Date.now(),
): string {
  const core = formatRelativeTime(ms, nowMs);
  if (!core) return '';
  return `${core} ${suffix}`.trim();
}
