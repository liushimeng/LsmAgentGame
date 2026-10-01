/**
 * 虚拟城市 (VirtualCity) 资产索引。
 *
 * 降级策略（前端架构文档 §9 + 08-UI优化/02-架构设计 §5）：所有 PNG 由
 * python-generate-image-tool 子模块的资产生成脚本（虚拟城市定位 v2.0 起持续维护）
 * 并行生成（skip-if-exists 可续跑），前端构建 **不依赖**生成成功 ——
 * import.meta.glob 按构建时存在的文件打包容错，缺失的键返回 ''，组件运行时
 * 用职业色 / emoji / CSS 渐变 / 简化几何 兜底。
 *
 * 文件清单：
 *   banner.png                                          2560×1440
 *   agents/{p01,p03,p05,p07,p08,p09,p10,p11,p15,p16}.png 512×512 透明
 *   districts/{finance,tech,industry,oldtown,commerce,residential,suburb,riverside}.png
 *     1024×1024（城区俯视底板纹理）
 *   goods/{food,clothing,housing,household,transport,education,healthcare,misc}.png
 *     128×128 透明（统计局 CPI 八大类消费品图标，P1 真实经济循环引擎）
 *   facades/<districtId>_{base,mid}.png                  512×1024 透明（P1-B 楼宇贴图）
 *   facade_tiles/<materialFamily>_{base,mid}.png        512×1024 四边无缝可平铺开间贴图
 *     （批次 37 P1：2 开间 × 4 层，物理周期 6 m × 12 m；**stem 为 16 个建筑材质族**
 *      而非 32 个城区 id，城区→材质族映射见 FACADE_TILE_STEM；登记表见
 *      components/virtualCity/texScale.ts::FACADE_TILE。缺失时消费端降级回 facades/）
 *   facade_tiles/<materialFamily>_lit.png               512×1024 亮窗遮罩（批次 39 B1）
 *     （只亮窗玻璃、墙面纯黑；无 PBR 三件套；emissiveMap 唯一来源，见 facadeTileLitUrl）
 *   roofs/<districtId>.png                               512×512  透明
 *   streets/{asphalt_main,asphalt_side,road_main,road_side,sidewalk_main,sidewalk_side,
 *            crosswalk,stopline,arrow_straight}.png
 *     街道铺装贴图（P1-A 道路重做；批次 24 真实马路升级：标线烘焙路面 + 停止线/箭头）
 *   ground/{grass_tile,plaza_tile,water_tile,urban_base}.png      512×512 可平铺（14-3D渲染深化地表环境）
 *   ground/{sand_tile,rock_snow_tile,forest_floor_tile,ocean_tile}.png
 *     1024×1024 无缝平铺（批次 26 四缘环境带：西沙漠/北雪山山脚/东森林/南海洋）
 *   ground/{grass_spring_tile,grass_autumn_tile,snow_cover_tile}.png
 *     1024×1024 无缝平铺（批次 27 季节地表：春嫩绿小花/秋黄褐落叶/冬雪覆）
 *   weather/{rain_drop,snow_flake}.png
 *     256×256 透明底（批次 27 天气粒子 sprite：雨滴/雪花）
 *   props/{streetlamp,tree,vehicle,pedestrian,sign,rooftop}/<variant>_<category>.png
 *     街景道具贴图（P1-C 街道道具层）
 */

const agentImgs = import.meta.glob<string>('./agents/*.png', { eager: true, import: 'default' });
const districtImgs = import.meta.glob<string>('./districts/*.png', { eager: true, import: 'default' });
const goodsImgs = import.meta.glob<string>('./goods/*.png', { eager: true, import: 'default' });
const bannerImgs = import.meta.glob<string>('./banner.png', { eager: true, import: 'default' });
const facadeImgs = import.meta.glob<string>('./facades/*.png', { eager: true, import: 'default' });
const roofImgs = import.meta.glob<string>('./roofs/*.png', { eager: true, import: 'default' });
const streetImgs = import.meta.glob<string>('./streets/*.png', { eager: true, import: 'default' });
const groundImgs = import.meta.glob<string>('./ground/*.png', { eager: true, import: 'default' });
const weatherImgs = import.meta.glob<string>('./weather/*.png', { eager: true, import: 'default' });
const propImgs = import.meta.glob<string>('./props/**/*.png', { eager: true, import: 'default' });

