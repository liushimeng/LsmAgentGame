/**
 * 批次 81 —— systemHistoryAdapter 零依赖单测运行器（§7.3-6 历史归一化）。
 *
 * 与 verify_city_show_adapter.mjs 同机制：项目没有 vitest / jest，用 esbuild
 * （vite 自带）把 `src/components/virtualCity/__tests__/systemHistoryAdapter.test.ts`
 * 打包成临时 ESM 后 import 执行；用例内部自带断言，失败抛错 ⇒ 进程非零退出。
 *
 * 用法：npm run verify:system-history
 */

import { build } from 'esbuild';
import { pathToFileURL } from 'node:url';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const entry = path.join(root, 'src/components/virtualCity/__tests__/systemHistoryAdapter.test.ts');
const outfile = path.join(root, 'node_modules/.cache/systemHistoryAdapter.test.mjs');

await build({
  entryPoints: [entry],
  outfile,
  bundle: true,
  platform: 'node',
  format: 'esm',
  target: 'node22',
  logLevel: 'warning',
  // @/* 路径别名由 tsconfig.json 的 compilerOptions.paths 解析。
});

await import(pathToFileURL(outfile).href);
