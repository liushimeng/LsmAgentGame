/**
 * cityVehicleRegistry —— 车辆实例位置注册表（批次 85 §3.2.1）。
 *
 * 用途：`focus_entity` 显示指令的 `vehicle:<id>` 解析（cityEntityResolver 补的
 * 一级）——车辆每 N 帧 / 位移超阈值时把自己的世界坐标上报进来，解析器查表
 * 定位后拉相机（如「把视角拉到那辆卡车」）。
 *
 * 设计纪律（批次 46 DC/帧率教训）：
 *   - **纯 Map 单例，零 React state** —— 车辆坐标逐帧变化，走 store 会引发高频
 *     重渲染；写入是 Map 原地改字段（稳态零分配），读取方（解析器）按需查表。
 *   - **上报方节流**（每 N 帧或位移超阈值，见 props/Vehicle.tsx），注册表本身
 *     不管节流。
 *   - `nearest()` 只作为「聚焦最近的车」类容错查询能力**导出备用**；
 *     `focus_entity` **不用它瞎聚** —— 只在明确 id 时聚焦，未命中回
 *     `entity_not_found`（不静默、不瞎聚，批次 84 §11 纪律）。
 *
 * id 约定：车辆渲染方（StreetPropsLayer）按 `<variant>-<序号>` 生成稳定 id
 * （如 `sedan-0` / `truck-2`），可被 `vehicle:<id>` 引用；kind = 车型
 * （sedan / truck / bus / taxi），供查询方甄别。
 *
 * 生命周期：Vehicle 组件挂载 register / 卸载 unregister；换房时挂载树整体
 * 重挂，unregister 兜底清残留（另有 `clearVehicleRegistry()` 供换房显式清空）。
 */

/** 一条车辆记录（世界坐标 + 可选车型）。 */
interface VehicleRecord {
  x: number;
  z: number;
  kind?: string;
}

const vehicles = new Map<string, VehicleRecord>();

/** 车辆位置查询接口（register/unregister 由车辆渲染组件调用）。 */
export const vehicleRegistry = {
  /** 登记 / 更新车辆世界坐标（同 id 重复 register = 原地更新，稳态零分配）。 */
  register(id: string, pos: { x: number; z: number }, kind?: string): void {
    const key = (id ?? '').trim();
    if (!key) return;
    const hit = vehicles.get(key);
    if (hit) {
      hit.x = pos.x;
      hit.z = pos.z;
      if (kind !== undefined) hit.kind = kind;
    } else {
      vehicles.set(key, { x: pos.x, z: pos.z, ...(kind ? { kind } : {}) });
    }
  },

  /** 注销车辆（组件卸载时调用，防残留坐标被 `focus_entity` 误命中）。 */
  unregister(id: string): void {
    vehicles.delete((id ?? '').trim());
  },

  /** 查车辆当前坐标；未登记返回 null。 */
  lookup(id: string): { x: number; z: number } | null {
    const hit = vehicles.get((id ?? '').trim());
    return hit ? { x: hit.x, z: hit.z } : null;
  },

  /**
   * 最近邻车辆 id（欧氏距离）；无车返回 null。
   * ⚠ 仅供容错查询（如「聚焦最近的车」由调用方显式决定），`focus_entity`
   * 的解析链**不**用它兜底（禁止瞎聚）。
   */
  nearest(x: number, z: number): string | null {
    let bestId: string | null = null;
    let bestD2 = Infinity;
    for (const [id, rec] of vehicles) {
      const d2 = (rec.x - x) * (rec.x - x) + (rec.z - z) * (rec.z - z);
      if (d2 < bestD2) {
        bestD2 = d2;
        bestId = id;
      }
    }
    return bestId;
  },

  /** 全部已登记车辆 id（快照数组；调试 / 枚举用）。 */
  ids(): string[] {
    return [...vehicles.keys()];
  },
};

/** 清空注册表（换房 / 场景整体重挂；单个车辆生命周期走 unregister）。 */
export function clearVehicleRegistry(): void {
  vehicles.clear();
}