/** 大厅 banner（缺失 = ''，VirtualCityLobbyPage 回落 CSS 渐变）。 */
export const VIRTUAL_CITY_BANNER: string = bannerImgs['./banner.png'] ?? '';

/**
 * 职业头像 URL。id 大小写不敏感（"P01" / "p01" 皆可）；文档池随机 id
 * （如 "N9012345"）无对应 PNG 时返回 ''，AgentToken 回落职业色圆环 + emoji。
 */
export function professionAvatar(id: string): string {
  return agentImgs[`./agents/${id.toLowerCase()}.png`] ?? '';
}

// ── 批次 20 §3.5：贴图别名机制（16 新区零新增贴图，复用旧 stem）──────
import type { VirtualCityDistrictId } from '@/types/virtualCity';

/**
 * 新区 → 旧贴图 stem 别名表（文档 1 §2.3 第 4 列逐字）。
 * 解析规则：贴图/PBR stem 一律先查本表，未命中回落 def.id；
 * 底板纹理 / 立面 / 屋顶 / PBR 法线粗糙度全部同源（districtTextureStem）。
 */
export const DISTRICT_TEXTURE_ALIAS: Partial<Record<VirtualCityDistrictId, string>> = {
  fin_sub_center: 'finance',
  software_park: 'hightech_park',
  airport_town: 'transport_hub',
  air_logistics: 'logistics_port',
  auto_city: 'industrial_park',
  mountain_resort: 'suburb',
  chem_park: 'industrial_park',
  agri_park: 'suburb',
  health_town: 'suburb',
  steel_town: 'industrial_park',
  old_city_culture: 'oldtown',
  university_town: 'edu_district',
  wetland_park: 'central_park',
  sports_new_city: 'transport_hub',
  bay_new_town: 'riverside',
  highspeed_rail_town: 'transport_hub',
};

/** 贴图 stem 解析（先查别名再取原 id；前 16 区恒等）。 */
export function districtTextureStem(id: string): string {
  return DISTRICT_TEXTURE_ALIAS[id as VirtualCityDistrictId] ?? id;
}

/** 城区底板纹理 URL（缺失 = ''，DistrictBlock 回落 DistrictDefs 主色）。 */
export function districtTexture(id: string): string {
  return districtImgs[`./districts/${districtTextureStem(id)}.png`] ?? '';
}

/** CPI 八大类消费品图标 URL（缺失 = ''，组件回落类别主色/emoji）。 */
export function goodsIcon(id: string): string {
  return goodsImgs[`./goods/${id}.png`] ?? '';
}

// ── 08-UI优化 v2 新增（P1-A / P1-B / P1-C）────────────────────

/** 楼宇立面 variant 字面量（与 python-generate-image-tool 子模块立面贴图变体常量对齐）。 */
export type FacadeVariant = 'base' | 'mid';

/**
 * 城区楼宇侧立面贴图 URL（缺失 = ''，BuildingMesh 退回到 DistrictDefs 主色）。
 *
 * @param districtId 城区 id（finance / tech / ... / riverside）
 * @param variant    'base' = 楼栋底层 / 'mid' = 楼栋中上层（循环贴图避免接缝）
 */
export function districtFacadeUrl(districtId: string, variant: FacadeVariant): string {
  return facadeImgs[`./facades/${districtTextureStem(districtId)}_${variant}.png`] ?? '';
}

// ── 批次 37 P1：可平铺立面开间贴图（物理周期 UV 管线）────────────────

