/**
 * catalog-districts — 32 个城区的 objectInfo 条目（批次 28 · 工作线 B1）。
 *
 * 名称三语复用 i18n `virtualCity.district.<id>`（与 hover 卡同源，切换语言一致）；
 * 简介为本表新建文案（后期按信息裁剪模型的依据之一）。
 */

import { translate, type Lang, type TKey } from '@/i18n';
import { VIRTUAL_CITY_DISTRICTS } from '@/types/virtualCity';
import type { ObjectInfoEntry, TriText } from './catalog';

/** 城区 id → 新建三语简介（中文 30–80 字：这是什么、在城市里干什么）。 */
const DISTRICT_DESC: Record<string, TriText> = {
  finance: {
    'zh-CN': '城市中央商务区，银行、证券与基金总部聚集地，房价租金全城最高，对市场周期也最敏感。',
    en: 'The central business district home to banks, brokerages and funds — the highest prices and rents in town, and the most sensitive to market cycles.',
    ja: '中央業務地区。銀行・証券・ファンドの本社が集積し、市内最高値の地価と家賃を誇る一方、景気変動への感応度も最も高い。',
  },
  tech: {
    'zh-CN': '科技企业与研发机构聚集的园区，创新活跃，写字楼与人才公寓需求两旺。',
    en: 'A hub of tech firms and R&D labs with vibrant innovation and strong demand for offices and talent apartments.',
    ja: 'テック企業と研究開発機関が集う园区。イノベーションが活発で、オフィスと人材住宅の需要がともに旺盛。',
  },
  industry: {
    'zh-CN': '传统制造业基地，厂房与仓储连片，房价亲民，是产业工人的主要聚居地。',
    en: 'The traditional manufacturing base — contiguous factories and warehouses, affordable homes, and a large industrial workforce.',
    ja: '伝統的な製造業基地。工場と倉庫が連なり、住宅価格は手ごろで、産業労働者の主要な居住地。',
  },
  oldtown: {
    'zh-CN': '历史形成的旧城街区，街巷密集、生活气息浓厚，房价稳定但增长平缓。',
    en: 'The historic old town with dense lanes and rich everyday life — stable prices but modest growth.',
    ja: '歴史的に形成された旧市街。路地が密集し生活の息づかいが濃く、地価は安定しているが伸びは緩やか。',
  },
  commerce: {
    'zh-CN': '零售、餐饮与娱乐聚集的核心商圈，人流如织，商铺租金高企。',
    en: 'The core retail quarter of shops, dining and entertainment, with heavy footfall and premium storefront rents.',
    ja: '小売・飲食・エンタメが集まる核心商圏。人流が絶えず、店舗賃料は高止まり。',
  },
  residential: {
    'zh-CN': '城市主力居住板块，普通住宅与社区配套齐全，是大多数居民的安家之所。',
    en: 'The city’s main housing district with complete community facilities — where most residents make their home.',
    ja: '都市の主力住宅地。一般住宅とコミュニティ施設が揃い、大多数の住民が暮らす場所。',
  },
  suburb: {
    'zh-CN': '城市外缘的低密度居住区，环境清静、房价亲民，通勤依赖道路与公交。',
    en: 'Low-density suburbs on the city fringe: quiet, affordable, and commuter-dependent on roads and buses.',
    ja: '都市外縁の低密度住宅地。静かで手ごろな価格、通勤は道路とバスに依存。',
  },
  riverside: {
    'zh-CN': '沿运河发展的新兴滨水城区，景观住宅与休闲商业并举，升值潜力备受关注。',
    en: 'A rising waterfront district along the canal, blending scenic housing with leisure commerce and strong upside potential.',
    ja: '運河沿いに発展する新しい水辺の街区。景観住宅と余暇商業が両立し、値上がり期待も高い。',
  },
  logistics_port: {
    'zh-CN': '临港物流与仓储基地，集装箱堆场与货运通道密集，支撑城市供应链运转。',
    en: 'Port-side logistics and warehousing with dense container yards and freight corridors feeding the city’s supply chain.',
    ja: '临港物流・倉庫基地。コンテナヤードと貨物動線が集積し、都市のサプライチェーンを支える。',
  },
  hightech_park: {
    'zh-CN': '高新技术产业集聚区，头部企业总部与孵化器林立，房价涨幅常年领跑。',
    en: 'The high-tech cluster of flagship HQs and incubators, whose property values consistently lead the market.',
    ja: '先端技術産業の集積地。大手本社とインキュベーターが並び、地価上昇率は常にトップクラス。',
  },
  edu_district: {
    'zh-CN': '学校与培训机构聚集的教育板块，学区需求稳定，社区文化氛围浓厚。',
    en: 'An education quarter of schools and training institutes with steady school-district demand and a studious atmosphere.',
    ja: '学校や研修機関が集まる教育地区。学区需要が安定し、コミュニティの文化度も高い。',
  },
  medical_city: {
    'zh-CN': '医院与健康产业聚集区，医疗服务完善，养老与康复需求持续旺盛。',
    en: 'The healthcare cluster of hospitals and wellness firms, with complete medical services and lasting demand for elder care.',
    ja: '病院とヘルスケア産業が集まる医療都市。医療サービスが充実し、介護・リハビリ需要も根強い。',
  },
  industrial_park: {
    'zh-CN': '新兴制造与生产基地，厂区规划整齐，房价处于全市洼地。',
    en: 'An emerging manufacturing base with orderly industrial parks and some of the lowest prices in the city.',
    ja: '新興の製造・生産基地。工場地区が整然と区画され、地価は市内の安値圏。',
  },
  central_park: {
    'zh-CN': '城市绿心——大片草坪、林荫与景观小亭，是全城最大的公共休闲空间。',
    en: 'The city’s green heart: lawns, tree shade and garden pavilions forming its largest public leisure space.',
    ja: '都市の緑の心臓部。広大な芝生と木陰、景観の小亭がそろう市内最大の公共休憩空間。',
  },
  transport_hub: {
    'zh-CN': '火车站与长途客运聚集的交通门户，人流量大，住宿与零售配套发达。',
    en: 'The transport gateway of rail and coach terminals, with heavy passenger flows and a thriving hotel/retail ecosystem.',
    ja: '鉄道と長距離バスが集まる交通の玄関口。人流が多く、宿泊・小売の関連産業も発達。',
  },
  cultural_creative: {
    'zh-CN': '设计、传媒与艺术工作室聚集的文创街区，旧厂房改造而来，活力十足。',
    en: 'A creative quarter of design, media and art studios carved out of converted old factories.',
    ja: 'デザイン・メディア・アートの工房が集まる文化創造街区。旧工場をリノベーションし、活気にあふれる。',
  },
  fin_sub_center: {
    'zh-CN': '城市第二金融中心，新兴写字楼集群，承接金融外溢的高端需求。',
    en: 'The city’s second financial center — a new office cluster absorbing high-end demand spilling over from the CBD.',
    ja: '都市第二の金融センター。新興オフィス街として、CBD から溢れる高級需要を受け止める。',
  },
  software_park: {
    'zh-CN': '软件与互联网企业园区，程序员聚居，住房租赁市场活跃。',
    en: 'A software and internet company park with a large developer population and a lively rental market.',
    ja: 'ソフトウェア・インターネット企業の园区。エンジニアが多く住み、賃貸市場も活発。',
  },
  airport_town: {
    'zh-CN': '依托机场发展的临空经济区，酒店与会展配套完善，商旅往来频繁。',
    en: 'An airport-driven aerotropolis with strong hotel and convention facilities and constant business travel.',
    ja: '空港を核に発展した臨空経済圏。ホテルとコンベンション施設が充実し、出張往来が絶えない。',
  },
  air_logistics: {
    'zh-CN': '航空货运与快递分拨基地，货机起降不停，是时效物流的核心节点。',
    en: 'The air-cargo and express-sorting base where freighters never stop — the hub of time-critical logistics.',
    ja: '航空貨物と宅配仕分けの基地。貨物機の離着陸が絶えず、時間指定物流の中核拠点。',
  },
  auto_city: {
    'zh-CN': '整车制造与汽车服务产业集聚区，4S 店与零部件市场连片分布。',
    en: 'An automotive cluster of vehicle assembly and services, with continuous rows of dealerships and parts markets.',
    ja: '完成車製造と自動車サービスの集積地。ディーラーと部品市場が連なる。',
  },
  mountain_resort: {
    'zh-CN': '依山而建的民宿与度假聚落，周末经济活跃，环境清幽宜人。',
    en: 'A hillside resort village of guesthouses and retreats, busy on weekends and serene all week.',
    ja: '山の斜面に広がる民宿とリゾートの集落。週末経済が活発で、環境は静かで心地よい。',
  },
  chem_park: {
    'zh-CN': '化工与材料生产企业园区，安全管理严格，房价处于全市低位。',
    en: 'A chemicals and materials industrial park under strict safety regimes, with prices among the city’s lowest.',
    ja: '化学・素材メーカーの园区。安全管理が厳格で、地価は市内の低位。',
  },
  agri_park: {
    'zh-CN': '设施农业与农产品加工基地，温室连片，供应城市餐桌。',
    en: 'The modern agriculture base of greenhouses and food processing that stocks the city’s tables.',
    ja: '施設園芸と農産加工の基地。温室が連なり、都市の食卓を支える。',
  },
  health_town: {
    'zh-CN': '面向退休与疗养人群的宜居小镇，医疗与颐养设施完善。',
    en: 'A livable town for retirees and convalescents, well served by medical and senior-care facilities.',
    ja: '退職・療養向きの住みやすい町。医療と養護施設が充実。',
  },
  steel_town: {
    'zh-CN': '特种钢铁产业重镇，钢厂与家属区相伴，房价平稳偏低。',
    en: 'A specialty-steel town where mills and worker housing sit side by side, keeping prices low and steady.',
    ja: '特殊鋼の産業都市。製鉄所と住宅地が隣接し、地価は安定して低め。',
  },
  old_city_culture: {
    'zh-CN': '保留历史街巷与文化遗迹的古城板块，文旅消费是其经济主脉。',
    en: 'The heritage quarter preserving historic lanes and relics, where cultural tourism drives the economy.',
    ja: '歴史的な路地と文化遺産を残す古城地区。文化観光が経済の主軸。',
  },
  university_town: {
    'zh-CN': '高校云集的大学城，青春活力足，租赁与商业围绕师生需求展开。',
    en: 'A campus town of universities buzzing with youth, its rentals and retail built around student life.',
    ja: '大学が集まる学園都市。若者の活気にあふれ、賃貸と商業は学生の需要を中心に回る。',
  },
  wetland_park: {
    'zh-CN': '城市湿地生态公园，芦苇摇曳、水鸟栖息，是观鸟与自然教育场所。',
    en: 'An urban wetland park of reeds and waterfowl — a prime spot for birdwatching and nature education.',
    ja: '都市の湿地生態公園。葦がゆらぎ水鳥が飛来する、バードウォッチングと自然教育の場。',
  },
  sports_new_city: {
    'zh-CN': '围绕体育场馆建设的新城板块，赛事与演艺活动带动周边消费。',
    en: 'A new district built around its stadiums, where matches and concerts fuel the surrounding economy.',
    ja: 'スポーツスタジアムを中心に発展した新都心。大会やライブが周辺消費を押し上げる。',
  },
  bay_new_town: {
    'zh-CN': '环湾开发的滨海新城，高端住宅与总部经济并重，房价坚挺。',
    en: 'A bayfront new town pairing luxury housing with headquarters economics, keeping prices firm.',
    ja: '湾岸開発の海浜ニュータウン。高級住宅と本社経済が両輪で、地価は底堅い。',
  },
  highspeed_rail_town: {
    'zh-CN': '依托高铁站崛起的枢纽新城，商务出行便利，开发热度居高不下。',
    en: 'A boomtown around the high-speed rail station, prized for business travel and nonstop development.',
    ja: '高速鉄道の駅を中心に台頭した結節都市。出張に便利で開発熱も高い。',
  },
};

function districtName(id: string): TriText {
  const key = `virtualCity.district.${id}` as TKey;
  return {
    'zh-CN': translate('zh-CN' as Lang, key),
    en: translate('en' as Lang, key),
    ja: translate('ja' as Lang, key),
  };
}

/** 32 城区条目（顺序与 VIRTUAL_CITY_DISTRICTS 一致）。 */
export const DISTRICT_ENTRIES: ObjectInfoEntry[] = VIRTUAL_CITY_DISTRICTS.map((d) => {
  const desc = DISTRICT_DESC[d.id];
  if (!desc) throw new Error(`[objectInfo] missing district desc: district.${d.id}`);
  return {
    id: `district.${d.id}`,
    category: 'district',
    icon: '🏘️',
    name: districtName(d.id),
    desc,
  };
});
