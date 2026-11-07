/**
 * 批次 89 —— 虚拟城市「真实数据口径卡与异常告警层」前端零依赖单测运行器。
 *
 * 与 verify_city_show_adapter.mjs 同机制：项目没有 vitest / jest（package.json
 * 无 test 脚本），故用 esbuild（vite 自带）把
 * `src/components/virtualCity/__tests__/virtualCityRealtimeMetric.test.tsx`
 * 打包成临时 ESM 后 import 执行；用例内部自带断言，失败抛错 ⇒ 进程非零退出。
 *
 * 用法：npm run verify:virtual-city-realtime
 */

import { build } from 'esbuild';
import { pathToFileURL } from 'node:url';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const entry = path.join(
  root,
  'src/components/virtualCity/__tests__/virtualCityRealtimeMetric.test.tsx',
);
const outfile = path.join(root, 'node_modules/.cache/virtualCityRealtimeMetric.test.mjs');

await build({
  entryPoints: [entry],
  outfile,
  bundle: true,
  platform: 'node',
  format: 'esm',
  target: 'node22',
  logLevel: 'warning',
  // @/* 路径别名由 root/tsconfig.json 的 compilerOptions.paths 解析。
  loader: { '.tsx': 'tsx' },
});

await import(pathToFileURL(outfile).href);

// ── 阈值边界口径静态扫描（主 Agent 收口裁定）───────────────────────────
//
// 后端 `citychrome/anomaly.go` 的判定式是**严格超过 `>`**（恰好等于阈值不触发）；
// 设计文档 §4.2 的阈值边界写法与 §9.1 验收「恰好等于阈值不触发」自相矛盾，裁定取后者。
// ⇒ 前端**类型注释 / i18n 文案**凡写「≥」「达到阈值」「大于等于」的必须改掉，
//   否则与后端口径打架。
//
// ⚠ 扫描只针对**阈值语义**：「≥」在 §26 对比度语境里是合法用法
//   （如「正文 ≥4.5:1」「徽章文字 ≥5:1」= 至少达到该对比度），不得误伤。
//   故此处按「≥ 紧跟阈值/倍数语义」匹配，而非裸字符。
//
// 本扫描在运行器（原生 node ESM，可读文件）里做 —— 打包产物无法 import 源码。
const SCAN_FILES = [
  'src/types/virtualCityRealtime.ts',
  'src/components/virtualCity/RealtimeBoard/boardUtils.ts',
  'src/components/virtualCity/RealtimeBoard/MetricSpecPanel.tsx',
  'src/components/virtualCity/RealtimeBoard/QuoteTable.tsx',
  'src/components/virtualCity/RealtimeBoard/RealtimeBoard.tsx',
  'src/components/virtualCity/RealtimeBoard/VirtualCityDashboard.tsx',
  'src/i18n/locales/virtualCityRealtime-zh.ts',
  'src/i18n/locales/virtualCityRealtime-en.ts',
  'src/i18n/locales/virtualCityRealtime-ja.ts',
];

/** 阈值语义下的「≥」写法（对比度语境的 ≥4.5:1 / ≥5:1 不在其中）。 */
const BANNED_THRESHOLD_PATTERNS = [
  /≥\s*(?:\d+\s*×)?\s*(?:类目)?阈值/,
  /≥\s*(?:\d+\s*×)?\s*类目阈值/,
  /阈值\s*≥/,
  /达到阈值/,
  /大于等于/,
];

for (const rel of SCAN_FILES) {
  const src = fs.readFileSync(path.join(root, rel), 'utf-8');
  for (const re of BANNED_THRESHOLD_PATTERNS) {
    const m = src.match(re);
    if (m) {
      throw new Error(
        `[virtualCityRealtimeMetric] ${rel} 仍含阈值语义「${m[0]}」——` +
          '阈值边界口径须统一为「严格超过 >」（主 Agent 收口裁定）',
      );
    }
  }
}

// 类型注释须显式写明边界口径（防后人改回「≥」）。
const typeSrc = fs.readFileSync(path.join(root, 'src/types/virtualCityRealtime.ts'), 'utf-8');
if (!typeSrc.includes('严格超过')) {
  throw new Error(
    '[virtualCityRealtimeMetric] types/virtualCityRealtime.ts 须写明「严格超过」边界口径',
  );
}

// eslint-disable-next-line no-console
console.log('[virtualCityRealtimeMetric] 阈值边界口径扫描通过（阈值语义无「≥」/「达到阈值」）');
