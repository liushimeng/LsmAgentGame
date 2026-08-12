/** 格式化 Token 数量：≥1000 用 K，≥1000000 用 M，保留 1 位小数。
 *  0 返回 "0"，undefined/null 返回 "—"。
 *  例：12345 → "12.3K"，1500000 → "1.5M"，999 → "999"。 */
export function formatK(n: number | undefined | null): string {
  if (n === undefined || n === null) return '—';
  if (n === 0) return '0';
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}
