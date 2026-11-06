/**
 * CivicLayer（18-AA · §1 总体架构 / §5.3 地面扩围）—— 市政层统一挂载点。
 *   在 VirtualCityCityMap 内挂载，调用 15 项 civic 设施（批次 24 起路口信号灯
 *   移交 <TrafficSignals> 全局实例化，civic/IntersectionSignals.tsx 已删除）。
 *   地面扩围 WORLD_GROUND_SIZE = 120（02 §5.3）：原 WORLD_SIZE=80 不变，仅 Ground 平面放大。
 *   这里同时挂载扩围后的远景 Outskirts（r∈[38,58]）。
 *
 *   批次 85 §3.2.3：逐设施**实例级筛选**（objectInfo tag，如 `civic.city-hall` /
 *   `park.bench`）—— `set_filter` 的「按类别显隐」退化口径逐件落点（visible
 *   闸门不卸载组件；无筛选 / 图层级筛选恒可见，零回归）。
 */
import { PortTerminal } from './civic/PortTerminal';
import { ParkBenches } from './props/ParkBenches';
import { ParkLamps } from './props/ParkLamps';
import { SportsField } from './civic/SportsField';
import { RailViaduct } from './civic/RailViaduct';
import { HeliPad } from './civic/HeliPad';
import { GasStation } from './civic/GasStation';
import { Substation } from './civic/Substation';
import { WaterTower } from './civic/WaterTower';
import { CommTower } from './civic/CommTower';
import { FireStation } from './civic/FireStation';
import { PoliceStation } from './civic/PoliceStation';
import { CityHall } from './civic/CityHall';
import { Outskirts } from './civic/Outskirts';
import { ParkExtras } from './civic/ParkExtras';
import { CanalExtras } from './civic/CanalExtras';
import { filterAllowsInstance, useCityShowStore } from './cityShowStore';

export function CivicLayer() {
  // 批次 85：一次订阅，逐设施纯函数判可见（tag = objectInfo 注册表 id）。
  const filter = useCityShowStore((s) => s.filter);
  const vis = (tag: string) => filterAllowsInstance(filter, tag);
  return (
    <group>
      <group visible={vis('civic.port-terminal')}><PortTerminal /></group>
      <group visible={vis('civic.sports-field')}><SportsField /></group>
      <group visible={vis('civic.rail-viaduct')}><RailViaduct /></group>
      <group visible={vis('civic.heli-pad')}><HeliPad /></group>
      <group visible={vis('civic.gas-station')}><GasStation /></group>
      <group visible={vis('civic.substation')}><Substation /></group>
      <group visible={vis('civic.water-tower')}><WaterTower /></group>
      <group visible={vis('civic.comm-tower')}><CommTower /></group>
      <group visible={vis('civic.fire-station')}><FireStation /></group>
      <group visible={vis('civic.police-station')}><PoliceStation /></group>
      <group visible={vis('civic.city-hall')}><CityHall /></group>
      <group visible={vis('civic.outskirts')}><Outskirts /></group>
      {/* ParkExtras 内含 park.pavilion / park.playground / park.fitness /
          civic.park-extras 四种 tag —— 逐件闸门在其内部（组件自带 infoId）。 */}
      <ParkExtras />
      {/* 批次 45 公园设施：长椅 ×10 / 园灯 ×12（实例化；点位见 props/parkLayout.ts） */}
      <group visible={vis('park.bench')}><ParkBenches /></group>
      <group visible={vis('park.lamp')}><ParkLamps /></group>
      <group visible={vis('civic.canal-extras')}><CanalExtras /></group>
    </group>
  );
}
