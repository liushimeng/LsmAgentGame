// Package errcode defines the global error code table used by every API response.
//
// Rules:
//   - Codes are 5-digit integers. The first digit encodes the class:
//     0xxxx = success, 1xxxx = auth, 2xxxx = validation, 3xxxx = resource,
//     4xxxx = internal/server, 5xxxx = upstream/dependency.
//   - Pair every code with a default English message; clients may override
//     the message field but the code is the contract.
package errcode

import "fmt"

// Standard codes. Add new ones here — never inline magic numbers in handlers.
const (
	OK = 0

	// 1xxxx — auth
	ErrAuthMissingToken    = 10001
	ErrAuthInvalidToken    = 10002
	ErrAuthTokenExpired    = 10003
	ErrAuthAccountNotFound = 10101
	ErrAuthPasswordWrong   = 10102
	ErrAuthAccountTaken    = 10103
	ErrAuthEmailTaken      = 10104
	ErrAuthPhoneTaken      = 10105
	ErrAuthNicknameTaken   = 10106
	ErrAuthReferrerMissing = 10201
	ErrAuthReferrerInvalid = 10202
	ErrAuthCaptchaMissing  = 10301
	ErrAuthCaptchaWrong    = 10302
	ErrAuthCaptchaExpired  = 10303
	ErrPermissionDenied    = 10403

	// 105xx — 认证节流（20260923-01 登录与 WebSocket 网络安全加固 §3）。
	// ErrAuthTooManyAttempts: 账户/IP 失败计数越阈触发滑动窗口锁定，
	// HTTP 429 + Retry-After；消息含 "retry after Ns" 供前端倒计时解析。
	ErrAuthTooManyAttempts = 10501
	// ErrAuthRateLimited: IP 令牌桶突发限流（login / captcha 签发 / ws 升级），
	// HTTP 429（Retry-After 可选）。
	ErrAuthRateLimited = 10502

	// 2xxxx — validation
	ErrValidationFailed = 20001

	// 3xxxx — resource / game
	ErrRoomNotFound    = 30001
	ErrRoomFull        = 30002
	ErrRoomAlreadyIn   = 30003
	ErrRoomNotIn       = 30004
	ErrGameNotFound    = 30005
	ErrMaxRoomsReached = 30006
	ErrInvalidMove     = 30007
	ErrNotYourTurn     = 30008
	ErrGameNotStarted  = 30009
	ErrGameAlreadyOver = 30010
	// ErrTopicNotFound: 辩论辩题详情查询未命中(内置池 + DB 自定义池均无此 ID)。
	// §20260831-08 GET /api/games/debate/topics/:id。
	ErrTopicNotFound = 30017

	// 3xxxx — spectator-related
	// ErrSpectatorInputForbidden: a spectator tried to send a game input frame
	// (move / bid / play / pass / action / layout / resign / promote).
	ErrSpectatorInputForbidden = 30011
	// ErrAlreadyInOtherRole: a user tried to join (or spectate) a room while
	// already in it as the opposite role (joining as player when already
	// spectating, or vice versa).
	ErrAlreadyInOtherRole = 30012

	// 3xxxx — wallet
	// ErrWalletInsufficientBalance: debit/transfer would drive balance negative.
	ErrWalletInsufficientBalance = 30013
	// ErrWalletDailyRewardClaimed: daily login bonus already collected today.
	ErrWalletDailyRewardClaimed = 30014
	// ErrWalletTxFailed: ledger write failed inside a transaction.
	ErrWalletTxFailed = 30015

	// 3xxxx — admin grant
	// ErrAdminGrantAlreadyClaimed: super-admin daily grant for (provider_id,
	// grant_date) has already been applied. ModelGrantAPI.GrantDaily treats
	// this as a soft "skipped" instead of an error and surfaces the list of
	// already-granted providers in the response — the code is reserved for
	// future GET/inspect endpoints that surface dedup state explicitly.
	ErrAdminGrantAlreadyClaimed = 30022

	// 3xxxx — LLM / AI agents
	// ErrLLMUnavailable: at least one requested AI agent seat could not be
	// filled because no configured LLM provider has a usable API key (every
	// provider is either unconfigured, placeholder, or empty). Returned by
	// CreateRoomWithAgents so the front-end can surface an actionable error
	// instead of silently creating a "0-AI" room.
	ErrLLMUnavailable = 30016

	// 4xxxx — internal
	ErrInternal = 40001
	ErrDB       = 40002

	// 4xxxx — concurrency / transient
	// ErrLockContended: r.mu couldn't be acquired within the deadline
	// (e.g. 200ms lockRoomBriefly). Callers should retry or fall back to
	// a cached snapshot. Used by werewolf REST/WS paths to avoid hanging.
	ErrLockContended = 40100
	// ErrPropEngineUnavailable: 道具引擎未注入(main.go 未接 PropEngine)。
	// 40110 段留给道具系统: 40110-40119 是 prop_use 主路径错误。
	ErrPropEngineUnavailable = 40110
	// ErrPropPlayerDead: 死亡玩家尝试使用道具(Action_UseProp)。
	// R173 报告 P2: ErrValidationFailed 过于模糊,改成专门 code 以便前端明确提示。
	ErrPropPlayerDead = 40111
	// ErrDeadPlayerAction: 死亡玩家尝试执行白天动作(如预言家发起投票)。
	// R176 报告 P1: 40111 已为道具路径设立先例,统一为「死亡玩家不能行动」专属 code。
	ErrDeadPlayerAction = 40112
	// ErrRestartVoteWrongPhase: a vote was submitted outside PhaseRestartVote.
	// 2026-07-10.
	ErrRestartVoteWrongPhase = 30200

	// 35001–35012 — 虚拟城市(virtual_city)专用段(2026-09-14 §财商流P0)。
	// 契约: lag_docs/虚拟城市/已实现/02-架构设计/虚拟城市-WS与HTTP协议契约-v1.md §5。
	ErrVirtualCityRoomNotFound          = 35001
	ErrVirtualCityNotPlaying            = 35002
	ErrVirtualCityNotEnoughPlayers      = 35003
	ErrVirtualCityWrongPhase            = 35004
	ErrVirtualCityPlayerInactive        = 35005
	ErrVirtualCityActionBudgetExhausted = 35006
	ErrVirtualCityInsufficientCash      = 35007
	ErrVirtualCityAssetInvalid          = 35008
	ErrVirtualCityLoanInvalid           = 35009
	ErrVirtualCityGateFailed            = 35010
	ErrVirtualCityNotOwner              = 35011
	ErrVirtualCityProfessionPoolEmpty   = 35012
	// P1: 虚拟城市提前还款 / 明斯基(v2.60 N11-4/N11-5/N12-3/N12-5)专用错误码。
	ErrLoanNotFound           = 35013
	ErrEarlyRepayOnlyMortgage = 35014
	ErrCashNotEnoughRepay     = 35015
	// 35016–35020 — 虚拟城市 P1 真实经济循环 + 社会调研(2026-09-16 §财商流P1-2)。
	ErrVirtualCitySurveyOptionsInvalid    = 35016 // 调研选项数非法(须 2-6)或选项索引越界
	ErrVirtualCitySurveyOpenExists        = 35017 // 已有进行中的调研(每房同时 1 个 open)
	ErrVirtualCitySurveyMonthlyLimit      = 35018 // 本月已达调研发起上限(每月 1 个/累计 20 个)
	ErrVirtualCitySurveyNotFound          = 35019 // 调研不存在/已关闭/已回答
	ErrVirtualCityConsumptionLevelInvalid = 35020 // 消费档位非法(须 0-3)
	// 35021–35035 — 虚拟城市 P2 玩家间交易与财富流动系统(2026-09-16 §财商流P2)。
	ErrVirtualCityListingInvalid    = 35021 // 挂单无效(资产不存在/参数非法)
	ErrVirtualCityListingExpired    = 35022 // 挂单已过期
	ErrVirtualCityListingNotFound   = 35023 // 挂单不存在
	ErrVirtualCityNegotiateNotFound = 35024 // 议价会话不存在
	ErrVirtualCityNotYourTurn       = 35025 // 非议价轮次(非本方出价)
	ErrVirtualCityLoanRateInvalid   = 35026 // 借贷利率超限(0.3%-3.6%/月)
	ErrVirtualCityLoanNoCredit      = 35027 // 信用不足(无法借贷)
	ErrVirtualCityGuarantorConflict = 35028 // 担保人冲突(不可自担保/已担保过)
	ErrVirtualCityAuctionEnded      = 35029 // 拍卖已结束
	ErrVirtualCityBidTooLow         = 35030 // 出价低于当前最高价/起拍价
	ErrVirtualCityNoPrivilege       = 35031 // 权限不足(非自由圈)
	ErrVirtualCityListingFull       = 35032 // 挂单已满(每座位最多 3 笔)
	ErrVirtualCitySelfTrade         = 35033 // 不可自交易(买卖双方相同)
	ErrVirtualCityAuctionNotFound   = 35034 // 拍卖不存在
	ErrVirtualCityInfoNotFound      = 35035 // 信息不存在/未成交
	ErrVirtualCityFullAgentReject   = 35036 // 全 Agent 房间拒绝人类加入(2026-09-19 §全Agent模式)
	// 35037–35041 — 虚拟城市 P1 商业保险与风险转移引擎(2026-09-19 §财商流P1-4)。
	// 契约: lag_docs/虚拟城市/已实现/10-P1保险系统/虚拟城市-P1-商业保险与风险转移引擎-v1.md §9。
	ErrVirtualCityInsuranceKindInvalid = 35037 // kind 非 4 险种之一
	ErrVirtualCityInsuranceExists      = 35038 // 重复投保同险种(每人每险种 1 张有效保单)
	ErrVirtualCityInsuranceNotFound    = 35039 // 退保/操作时无有效保单
	ErrVirtualCityInsuranceAgeGate     = 35040 // 主时钟年龄 > 55 禁止新投保
	ErrVirtualCityInsuranceDisabled    = 35041 // insurance_enabled=false 引擎关闭
	// ErrVirtualCityResidentNotFound: 居民人物卡档案不存在(未锚定/卡号未命中)。
	// 2026-09-21 §档案锚定契约 §7。注:契约原文写 35013,该码已被
	// ErrLoanNotFound(提前还款)占用,顺延取本段首个空闲码 35042 ——
	// 前端契约以 errcode 常量为准。
	ErrVirtualCityResidentNotFound = 35042
	// 35043–35044 — 虚拟城市批次20 股票交易微观结构(2026-09-24
	// §批次20-市长选举启用与股票微观结构 文档3 B3)。
	ErrVirtualCityMarketCircuitBreak = 35043 // 熔断期股票交易暂停(仅 stock_index)
	ErrVirtualCityStockT1Locked      = 35044 // 当月买入份额 T+1 冻结不可卖
	// 35100–35103 — 虚拟城市 City-Human 感知与行动工具(2026-09-22 §CityHuman重构)。
	// 契约: lag_docs/虚拟城市/已实现/12-CityHuman重构/虚拟城市-CityHuman-Agent合并与感知系统设计-v1.md §4.4。
	ErrVirtualCitySenseInvalid   = 35100 // 感知/移动工具参数非法(未知城区、未知 mode、目标不存在)
	ErrVirtualCitySenseLimit     = 35101 // 当月感知/发言次数超限(see/hear/smell 各 ≤2,speak 合计 ≤2)
	ErrVirtualCityMoveForbidden  = 35102 // 当前状态不允许移动(破产清算/停赛中等)
	ErrVirtualCityWhisperTarget  = 35103 // 私聊目标不可达(目标出局/非座位居民/跨房)
	// 35104 — 虚拟城市房间停用人类文字聊天(2026-09-25 §23 房间聊天删除与
	// 3D语音气泡):人类(含观战者)在 virtual_city 房间发 chat.send /
	// chat.whisper 一律拒绝;bot 发言走 SendFromBot/WhisperFromBot 不经此拦截。
	ErrVirtualCityRoomChatDisabled = 35104
	// ErrAlreadyWolfVoted: 狼人在 night_wolves 阶段已投过票(含弃权),
	// 再次调用 wolf_kill 一律拒绝。R196 报告 P1:Bot 8 (GLM-5.2) 反复投票
	// 15+ 次服务端仅覆盖不报错,LLM 看不到反馈陷入循环。
	ErrAlreadyWolfVoted = 30201
)

