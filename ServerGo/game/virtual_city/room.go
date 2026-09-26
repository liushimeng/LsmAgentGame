// Package virtual_city — room.go: VirtualCityRoom(锁 + 月度 tick + 座位 + 广播回调)
// 2026-09-14 §财商流P0。
//
// 契约: 后端架构文档 §14(月度 tick 时序)。锁纪律 §92a:所有广播回调在
// **释放 r.mu 之后**调用(texasholdem §B6 同款「持锁结算 → 锁外回调」);
// BuildClientState 走纯函数快照,不回调房间方法。
package virtual_city

import (
	"context"
	"fmt"
	"math/rand"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"LsmAgentGame/agent/vcplayer"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/city"
	"LsmAgentGame/game/virtual_city/profession"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"
	"LsmAgentGame/service"

	"go.uber.org/zap"
)

// BotTranscript 单座位 Agent 思维可见性(game.state.bot_contexts)。
type BotTranscript struct {
	Month               int
	LastDecisionMonth   int
	UpdatedAt           int64
	Active              bool
	LastDecisionSummary string
	LastToolInput       string
	LastToolResult      string
	HeartThought        string
	// LastSenses 最近感知记录(bot_contexts[].last_senses;2026-09-22
	// §CityHuman重构,see/hear/smell 工具结果摘要,最多保留 3 条)。
	LastSenses []SenseEntry
}

// SenseEntry 单条感知记录(bot_contexts[].last_senses 元素)。
type SenseEntry struct {
	Month int    `json:"month"`
	Kind  string `json:"kind"` // see|hear|smell
	Text  string `json:"text"` // 人读摘要(≤160 字)
}

// UtteranceRecord 单条公开发言记录(hear 感知数据源;房间内 ring, cap 50)。
type UtteranceRecord struct {
	Month    int
	Seat     int // -1 = 背景居民(城市之声)
	District string
	Text     string
}

// ChatSender bot 公屏发言通道(ws.ChatService 适配器注入;nil-safe)。
type ChatSender interface {
	SendFromBot(roomID, botUserID, botAccount, modelKey, text string) error
}

// ChatWhisperer bot 私聊(耳语)通道(2026-09-22 §CityHuman重构;
// 可选接口 —— chatSender 实现了才开放 speak scope=private,否则 35103)。
// 参考狼人杀 ws.ChatService.WhisperFromBot 路径:仅目标座位与观战者可见。
type ChatWhisperer interface {
	WhisperFromBot(roomID, botUserID, botAccount, modelKey, toUserID, toAccount, text string) error
}

// BroadcastHooks ws 层广播钩子(全部在锁外调用)。
type BroadcastHooks struct {
	OnEvent   func(roomID string, ev EventRecord)         // game.event
	OnMonth   func(roomID string, res *SettleResult)      // game.month(BroadcastRoom)
	OnState   func(roomID string)                         // game.state 逐座位单发
	OnOver    func(roomID string, scores []FinalScore)    // game.over
	OnStarted func(roomID string, payload map[string]any) // game.started
	OnRemoved func(roomID string)                         // game.removed(终局 60s 后)
	// OnSurvey 调研关闭广播(P1 §财商流P1-2 调研契约 §4.4):deadline 到期
	// (SettleResult.ClosedSurveys)或全员已答提前关闭两条路径均在锁外触发
	// → ws 层发 game.survey_result。
	OnSurvey func(roomID string, sv *Survey)
}

// VirtualCityRoom 单房间运行时。
type VirtualCityRoom struct {
	mu     sync.Mutex
	RoomID string

	OwnerID string

	Seats         [MaxSeats]string
	SeatModelKeys [MaxSeats]string
	BotSeats      [MaxSeats]bool
	Nicknames     [MaxSeats]string
	Spectators    map[string]struct{}
	IdleSeats     map[int]bool // 人类中途离房 → 自动挂机(P0;bot 接管为 P1)

	World *World

	Status string // open | playing | over
	Phase  string // acting | settling

	MonthMs     int
	NextMonthAt time.Time
	Paused      bool

	Transcripts [MaxSeats]BotTranscript
	// Agent 月度调度槽:expected month + active 构成房间级 token。旧月 LLM
	// 响应必须同时匹配 active 与 World.Month 才能执行动作/提交。
	agentDecisionActive [MaxSeats]bool
	agentDecisionMonth  [MaxSeats]int

	// 卡池与随机源(房间级;开局抽卡用)。
	// 2026-09-22 §17-CityHuman:pool 字段退役 —— 发卡恒走文档池(docLoader),
	// 不可用时 SyntheticCards 合成兜底(契约 03 §2.1,没有非 docs 模式)。
	seed      int64
	rng       *rand.Rand
	docLoader *profession.Loader // Manager 注入(可能为 nil → 合成兜底)

	hooks      BroadcastHooks
	chatSender ChatSender
	agents     map[int]*vcplayer.Agent
	agentSem   chan struct{} // 房间级 LLM 并发信号量(默认 DefaultAgentConcurrency=8)
	eventsSent int           // World.Events 已下发条数
	cardPool   []profession.Card
	// cardPoolPaths 批次 25 persona 统一:与 cardPool 按序对应的卡来源相对路径
	// (文档池卡 = 真实路径;合成兜底卡 = "")。N≤12 时档案锚定复用同一批路径,
	// 使背景居民 i 的人物卡 = 座位 i 的人物卡(消掉双 rng 流)。
	cardPoolPaths    []string
	cardPoolIdx      int
	openingHooksSent bool

	gameStartedAt int64 // 开局 unix s(view 下发 game_started_at 字段)

	// P1(§财商流P1-2 §6.5):economy/survey 房间级开关(默认 true;
	// Manager.CreateRoom 按 Manager.Config 调 SetEconomyFlags 回写)。
	economyEnabled bool
	surveyEnabled  bool
	// P1-4(2026-09-19 §财商流P1-4 §11):商业保险引擎开关(默认 true;
	// Manager.CreateRoom 按 Manager.Config 调 SetInsuranceEnabled 回写)。
	insuranceEnabled bool
	// electionEnabled 批次20(文档3 A2):市长选举房间级开关。
	// **默认 false(零值)= 关闭** —— 与 insuranceEnabled(NewWorld 恒 true,
	// Start 回写)方向相反;NewVirtualCityRoom 不初始化 true,勿按保险家族惯性写。
	electionEnabled bool
	// FullAgentMode 标记该房间为全 Agent 模式（人类不能参与对局）。
	// 2026-09-19 §全Agent模式 新增：创建时由 agent_seats 满 MinSeats 自动置位，
	// 或前端显式请求 full_agent=true 置位。
	// 2026-09-26 §批次25:full_agent 建房字段接线 —— 显式 false 时保持 false
	// (允许人类加入空位);fullAgentModeSet 记录「已被显式配置」,ws 侧
	// EnsureFullAgentMode 防御门只兜底从未配置过的房间,不覆盖显式 false。
	FullAgentMode    bool
	fullAgentModeSet bool

	// 2026-09-21 §虚拟城市(契约 03 §6)— 城市背景层。
	// ResidentCount 建房参数(>0 时 Start 建城);City 背景居民世界。
	// 持久化说明:房间级 wealth 选项(含本字段)仅存内存 pendingOpts,
	// 重启不恢复(与 month_ms/pool/seed 同现状),详见 room_city.go 头注。
	ResidentCount int
	City          *city.Backdrop
	// cityRng 城市演化专用 rng(房间 seed ^ citySeedSalt 派生;与引擎 rng 流分离)。
	cityRng *rand.Rand
	// linePoolSource LLM 线路池来源(Manager 注入;池驱动座位 + 城市之声共用)。
	linePoolSource func() *llm.LinePool
	// cityVoice / 城市之声配置(Start 时构造调度器;2026-09-22 §17-CityHuman
	// 起与 cityDriver 互斥 —— 驱动层启用时 cityVoice 恒 nil)。
	cityVoice         *city.VoiceScheduler
	cityVoiceEnabled  bool
	cityVoicePerMonth int
	// cityDriver 居民驱动层(2026-09-22 §17-CityHuman 契约 02 §5;Start 时
	// 按 cityDriverEnabled 构造,与 VoiceScheduler 互斥)。
	cityDriver         *city.ResidentDriver
	cityDriverEnabled  bool
	cityDriverWorkers  int
	cityDriverPerMonth int
	// cityDriverLines 2026-09-25 §LLM线路池配额 — 建房 llm_lines([1,64];
	// 0=未指定保持缺省:agentSem=池总量、Workers=config)。
	cityDriverLines int
	// agentLLMMinIntervalMs 2026-09-26 §批次25(§3.3)— 每个 City-Human 的
	// LLM 调用令牌桶补充间隔(驱动层 RunMonth 共享桶同款间隔;座位 Agent 的
	// 桶由 manager 在 ensureAgentsWithPool 装配)。
	agentLLMMinIntervalMs int

	// 2026-09-22 §CityHuman重构:感知与发言支撑。
	utterances []UtteranceRecord // 公开发言环形缓冲(cap 50,hear 数据源)

	// agentPending 批次 25(§3.3):per-seat 单槽 pending —— agentSem 满槽时
	// 登记(true 覆盖 true 幂等),信号量释放侧补跑;替代旧的无界排队 goroutine。
	agentPending [MaxSeats]bool
	// agentDecisionDone 批次 25(§3.3):每座位「本月已决策」去重标记
	// (值 = 已完成决策的月份;BeginDecision 遇同月拒绝)。
	agentDecisionDone [MaxSeats]int

	// llmStats 批次 25 可观测性:房间级 LLM 调用计数(seat/driver/voice)。
	llmStats roomLLMStats

	// 城市时钟(批次 25 问题 3):cityEpochBaseMs 固定纪元(2025-01-01 08:00
	// +0800);pausedAccumMs 累计暂停时长;pauseStartAtMs 当前暂停起点(0=未暂停)。
	cityEpochBaseMs int64
	pausedAccumMs   int64
	pauseStartAtMs  int64

	done     chan struct{}
	settleCh chan struct{}
	closed   bool
}

