#!/usr/bin/env node
/**
 * 云层昼夜着色断言护栏（批次 58）。
 *
 * 为什么需要它：批次 58 首次实现 `cloudTintFor` 时，用
 * `|tod - 0.5| * 2` 当「距正午多远」，而该式在日落（tod=0.75）时只有 0.5，
 * 永远够不到金色阈值 0.88 ⇒ **`#e8955c` 是死代码**，金色时段从未出现过。
 * tsc / build / 实拍截图全部无感（截图恰好都落在别的时间段）。
 * 本脚本**从 CloudLayer.tsx 原样抽取该函数**（不复制逻辑，避免测的不是真代码），
 * 对 24 小时逐 6 分钟采样 + 15 条断言，非零退出码即失败。
 *
 * 用法：node scripts/ci/check_cloud_tint.mjs
 */

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = resolve(HERE, '../../ClientWeb/src/components/virtualCity/props/CloudLayer.tsx');

/** 最小 THREE.Color 替身：只需要 getHexString（被测函数只读颜色不改）。 */
const STUB = `const THREE = { Color: class { constructor(h){ this.h = h; }
  getHexString(){ return this.h.replace('#',''); } } };`;

const src = readFileSync(SRC, 'utf8');

const tintBlock = src.match(/const CLOUD_TINT = \{[\s\S]*?\n\} as const;/);
const fnBlock = src.match(/function cloudTintFor\([\s\S]*?\n\}/);
if (!tintBlock || !fnBlock) {
  console.error('FAIL 未能从 CloudLayer.tsx 抽取 CLOUD_TINT / cloudTintFor（源码结构变了？）');
  process.exit(1);
}

// 去掉 TS 类型标注，得到可直接在 Node 里跑的等价源码
let body = tintBlock[0].replace('} as const;', '};') + '\n' + fnBlock[0];
body = body.replace(/\):[^\n]*\{/, ') {');
body = body.replace(/(function cloudTintFor\()([^)]*)(\))/, (_m, a, params, c) =>
  a + params.replace(/:\s*\w+/g, '') + c);

const { cloudTintFor } = await import(
  'data:text/javascript;base64,' +
  Buffer.from(`${STUB}\n${body}\nexport { cloudTintFor };`).toString('base64')
);

const at = (tod, sr, ss) => {
  const [c, b] = cloudTintFor(tod, sr, ss);
  return { color: '#' + c.getHexString(), brightness: b };
};

const SR = 0.25, SS = 0.75;               // 基准：06:00 日出 / 18:00 日落
const h = (hour) => hour / 24;
const checks = [];
const eq = (name, got, want) => checks.push([name, JSON.stringify(got) === JSON.stringify(want), `got ${JSON.stringify(got)} want ${JSON.stringify(want)}`]);

eq('正午 12:00 近白',        at(h(12), SR, SS).color, '#f2f6fb');
eq('正午亮度 = 1',          at(h(12), SR, SS).brightness, 1);
eq('15:00 仍近白',          at(h(15), SR, SS).color, '#f2f6fb');
eq('17:00 转午后暖白',      at(h(17), SR, SS).color, '#eef2f7');
eq('17:24 进入金色时段',    at(h(17.4), SR, SS).color, '#e8955c');
eq('17:48 金色',            at(h(17.8), SR, SS).color, '#e8955c');
eq('日出 06:00 金色',       at(h(6), SR, SS).color, '#e8955c');
eq('日出后 06:36 金色',     at(h(6.6), SR, SS).color, '#e8955c');
eq('日出前 05:24 仍是夜',  at(h(5.4), SR, SS).color, '#2a3040');
eq('入夜 18:30 城市光污染', at(h(18.5), SR, SS).color, '#3d3a44');
eq('午夜 00:00 夜云暗蓝',   at(h(0), SR, SS).color, '#2a3040');
eq('02:00 夜云暗蓝',        at(h(2), SR, SS).color, '#2a3040');
checks.push(['夜间亮度均 < 0.25',
  [19, 21, 23, 1, 3].every((x) => at(h(x), SR, SS).brightness < 0.25), '']);
checks.push(['金色时段确实出现过（死代码回归哨兵）',
  Array.from({ length: 240 }, (_, i) => at(i / 240 * 24 / 24, SR, SS).color).includes('#e8955c'), '']);
// 不写死 0.5：夏至长昼（05:02 日出 / 18:58 日落）同样正确
eq('夏至正午近白',   at(h(12), 0.21, 0.79).color, '#f2f6fb');
eq('夏至 18:30 金色', at(h(18.5), 0.21, 0.79).color, '#e8955c');
eq('夏至 19:30 入夜', at(h(19.5), 0.21, 0.79).color, '#3d3a44');

let bad = 0;
for (const [name, ok, extra] of checks) {
  console.log(`  ${ok ? 'PASS' : 'FAIL'}  ${name}${ok ? '' : '  ' + extra}`);
  if (!ok) bad++;
}
if (bad) {
  console.error(`\n云层昼夜着色断言失败 ${bad} 项`);
  process.exit(1);
}
console.log(`\n云层昼夜着色断言全部通过（${checks.length} 项）`);
