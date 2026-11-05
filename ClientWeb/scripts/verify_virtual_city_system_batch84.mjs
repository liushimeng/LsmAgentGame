/**
 * 批次 84 —— 虚拟城市「系统对话」前端零依赖单测运行器。
 *
 * 与 verify_city_show_adapter.mjs 同机制：项目没有 vitest / jest，用 esbuild
 * （vite 自带）把 `src/components/virtualCity/__tests__/*.test.ts` 打包成
 * 临时 ESM 后 import 执行；用例内部自带断言，失败抛错 ⇒ 进程非零退出。
 *
 * 覆盖三份用例：
 *   - virtualCitySystemStore.test.ts（agent_message 来信 + systemInfo）
 *   - systemDialogCatalog.test.ts（点条目只回填、不发送）
 *   - virtualCitySystemI18n.test.ts（三语键集合一致）
 *
 * 用法：npm run verify:virtual-city-system
 */

import { build } from 'esbuild';
import { pathToFileURL } from 'node:url';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const outDir = path.join(root, 'node_modules/.cache');

const entries = [
  'virtualCitySystemStore.test.ts',
  'systemDialogCatalog.test.ts',
  'virtualCitySystemI18n.test.ts',
];

for (const name of entries) {
  const entry = path.join(root, 'src/components/virtualCity/__tests__', name);
  const outfile = path.join(outDir, name.replace(/\.ts$/, '.mjs'));
  await build({
    entryPoints: [entry],
    outfile,
    bundle: true,
    platform: 'node',
    format: 'esm',
    target: 'node22',
    logLevel: 'warning',
    // @/* 路径别名由 root/tsconfig.json 的 compilerOptions.paths 解析。
  });
  await import(pathToFileURL(outfile).href);
}
