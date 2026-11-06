/**
 * `react-dom/server.browser` 的最小类型声明（批次 85 单测基建）。
 *
 * 背景：`@types/react-dom@18` 只声明了 `react-dom/server`（node 版），没有
 * `server.browser` 的 d.ts；而 `verify_city_show_adapter.mjs` 的 esbuild 打包
 * （format: 'esm'）遇到 `react-dom/server.node` 里的 `require('stream')` 会抛
 * 「Dynamic require of "stream" is not supported」。`react-dom/server.browser`
 * 不 require 任何 node 内置模块且导出同名 `renderToStaticMarkup`，可干净打包。
 *
 * 只为单测服务：真实产品代码不经此模块渲染（LabelsGate 是纯组件）。
 */
declare module 'react-dom/server.browser' {
  import type { ReactNode } from 'react';
  /** 把 React 元素渲染成静态 HTML 字符串（无副作用标记、不跑 effects）。 */
  export function renderToStaticMarkup(element: ReactNode): string;
  /** 同上（保留 SSR 文档形态；单测未用，仅补齐导出面）。 */
  export function renderToString(element: ReactNode): string;
}