const facadeTileImgs = import.meta.glob<string>('./facade_tiles/*.png', {
  eager: true,
  import: 'default',
});

/**
 * 城区 → **立面材质族 stem** 映射（32 城区 → 16 族 × {base,mid} = 32 件）。
 *
 * 为什么去重：开间贴图经 `import.meta.glob(..., { eager: true })` 直接打进 JS bundle，
 * 逐城区出图 64 件在 1024x2048 档下约 314 MB（≈ 现有全量资产 3.5 倍，首屏不可接受）。
 * 建筑立面本就按材质族群分（玻璃幕墙 / 办公板材 / 住宅砖 / 老城面砖 / 厂房金属板…），
 * 故按材质族出图，32 个城区复用 16 张。
 *
 * 归并依据：`DISTRICT_ARCHETYPE`（building_shapes.tsx 导出）+ 城区业态定位 ——
 *   塔楼 → cbd_glass / med_glass / civic_stone；板楼 → office_panel / tech_curtain /
 *   edu_brick / retail_shophouse / transport_glass；住宅 → resi_brick；
 *   老城/文创/古城 → oldtown_tile / culture_wall；厂房 → industrial_metal；
 *   仓库/物流 → warehouse_metal；郊野/农/康养/度假 → rural_white / resort_wood；
 *   公园/湿地 → park_wood。
 *
 * ⚠️ 同步纪律：本表 stem 同时被两端消费 ——
 *   - 前端 `facadeTileUrl` / `facadeTilePbrUrl`（本文件）；
 *   - 美术侧 `python-generate-image-tool/generate_virtual_city_facade_tiles.py` 的 JOBS、
 *     `3d_script/audit_texture_fit.py` 的登记表审计。
 *   **映射一旦变更需三端同步**，否则前端会静默落到降级态 2（0..1 UV，形变仍在）。
 *   缺表项按既有 `DISTRICT_TEXTURE_ALIAS` → 城区 id 两级回落，保证新城区不炸。
 */
export const FACADE_TILE_STEM: Partial<Record<VirtualCityDistrictId, string>> = {
  // ── 塔楼（CBD / 医疗 / 行政新城）──
  finance: 'cbd_glass',
  riverside: 'cbd_glass',
  fin_sub_center: 'cbd_glass',
  medical_city: 'med_glass',
  bay_new_town: 'civic_stone',
  sports_new_city: 'civic_stone',
  // ── 板楼（办公 / 科技幕墙 / 教育 / 商业 / 交通枢纽）──
  tech: 'office_panel',
  highspeed_rail_town: 'office_panel',
  hightech_park: 'tech_curtain',
  software_park: 'tech_curtain',
  edu_district: 'edu_brick',
  university_town: 'edu_brick',
  commerce: 'retail_shophouse',
  transport_hub: 'transport_glass',
  airport_town: 'transport_glass',
  cultural_creative: 'culture_wall',
  // ── 住宅（小区板楼 / 郊区别墅）──
  residential: 'resi_brick',
  suburb: 'resi_brick',
  // ── 老城 / 文保 ──
  oldtown: 'oldtown_tile',
  old_city_culture: 'oldtown_tile',
  // ── 郊野 / 农 / 康养 / 度假 ──
  mountain_resort: 'resort_wood',
  health_town: 'resort_wood',
  agri_park: 'rural_white',
  // ── 工业 / 仓储 ──
  industry: 'industrial_metal',
  industrial_park: 'industrial_metal',
  auto_city: 'industrial_metal',
  chem_park: 'industrial_metal',
  steel_town: 'industrial_metal',
  logistics_port: 'warehouse_metal',
  air_logistics: 'warehouse_metal',
  // ── 公园 / 湿地 ──
  central_park: 'park_wood',
  wetland_park: 'park_wood',
};