// NewVirtualCityRoom 构造空房间(不启动 loop;Start 后进入 playing)。
// 2026-09-22 §17-CityHuman(契约 03 §2.1):pool 参数随精选层退役删除。
func NewVirtualCityRoom(roomID string, monthMs int, seed int64, llmConcurrency int) *VirtualCityRoom {
	if monthMs < 3000 {
		monthMs = 3000
	}
	if monthMs > 30000 {
		monthMs = 30000
	}
	if llmConcurrency <= 0 {
		// 2026-09-16 §12 座扩容:默认 4 → DefaultAgentConcurrency(8),让 10+ bot
		// 同月的并发决策不再被 4 路信号量压成串行(详见 engine.go 常量注释)。
		llmConcurrency = DefaultAgentConcurrency
	}
	seedVal := seed
	if seedVal == 0 {
		seedVal = time.Now().UnixNano()
	}
	return &VirtualCityRoom{
		RoomID:     roomID,
		Status:     StatusOpen,
		Phase:      PhaseActing,
		MonthMs:    monthMs,
		seed:       seedVal,
		rng:        rand.New(rand.NewSource(seedVal)),
		Spectators: map[string]struct{}{},
		IdleSeats:  map[int]bool{},
		agents:     map[int]*vcplayer.Agent{},
		agentSem:   make(chan struct{}, llmConcurrency),
		done:       make(chan struct{}),
		settleCh:   make(chan struct{}, 1),
		// P1: 真实经济循环 / 社会调研默认开启(§6.5;SetEconomyFlags 可覆盖)。
		economyEnabled:   true,
		surveyEnabled:    true,
		insuranceEnabled: true,
		// 2026-09-21 §虚拟城市:城市之声默认开(契约 03 §7;Manager.CreateRoom
		// 按 cfg 覆盖;无线路池时调度器自动空转,零开销)。
		cityVoiceEnabled:  true,
		cityVoicePerMonth: 4,
		// 批次 25(问题 3):城市时钟固定纪元 2025-01-01 08:00 +0800。
		cityEpochBaseMs: cityEpochBaseMillis(),
	}
}

// cityEpochTZ 城市时钟纪元时区(固定东八区;批次 25)。
var cityEpochTZ = time.FixedZone("Asia/Shanghai", 8*3600)

// cityEpochBaseMillis 城市时钟纪元毫秒:2025-01-01 08:00 +0800(批次 25 问题 3)。
func cityEpochBaseMillis() int64 {
	return time.Date(2025, 1, 1, 8, 0, 0, 0, cityEpochTZ).UnixMilli()
}

// SetEconomyFlags 回写房间级 economy/survey 开关(P1 §6.5;由
// Manager.CreateRoom 调用,须在 Start 之前)。economy=false 时 World 保持
// P0 行为(消费 to=world、无 Goods/Labor/Society、失业概率/ratio/工资增长回退)。
func (r *VirtualCityRoom) SetEconomyFlags(economy, survey bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.economyEnabled = economy
	r.surveyEnabled = survey
	if r.World != nil {
		r.World.EconomyEnabled = economy
	}
}

// SetInsuranceEnabled 回写房间级保险引擎开关(P1-4 §11;由 Manager.CreateRoom
// 调用,须在 Start 之前)。false 时投保/退保返回 35041、月结不扣缴保费、
// 意外事件不掷骰(rand 序列零偏移,固定种子存量对局回归一致)。
func (r *VirtualCityRoom) SetInsuranceEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insuranceEnabled = enabled
	if r.World != nil {
		r.World.InsuranceEnabled = enabled
	}
}

