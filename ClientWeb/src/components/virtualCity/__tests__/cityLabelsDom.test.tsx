/**
 * labels 图层受控渲染单测（批次 85 §3.2.2）。
 *
 * 由 `scripts/verify_city_show_adapter.mjs` 经入口 import 打包执行
 * （npm run verify:city-show-adapter）；模块加载即自跑，失败抛错 ⇒ 非零退出。
 *
 * §7.1 硬性断言：`labels` 图层关闭 ⇒ 名牌 / 气泡**不渲染**（DOM 里不存在
 * 节点）；开启 ⇒ 存在。用 react-dom/server 直渲**真实**闸门与标签组件
 * （LabelsGate / AgentNameTag / SpeechBubble / CityAnnotationLabel），
 * 断言输出 markup 里 class 节点的有无 —— 不是 opacity 糊弄（§26）。
 */

// react-dom/server.browser：esbuild（format:esm）打包 node 版 server 会因
// `require('stream')` 抛「Dynamic require not supported」，browser 版零 node
// 内置依赖且导出同名 renderToStaticMarkup（类型声明见 react-dom-server-browser.d.ts）。
import { renderToStaticMarkup } from 'react-dom/server.browser';
import {
  AgentNameTag,
  CityAnnotationLabel,
  LabelsGate,
  SpeechBubble,
} from '../cityLabels';
import { cityShowApi, useCityShowStore } from '../cityShowStore';

function ok(cond: boolean, msg: string): void {
  if (!cond) throw new Error(`[cityLabelsDom] ${msg}`);
}

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[cityLabelsDom] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

/**
 * 测试夹具写入 —— **原地改字段**（不是 setState）。
 *
 * 原因：zustand v4 的 `useStore` 把 SSR 快照指向 `getInitialState`（初始对象，
 * 稳定可水合，vanilla.js:41 `initialState = state = createState(...)`）；
 * `renderToStaticMarkup` 走的正是 SSR 快照 ⇒ 先 `setState` 再渲染会读到**旧**
 * 引用。原地改字段时 `state === initialState`，SSR 与 `getState()` 同时可见。
 * 仅单测夹具这么写（生产写路径一律走 `cityShowApi` 的 set* 动作）。
 */
function fixture(mutate: (s: ReturnType<typeof useCityShowStore.getState>) => void): void {
  mutate(useCityShowStore.getState());
}

/** 名牌 / 气泡节点的 class 标记（生产 DOM 的识别锚）。 */
const NAME_TAG_MARK = 'virtualCity-token-tag';
const BUBBLE_MARK = 'virtualCity-speech-bubble';

export function runCityLabelsDomTests(): void {
  const setLabels = (on: boolean) => {
    fixture((s) => {
      s.layers.labels = on;
    });
  };

  const tag = (
    <AgentNameTag
      seat={1}
      name="测试员"
      emoji="📊"
      statusIcon=""
      netWorthText="¥1万"
      isMe={false}
      isBot={false}
      compact={false}
    />
  );
  const speech = <SpeechBubble text="你好" />;
  const action = <SpeechBubble variant="action" icon="💭" text="想一想" />;
  const voice = <SpeechBubble variant="voice" name="🗣 张三" text="市民之声" />;

  // ── 1. labels 开启 ⇒ 名牌 / 气泡节点存在 ─────────────────────────
  setLabels(true);
  const on = renderToStaticMarkup(
    <LabelsGate>
      {tag}
      {speech}
      {action}
      {voice}
    </LabelsGate>,
  );
  ok(on.includes(NAME_TAG_MARK), '开启时 DOM 应存在名牌节点');
  ok(on.includes(BUBBLE_MARK), '开启时 DOM 应存在气泡节点');
  ok(on.includes('virtualCity-token-tag__seat'), '开启时名牌结构应完整');
  ok(on.includes('virtualCity-speech-bubble--action'), '开启时动作气泡应存在');
  ok(on.includes('virtualCity-speech-bubble--voice'), '开启时市民之声气泡应存在');
  ok(on.includes('virtualCity-speech-bubble__name'), '开启时气泡署名位应存在');

  // ── 2. labels 关闭 ⇒ 名牌 / 气泡**不进 DOM**（整段为空）──────────
  setLabels(false);
  const off = renderToStaticMarkup(
    <LabelsGate>
      {tag}
      {speech}
      {action}
      {voice}
    </LabelsGate>,
  );
  ok(off === '', '关闭时 LabelsGate 应返回 null（children 不渲染）');
  ok(!off.includes(NAME_TAG_MARK), '关闭时 DOM 不应存在名牌节点');
  ok(!off.includes(BUBBLE_MARK), '关闭时 DOM 不应存在气泡节点');

  // ── 3. CityAnnotationLabel（annotate 标注）同样受控 ──────────────
  fixture((s) => {
    s.annotation = { text: '地震震中', ts: Date.now() };
  });
  setLabels(true);
  const annOn = renderToStaticMarkup(<CityAnnotationLabel />);
  ok(
    annOn.includes('virtualCity-sysdialog-annotation'),
    '开启时标注节点应存在',
  );
  setLabels(false);
  const annOff = renderToStaticMarkup(<CityAnnotationLabel />);
  ok(annOff === '', '关闭时标注不应进 DOM');
  ok(
    !annOff.includes('virtualCity-sysdialog-annotation'),
    '关闭时 DOM 不应存在标注节点',
  );

  // 无标注时无论开关都不渲染。
  fixture((s) => {
    s.annotation = null;
  });
  setLabels(true);
  eq(renderToStaticMarkup(<CityAnnotationLabel />), '', '无标注时不应渲染');

  // ── 4. 默认值回归：labels 默认开启（既有视觉零变化）──────────────
  // 换房/清覆盖后 layers 回默认全可见（resetLayers）。
  setLabels(false);
  cityShowApi.resetLayers();
  ok(
    useCityShowStore.getState().layers.labels === true,
    'labels 默认应为开启',
  );

  // eslint-disable-next-line no-console
  console.log('[cityLabelsDom] 全部断言通过');
}

// 直接执行（被打包器当作入口 import）时自动运行。
runCityLabelsDomTests();
