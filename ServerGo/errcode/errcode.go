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