// SetElectionEnabled 市长选举房间级开关(批次20 文档3 A2)。与保险家族的
// 「NewWorld 恒 true + Start 回写」不同:NewCivicElection 默认即 false,
// 本方法显式 true 才启用;false 房 MonthlyStep 完全 no-op(R8-2 回归红线)。
func (r *VirtualCityRoom) SetElectionEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.electionEnabled = enabled
	if r.World != nil && r.World.Election != nil {
		r.World.Election.Enabled = enabled
	}
}

// SetFullAgentMode 设置房间的全 Agent 模式标志(2026-09-19 §全Agent模式)。
// 全 Agent 模式下人类玩家不能加入对局,仅可以观战者身份观看。
// 2026-09-26 §批次25:记录「已被显式配置」(fullAgentModeSet),供
// EnsureFullAgentMode 防御门识别显式 full_agent:false。
func (r *VirtualCityRoom) SetFullAgentMode(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.FullAgentMode = enabled
	r.fullAgentModeSet = true
}

// EnsureFullAgentMode 防御性置位(2026-09-26 §批次25):仅当 FullAgentMode
// 从未被显式配置过时置 true。ws 注册 bot 座位时(botSeats ≥ MinSeats)调用,
// 兜底「service 层未先置位」的旧链路;建房请求显式 full_agent:false 的房间
// 已由 service 层置位过(fullAgentModeSet=true),此处不覆盖。
func (r *VirtualCityRoom) EnsureFullAgentMode() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.fullAgentModeSet {
		r.FullAgentMode = true
		r.fullAgentModeSet = true
	}
}

// IsFullAgentMode 返回房间是否为全 Agent 模式。
func (r *VirtualCityRoom) IsFullAgentMode() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.FullAgentMode
}

// SetAgentLLMMinIntervalMs 批次 25(§3.3):每 Agent LLM 令牌桶补充间隔
// (Manager.CreateRoom 装配;驱动层 RunMonth 共享桶同款间隔)。
func (r *VirtualCityRoom) SetAgentLLMMinIntervalMs(ms int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentLLMMinIntervalMs = ms
}

// SetHooks 注入 ws 广播钩子(房间创建后、Start 前调用一次)。
func (r *VirtualCityRoom) SetHooks(h BroadcastHooks) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = h
}

// SetChatSender 注入聊天通道(bot speak 走 SendFromBot)。
func (r *VirtualCityRoom) SetChatSender(cs ChatSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chatSender = cs
}

// applyOpts 应用房间级 VirtualCityRoomOptions(由 manager.ApplyRoomOptions 调用)。
func (r *VirtualCityRoom) applyOpts(opts *service.VirtualCityRoomOptions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if opts.MonthMs > 0 {
		r.MonthMs = clampInt(opts.MonthMs, 3000, 30000)
	}
	if opts.Seed != 0 {
		r.seed = opts.Seed
	}
	// 2026-09-21 §虚拟城市:resident_count(service 层已 clamp MaxResidents;
	// 此处再防御负数)。仅 Start 前生效(城市在 Start 一次性合成)。
	if opts.ResidentCount > 0 {
		r.ResidentCount = opts.ResidentCount
	} else if opts.ResidentCount < 0 {
		r.ResidentCount = 0
	}
	// 批次20(文档3 A2):body.civic_election_enabled(缺省 false)。零值与
	// 「未传」不可区分,故单调置位:true 覆盖 manager 默认,false 保持
	// manager 默认(生产默认即 false → 语义 = 任一来源为 true 才启用)。
	// applyOpts 已持 r.mu,直写字段(不可调 SetElectionEnabled,锁不可重入)。
	if opts.CivicElectionEnabled {
		r.electionEnabled = true
		if r.World != nil && r.World.Election != nil {
			r.World.Election.Enabled = true
		}
	}
	// 2026-09-25 §LLM线路池配额:body.llm_lines([1,64],0=未指定保持缺省)。
	if opts.LLMLines > 0 {
		r.cityDriverLines = clampInt(opts.LLMLines, 1, 64)
	} else if opts.LLMLines < 0 {
		r.cityDriverLines = 0
	}
	// 2026-09-26 §批次25:full_agent 三态(body 顶层 full_agent → 本字段)。
	// 经 pendingOpts 在房间创建时应用(早于 ws 层 EnsureFullAgentMode 防御门,
	// 故显式 false 不会被翻回 true)。仅 Start 前生效。
	if opts.FullAgent != nil {
		r.FullAgentMode = *opts.FullAgent
		r.fullAgentModeSet = true
	}
}

// SetOwner 记录房主(game.virtual_city_start/pause 权限,35011)。
// 首位人类入座者即创建者(CreateRoomWithAgents → SyncSeat 顺序保证)。
func (r *VirtualCityRoom) SetOwner(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.OwnerID == "" {
		r.OwnerID = userID
	}
}

// SeatOf 返回 userID 所在座位。
func (r *VirtualCityRoom) SeatOf(userID string) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, u := range r.Seats {
		if u == userID && u != "" {
			return i, true
		}
	}
	return -1, false
}

// Occupied 已入座数。
func (r *VirtualCityRoom) Occupied() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.occupiedLocked()
}

func (r *VirtualCityRoom) occupiedLocked() int {
	n := 0
	for _, u := range r.Seats {
		if u != "" {
			n++
		}
	}
	return n
}

