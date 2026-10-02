/**
 * catalog-scene — 市政 / 地标 / 四缘环境 / 水系 / 天空 / 地面 的 objectInfo 条目
 * （批次 28 · 工作线 B1）。
 */

import type { ObjectInfoEntry } from './catalog';

export const SCENE_ENTRIES: ObjectInfoEntry[] = [
  // ── 市政设施（CivicLayer 14 组件）────────────────────────────────
  {
    id: 'civic.city-hall',
    category: 'civic',
    icon: '🏛️',
    name: { 'zh-CN': '市政厅', en: 'City Hall', ja: '市役所' },
    desc: {
      'zh-CN': '城市行政中枢，市政机构办公地，也是市民之声与选举公告的发布点。',
      en: 'The seat of city government — and where civic voices and election notices are posted.',
      ja: '都市行政の中枢。市政機関の執務地であり、市民の声や選挙告示の発信点でもある。',
    },
  },
  {
    id: 'civic.port-terminal',
    category: 'civic',
    icon: '⚓',
    name: { 'zh-CN': '港口码头', en: 'Port Terminal', ja: '港湾ターミナル' },
    desc: {
      'zh-CN': '物流港的码头作业区，岸吊与集装箱堆场昼夜运转，支撑城市对外贸易。',
      en: 'The working quay of the logistics port, where cranes and container yards run day and night for the city’s trade.',
      ja: '物流港の埠頭作業区。岸壁クレーンとコンテナヤードが昼夜稼働し、都市の対外貿易を支える。',
    },
  },
  {
    id: 'civic.sports-field',
    category: 'civic',
    icon: '⚽',
    name: { 'zh-CN': '运动场', en: 'Sports Field', ja: 'スポーツ場' },
    desc: {
      'zh-CN': '带跑道与球门的综合运动场，供市民锻炼健身与举办社区赛事。',
      en: 'A multi-sport field with running track and goals for exercise and community matches.',
      ja: 'トラックとゴールを備えた総合スポーツ場。市民の運動やコミュニティ大会に使われる。',
    },
  },
  {
    id: 'civic.rail-viaduct',
    category: 'civic',
    icon: '🚄',
    name: { 'zh-CN': '轻轨高架', en: 'Rail Viaduct', ja: '高架鉄道' },
    desc: {
      'zh-CN': '城市轻轨高架线路与站台，大运量通勤轨道，缓解地面交通压力。',
      en: 'The elevated light-rail line and stations carrying mass commuter flows above the street traffic.',
      ja: '都市ライトレールの高架線と駅。大量の通勤客を運び、地上交通の負担を軽減する。',
    },
  },
  {
    id: 'civic.heli-pad',
    category: 'civic',
    icon: '🚁',
    name: { 'zh-CN': '直升机坪', en: 'Heli-pad', ja: 'ヘリポート' },
    desc: {
      'zh-CN': '应急与商务直升机停机坪，提供城市空中快速通道。',
      en: 'A heli-pad for emergency and business helicopters — the city’s fast lane through the sky.',
      ja: '緊急・業務用ヘリコプターの発着場。都市の空中快速路を提供する。',
    },
  },
  {
    id: 'civic.gas-station',
    category: 'civic',
    icon: '⛽',
    name: { 'zh-CN': '加油站', en: 'Gas Station', ja: 'ガソリンスタンド' },
    desc: {
      'zh-CN': '为机动车加油补给的服务站，附设便利店，是车流的能源补给点。',
      en: 'A fuel station with a convenience store attached — the energy stop for the city’s vehicles.',
      ja: '自動車への給油・補給を行うサービスステーション。併設のコンビニとともに車両の能源補給点。',
    },
  },
  {
    id: 'civic.substation',
    category: 'civic',
    icon: '⚡',
    name: { 'zh-CN': '变电站', en: 'Substation', ja: '変電所' },
    desc: {
      'zh-CN': '城市电力变电站，将高压电降压配送至街区，是供电网络的关键节点。',
      en: 'A power substation stepping high voltage down for the neighbourhoods — a key node of the grid.',
      ja: '都市の変電所。高圧電を降圧して街区へ配送する、給電網の要所。',
    },
  },
  {
    id: 'civic.water-tower',
    category: 'civic',
    icon: '🗼',
    name: { 'zh-CN': '水塔', en: 'Water Tower', ja: '給水塔' },
    desc: {
      'zh-CN': '城市供水水塔，储水稳压，保障居民与产业的日常用水。',
      en: 'The water tower storing and pressurising supply to keep taps flowing for homes and industry.',
      ja: '都市給水の水塔。貯水と圧力調整により、住民と産業の日常用水を保証する。',
    },
  },
  {
    id: 'civic.comm-tower',
    category: 'civic',
    icon: '📡',
    name: { 'zh-CN': '通讯塔', en: 'Comm Tower', ja: '通信塔' },
    desc: {
      'zh-CN': '无线通讯塔，承载移动信号与微波链路，维系城市信息互联。',
      en: 'A wireless mast carrying mobile signals and microwave links that keep the city connected.',
      ja: '無線通信塔。移動体信号とマイクロ波回線を担い、都市の情報接続を維持する。',
    },
  },
  {
    id: 'civic.fire-station',
    category: 'civic',
    icon: '🚒',
    name: { 'zh-CN': '消防站', en: 'Fire Station', ja: '消防署' },
    desc: {
      'zh-CN': '消防救援站，驻守消防车与队员，承担火灾扑救与应急救援任务。',
      en: 'The fire and rescue station housing engines and crews for firefighting and emergencies.',
      ja: '消防・救助の拠点。消防車と隊員が駐留し、消火と緊急救助にあたる。',
    },
  },
  {
    id: 'civic.police-station',
    category: 'civic',
    icon: '🚓',
    name: { 'zh-CN': '警察局', en: 'Police Station', ja: '警察署' },
    desc: {
      'zh-CN': '公安派出所，负责治安巡逻、案件受理与公共安全服务。',
      en: 'The local police station handling patrols, case intake and public-safety services.',
      ja: '治安の派出所。パトロール、案件受理、公共安全サービスを担当する。',
    },
  },
  {
    id: 'civic.outskirts',
    category: 'civic',
    icon: '🌾',
    name: { 'zh-CN': '外围腹地', en: 'City Outskirts', ja: '都市外縁' },
    desc: {
      'zh-CN': '建成区外围的开阔腹地，风力发电机与郊野设施点缀其间。',
      en: 'Open land beyond the built-up area, dotted with wind turbines and rural facilities.',
      ja: '建成地の外側に広がる開けた腹地。風力タービンと郊外施設が点在する。',
    },
  },
  {
    id: 'civic.park-extras',
    category: 'civic',
    icon: '🚻',
    name: { 'zh-CN': '公共卫生间', en: 'Public Restroom', ja: '公衆トイレ' },
    desc: {
      'zh-CN': '公园西北角的公共卫生间，服务全园游园时长（批次 45 起凉亭/游乐/健身/座椅/园灯单列条目）。',
      en: 'The park’s public restroom in its northwest corner, serving visits of any length.',
      ja: '公園北西隅の公衆トイレ。遊園時間を支える。',
    },
  },
  // ── 批次 45 公园设施五件（方案 45 §4 B4：拆分自 civic.park-extras 单条目）──
  {
    id: 'park.pavilion',
    category: 'civic',
    icon: '⛩️',
    name: { 'zh-CN': '六角亭', en: 'Hexagonal Pavilion', ja: '六角東屋' },
    desc: {
      'zh-CN': '青灰瓦攒尖顶六角亭：石台基、六根檐柱、柱间美人靠。清式小式比例（柱高 = 面宽 × 0.8）。',
      en: 'A slate-roofed hexagonal pavilion on a stone plinth — six columns, a latticed balustrade seat, classical proportions.',
      ja: '石台基の上の六角東屋。柱間には腰掛、青瓦の宝形造屋根。',
    },
  },
  {
    id: 'park.playground',
    category: 'civic',
    icon: '🛝',
    name: { 'zh-CN': '儿童乐园', en: 'Children’s Playground', ja: '児童遊園' },
    desc: {
      'zh-CN': '组合滑梯与双座秋千：1.2 m 平台、32° 滑道、EPDM 柔性垫层，符合儿童活动场地安全要求。',
      en: 'A slide-and-swings combo on a soft rubber pad — 1.2 m platform, 32° chute, safety surfacing throughout.',
      ja: '滑り台とブランコの複合遊具。1.2 m プラットホーム、32° スライド、弾性舗装。',
    },
  },
  {
    id: 'park.fitness',
    category: 'civic',
    icon: '🏋️',
    name: { 'zh-CN': '健身角', en: 'Fitness Corner', ja: '健康広場' },
    desc: {
      'zh-CN': '全民健身三件套：双位太空漫步机、扭腰器与单杠，器材绿涂装，落地地脚法兰锚固。',
      en: 'The outdoor fitness trio — air walker, waist twister and pull-up bar — bolted to the ground in municipal green.',
      ja: '屋外健身器材三種：スカイウォーカー、ウェストツイスター、鉄棒。',
    },
  },
  {
    id: 'park.bench',
    category: 'civic',
    icon: '🪑',
    name: { 'zh-CN': '公园长椅', en: 'Park Bench', ja: '公園ベンチ' },
    desc: {
      'zh-CN': '三人位防腐木长椅（1.8 m，铸铝弓形脚）：座高 43 cm、靠背 103°，沿园路按 50~100 m 间隔布置。',
      en: 'A 1.8 m timber bench on cast-aluminium legs — 43 cm seat, 103° back — spaced along the paths.',
      ja: '1.8 m の防腐木ベンチ。通路沿いに 50~100 m 間隔で配置。',
    },
  },
  {
    id: 'park.lamp',
    category: 'civic',
    icon: '🏮',
    name: { 'zh-CN': '庭院灯', en: 'Garden Lamp', ja: '庭園灯' },
    desc: {
      'zh-CN': '3 m 单头方罩庭院灯：暖白灯罩夜间渐亮，沿园路 10 m 交错布置，是公园夜景的骨架。',
      en: 'A 3 m lantern-head garden lamp, warming up after dusk and spacing the paths every 10 m.',
      ja: '高さ 3 m の庭園灯。日没後に明るさを増し、園路を 10 m 間隔で照らす。',
    },
  },
  {
    id: 'civic.canal-extras',
    category: 'civic',
    icon: '🛶',
    name: { 'zh-CN': '运河配套', en: 'Canal Extras', ja: '運河付帯施設' },
    desc: {
      'zh-CN': '运河沿岸的系船柱、护栏与游船等配套，服务滨水休闲与航运。',
      en: 'Canal-side bollards, railings and boats serving waterfront leisure and light shipping.',
      ja: '運河沿いの係船柱、手すり、遊覧船などの付帯施設。水辺の余暇と水運に役立つ。',
    },
  },

  // ── 地标（LandmarksLayer）────────────────────────────────────────
  {
    id: 'landmark.fountain',
    category: 'landmark',
    icon: '⛲',
    name: { 'zh-CN': '景观喷泉', en: 'Fountain', ja: '噴水' },
    desc: {
      'zh-CN': '公园中心的景观喷泉，水柱起伏，是市民休憩聚集的地标。',
      en: 'The park’s centrepiece fountain, its dancing jets a natural gathering spot for citizens.',
      ja: '公園の中心にある景観噴水。水柱が躍り、市民が集う憩いのランドマーク。',
    },
  },
  {
    id: 'landmark.park-grounds',
    category: 'landmark',
    icon: '🌼',
    name: { 'zh-CN': '公园园地', en: 'Park Grounds', ja: '公園園地' },
    desc: {
      'zh-CN': '中央公园的园路、花坛与草坪，城市的绿肺与公共客厅。',
      en: 'Paths, flowerbeds and lawns of Central Park — the city’s green lung and public living room.',
      ja: '中央公園の園路・花壇・芝生。都市の緑の肺であり、みんなの居間。',
    },
  },
  {
    id: 'landmark.construction-site',
    category: 'landmark',
    icon: '🏗️',
    name: { 'zh-CN': '建筑工地', en: 'Construction Site', ja: '建設現場' },
    desc: {
      'zh-CN': '围挡中的建筑工地，塔吊林立，见证城市的建设与扩张。',
      en: 'A fenced construction site of tower cranes, witnessing the city’s build-out and growth.',
      ja: '仮囲いの建設現場。タワークレーンが立ち並び、都市の建設と拡張を見守る。',
    },
  },
  {
    id: 'landmark.parking-lot',
    category: 'landmark',
    icon: '🅿️',
    name: { 'zh-CN': '停车场', en: 'Parking Lot', ja: '駐車場' },
    desc: {
      'zh-CN': '集中停车场，成排车位与照明设施，消化周边街区的停车需求。',
      en: 'A central parking lot of marked bays and lighting that absorbs the surrounding blocks’ parking demand.',
      ja: '集中駐車場。整然とした区画と照明がそろい、周辺街区の駐車需要を受け止める。',
    },
  },

  // ── 四缘环境带（CityEdgeLayer）──────────────────────────────────
  {
    id: 'edge.north-mountains',
    category: 'edge',
    icon: '🏔️',
    name: { 'zh-CN': '北部雪山', en: 'Northern Snow Mountains', ja: '北部雪山' },
    desc: {
      'zh-CN': '城市北缘的雪山群峰，为城市提供天际线屏障与山地景观。',
      en: 'Snow-capped peaks along the northern fringe, forming the city’s skyline barrier and mountain scenery.',
      ja: '都市北縁の雪山連峰。スカイラインの防壁と山岳景観を提供する。',
    },
  },
  {
    id: 'edge.west-desert',
    category: 'edge',
    icon: '🏜️',
    name: { 'zh-CN': '西部沙漠', en: 'Western Desert', ja: '西部砂漠' },
    desc: {
      'zh-CN': '城市西缘的沙丘与戈壁，荒漠植被稀疏，塑造独特的干旱风貌。',
      en: 'Dunes and gravel desert on the western fringe, its sparse flora shaping an arid face for the city.',
      ja: '都市西縁の砂丘と戈壁。乾燥植生がまばらで、独特の乾燥景観をつくる。',
    },
  },
  {
    id: 'edge.east-forest',
    category: 'edge',
    icon: '🌿',
    name: { 'zh-CN': '东部森林', en: 'Eastern Forest', ja: '東部森林' },
    desc: {
      'zh-CN': '城市东缘的连绵森林，郁郁葱葱，是城市的生态屏障与天然氧吧。',
      en: 'Continuous woodland on the eastern fringe — an ecological barrier and the city’s natural oxygen bar.',
      ja: '都市東縁に広がる森林。緑が深く、都市の生態防壁と天然のオーバーバーとなる。',
    },
  },
  {
    id: 'edge.south-ocean',
    category: 'edge',
    icon: '🌊',
    name: { 'zh-CN': '南部海洋', en: 'Southern Ocean', ja: '南部海洋' },
    desc: {
      'zh-CN': '城市南缘的海洋与沙滩，海风与浪涛构成滨海休闲带。',
      en: 'Ocean and beach along the southern fringe, where sea breeze and surf form a coastal leisure belt.',
      ja: '都市南縁の海と砂浜。潮風と波が海辺のリゾート地帯を形づくる。',
    },
  },
  {
    id: 'edge.lighthouse',
    category: 'edge',
    icon: '🗼',
    name: { 'zh-CN': '灯塔', en: 'Lighthouse', ja: '灯台' },
    desc: {
      'zh-CN': '防波堤端的红白条纹灯塔，为进出港口的船舶指引航向。',
      en: 'The red-and-white striped lighthouse at the breakwater tip, guiding ships in and out of the harbour.',
      ja: '防波堤の先端に立つ紅白縞の灯台。入出港する船舶の航路を示す。',
    },
  },
  {
    id: 'edge.cargo-ship',
    category: 'edge',
    icon: '🚢',
    name: { 'zh-CN': '货轮', en: 'Cargo Ship', ja: '貨物船' },
    desc: {
      'zh-CN': '往返航线上的货轮，运载集装箱货物，连接城市与世界市场。',
      en: 'Cargo ships on the trade routes, carrying containers that tie the city to world markets.',
      ja: '航路を行き来する貨物船。コンテナ貨物を運び、都市と世界市場を結ぶ。',
    },
  },
  {
    id: 'edge.crane',
    category: 'edge',
    icon: '🏗️',
    name: { 'zh-CN': '岸边吊机', en: 'Harbour Gantry Crane', ja: '岸壁クレーン' },
    desc: {
      'zh-CN': '码头岸边的集装箱岸吊，装卸船载货物，是港口作业的主力机械。',
      en: 'Gantry cranes on the quay loading and unloading ships — the workhorses of port operations.',
      ja: '埠頭のガントリークレーン。船荷の積み降ろしを担う港湾作業の主力機械。',
    },
  },
  {
    id: 'edge.sailboat',
    category: 'edge',
    icon: '⛵',
    name: { 'zh-CN': '帆船', en: 'Sailboat', ja: '帆船' },
    desc: {
      'zh-CN': '停泊在港湾的帆船，白帆点点，是滨海休闲生活的象征。',
      en: 'Sailboats moored in the bay, their white sails a symbol of coastal leisure life.',
      ja: '港湾に停泊する帆船。白い帆は海辺の余暇の象徴。',
    },
  },

  // ── 水系 ─────────────────────────────────────────────────────────
  {
    id: 'water.canal',
    category: 'water',
    icon: '🏞️',
    name: { 'zh-CN': '城市运河', en: 'City Canal', ja: '都市運河' },
    desc: {
      'zh-CN': '横贯城市东西的景观运河，调节微气候，并提供滨水休闲岸线。',
      en: 'The scenic canal crossing the city east to west, softening the microclimate and lining the waterfront with leisure.',
      ja: '街の東西を横断する景観運河。微気候を調整し、水辺の余暇岸壁を提供する。',
    },
  },
  {
    id: 'water.harbour',
    category: 'water',
    icon: '🛳️',
    name: { 'zh-CN': '物流港池', en: 'Harbour Basin', ja: '港内水域' },
    desc: {
      'zh-CN': '物流港的港池水域，供船舶停靠与货物装卸，是城市水运门户。',
      en: 'The harbour basin where ships berth and cargo is worked — the city’s gateway for water transport.',
      ja: '物流港の港内水域。船舶の接岸と荷役に使われ、都市の水運ゲートウェイとなる。',
    },
  },
  {
    id: 'water.mist',
    category: 'water',
    icon: '🌫️',
    name: { 'zh-CN': '水岸薄雾', en: 'Water Mist', ja: '水辺の霧' },
    desc: {
      'zh-CN': '水面蒸腾形成的薄雾，笼罩运河与港池岸边，营造湿润的滨水氛围。',
      en: 'Mist rising off the water and hanging over canal and harbour banks, lending the waterfront its damp mood.',
      ja: '水面から立ち上る薄い霧。運河と港の岸辺を包み、湿った水辺の空気を演出する。',
    },
  },

  // ── 天空 / 地面 ──────────────────────────────────────────────────
  {
    id: 'sky.cloud',
    category: 'sky',
    icon: '☁️',
    name: { 'zh-CN': '天空云团', en: 'Sky Cloud', ja: '雲' },
    desc: {
      'zh-CN': '漂浮在城市上空的云团，缓慢移动，为天际线增添层次与光影变化。',
      en: 'Clouds drifting slowly above the city, adding depth and shifting light to the skyline.',
      ja: '都市上空を漂う雲。ゆっくりと流れ、スカイラインに奥行きと光の変化を与える。',
    },
  },
  {
    id: 'ground.city',
    category: 'ground',
    icon: '🟫',
    name: { 'zh-CN': '城市地面', en: 'City Ground', ja: '都市地面' },
    desc: {
      'zh-CN': '城市建成区的地表铺装，承载全部街区、道路与市政设施。',
      en: 'The paved surface of the built-up city, carrying every district, road and public facility.',
      ja: '都市建成地の地表舗装。すべての街区・道路・公益施設を支える。',
    },
  },
];
