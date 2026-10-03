/**
 * cityScale — 虚拟城市 3D 高度系统 + **度量衡唯一事实来源**（2026-09-21，批次 29 升格）。
 *
 * 世界标尺：**1 世界单位 = 10 米**。地图 120×120 = 1200m×1200m 城区
 * （v2.12 阶段 2：40×40 → 80×80，16 城区；批次 20：80×80 → 120×120，32 城区）。
 * 所有 3D 建筑 / 道具的「真实米制尺寸 → 世界单位」换算统一经 u()，
 * 禁止在组件内硬编码米制尺寸（方案：tmpPlan/虚拟城市-3D高度系统与贴图渲染优化方案-20260921.md）。
 *
 * 批次 29（GLB 尺寸口径统一）起，**真实米制尺寸表 REAL_DIMS_M 是本文件的核心**：
 *   - 每个 3D 物件的真实尺寸（米）与落地约定（minY）只有这一处；
 *   - **同物件的两条渲染路径共用同一行表值** —— Blender `.glb` 路径（美术按本表重导出）
 *     与程序化 fallback 路径（前端消费本表），从而"包围盒一致"成为可测不变量；
 *   - 表值经 worldDims() 取世界单位（消费端零 scale，批次 26 轴向/单位契约）；
 *   - 表值经 sizeTargetFor() 变成 engine3d/glbSizeGuard 的校验目标：GLB 载入后
 *     dev 态自动量测 Box3 并比对，偏差超 ±5% 即告警（见 engine3d/glbSizeGuard.ts）。
 *   - **轴向约定**（护栏判据 ⑤ 直立 / ⑥ 向前轴）集中在 MODEL_AXIS_CONVENTIONS，
 *     随 sizeTargetFor() 自动带上；只对"能确证"的物件声明（扁平物件/多物件合成 GLB
 *     不声明，理由逐条留在该表注释里）—— 见 guard 文件头「已知边界」。
 *   ⚠️ 改本表 = 同时改「美术导出脚本 + GLB + 两条前端路径」的契约，**改动需三侧同步**。
 *
 * 表值口径（务必遵守，否则 GLB 与 fallback 会再次漂移）：
 *   - x/y/z = 物件**包围盒**尺寸（米）：车/树 = 长(X) × 总高(Y) × 宽(Z)；
 *     建筑 = 占地轮廓宽(X) × **总高含塔尖/屋顶(Y)** × 进深(Z)。建筑的 x/z 取
 *     **主体轮廓（含屋檐腰线，不含门廊/雨棚/台阶/旗杆等外挑物）**；
 *   - minY = 包围盒底面（米）：`0` = GLB 原点落在物件底面（轮底/脚底/桶底/建筑基座），
 *     即 `<Model>` 零补偿直挂时自动贴地；缺省 = 不做落地校验。
 */

import type { VirtualCityDistrictId } from '@/types/virtualCity';
import type { ModelSizeTarget } from '@/engine3d';

/** 世界标尺：1 单位 = 10 米。 */
export const METERS_PER_UNIT = 10;

/** 米 → 世界单位。 */
export const u = (meters: number): number => meters / METERS_PER_UNIT;

/** 层高：3 m/层（0.3 世界单位）。 */
export const FLOOR_HEIGHT_M = 3;

/** 路面标高（世界单位）：0.15 m 抬高避免与地面 z-fighting（Road.tsx ROAD_Y 消费）。 */
export const ROAD_SURFACE_Y = 0.015;

/** 车辆落位 y（世界单位）= 路面之上 5 cm（胎底贴路面又不 z-fighting）。 */
export const VEHICLE_GROUND_Y = ROAD_SURFACE_Y + 0.005;

/**
 * 地块表面标高（世界单位）= DistrictBlock 底板顶面。
 * 批次 30 P0-6「落地基准三源统一」：站在**地块**上的物件（行人 / 街具）以此为
 * 落位 y；路面系物件用 ROAD_SURFACE_Y / VEHICLE_GROUND_Y，人行道用
 * SIDEWALK_Y（Road.tsx = ROAD_SURFACE_Y + 0.012）。禁止再出现第四个 0/0.02/0.035。
 *
 * 批次 38 §4.6 标高修复：0.02 → **0.010**（低于路面 0.015），把分层关系从
 * 「板盖路」翻成「路在板上」—— 街区内部道路不再被区底板吞没。视觉等价于
 * DistrictBlock 几何开口，但零几何重构（方案降级方案优先）。
 */
export const DISTRICT_SURFACE_Y = 0.010;

/**
 * 主干道路面宽（世界单位 1.4u = 14m）。批次 38：从 Road.tsx 上移到本模块
 * （cityObstacles 路廊半宽与 Road 同源，避免 tsx↔ts 循环 import 踩 TDZ）。
 * Road.tsx 仍 re-export 保持既有 `import { ROAD_WIDTH_MAIN } from './Road'` 零回归。
 */
export const ROAD_WIDTH_MAIN = 1.4;
/** 次干道路面宽（0.9u = 9m；同上）。 */
export const ROAD_WIDTH_SIDE = 0.9;

/**
 * 真实米制尺寸表（单位 = 米）—— **虚拟城市 3D 物件尺寸的唯一事实来源**。
 *
 * 与 art-designer 重导出 GLB 时使用的同一张表；`satisfies` 保证字段名受约束。
 * 只填已知值（缺省字段 = 不做该轴校验）；**不要凭感觉改数值** ——
 * 与实测 fallback 不符时先报备（改动需 GLB / 脚本 / 前端三侧同步）。
 */
export interface RealDimsM {
  /** X 向尺寸（米）：车长 / 建筑宽 / 树冠宽。 */
  x?: number;
  /** Y 向尺寸（米）：总高。 */
  y?: number;
  /** Z 向尺寸（米）：车宽 / 建筑进深 / 树冠深。 */
  z?: number;
  /** 包围盒底面（米）：0 = 原点在物件底面（贴地）；缺省 = 不校验落地。 */
  minY?: number;
}