// JoinGame 人类入座(first-empty,跳过已标记 bot 座)。幂等。
// 返回 (座位, 是否因此满员触发自动开局)。
func (r *VirtualCityRoom) JoinGame(userID, nickname string) (int, bool, *errcode.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Status == StatusOver {
		return -1, false, errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	// 2026-09-19 §全Agent模式: 全 Agent 房间拒绝人类加入。该检查必须覆盖
	// open / playing 两种状态,不能只挂在 StatusOver 分支内。
	if r.FullAgentMode {
		return -1, false, errcode.Code(errcode.ErrVirtualCityFullAgentReject)
	}
	// 幂等。
	for i, u := range r.Seats {
		if u == userID {
			return i, false, nil
		}
	}
	seat := -1
	for i, u := range r.Seats {
		if u == "" && !r.BotSeats[i] {
			seat = i
			break
		}
	}
	if seat == -1 {
		return -1, false, errcode.Code(errcode.ErrRoomFull)
	}
	r.Seats[seat] = userID
	r.Nicknames[seat] = nickname
	if r.OwnerID == "" {
		r.OwnerID = userID
	}
	if r.World != nil {
		// 中途加入:发卡 + 本月不参与(B7:当月不补结,下月正式参与)。
		card := r.drawCardLocked()
		p := newPlayerFromCard(seat, card)
		p.Submitted = true
		p.ActionBudget = 0
		r.World.Players[seat] = p
	}
	// 2026-09-16 §12 座扩容:「满员自动开局」阈值从 MaxSeats(12) 下调到
	// MinSeats(10)——10-11 bot 的全 Agent 房(创建者降级为观战者)在注册完
	// bot 后即可自动开局,不必凑满 12 人。房间仍可容纳到 MaxSeats(12),超出的
	// 2 头寸留给中途加入的人类玩家。
	full := r.occupiedLocked() >= MinSeats && r.Status == StatusOpen
	return seat, full, nil
}

// RegisterBotSeats 标记并入住 bot 座位(建房时调用,先于人类 JoinGame)。
// 2026-09-14 §财商流P0-bugfix: 旧版只写 BotSeats/SeatModelKeys,不写
// Seats[seat] 的 bot userID,导致 Start 发卡跳过 bot 座位、EnsureAgents 因
// Seats[seat]=="" 跳过 → bot 永不上场。现扩展为同时入住 bot userID
// (seatUsers,仅写空位,不覆盖已有人类座位)。
// 2026-09-22 §17-CityHuman(契约 03 §1.4):第三参 professions 座位职业偏好
// 随精选层退役删除(原恒传 nil 的死路径,§130 死代码清算)。2026-09-24 重构:
// 座位 = 当月抽样展示居民,档案来自 Backdrop 真实居民,无人类玩家概念。
func (r *VirtualCityRoom) RegisterBotSeats(seatUsers map[int]string, seatModels map[int]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for seat, userID := range seatUsers {
		if seat < 0 || seat >= MaxSeats || userID == "" {
			continue
		}
		if r.Seats[seat] == "" {
			r.Seats[seat] = userID
		}
		r.BotSeats[seat] = true
		if r.Nicknames[seat] == "" {
			if mk := seatModels[seat]; mk != "" {
				r.Nicknames[seat] = "AI·" + mk
			} else {
				// 2026-09-21 §虚拟城市(契约 04 §1.4):池驱动座位注册时先占位
				// 「AI·居民<seat>号」(1-based 展示);Start 抽卡后升级为 AI·<Card.Name>。
				r.Nicknames[seat] = fmt.Sprintf("AI·居民%d号", seat+1)
			}
		}
	}
	for seat, modelKey := range seatModels {
		if seat < 0 || seat >= MaxSeats || modelKey == "" {
			continue
		}
		r.BotSeats[seat] = true
		r.SeatModelKeys[seat] = modelKey
	}
}

// drawCardLocked 从房间卡池抽下一张(池尽循环 curated;确定性:房间 rng)。
func (r *VirtualCityRoom) drawCardLocked() profession.Card {
	if len(r.cardPool) == 0 || r.cardPoolIdx >= len(r.cardPool) {
		r.cardPool = r.buildCardPoolLocked()
		r.cardPoolIdx = 0
	}
	card := r.cardPool[r.cardPoolIdx]
	r.cardPoolIdx++
	return card
}

// buildCardPoolLocked 构建洗牌后的开局卡池(2026-09-22 §17-CityHuman 契约
// 03 §2.1:恒走文档池 docLoader.Draw,不可用时 SyntheticCards 合成兜底并
// 洗牌 —— 精选 14 卡层已退役,没有非 docs 模式)。
// 2026-09-26 §批次25 persona 统一:文档池改走 DrawWithDomain(与 Draw 共用
// drawPairs,rng 消耗序列零变化),同时把每张卡的来源相对路径存入
// cardPoolPaths(供 N≤12 时档案锚定复用同一批卡,见 room_city.go
// anchorCityProfiles 的 sharedPaths 参数)。
func (r *VirtualCityRoom) buildCardPoolLocked() []profession.Card {
	r.cardPoolPaths = nil
	if r.docLoader != nil {
		if pairs := r.docLoader.DrawWithDomain(MaxSeats, r.rng); len(pairs) > 0 {
			cards := make([]profession.Card, len(pairs))
			paths := make([]string, len(pairs))
			for i := range pairs {
				cards[i] = pairs[i].Card
				paths[i] = pairs[i].SourcePath
			}
			r.cardPoolPaths = paths
			return cards
		}
	}
	cards := profession.SyntheticCards(MaxSeats, r.rng)
	r.rng.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
	return cards
}

// Start 开局:发卡 → 初始注入 → 广播 → 进入月份 1。返回错误(人数不足等)。
// caller: manager(锁外;内部自行持锁)。
func (r *VirtualCityRoom) Start(loader *profession.Loader) *errcode.Error {
	// 2026-09-25 §建房超时修复 — 兜底预热必须发生在锁外:sync.Once 未完成时
	// ForceIndex 可能等待数十秒,若发生在 r.mu 持锁区内,会把大厅
	// GET /rooms 轮询(IsFullAgentRoom 探针)整条链路一起阻塞(实测 33.8s)。
	if loader != nil {
		loader.ForceIndex()
	}
	r.mu.Lock()
	if r.Status != StatusOpen {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	if n := r.occupiedLocked(); n < MinSeats {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrVirtualCityNotEnoughPlayers)
	}

	// 发卡:座位序抽取(2026-09-22 §17-CityHuman:座位职业偏好死路径随精选
	// 层退役删除,契约 03 §1.4;恒 docs 池 + synthetic 兜底)。
	var cards [MaxSeats]profession.Card
	for seat := 0; seat < MaxSeats; seat++ {
		if r.Seats[seat] == "" {
			continue
		}
		cards[seat] = r.drawCardLocked()
	}

	// 2026-09-21 §虚拟城市(契约 04 §1.4):池驱动座位(model_key=="")抽卡后
	// 昵称升级为 AI·<Card.Name>(文档池真实职业卡人名);无卡名(curated 精选卡
	// 不带化名)回退 AI·居民<seat>号。显式 model_key 座位保持 AI·<model_key> 不变。
	// 此处先于 hooks.OnState 下发(玩家昵称经 game.state 全房可见)。
	for seat := 0; seat < MaxSeats; seat++ {
		if !r.BotSeats[seat] || r.SeatModelKeys[seat] != "" {
			continue
		}
		if name := cards[seat].Name; name != "" {
			r.Nicknames[seat] = "AI·" + name
		} else {
			r.Nicknames[seat] = fmt.Sprintf("AI·居民%d号", seat+1)
		}
	}

	seed := r.seed
	r.World = NewWorld(seed, cards)
	// P1(§6.5):NewWorld 恒置 EconomyEnabled=true,此处按房间级开关回写。
	r.World.EconomyEnabled = r.economyEnabled
	// P1-4(§财商流P1-4 §11):NewWorld 恒置 InsuranceEnabled=true,此处按开关回写。
	r.World.InsuranceEnabled = r.insuranceEnabled
	// 批次20(文档3 A2):市长选举接线 —— NewCivicElection 默认 Enabled=false
	// (零值=false 家族,勿按上面 InsuranceEnabled 的反向语义写);房间级
	// 开关 true 才启用。false 时 MonthlyStep 完全 no-op,与升级前逐分不差。
	if r.World.Election != nil {
		r.World.Election.Enabled = r.electionEnabled
	}
	r.World.StartGame()
	// 2026-09-21 §虚拟城市(契约 03 §6):resident_count>0 时建城(确定性:
	// 房间 seed 派生独立 rng);B5:线路池可用时房间信号量容量 = 总线路数。
	r.startCityLocked()
	r.resizeAgentSemLocked()
	r.Status = StatusPlaying
	r.Phase = PhaseActing
	r.gameStartedAt = time.Now().Unix()
	// 批次 25(问题 3):城市时钟暂停累计归零(重开/再开局不复用旧暂停)。
	r.pausedAccumMs = 0
	r.pauseStartAtMs = 0
	r.MonthMs = clampInt(r.MonthMs, 3000, 30000)
	r.NextMonthAt = time.Now().Add(time.Duration(r.MonthMs) * time.Millisecond)
	r.resetMonthFlagsLocked()
	openings := r.openingHooksLocked()

	// 2026-09-22 §17-CityHuman(契约 03 §1.4):startedPayload["professions"]
	// 随精选层退役删除 —— 开局职业不再作为独立公开表下发(身份经由每座位
	// game.state.my 卡面与昵称呈现)。
	occupied := r.occupiedLocked()
	age := r.World.Age()
	startedPayload := map[string]any{
		"room_id": r.RoomID, "game_kind": "virtual_city",
		"month": 1, "age": age, "start_age": age,
	}
	hooks := r.hooks
	r.mu.Unlock()

	if hooks.OnStarted != nil {
		hooks.OnStarted(r.RoomID, startedPayload)
	}
	if hooks.OnState != nil {
		hooks.OnState(r.RoomID)
	}
	r.sendOpeningHooks(openings)
	r.startLLMStatsLoop() // 批次 25 可观测性:60s 汇总 virtual_city llm rate
	r.wakeBots()
	logger.L().Info("virtual_city game started",
		zap.String("room_id", r.RoomID), zap.Int("seats", occupied))
	return nil
}

// resetMonthFlagsLocked 新月 acting 开始:重置预算/发言/提交标记(锁内,§92a)。
func (r *VirtualCityRoom) resetMonthFlagsLocked() {
	if r.World == nil {
		return
	}
	now := time.Now().UnixMilli()
	for seat, p := range r.World.Players {
		if p == nil {
			continue
		}
		p.ActionBudget = monthlyActionBudget
		p.SpeakCountThisMonth = 0
		p.Submitted = false
		p.LastActionText = ""
		p.StatusIcon = "idle"
		r.refreshTranscriptLocked(seat, p, now)
	}
}

// openingHooksLocked 收集全部 bot 职业卡开场白(锁内快照,锁外发送)。
func (r *VirtualCityRoom) openingHooksLocked() []openingHookMessage {
	if r.openingHooksSent || r.World == nil {
		return nil
	}
	out := make([]openingHookMessage, 0, MaxSeats)
	for seat, p := range r.World.Players {
		if p == nil || !r.BotSeats[seat] || p.Card.OpeningHook == "" {
			continue
		}
		account := r.Nicknames[seat]
		if account == "" {
			account = r.SeatModelKeys[seat]
		}
		out = append(out, openingHookMessage{
			seat: seat, userID: r.Seats[seat], account: account,
			modelKey: r.SeatModelKeys[seat], text: clip(p.Card.OpeningHook, 100),
		})
	}
	r.openingHooksSent = true
	return out
}

type openingHookMessage struct {
	seat                            int
	userID, account, modelKey, text string
}

// sendOpeningHooks 逐个走 ChatService.SendFromBot;单个持久化失败不阻断开局,
// 但必须记录错误,便于观测 12 条 opening_hook 的实际到达率。
func (r *VirtualCityRoom) sendOpeningHooks(messages []openingHookMessage) {
	if len(messages) == 0 {
		return
	}
	r.mu.Lock()
	chat := r.chatSender
	roomID := r.RoomID
	r.mu.Unlock()
	if chat == nil {
		return
	}
	for _, msg := range messages {
		if err := chat.SendFromBot(roomID, msg.userID, msg.account, msg.modelKey, msg.text); err != nil {
			logger.L().Warn("wealth opening hook send failed",
				zap.String("room_id", roomID), zap.Int("seat", msg.seat), zap.Error(err))
		}
	}
}

// refreshTranscriptLocked 每月刷新权威 transcript 元数据。存活 bot 从
// “等待决策”重新开始;出局 bot 清空旧决策并标记 inactive,避免前端把历史
// 摘要误读为仍在参与。
func (r *VirtualCityRoom) refreshTranscriptLocked(seat int, p *Player, nowUnixMilli int64) {
	t := r.Transcripts[seat]
	t.Month = r.World.Month
	t.UpdatedAt = nowUnixMilli
	t.Active = p.Alive
	if !p.Alive {
		t.LastDecisionSummary = "已出局,停止月度决策"
		t.LastDecisionMonth = r.World.Month
		t.LastToolInput = ""
		t.LastToolResult = ""
		t.HeartThought = ""
	} else {
		// Month 表示房间当前月;LastDecisionMonth 表示摘要所属月。
		// 月结后保留上一月“无动作/超时/已提交”的结论,直到本月 Agent 发布
		// 新快照,避免强制结算结论在锁内被新月初始化覆盖而不可见。
		if t.LastDecisionMonth == 0 {
			t.LastDecisionMonth = r.World.Month
			t.LastDecisionSummary = "等待 Agent 月度决策"
			t.LastToolInput = ""
			t.LastToolResult = ""
			t.HeartThought = ""
		}
	}
	r.Transcripts[seat] = t
}

// markUnsubmittedBotsLocked 在月窗强制结束前为未提交 bot 写入系统超时摘要,
// 保证“无动作 / LLM 未返回”也能被 bot_contexts 观测到。
func (r *VirtualCityRoom) markUnsubmittedBotsLocked() {
	now := time.Now().UnixMilli()
	for seat, p := range r.World.Players {
		if p == nil || !r.BotSeats[seat] {
			continue
		}
		t := r.Transcripts[seat]
		t.Month = r.World.Month
		t.LastDecisionMonth = r.World.Month
		t.UpdatedAt = now
		t.Active = p.Alive
		if !p.Alive {
			t.LastDecisionSummary = "已出局,停止月度决策"
			t.LastDecisionMonth = r.World.Month
			t.LastToolInput = ""
			t.LastToolResult = ""
			t.HeartThought = ""
		} else if !p.Submitted {
			t.LastDecisionSummary = "月窗结束,Agent 未提交动作,系统自动结算"
			t.LastToolInput = "system_timeout"
			t.LastToolResult = "强制 submit_month"
		}
		r.Transcripts[seat] = t
	}
}

// RunLoop 月度主循环(单 goroutine;禁止锁内回调)。
func (r *VirtualCityRoom) RunLoop(onFinish func(roomID string)) {
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return
		}
		var wait time.Duration
		if r.Status == StatusPlaying && r.Phase == PhaseActing {
			if r.Paused {
				// 暂停:冻结窗口(顺延)。
				r.NextMonthAt = time.Now().Add(time.Duration(r.MonthMs) * time.Millisecond)
			}
			wait = time.Until(r.NextMonthAt)
		} else {
			wait = time.Hour // over / settling(由 settleCh 推进)
			if r.Status == StatusOver {
				wait = time.Hour
			}
		}
		r.mu.Unlock()
		if wait < 0 {
			wait = 0
		}

		select {
		case <-r.done:
			return
		case <-r.settleCh:
		case <-time.After(wait):
		}

		r.trySettle(onFinish)
	}
}

