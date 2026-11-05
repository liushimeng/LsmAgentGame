/**
 * virtualCitySystem i18n 三语键集合一致性单测（批次 84）。
 *
 * 零依赖断言写法（同 cityShowAdapter.test.ts）：由
 * `scripts/verify_virtual_city_system_batch84.mjs` 用 esbuild 打包后调用
 * （npm run verify:virtual-city-system）；模块加载即自跑。
 *
 * §29.8 铁律：`t(映射表[id])` 必须恒返回字符串 ⇒ 三语键集合必须**完全相同**
 * （缺一个 key 即白屏）。本测试把「脚本 diff 键集合」固化成单测。
 */

import zh from '@/i18n/locales/virtualCitySystem-zh';
import en from '@/i18n/locales/virtualCitySystem-en';
import ja from '@/i18n/locales/virtualCitySystem-ja';

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[virtualCitySystemI18n] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function keysOf(dict: Record<string, unknown>): string[] {
  return Object.keys(dict).sort();
}

export function runVirtualCitySystemI18nTests(): void {
  const zhKeys = keysOf(zh as Record<string, unknown>);
  const enKeys = keysOf(en as Record<string, unknown>);
  const jaKeys = keysOf(ja as Record<string, unknown>);

  const missingInEn = zhKeys.filter((k) => !enKeys.includes(k));
  const extraInEn = enKeys.filter((k) => !zhKeys.includes(k));
  const missingInJa = zhKeys.filter((k) => !jaKeys.includes(k));
  const extraInJa = jaKeys.filter((k) => !zhKeys.includes(k));

  eq(missingInEn.length, 0, `en 缺少键：${missingInEn.join(',')}`);
  eq(extraInEn.length, 0, `en 多余键：${extraInEn.join(',')}`);
  eq(missingInJa.length, 0, `ja 缺少键：${missingInJa.join(',')}`);
  eq(extraInJa.length, 0, `ja 多余键：${extraInJa.join(',')}`);

  // 三语值均非空串（空串 = 界面上出现空白标签）。
  for (const [lang, dict] of [['zh', zh], ['en', en], ['ja', ja]] as const) {
    for (const [k, v] of Object.entries(dict as Record<string, unknown>)) {
      if (typeof v !== 'string') {
        throw new Error(`[virtualCitySystemI18n] ${lang}.${k} 不是字符串`);
      }
      if (v.length === 0) {
        throw new Error(`[virtualCitySystemI18n] ${lang}.${k} 为空串`);
      }
    }
  }

  // eslint-disable-next-line no-console
  console.log(`[virtualCitySystemI18n] 三语键集合一致（${zhKeys.length} 键）`);
}

// 直接执行（被打包器当作入口调用）时自动运行。
runVirtualCitySystemI18nTests();