export const REAL_DIMS_M = {
  // ── 街具 ──
  /** 分类垃圾桶：⌀0.50 × H1.00（含盖）—— 批次 30 复核维持原值。
   *  二进制级证据（raw accessor 顶点解析 + 3d_script/verify_glb_aabb.py）：
   *  TrashCan_Green/Blue 节点跨度恰 0.050×0.100×0.050 world、minY=0、节点 scale
   *  全 1 ⇒ GLB 路径 ⌀0.50；程序化 fallback 经 worldDims 同行表值，双路径一致。
   *  （批次 30 曾误判 0.68 —— 系把改表后的 fallback 桶当成 GLB 实测，已回退。） */
  trashCan: { x: 0.5, y: 1.0, z: 0.5, minY: 0 },
  /** 主干道路灯：杆高 12.00（含基座与灯头）。 */
  streetLight: { y: 12.0, minY: 0 },
  /** 次干道路灯总高 7.00（项目现行值；road_props.glb 已随批次 31 删除）。 */
  streetLightSide: { y: 7.0, minY: 0 },
  // ── 批次 42 街具（表值 = 设计 42 §3；GLB 长轴朝向见各行）──
  /**
   * 公交候车亭 5.00 × 2.70 × 1.80（长边 X 沿道路向，含站牌灯箱）。
   * GLB `road/bus_stop`（节点 BusStop）与 fallback 同取本行；灯箱材质名含 `Lightbox`。
   */
  busStop: { x: 5.0, y: 2.7, z: 1.8, minY: 0 },
  /** 柱式邮筒 0.60 × 1.40 × 0.60（直立件；中国邮政绿，投信口朝 +Z）。 */
  mailbox: { x: 0.6, y: 1.4, z: 0.6, minY: 0 },
  /**
   * 路名牌/标志杆 0.62 × 2.65 × 0.12（直立件）。GLB `road/street_sign` 双变体节点
   * Sign_Traffic / Sign_Info 沿 X 排开（仿 trash_can 双桶），消费端 measureNode 单取。
   */
  streetSign: { x: 0.62, y: 2.65, z: 0.12, minY: 0 },
  /** 停车咪表 0.32 × 1.38 × 0.28（直立件；屏幕朝 +Z，材质名含 `Screen`）。 */
  parkingMeter: { x: 0.32, y: 1.38, z: 0.28, minY: 0 },
  /** 倒 U 停车架 + 斜靠自行车 1.80 × 1.10 × 0.62（长边 X；GLB 双节点 BikeRack / BikeRack_Bike）。 */
  bikeRack: { x: 1.8, y: 1.1, z: 0.62, minY: 0 },

  // ── 批次 30 新增（只增不改：旧值不动 ⇒ 无需重导 GLB）──
  /**
   * 城市行道树 = **悬铃木（法桐）**，4.96 × 9.00 × 4.90 m（批次 44 改为真实设计值，
   * 并首次真正消费：GLB 接入 + `crosscheck` 送检，见批次 44 方案 §3.1）。
   *
   * **批次 44 改值理由**：原 4.5 × 9.0 × 4.5 是批次 30 为「程序化 fallback 归一」
   * 设的占位值，挂在 `crosscheck` 的 `PROCEDURAL_ONLY` 里、**零消费点**（T12）。
   * 现按 CJJ/T 75-2023 + 苗圃分级取真值：分枝点 3.0 m、树高 9.0 m、冠幅 5.0 m。
   */
  streetTree: { x: 4.96, y: 9.0, z: 4.9, minY: 0 },
  /** 交通信号杆（杆 + 灯头总高 5.50；灯头尺寸见 TrafficSignals 消费点）。 */
  trafficSignalPole: { y: 5.5, minY: 0 },
  // ── 批次 43 新增：交通信号灯三件（GLB 与 fallback 同取本表，唯一事实来源）──
  /**
   * 机动车信号灯（竖式三色）0.35 × 5.50 × 0.53。进深 0.53 = 灯箱 0.25
   * + 半筒遮光罩前伸 0.25 + LED 环面 0.03。LED 按材质名 `LEDRed`/`LEDYellow`/
   * `LEDGreen` 分组，前端按相位对三者分别调制 emissive（同一时刻只亮一色）。
   */
  trafficSignal: { x: 0.35, y: 5.5, z: 0.53, minY: 0 },
  /**
   * 行人信号灯 0.35 × 2.50 × 0.28（直立件）。含方盘灯箱 + U 形遮光罩 + 红人/绿人
   * 双色剪影 + 倒计时屏 + 侧挂黄色过街按钮盒。LED 材质名 `LEDRed`/`LEDGreen`，
   * 与机动车灯反相耦合（机动车绿 ⇒ 行人红）。
   */
  pedestrianSignal: { x: 0.35, y: 2.5, z: 0.28, minY: 0 },
  /**
   * 悬臂式信号灯 4.38 × 6.32 × 0.59。x = 混凝土基础 0.40 + 悬臂 4.0 沿 X 居中；
   * y = 基础 0.20 + 立柱 6.0 + 顶帽 0.12。柱高 > 悬臂长是真实形态（主包围轴 = Y）。
   */
  mastArmSignal: { x: 4.38, y: 6.32, z: 0.59, minY: 0 },

  // ── 批次 45 公园设施五件（表值 = GLB 实测真值，见 3d_script/verify_glb_aabb）──
  /**
   * 六角亭（单檐攒尖，仿古）6.11 × 5.16 × 5.34 m。x = 对角 4.16 + 2×角梁外挑 0.95；
   * z = 对边向（攒尖脊线对齐柱位后）；y = 台基 0.40 + 柱 2.80 + 两段攒尖 1.45 + 宝顶。
   * 入口台阶朝导出 -Z（Blender +Y 面）。GLB `civic/park_pavilion` + ParkExtras fallback。
   */
  parkPavilion: { x: 6.11, y: 5.16, z: 5.34, minY: 0 },
  /**
   * 儿童游乐组合（滑梯 + 秋千）4.64 × 2.50 × 2.65 m。长轴 X：爬梯→平台(1.2 m)→
   * 32° 滑道→出料段；秋千架（梁高 2.26 m 双摆位）在 +Z 侧 1.35 m。GB/T 27689 档。
   */
  parkPlayground: { x: 4.64, y: 2.5, z: 2.65, minY: 0 },
  /**
   * 健身三件套（双位太空漫步机 + 扭腰器 + 单杠）3.97 × 2.12 × 1.40 m。
   * GB 19272 器材绿涂装；柱脚带地脚法兰；三件沿 X 一字排开（跌落间距 ≥1.2 m）。
   */
  parkFitness: { x: 3.97, y: 2.12, z: 1.4, minY: 0 },
  /**
   * 公园长椅（三人位防腐木 + 铸铝弓形脚）1.80 × 0.89 × 0.48 m。
   * GB 3326-1997：座高 0.43 / 座深 0.41（5 板条）/ 座面 6° 后倾 / 靠背 103°。
   * 座面向 +Z（面朝方向）。
   */
  parkBench: { x: 1.8, y: 0.89, z: 0.48, minY: 0 },
  /**
   * 庭院灯（单头方灯罩）0.38 × 3.21 × 0.38 m（厂家档 2.5~4.0 m 取中低档）。
   * 灯罩材质名固定 `ParkLamp_Lantern_Mat` —— 前端按名调制夜间 emissive。
   */
  parkLamp: { x: 0.38, y: 3.21, z: 0.38, minY: 0 },

  // ── 车辆（长 X × 高 Y × 宽 Z）──
  sedan: { x: 4.6, y: 1.45, z: 1.82, minY: 0 },
  taxi: { x: 4.7, y: 1.5, z: 1.85, minY: 0 },
  bus: { x: 12.0, y: 3.2, z: 2.55, minY: 0 },
  truck: { x: 8.5, y: 3.4, z: 2.5, minY: 0 },

  // ── 角色（与 18-架构设计 §4.1 一致：宽 × 高 × 深）──
  pedestrian: { x: 0.55, y: 1.67, z: 0.35, minY: 0 },

  // ── 植被 ──
  /** 阔叶树 · 夏 / 春 / 秋（oak_tree / _spring / _autumn，三变体同尺寸，实测 = 表值 ✓）。 */
  oakTree: { x: 4.5, y: 10.1, z: 4.7, minY: 0 },
  /**
   * 阔叶树 · 冬（oak_tree_winter.glb）—— **冬季落叶，冠幅本就更细**，故单列一行
   * （实测资产真值 0.412 × 0.955 × 0.382，节点 identity，不在美术重导清单内）。
   * 消费端 = EastForest（按 season 切 url/目标/fallback 归一化系数）。
   */
  oakTreeWinter: { x: 4.12, y: 9.55, z: 3.82, minY: 0 },
  /**
   * 针叶树（黑松/雪松）：**3.98 × 7.00 × 3.95 m**。
   * 批次 44 重制 `pine_tree.glb`（原 3 锥 70 面玩具树）后，表值由「资产现值
   * 3.4×5.4×3.4」改为**真实值** —— DB11/T 211—2017 雪松 6.0~8.0 m 株高档，
   * 冠幅该档 ≥5.0(Ⅰ)/≥4.0(Ⅱ) m。原注释「本批不重导，故表值 = 资产现值」的
   * 遗留自认于本批作废（§27.3-7 三侧同步：EastForest / NorthMountains 的
   * sizeTarget 走 `sizeTargetFor` 自动跟随，`proceduralFlora` 的 PINE_KX/KY 需手改）。
   */
  pineTree: { x: 3.98, y: 7.0, z: 3.95, minY: 0 },
  cactus: { x: 1.7, y: 2.9, z: 0.7, minY: 0 },
  /**
   * 棕榈（老人葵 Trachycarpus fortunei）：2.76 × 5.42 × 2.81 m，净干高 3.6 m。
   * 批次 44 新增，用于补齐 `TREE_VARIANTS` 里声明了却**零资产**的 `'palm'` 变体（T4）。
   * 选棕榈而非蒲葵：棕榈耐 −15 ℃（长江流域可露地越冬），蒲葵仅短期耐 −5 ℃
   * 「寒地多作盆栽」。冠幅取工程苗 2.5~3.0 m 档。
   */
  palmTree: { x: 2.76, y: 5.42, z: 2.81, minY: 0 },

  // ── 市政建筑（占地宽 X × 总高 Y × 进深 Z；不含门廊/雨棚外挑）──
  cityHall: { x: 14.4, y: 20.0, z: 8.4, minY: 0 },
  /** 通讯塔：x/z 由**塔基垫脚外缘**定义（3d_script/build_comm_tower.py FOOT=0.50 ⇒ 10 m）
   *  —— GLB 实测 1.000 × 2.050 × 1.000 ✓；fallback 已按同口径重建（垫脚 ±5 m + 4 柱 8.8 m 跨）。 */
  commTower: { x: 10.0, y: 20.5, z: 10.0, minY: 0 },
  waterTower: { x: 3.6, y: 13.3, z: 3.6, minY: 0 },
  policeStation: { x: 12.1, y: 6.6, z: 7.0, minY: 0 },
  fireStation: { x: 18.1, y: 8.5, z: 8.0, minY: 0 },

  // ── 批次 46 城市公用设施（表值 = 3d_script/verify_glb_aabb.py 实测真值）──
  /**
   * 城区 10/35kV 配电站 18.34 × 12.41 × 19.67 m。
   * x = 围墙外轮廓（18.0 + 压顶外挑 0.34）；z = 围墙半深 7.17 + 出线电杆位
   * 9.65 + 引下线段 2.85；y = 电杆全高（杆脚墩 0.40 + 杆 12.0 + 渐收帽）。
   * 依据 GB 50059-2011 §2.0.5（实体围墙 ≥2.2 m，实测 2.45 m）+ GB 50053-2013
   * §4.2.2（变压器外廓距围栏 ≥0.8 m / 底部距地 ≥0.3 m）。GLB `civic/substation`。
   */
  substation: { x: 18.34, y: 12.41, z: 19.67, minY: 0 },
  /**
   * 加油站 25.93 × 9.40 × 20.07 m。
   * x = 形象牌外缘 14.72 ~ 罩棚右缘 11.21；y = 形象牌灯箱顶（杆 9.0 + 灯箱 2.6/2
   * + 边框 0.10）；z = 罩棚天沟 -7.26 ~ 站房屋面 12.81。
   * 罩棚投影 22×14 = 308 ㎡，檐口下**有效净高 4.70 m**（GB 50156-2012 要求
   * 不小于 4.5 m）。GLB `civic/gas_station`。
   */
  gasStation: { x: 25.93, y: 9.4, z: 20.07, minY: 0 },
  /**
   * 直升机停机坪 ⌀28.20 × 4.09 m。
   * TLOF 标称直径 28.0 m（中型机位，ICAO Annex 14 Vol.II：按旋翼直径 1.5 倍）；
   * y = 坪面标高 0.49 + 风向袋杆 3.60。
   * GLB `civic/heli_pad`。
   */
  heliPad: { x: 28.2, y: 4.09, z: 28.2, minY: 0 },

  // ── 批次 47 体育场（表值 = 3d_script/verify_glb_aabb.py 实测真值）────────
  /**
   * 200 m 半圆式田径场 110.0 × 15.58 × 74.0 m（长轴沿 X = 场长向）。
   * 跑道本体（GB/T 跑道通用参数）= r=20.00 m + 6 道×1.22 m ⇒ 外半径 27.32 m、
   * 单侧直道 37.17 m（闭式 2×37.17 + 2π×20.00 = 200.00 m）、外接 91.8×54.6 m；
   * 场地 110×74 m 容下跑道外安全区 3 m、端部看台 2 排、主看台 5 排 + 罩棚、4.0 m
   * 消防车道与 4.0 m 围网。内场 74.34×40 m 放七人制人造草 60×32 m
   * （200 m 场放不下 11 人制 105×68 m —— 这是场地规格的硬约束）。
   * Y = 灯杆 15.58 m（湖南省社会足球场地技术标准：11 人制 ≥15 m）。
   * GLB `civic/sports_field`；布点由 `cityObstacles.SPORTS_FIELD_AREA` 保留地锁定。
   */
  sportsField: { x: 110.0, y: 15.58, z: 74.0, minY: 0 },

  // ── 批次 48 港口码头（表值 = 3d_script/verify_glb_aabb.py 实测真值）────────
  /**
   * 内河支线集装箱码头 81.6 × 34.6 × 64.0 m。
   * x = 泊位长（80 m 泊位 + 护舷外挑 1.6 m）；
   * y = 岸桥 A 字架顶 34.6 m（轨下起升 26 m + A 字架 8.6 m）；
   * z = **陆侧堆场后沿 → 悬臂罩住水面**：码头面进深 34 m + 岸桥外伸 26 m + 后伸 12 m。
   * 依据 DB36/T 1833-2023（轨距 16 m）与岸桥通用参数；40 ft 箱 12.19×2.44×2.59 m
   * （ISO 668）—— 批次 18-AA 的 6×2.6×2.4 m 既不是 40 ft 也不是 20 ft。
   * GLB `civic/port_terminal`；陆侧用地由 `cityObstacles.PORT_TERMINAL_AREA` 锁定。
   */
  portTerminal: { x: 81.6, y: 34.6, z: 64.0, minY: 0 },

  // ── 批次 49 高架铁路（表值 = verify_glb_aabb.py 实测真值）────────────────
  /**
   * 30.5 m 标准跨模块 32.89 × 9.00 × 17.90 m（GLB `civic/rail_span`）。
   * x = 30.5 m 跨 + 承台纵向半宽外挑（32.89）；y = 接触网门架顶 17.90 m
   *     （桥面 11.36 + 接触网 6.54）；z = **盖梁横向总宽 10.45 m** ——
   *     桥面本身 9.00 m，盖梁按圆端形墩惯例做到 10.40 m 并带端头斜托，
   *     故整件最宽处是盖梁而不是桥面（与体育场/码头同类：包围盒 ≠ 主轮廓）。
   * 依据 GB/T 51234-2017：标准跨 25~30 m、单箱单室箱梁梁高 2.0 m、
   * 桥面 8.65~9.0 m、轨距 1435 mm、线间距 3.6 m、桥下净空 ≥4.5 m（本件 7.80 m）。
   * ⚠ 30.5 m 是「桥墩离干道路缘 ≥3.1 m」那个净距数值解的**唯一可行跨距**
   *   （设计 49 §1.2），不是凑出来的整数。
   * ⚠ **实例化资产**：消费端 `RailViaduct.tsx` 用 `GlbInstanced` 放 12 个实例，
   *   本表描述的是**单件模块**，不是 610 m 走廊总长。
   */
  railSpan: { x: 32.89, y: 17.90, z: 10.45, minY: 0 },

  /**
   * 120 m 侧式站台车站 120.24 × 25.20 × 23.60 m（GLB `civic/rail_station`）。
   * x = 站台长 120 m；y = 出入口塔顶 23.60 m（站台面 12.94 + 站厅 10.66）；
   * z = 含出入口塔挑出侧的横向总宽 25.20 m（桥面加宽到 14.4 m + 塔挑出 10.8 m）。
   * 站台面高出轨顶 1.10 m（GB 50157-2013 的 1.05~1.25 m 区间）。
   * 本件**自持结构**（站区箱梁 + 加宽盖梁 + 3 座加大承台），故
   * `RailViaduct.tsx` 会跳过其覆盖的 4 个标准跨模块。
   */
  railStation: { x: 120.24, y: 23.60, z: 25.20, minY: 0 },

  // ── 批次 50 施工工地（表值 = verify_glb_aabb.py 实测真值）────────────────
  /**
   * 城市施工工地 66.4 × 44.2 × 45.6 m（GLB `civic/construction_site`）。
   * y = 塔帽顶 **44.2 m**（承台 1.2 + 塔身 14×2.5 = 35.0 + 塔帽 8.0）——
   *     这是全城最高的构筑物（对比市政厅 20 m / 通信塔 20.5 m），但真实且必需：
   *     QTZ80 独立式总高本就约 55.9 m，臂根 41.8 m 远高于沿线 15~25 m 建筑。
   * x = 66.4 m **不是场坪宽** —— 场坪 50 m，**起重臂 50 m 从回转中心（场坪内
   *     偏西 9 m）探出 16 m 到相邻街区**。塔机越场界回转是它的本职。
   * z = 45.6 m（场坪 44 m + 围挡 2.5 m 高向的投影 + 臂/配重的横向外挑）。
   * 依据方圆 QTZ80(TC6010) + TC5015 臂长档：标准节 1.8×1.8×2.5 m、
   * 起重臂 50 m（三���桁架桁高 1.7 m）、平衡臂 13 m、配重 11.75~18 t（6 块）、
   * 承台 4.0×4.0×1.2 m。
   * 落位 (30.5, −1.5) 是净距数值解：原 (28, −1) 的围挡压在 `arterial-z26`
   * 骨干主路里 4.0 m，新落点离路缘 13 m。
   */
  constructionSite: { x: 66.4, y: 44.2, z: 45.6, minY: 0 },
  // ── 批次 53 建筑屋顶 ──
  /**
   * 屋顶设备机组（GLB `civic/rooftop_plant`，`3d_script/build_rooftop_plant.py`）：
   * **8.37 (X) × 4.19 (Y = 高) × 5.47 (Z = 南北进深) m**。
   * 三段沿 X 自西向东：水箱（⌀2.20×2.60，4 支腿 0.50）/ 冷却塔（3.80×2.60×3.72
   * 塔体 + ⌀2.00 风机罩，支腿 0.40）+ 空调外机 2 台（0.90×0.30×0.70）。
   * Z 向 5.47 m 是**冷却塔检修平台**（比塔体 +Y 侧多出 0.80 m 走道）撑出来的，
   * 不是设备本体进深 —— 布置时按平台尺寸判屋面够不够，不要按塔体判。
   * 最高点 = 塔顶风机罩 4.12 m（支腿 0.40 + 样本整塔高 3.72）。
   *
   * ⚠ **落位判据**：本件只放在屋面 ≥ 9.2 × 6.3 m（含女儿墙内缩）的平屋面上；
   * 坡屋顶（house）与窄体量一律跳过 —— 设备绝对不允许悬空出挑。
   */
  roofPlant: { x: 8.37, y: 4.19, z: 5.47, minY: 0 },
} as const satisfies Record<string, RealDimsM>;