// trySettle 窗口结束(或全员提交)→ 月结。返回是否推进了一月。
func (r *VirtualCityRoom) trySettle(onFinish func(roomID string)) bool {
	r.mu.Lock()
	if r.closed || r.Status != StatusPlaying || r.Phase != PhaseActing {
		r.mu.Unlock()
		return false
	}
	if r.Paused || time.Now().Before(r.NextMonthAt) {
		// 2026-09-26 §批次25(§3.3 月节拍回归):月结固定发生在 NextMonthAt ——
		// 「全员提交提前结算」快路径已删除(原会把实际月速压到最慢 bot 决策时长,
		// month_ms 从节拍退化为上限,放大 LLM 调用频率)。
		r.mu.Unlock()
		return false
	}

	// settling:停止接收动作。
	r.Phase = PhaseSettling
	r.markUnsubmittedBotsLocked()
	for _, p := range r.World.Players {
		if p != nil && !p.Submitted {
			p.Submitted = true // 窗口强制结束(watchdog 兜底语义)
		}
	}
	prevEvents := len(r.World.Events)
	finished, res := r.World.SettleMonth()
	newEvents := append([]EventRecord(nil), r.World.Events[prevEvents:]...)
	// 2026-09-21 §虚拟城市(契约 03 §6):月结顺序 SettleMonth() → City.TickMonth
	// → 广播。城市 tick 在锁内(与引擎结算同互斥域),cpi 取引擎月环比通胀。
	voiceMonth := r.World.Month
	if !finished {
		r.tickCityLocked()
	}
	hooks := r.hooks
	r.eventsSent = len(r.World.Events)

	var scores []FinalScore
	over := false
	if finished {
		r.Status = StatusOver
		scores = r.World.FinalScores()
		over = true
	} else {
		r.Phase = PhaseActing
		r.NextMonthAt = time.Now().Add(time.Duration(r.MonthMs) * time.Millisecond)
		r.resetMonthFlagsLocked()
	}
	roomID := r.RoomID
	r.mu.Unlock()

	// ── 锁外回调(§92a) ──
	if hooks.OnEvent != nil {
		for _, ev := range newEvents {
			hooks.OnEvent(roomID, ev)
		}
	}
	// P1: 本月关闭的调研逐个广播 game.survey_result(调研契约 §4.4)。
	if hooks.OnSurvey != nil {
		for _, sv := range res.ClosedSurveys {
			hooks.OnSurvey(roomID, sv)
		}
	}
	if hooks.OnMonth != nil {
		hooks.OnMonth(roomID, res)
	}
	if over {
		if hooks.OnOver != nil {
			hooks.OnOver(roomID, scores)
		}
		if onFinish != nil {
			onFinish(roomID)
		}
		return true
	}
	if hooks.OnState != nil {
		hooks.OnState(roomID)
	}
	// 2026-09-21 §虚拟城市(契约 03 §5):月结后异步触发城市之声/居民驱动层
	// (goroutine,绝不阻塞月结;终局房不发声)。
	r.launchCityDriver(voiceMonth)
	r.wakeBots()
	return true
}

