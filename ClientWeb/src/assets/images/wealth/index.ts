/**
 * 虚拟城市 (Wealth) 资产索引。
 *
 * 降级策略（前端架构文档 §9 + 08-UI优化/02-架构设计 §5）：所有 PNG 由
 * python-generate-image-tool/{generate_wealth_assets,generate_wealth_city_assets}.py
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
 *   roofs/<districtId>.png                               512×512  透明
 *   streets/{asphalt_main,asphalt_side,sidewalk_main,sidewalk_side,crosswalk,centerline}.png
 *     街道铺装贴图（P1-A 道路重做）
 *   ground/{grass_tile,plaza_tile,water_tile}.png           512×512 可平铺（14-3D渲染深化地表环境）
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
const propImgs = import.meta.glob<string>('./props/**/*.png', { eager: true, import: 'default' });

/** 大厅 banner（缺失 = ''，WealthLobbyPage 回落 CSS 渐变）。 */
export const WEALTH_BANNER: string = bannerImgs['./banner.png'] ?? '';

/**
 * 职业头像 URL。id 大小写不敏感（"P01" / "p01" 皆可）；文档池随机 id
 * （如 "N9012345"）无对应 PNG 时返回 ''，AgentToken 回落职业色圆环 + emoji。
 */
export function professionAvatar(id: string): string {
  return agentImgs[`./agents/${id.toLowerCase()}.png`] ?? '';
}

/** 城区底板纹理 URL（缺失 = ''，DistrictBlock 回落 DistrictDefs 主色）。 */
export function districtTexture(id: string): string {
  return districtImgs[`./districts/${id}.png`] ?? '';
}

/** CPI 八大类消费品图标 URL（缺失 = ''，组件回落类别主色/emoji）。 */
export function goodsIcon(id: string): string {
  return goodsImgs[`./goods/${id}.png`] ?? '';
}

// ── 08-UI优化 v2 新增（P1-A / P1-B / P1-C）────────────────────

/** 楼宇立面 variant 字面量（与 generate_wealth_city_assets.py::FACADES 对齐）。 */
export type FacadeVariant = 'base' | 'mid';

/**
 * 城区楼宇侧立面贴图 URL（缺失 = ''，BuildingMesh 退回到 DistrictDefs 主色）。
 *
 * @param districtId 城区 id（finance / tech / ... / riverside）
 * @param variant    'base' = 楼栋底层 / 'mid' = 楼栋中上层（循环贴图避免接缝）
 */
export function districtFacadeUrl(districtId: string, variant: FacadeVariant): string {
  return facadeImgs[`./facades/${districtId}_${variant}.png`] ?? '';
}

/** 城区楼顶贴图 URL（缺失 = ''，BuildingMesh 退回到 DistrictDefs 主色）。 */
export function districtRoofUrl(districtId: string): string {
  return roofImgs[`./roofs/${districtId}.png`] ?? '';
}

/** 街道铺装类型字面量（与 generate_wealth_city_assets.py::STREETS 对齐）。 */
export type StreetTileName =
  | 'asphalt_main'
  | 'asphalt_side'
  | 'sidewalk_main'
  | 'sidewalk_side'
  | 'crosswalk'
  | 'centerline';

/**
 * 街道铺装贴图 URL（缺失 = ''，Road / Ground 退回到纯色 / 简化几何）。
 * 路面贴图通常配 RepeatWrapping × N 平铺。
 */
export function streetTileUrl(name: StreetTileName): string {
  return streetImgs[`./streets/${name}.png`] ?? '';
}

/** 地表环境贴图字面量（与 3d_script/procedural_city_textures.py::ground 对齐，14-3D渲染深化）。 */
export type GroundTileName = 'grass_tile' | 'plaza_tile' | 'water_tile' | 'urban_base';

/**
 * 地表环境贴图 URL（缺失 = ''，组件回退纯色：grass #3f7a3a / plaza #9aa1ab / water #1a3a52）。
 * 草地/广场/水面均 RepeatWrapping 可平铺；水面沿 v 方向滚动。
 */
export function groundTileUrl(name: GroundTileName): string {
  return groundImgs[`./ground/${name}.png`] ?? '';
}

/** 街景道具类别字面量（与 generate_wealth_city_assets.py::PROPS 对齐）。 */
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
 * 文件命名约定：props/pedestrian/head_<outfit>.png（与 generate_wealth_v2_props.py 对齐）。
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