/**
 * 真实米制**间距**表（单位 = 米；批次 30 A1）。
 *
 * 与 REAL_DIMS_M 平行：尺寸表管"物件多大"，本表管"物件摆多密"。
 * 消费端经 `spacing(key)` 换算世界单位；禁止组件内硬编码间距魔数。
 */
export const REAL_SPACING_M = {
  /** 主干道路灯间距（单侧）；两侧交替布置 ⇒ 同侧实际 2×。 */
  lampMain: 37,
  /** 次干道路灯间距（单侧；两侧交替）。 */
  lampSide: 50,
  /** 行道树株距（两侧交替 ⇒ 同侧 2×；真实行道树 8~10 m，取 9）。 */
  streetTree: 9,
  /** 路侧垃圾桶沿线间距（两侧交替 ⇒ 单侧 120 m；公交站台旁另补 1 个）。 */
  trashCan: 60,
  /** 公交站台间距（主干道沿线）。 */
  busStop: 500,
  /** 停车咪表沿路缘间距（两侧交替 ⇒ 单侧 16 m；批次 42 C1）。 */
  parkingMeter: 8,
} as const;

export type RealSpacingKey = keyof typeof REAL_SPACING_M;

/** 间距表值 → 世界单位。 */
export function spacing(key: RealSpacingKey): number {
  return u(REAL_SPACING_M[key]);
}