/** 贴图 stem 解析（开间贴图专用）：先查材质族表，再回落既有别名表（两级查表）。 */
export function facadeTileStem(districtId: string): string {
  return FACADE_TILE_STEM[districtId as VirtualCityDistrictId] ?? districtTextureStem(districtId);
}

/**
 * 城区立面**开间贴图** URL（缺失 = ''，DistrictBuildings 走三级降级链第 2 级）。
 *
 * 与 `districtFacadeUrl`（整栋立面图，0..1 UV）的区别：
 *   - 本图语义是「2 开间 × 4 层、**四边无缝**」，必须配 `wrap: 'repeat'` +
 *     `engine3d/boxFacesUV` 的物理尺寸 UV 投影（周期 6 m × 12 m，见 texScale.ts）；
 *   - 缺失时**不得**改用本函数拉伸旧图，须回落到 districtFacadeUrl 的 0..1 行为。
 *
 * @param districtId 城区 id（经 `facadeTileStem` 解析材质族，缺表时回落别名表）
 * @param variant    'base' = 楼栋底层 / 'mid' = 楼栋中上层
 */
export function facadeTileUrl(districtId: string, variant: FacadeVariant): string {
  return facadeTileImgs[`./facade_tiles/${facadeTileStem(districtId)}_${variant}.png`] ?? '';
}

/**
 * 城区立面**亮窗遮罩** URL（批次 39 B1；缺失 = '' ⇒ 材质不设 emissiveMap）。
 *
 * 语义（与 `_base/_mid` 开间贴图同周期 6 m × 12 m，但内容不同）：
 *   - 只有**窗玻璃**区域非黑，按确定性随机取「灭 / 暖黄 / 暖白」三档；
 *   - 墙面、窗间墙、贴图里画好的空调外机/晾衣绳一律纯黑。
 *
 * 用途：`MeshStandardMaterial.emissiveMap` 的**唯一**来源。批次 39 之前 emissiveMap
 * 直接挂 albedo（`sideMatProps`）⇒ 整个墙面（含窗间墙）一起发光、且全城同族亮窗分布
 * 完全一致 —— 遮罩化之后才是「只有窗发光」。
 *
 * ⚠ 与 `facadeTilePbrUrl` 的区别：亮窗遮罩**没有 PBR 三件套**（自发光不吃 PBR），
 * 故本图不进 `pbr/` 目录、也不提供 `facadeTileLitPbrUrl`。
 *
 * @param districtId 城区 id（经 `facadeTileStem` 解析材质族，与 `facadeTileUrl` 同一张映射表）
 */
export function facadeTileLitUrl(districtId: string): string {
  return facadeTileImgs[`./facade_tiles/${facadeTileStem(districtId)}_lit.png`] ?? '';
}

/** 城区楼顶贴图 URL（缺失 = ''，BuildingMesh 退回到 DistrictDefs 主色）。 */
export function districtRoofUrl(districtId: string): string {
  return roofImgs[`./roofs/${districtTextureStem(districtId)}.png`] ?? '';
}

/**
 * 街道铺装类型字面量（与 python-generate-image-tool 子模块街道铺装常量对齐）。
 * 批次 24 新增：road_main / road_side（标线烘焙进整幅路面）/ stopline / arrow_straight。
 * centerline 自批次 24 起零运行时引用，批次 31 已连文件一并删除（设计 31 §4.2）。
 */
export type StreetTileName =
  | 'asphalt_main'
  | 'asphalt_side'
  | 'road_main'
  | 'road_side'
  | 'sidewalk_main'
  | 'sidewalk_side'
  | 'crosswalk'
  | 'stopline'
  | 'arrow_straight';

/**
 * 街道铺装贴图 URL（缺失 = ''，Road / Ground 退回到纯色 / 简化几何）。
 * 路面贴图通常配 RepeatWrapping × N 平铺。
 */
export function streetTileUrl(name: StreetTileName): string {
  return streetImgs[`./streets/${name}.png`] ?? '';
}