// allSubmittedLocked 全员已提交(空座不计)。
func (r *VirtualCityRoom) allSubmittedLocked() bool {
	if r.World == nil {
		return false
	}
	n, submitted := 0, 0
	for _, p := range r.World.Players {
		if p == nil {
			continue
		}
		n++
		if p.Submitted {
			submitted++
		}
	}
	return n > 0 && n == submitted
}

// SubmitMonth 座位提交本月(submit_month;不耗预算;全员提交 → 提前月结)。
func (r *VirtualCityRoom) SubmitMonth(seat int) *errcode.Error {
	r.mu.Lock()
	if r.Status != StatusPlaying {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	if r.Phase != PhaseActing {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrVirtualCityWrongPhase)
	}
	p := r.World.Players[seat]
	if p == nil || !p.Alive {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if p.Submitted {
		r.mu.Unlock()
		return nil // 幂等
	}
	p.Submitted = true
	r.mu.Unlock()
	// 2026-09-26 §批次25(§3.3 月节拍回归):「最后一座提交即推 settleCh」
	// 快路径已删除 —— 月结固定发生在 NextMonthAt(月节拍 = month_ms,
	// 一局 420 月 ≈ 56 分钟),不再被最慢 bot 决策时长压速。
	return nil
}

// NotifySubmitted 人类/Agent submit 后由 ws 层触发一次状态刷新。
func (r *VirtualCityRoom) NotifySubmitted() {
	r.mu.Lock()
	hooks := r.hooks
	r.mu.Unlock()
	if hooks.OnState != nil {
		hooks.OnState(r.RoomID)
	}
}

// MarkIdle 人类中途离房:P0 座位自动挂机(每月自动 submit_month,不接 LLM);
// bot 接管为 P1(见 ws/game_service_wealth.go 注释)。
func (r *VirtualCityRoom) MarkIdle(userID string) {
	r.mu.Lock()
	seat, ok := -1, false
	for i, u := range r.Seats {
		if u == userID {
			seat, ok = i, true
			break
		}
	}
	if ok && !r.BotSeats[seat] {
		r.IdleSeats[seat] = true
	}
	r.mu.Unlock()
}

// Pause 房主暂停/恢复(月结边界生效;B8)。
// 2026-09-26 §批次25(问题 3):暂停期间城市时钟冻结 —— 暂停开始记
// pauseStartAtMs,恢复时累加进 pausedAccumMs(CityClockMs 计算时扣除)。
func (r *VirtualCityRoom) Pause(userID string, pause bool) *errcode.Error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.OwnerID == "" || userID != r.OwnerID {
		return errcode.Code(errcode.ErrVirtualCityNotOwner)
	}
	if r.Status != StatusPlaying {
		return errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	nowMs := time.Now().UnixMilli()
	if pause && !r.Paused {
		r.pauseStartAtMs = nowMs
	}
	if !pause && r.Paused {
		r.pausedAccumMs += nowMs - r.pauseStartAtMs
		r.pauseStartAtMs = 0
		r.NextMonthAt = time.Now().Add(time.Duration(r.MonthMs) * time.Millisecond)
	}
	r.Paused = pause
	logger.L().Info("virtual_city room pause toggled",
		zap.String("room_id", r.RoomID), zap.Bool("paused", pause))
	return nil
}

// CityClockMs 城市时钟(2026-09-26 §批次25 问题 3):现实 1 分钟 = 城市 1 小时
// (60× 加速),与月结 tick 解耦,纯叙事/展示层。
//
//	CityClockMs = cityEpochBaseMs + (now - gameStartedAt - pausedAccumMs) × 60
//
// 未开局(gameStartedAt==0)返回 0;暂停期间冻结(进行中的暂停段即时扣除)。
func (r *VirtualCityRoom) CityClockMs() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cityClockMsLocked(time.Now())
}