/** 尺寸表键（= 虚拟城市 3D 物件类别）。 */
export type RealDimKey = keyof typeof REAL_DIMS_M;

/**
 * 取表值的**世界单位**副本（逐字段 u() 换算；字段集与表条目一致）。
 * 例：`worldDims('sedan').x` = 0.46（4.6 m 车长）。
 */
export function worldDims<K extends RealDimKey>(
  key: K,
): { [A in keyof (typeof REAL_DIMS_M)[K]]: number } {
  const src = REAL_DIMS_M[key] as Record<string, number>;
  const out: Record<string, number> = {};
  for (const k of Object.keys(src)) out[k] = u(src[k]);
  return out as { [A in keyof (typeof REAL_DIMS_M)[K]]: number };
}

/**
 * 轴向约定（engine3d/glbSizeGuard 判据 ⑤ 直立 / ⑥ 向前轴）—— **按表键的唯一事实来源**。
 *
 * 与 REAL_DIMS_M 同源同理：轴向是**模型的属性**（资产轴 ↔ 消费端 yaw 公式的契约），
 * 与具体调用点无关，故在此集中声明，`sizeTargetFor()` 自动带上（调用点无需改）。
 *
 * ⚠️ 声明纪律（**只声明能确证的**，判据是「尺寸主导性」，无容差）：
 *   - `upright: true` 只在**该物件确实 Y 主导**（Y 尺寸 > X 且 > Z）时成立。扁平物件
 *     （车辆 长>宽>高；低层站房 宽>高）主导轴**本就不是 Y** ⇒ **一律不得声明**：
 *     声明即"正确资产被判侧躺"的反向误报，而真正的"车尾立起"（长轴竖直）反倒能通过。
 *     这类物件的轴向错误由 engine3d 判据 ① 兜住（三轴目标互异 ⇒ 任意 90° 轴置换都会
 *     让某轴偏差超 ±5%）。
 *   - `forward` 只在**该物件沿行进方向拉长**时成立（车辆长轴 = 行进轴）。行人不沿
 *     行进轴拉长（宽 0.55 > 深 0.35）⇒ 不声明；其「+Z 向前」约定由消费端
 *     `PedestrianV3.tsx` 的 `atan2(dx, dz)` 唯一承载，包围盒无法自证。
 */
