/**
 * FundFlowSankeyPanel — 资金流向简化桑基图（2026-09-19 §P2 v2 §13.3）
 *
 * 契约: lag_docs/虚拟城市/已实现/07-P2财富可视化/.../§13.3。
 * 数据源: game.state.flow_stat(本月);节点固定 6 个(salary/firms/market/bank/gov/player)。
 *
 * 渲染: 左侧源节点 + 右侧汇节点 + 中间贝塞尔连线(宽 ∝ amount);
 * 玩家聚合("player")居中,工资注入从左侧入,消费/税/还款向右侧出。
 *
 * - 节点颜色按 kind:source=绿 / sink=橙 / pass=紫;
 * - 对比度(CLAUDE.md §26):文字 ≥4.5:1,连线透明度 0.55(hover 提到 0.85);
 * - 空态:本月无流水 → 渲染 wealth-flow__empty。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny } from '@/types/wealth';
import type { WealthFlowStat, WealthFlowNode, WealthFlowLink } from '@/types/wealth';

interface Props {
  flowStat: WealthFlowStat | null | undefined;
}

const W = 360;
const H = 200;
const PAD_X = 16;
const PAD_TOP = 18;
const PAD_BOTTOM = 18;

const NODE_LABEL_KEYS: Record<string, string> = {
  salary: 'wealth.dashboard.flow.node.salary',
  firms: 'wealth.dashboard.flow.node.firms',
  market: 'wealth.dashboard.flow.node.market',
  bank: 'wealth.dashboard.flow.node.bank',
  gov: 'wealth.dashboard.flow.node.gov',
  player: 'wealth.dashboard.flow.node.player',
  world: 'wealth.dashboard.flow.node.world',
};

const STROKE_CLASS: Record<string, string> = {
  source: 'wealth-flow__link--source',
  sink: 'wealth-flow__link--sink',
  pass: 'wealth-flow__link--pass',
};

export function FundFlowSankeyPanel({ flowStat }: Props) {
  const t = useT();

  if (!flowStat || !flowStat.links || flowStat.links.length === 0) {
    return (
      <div className="wealth-flow">
        <div className="wealth-flow__title">
          <span>{t('wealth.dashboard.flow.title' as TKey)}</span>
        </div>
        <div className="wealth-flow__empty">{t('wealth.dashboard.flow.empty' as TKey)}</div>
      </div>
    );
  }

  // 节点布局:左列(source/p world) | 中列(player) | 右列(sink: firms/gov)
  // 其他(pass: bank/market)根据连接方向动态放左/右
  const leftIds: string[] = [];
  const rightIds: string[] = [];
  const idToAmount = new Map<string, number>();
  for (const n of flowStat.nodes) {
    idToAmount.set(n.id, n.amount_cny);
  }
  // 按 link 出现的位置分类:左侧 = 出现为 from 且目标是 player / 右侧 source;
  // 简化:source → 左; sink → 右; pass → 左/右二选一(按净额方向)
  for (const n of flowStat.nodes) {
    if (n.kind === 'source') leftIds.push(n.id);
    else if (n.kind === 'sink') rightIds.push(n.id);
    else leftIds.push(n.id); // pass 默认左;player 必中,稍后移中
  }
  // player 永远居中
  const playerIdx = leftIds.indexOf('player');
  if (playerIdx >= 0) leftIds.splice(playerIdx, 1);

  // 计算纵坐标:按 amount_cny 比例分配
  const layoutCols = (ids: string[]) => {
    const totalAmount = ids.reduce((s, id) => s + (idToAmount.get(id) || 0), 0) || 1;
    const usableH = H - PAD_TOP - PAD_BOTTOM;
    let yCursor = PAD_TOP;
    return ids.map((id) => {
      const amt = idToAmount.get(id) || 0;
      const nodeH = Math.max(8, (amt / totalAmount) * usableH);
      const y = yCursor;
      yCursor += nodeH + 4; // 4px gap
      return { id, y, h: nodeH, amt };
    });
  };

  const leftCols = layoutCols(leftIds);
  const rightCols = layoutCols(rightIds);
  const leftMap = new Map(leftCols.map((c) => [c.id, c]));
  const rightMap = new Map(rightCols.map((c) => [c.id, c]));

  // player 节点固定居中,高度 = Σlinks to/from player amount 的 60%
  const playerAmt = idToAmount.get('player') || 0;
  const playerH = Math.max(20, Math.min(80, playerAmt / Math.max(1, totalMaxAmount(idToAmount)) * (H - PAD_TOP - PAD_BOTTOM)));
  const playerY = (H - playerH) / 2;

  // 边布局:每条边在源节点上纵向累加 + 在目标节点上纵向累加
  // 简化:每条边在源端 y = 节点 y 顶端 + (i / n) * 节点 h
  const outgoingByFrom = new Map<string, WealthFlowLink[]>();
  for (const l of flowStat.links) {
    const arr = outgoingByFrom.get(l.from) || [];
    arr.push(l);
    outgoingByFrom.set(l.from, arr);
  }
  const incomingByTo = new Map<string, WealthFlowLink[]>();
  for (const l of flowStat.links) {
    const arr = incomingByTo.get(l.to) || [];
    arr.push(l);
    incomingByTo.set(l.to, arr);
  }

  // 绘制贝塞尔路径
  const xLeft = PAD_X + 80; // 节点宽度 80
  const xRight = W - PAD_X - 80;
  const xPlayer = (W) / 2 - 30;

  const isLeft = (id: string) => leftMap.has(id) || id === 'player';
  const xOf = (id: string) => {
    if (id === 'player') return xPlayer + 60;
    return isLeft(id) ? xLeft + 80 : xRight;
  };

  // 计算每条边的源点 y / 目标点 y(按 amount 累加)
  const computeYs = () => {
    type EdgeGeom = {
      link: WealthFlowLink;
      x1: number;
      y1: number;
      x2: number;
      y2: number;
      strokeWidth: number;
    };
    const edges: EdgeGeom[] = [];

    // 源端累加游标
    const fromCursor = new Map<string, number>();
    for (const [fromId, links] of outgoingByFrom) {
      fromCursor.set(fromId, 0);
      for (const l of links) {
        const node = leftMap.get(fromId) || rightMap.get(fromId);
        const playerNode = { id: 'player', y: playerY, h: playerH };
        const yStart = (() => {
          if (fromId === 'player') {
            const cur = fromCursor.get(fromId) || 0;
            fromCursor.set(fromId, cur + Math.max(1, l.amount_cny / Math.max(1, playerAmt) * playerH));
            return playerNode.y + cur;
          }
          if (!node) return PAD_TOP;
          const cur = fromCursor.get(fromId) || 0;
          const slotH = (l.amount_cny / (idToAmount.get(fromId) || 1)) * node.h;
          fromCursor.set(fromId, cur + slotH);
          return node.y + cur;
        })();

        const toCursorKey = l.to;
        const toNode = leftMap.get(l.to) || rightMap.get(l.to);
        const playerNodeT = { id: 'player', y: playerY, h: playerH };
        const yEnd = (() => {
          if (l.to === 'player') {
            const cur = (incomingByTo.get(toCursorKey)?.findIndex((x) => x === l) ?? 0);
            const slotH = (l.amount_cny / Math.max(1, playerAmt)) * playerH;
            return playerNodeT.y + cur * slotH;
          }
          if (!toNode) return PAD_TOP;
          const incoming = incomingByTo.get(l.to) || [];
          const idx = incoming.findIndex((x) => x === l);
          const slotH = (l.amount_cny / (idToAmount.get(l.to) || 1)) * toNode.h;
          return toNode.y + idx * slotH;
        })();

        // 宽度按 amount(对数缩放防超大边压扁其他边)
        const maxLinkAmount = Math.max(...flowStat.links.map((ll) => ll.amount_cny));
        const strokeWidth = Math.max(1.5, (l.amount_cny / maxLinkAmount) * 10);

        edges.push({
          link: l,
          x1: xOf(fromId),
          y1: yStart,
          x2: xOf(l.to),
          y2: yEnd,
          strokeWidth,
        });
      }
    }
    return edges;
  };

  const edges = computeYs();

  return (
    <div className="wealth-flow">
      <div className="wealth-flow__title">
        <span>{t('wealth.dashboard.flow.title' as TKey)}</span>
        <span className="wealth-flow__totals">
          <span className="wealth-flow__total--in">
            ↑ {t('wealth.dashboard.flow.in' as TKey)}: ¥{formatCny(flowStat.total_in_cny)}
          </span>
          <span className="wealth-flow__total--out">
            ↓ {t('wealth.dashboard.flow.out' as TKey)}: ¥{formatCny(flowStat.total_out_cny)}
          </span>
        </span>
      </div>
      {/* 18/04 AB-3：外层横向滚动容器 + SVG width:100%/height:auto —— 380/300px
          侧栏下既不横向溢出也不压扁不可读（viewBox 已有，收缩交给 CSS）。 */}
      <div className="wealth-sankey-scroll">
      <svg
        className="wealth-flow__svg"
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="xMidYMid meet"
        role="img"
        aria-label={t('wealth.dashboard.flow.title' as TKey)}
      >
        {/* 边 */}
        {edges.map((e, i) => {
          const kindClass = STROKE_CLASS[linkKind(e.link, flowStat.nodes)] || STROKE_CLASS.pass;
          const path = bezierPath(e.x1, e.y1 + e.strokeWidth / 2, e.x2, e.y2 + e.strokeWidth / 2);
          const label = t('wealth.dashboard.flow.tooltip' as TKey, {
            from: t(NODE_LABEL_KEYS[e.link.from] as TKey),
            to: t(NODE_LABEL_KEYS[e.link.to] as TKey),
            amount: formatCny(e.link.amount_cny),
          });
          return (
            <path
              key={`edge-${i}`}
              d={path}
              className={`wealth-flow__link ${kindClass}`}
              strokeWidth={e.strokeWidth}
            >
              <title>{label}</title>
            </path>
          );
        })}
        {/* 左列节点 */}
        {leftCols.map((c) => (
          <NodeBox
            key={c.id}
            x={xLeft}
            y={c.y}
            w={80}
            h={c.h}
            label={t(NODE_LABEL_KEYS[c.id] as TKey)}
            amount={c.amt}
            side="left"
          />
        ))}
        {/* 右列节点 */}
        {rightCols.map((c) => (
          <NodeBox
            key={c.id}
            x={xRight}
            y={c.y}
            w={80}
            h={c.h}
            label={t(NODE_LABEL_KEYS[c.id] as TKey)}
            amount={c.amt}
            side="right"
          />
        ))}
        {/* 玩家节点(中) */}
        <NodeBox
          x={xPlayer}
          y={playerY}
          w={60}
          h={playerH}
          label={t(NODE_LABEL_KEYS.player as TKey)}
          amount={playerAmt}
          side="center"
        />
      </svg>
      </div>
    </div>
  );
}