/** 地表环境贴图字面量（与 3d_script/procedural_city_textures.py::ground 对齐，14-3D渲染深化；
 *  批次 26 追加 sand_tile / rock_snow_tile / forest_floor_tile / ocean_tile，
 *  与 python-generate-image-tool/generate_virtual_city_edge_assets.py::TILES 对齐；
 *  批次 27 追加季节地表 grass_spring_tile / grass_autumn_tile / snow_cover_tile，
 *  与 python-generate-image-tool/generate_virtual_city_season_assets.py::JOBS 对齐
 *  ——键名沿用既有「= 文件 stem（含 _tile 后缀）」惯例）。 */
export type GroundTileName =
  | 'grass_tile'
  | 'plaza_tile'
  | 'water_tile'
  | 'urban_base'
  | 'sand_tile'
  | 'rock_snow_tile'
  | 'forest_floor_tile'
  | 'ocean_tile'
  | 'grass_spring_tile'
  | 'grass_autumn_tile'
  | 'snow_cover_tile'
  // 批次 38 R3 · §4.7(a)：运河水面 / 河床（art-agent 产出；缺失返回 '' 时
  // WaterPlane 自动回落 water_tile —— 既有降级链兜住）。
  | 'canal_tile'
  | 'canal_bed';

/**
 * 地表环境贴图 URL（缺失 = ''，组件回退纯色：grass #3f7a3a / plaza #9aa1ab / water #1a3a52）。
 * 草地/广场/水面均 RepeatWrapping 可平铺；水面沿 v 方向滚动。
 * 批次 27 季节消费：夏 = 既有 grass_tile；春/秋/冬 = grass_spring_tile /
 * grass_autumn_tile / snow_cover_tile（缺失时调用方降级回 grass_tile）。
 */
export function groundTileUrl(name: GroundTileName): string {
  return groundImgs[`./ground/${name}.png`] ?? '';
}

/** 天气粒子 sprite 字面量（与 python-generate-image-tool/
 *  generate_virtual_city_season_assets.py::JOBS 的 sprite 项对齐，批次 27）。 */
export type WeatherSpriteName = 'rain_drop' | 'snow_flake';

/**
 * 天气粒子 sprite URL（256×256 透明底；缺失 = ''，WeatherFX 降级内置几何点/着色）。
 * 消费方：engine3d/WeatherFX（雨=细长条 sprite，雪=柔光点 sprite）。
 */
export function weatherSpriteUrl(name: WeatherSpriteName): string {
  return weatherImgs[`./weather/${name}.png`] ?? '';
}

/** 街景道具类别字面量（与 python-generate-image-tool 子模块道具分类常量对齐）。 */
export type PropCategory = 'streetlamp' | 'tree' | 'vehicle' | 'pedestrian' | 'sign' | 'rooftop';

/**
 * 街景道具贴图 URL（缺失 = ''，对应组件退回到简化几何 / 颜色块）。
 *
 * @param category 类别（streetlamp / tree / vehicle / pedestrian / sign / rooftop）
 * @param variant  变种（a/b/c / oak/pine/palm / sedan/truck/bus/taxi / warm/cool / traffic/info / ac/tank/antenna）
 */
export function propUrl(category: PropCategory, variant: string): string {
  return propImgs[`./props/${category}/${variant}_${category}.png`] ?? '';
}

// ── 15-3D城市全面真实感深化 · 阶段 K/M 新增（V2 道具贴图）─────────

/** V2 行人头部 outfit（与 PedestrianV2.tsx outfit prop 对齐）。 */
export type PedestrianHeadOutfit = 'business' | 'casual' | 'bright' | 'khaki';

/**
 * V2 行人头部 sprite URL（缺失 = ''，PedestrianV2 降级到圆片兜底）。
 * 文件命名约定：props/pedestrian/head_<outfit>.png（与 python-generate-image-tool 子模块行人贴图命名对齐）。
 */