export interface ModelAxisConvention {
  /** ⑤ 直立：竖直轴 = +Y（Y 为主导轴）。缺省 = 不校验。 */
  upright?: boolean;
  /** ⑥ 向前轴：水平长轴 = 消费端 yaw 公式假设的向前轴。缺省 = 不校验。 */
  forward?: NonNullable<ModelSizeTarget['forward']>;
}

export const MODEL_AXIS_CONVENTIONS: Readonly<Partial<Record<RealDimKey, ModelAxisConvention>>> = {
  // ── 车辆（长轴 = +X）──
  // `Vehicle.tsx` 的 yaw = `atan2(-dz, dx)`（+X 对齐行进方向）⇒ 声明 forward:'x'。
  // **不声明 upright**：轿车 4.6×1.45×1.82 / 公交 12×3.2×2.55 等 Y 都是最小轴，
  // 声明即反向误报（见 ModelAxisConvention 注释）。
  sedan: { forward: 'x' },
  taxi: { forward: 'x' },
  bus: { forward: 'x' },
  truck: { forward: 'x' },

  // ── 角色 ──
  // 行人 Y 主导（0.55×1.67×0.35）⇒ upright 成立；
  // **不声明 forward**：行人宽 0.55 > 深 0.35，不沿行进轴拉长，
  // 其 +Z 向前由 `PedestrianV3.tsx` 的 `atan2(dx, dz)` 承载（与车辆约定不同、各自自洽）。
  pedestrian: { upright: true },

  // ── 街具 ──
  // ⚠️ 量测口径 = `RoadsideBins.tsx` 传的 `measureNode: 'TrashCan_Green'`（单桶
  // 0.5×1.0×0.5 ⇒ Y 主导 ✓）。若哪天改为量测整棵场景（Green+Blue 两变体沿 X 排布
  // ⇒ 1.7×1.0×0.5，X 主导），本声明会误报 —— 届时须同时改 measureNode 与本节。
  trashCan: { upright: true },
  // 批次 42 街具：直立件（Y 主导 1.40>0.60 / 2.65>0.62 / 1.38>0.32）声明 upright。
  // busStop（5.0×2.7×1.8）与 bikeRack（1.8×1.1×0.62）长边 X 沿道路向
  // ⇒ **不声明 upright**（声明即"正确资产被判侧躺"的反向误报，见 ModelAxisConvention）。
  mailbox: { upright: true },
  streetSign: { upright: true },
  parkingMeter: { upright: true },
  // 批次 43 交通信号灯：trafficSignal（0.35×5.50×0.53）与 pedestrianSignal
  // （0.35×2.50×0.28）Y 主导 ⇒ 声明 upright；mastArmSignal 柱高 6.32 > 悬臂 4.38，
  // 主包围轴仍是 Y ⇒ 同样声明 upright。
  trafficSignal: { upright: true },
  pedestrianSignal: { upright: true },
  mastArmSignal: { upright: true },

  // ── 批次 45 公园设施 ──
  // parkLamp（0.38×3.21×0.38）Y 主导 ⇒ 声明 upright。
  parkLamp: { upright: true },
  // pavilion（6.11×5.16×5.34，屋面出檐主导水平包围）/ playground（4.64 长轴 X）/
  // fitness（3.97 一字排开）/ bench（1.80 长轴 X）—— 主包围轴本就不是 Y
  // ⇒ 不声明 upright（声明即反向误报，见 ModelAxisConvention 注释；
  // 轴向错误由判据 ① 三轴互异兜住，同 busStop/bikeRack 先例）。

  // ── 市政建筑 ──
  // 仅声明 **Y 主导** 的塔/高层楼；`policeStation`(12.1×6.6×7.0) 与
  // `fireStation`(18.1×8.5×8.0) 是低层站房（宽 > 高）⇒ 不声明 upright（轴向错误由 ① 兜住）。
  cityHall: { upright: true },
  commTower: { upright: true },
  waterTower: { upright: true },
  // 批次 46 城市公用设施三件：主包围轴**均落在水平轴**（变电站 18.3×19.7 的平面
  // 院落、加油站 25.9×20.1 的大跨罩棚、停机坪 ⌀28.2 圆台）—— 声明 upright 会
  // 反向误报（判据 ⑤ 要求 Y 主导）。同 policeStation/fireStation 先例：**不声明**，
  // 轴向错误由「三轴互异 + Y 尺寸判据」兜住，直立性由 minY=0 承担。
  // 批次 47 体育场：110×15.58×74 的平面场馆，长轴 X 主导 ⇒ 同 policeStation/
  // fireStation 先例，**不登记条目**（空对象无意义，只会让人误以为已声明）。
  // 批次 48 港口码头：81.6×34.6×64 的泊位 + 岸桥，长轴 X 主导 ⇒ 同上不登记。
  // 批次 53 屋顶设备机组：8.37×4.19×5.47 的水平设备区（长轴 X 主导）⇒ 同上不登记。

  // ── 植被 ──
  // 橡树四季（前 3 变体同尺寸 4.5×10.1×4.7、冬季 4.12×9.55×3.82）、
  // 城市树三件（法桐 4.96×9.0×4.9 / 黑松 3.98×7.0×3.95 / 棕榈 2.76×5.42×2.81）、
  // 仙人掌 1.7×2.9×0.7 —— 全部 Y 主导（判据 ⑤ 实测 ✓）。
  oakTree: { upright: true },
  oakTreeWinter: { upright: true },
  pineTree: { upright: true },
  cactus: { upright: true },
  // 批次 44：cityScale.ts 此前**没有** streetTree 的轴向声明 ⇒ 城内 1800 株树
  // 零度量衡护栏覆盖（批次 44 T12）。三件城市树一并补齐。
  streetTree: { upright: true },
  palmTree: { upright: true },

  // ── 明文豁免（不声明，理由留档）──
  // 下列 4 项均经 `<Model url={modelUrl(...)}>` **不带 sizeTarget** 直挂
  //（SouthOcean.tsx / NorthMountains.tsx）⇒ 护栏本就看不到它们；此处留档的是
  //「**即便**将来接入校验也不得声明 upright」的理由。
  // streetLight / streetLightSide：原 `road/road_props.glb` 是**多物件合成 GLB**
  //   （批次 31 已删文件，此处留档豁免理由）
  //   （3 盏路灯沿 X 排布 ⇒ 场景盒 31.3×12.0×5.05，主导轴 = X 是"排布"而非"侧躺"）
  //   ⇒ 声明 upright 必然误报；判据 ⑤ 不可用于多物件合成 GLB（勿为迁就改断言语义）。
  // ocean/cargo_ship (68.2×14.2×10.4) · ocean/sailboat：长轴 X 即**正确摆放**
  //   （船体沿航向），同理不声明 upright；且有独立 yaw/浮沉约定，不在本表校验范围。
  // nature/snow_mountain：**地形基准**（非"物件"，3.1×2.55×2.8 三轴同级，主导轴无意义）
  //   ⇒ 不声明 upright。
  // ocean/lighthouse：Y 主导但**无 sizeTargetFor 调用点**（未接入校验），故不入表。
};

