/**
 * 余额格式化工具。
 * - ≥ 10000 时简写为 "1.2 万" / "12.5K" / "1.2万"
 * - 负数同样处理，前置 "-"。
 * - null/undefined → "—" (由调用方控制，这里直接返回空字符串)。
 */

export function formatBalance(raw: number | null | undefined, lang: string): string {
  if (raw == null) return '—';
  const sign = raw < 0 ? '-' : '';
  const v = Math.abs(raw);

  if (v >= 10000) {
    const w = v / 10000;
    // 小数点后最多 1 位；去除末尾 0
    const s = w % 1 === 0 ? String(w) : w.toFixed(1).replace(/\.0$/, '');
    if (lang === 'en') return `${sign}${s}K`;
    if (lang === 'ja') return `${sign}${s}万`;
    return `${sign}${s}万`; // zh-CN
  }

  // 整数显示，千分位分隔
  return sign + v.toLocaleString(lang === 'zh-CN' ? 'zh-CN' : lang === 'ja' ? 'ja-JP' : 'en-US');
}

/** 金额带 +/- 符号 + 颜色 class。.
 * 防御性实现：当 API 返回 malformed amount（null/undefined/非数字）时退化到 0，
 * 避免 toLocaleString 抛错导致钱包下拉渲染崩溃。 */
export function signedAmount(n: number | null | undefined): { text: string; cls: string } {
  const num = typeof n === 'number' ? n : Number(n);
  if (!Number.isFinite(num) || num === 0) return { text: '0', cls: 'amount--zero' };
  if (num > 0) return { text: `+${num.toLocaleString()}`, cls: 'amount--gain' };
  return { text: num.toLocaleString(), cls: 'amount--loss' };
}