// DefaultMessages maps a code to its canonical English message.
var DefaultMessages = map[int]string{
	OK: "ok",

	ErrAuthMissingToken:    "missing authorization token",
	ErrAuthInvalidToken:    "invalid authorization token",
	ErrAuthTokenExpired:    "authorization token expired",
	ErrAuthAccountNotFound: "account not found",
	ErrAuthPasswordWrong:   "password does not match",
	ErrAuthAccountTaken:    "account already exists",
	ErrAuthEmailTaken:      "email already registered",
	ErrAuthPhoneTaken:      "phone already registered",
	ErrAuthNicknameTaken:   "nickname already taken",
	ErrAuthReferrerMissing: "referrer invite code is required for registration",
	ErrAuthReferrerInvalid: "referrer invite code is not valid",
	ErrAuthCaptchaMissing:  "captcha is required",
	ErrAuthCaptchaWrong:    "captcha does not match",
	ErrAuthCaptchaExpired:  "captcha has expired",
	ErrPermissionDenied:    "permission denied",
	// 20260923-01 §3 —— 认证节流。10501 在锁定路径会被替换为含
	// "retry after Ns" 的动态消息，供前端倒计时解析。
	ErrAuthTooManyAttempts: "too many failed attempts, retry later",
	ErrAuthRateLimited:     "request rate exceeded, slow down",

	ErrValidationFailed: "validation failed",

	ErrRoomNotFound:    "room not found",
	ErrRoomFull:        "room is full",
	ErrRoomAlreadyIn:   "already in this room",
	ErrRoomNotIn:       "not in this room",
	ErrGameNotFound:    "game not found",
	ErrTopicNotFound:   "debate topic not found",
	ErrMaxRoomsReached: "maximum rooms reached for this game",
	ErrInvalidMove:     "invalid move",
	ErrNotYourTurn:     "not your turn",
	ErrGameNotStarted:  "game has not started",
	ErrGameAlreadyOver: "game is already over",

	ErrSpectatorInputForbidden: "spectators cannot send game input",
	ErrAlreadyInOtherRole:      "already in this room under a different role",

	ErrWalletInsufficientBalance: "insufficient wallet balance",
	ErrWalletDailyRewardClaimed:  "daily reward already claimed today",
	ErrWalletTxFailed:            "wallet ledger write failed",

	ErrAdminGrantAlreadyClaimed: "admin grant already claimed for this provider today",

	ErrLLMUnavailable: "no usable LLM API key configured; AI agent seats cannot be filled",

	ErrInternal: "internal server error",
	ErrDB:       "database error",

	ErrLockContended:         "room lock contended, retry later",
	ErrRestartVoteWrongPhase: "restart vote is not active in this room",
	ErrAlreadyWolfVoted:      "狼人本轮已投票，不能重复投票（wolf_kill 一次性）",

	ErrPropEngineUnavailable: "prop engine unavailable (server not configured)",
	ErrPropPlayerDead:        "死亡玩家不能使用道具（仅存活玩家可用）",
	ErrDeadPlayerAction:      "死亡玩家不能执行该动作（仅存活玩家可用）",

	// 虚拟城市(virtual_city)专用段 — 协议契约文档 §5 默认英文消息照抄。
	ErrVirtualCityRoomNotFound:          "virtual_city room not found",
	ErrVirtualCityNotPlaying:            "virtual_city game not in playing state",
	ErrVirtualCityNotEnoughPlayers:      "virtual_city game needs at least 3 seated players",
	ErrVirtualCityWrongPhase:            "virtual_city action only allowed in acting phase",
	ErrVirtualCityPlayerInactive:        "virtual_city player is stopped/bankrupt/eliminated",
	ErrVirtualCityActionBudgetExhausted: "virtual_city monthly action budget exhausted",
	ErrVirtualCityInsufficientCash:      "virtual_city insufficient cash",
	ErrVirtualCityAssetInvalid:          "virtual_city asset/units invalid",
	ErrVirtualCityLoanInvalid:           "virtual_city loan kind/amount/credit gate invalid",
	ErrVirtualCityGateFailed:            "virtual_city cognition/energy/network gate failed",
	ErrVirtualCityNotOwner:              "virtual_city operation requires room owner",
	ErrVirtualCityProfessionPoolEmpty:   "virtual_city profession pool unavailable",
	// P1: 虚拟城市提前还款 / 明斯基错误码默认英文消息。
	ErrLoanNotFound:           "loan not found",
	ErrEarlyRepayOnlyMortgage: "early repay only allowed for mortgage loans",
	ErrCashNotEnoughRepay:     "cash not enough for early repayment (including penalty)",
	// 35016–35020 — 虚拟城市 P1 真实经济循环 + 社会调研(2026-09-16 §财商流P1-2)。
	ErrVirtualCitySurveyOptionsInvalid:    "survey options invalid (need 2-6 non-empty) or option index out of range",
	ErrVirtualCitySurveyOpenExists:        "an open survey already exists (one open per room)",
	ErrVirtualCitySurveyMonthlyLimit:      "survey launch limit reached (1 per month / 20 per room)",
	ErrVirtualCitySurveyNotFound:          "survey not found / closed / already answered",
	ErrVirtualCityConsumptionLevelInvalid: "consumption level invalid (must be 0-3)",
	// 35021–35035 — 虚拟城市 P2 玩家间交易与财富流动系统(2026-09-16 §财商流P2)。
	ErrVirtualCityListingInvalid:    "virtual_city listing invalid (asset not found or params invalid)",
	ErrVirtualCityListingExpired:    "virtual_city listing expired",
	ErrVirtualCityListingNotFound:   "virtual_city listing not found",
	ErrVirtualCityNegotiateNotFound: "virtual_city negotiate session not found",
	ErrVirtualCityNotYourTurn:       "virtual_city negotiate not your turn to respond",
	ErrVirtualCityLoanRateInvalid:   "virtual_city loan rate out of range (0.3%-3.6% per month)",
	ErrVirtualCityLoanNoCredit:      "virtual_city loan credit score too low",
	ErrVirtualCityGuarantorConflict: "virtual_city guarantor conflict (self-guarantee or already guaranteed)",
	ErrVirtualCityAuctionEnded:      "virtual_city auction already ended",
	ErrVirtualCityBidTooLow:         "virtual_city bid too low (below current highest/reserve)",
	ErrVirtualCityNoPrivilege:       "virtual_city operation requires free-circle privilege",
	ErrVirtualCityListingFull:       "virtual_city listing full (max 3 per seat)",
	ErrVirtualCitySelfTrade:         "virtual_city self-trade not allowed (buyer=seller)",
	ErrVirtualCityAuctionNotFound:   "virtual_city auction not found",
	ErrVirtualCityInfoNotFound:      "virtual_city info not found or not won",
	ErrVirtualCityFullAgentReject:   "virtual_city full-agent room rejects human join, use spectate instead",
	// 35037–35041 — 虚拟城市 P1 商业保险(2026-09-19 §财商流P1-4 §9 默认英文消息)。
	ErrVirtualCityInsuranceKindInvalid: "virtual_city insurance kind invalid",
	ErrVirtualCityInsuranceExists:      "virtual_city active policy already exists for this kind",
	ErrVirtualCityInsuranceNotFound:    "virtual_city no active policy for this kind",
	ErrVirtualCityInsuranceAgeGate:     "virtual_city insurance purchase not allowed above age 55",
	ErrVirtualCityInsuranceDisabled:    "virtual_city insurance engine disabled by config",
	// 35042 — 虚拟城市居民人物卡档案(2026-09-21 §档案锚定契约 §7)。
	ErrVirtualCityResidentNotFound: "居民档案不存在",
	// 35043–35044 — 虚拟城市批次20 股票交易微观结构(2026-09-24 文档3 B3)。
	ErrVirtualCityMarketCircuitBreak: "virtual_city stock trading suspended by monthly circuit breaker",
	ErrVirtualCityStockT1Locked:      "virtual_city stock units bought this month are T+1 locked and not sellable",
	// 35100–35103 — 虚拟城市 City-Human 感知与行动工具(2026-09-22 §CityHuman重构)。
	ErrVirtualCitySenseInvalid:   "virtual_city sense/move params invalid (unknown district/mode/target)",
	ErrVirtualCitySenseLimit:     "virtual_city sense/speak monthly limit reached",
	ErrVirtualCityMoveForbidden:  "virtual_city move forbidden in current state",
	ErrVirtualCityWhisperTarget:  "virtual_city whisper target unreachable",
	// 中文文案(前端 i18n 落地):虚拟城市房间已停用文字聊天，居民发言请在 3D 地图查看。
	ErrVirtualCityRoomChatDisabled: "virtual_city room chat is disabled",
}

// Code constructs a Coded error.
func Code(code int) *Error {
	msg, ok := DefaultMessages[code]
	if !ok {
		msg = "unknown error"
	}
	return &Error{Code: code, Message: msg}
}

// CodeMsg constructs a Coded error with a custom message (code preserved).
func CodeMsg(code int, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// Error is the unified error type returned across HTTP and WSS boundaries.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return "nil error"
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// AsError unwraps a generic error into *Error if possible, else wraps it as ErrInternal.
func AsError(err error) *Error {
	if err == nil {
		return Code(OK)
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	return CodeMsg(ErrInternal, err.Error())
}