export function pedestrianHeadUrl(outfit: PedestrianHeadOutfit): string {
  return propImgs[`./props/pedestrian/head_${outfit}.png`] ?? '';
}

/** V2 商铺/报刊亭类（与 VendorKiosk.tsx 对齐）。 */
export type VendorName = 'vendor_kiosk';

/** V2 商铺 sprite URL（缺失 = ''，VendorKiosk 降级到纯几何）。 */
export function vendorUrl(name: VendorName): string {
  return propImgs[`./props/vendor/${name}.png`] ?? '';
}

/** V2 扩展标识牌（与 ParkingMeter.tsx 对齐；traffic / info 仍走 propUrl）。 */
export type SignNameExt = 'parking_sign';

/** V2 扩展标识牌 sprite URL（缺失 = ''，ParkingMeter 降级到纯色 box）。 */
export function signUrlExt(name: SignNameExt): string {
  return propImgs[`./props/sign/${name}.png`] ?? '';
}

// ── 16-3D城市WebGL质感与城市补全 · 阶段 U 新增（天空贴图）─────────

const skyImgs = import.meta.glob<string>('./sky/*.png', { eager: true, import: 'default' });

/** 天空贴图字面量（与 3d_script/procedural_city_textures.py::gen_cloud_puff 对齐）。 */
export type SkyName = 'cloud_puff';

/**
 * 天空贴图 URL（缺失 = ''，CloudLayer 降级白色扁球兜底）。
 */
export function skyUrl(name: SkyName): string {
  return skyImgs[`./sky/${name}.png`] ?? '';
}

// ── 18-3D城市PBR材质与真实城市冲刺 · 阶段 X 新增（PBR 贴图）─────────

const pbrImgs = import.meta.glob<string>('./pbr/**/*.png', { eager: true, import: 'default' });

/** 派生 PBR 贴图类别（与 3d_script/procedural_pbr_maps.py::DERIVED_JOBS 对齐）。 */
export type PbrCategory = 'facades' | 'roofs' | 'streets' | 'ground' | 'districts' | 'synth';

/**
 * 法线贴图 URL（缺失 = ''，调用方保持现状材质）。
 * @param category 派生类别；name 为颜色贴图的 stem
 *   例：pbrNormalUrl('facades', 'finance_mid') → ./pbr/facades/finance_mid_n.png
 *       pbrNormalUrl('synth', 'water')         → ./pbr/synth/water_n.png
 */
export function pbrNormalUrl(category: PbrCategory, name: string): string {
  return pbrImgs[`./pbr/${category}/${name}_n.png`] ?? '';
}

/** 粗糙度贴图 URL（缺失 = ''）。synth/water 无 _r（水面粗糙度是材质常量）。 */
export function pbrRoughUrl(category: PbrCategory, name: string): string {
  return pbrImgs[`./pbr/${category}/${name}_r.png`] ?? '';
}

/**
 * 开间贴图（`facade_tiles/`）的派生 PBR 贴图 URL（缺失 = ''，材质降级为无凹凸）。
 *
 * **注意与 `pbrNormalUrl('facades', ...)` 不可混用**：facades 下的 `_n/_r` 是从
 * 「整栋立面图」派生的，与开间贴图内容不同源，混用会让凹凸与颜色图错位
 * （批次 37 §1.7 缺陷 D5）。派生图须与颜色图同源、同宽高比（判据 A4）。
 *
 * @param kind 'n' = 法线图 / 'r' = 粗糙度图
 * @param name 城区 id（经 `facadeTileStem` 解析材质族；已解析的 stem 传入亦可幂等命中）
 * @param variant 'base' / 'mid'
 */
export function facadeTilePbrUrl(
  kind: 'n' | 'r',
  name: string,
  variant: FacadeVariant,
): string {
  return pbrImgs[`./pbr/facade_tiles/${facadeTileStem(name)}_${variant}_${kind}.png`] ?? '';
}
