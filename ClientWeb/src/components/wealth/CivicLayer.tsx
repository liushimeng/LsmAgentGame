/**
 * CivicLayer（18-AA · §1 总体架构 / §5.3 地面扩围）—— 市政层统一挂载点。
 *   在 WealthCityMap 内挂载，调用 15 项 civic 设施 + IntersectionSignals + TreeV3。
 *   地面扩围 WORLD_GROUND_SIZE = 120（02 §5.3）：原 WORLD_SIZE=80 不变，仅 Ground 平面放大。
 *   这里同时挂载扩围后的远景 Outskirts（r∈[38,58]）。
 */
import { PortTerminal } from './civic/PortTerminal';
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
import { IntersectionSignals } from './civic/IntersectionSignals';

export function CivicLayer() {
  return (
    <group>
      <PortTerminal />
      <SportsField />
      <RailViaduct />
      <HeliPad />
      <GasStation />
      <Substation />
      <WaterTower />
      <CommTower />
      <FireStation />
      <PoliceStation />
      <CityHall />
      <Outskirts />
      <ParkExtras />
      <CanalExtras />
      <IntersectionSignals />
    </group>
  );
}