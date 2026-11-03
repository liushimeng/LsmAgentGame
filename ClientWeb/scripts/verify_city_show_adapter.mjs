/**
 * 批次 81 —— cityShowAdapter 零依赖单测运行器。
 *
 * 项目没有 vitest / jest（package.json 无 test 脚本），故用 esbuild（vite 自带）
 * 把 `src/components/virtualCity/__tests__/cityShowAdapter.test.ts` 打包成
 * 临时 ESM 后 import 执行；用例内部自带断言，失败抛错 ⇒ 进程非零退出。
 *
 * 用法：npm run verify:city-show-adapter
 */

import { build } from 'esbuild';
import { pathToFileURL } from 'node:url';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const entry = path.join(root, 'src/components/virtualCity/__tests__/cityShowAdapter.test.ts');
const outfile = path.join(root, 'node_modules/.cache/cityShowAdapter.test.mjs');

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
