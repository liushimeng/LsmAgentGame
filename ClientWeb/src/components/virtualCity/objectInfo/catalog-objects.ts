/**
 * catalog-objects — 楼宇 / 街具 / 树 / 车辆 / 角色 / 道路 的 objectInfo 条目
 * （批次 28 · 工作线 B1）。
 */

import type { ObjectInfoEntry } from './catalog';

export const OBJECT_ENTRIES: ObjectInfoEntry[] = [
  // ── 楼宇（building_shapes 5 体块类型）────────────────────────────
  {
    id: 'building.tower',
    category: 'building',
    icon: '🏢',
    name: { 'zh-CN': '塔楼', en: 'High-rise Tower', ja: '高層タワー' },
    desc: {
      'zh-CN': '高层塔楼，多为写字楼与公寓混合体，玻璃幕墙配电梯机房与屋顶设备，撑起城区天际线。',
      en: 'A high-rise mixing offices and apartments, its glass curtain walls, lift rooms and rooftop plant defining the district skyline.',
      ja: 'オフィスと集合住宅の複合高層ビル。ガラスカーテンウォール、エレベーター機械室、屋上設備が街のスカイラインを形づくる。',
    },
  },
  {
    id: 'building.slab',
    category: 'building',
    icon: '🏬',
    name: { 'zh-CN': '板楼', en: 'Slab Building', ja: '板状ビル' },
    desc: {
      'zh-CN': '多层板式建筑，一梯多户的长条体量，常见于居住区与老城区，屋顶带女儿墙与水箱。',
      en: 'A long mid-rise slab with many units per stairwell, typical of residential and old-town blocks, capped by parapets and water tanks.',
      ja: '一廊下に多戸が並ぶ細長い中層建築。住宅地や旧市街に多く、屋上にはパラペットと受水槽を備える。',
    },
  },
  {
    id: 'building.house',
    category: 'building',
    icon: '🏠',
    name: { 'zh-CN': '独栋住宅', en: 'Detached House', ja: '戸建て住宅' },
    desc: {
      'zh-CN': '低层独栋或联排住宅，坡屋顶与小院构成居住区的低密度肌理。',
      en: 'Low-rise detached or row houses whose pitched roofs and small yards give residential areas their low-density texture.',
      ja: '低層の戸建てまたは連棟住宅。勾配屋根と小さな庭が、住宅地に低密度の街並みを与える。',
    },
  },
  {
    id: 'building.shed',
    category: 'building',
    icon: '🏭',
    name: { 'zh-CN': '厂房仓库', en: 'Factory Shed', ja: '工場・倉庫' },
    desc: {
      'zh-CN': '工业区的单层厂房与仓库，大跨度平顶与装卸面，服务物流与制造企业。',
      en: 'Single-storey factories and warehouses with wide flat roofs and loading faces, serving logistics and manufacturing firms.',
      ja: '工業地帯の平屋工場と倉庫。大スパンの平屋根と荷役面を備え、物流・製造企業を支える。',
    },
  },
  {
    id: 'building.pavilion',
    category: 'building',
    icon: '🏛️',
    name: { 'zh-CN': '景观点亭', en: 'Garden Pavilion', ja: '景観パビリオン' },
    desc: {
      'zh-CN': '公园与广场中的景观小品建筑，体量轻巧，供市民休憩与举办小型活动。',
      en: 'A light garden structure in parks and squares where citizens rest and small events take place.',
      ja: '公園や広場の軽やかな景観建築。市民の休憩や小規模イベントに使われる。',
    },
  },

  // ── 街具（StreetPropsLayer）──────────────────────────────────────
  {
    id: 'prop.trash-can',
    category: 'street-prop',
    icon: '🗑️',
    name: { 'zh-CN': '分类垃圾箱', en: 'Sorting Trash Can', ja: '分別ゴミ箱' },
    desc: {
      'zh-CN': '城市街道的分类垃圾箱：主干道路侧按厨余（绿）/可回收（蓝）成对布设，街区另有可回收/其他/有害三色分类桶，由环卫系统定时清运。',
      en: 'Street sorting bins: paired kitchen-waste (green) and recyclable (blue) bins along main roads, plus three-colour block bins — cleared on schedule by sanitation crews.',
      ja: '街路の分別ゴミ箱。幹線道路沿いに生ごみ（緑）とリサイクル（青）のペアを配置し、街区には三色分別箱もある。清掃システムが定時回収する。',
    },
  },
  {
    id: 'prop.mailbox',
    category: 'street-prop',
    icon: '📮',
    name: { 'zh-CN': '邮筒', en: 'Mailbox', ja: '郵便ポスト' },
    desc: {
      'zh-CN': '街头柱式邮筒（中国邮政绿涂装），供市民投寄信件与明信片，是邮政服务在街区的末梢触点。',
      en: 'A green street pillar box for letters and postcards — the postal service’s fingertips in every neighbourhood.',
      ja: '緑色の街角の柱式郵便ポスト。手葉書を投函し、郵便サービスが街区に届く末端の接点となる。',
    },
  },
  {
    id: 'prop.vendor-kiosk',
    category: 'street-prop',
    icon: '🗞️',
    name: { 'zh-CN': '报刊亭', en: 'Newsstand Kiosk', ja: '売店キオスク' },
    desc: {
      'zh-CN': '街角零售小亭，售卖报刊、饮料与零食，也是街区的信息与社交小枢纽。',
      en: 'A corner kiosk selling papers, drinks and snacks, doubling as a tiny information and social hub.',
      ja: '新聞・飲料・軽食を売る街角のキオスク。地域の情報と社交の小さな拠点も兼ねる。',
    },
  },
  {
    id: 'prop.bicycle-rack',
    category: 'street-prop',
    icon: '🚲',
    name: { 'zh-CN': '自行车停放架', en: 'Bicycle Rack', ja: '駐輪ラック' },
    desc: {
      'zh-CN': '人行道自行车停放架，规范共享单车与私人单车有序停放，减少占道。',
      en: 'Sidewalk racks that keep shared and private bikes parked in order instead of blocking the pavement.',
      ja: '歩道の駐輪ラック。シェア自転車や自家用車両を整然と停め、歩道の占有を防ぐ。',
    },
  },
  {
    id: 'prop.phone-booth',
    category: 'street-prop',
    icon: '☎️',
    name: { 'zh-CN': '公共电话亭', en: 'Phone Booth', ja: '公衆電話ボックス' },
    desc: {
      'zh-CN': '街头公共电话亭，红绿两色，如今多兼具紧急呼叫与小型广告位功能。',
      en: 'Street phone booths in red and green, now often serving as emergency call points and small ad panels.',
      ja: '赤と緑の公衆電話ボックス。現在は緊急通話設備や小型広告枠も兼ねる。',
    },
  },
  {
    id: 'prop.parking-meter',
    category: 'street-prop',
    icon: '🅿️',
    name: { 'zh-CN': '停车计时器', en: 'Parking Meter', ja: 'パーキングメーター' },
    desc: {
      'zh-CN': '路边停车计时咪表，管理路侧临时停车位的周转与收费。',
      en: 'Curb-side parking meters that manage turnover and fees for short-stay on-street spaces.',
      ja: '路上の時間制パーキングメーター。路側の一時駐車スペースの回転と課金を管理する。',
    },
  },
  {
    id: 'prop.sign',
    category: 'street-prop',
    icon: '🪧',
    name: { 'zh-CN': '路牌标识', en: 'Street Sign', ja: '道路標識' },
    desc: {
      'zh-CN': '交通指示与街道标识牌，指引车流与行人方向，是道路系统的导视层。',
      en: 'Traffic and street signs guiding cars and pedestrians — the wayfinding layer of the road system.',
      ja: '車両と歩行者の方向を示す交通・案内標識。道路システムの案内レイヤー。',
    },
  },
  {
    id: 'prop.solar-panel',
    category: 'street-prop',
    icon: '🔆',
    name: { 'zh-CN': '屋顶太阳能板', en: 'Rooftop Solar Panel', ja: '屋上ソーラーパネル' },
    desc: {
      'zh-CN': '屋顶光伏发电板，为建筑分担部分用电，是城市清洁能源的分布式节点。',
      en: 'Rooftop PV panels offsetting a building’s power draw — distributed clean-energy nodes across the city.',
      ja: '屋上の太陽光パネル。建物の電力を一部まかない、都市のクリーンエネルギー拠点となる。',
    },
  },
  {
    id: 'prop.rooftop-acc',
    category: 'street-prop',
    icon: '🌀',
    name: { 'zh-CN': '楼顶设备', en: 'Rooftop Equipment', ja: '屋上設備' },
    desc: {
      'zh-CN': '楼顶设备群——空调外机、水箱与卫星天线，维系建筑的机电与通信运转。',
      en: 'Rooftop kit — AC units, water tanks and satellite dishes — keeping the building’s services running.',
      ja: '屋上の設備群。エアコン室外機、受水槽、衛星アンテナが建物の設備と通信を支える。',
    },
  },
  {
    id: 'prop.bus-stop',
    category: 'street-prop',
    icon: '🚏',
    name: { 'zh-CN': '公交站台', en: 'Bus Stop', ja: 'バス停' },
    desc: {
      'zh-CN': '主干道公交站台，带顶棚与站牌，是居民通勤出行的重要换乘节点。',
      en: 'Sheltered bus stops on main roads — key transfer nodes for residents’ daily commutes.',
      ja: '幹線道路沿いの屋根付きバス停。住民の通勤動線の重要な乗換拠点。',
    },
  },

  // ── 树木 ─────────────────────────────────────────────────────────
  {
    id: 'tree.road',
    category: 'tree',
    icon: '🌳',
    name: { 'zh-CN': '行道树', en: 'Street Tree', ja: '街路樹' },
    desc: {
      'zh-CN': '沿道路成排行道树，遮荫降噪、滞尘净化，构成城市绿廊骨架。',
      en: 'Rows of street trees that shade, quiet and clean the air, forming the skeleton of the city’s green corridors.',
      ja: '道路沿いに並ぶ街路樹。日よけと騒音低減、大気浄化を担う都市の緑廊骨格。',
    },
  },
  {
    id: 'tree.park',
    category: 'tree',
    icon: '🌲',
    name: { 'zh-CN': '园林树', en: 'Park Tree', ja: '公園樹木' },
    desc: {
      'zh-CN': '城区与公园内的园林树木，随季节变换树冠色彩，是居民身边的自然景观。',
      en: 'Garden trees in districts and parks whose crowns change colour with the seasons — nature on residents’ doorsteps.',
      ja: '街区や公園の園芸樹木。季節ごとに樹冠の色を変え、住民の身近な自然景観となる。',
    },
  },

  // ── 车辆 ─────────────────────────────────────────────────────────
  {
    id: 'vehicle.sedan',
    category: 'vehicle',
    icon: '🚗',
    name: { 'zh-CN': '轿车', en: 'Sedan', ja: '乗用車' },
    desc: {
      'zh-CN': '城市道路上行驶的私人轿车，车流的主力车型，承载居民通勤与出行。',
      en: 'Private sedans — the workhorses of city traffic carrying residents to work and around town.',
      ja: '都市の道路を走る自家用乗用車。車流の主力で、住民の通勤と移動を担う。',
    },
  },
  {
    id: 'vehicle.truck',
    category: 'vehicle',
    icon: '🚚',
    name: { 'zh-CN': '货车', en: 'Truck', ja: 'トラック' },
    desc: {
      'zh-CN': '物流货运卡车，服务港区与产业基地的物资集散，昼夜穿梭于主干道。',
      en: 'Freight trucks serving ports and industrial parks with around-the-clock hauls along the main roads.',
      ja: '港区や産業基地の物資集散を支える貨物トラック。昼夜を問わず幹線道路を走る。',
    },
  },
  {
    id: 'vehicle.bus',
    category: 'vehicle',
    icon: '🚌',
    name: { 'zh-CN': '公交车', en: 'City Bus', ja: '路線バス' },
    desc: {
      'zh-CN': '城市公共汽车，沿主干道按站停靠，是大运量公共交通的骨干。',
      en: 'City buses stopping along the main roads — the backbone of mass public transport.',
      ja: '幹線道路に沿って停留所を回る路線バス。大量公共交通の骨格。',
    },
  },
  {
    id: 'vehicle.taxi',
    category: 'vehicle',
    icon: '🚕',
    name: { 'zh-CN': '出租车', en: 'Taxi', ja: 'タクシー' },
    desc: {
      'zh-CN': '巡游出租车，扬招即停，为居民提供门到门的灵活出行服务。',
      en: 'Cruising taxis that stop on the hail, giving residents flexible door-to-door rides.',
      ja: '流しのタクシー。手を挙げれば停まり、住民に柔軟なドア・トゥ・ドアの移動手段を提供する。',
    },
  },

  // ── 角色 ─────────────────────────────────────────────────────────
  {
    id: 'actor.pedestrian',
    category: 'pedestrian',
    icon: '🚶',
    name: { 'zh-CN': '行人', en: 'Pedestrian', ja: '歩行者' },
    desc: {
      'zh-CN': '城市街道上的行人——通勤、购物与散步的居民，构成街区的活力流线。',
      en: 'People walking the streets — commuting, shopping, strolling — the living flow that animates each block.',
      ja: '街路を歩く人々。通勤・買い物・散歩の住民が街区の活気ある流線をつくる。',
    },
  },
  {
    id: 'actor.player',
    category: 'agent',
    icon: '🎯',
    name: { 'zh-CN': '玩家代币', en: 'Player Token', ja: 'プレートークン' },
    desc: {
      'zh-CN': '本局玩家在城市中的化身标记，落在所居城区，可查看其职业、昵称与资产概况。',
      en: 'The in-city avatar marker of a player, sitting in their home district; inspect it for profession, nickname and assets.',
      ja: '本局プレイヤーの街中アバター。居住区に置かれ、職業・ニックネーム・資産概況を確認できる。',
    },
  },

  // ── 道路 ─────────────────────────────────────────────────────────
  {
    id: 'road.surface',
    category: 'road',
    icon: '🛣️',
    name: { 'zh-CN': '城市道路', en: 'City Road', ja: '都市道路' },
    desc: {
      'zh-CN': '沥青铺装的城市道路，划有行车标线，由一环路、方格骨干与邻接次干道连通全城路网。',
      en: 'Asphalt roads with painted lane markings, linked city-wide by the ring road, grid arterials and district connectors.',
      ja: 'アスファルト舗装の都市道路。車線標線を引き、環状路・格子幹線・区内連絡路で全市を結ぶ。',
    },
  },
  {
    id: 'road.ring',
    category: 'road',
    icon: '⭕',
    name: { 'zh-CN': '环形路', en: 'Ring Road', ja: '環状道路' },
    desc: {
      'zh-CN': '金融 CBD 外围的环形道路，环绕中央商务区，分流核心区车流。',
      en: 'The ring road around the financial CBD, diverting through-traffic and linking surrounding blocks.',
      ja: '金融CBDを囲む環状道路。中心部の車流を分散させ、周辺街区をつなぐ。',
    },
  },
  {
    id: 'road.first-ring',
    category: 'road',
    icon: '🛞',
    name: { 'zh-CN': '一环路', en: 'First Ring Road', ja: '一環路' },
    desc: {
      'zh-CN': '城市核心区的一环路，环绕内城各区，与放射主干道互通并设信号灯控，是城区的交通骨架。',
      en: 'The First Ring Road encircling the inner city, interchanging with radial arterials under signal control — the backbone of urban traffic.',
      ja: '内城を取り巻く一環路。放射主道と信号連動で接続され、市街交通の基幹を成す。',
    },
  },
  {
    id: 'road.gate',
    category: 'road',
    icon: '🚧',
    name: { 'zh-CN': '高速收费站', en: 'Highway Toll Gate', ja: '高速料金所' },
    desc: {
      'zh-CN': '城市出入口的高速收费站，龙门架横跨联络线，右侧双收费亭，连通城区与环城高速。',
      en: 'Toll gates at the city entrances: a gantry spans each connector road with twin booths, linking the city to the ring expressway.',
      ja: '都市の出入り口にある高速料金所。ゲートが連絡線を跨ぎ、右側に二つの料金ブースが並び、市街と環状高速を結ぶ。',
    },
  },
  {
    id: 'road.bridge',
    category: 'road',
    icon: '🌉',
    name: { 'zh-CN': '运河桥', en: 'Canal Bridge', ja: '運河橋' },
    desc: {
      'zh-CN': '跨越城市运河的桥梁，连接被水系分隔的南北城区，桥侧设路灯。',
      en: 'Bridges over the city canal linking districts split by the water, lined with their own street lamps.',
      ja: '都市運河を渡る橋。水系で分断された南北の街区を結び、橋側に街路灯が並ぶ。',
    },
  },
  {
    id: 'road.street-light',
    category: 'road',
    icon: '💡',
    name: { 'zh-CN': '路灯', en: 'Street Light', ja: '街路灯' },
    desc: {
      'zh-CN': '道路照明灯杆，入夜自动点亮，保障车行与人行的夜间安全。',
      en: 'Road lighting poles that switch on at dusk to keep drivers and pedestrians safe after dark.',
      ja: '道路照明の灯柱。日没とともに点灯し、夜間の車両・歩行者の安全を守る。',
    },
  },
  {
    id: 'road.traffic-signal',
    category: 'road',
    icon: '🚦',
    name: { 'zh-CN': '交通信号灯', en: 'Traffic Signal', ja: '信号機' },
    desc: {
      'zh-CN': '路口红绿灯，按相位轮转红黄绿，指挥车流有序通行。',
      en: 'Intersection signals cycling red, amber and green to keep traffic flowing in order.',
      ja: '交差点の信号機。赤・黄・緑を位相で切り替え、車流を秩序よく導く。',
    },
  },
  {
    // 批次 43 新增：与 road.traffic-signal 反相联动（车绿 ⇒ 人红）。
    id: 'road.pedestrian-signal',
    category: 'road',
    icon: '🚶',
    name: { 'zh-CN': '行人信号灯', en: 'Pedestrian Signal', ja: '歩行者信号機' },
    desc: {
      'zh-CN': '路口人行过街信号，与车行信号反相联动：车放行时禁行，车停时放行。',
      en: 'Pedestrian crossing signals interlocked with the vehicle ones: stop when traffic moves, walk when it stops.',
      ja: '歩行者用信号。車用信号と逆位相で連動し、車が通行中は赤、停止中は緑。',
    },
  },
  {
    // 批次 43 新增：宽路口专用（GB 14886 §4.3）。
    id: 'road.mast-arm-signal',
    category: 'road',
    icon: '🏗️',
    name: { 'zh-CN': '悬臂式信号灯', en: 'Mast-arm Signal', ja: '門柱式信号機' },
    desc: {
      'zh-CN': '宽路口专用的横杆式信号灯：立柱在路口外缘，横臂伸到车流上方，让远处驾驶员提前看到信号。',
      en: 'Mast-arm signals for wide junctions: the column stands at the kerb while the arm reaches over the traffic so drivers see it early.',
      ja: '広い交差点用の門柱式信号。柱は路端に立ち、腕が車流の上まで伸び、遠方の運転手にも早い段階で信号が見える。',
    },
  },
];
