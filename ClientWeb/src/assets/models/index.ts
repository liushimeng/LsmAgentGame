/**
 * Blender 真实 3D 模型（.glb）访问层（19-Blender3D模型集成）。
 *
 * 与 `assets/images/virtualCity/index.ts` 完全同构：
 *   - `import.meta.glob` eager=true,import default → Vite 自动为每个 .glb 产物生成 contenthash
 *   - 缺失文件返回 ''（不抛错），调用方走降级链（程序化几何）
 *
 * 规约（02-架构设计 §2）：
 *   - URL 拼接：`./<category>/<name>.glb`，category ∈ {civic,vehicles,nature,characters,road,ocean}
 *     （ocean 为批次 26 新增类别：灯塔/货船/帆船，南·海洋带用）
 *   - 单文件 ≤ 500 KB（CI 硬约束，3d_script/__common__.py::export_glb 内不强制但 build_city_hall.py 已自检）
 *   - 调用方 `modelUrl(...)` 必传 category+name 两参；MODEL_NAMES 字面量禁止运行时拼字符串
 *
 * ⚠️ 注意：前端构建**不依赖**所有 .glb 都已生成 —— import.meta.glob 按构建时存在的文件打包容错
 *    新增/删除 .glb 只需重启 vite dev server 或重新 build。
 */

const civicImgs = import.meta.glob<string>('./civic/*.glb', { eager: true, query: '?url', import: 'default' });
const vehicleImgs = import.meta.glob<string>('./vehicles/*.glb', { eager: true, query: '?url', import: 'default' });
const natureImgs = import.meta.glob<string>('./nature/*.glb', { eager: true, query: '?url', import: 'default' });
const charImgs = import.meta.glob<string>('./characters/*.glb', { eager: true, query: '?url', import: 'default' });
const roadImgs = import.meta.glob<string>('./road/*.glb', { eager: true, query: '?url', import: 'default' });
const oceanImgs = import.meta.glob<string>('./ocean/*.glb', { eager: true, query: '?url', import: 'default' });

export type ModelCategory = 'civic' | 'vehicles' | 'nature' | 'characters' | 'road' | 'ocean';

/**
 * 模型 URL（缺失 = ''，调用方退回原程序化几何）。
 * @param category 类别（civic/vehicles/nature/characters/road/ocean）
 * @param name 文件 stem（不带 .glb 后缀；与 3d_script/build_<name>.py 一一对应）
 */
export function modelUrl(category: ModelCategory, name: string): string {
  switch (category) {
    case 'civic':
      return civicImgs[`./civic/${name}.glb`] ?? '';
    case 'vehicles':
      return vehicleImgs[`./vehicles/${name}.glb`] ?? '';
    case 'nature':
      return natureImgs[`./nature/${name}.glb`] ?? '';
    case 'characters':
      return charImgs[`./characters/${name}.glb`] ?? '';
    case 'road':
      return roadImgs[`./road/${name}.glb`] ?? '';
    case 'ocean':
      return oceanImgs[`./ocean/${name}.glb`] ?? '';
  }
}