// cityClockMsLocked 锁内变体(view 广播路径在锁内构造快照时复用)。
func (r *VirtualCityRoom) cityClockMsLocked(now time.Time) int64 {
	if r.gameStartedAt == 0 {
		return 0
	}
	nowMs := now.UnixMilli()
	excluded := r.pausedAccumMs
	if r.Paused && r.pauseStartAtMs > 0 {
		excluded += nowMs - r.pauseStartAtMs
	}
	elapsedMs := nowMs - r.gameStartedAt*1000 - excluded
	if elapsedMs < 0 {
		elapsedMs = 0
	}
	return r.cityEpochBaseMs + elapsedMs*60
}

// Close 停止 loop(房间删除/终局清理)。
func (r *VirtualCityRoom) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.done)
	r.mu.Unlock()
}

// SpecSnapshot 供 ws 层构造 game.joined 的轻量快照。
type SpecSnapshot struct {
	Status string
	Phase  string
	Month  int
}

// SnapshotStatus 返回状态快照。
func (r *VirtualCityRoom) SnapshotStatus() SpecSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := SpecSnapshot{Status: r.Status, Phase: r.Phase, Month: 1}
	if r.World != nil {
		s.Month = r.World.Month
	}
	return s
}

// GetStatus 房间状态(锁内)。
func (r *VirtualCityRoom) GetStatus() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Status
}

// GetPhase 阶段。
func (r *VirtualCityRoom) GetPhase() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Phase
}

// Engine 返回引擎快照指针(锁内;调用方不得修改指针指向结构)。
func (r *VirtualCityRoom) Engine() *World {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.World
}

// EngineLocked 返回引擎指针(不加锁;调用方必须已持 MuLock —— §92a ws 层
// apply 路径专用)。2026-09-14 §财商流P0-bugfix: 修复「持锁后调 Engine()
// 二次加锁自死锁」;wealth 包内部持锁路径同理应直接读 r.World 字段。
func (r *VirtualCityRoom) EngineLocked() *World {
	return r.World
}

// MuLock/MuUnlock 是 ws 层 SyncSeat/apply 路径专用(§92a,锁外不允许)。
// 其他路径优先用短方法。
func (r *VirtualCityRoom) MuLock()   { r.mu.Lock() }
func (r *VirtualCityRoom) MuUnlock() { r.mu.Unlock() }

// SeedView 返回房间种子(锁内读)。ws 层用于 PlaceholderWorld。
func (r *VirtualCityRoom) SeedView() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seed
}

// SubmitMonthLocked 提交单座位(锁内,§92a)。
// 别名保持:SubmitMonthLocked = SubmitMonthLocked(同上)。

// Month 返回当前月份。
func (r *VirtualCityRoom) Month() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return 0
	}
	return r.World.Month
}

// Age 返回主时钟年龄。
func (r *VirtualCityRoom) Age() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return 25
	}
	return r.World.Age()
}

// SignalSettle 触发提前月结信号。
func (r *VirtualCityRoom) SignalSettle() {
	select {
	case r.settleCh <- struct{}{}:
	default:
	}
}

// AllSubmittedLocked 全员已提交(锁内调用,§92a)。
func (r *VirtualCityRoom) AllSubmittedLocked() bool {
	return r.allSubmittedLocked()
}

// SubmitMonthLocked 提交单座位(锁内,§92a)。
func (r *VirtualCityRoom) SubmitMonthLocked(seat int) *errcode.Error {
	if r.Status != StatusPlaying {
		return errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	if r.Phase != PhaseActing {
		return errcode.Code(errcode.ErrVirtualCityWrongPhase)
	}
	p := r.World.Players[seat]
	if p == nil || !p.Alive {
		return errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	p.Submitted = true
	return nil
}

// EnqueueEventForUI 把事件加入 World.Events(供 ws 层即时广播)。
func (r *VirtualCityRoom) EnqueueEventForUI(ev EventRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return
	}
	r.World.Events = append(r.World.Events, ev)
}

// ── P1: 社会调研(§财商流P1-2 调研契约 §3.3)──

// LaunchSurvey 发起调研(持 r.mu;§3.2 全部校验在此)。
// 成功后 emitEvent("survey", -1, "新调研:<question>(截止月 <DeadlineMonth>)")。
// 权限:任意登录用户(HTTP 层鉴权;调研是「向 AI 提问」,与座位无关)。
func (r *VirtualCityRoom) LaunchSurvey(question string, options []string) (*Survey, *errcode.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil || r.Status != StatusPlaying {
		return nil, errcode.Code(errcode.ErrVirtualCityNotPlaying) // 35002
	}
	if !r.surveyEnabled {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "调研系统未启用") // 35010
	}
	q := strings.TrimSpace(question)
	if q == "" {
		return nil, errcode.Code(errcode.ErrVirtualCitySurveyOptionsInvalid) // 35016
	}
	if len([]rune(q)) > surveyQuestionMaxRunes {
		q = string([]rune(q)[:surveyQuestionMaxRunes])
	}
	if len(options) < 2 || len(options) > 6 {
		return nil, errcode.Code(errcode.ErrVirtualCitySurveyOptionsInvalid) // 35016
	}
	opts := make([]string, 0, len(options))
	for _, o := range options {
		o = strings.TrimSpace(o)
		if o == "" {
			return nil, errcode.Code(errcode.ErrVirtualCitySurveyOptionsInvalid)
		}
		if len([]rune(o)) > surveyOptionMaxRunes {
			o = string([]rune(o)[:surveyOptionMaxRunes])
		}
		opts = append(opts, o)
	}
	// 限流 ①:每房同时仅 1 个 open。
	if r.World.OpenSurvey() != nil {
		return nil, errcode.Code(errcode.ErrVirtualCitySurveyOpenExists) // 35017
	}
	// 限流 ②:每月(w.Month)仅可发起 1 个新调研(扫描 LaunchMonth,零新增状态)。
	for _, sv := range r.World.Surveys {
		if sv.LaunchMonth == r.World.Month {
			return nil, errcode.Code(errcode.ErrVirtualCitySurveyMonthlyLimit) // 35018
		}
	}
	// 限流 ③:每房累计 ≤ 20 个。
	if len(r.World.Surveys) >= surveyMaxPerRoom {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCitySurveyMonthlyLimit, "调研累计上限 20 个")
	}
	r.World.SurveySeq++
	sv := &Survey{
		ID:            fmt.Sprintf("SV%d", r.World.SurveySeq),
		Question:      q,
		Options:       opts,
		LaunchMonth:   r.World.Month,
		DeadlineMonth: r.World.Month + 2,
		Status:        SurveyOpen,
		Answers:       map[int]*SurveyAnswer{},
	}
	r.World.Surveys = append(r.World.Surveys, sv)
	r.World.emitEvent("survey", -1, fmt.Sprintf("新调研:%s(截止月 %d)", sv.Question, sv.DeadlineMonth))
	return sv, nil
}