function totalMaxAmount(m: Map<string, number>): number {
  let max = 0;
  for (const v of m.values()) if (v > max) max = v;
  return max;
}

function linkKind(link: WealthFlowLink, nodes: WealthFlowNode[]): string {
  const to = nodes.find((n) => n.id === link.to);
  return to?.kind || 'pass';
}

function bezierPath(x1: number, y1: number, x2: number, y2: number): string {
  // 横向贝塞尔:控制点 x = 中点,保持平滑
  const cx1 = (x1 + x2) / 2;
  return `M ${x1} ${y1} C ${cx1} ${y1}, ${cx1} ${y2}, ${x2} ${y2}`;
}

function NodeBox({
  x,
  y,
  w,
  h,
  label,
  amount,
  side,
}: {
  x: number;
  y: number;
  w: number;
  h: number;
  label: string;
  amount: number;
  side: 'left' | 'right' | 'center';
}) {
  // 节点配色按 kind 推断(简化):source=绿 / sink=橙 / pass=紫
  const fill =
    label.includes('工资') || label.includes('Salary') || label.includes('給料') || label.includes('系统外')
      ? '#0d9488'
      : label.includes('企业') || label.includes('Firms') || label.includes('企業') || label.includes('政府') || label.includes('Gov')
        ? '#7c2d12'
        : '#4c1d95';

  const labelX = side === 'left' ? x + 4 : side === 'right' ? x + 4 : x + 4;
  const amountX = side === 'left' ? x + 4 : side === 'right' ? x + 4 : x + 4;
  const textAnchor = side === 'left' ? 'start' : side === 'right' ? 'end' : 'middle';

  return (
    <g>
      <rect x={x} y={y} width={w} height={h} fill={fill} rx={3} />
      <text x={labelX} y={y + h / 2 - 2} className="wealth-flow__node-label" textAnchor={textAnchor}>
        {label}
      </text>
      <text x={amountX} y={y + h / 2 + 10} className="wealth-flow__node-amount" textAnchor={textAnchor}>
        ¥{formatCny(amount)}
      </text>
    </g>
  );
}