/** 模型名字面量（与 3d_script/build_*.py 文件名一一对齐，禁止运行时拼字符串）。 */
export const MODEL_NAMES = {
  civic: [
    'city_hall', 'comm_tower', 'water_tower', 'fire_station', 'police_station',
    // 批次 46 城市公用设施三件（3d_script/build_substation|_gas_station|_heli_pad.py）：
    //   substation   = 城区 10/35kV 配电站 18.34×12.41×19.67 m（围墙 2.45 m + 12 m 出线电杆）
    //   gas_station  = 加油站 25.93×9.40×20.07 m（罩棚净高 4.70 m，进站口朝 three +Z）
    //   heli_pad     = ⌀28.20×4.09 m 直升机停机坪（FATO 环 + H 标识 + TLOF 着陆区灯）
    // 消费方：civic/{Substation,GasStation,HeliPad}.tsx，均包
    // `<Model>` 式降级链（GLB 缺失/未启用 ⇒ 原程序化几何）。
    // 材质槽名（供昼夜调制）：Substation_Sign / GasStation_SIGN_Panel / GasStation_PUMP_Screen
    //                        / HeliPad_TLOF_Green / HeliPad_EdgeLight。
    //
    // ⚠ park_bench / park_pavilion / park_playground / park_fitness / park_lamp
    //   （批次 45）**未登记在本表** —— 属批次 45 遗留的 §130 缺口（`ParkExtras.tsx`
    //   以 `glbName: string` 形参直传 modelUrl，绕过了本表的字面量约束）。
    //   本批不越界修，后续「asset 接线收口」批次统一处理。
    'substation', 'gas_station', 'heli_pad',
    // 批次 47 体育场（3d_script/build_sports_field.py）：200 m 半圆式田径场
    //   110.0×15.58×74.0 m（6 道 × 1.22 m，r=20 m，单侧直道 37.17 m ⇒ 精确 200 m）。
    //   消费方：civic/SportsField.tsx。材质槽名 `Stadium_Floodlight`（夜间 2.6）。
    'sports_field',
  ] as const,
  vehicles: [
    'sedan', 'truck', 'bus', 'taxi',
    // 批次 41 涂装变体（A6）：sedan_silver 银灰漆（尺寸同 sedan）/ truck_white 白漆
    // （尺寸同 truck）—— 只作 Vehicle glbName，variant 仍走 4 原值查 REAL_DIMS_M。
    'sedan_silver', 'truck_white',
  ] as const,
  nature: [
    'oak_tree',
    'snow_mountain',
    'pine_tree',
    'cactus',
    // 批次 27 季节橡树（3d_script/build_oak_tree_season.py，Z-up 直立贴地；
    // EastForest 按 season 切换，缺失降级回 oak_tree）
    'oak_tree_spring',
    'oak_tree_autumn',
    'oak_tree_winter',
    // 批次 44 城市树（行道树/区内园林树三变体，配 StreetPropsLayer.TREE_VARIANTS）：
    //   street_tree = 悬铃木(法桐)杯状形行道树 4.96×9.0×4.9 m
    //   pine_tree   = 黑松/雪松（**已重制**，原 3 锥玩具树 → 4 层平展针叶盘）3.98×7.0×3.95 m
    //   palm_tree   = 棕榈(老人葵) 2.76×5.42×2.81 m，补齐此前「声明了却无资产」的 'palm' 变体
    'street_tree',
    'palm_tree',
  ] as const,
  characters: [
    // 批次 36 §4.1：性别 × 年龄段 15 模型（3d_script/build_character.py --variant <key> 产出）。
    // 材质槽固定 5 个供运行时换色：PedestrianBody/Pants/Head/Shoes/Hair；
    // 选型由 crowdFormula.modelKeyFor(gender, age) 推导，缺失走 char_casual → pedestrian_walk 兜底。
    'char_m_youth', 'char_m_young', 'char_m_middle', 'char_m_senior', 'char_m_elder',
    'char_f_youth', 'char_f_young', 'char_f_middle', 'char_f_senior', 'char_f_elder',
    'char_u_youth', 'char_u_young', 'char_u_middle', 'char_u_senior', 'char_u_elder',
    // 批次 34 §5.2：8 个人物原型（3d_script/build_character.py --archetype <name> 产出）。
    // 批次 36 §4.6：保留为兜底池 + 装饰池多样性，不再作为居民选型主路径。
    // 每个原型剪影/体型不同（elder 驼背矮壮 / student 高瘦 / worker 壮实 / formal 挺拔…），
    // 共享 10 骨 + 24 帧 walk clip；材质槽名 PedestrianBody/Pants/Head/Shoes 供运行时换色。
    'char_business',
    'char_casual',
    'char_worker',
    'char_elder',
    'char_student',
    'char_service',
    'char_formal',
    'char_parent',
    // 原型缺失时的降级 GLB（批次 19 交付，保留为 fallback）
    'pedestrian_walk',
  ] as const,
  // 批次 30：`road_props` 注销（§130「声明了却从不接线」）—— 全仓零消费点：
  // 该 GLB 是 3 盏灯沿 X 排布的**合成 mesh**（单节点 RoadProps/单 mesh Cylinder，
  // 无逐灯命名节点），无法做单灯实例化（measureNode 无从指定）；路灯实际由
  // props/StreetLightsInstanced.tsx 程序化实例化。重生为逐灯命名节点后再注册。
  // 批次 31：road_props.glb 文件已删除（设计 31 §4.2 资产清理），本清单维持空。
  // trash_can.glb 不在本清单（MODEL_NAMES 仅文档/类型用，glob 才是取 URL 的事实来源）。
  //
  // 批次 38 R3 · §4.7(b)：运河水系三件套（art-designer 产出；
  // `3d_script/build_canal_bank.py` / `build_canal_reed.py` / `build_bridge_rail.py`）。
  // 消费方：civic/CanalExtras（驳岸/芦苇）+ CanalBridge（桥栏杆），一律包
  // `<Model url={...}>{程序化 fallback}</Model>`（CLAUDE.md §27.3 硬约束 3）。
  road: [
    'canal_bank', 'canal_reed', 'bridge_rail',
    // 批次 42 街具（3d_script/build_bus_stop|mailbox|street_sign|parking_meter|bike_rack.py）：
    // bus_stop / parking_meter 单节点 GLB（灯箱 Lightbox / 屏幕 Screen 材质名供昼夜调制）；
    // street_sign 双变体节点 Sign_Traffic / Sign_Info；bike_rack 双节点 BikeRack + BikeRack_Bike。
    // 消费方：props/{BusStop,Mailbox,Sign,ParkingMeter,BicycleRack}.tsx（缺失自动走程序化 fallback）。
    'bus_stop', 'mailbox', 'street_sign', 'parking_meter', 'bike_rack',
    // 批次 43 交通信号灯（3d_script/build_traffic_signal|pedestrian_signal|mast_arm_signal.py）：
    // 单灯头 GLB，LED 按材质名 LEDRed / LEDYellow / LEDGreen（行人灯 LEDRed / LEDGreen）
    // 分组，前端按相位对各组分别调制 emissive —— 真实信号灯同一时刻只亮一色。
    // 消费方：props/TrafficSignals.tsx（机动车）+ props/PedestrianSignals.tsx（行人）。
    'traffic_signal', 'pedestrian_signal', 'mast_arm_signal',
  ] as const,
  ocean: ['lighthouse', 'cargo_ship', 'sailboat'] as const,
} as const;