/** sizeTargetFor 可选项。 */
export interface SizeTargetOpts {
  /** 日志标签（缺省用表键，建议传 `<category>/<name>` 与 GLB 路径对齐）。 */
  label?: string;
  /** 量测子节点名（合成 GLB 指定单变体，如 road/trash_can 的 `TrashCan_Green`）。 */
  measureNode?: string;
  /** 尺寸相对容差（缺省 engine3d 默认 ±5%）。 */
  tol?: number;
  /** ⑤ 直立断言覆盖（缺省取 `MODEL_AXIS_CONVENTIONS[key].upright`；`false` = 强制关闭）。 */
  upright?: boolean;
  /** ⑥ 向前轴断言覆盖（缺省取 `MODEL_AXIS_CONVENTIONS[key].forward`）。 */
  forward?: NonNullable<ModelSizeTarget['forward']>;
}

/** 声明层自校验的入参（缺省 = 取表内声明）。 */
export interface AxisConventionDecl {
  /** ⑤ 直立声明。 */
  upright?: boolean;
  /** ⑥ 向前轴声明。 */
  forward?: NonNullable<ModelSizeTarget['forward']>;
}

const AXIS_DECL_MOTIVE =
  '\n  → 本校验的动机（批次 29 收尾实况）：主 Agent 按「直觉物理」给车辆写声明，' +
  '若非实现方按纪律拒绝，会在 dev 控制台留下 7 条误报 —— 「狼来了」噪声会掩盖真问题。' +
  '这类错误的本质是「**声明与表值自相矛盾**」，不需要模型即可判定。';

/**
 * **声明层自校验**（纯函数，不依赖 GLB）：声明与 `REAL_DIMS_M` 表值是否自洽。
 *
 *   - `upright: true` ⇒ 要求表值 **y 严格最大**（`x ≥ y` 或 `z ≥ y` 即矛盾）；
 *   - `forward: 'x'` ⇒ 要求表值 `x > z`；`forward: 'z'` ⇒ 要求 `z > x`；
 *   - **缺对应表值字段即跳过**（如只填 y 而声明 upright），不误报。
 *
 * 返回告警文案（`null` = 自洽）。由 `sizeTargetFor()` 在 dev 态**每份唯一声明各调一次**
 * （引用缓存保证不随模型/渲染重复）。
 */
