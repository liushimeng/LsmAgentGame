/**
 * crowdRegistry — 批次 35 §5.3：上街居民（背景行人）live 位置注册表。
 *
 * 用途：CityVoiceBubbleLayer 把「市民之声」气泡**锚定到发声居民本人头顶**——
 * 按 voice.name 匹配 crowd entry.name，命中后读本表拿该行人当前世界坐标。
 *
 * 写入方：PedestrianV3（trackIndex prop，useFrame 节流块内同步自身 group 位置，
 * 纯 Map 写零分配）；清空方：StreetPropsLayer 在 crowdPeds 重算（换班/换代）时
 * clearCrowdPositions()，防旧代居民残留坐标被气泡错误锚定。
 *
 * 模块级单例（不进 React state）：位置每帧变化，走 store 会引发高频重渲染；
 * 消费方（气泡层）按自身 1s 时钟 tick 重读即可满足「跟随」观感。
 */

/** residentIndex → 当前世界坐标（世界单位；y 恒为路面/底板标高，不入表）。 */
const livePositions = new Map<number, { x: number; z: number }>();

/**
 * 写入/更新一名居民的 live 位置（PedestrianV3 每节流帧调用）。
 * 稳态零分配：命中即原地改字段，未命中才分配新条目。
 */
export function setCrowdPosition(index: number, x: number, z: number): void {
  const hit = livePositions.get(index);
  if (hit) {
    hit.x = x;
    hit.z = z;
  } else {
    livePositions.set(index, { x, z });
  }
}

/** 读一名居民的 live 位置（未注册 = undefined，调用方自行回落）。 */
export function getCrowdPosition(index: number): { x: number; z: number } | undefined {
  return livePositions.get(index);
}

/** 清空注册表（换班/换代/卸载时调用）。 */
export function clearCrowdPositions(): void {
  livePositions.clear();
}