// ListSurveys 返回全部调研快照(持 r.mu 拷贝;≤20,按发起序)。
func (r *VirtualCityRoom) ListSurveys() []Survey {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil || len(r.World.Surveys) == 0 {
		return nil
	}
	out := make([]Survey, 0, len(r.World.Surveys))
	for _, sv := range r.World.Surveys {
		cp := *sv
		cp.Options = append([]string(nil), sv.Options...)
		if sv.Result != nil {
			res := *sv.Result
			res.Counts = append([]int(nil), sv.Result.Counts...)
			res.Percents = append([]float64(nil), sv.Result.Percents...)
			res.TopReasons = append([]string(nil), sv.Result.TopReasons...)
			cp.Result = &res
		}
		// Answers 明细不下发(view 层只取聚合;§11.5 匿名投票原理)。
		cp.Answers = nil
		out = append(out, cp)
	}
	return out
}

// SnapshotSeats/Nicknames/BotSeats/ModelKeys/Transcripts 锁内取快照。
func (r *VirtualCityRoom) SnapshotSeats() [MaxSeats]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Seats
}
func (r *VirtualCityRoom) SnapshotNicknames() [MaxSeats]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Nicknames
}
func (r *VirtualCityRoom) SnapshotBotSeats() [MaxSeats]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.BotSeats
}
func (r *VirtualCityRoom) SnapshotModelKeys() [MaxSeats]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeatModelKeys
}
func (r *VirtualCityRoom) SnapshotTranscripts() [MaxSeats]BotTranscript {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Transcripts
}

// GameStartedAtUnix 开局 unix 时间戳(开 Start 时冻结;供 view next_month_at 等)。
func (r *VirtualCityRoom) GameStartedAtUnix() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gameStartedAt
}

// NextMonthAtUnix 当前月窗口结束 unix ms(若未 playing,返回 0)。
func (r *VirtualCityRoom) NextMonthAtUnix() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Status != StatusPlaying {
		return 0
	}
	return r.NextMonthAt.UnixMilli()
}

// SetChatSender 注入聊天通道(由 ws 层装配时调用)。
func (r *VirtualCityRoom) ChatSender() ChatSender {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.chatSender
}

// clampInt 整数夹取。
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wakeBots 唤醒全部 bot 座位决策(由 Start()/每轮 settle 后调用,锁外路径)。
// 房间级并发信号量(默认 4;NewVirtualCityRoom 已设)。
// 2026-09-26 §批次25(§3.3):跳过当月已 Submitted 的座位 —— 已提交的居民
// 本月不再需要 wake(同月重复决策由 BeginDecision 的 agentDecisionDone 拦截,
// 此处提前短路省 goroutine)。
func (r *VirtualCityRoom) wakeBots() {
	r.mu.Lock()
	if r.closed || r.Status != StatusPlaying || r.Paused {
		r.mu.Unlock()
		return
	}
	agents := make(map[int]*vcplayer.Agent, len(r.agents))
	for s, a := range r.agents {
		if p := r.World.Players[s]; p != nil && p.Submitted {
			continue
		}
		agents[s] = a
	}
	r.mu.Unlock()
	for seat, agent := range agents {
		go r.runOneBot(seat, agent)
	}
}

// wakeBotSeat 唤醒单个 bot 座位(2026-09-26 §批次25;EndDecision 跨月补 wake
// 只针对本座位,不再全员重跑)。
func (r *VirtualCityRoom) wakeBotSeat(seat int) {
	r.mu.Lock()
	if r.closed || r.Status != StatusPlaying || r.Paused {
		r.mu.Unlock()
		return
	}
	agent := r.agents[seat]
	r.mu.Unlock()
	if agent == nil {
		return
	}
	go r.runOneBot(seat, agent)
}

// runOneBot 单座位决策入口。2026-09-26 §批次25(§3.3):agentSem 满槽时不再
// 起无界排队 goroutine(旧实现阻塞等待,月窗高速推进时可堆积),改为登记
// per-seat 单槽 pending(幂等覆盖),由信号量释放侧补跑。
func (r *VirtualCityRoom) runOneBot(seat int, agent *vcplayer.Agent) {
	select {
	case r.agentSem <- struct{}{}:
	default:
		r.mu.Lock()
		r.agentPending[seat] = true
		r.mu.Unlock()
		return
	}
	// 释放 + pending 补跑必须在 panic 时也执行(信号量不泄漏);
	// recover 兜底防止单 bot panic 拖垮整个进程。
	defer func() {
		if rec := recover(); rec != nil {
			logger.L().Error("virtual_city runOneBot panic recovered",
				zap.String("room_id", r.RoomID), zap.Int("seat", seat),
				zap.Any("panic", rec), zap.String("stack", string(debug.Stack())))
		}
		r.releaseAgentSemAndDrain()
	}()
	r.runAgentCtx(seat, agent)
}

// releaseAgentSemAndDrain 释放并发信号量并补跑一个 pending 座位(若有)。
// pending 座位重新走 runOneBot(拿不到槽会再回 pending,由下一次释放再补;
// 极端无后续释放时由 watchdog/月结兜底提交,绝不泄漏 goroutine)。
func (r *VirtualCityRoom) releaseAgentSemAndDrain() {
	<-r.agentSem
	r.mu.Lock()
	if r.closed || r.Status != StatusPlaying || r.Paused {
		r.mu.Unlock()
		return
	}
	seat := -1
	var agent *vcplayer.Agent
	for s, pending := range r.agentPending {
		if !pending {
			continue
		}
		r.agentPending[s] = false
		if a := r.agents[s]; a != nil {
			seat, agent = s, a
		}
		break
	}
	r.mu.Unlock()
	if agent != nil {
		r.runOneBot(seat, agent)
	}
}

func (r *VirtualCityRoom) runAgentCtx(seat int, agent *vcplayer.Agent) {
	ctx, ok := BuildContextForAgent(r, seat)
	if !ok || ctx == nil {
		return
	}
	agent.OnMonthStart(context.Background(), ctx)
}