export function axisConventionIssue(
  key: RealDimKey,
  decl: AxisConventionDecl = MODEL_AXIS_CONVENTIONS[key] ?? {},
): string | null {
  const m: RealDimsM = REAL_DIMS_M[key];
  const fmt = (v: number | undefined): string => (v === undefined ? '—' : v.toFixed(2));
  if (decl.upright && m.x !== undefined && m.y !== undefined && m.z !== undefined) {
    if (!(m.y > m.x && m.y > m.z)) {
      return (
        '[cityScale] 声明自相矛盾：`upright` 要求竖直轴为 Y，' +
        `但 REAL_DIMS_M.${key} 的 y(${fmt(m.y)}) 不是最大轴（x=${fmt(m.x)}, z=${fmt(m.z)}）` +
        '—— 扁平物件（车辆 / 低层站房）**不得**声明 `upright`，其轴向由 engine3d 判据 ① 兜住' +
        '（三轴目标两两互异 ⇒ 任意 90° 轴置换都会让某轴偏差超 ±5%）。' +
        '（反向失效：扁平物件"长轴竖直"反而能通过该断言。）' +
        AXIS_DECL_MOTIVE
      );
    }
  }
  if (decl.forward && m.x !== undefined && m.z !== undefined) {
    const want = decl.forward;
    const pair: [string, number, string, number] =
      want === 'x' ? ['x', m.x, 'z', m.z] : ['z', m.z, 'x', m.x];
    const [wl, wv, sl, sv] = pair;
    if (!(wv > sv)) {
      return (
        `[cityScale] 声明自相矛盾：\`forward='${want}'\` 要求水平长轴为 ${wl.toUpperCase()}，` +
        `但 REAL_DIMS_M.${key} 的 ${wl}(${fmt(wv)}) ≤ ${sl}(${fmt(sv)})` +
        '—— `forward` 是「水平长轴」声明，与表值矛盾说明声明写错，或该物件本就不该声明' +
        '（如行人不沿行进轴拉长，"朝向"无法由包围盒自证，由消费端 yaw 公式承载）。' +
        AXIS_DECL_MOTIVE
      );
    }
  }
  return null;
}

/**
 * 表值 → engine3d/glbSizeGuard 的校验目标（世界单位）。
 * **引用缓存**：同 (key,label,measureNode,tol,upright,forward) 恒返回同一对象，可直接作为
 * `useSharedGLTF` / `<Model sizeTarget>` 的稳定 prop（避免每次渲染重跑 effect）。
 * 缓存未命中时（= 每份唯一声明恰好一次）顺带跑**声明层自校验**（dev 态，只告警）。
 */
const SIZE_TARGET_CACHE = new Map<string, ModelSizeTarget>();

export function sizeTargetFor(key: RealDimKey, opts?: SizeTargetOpts): ModelSizeTarget {
  const conv: ModelAxisConvention | undefined = MODEL_AXIS_CONVENTIONS[key];
  const upright = opts?.upright ?? conv?.upright;
  const forward = opts?.forward ?? conv?.forward;
  const cacheKey =
    `${key}|${opts?.label ?? ''}|${opts?.measureNode ?? ''}|${opts?.tol ?? ''}` +
    `|${upright ?? ''}|${forward ?? ''}`;
  const hit = SIZE_TARGET_CACHE.get(cacheKey);
  if (hit) return hit;
  if (import.meta.env.DEV) {
    const issue = axisConventionIssue(key, { upright, forward });
    if (issue) console.warn(issue);
  }
  const m: RealDimsM = REAL_DIMS_M[key];
  const target: ModelSizeTarget = { label: opts?.label ?? key };
  if (m.x !== undefined) target.x = u(m.x);
  if (m.y !== undefined) target.y = u(m.y);
  if (m.z !== undefined) target.z = u(m.z);
  if (m.minY !== undefined) target.minY = u(m.minY);
  if (opts?.measureNode) target.measureNode = opts.measureNode;
  if (opts?.tol !== undefined) target.tol = opts.tol;
  if (upright) target.upright = true;
  if (forward) target.forward = forward;
  SIZE_TARGET_CACHE.set(cacheKey, target);
  return target;
}

/** 各城区楼层区间 [minF, maxF]（真实城市形态：CBD 摩天 / 郊区别墅；
 *  v2.12 阶段 2 追加 8 个新城区的楼层区间，前 8 区不可修改）。 */
export const DISTRICT_FLOORS: Record<VirtualCityDistrictId, [number, number]> = {
  finance:     [8, 20],   // 金融CBD   24~60 m
  tech:        [6, 15],   // 科技园    18~45 m
  industry:    [2, 6],    // 工业区     6~18 m（厂房高顶棚）
  oldtown:     [2, 7],    // 老城区     6~21 m（多层老公房）
  commerce:    [4, 12],   // 商业中心  12~36 m
  residential: [6, 18],   // 居住区    18~54 m（高层住宅）
  suburb:      [1, 3],    // 郊区       3~9 m（别墅/排屋）
  riverside:   [8, 20],   // 滨河新区  24~60 m
  // ── v2.12 阶段 2 扩展城区（80×80 地图外圈）──
  logistics_port:    [3, 8],   // 物流港     9~24 m（低层仓库/厂房）
  hightech_park:     [6, 15],  // 高新园区  18~45 m（科技办公楼）
  edu_district:      [4, 10],  // 教育园区  12~30 m（教学楼/宿舍）
  medical_city:      [8, 18],  // 医疗城    24~54 m（医院大楼）
  industrial_park:   [2, 5],   // 产业基地   6~15 m（厂房）
  central_park:      [1, 3],   // 中央公园   3~9 m（低层景观建筑）
  transport_hub:     [3, 12],  // 交通枢纽   9~36 m（车站/商业）
  cultural_creative: [3, 8],   // 文创区     9~24 m（创意园区）
  // ── 批次 20 城市扩张新增 16 区（楼层区间 = 文档 1 §2.3 表）──
  fin_sub_center:    [10, 24], // 金融副中心 30~72 m（CBD 外溢塔楼群）
  software_park:     [5, 14],  // 软件园    15~42 m（板楼办公）
  airport_town:      [3, 10],  // 空港小镇   9~30 m（临空商业）
  air_logistics:     [2, 6],   // 航空物流园 6~18 m（仓库厂房）
  auto_city:         [2, 7],   // 汽车城     6~21 m（整车制造厂房）
  mountain_resort:   [1, 3],   // 山居民宿区 3~9 m（低层度假）
  chem_park:         [2, 6],   // 化工园区   6~18 m（重工业）
  agri_park:         [1, 4],   // 现代农业园 3~12 m（大棚/农舍）
  health_town:       [2, 6],   // 康养小镇   6~18 m（疗养低层）
  steel_town:        [2, 6],   // 特钢镇     6~18 m（厂区宿舍）
  old_city_culture:  [1, 4],   // 古城文化区 3~12 m（坡顶院落）
  university_town:   [4, 12],  // 大学城    12~36 m（校园板楼）
  wetland_park:      [1, 3],   // 湿地公园   3~9 m（pavilion 景观）
  sports_new_city:   [3, 14],  // 体育新城   9~42 m（场馆商业）
  bay_new_town:      [8, 20],  // 湾区新城  24~60 m（高层滨水）
  highspeed_rail_town: [3, 12],// 高铁新城   9~36 m（TOD 综合体）
};

