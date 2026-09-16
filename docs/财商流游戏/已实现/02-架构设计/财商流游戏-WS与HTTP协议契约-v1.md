# 财商流游戏 — WS 与 HTTP 协议契约 v1

> **定位**：`game_kind = wealth` 的线上协议**逐字段契约**。前端 `src/types/wealth.ts` 必须与本文
> `game.state` 载荷**逐字段对齐**；后端 `game/wealth/view.go` 是本文的唯一实现。
> 数值语义出处：《规则与经济系统》（下称《规则》）§x；「P0 新定」= 本简报新增。
>
> WS Envelope 与全平台一致：`{type, payload}`（JSON over WSS `wss://HOST:39001/ws?token=<jwt>`）。

---

## 目录

1. [C→S 帧总表](#1-cs-帧总表)
2. [S→C 帧总表](#2-sc-帧总表)
3. [`game.state` 载荷逐字段契约](#3-gamestate-载荷逐字段契约)
4. [动作语义表（`game.wealth_action` / Agent 工具共用）](#4-动作语义表)
5. [错误码（350xx 段）](#5-错误码)
6. [HTTP API](#6-http-api)
7. [脱敏与安全](#7-脱敏与安全)

---

## 1. C→S 帧总表

| 帧 | payload | 说明 |
|----|---------|------|
| `game.join` | `{room_id, game_kind:"wealth"}` | 入座（幂等）；进行中对局允许中途加入 |
| `game.leave` | `{room_id}` | 退出座位；进行中转 bot 接管（有可用 provider 时） |
| `game.spectate` | `{room_id, game_kind:"wealth"}` | 注册观战者（不消耗座位） |
| `game.unspectate` | `{room_id}` | 退出观战 |
| `game.state` | `{room_id, game_kind:"wealth"}` | 请求全量快照（断线恢复 / 手动刷新） |
| `game.wealth_start` | `{room_id}` | 房主提前开始（已占座 ≥3；满 8 自动开局无需此帧） |
| `game.wealth_action` | `{room_id, action:{type, …}}` | 人类动作；`action` 字段见 §4；观战者发送 → errcode 30011 |
| `game.wealth_pause` | `{room_id, pause:true\|false, reason?}` | 房主暂停/恢复（月结完成后生效） |

`gameKindProbe`（`ws/game_service.go`）探测表新增 `case "wealth": return "wealth"`。
`game.wealth_action` / `game.wealth_start` / `game.wealth_pause` 三帧在 `HandleClientFrame`
switch 中路由到 `ws/game_service_wealth.go`。

## 2. S→C 帧总表

| 帧 | 触发时机 | payload |
|----|---------|---------|
| `game.joined` | 入座成功 | `{my_seat, month, phase}` |
| `game.started` | 开局 | `{month:1, age, start_age, professions:[{seat,profession_id}]}`（职业对全房公开——设定层职业非隐藏信息） |
| `game.state` | 每月 / 每次请求 | 全量快照，见 §3；**按座位脱敏**，经 `hub.BroadcastTo(uid,…)` 单发 |
| `game.event` | 单个事件 | `{room_id, month, seat?, type:"action"\|"move"\|"settle"\|"market"\|"life"\|"survey"\|"chat"\|"error", text, data?}` |
| `game.month` | 每月结算后（BroadcastRoom 全房同帧） | `{room_id, month, age, summaries:[{seat, cash_delta, net_worth, fi_index, note}], market_changes:{stock_index, gold_price, bond_rate, house_idx:{district:Δ}, cpi?:number, unemployment_rate?:number}, events:[…]}` |
| `game.survey_result` | 调研关闭（deadline / 全员已答） | `{room_id, survey:{id, question, options, launch_month, deadline_month, status, answers_count, result?:{options, counts, percents, total, top_reasons}}}`（BroadcastRoomIncludingSpectators，含观战者） |
| `game.over` | 60 岁终局 | `{room_id, scores:[{seat, fi_score, life_score, social_score, total, ending}], report}` |
| `game.error` | 操作错误 | `{code, message}`（seq 回带） |
| `game.removed` | 终局 60s 后房间清理 | `{room_id}` |

`game.event.type` 语义：`action`=某人动作回执 ｜ `move`=迁区 ｜ `settle`=月结（仅发本人明细）
｜ `market`=周期切换 / 大幅波动 ｜ `life`=人生事件 ｜ `chat`=聊天回执 ｜ `error`=引擎拒绝。

---

## 3. `game.state` 载荷逐字段契约

（字段名**一字不改**；`view.go::BuildClientState` 产出；类型标注 TS 风格）

```jsonc
{
  room_id: string,
  game_kind: "wealth",
  status: "open" | "playing" | "over",
  month: number,            // 1..420
  age: number,              // 主时钟年龄（开局基准 + floor((month-1)/12)）
  phase: "acting" | "settling",
  cycle: {
    phase: "recovery" | "boom" | "recession" | "depression",
    lpr: number,            // 0.035 等（小数，非百分数）
    cpi: number,            // 0.02 等
    months_left: number     // 阶段剩余月（含钟声重掷的不确定性，仅展示）
  },
  market: {
    stock_index: number,    // 元/份（初始 3.50）
    gold_price: number,     // 元/克（初始 750）
    bond_yield: number,     // 当期新购债券年化（小数，如 0.032）
    districts: [            // 8 项，顺序 = DistrictDefs 静态表
      { id: "finance", price_index: number, rent_index: number }  // price_index=idx_d, rent_index=idx_d×0.0016 归一
    ]
  },
  max_seat: 8,
  next_month_at: number,    // unix_ms；前端倒计时 = next_month_at − now
  game_started_at: number,  // unix_s；与狼人杀 RoomRunningClock 同源语义
  players: [{
    seat: number,           // 0..7
    account: string,        // 账号（bot 为 bot_<modelkey>）
    nickname: string,
    is_bot: boolean,
    model_display: string,  // bot 的 agent_name；人类为 ""
    profession: { id: "P01", title: "外卖骑手", avatar: "p01" },  // avatar = 职业头像文件名主干
    district: string,       // 当前所在区 id
    home_district: string,  // 住房所在区 id
    alive: boolean,
    retired: boolean,       // P0 恒 false（P1 提前退休预留）
    age: number,            // 个人年龄（文档池差异卡展示用）
    resources: { energy: number, network: number, cognition: number },
    net_worth: number,      // 元（公开）
    fi_index: number,       // 公开（0–2 封顶）
    income_band: "low" | "mid" | "high" | "top",  // 月总收入 <8000/8000–20000/20000–50000/>50000（《规则》§5.3）
    status_icon: "working" | "idle" | "trading" | "resting" | "moved",  // 本月最近动作类别
    last_action: string,    // 人读，如 "买入黄金 50g"；空 = 本月未动作
    ending: string,         // 终局结局 id；进行中为 ""
    consumption_level: number  // P1: 消费档位 0 节俭/1 标准/2 精致/3 奢侈（公开生活方式）
  }],
  my_seat: number,          // -1 = 观战
  my: null | {              // 仅本人座位填充；观战者为 null
    cash: number,
    salary: number,         // 当前基准月薪（税前）
    spouse_income: number,  // 税后净额
    side_income: number,    // 上月副业净收入
    passive_income: number, // 上月被动收入合计
    monthly: {              // 最近一次月结（或当月预估）
      income: number, expense: number, net: number,
      tax: number, social: number,
      detail: [{ key: "salary" | "tax" | "living" | …, amount_cny: number, text: string }]
    },
    resources: { energy: number, network: number, cognition: number },
    assets: [{              // 持仓列表
      kind: "stock_index" | "bond" | "gold" | "house:<district>" | "shop:<district>" | "side_business" | "pension",
      name: string,         // 人读名，如 "指数基金" "老城区住宅"
      units: number,        // 份/克/套/间
      price: number,        // 当前单价
      value_cny: number,    // 市值
      monthly_flow_cny: number  // 月现金流（租金+/月供−；正为流入）
    }],
    loans: [{ id: "L3", kind: "mortgage" | "consumer" | "credit_tier1" | "credit_tier2" | "credit_tier3" | "business",
              principal: number, balance: number, annual_rate: number,
              monthly_payment: number, months_left: number }],
    pension_cny: number,    // 养老金账户余额
    credit_score: number,   // 400–850
    family: { marital: "single" | "married", children: number },
    fi_index: number,
    net_worth: number,
    goals: [string],        // 职业卡 goals（含 5 年目标），终局对照展示
    consumption_by_goods: { [id: string]: number }  // P1: 本人上月八大类消费拆分（元）
  },
  bot_contexts: [{          // Agent 思维可见性：本人座位 + 观战者可见；其他玩家不可见
    seat: number,
    last_decision_summary: string,   // Agent 自述本月决策（≤120 字）
    last_tool_input: string,         // JSON 字符串
    last_tool_result: string,        // 人读结果
    heart_thought: string            // 内心独白（speak 的 internal_thought）
  }],
  ledger_recent: [{ month: number, from: string, to: string,
                    amount_cny: number, category: string, note: string }],  // 最近 50 条：本人相关 + 公共
  events_recent: [{ month: number, type: string, text: string }],          // 最近 100 条
  // ── P1 真实经济循环（economy_enabled=false 时为零值/空数组）──
  consumer_market: { cpi_yoy: number, cpi_mom: number,                    // 内生 CPI 同比/环比
    goods: [{ id: string, weight: number, price_idx: number, mom_change: number }] },  // 八大类
  labor_market: { unemployment_rate: number, employment_ratio: number,     // 内生失业率/就业率
    avg_wage_growth_yoy: number, firm_revenue_cny: number, layoff_wave: number },  // Phillips 工资/企业营收/裁员潮
  society: { gini: number, quintiles: [number ×5],                        // 基尼/五等份
    circles: { survival: number, accumulate: number, freedom: number } }, // 三圈层分布
  surveys: [{ id: string, question: string, options: [string],            // 调研（open ≤1 + 最近 4 closed）
    launch_month: number, deadline_month: number, status: "open"|"closed",
    answers_count: number,
    result?: { options: [string], counts: [number], percents: [number], total: number, top_reasons: [string] } }]
}
```

---

## 4. 动作语义表

`game.wealth_action{room_id, action:{type, …}}` 与 Agent 的 17 工具（`speak`/`check_state` 除外）
**共用同一套引擎 action 语义**——本表是人类按钮、Agent 工具、`actions.go` 校验三方的唯一事实来源。

通用校验（所有 action）：① `status=="playing"` 且 `phase=="acting"`（错误码 35002/35004）；
② 本人 `alive==true` 且 `StoppedMonths==0`（35005）；③ `ActionBudget>0`（35006，`submit_month`
不耗预算）；④ 观战者拒收（errcode 30011）。

| type | 参数 | 校验 | 费用/效果 | Ledger |
|------|------|------|-----------|--------|
| `check_state` | 无 | 仅 Agent | 返回本人摘要文本（人类用面板，不发此帧） | — |
| `buy_asset` | `{asset:"stock_index"\|"bond"\|"gold", amount_cny}` | 金额 ≥1000（gold ≥1 克价）；现金足额 | 按市价整份成交；佣金 0.025%（min 5 元，仅 stock）；bond 锁定当期利率 | `seat→market`（买价+费） |
| `sell_asset` | `{asset:"stock_index"\|"bond"\|"gold"\|"house:<d>"\|"shop:<d>", units}` | 持仓足额；房产整售 units=1 | stock 佣金 0.025%(min5)；gold 费 0.5%；房/铺 增值税 5%+中介 2%（满 5 年唯一免）；卖房先偿房贷 | `market→seat`（净额）+`seat→gov`（税）+`seat→market`（费） |
| `buy_house` | `{district, downpay_ratio}` | ratio ∈[0.3,1.0]；持有上限住宅 4/商铺 4；现金 ≥ 首付；信用分 ≥600（贷款时） | 房价 = §4 公式；首付现金支付；余额 30 年房贷（LPR+0.5%+信用上浮）等额本息 | `seat→market`（首付）；`bank→seat`（贷款）；asset 记账 |
| `take_loan` | `{kind:"consumer"\|"credit"\|"business", amount_cny}` | credit 档位必须是 50000/100000/200000 之一；consumer ≤ min(月收入×12, 20 万)；business 需运行中副业且 ≤20 万；信用分门槛（§7） | 款项入现金；Brass 档副作用即刻生效 | `bank→seat` |
| `repay_loan` | `{loan_id, amount_cny}` | loan 存在；提前还本 ≥10000 或结清；现金足额 | 先扣当期利息再还本；等额本息重算月供；Brass 档恢复判定 | `seat→bank` |
| `start_side_business` | `{kind:"delivery"\|"content"\|"tutoring"\|"freelance"}` | 无副业运行中；门槛：delivery 无 / content 认知≥2 / freelance 认知≥3 / tutoring 认知≥4 | 启动费 0；月收入档：delivery 2500–4000 / content 2000–6000(高波动) / freelance 3000–6000 / tutoring 3000–5000；月耗精力 2 | 月结 `market→seat` |
| `stop_side_business` | 无 | 有副业 | 停止，无残值；精力释放 | — |
| `study` | 无 | 认知 <10；精力 ≥1；现金 ≥2000 | 现金-2000；精力-1；认知+1（《规则》§2.6） | `seat→world`（`study`） |
| `socialize` | 无 | 人脉 <10；现金 ≥1000 | 现金-1000；人脉+1（《规则》§2.5） | `seat→world`（`social`） |
| `rest` | 无 | 精力 <10 | 精力 +2（P0 新定） | — |
| `work_overtime` | 无 | 精力 ≥2；在职 | 精力-2；当月工资 ×0.3 奖金（随月结发放）（P0 新定） | 月结 `bank→seat`（`overtime`） |
| `move_district` | `{district}` | district ≠ 当前；现金 ≥3000；精力 ≥1 | 现金-3000；精力-1；token 迁移；若目标区有自住房自动改自住（P0 新定） | `seat→world`（`moving`） |
| `consume` | `{amount_cny, reason?}` | 金额 ≥1；现金足额 | 纯消费（记事）；无机制效果；P1 计入 ConsumptionByGoods["misc"] | `seat→firms`（`consume`，economy_enabled=true 时；false 回退 to=world） |
| `donate` | `{amount_cny}` | 金额 ≥1000；现金足额 | 累计入社会贡献分（每万 1 分）；人脉+1（累计前 3 次，《规则》§2.5） | `seat→world`（`donate`） |
| `set_consumption` | `{level:0\|1\|2\|3}` | level ∈[0,3] | 调整消费档位（节俭/标准/精致/奢侈），本月月结按新档位结算（支出乘数 0.6/1.0/1.5/2.2，精力效果 −1/0/+1/+2）| — |
| `speak` | `{text}` | 仅 Agent（`chatSvc.SendFromBot`/`WhisperFromBot`）；≤100 字截断；SpeakLimiter 节流 | 公屏发言（`game.event type:"chat"`） | — |
| `submit_month` | 无 | 本月未提交 | 标记 Submitted；不耗动作预算；全员提交 → 提前进入月结 | — |

动作执行即时返回：成功 → `game.event{type:"action"|"move", seat, text}` 广播全房 + 本人
`game.state` 单发刷新；失败 → `game.error{code:350xx}`（seq 回带）+ `game.event{type:"error"}`。

---

## 5. 错误码

沿用 `errcode/errcode.go` 全局表风格（5 位、3 开头 = 资源/游戏类）。**wealth 专用段 35001–35012**
（`errcode.go` 新增常量 + `DefaultMessages`；命名前缀 `ErrWealth*`）：

| 码 | 常量 | 默认英文消息 | 触发 |
|----|------|-------------|------|
| 35001 | `ErrWealthRoomNotFound` | `wealth room not found` | manager 无此房间 |
| 35002 | `ErrWealthNotPlaying` | `wealth game not in playing state` | status != playing |
| 35003 | `ErrWealthNotEnoughPlayers` | `wealth game needs at least 3 seated players` | 提前开始 <3 座 |
| 35004 | `ErrWealthWrongPhase` | `wealth action only allowed in acting phase` | settling 期发动作 |
| 35005 | `ErrWealthPlayerInactive` | `wealth player is stopped/bankrupt/eliminated` | 停赛/出局者动作 |
| 35006 | `ErrWealthActionBudgetExhausted` | `wealth monthly action budget exhausted` | 本月 3 动作已用完 |
| 35007 | `ErrWealthInsufficientCash` | `wealth insufficient cash` | 现金不足 |
| 35008 | `ErrWealthAssetInvalid` | `wealth asset/units invalid` | 资产种类/份额/上限非法 |
| 35009 | `ErrWealthLoanInvalid` | `wealth loan kind/amount/credit gate invalid` | 贷款档位/额度/信用分不满足 |
| 35010 | `ErrWealthGateFailed` | `wealth cognition/energy/network gate failed` | 副业/学习等门槛不满足 |
| 35011 | `ErrWealthNotOwner` | `wealth operation requires room owner` | 非房主 start/pause |
| 35012 | `ErrWealthProfessionPoolEmpty` | `wealth profession pool unavailable` | docs 池路径不可读且回退也失败 |
| 35016 | `ErrWealthSurveyOptionsInvalid` | `wealth survey options must be 2-6 non-empty, or answer option_index out of range` | 选项数非 2–6 / 选项文本空 / 作答 option_index 越界 |
| 35017 | `ErrWealthSurveyOpenExists` | `wealth a survey is already open in this room` | 已有进行中调研（每房同时 1 个 open）|
| 35018 | `ErrWealthSurveyMonthlyLimit` | `wealth survey launch limit reached (1/month, 20 max per room)` | 本月已达发起上限 / 累计 20 个上限 |
| 35019 | `ErrWealthSurveyNotFound` | `wealth survey not found, closed, or already answered` | 调研不存在 / 已关闭 / 本人已回答 |
| 35020 | `ErrWealthConsumptionLevelInvalid` | `wealth consumption level must be 0-3` | 消费档位非法（须 0–3）|

通用码复用：`30011` 观战者输入禁止 ｜ `30002` 房满 ｜ `30001` 房间不存在 ｜ `20001` 参数非法（payload 解析失败 / DisallowUnknownFields 命中）。

---

## 6. HTTP API

复用参数化路由（`api/room_api.go`；`DisallowUnknownFields` 严格校验）：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/games/wealth/rooms` | GET | 列出开放房间（复用 `ListRoomsForUser`） |
| `/api/games/wealth/rooms` | POST | 建房。body 在 `createRoomRequest` 上**新增字段** `wealth *WealthRoomOptions`：`{month_ms?: number, pool?: "curated"\|"docs", seed?: number}`；`month_ms` clamp 3000–30000；`agent_seats` 复用狼人杀 `CreateRoomWithAgents` 流程（§14.2 model_key 去重自动生效；`MaxAgentSeats=8`） |
| `/api/rooms/:id/join` | POST | 加入（复用） |
| `/api/rooms/:id/leave` | POST | 离开（复用） |
| `/api/rooms/:id` | GET | 详情（复用） |
| `/api/rooms/:id/spectate` | POST | 观战（复用，§19.5） |
| `/api/rooms/:id/leave_spectate` | POST | 退出观战（复用） |
| `/api/games/wealth/professions` | GET（需登录） | **新增**。返回 `{curated:[…精选手卡], pool:{available: bool, total: number, indexed: number}}`；`curated` 每项 = 职业卡公开字段（id/title/salary/expense/初始值/opening_hook/goals）；`pool.total` = 文档池卡总数（懒加载前可为估算），`indexed` = 已建索引数；池不可用时 `available=false`。职业卡数据结构见职业卡加载器文档 |
| `/api/games/wealth/rooms/:id/survey` | POST（需登录） | P1 新增。发起调研。body `{question: string(≤100 rune), options: []string(2-6, 各≤40 rune)}`；返回 `{survey: SurveyJSON}`；限流（同时 1 个 open / 每月 1 个 / 累计 20）；`survey_enabled=false` → 35010 |
| `/api/games/wealth/rooms/:id/surveys` | GET（需登录） | P1 新增。返回 `{surveys: [SurveyJSON]}`（全部历史 ≤20）|

**与 config 的关系**：`pool:"docs"` 的磁盘根 = `cfg.Wealth.ProfessionDocsPath`
（json 键 `profession_docs_path`，默认 `./docs/财商流游戏/玩家职业设计`）；请求级 `pool` 覆盖
`cfg.Wealth.ProfessionPoolDefault`。Agent 座位（`LsmAgentGame-Wealth-Player`）的 System prompt
同样从该路径加载文档池画像（详见 Agent 设计文档 §4）。

---

## 7. 脱敏与安全

1. `players[]` 全公开（净资产 / 收入档 / 资源 / 位置 / 职业）——财商流是**公开经济信息博弈**，
   隐藏信息仅 `my.*`（服务端权威下发，不走 BroadcastRoom 逐字段广播，`view.go` 按座位产出）。
2. `my` 仅本人座位填充；观战者 `my=null, my_seat=-1`。
3. `bot_contexts`：本人座位 + 观战者可见（思维透明是平台卖点）；其他玩家不可见（`view.go` 过滤）。
4. 动作帧服务端全量校验（§4 表），前端禁用态仅为 UX，不是安全边界。
5. 观战者发送任何 `game.wealth_action` → `ErrSpectatorInputForbidden`(30011)，硬拒（§19.5）。

---

## 更新日志

- v1（2026-09-14）：首版。C→S / S→C 帧总表、`game.state` 逐字段契约、17 动作语义表、350xx 错误码、HTTP API、脱敏规则。