/**
 * 楼层数 → 世界单位楼高。
 * 批次 39 C2：消费方 `buildingTopY` 恒传**整数**层数 ⇒ 楼高恒为 3 m 整数倍
 * （立面开间贴图 12 m = 4 层、楼层线自 y=0 起 ⇒ 楼层线对齐）。
 */
export const buildingHeight = (floors: number): number => u(FLOOR_HEIGHT_M) * floors;

/**
 * 批次 32：房价指数（0.8–1.6）→ prosperity（0–1）。
 *
 * **单一事实来源**：`DistrictBlock` 渲染与 `freeViewColliders` 相机碰撞体装配
 * 必须共用此函数，否则改了渲染公式而漏改碰撞公式 ⇒ 相机穿楼 / 被空气挡住
 * （CLAUDE.md §130「声明了却从不接线」在数据侧的等价形态）。
 */
export const prosperityOf = (priceIndex: number): number =>
  Math.min(1, Math.max(0, (priceIndex - 0.8) / 0.8));

/**
 * 批次 39 C2：`factor`（楼高布局系数）的取值区间 —— **布局与楼高共用的唯一事实来源**。
 *
 * `building_layout.buildingsFor` 按幂律 `MIN + (MAX-MIN)·rnd()^2.2` 在此区间采样
 * （多数楼偏低、少数楼拔高），`buildingTopY` 在同一区间归一化。两侧引用本常量，
 * 任何一侧改区间都不会与另一侧漂移（CLAUDE.md §130）。
 */
export const BUILDING_FACTOR_MIN = 0.55;
export const BUILDING_FACTOR_MAX = 1.0;

/**
 * 批次 39 C2：楼高离散度 —— 单栋楼相对**标称层数**的最大偏移占 `maxF - minF` 的比例。
 *
 * 现状（批次 39 之前）`0.85 + factor*0.15` 只贡献 0.94~1.00 ⇒ 同区 11 栋楼高差
 * ≤ 6%，天际线齐平（B7）。改为**层数增量**口径后，同区楼高按 factor 双向拉开
 * `±(maxF-minF) × FLOOR_SPREAD` 层：finance（8~20 层，跨度 12）⇒ ±3 层，
 * 最高最低差 6 层 = 18 m，肉眼可见的天际线。
 */
export const FLOOR_SPREAD = 0.25;

const clamp01 = (v: number): number => (v < 0 ? 0 : v > 1 ? 1 : v);

/**
 * 批次 39 C2：**单栋楼的层数**（整数，clamp 到 `DISTRICT_FLOORS` 的 [minF, maxF]）。
 *
 * 这是「层数 → 楼高」推导链的**唯一事实来源**：
 *   - `buildingTopY`（渲染 + `freeViewColliders` 相机碰撞体 + `StreetPropsLayer` 屋顶锚点）
 *     内部只调它；
 *   - `DistrictBuildings` 的悬停提示「N 层」也调它 —— 此前那份
 *     `Math.round(minF + (maxF-minF)*prosperity)` **不含 factor**，与真实楼高最多差
 *     `span × FLOOR_SPREAD` 层（finance 满繁荣度 ±3 层），即提示的楼层数与看到的楼对不上。
 *     两处共用本函数即杜绝第二份公式（CLAUDE.md §130）。
 *
 * @param districtId 城区 id（取 DISTRICT_FLOORS 的楼层区间）
 * @param prosperity 0–1，由 `prosperityOf(price_index)` 得到
 * @param factor 布局系数 {@link BUILDING_FACTOR_MIN}–{@link BUILDING_FACTOR_MAX}（`buildingsFor` 幂律采样产出）
 */
export const buildingFloorsOf = (
  districtId: VirtualCityDistrictId,
  prosperity: number,
  factor: number,
): number => {
  const [minF, maxF] = DISTRICT_FLOORS[districtId];
  const span = maxF - minF;
  // 标称层数（浮点）：繁荣度在 [minF, maxF] 区间内插值，语义与批次 32 一致
  const nominal = minF + span * clamp01(prosperity);
  // factor 归一化到 [0,1]（0 = 矮，1 = 高），与布局侧同区间（BUILDING_FACTOR_*）
  const fn = clamp01(
    (factor - BUILDING_FACTOR_MIN) / (BUILDING_FACTOR_MAX - BUILDING_FACTOR_MIN),
  );
  // 双向离散：fn=0 ⇒ 矮 span×FLOOR_SPREAD 层，fn=1 ⇒ 高同样层数；clamp 回楼层带
  return Math.min(
    maxF,
    Math.max(minF, Math.round(nominal + span * FLOOR_SPREAD * (2 * fn - 1))),
  );
};

/**
 * 批次 32：单栋楼的**顶面 y**（世界单位）。
 * 与 `DistrictBuildings` 的渲染楼高公式逐字节同源，碰撞体直接复用。
 *
 * 批次 39 C2（本函数是 B3「楼高对齐层高」的落点）：
 *   - 层数**取整**（经 {@link buildingFloorsOf}）⇒ `buildingTopY ≡ FLOOR_HEIGHT_M ×
 *     整数层数`，立面开间贴图（12 m = 4 层、楼层线自 y=0 起）的楼层线不再永久错位、
 *     顶行窗不再被腰斩；
 *   - 缩放系数由浮点倍率 `0.85 + factor*0.15` 改为**层数增量**（`FLOOR_SPREAD`），
 *     消除「齐平天际线」（B7）。
 */
export const buildingTopY = (
  districtId: VirtualCityDistrictId,
  prosperity: number,
  factor: number,
): number => buildingHeight(buildingFloorsOf(districtId, prosperity, factor));
